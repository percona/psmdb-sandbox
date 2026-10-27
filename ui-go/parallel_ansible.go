package main

import (
	"fmt"
	"regexp"
	"strings"
)

// pmmServerPart is the job part name of the PMM server playbook run.
const pmmServerPart = "pmm-server"

var (
	jobPartNameRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	jobPartNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)
)

func validJobPartName(name string) bool { return jobPartNameRe.MatchString(name) }

// jobPartName turns an inventory name into a part name that is safe to use in
// file names and shell words.
func jobPartName(name string) string {
	s := jobPartNameUnsafe.ReplaceAllString(name, "_")
	if s == "" {
		s = "_"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// parallelPartNames returns a unique, file-safe part name per inventory that
// never collides with the PMM server part.
func parallelPartNames(invs []parallelInventory) []string {
	used := map[string]bool{pmmServerPart: true}
	names := make([]string, len(invs))
	for i, inv := range invs {
		base := jobPartName(inv.Name)
		name := base
		for n := 2; used[name]; n++ {
			suffix := fmt.Sprintf("_%d", n)
			name = base[:min(len(base), 64-len(suffix))] + suffix
		}
		used[name] = true
		names[i] = name
	}
	return names
}

// parallelInventory is one inventory configured by parallelConfigureShell.
type parallelInventory struct {
	Name      string // topology name, e.g. "cl01"
	Inventory string // inventory file name, unquoted
	ExtraArgs string // extra ansible-playbook arguments, shell-quoted, with a leading space
}

// sshWaitShell waits until every host matching pattern in the (quoted)
// inventory answers an Ansible ping, giving up after 10 minutes.
func sshWaitShell(inv, pattern string) string {
	return fmt.Sprintf(
		`printf "Waiting for SSH on %%s (up to 10 min)…\n" %[1]s; `+
			`_ssh_ready=false; `+
			`for _n in $(seq 1 20); do `+
			`ansible -i %[1]s %[2]s -m ping --timeout=10 2>&1 && { _ssh_ready=true; break; }; `+
			`printf "  attempt %%s/20 – not ready yet, retrying in 10s…\n" "$_n"; `+
			`[ "$_n" -lt 20 ] && sleep 10; done; `+
			`$_ssh_ready || { printf "ERROR: timed out waiting for SSH (%%s)\n" %[1]s; exit 1; }; `,
		inv, pattern,
	)
}

// parallelConfigureShell returns a shell block that runs playbook against all
// inventories at once instead of one after another:
//
//   - The PMM server is set up once, from the first inventory with a [pmm]
//     group, while the MongoDB hosts are installed (--tags pmm_server).
//   - Each inventory runs the playbook once without the PMM server play. Its
//     PMM client plays wait for the PMM server API before registering (see
//     ansible/tasks/wait_for_pmm_server.yml).
//   - plgm is configured only by the first inventory with a [plgm] group.
//
// Each run writes $PSMDB_JOBS_DIR/$PSMDB_JOB_ID.<part>.{log,status,start,end},
// and the part names are listed in $PSMDB_JOB_ID.parts. A failed run does not
// stop the others; the block fails once all of them have finished.
func parallelConfigureShell(playbook string, invs []parallelInventory) string {
	if len(invs) == 0 {
		return `printf "WARNING: no clusters or replica sets configured – nothing to run\n"`
	}
	pb := shellQuote(playbook)
	parts := parallelPartNames(invs)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w(`{ _psb_dir="${PSMDB_JOBS_DIR:-.}"; _psb_id="${PSMDB_JOB_ID:-job}"; _psb_pids=""; _psb_pmm=""; _psb_plgm=""`)
	w(`_psb_run() { _psb_b="$_psb_dir/$_psb_id.$1"; date +%%s > "$_psb_b.start"; echo running > "$_psb_b.status"; printf '==> [%%s] started\n' "$1"; ( "$2" ) > "$_psb_b.log" 2>&1; _psb_rc=$?; date +%%s > "$_psb_b.end"; if [ "$_psb_rc" -eq 0 ]; then _psb_st=success; else _psb_st="failed:$_psb_rc"; fi; echo "$_psb_st" > "$_psb_b.status"; _psb_s=$(( $(cat "$_psb_b.end") - $(cat "$_psb_b.start") )); printf '==> [%%s] finished: %%s (%%dm %%ds)\n' "$1" "$_psb_st" $((_psb_s / 60)) $((_psb_s %% 60)); return "$_psb_rc"; }`)

	for i, inv := range invs {
		q := shellQuote(inv.Inventory)
		w(`if [ -z "$_psb_pmm" ] && grep -qs '^\[pmm\]' %s; then _psb_pmm=%d; fi`, q, i)
		w(`if [ -z "$_psb_plgm" ] && grep -qs '^\[plgm\]' %s; then _psb_plgm=%d; fi`, q, i)
	}

	// PMM server run: uses the vars of whichever inventory provides [pmm].
	w(`_psb_p_pmm() { case "$_psb_pmm" in`)
	for i, inv := range invs {
		q := shellQuote(inv.Inventory)
		w(`%d) %sprintf "==> ansible-playbook -i %%s --tags pmm_server --limit pmm\n" %s; ansible-playbook -i %s %s%s --tags pmm_server --limit pmm;;`,
			i, sshWaitShell(q, "pmm"), q, q, pb, inv.ExtraArgs)
	}
	w(`esac; }`)

	for i, inv := range invs {
		q := shellQuote(inv.Inventory)
		run := fmt.Sprintf("ansible-playbook -i %s %s%s", q, pb, inv.ExtraArgs)
		w(`_psb_p_%d() { [ -f %s ] || { printf "ERROR: inventory file %%s not found\n" %s; exit 1; }; %s`, i, q, q, sshWaitShell(q, "all"))
		w(`_psb_skip=""; if [ -n "$_psb_pmm" ]; then _psb_skip=pmm_server; fi`)
		w(`if [ -n "$_psb_plgm" ] && [ "$_psb_plgm" != %d ]; then _psb_skip="${_psb_skip:+$_psb_skip,}plgm"; fi`, i)
		w(`if [ -n "$_psb_skip" ]; then printf "==> ansible-playbook -i %%s --skip-tags %%s\n" %s "$_psb_skip"; %s --skip-tags "$_psb_skip" || exit $?`, q, run)
		w(`else printf "==> ansible-playbook -i %%s\n" %s; %s || exit $?; fi; }`, q, run)
	}

	w(`: > "$_psb_dir/$_psb_id.parts"`)
	w(`if [ -n "$_psb_pmm" ]; then echo %[1]s >> "$_psb_dir/$_psb_id.parts"; echo pending > "$_psb_dir/$_psb_id.%[1]s.status"; fi`, pmmServerPart)
	for _, part := range parts {
		w(`echo %[1]s >> "$_psb_dir/$_psb_id.parts"; echo pending > "$_psb_dir/$_psb_id.%[1]s.status"`, part)
	}
	w(`if [ -n "$_psb_pmm" ]; then _psb_run %s _psb_p_pmm & _psb_pids="$_psb_pids $!"; fi`, pmmServerPart)
	for i, part := range parts {
		w(`_psb_run %s _psb_p_%d & _psb_pids="$_psb_pids $!"`, part, i)
	}
	w(`_psb_failed=0; for _psb_pid in $_psb_pids; do wait "$_psb_pid" || _psb_failed=1; done`)
	b.WriteString(`[ "$_psb_failed" -eq 0 ] || { printf "ERROR: one or more playbooks failed – see the per-playbook tabs\n"; exit 1; }; printf "==> All playbooks finished successfully\n"; }`)
	return b.String()
}
