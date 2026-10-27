package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const stubAnsiblePlaybook = `#!/bin/bash
args="$*"
case "$args" in
  *"--tags pmm_server"*) sleep 1 ;;
esac
printf '%s\n' "$args" >> "$CALLS"
for f in $FAIL_MATCH; do
  case "$args" in *"$f"*) exit 3 ;; esac
done
exit 0
`

type parallelRun struct {
	dir   string
	calls []string
	err   error
	out   string
}

func runParallelShell(t *testing.T, invs map[string]string, order []string, failMatch string) parallelRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires bash")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("requires bash")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ansible-playbook"), []byte(stubAnsiblePlaybook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ansible"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var list []parallelInventory
	for _, name := range order {
		file := "env_inventory_" + name
		if err := os.WriteFile(filepath.Join(dir, file), []byte(invs[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		list = append(list, parallelInventory{Name: name, Inventory: file, ExtraArgs: " --skip-tags vault"})
	}
	script := parallelConfigureShell("/ansible/main.yml", list)
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	callsPath := filepath.Join(dir, "calls")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"PSMDB_JOBS_DIR="+dir,
		"PSMDB_JOB_ID=job1",
		"CALLS="+callsPath,
		"FAIL_MATCH="+failMatch,
	)
	out, err := cmd.CombinedOutput()
	data, _ := os.ReadFile(callsPath)
	var calls []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return parallelRun{dir: dir, calls: calls, err: err, out: string(out)}
}

func (r parallelRun) find(t *testing.T, substrs ...string) (string, int) {
	t.Helper()
	for i, c := range r.calls {
		ok := true
		for _, s := range substrs {
			if !strings.Contains(c, s) {
				ok = false
				break
			}
		}
		if ok {
			return c, i
		}
	}
	return "", -1
}

func (r parallelRun) status(t *testing.T, part string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "job1."+part+".status"))
	if err != nil {
		t.Fatalf("read %s status: %v", part, err)
	}
	return strings.TrimSpace(string(data))
}

const (
	invClusterWithPMM = "[cfg]\nh1\n\n[pmm]\npmm1\n\n[plgm]\nplgm1\n"
	invReplsetWithPMM = "[replsets:children]\nrs01\n\n[pmm]\npmm1\n\n[plgm]\nplgm1\n"
	invPMMClients     = "[pmm]\npmm1\n\n[pmm_bare_client]\nc1\n"
)

func TestParallelConfigureRunsPMMServerOnceAlongsideInventories(t *testing.T) {
	r := runParallelShell(t, map[string]string{
		"cl01": invClusterWithPMM, "rs01": invReplsetWithPMM, "pmm_clients": invPMMClients,
	}, []string{"cl01", "pmm_clients", "rs01"}, "")
	if r.err != nil {
		t.Fatalf("shell failed: %v\n%s", r.err, r.out)
	}

	manifest, _ := os.ReadFile(filepath.Join(r.dir, "job1.parts"))
	if got := strings.Fields(string(manifest)); strings.Join(got, ",") != "pmm-server,cl01,pmm_clients,rs01" {
		t.Fatalf("parts manifest = %v", got)
	}
	for _, part := range []string{"pmm-server", "cl01", "pmm_clients", "rs01"} {
		if s := r.status(t, part); s != "success" {
			t.Errorf("%s status = %q", part, s)
		}
		for _, ext := range []string{"log", "start", "end"} {
			if _, err := os.Stat(filepath.Join(r.dir, "job1."+part+"."+ext)); err != nil {
				t.Errorf("%s missing .%s file", part, ext)
			}
		}
	}

	pmmCalls := 0
	for _, c := range r.calls {
		if strings.Contains(c, "--tags pmm_server") {
			pmmCalls++
			if !strings.Contains(c, "-i env_inventory_cl01 ") || !strings.Contains(c, "--limit pmm") {
				t.Errorf("PMM server should run from the first inventory, limited to pmm: %s", c)
			}
		}
	}
	if pmmCalls != 1 {
		t.Errorf("PMM server playbook ran %d times, want 1", pmmCalls)
	}

	if len(r.calls) != 4 {
		t.Errorf("want the PMM server run plus one run per inventory, got %v", r.calls)
	}
	for _, name := range []string{"cl01", "pmm_clients", "rs01"} {
		run, idx := r.find(t, "-i env_inventory_"+name+" ", "--skip-tags pmm_server")
		if idx < 0 {
			t.Fatalf("%s: missing run without the PMM server play in %v", name, r.calls)
		}
		if !strings.Contains(run, "--skip-tags vault") {
			t.Errorf("%s: run lost the inventory's extra args: %s", name, run)
		}
		wantPlgmSkip := name != "cl01"
		if got := strings.Contains(run, "--skip-tags pmm_server,plgm"); got != wantPlgmSkip {
			t.Errorf("%s: plgm skipped = %v, want %v: %s", name, got, wantPlgmSkip, run)
		}
		if strings.Contains(run, "--tags ") {
			t.Errorf("%s: inventory runs must not be limited to tags: %s", name, run)
		}
	}
}

func TestParallelConfigureLetsOtherRunsFinishOnFailure(t *testing.T) {
	r := runParallelShell(t, map[string]string{
		"cl01": invClusterWithPMM, "rs01": invReplsetWithPMM,
	}, []string{"cl01", "rs01"}, "env_inventory_rs01")
	if r.err == nil {
		t.Fatalf("shell should fail when one inventory fails\n%s", r.out)
	}
	if s := r.status(t, "rs01"); s != "failed:3" {
		t.Errorf("rs01 status = %q", s)
	}
	if s := r.status(t, "cl01"); s != "success" {
		t.Errorf("cl01 status = %q, other runs must finish", s)
	}
	if s := r.status(t, "pmm-server"); s != "success" {
		t.Errorf("pmm-server status = %q", s)
	}
	if !strings.Contains(r.out, "one or more playbooks failed") {
		t.Errorf("missing failure summary:\n%s", r.out)
	}
}

func TestParallelConfigureFailsWhenPMMServerFails(t *testing.T) {
	// Only the PMM server run uses --limit.
	r := runParallelShell(t, map[string]string{"cl01": invClusterWithPMM}, []string{"cl01"}, "--limit")
	if r.err == nil {
		t.Fatal("shell should fail when the PMM server fails")
	}
	if s := r.status(t, "pmm-server"); s != "failed:3" {
		t.Errorf("pmm-server status = %q", s)
	}
	if s := r.status(t, "cl01"); s != "success" {
		t.Errorf("cl01 status = %q, the inventory run must still finish", s)
	}
}

// The inventory runs skip the PMM server play, so every play that registers
// with PMM must wait for the server itself.
func TestPMMRegistrationWaitsForPMMServer(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "ansible", "pmm_client_install.yml"))
	if err != nil {
		t.Fatal(err)
	}
	plays := strings.Split(string(data), "\n- name: ")
	registering := 0
	for _, play := range plays {
		registerAt := strings.Index(play, "pmm-admin config")
		if registerAt < 0 {
			continue
		}
		registering++
		name, _, _ := strings.Cut(play, "\n")
		waitAt := strings.Index(play, "import_tasks: tasks/wait_for_pmm_server.yml")
		if waitAt < 0 || waitAt > registerAt {
			t.Errorf("play %q registers with PMM without waiting for the PMM server first", name)
		}
	}
	if registering != 3 {
		t.Errorf("found %d plays that register with PMM, want 3", registering)
	}

	main, err := os.ReadFile(filepath.Join("..", "ansible", "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if plgm, pmm := strings.Index(string(main), "plgm_install.yml"), strings.Index(string(main), "pmm_client_install.yml"); plgm < 0 || plgm > pmm {
		t.Error("plgm_install.yml should be imported before pmm_client_install.yml so it does not wait for PMM")
	}
}

func TestParallelConfigureWithoutPMMRunsPlainPlaybook(t *testing.T) {
	r := runParallelShell(t, map[string]string{
		"cl01": "[cfg]\nh1\n\n[plgm]\nplgm1\n", "rs01": "[replsets:children]\nrs01\n\n[plgm]\nplgm1\n",
	}, []string{"cl01", "rs01"}, "")
	if r.err != nil {
		t.Fatalf("shell failed: %v\n%s", r.err, r.out)
	}
	if len(r.calls) != 2 {
		t.Fatalf("want one run per inventory, got %v", r.calls)
	}
	cl, _ := r.find(t, "-i env_inventory_cl01 ")
	if strings.Contains(cl, "pmm_server") || strings.Contains(cl, "plgm") {
		t.Errorf("cl01 should run the full playbook: %s", cl)
	}
	rs, _ := r.find(t, "-i env_inventory_rs01 ")
	if !strings.Contains(rs, "--skip-tags plgm") {
		t.Errorf("rs01 should skip plgm: %s", rs)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "job1.pmm-server.status")); err == nil {
		t.Error("no PMM server part expected")
	}
}

func TestParallelConfigureRunsInParallel(t *testing.T) {
	r := runParallelShell(t, map[string]string{
		"cl01": invClusterWithPMM, "rs01": invReplsetWithPMM,
	}, []string{"cl01", "rs01"}, "")
	if r.err != nil {
		t.Fatalf("shell failed: %v\n%s", r.err, r.out)
	}
	// The PMM server stub sleeps 1s; sequential runs would put both core
	// runs after it.
	_, pmmIdx := r.find(t, "--tags pmm_server")
	_, cl := r.find(t, "-i env_inventory_cl01 ", "--skip-tags pmm_server")
	_, rs := r.find(t, "-i env_inventory_rs01 ", "--skip-tags pmm_server")
	if cl > pmmIdx || rs > pmmIdx {
		t.Errorf("core runs should finish before the slow PMM server run: %v", r.calls)
	}
}

func TestParallelPartNamesAreUniqueAndSafe(t *testing.T) {
	got := parallelPartNames([]parallelInventory{
		{Name: "cl01"}, {Name: "pmm-server"}, {Name: "a b"}, {Name: "a_b"}, {Name: "../x"},
	})
	want := []string{"cl01", "pmm-server_2", "a_b", "a_b_2", "___x"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parallelPartNames = %v, want %v", got, want)
	}
	for _, n := range got {
		if !validJobPartName(n) {
			t.Errorf("%q is not a valid part name", n)
		}
	}
}

func TestReadJobPartsReportsInterruptedParts(t *testing.T) {
	old := jobsDir
	jobsDir = t.TempDir()
	t.Cleanup(func() { jobsDir = old })
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(jobsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("j.parts", "pmm-server\ncl01\n../evil\n")
	write("j.status", "running")
	write("j.pmm-server.status", "success\n")
	write("j.pmm-server.start", "100\n")
	write("j.pmm-server.end", "160\n")
	write("j.cl01.status", "running\n")
	write("j.cl01.start", "100\n")

	parts := readJobParts("j", time.Unix(130, 0))
	if len(parts) != 2 {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[0].Elapsed != 60 || parts[0].Status != "success" {
		t.Errorf("pmm-server = %+v", parts[0])
	}
	if parts[1].Elapsed != 30 || parts[1].Status != "running" {
		t.Errorf("cl01 = %+v", parts[1])
	}
	if jobHasPart("j", "../evil") {
		t.Error("invalid part names must be rejected")
	}

	write("j.status", "failed:137")
	parts = readJobParts("j", time.Unix(130, 0))
	if parts[1].Status != "interrupted" {
		t.Errorf("unfinished part of a finished job = %q, want interrupted", parts[1].Status)
	}
}

func TestJobPartEndpoints(t *testing.T) {
	old := jobsDir
	jobsDir = t.TempDir()
	t.Cleanup(func() { jobsDir = old })
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(jobsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("j.log", "main output\n")
	write("j.status", "success")
	write("j.parts", "cl01\n")
	write("j.cl01.log", "\x1b[0;32mok: [h1]\x1b[0m\n")
	write("j.cl01.status", "success\n")
	write("j.cl01.start", "100\n")
	write("j.cl01.end", "175\n")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/job/{job_id}/log", jobLogHandler)
	mux.HandleFunc("GET /api/job/{job_id}/parts", jobPartsHandler)
	mux.HandleFunc("GET /api/job/{job_id}/stream", jobStreamHandler)
	get := func(url string) (int, string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		return rec.Code, rec.Body.String()
	}

	if code, body := get("/api/job/j/parts"); code != 200 || !strings.Contains(body, `"name":"cl01"`) || !strings.Contains(body, `"elapsed":75`) {
		t.Errorf("parts: %d %s", code, body)
	}
	if code, body := get("/api/job/j/log?part=cl01"); code != 200 || !strings.Contains(body, `"log":"ok: [h1]\n"`) || !strings.Contains(body, `"status":"success"`) {
		t.Errorf("part log: %d %s", code, body)
	}
	if code, body := get("/api/job/j/log"); code != 200 || !strings.Contains(body, "main output") {
		t.Errorf("main log: %d %s", code, body)
	}
	if code, _ := get("/api/job/j/log?part=rs01"); code != http.StatusNotFound {
		t.Errorf("unknown part log status = %d", code)
	}
	if code, body := get("/api/job/j/stream?part=cl01"); code != 200 || !strings.Contains(body, `data: "ok: [h1]"`) || !strings.Contains(body, "event: done\ndata: \"success\"") {
		t.Errorf("part stream: %d %s", code, body)
	}
	if code, body := get("/api/job/other/parts"); code != 200 || !strings.Contains(body, `"parts":[]`) {
		t.Errorf("job without parts: %d %s", code, body)
	}
}

func TestMainPlaybookSkipsRepeatedFactGathering(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "ansible", "main.yml"))
	if err != nil {
		t.Fatal(err)
	}
	main := string(data)
	imports := strings.Count(main, "- import_playbook:")
	if imports == 0 || strings.Count(main, "skip_repeated_facts: true\n") < imports {
		t.Errorf("every import_playbook in main.yml must pass skip_repeated_facts: true")
	}
}
