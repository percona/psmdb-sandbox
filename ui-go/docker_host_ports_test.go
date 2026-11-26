package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func stubDockerHostPorts(t *testing.T, published []dockerPublishedPort, listErr error, busyOnHost ...int) {
	t.Helper()
	origList, origAvailable := listDockerPublishedPorts, hostPortAvailable
	t.Cleanup(func() { listDockerPublishedPorts, hostPortAvailable = origList, origAvailable })
	listDockerPublishedPorts = func() ([]dockerPublishedPort, error) { return published, listErr }
	hostPortAvailable = func(port int) bool {
		for _, busy := range busyOnHost {
			if port == busy {
				return false
			}
		}
		return true
	}
}

func TestParseDockerPublishedPorts(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  []int
	}{
		{"empty", "", nil},
		{"unpublished", "389/tcp, 636/tcp", nil},
		{"ipv4 and ipv6 deduplicated", "0.0.0.0:27017->27017/tcp, [::]:27017->27017/tcp", []int{27017}},
		{"range", "127.0.0.1:27017-27019->27017-27019/tcp", []int{27017, 27018, 27019}},
		{"mixed", "0.0.0.0:8443->8443/tcp, 80/tcp, 127.0.0.1:55001->27017/tcp", []int{8443, 55001}},
		{"udp ignored", "0.0.0.0:5353->5353/udp", nil},
		{"malformed", "0.0.0.0:abc->1/tcp, 0.0.0.0:9-3->1/tcp", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDockerPublishedPorts(tt.field); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseDockerPublishedPorts(%q) = %v, want %v", tt.field, got, tt.want)
			}
		})
	}
}

func TestDockerHostScanExcludesOwnContainers(t *testing.T) {
	stubDockerHostPorts(t, []dockerPublishedPort{
		{Port: 27017, Container: "mine-rs01-0", EnvID: "mine"},
		{Port: 27018, Container: "mine-legacy", EnvID: ""},
		{Port: 8443, Container: "other-pmm", EnvID: "other"},
		{Port: 3306, Container: "mysql", EnvID: ""},
	}, nil, 27017, 27018)

	scan := newDockerHostScan("mine")
	for _, port := range []int{27017, 27018} {
		if holder := scan.busyBy(port); holder != "" {
			t.Fatalf("port %d belongs to this environment, got busy by %q", port, holder)
		}
	}
	if holder := scan.busyBy(8443); holder != "container other-pmm" {
		t.Fatalf("port 8443 holder = %q", holder)
	}
	if holder := scan.busyBy(3306); holder != "container mysql" {
		t.Fatalf("port 3306 holder = %q", holder)
	}

	if !newDockerHostScan("").busy(27017) {
		t.Fatal("without an environment ID every published port is a conflict")
	}
}

func TestDockerHostScanProbesPortsNotPublishedByContainers(t *testing.T) {
	stubDockerHostPorts(t, nil, nil, 27019)
	scan := newDockerHostScan("mine")
	if got := scan.busyBy(27019); !strings.Contains(got, "process") {
		t.Fatalf("expected a local process to hold 27019, got %q", got)
	}
	if scan.busy(27020) {
		t.Fatal("port 27020 is free")
	}
}

func TestDockerHostScanFallsBackToProbeWhenDockerFails(t *testing.T) {
	stubDockerHostPorts(t, nil, errors.New("docker not running"), 8443)
	scan := newDockerHostScan("mine")
	if !scan.busy(8443) || scan.busy(8444) {
		t.Fatal("bind probe should still report busy and free ports")
	}
}

func TestReassignBusyDockerPortsMovesOnlyBusyPorts(t *testing.T) {
	stubDockerHostPorts(t, []dockerPublishedPort{
		{Port: 27018, Container: "other-rs01-1", EnvID: "other"},
		{Port: 8333, Container: "other-seaweedfs", EnvID: "other"},
		{Port: 8888, Container: "other-seaweedfs", EnvID: "other"},
		{Port: 8080, Container: "other-ldap", EnvID: "other"},
		{Port: 9999, Container: "other-plgm", EnvID: "other"},
	}, nil)
	cfg := Config{
		EnablePlgm: true,
		Replsets: map[string]ReplsetConfig{
			"rs01": {DataNodesPerReplset: 3, ArbitersPerReplset: intPtr(0), ReplsetPort: 27017, ArbiterPort: 27017},
			"rs02": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(0), ReplsetPort: 27117, ArbiterPort: 27117},
		},
		PmmServers:       map[string]PmmServerConfig{"pmm": {PmmPort: 8443, PmmExternalPort: 8443}},
		SeaweedFSServers: map[string]SeaweedFSServerConfig{"sw": {SeaweedFSPort: 8333, SeaweedFSAdminPort: 9333}},
		LdapServers:      map[string]LdapServerConfig{"ldap": {LdapPort: 389}},
	}

	notes := reassignBusyDockerPorts(&cfg, newDockerHostScan("mine"), nil)

	if got := cfg.Replsets["rs01"]; got.ReplsetPort != 27037 || got.ArbiterBasePort != 27047 {
		t.Fatalf("rs01 should move past the busy 27018 to 27037, got %+v", got)
	}
	if got := cfg.Replsets["rs02"].ReplsetPort; got != 27117 {
		t.Fatalf("rs02 was free and must not move, got %d", got)
	}
	if got := cfg.PmmServers["pmm"].PmmExternalPort; got != 8443 {
		t.Fatalf("PMM port was free and must not move, got %d", got)
	}
	sw := cfg.SeaweedFSServers["sw"]
	if sw.SeaweedFSPort != 8334 || sw.SeaweedFSAdminPort != 9333 || sw.SeaweedFSFilerPort != 8889 {
		t.Fatalf("unexpected SeaweedFS ports %+v", sw)
	}
	if got := cfg.LdapServers["ldap"].LdapAdminPort; got != 8081 {
		t.Fatalf("LDAP admin port = %d, want 8081", got)
	}
	if cfg.PlgmExternalPort != 10000 {
		t.Fatalf("plgm port = %d, want 10000", cfg.PlgmExternalPort)
	}
	joined := strings.Join(notes, "\n")
	for _, want := range []string{"replica set rs01", "container other-rs01-1", "SeaweedFS API sw", "SeaweedFS filer sw", "LDAP admin UI ldap", "plgm Web UI"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notes missing %q:\n%s", want, joined)
		}
	}
	if len(notes) != 5 {
		t.Fatalf("expected 5 notes, got %d:\n%s", len(notes), joined)
	}
}

func TestReassignBusyDockerPortsKeepsOwnContainerPorts(t *testing.T) {
	stubDockerHostPorts(t, []dockerPublishedPort{{Port: 27017, Container: "mine-rs01-0", EnvID: "mine"}}, nil, 27017)
	cfg := Config{Replsets: map[string]ReplsetConfig{
		"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(0), ReplsetPort: 27017, ArbiterPort: 27017},
	}}
	if notes := reassignBusyDockerPorts(&cfg, newDockerHostScan("mine"), nil); len(notes) != 0 {
		t.Fatalf("own containers must not trigger reassignment, got %v", notes)
	}
	if cfg.Replsets["rs01"].ReplsetPort != 27017 {
		t.Fatal("replica set port changed")
	}
}

func TestReassignBusyDockerPortsAvoidsOtherEnvironmentsAndItself(t *testing.T) {
	stubDockerHostPorts(t, nil, nil)
	cfg := Config{
		Replsets: map[string]ReplsetConfig{
			"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(0), ReplsetPort: 27017, ArbiterPort: 27017},
		},
		PmmServers: map[string]PmmServerConfig{"pmm": {PmmPort: 8443, PmmExternalPort: 8443}},
	}
	other := map[int]struct{}{27017: {}, 8443: {}, 8444: {}}
	reassignBusyDockerPorts(&cfg, newDockerHostScan("mine"), other)
	if got := cfg.Replsets["rs01"].ReplsetPort; got != 27037 {
		t.Fatalf("replset port = %d, want 27037", got)
	}
	if got := cfg.PmmServers["pmm"].PmmExternalPort; got != 8445 {
		t.Fatalf("PMM port = %d, want 8445", got)
	}
}

func TestValidateDockerHostPorts(t *testing.T) {
	stubDockerHostPorts(t, []dockerPublishedPort{{Port: 27018, Container: "other-rs01-1", EnvID: "other"}}, nil, 389)
	cfg := Config{
		Replsets: map[string]ReplsetConfig{
			"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(0), ReplsetPort: 27017, ArbiterPort: 27017},
		},
		LdapServers: map[string]LdapServerConfig{"ldap": {LdapPort: 389, LdapAdminPort: 18080}},
	}
	err := validateDockerHostPorts(cfg, newDockerHostScan("mine"))
	if err == nil {
		t.Fatal("expected a host port conflict")
	}
	for _, want := range []string{"port 27018", "container other-rs01-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "port 389") {
		t.Fatalf("the LDAP service port is not published on a fixed host port: %v", err)
	}

	cfg.Replsets["rs01"] = ReplsetConfig{DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(0), ReplsetPort: 27117, ArbiterPort: 27117}
	if err := validateDockerHostPorts(cfg, newDockerHostScan("mine")); err != nil {
		t.Fatalf("unexpected conflict: %v", err)
	}
}

func TestAssignDockerReplsetPortsSkipsHostBusyRanges(t *testing.T) {
	stubDockerHostPorts(t, []dockerPublishedPort{{Port: 27017, Container: "mongo", EnvID: ""}}, nil)
	cfg := &Config{Replsets: map[string]ReplsetConfig{"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(0)}}}
	assignDockerReplsetPorts(cfg, newDockerHostScan("mine").busy)
	if got := cfg.Replsets["rs01"].ReplsetPort; got != 27037 {
		t.Fatalf("replset port = %d, want 27037", got)
	}
}

func TestDockerPortsReassignable(t *testing.T) {
	applied := &Config{}
	tests := []struct {
		name string
		env  *Environment
		want bool
	}{
		{"new environment", nil, true},
		{"configured", &Environment{Status: "configured"}, true},
		{"failed first deploy", &Environment{Status: "deploy_failed"}, true},
		{"destroyed", &Environment{Status: "deleted"}, true},
		{"applied", &Environment{Status: "stopped", LastAppliedConfig: applied}, false},
		{"running legacy record", &Environment{Status: "running"}, false},
	}
	for _, tt := range tests {
		if got := dockerPortsReassignable(tt.env); got != tt.want {
			t.Errorf("%s: dockerPortsReassignable = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWriteTfvarsDockerFilerAndLdapAdminPorts(t *testing.T) {
	origTerraformDir := terraformDir
	terraformDir = t.TempDir()
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		SeaweedFSServers: map[string]SeaweedFSServerConfig{"sw": {SeaweedFSFilerPort: 18888}},
		LdapServers:      map[string]LdapServerConfig{"ldap": {LdapAdminPort: 18080}},
	}
	if err := writeTfvars("docker-extra-ports", "docker", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}
	content, err := os.ReadFile(tfvarsPath("docker-extra-ports", "docker"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"seaweedfs_filer_port = 18888", "ldap_external_admin_port = 18080"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("tfvars missing %q:\n%s", want, content)
		}
	}
}
