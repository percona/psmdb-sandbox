package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Host ports published by containers or held by other local processes are
// invisible to the saved environment definitions. dockerHostScan finds them so
// that port assignment and validation never collide with unrelated workloads.

const (
	dockerDefaultSeaweedFSFilerPort = 8888
	dockerDefaultLdapAdminPort      = 8080
)

type dockerPublishedPort struct {
	Port      int
	Container string
	EnvID     string // value of the psmdb-sandbox.environment label
}

var (
	listDockerPublishedPorts = dockerPublishedPortsFromCLI
	hostPortAvailable        = tcpPortAvailable
)

// parseDockerPublishedPorts extracts the published host TCP ports from the
// Ports column of `docker ps`, e.g.
// "0.0.0.0:27017-27019->27017-27019/tcp, [::]:8443->8443/tcp, 389/tcp".
func parseDockerPublishedPorts(field string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, mapping := range strings.Split(field, ",") {
		mapping = strings.TrimSpace(mapping)
		host, _, published := strings.Cut(mapping, "->")
		if !published || strings.HasSuffix(mapping, "/udp") {
			continue
		}
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[i+1:]
		}
		first, last, isRange := strings.Cut(host, "-")
		lo, err := strconv.Atoi(first)
		if err != nil {
			continue
		}
		hi := lo
		if isRange {
			if hi, err = strconv.Atoi(last); err != nil || hi < lo {
				continue
			}
		}
		for port := lo; port <= hi; port++ {
			if !seen[port] {
				seen[port] = true
				ports = append(ports, port)
			}
		}
	}
	return ports
}

func dockerPublishedPortsFromCLI() ([]dockerPublishedPort, error) {
	format := fmt.Sprintf("{{.Names}}\t{{.Label %q}}\t{{.Ports}}", dockerEnvironmentLabel)
	out, err := execOutput("docker", "ps", "--format", format)
	if err != nil {
		return nil, err
	}
	var published []dockerPublishedPort
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		for _, port := range parseDockerPublishedPorts(fields[2]) {
			published = append(published, dockerPublishedPort{Port: port, Container: fields[0], EnvID: fields[1]})
		}
	}
	return published, nil
}

// tcpPortAvailable reports whether a TCP port can be bound on both the loopback
// and wildcard addresses. Permission errors do not mean the port is in use.
func tcpPortAvailable(port int) bool {
	for _, host := range []string{"127.0.0.1", "0.0.0.0"} {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return errors.Is(err, os.ErrPermission)
		}
		listener.Close()
	}
	return true
}

type dockerHostScan struct {
	foreign map[int]string // host port -> container outside this environment
	own     map[int]bool   // host ports published by this environment's containers
	probed  map[int]string // bind-probe results for ports without a known container
}

// newDockerHostScan inspects running containers once. Containers belonging to
// envID (by label, or by name prefix for unlabeled legacy containers) are not
// conflicts: Terraform manages them. If Docker cannot be queried, only the bind
// probe remains.
func newDockerHostScan(envID string) *dockerHostScan {
	scan := &dockerHostScan{foreign: map[int]string{}, own: map[int]bool{}, probed: map[int]string{}}
	published, err := listDockerPublishedPorts()
	if err != nil {
		slog.Warn("docker port scan failed; checking host ports by binding only", "err", err)
		return scan
	}
	for _, item := range published {
		owned := envID != "" && (item.EnvID == envID || (item.EnvID == "" && strings.HasPrefix(item.Container, envID+"-")))
		if owned {
			scan.own[item.Port] = true
		} else if _, exists := scan.foreign[item.Port]; !exists {
			scan.foreign[item.Port] = item.Container
		}
	}
	return scan
}

// busyBy describes what holds port on the host, or returns "" when it is free.
func (s *dockerHostScan) busyBy(port int) string {
	if s == nil || port <= 0 || s.own[port] {
		return ""
	}
	if name, ok := s.foreign[port]; ok {
		return "container " + name
	}
	holder, done := s.probed[port]
	if !done {
		if !hostPortAvailable(port) {
			holder = "another process on this host"
		}
		s.probed[port] = holder
	}
	return holder
}

func (s *dockerHostScan) busy(port int) bool {
	return s.busyBy(port) != ""
}

// dockerPortBusy combines a known-occupied set with host checks.
func dockerPortBusy(port int, occupied map[int]struct{}, busy []func(int) bool) bool {
	if _, taken := occupied[port]; taken {
		return true
	}
	for _, check := range busy {
		if check != nil && check(port) {
			return true
		}
	}
	return false
}

// dockerPortsReassignable reports whether an environment may still change its
// host ports: nothing has been applied yet, so no data or client depends on them.
func dockerPortsReassignable(env *Environment) bool {
	if env == nil {
		return true
	}
	return env.LastAppliedConfig == nil && env.Status != "running" && env.Status != "deploy_success"
}

// dockerStatePorts collects the host ports of the other running Docker
// environments recorded in state.
func dockerStatePorts(state map[string]*Environment, excludeEnvID string) map[int]struct{} {
	occupied := make(map[int]struct{})
	for envID, env := range state {
		if envID == excludeEnvID || env == nil || env.Platform != "docker" || env.Status != "running" {
			continue
		}
		for _, use := range dockerConfigPortUses(env.Config, "") {
			occupied[use.Port] = struct{}{}
		}
	}
	return occupied
}

// reassignBusyDockerPorts moves every host port of cfg that is busy on the host
// or used by another environment to the next free one, and describes each move.
func reassignBusyDockerPorts(cfg *Config, scan *dockerHostScan, otherEnvPorts map[int]struct{}) []string {
	occupied := make(map[int]struct{}, len(otherEnvPorts))
	for port := range otherEnvPorts {
		occupied[port] = struct{}{}
	}
	hostChecks := []func(int) bool{scan.busy}
	holderOf := func(port int) string {
		if holder := scan.busyBy(port); holder != "" {
			return holder
		}
		return "another environment"
	}
	var notes []string

	move := func(label string, port *int, effective int) {
		if effective <= 0 {
			return
		}
		if dockerPortBusy(effective, occupied, hostChecks) {
			next := nextFreeDockerPort(effective+1, occupied, scan.busy)
			notes = append(notes, fmt.Sprintf("%s: host port %d is in use by %s; using %d", label, effective, holderOf(effective), next))
			*port, effective = next, next
		}
		occupied[effective] = struct{}{}
	}

	for _, nr := range sortedReplsets(cfg.Replsets) {
		rs := nr.Config
		var busyPort int
		for _, port := range dockerReplsetPorts(rs) {
			if dockerPortBusy(port, occupied, hostChecks) {
				busyPort = port
				break
			}
		}
		if busyPort != 0 {
			next := rs.ReplsetPort + dockerReplsetPortStep
			for !dockerReplsetPortRangeFree(next, rs, occupied, scan.busy) {
				next += dockerReplsetPortStep
			}
			notes = append(notes, fmt.Sprintf("replica set %s: host port %d is in use by %s; using ports starting at %d", nr.Name, busyPort, holderOf(busyPort), next))
			rs.ReplsetPort, rs.ArbiterPort, rs.ArbiterBasePort = next, next, next+10
			cfg.Replsets[nr.Name] = rs
		}
		for _, port := range dockerReplsetPorts(rs) {
			occupied[port] = struct{}{}
		}
	}
	for _, ns := range sortedPmmServers(cfg.PmmServers) {
		svc := ns.Config
		move("PMM server "+ns.Name, &svc.PmmExternalPort, intDefault(svc.PmmExternalPort, svc.PmmPort))
		cfg.PmmServers[ns.Name] = svc
	}
	for _, ns := range sortedSeaweedFSServers(cfg.SeaweedFSServers) {
		svc := ns.Config
		move("SeaweedFS API "+ns.Name, &svc.SeaweedFSPort, svc.SeaweedFSPort)
		move("SeaweedFS admin "+ns.Name, &svc.SeaweedFSAdminPort, svc.SeaweedFSAdminPort)
		move("SeaweedFS filer "+ns.Name, &svc.SeaweedFSFilerPort, intDefault(svc.SeaweedFSFilerPort, dockerDefaultSeaweedFSFilerPort))
		cfg.SeaweedFSServers[ns.Name] = svc
	}
	for _, ns := range sortedLdapServers(cfg.LdapServers) {
		svc := ns.Config
		move("LDAP admin UI "+ns.Name, &svc.LdapAdminPort, intDefault(svc.LdapAdminPort, dockerDefaultLdapAdminPort))
		cfg.LdapServers[ns.Name] = svc
	}
	if cfg.EnablePlgm {
		move("plgm Web UI", &cfg.PlgmExternalPort, intDefault(cfg.PlgmExternalPort, plgmDefaultExternalPort))
	}
	return notes
}

// validateDockerHostPorts rejects a configuration whose published host ports
// are held by containers outside the environment or by other processes.
func validateDockerHostPorts(cfg Config, scan *dockerHostScan) error {
	var conflicts []string
	for _, use := range dockerConfigPortUses(cfg, "") {
		if !use.Published {
			continue
		}
		if holder := scan.busyBy(use.Port); holder != "" {
			conflicts = append(conflicts, fmt.Sprintf("port %d (%s) is in use by %s", use.Port, use.Label, holder))
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return fmt.Errorf("docker host port conflicts detected:\n- %s", strings.Join(conflicts, "\n- "))
}
