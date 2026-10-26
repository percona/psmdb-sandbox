package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func intPtr(v int) *int {
	return &v
}

func TestTLSJSONUsesBreakingUseTLSName(t *testing.T) {
	data, err := json.Marshal(Config{
		UseTLS: true,
		Clusters: map[string]ClusterConfig{
			"cl01": {UseTLS: boolPtr(true)},
		},
		Replsets: map[string]ReplsetConfig{
			"rs01": {UseTLS: boolPtr(false)},
		},
	})
	if err != nil {
		t.Fatalf("marshal TLS config: %v", err)
	}
	if strings.Contains(string(data), "enable_tls") || !strings.Contains(string(data), `"use_tls":true`) {
		t.Fatalf("unexpected TLS JSON: %s", data)
	}

	var cfg Config
	if err := json.Unmarshal([]byte(`{"enable_tls":true,"clusters":{"cl01":{"enable_tls":true}}}`), &cfg); err != nil {
		t.Fatalf("unmarshal old TLS names: %v", err)
	}
	if cfg.UseTLS || cfg.Clusters["cl01"].UseTLS != nil {
		t.Fatalf("old enable_tls names unexpectedly populated config: %#v", cfg)
	}
}

func TestWriteTfvarsCloudTLS(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{Prefix: "tls", CAPlacement: "dedicated", Clusters: map[string]ClusterConfig{
		"secure": {UseTLS: boolPtr(true)},
		"plain":  {UseTLS: boolPtr(false)},
	}}
	if err := writeTfvars("tls", "aws", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}
	content, err := os.ReadFile(tfvarsPath("tls", "aws"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	for _, want := range []string{"enable_ca = true", `ca_placement = "dedicated"`, `"secure" = {`, `use_tls = true`, `"plain" = {`, `use_tls = false`} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, content)
		}
	}
}

func TestTopologyAnsibleVarsOverrideTLSPerInventory(t *testing.T) {
	cfg := Config{
		PmmImage:    " pmm:test ",
		AnsibleVars: map[string]string{"use_tls": "wrong", "custom": "value"},
		Clusters:    map[string]ClusterConfig{"secure": {UseTLS: boolPtr(true)}},
		Replsets:    map[string]ReplsetConfig{"plain": {UseTLS: boolPtr(false)}},
	}

	secure := topologyAnsibleVars(cfg, "chaos", "secure", map[string]string{"step": "cert"})
	plain := topologyAnsibleVars(cfg, "chaos", "plain", nil)
	if value, ok := secure["use_tls"].(bool); !ok || !value {
		t.Fatalf("secure topology use_tls = %#v, want boolean true", secure["use_tls"])
	}
	if value, ok := plain["use_tls"].(bool); !ok || value {
		t.Fatalf("plain topology use_tls = %#v, want boolean false", plain["use_tls"])
	}
	if secure["pmm_image"] != "pmm:test" || secure["custom"] != "value" || secure["step"] != "cert" {
		t.Fatalf("unexpected runtime variables: %#v", secure)
	}
}

func TestNormalizeAndValidateUseTLSConfig(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		cfg      Config
		wantErr  string
	}{
		{name: "dedicated", platform: "aws", cfg: Config{Clusters: map[string]ClusterConfig{"cl01": {UseTLS: boolPtr(true)}}}},
		{name: "TLS without CA", platform: "aws", cfg: Config{EnableCA: boolPtr(false), Clusters: map[string]ClusterConfig{"cl01": {UseTLS: boolPtr(true)}}}, wantErr: "provision a certificate authority"},
		{name: "global default", platform: "aws", cfg: Config{UseTLS: true, Clusters: map[string]ClusterConfig{"cl01": {}}}},
		{name: "pmm", platform: "gcp", cfg: Config{CAPlacement: "pmm", EnablePmm: boolPtr(true), Replsets: map[string]ReplsetConfig{"rs01": {UseTLS: boolPtr(true)}}}},
		{name: "pmm disabled", platform: "azure", cfg: Config{CAPlacement: "pmm", EnablePmm: boolPtr(false), Replsets: map[string]ReplsetConfig{"rs01": {UseTLS: boolPtr(true)}}}, wantErr: "PMM server"},
		{name: "chaos", platform: "chaos", cfg: Config{Replsets: map[string]ReplsetConfig{"rs01": {UseTLS: boolPtr(true)}}}},
		{name: "invalid chaos CA CPU", platform: "chaos", cfg: Config{CACpuCores: 1, Replsets: map[string]ReplsetConfig{"rs01": {UseTLS: boolPtr(true)}}}, wantErr: "vCPU"},
		{name: "unsupported platform", platform: "docker", cfg: Config{Clusters: map[string]ClusterConfig{"cl01": {UseTLS: boolPtr(true)}}}, wantErr: "supported only"},
		{name: "plgm", platform: "aws", cfg: Config{EnablePlgm: true, Clusters: map[string]ClusterConfig{"cl01": {UseTLS: boolPtr(true)}}}},
		{name: "mongot", platform: "aws", cfg: Config{Clusters: map[string]ClusterConfig{"cl01": {UseTLS: boolPtr(true), EnableMongot: boolPtr(true)}}}, wantErr: "mongot"},
		{name: "raw override", platform: "aws", cfg: Config{AnsibleVars: map[string]string{"use_tls": "true"}}, wantErr: "cannot be overridden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalizeAndValidateUseTLSConfig(tt.platform, &tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.cfg.CAPlacement == "" {
					t.Fatal("CA placement was not normalized")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestWriteTfvarsProvisionsCAWithoutTLS(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{EnableCA: boolPtr(true), CAPlacement: "dedicated", Clusters: map[string]ClusterConfig{"plain": {UseTLS: boolPtr(false)}}}
	if err := writeTfvars("ca-only", "aws", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}
	content, err := os.ReadFile(tfvarsPath("ca-only", "aws"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	if !strings.Contains(string(content), "enable_ca = true") {
		t.Fatalf("expected shared CA infrastructure to be enabled:\n%s", content)
	}
}

func TestWriteTfvarsDockerPmmExternalAndEmptyServiceMaps(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		Clusters: map[string]ClusterConfig{
			"cl01": {EnvTag: "test"},
		},
		Replsets: map[string]ReplsetConfig{
			"rs01": {EnvTag: "test", DataNodesPerReplset: 1, ArbitersPerReplset: intPtr(0), ReplsetPort: 27017, ArbiterPort: 27017, ArbiterBasePort: 27027},
		},
		PmmServers: map[string]PmmServerConfig{
			"pmm-server": {EnvTag: "test", PmmExternalPort: 9443},
		},
	}

	if err := writeTfvars("docker-ports", "docker", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("docker-ports", "docker"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	for _, want := range []string{
		"pmm_port = 8443",
		"pmm_external_port = 9443",
		"arbiter_base_port = 27027",
		"seaweedfs_servers = {}",
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
}

func TestWriteTfvarsDockerPerComponentImageNamespaces(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		Clusters: map[string]ClusterConfig{
			"cl01": {
				EnvTag:         "test",
				PsmdbImage:     "perconalab/percona-server-mongodb:latest",
				PbmImage:       "percona/percona-backup-mongodb:latest",
				PmmClientImage: "perconalab/pmm-client:latest",
				EnablePbm:      boolPtr(true),
				EnablePmm:      boolPtr(true),
			},
		},
		PmmServers: map[string]PmmServerConfig{
			"pmm-server": {PmmServerImage: "perconalab/pmm-server:latest"},
		},
	}

	if err := writeTfvars("docker-image-namespace", "docker", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("docker-image-namespace", "docker"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)
	for _, want := range []string{
		`psmdb_image = "perconalab/percona-server-mongodb:latest"`,
		`pbm_image = "percona/percona-backup-mongodb:latest"`,
		`pmm_client_image = "perconalab/pmm-client:latest"`,
		`pmm_server_image = "perconalab/pmm-server:latest"`,
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
}

func TestWriteTfvarsDockerEmptyPmmServersMap(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		Clusters: map[string]ClusterConfig{
			"cl01": {EnvTag: "test"},
		},
	}

	if err := writeTfvars("docker-empty-services", "docker", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("docker-empty-services", "docker"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	for _, want := range []string{
		"pmm_servers = {}",
		"seaweedfs_servers = {}",
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
}

func TestWriteTfvarsCloudPmmClientVersion(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		Clusters: map[string]ClusterConfig{
			"cl01": {EnvTag: "test", PmmClientVersion: "3.4.0"},
		},
	}

	if err := writeTfvars("cloud-pmm-client", "gcp", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("cloud-pmm-client", "gcp"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	if !strings.Contains(tfvars, "pmm_client_version = \"3.4.0\"") {
		t.Fatalf("expected pmm_client_version in tfvars:\n%s", tfvars)
	}
}

func TestWriteTfvarsCloudPackageOverrides(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		Clusters: map[string]ClusterConfig{
			"cl01": {
				EnvTag:              "test",
				MongoDBDistribution: "community",
				MongoRelease:        "8.0",
				MongoVersion:        "8.0.13",
				MongoRepo:           "testing",
				MongotRepo:          "testing",
				PbmVersion:          "2.7.0",
				PbmRepo:             "experimental",
				PmmClientVersion:    "3.3.0",
				PmmClientRepo:       "testing",
			},
		},
		Replsets: map[string]ReplsetConfig{
			"rs01": {
				EnvTag:              "test",
				MongoDBDistribution: "enterprise",
				MongoRelease:        "7.0",
				MongoRepo:           "experimental",
				MongotRepo:          "experimental",
				PbmRepo:             "testing",
				PmmClientRepo:       "experimental",
			},
		},
	}

	if err := writeTfvars("cloud-package-overrides", "gcp", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("cloud-package-overrides", "gcp"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	for _, want := range []string{
		`"cl01" = {`,
		`enable_pmm = false`,
		`enable_pbm = false`,
		`mongodb_distribution = "community"`,
		`mongo_release = "8.0"`,
		`mongo_version = "8.0.13"`,
		`mongo_repo = "testing"`,
		`mongot_repo = "testing"`,
		`pbm_version = "2.7.0"`,
		`pbm_repo = "experimental"`,
		`pmm_client_version = "3.3.0"`,
		`pmm_client_repo = "testing"`,
		`"rs01" = {`,
		`enable_pmm = false`,
		`enable_pbm = false`,
		`mongodb_distribution = "enterprise"`,
		`mongo_release = "7.0"`,
		`mongo_repo = "experimental"`,
		`mongot_repo = "experimental"`,
		`pbm_repo = "testing"`,
		`pmm_client_repo = "experimental"`,
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}

	for _, unwanted := range []string{
		`mongo_version = "8.0.4"`,
		`pmm_client_version = "3.4.0"`,
	} {
		if strings.Contains(tfvars, unwanted) {
			t.Fatalf("did not expect top-level %q in tfvars:\n%s", unwanted, tfvars)
		}
	}
}

func TestWriteTfvarsCloudAgentToggles(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		Clusters: map[string]ClusterConfig{
			"cl01": {
				EnvTag:    "test",
				EnablePmm: boolPtr(false),
				EnablePbm: boolPtr(true),
			},
		},
		Replsets: map[string]ReplsetConfig{
			"rs01": {
				EnvTag:    "test",
				EnablePmm: boolPtr(true),
				EnablePbm: boolPtr(false),
			},
		},
	}

	if err := writeTfvars("cloud-agent-toggles", "gcp", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("cloud-agent-toggles", "gcp"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	for _, want := range []string{
		`"cl01" = {`,
		`enable_pmm = false`,
		`enable_pbm = true`,
		`"rs01" = {`,
		`enable_pmm = true`,
		`enable_pbm = false`,
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
}

func TestWriteTfvarsChaosOsImageAndNeverDelete(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		DeleteAfterDays: 0,
		Clusters: map[string]ClusterConfig{
			"cl01": {EnvTag: "test", OsImage: "Rocky Linux 9"},
		},
		Replsets: map[string]ReplsetConfig{
			"rs01": {EnvTag: "test", OsImage: "Ubuntu 22.04"},
		},
	}

	if err := writeTfvars("chaos-os-never", "chaos", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("chaos-os-never", "chaos"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	for _, want := range []string{
		`os_image = "Rocky Linux 9"`,
		`os_image = "Ubuntu 22.04"`,
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
	if strings.Contains(tfvars, "delete_after_days") {
		t.Fatalf("did not expect delete_after_days when Never is selected:\n%s", tfvars)
	}
}

func TestWriteTfvarsIncludesEnablePlgmForAllPlatforms(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	platforms := []string{"docker", "gcp", "aws", "azure", "chaos"}
	for _, platform := range platforms {
		cfg := Config{
			EnablePlgm: true,
			Clusters: map[string]ClusterConfig{
				"cl01": {EnvTag: "test"},
			},
		}

		if err := writeTfvars("enable-plgm-"+platform, platform, cfg); err != nil {
			t.Fatalf("platform %s: writeTfvars failed: %v", platform, err)
		}

		content, err := os.ReadFile(tfvarsPath("enable-plgm-"+platform, platform))
		if err != nil {
			t.Fatalf("platform %s: read tfvars failed: %v", platform, err)
		}
		if !strings.Contains(string(content), "enable_plgm = true") {
			t.Fatalf("platform %s: expected enable_plgm in tfvars:\n%s", platform, string(content))
		}
	}
}

func TestWriteTfvarsDockerPlgmSettings(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		EnablePlgm:          true,
		PlgmImage:           "custom/plgm:dev",
		PlgmOsImage:         "alpine:3.21",
		PlgmContainerSuffix: "bench",
		PlgmExternalPort:    19999,
		Clusters: map[string]ClusterConfig{
			"cl01": {EnvTag: "test"},
		},
	}

	if err := writeTfvars("docker-plgm-settings", "docker", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}

	content, err := os.ReadFile(tfvarsPath("docker-plgm-settings", "docker"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)

	for _, want := range []string{
		"enable_plgm = true",
		`plgm_image = "custom/plgm:dev"`,
		`plgm_os_image = "alpine:3.21"`,
		`plgm_container_suffix = "bench"`,
		"plgm_external_port = 19999",
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
}

func TestWriteTfvarsCloudLDAP(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		LdapServers: map[string]LdapServerConfig{
			"directory": {LdapDomain: "example.org", LdapAdminPassword: "secret"},
		},
		Clusters: map[string]ClusterConfig{
			"cl01": {EnvTag: "test", LdapServer: "directory"},
		},
		Replsets: map[string]ReplsetConfig{
			"rs01": {EnvTag: "test", LdapServer: "directory"},
		},
	}

	if err := writeTfvars("cloud-ldap", "gcp", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}
	content, err := os.ReadFile(tfvarsPath("cloud-ldap", "gcp"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	tfvars := string(content)
	for _, want := range []string{
		`ldap_servers = {`,
		`"directory" = {`,
		`domain = "example.org"`,
		`admin_password = "secret"`,
		`ldap_server = "directory"`,
	} {
		if !strings.Contains(tfvars, want) {
			t.Fatalf("expected %q in tfvars:\n%s", want, tfvars)
		}
	}
}

func TestWriteTfvarsCloudLDAPDefaultsDomainToExampleCom(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{LdapServers: map[string]LdapServerConfig{"directory": {}}}
	if err := writeTfvars("cloud-ldap-default-domain", "gcp", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}
	content, err := os.ReadFile(tfvarsPath("cloud-ldap-default-domain", "gcp"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	if !strings.Contains(string(content), `domain = "example.com"`) {
		t.Fatalf("expected default LDAP domain in tfvars:\n%s", content)
	}
}

func TestAssignDockerReplsetPortsAvoidsServicePorts(t *testing.T) {
	cfg := &Config{
		Replsets: map[string]ReplsetConfig{
			"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(1)},
		},
		PmmServers: map[string]PmmServerConfig{
			"pmm-server": {PmmExternalPort: 27017},
		},
	}

	assignDockerReplsetPorts(cfg)

	rs := cfg.Replsets["rs01"]
	if rs.ReplsetPort != 27037 {
		t.Fatalf("expected replset port 27037 to avoid PMM port 27017, got %d", rs.ReplsetPort)
	}
	if rs.ArbiterBasePort != 27047 {
		t.Fatalf("expected arbiter base port 27047, got %d", rs.ArbiterBasePort)
	}
}

func TestAssignDockerReplsetPortsDerivesLegacyArbiterBasePort(t *testing.T) {
	cfg := &Config{
		Replsets: map[string]ReplsetConfig{
			"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(1), ReplsetPort: 27017, ArbiterPort: 27017},
		},
	}

	assignDockerReplsetPorts(cfg)

	rs := cfg.Replsets["rs01"]
	if rs.ArbiterBasePort != 27019 {
		t.Fatalf("expected legacy arbiter base port 27019, got %d", rs.ArbiterBasePort)
	}
}

func TestValidateDockerPortConflictsDetectsOverlap(t *testing.T) {
	cfg := Config{
		Replsets: map[string]ReplsetConfig{
			"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(1), ReplsetPort: 27017, ArbiterPort: 27017},
		},
		SeaweedFSServers: map[string]SeaweedFSServerConfig{
			"seaweedfs-server": {SeaweedFSPort: 27018},
		},
	}

	err := validateDockerPortConflicts("", cfg, nil)
	if err == nil {
		t.Fatal("expected docker port conflict error")
	}
	if !strings.Contains(err.Error(), "port 27018") {
		t.Fatalf("expected conflict error to mention port 27018, got %v", err)
	}
}

func TestDockerConfigureDefaultsAvoidRunningEnvironmentServicePorts(t *testing.T) {
	state := map[string]*Environment{
		"env-running": {
			Platform: "docker",
			Status:   "running",
			Config: Config{
				PmmServers: map[string]PmmServerConfig{
					"pmm-server": {PmmPort: 8443, PmmExternalPort: 8443},
				},
				SeaweedFSServers: map[string]SeaweedFSServerConfig{
					"seaweedfs": {SeaweedFSPort: 8333, SeaweedFSAdminPort: 9333},
				},
			},
		},
	}

	occupied := dockerOccupiedServicePorts(state, "")
	pmmPort := nextFreeDockerPort(8443, occupied)
	seaweedFSPort, seaweedFSAdminPort := nextFreeSeaweedFSPorts(8333, occupied)

	if pmmPort != 8444 {
		t.Fatalf("expected PMM external port 8444, got %d", pmmPort)
	}
	if seaweedFSPort != 8334 || seaweedFSAdminPort != 9334 {
		t.Fatalf("expected SeaweedFS ports 8334/9334, got %d/%d", seaweedFSPort, seaweedFSAdminPort)
	}
}

func TestSaveEnvironmentHandlerDetectsRunningDockerEnvironmentPortConflicts(t *testing.T) {
	dir := t.TempDir()
	origStateFile := stateFile
	origTerraformDir := terraformDir
	stateFile = dir + "/environments.json"
	terraformDir = dir
	t.Cleanup(func() {
		stateFile = origStateFile
		terraformDir = origTerraformDir
	})

	existing := map[string]*Environment{
		"envrunning": {
			Platform: "docker",
			Status:   "running",
			Config: Config{
				Prefix: "envrunning",
				Replsets: map[string]ReplsetConfig{
					"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(1), ReplsetPort: 27017, ArbiterPort: 27017},
				},
				PmmServers: map[string]PmmServerConfig{
					"pmm-server": {PmmPort: 8443, PmmExternalPort: 8443},
				},
				SeaweedFSServers: map[string]SeaweedFSServerConfig{
					"seaweedfs": {SeaweedFSPort: 8333, SeaweedFSAdminPort: 9333},
				},
			},
		},
	}
	if err := saveState(existing); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	payload := map[string]interface{}{
		"env_id":   "envnew",
		"platform": "docker",
		"config": Config{
			Prefix: "envnew",
			Replsets: map[string]ReplsetConfig{
				"rs01": {DataNodesPerReplset: 2, ArbitersPerReplset: intPtr(1), ReplsetPort: 27017, ArbiterPort: 27017},
			},
			PmmServers: map[string]PmmServerConfig{
				"pmm-server": {PmmPort: 8443, PmmExternalPort: 8443},
			},
			SeaweedFSServers: map[string]SeaweedFSServerConfig{
				"seaweedfs": {SeaweedFSPort: 8333, SeaweedFSAdminPort: 9333},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !strings.Contains(resp.Error, "port 27017") {
		t.Fatalf("expected conflict response to mention port 27017, got %s", resp.Error)
	}
	if !strings.Contains(resp.Error, "docker port conflicts detected:\n-") {
		t.Fatalf("expected multiline conflict response, got %s", resp.Error)
	}
	if !strings.Contains(resp.Error, "this environment replica set") {
		t.Fatalf("expected conflict response to mention this environment, got %s", resp.Error)
	}
	if !strings.Contains(resp.Error, "environment envrunning replica set") {
		t.Fatalf("expected conflict response to mention running environment, got %s", resp.Error)
	}

	state, err := loadState()
	if err != nil {
		t.Fatalf("loadState failed: %v", err)
	}
	if _, exists := state["envnew"]; exists {
		t.Fatal("conflicting environment should not be saved")
	}
}

func TestSaveEnvironmentHandlerGeneratedNameStartsWithLetter(t *testing.T) {
	dir := t.TempDir()
	origStateFile := stateFile
	origTerraformDir := terraformDir
	stateFile = dir + "/environments.json"
	terraformDir = dir
	t.Cleanup(func() {
		stateFile = origStateFile
		terraformDir = origTerraformDir
	})

	req := httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(`{"platform":"docker","config":{}}`))
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		EnvID string `json:"env_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if !gcpEnvNameRe.MatchString(resp.EnvID) {
		t.Fatalf("generated environment name must be valid for GCP, got %q", resp.EnvID)
	}
}

func TestSaveEnvironmentHandlerRejectsDigitPrefixedGCPName(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(`{"env_id":"41bec196","platform":"gcp","config":{}}`))
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must start with a lowercase letter") {
		t.Fatalf("expected GCP naming error, got %s", rec.Body.String())
	}
}

func TestGCPEnvironmentNameLengthLimit(t *testing.T) {
	if !gcpEnvNameRe.MatchString("abcdefghijklmn") {
		t.Fatal("expected 14-character GCP environment name to be valid")
	}
	if gcpEnvNameRe.MatchString("abcdefghijklmno") {
		t.Fatal("expected 15-character GCP environment name to be invalid")
	}
}

func TestSaveEnvironmentHandlerUsesGlobalGCPProject(t *testing.T) {
	dir := t.TempDir()
	origStateFile := stateFile
	origSettingsFile := settingsFile
	origTerraformDir := terraformDir
	stateFile = dir + "/environments.json"
	settingsFile = dir + "/settings.json"
	terraformDir = dir
	t.Cleanup(func() {
		stateFile = origStateFile
		settingsFile = origSettingsFile
		terraformDir = origTerraformDir
	})

	publicKey := dir + "/id_test.pub"
	if err := os.WriteFile(publicKey, []byte("ssh-ed25519 test"), 0600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	if err := saveAppSettings(AppSettings{
		GCPProjectID:     "global-project",
		GCPSSHUser:       "test-user",
		SSHPublicKeyPath: publicKey,
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(`{"env_id":"gcptest","platform":"gcp","config":{"project_id":"ignored-project"}}`))
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	state, err := loadState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got := state["gcptest"].Config.ProjectID; got != "global-project" {
		t.Fatalf("expected global GCP project, got %q", got)
	}
	tfvars, err := os.ReadFile(tfvarsPath("gcptest", "gcp"))
	if err != nil {
		t.Fatalf("read tfvars: %v", err)
	}
	if !strings.Contains(string(tfvars), `project_id = "global-project"`) {
		t.Fatalf("expected global project in tfvars, got:\n%s", tfvars)
	}
}

func TestSaveEnvironmentHandlerRequiresGlobalGCPProject(t *testing.T) {
	dir := t.TempDir()
	origSettingsFile := settingsFile
	settingsFile = dir + "/settings.json"
	t.Cleanup(func() { settingsFile = origSettingsFile })

	if err := saveAppSettings(AppSettings{}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(`{"env_id":"gcptest","platform":"gcp","config":{}}`))
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GCP project ID is required in Settings") {
		t.Fatalf("expected missing project settings error, got %s", rec.Body.String())
	}
}

func TestWriteTfvarsCloudPlgmLocalPort(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	for _, platform := range []string{"gcp", "aws", "azure", "chaos"} {
		cfg := Config{
			EnablePlgm:    true,
			PlgmLocalPort: "39999",
			Clusters:      map[string]ClusterConfig{"cl01": {EnvTag: "test"}},
		}
		if err := writeTfvars("plgm-port-"+platform, platform, cfg); err != nil {
			t.Fatalf("platform %s: writeTfvars failed: %v", platform, err)
		}
		content, err := os.ReadFile(tfvarsPath("plgm-port-"+platform, platform))
		if err != nil {
			t.Fatalf("platform %s: read tfvars failed: %v", platform, err)
		}
		if !strings.Contains(string(content), `plgm_local_port = "39999"`) {
			t.Fatalf("platform %s: expected plgm_local_port in tfvars:\n%s", platform, string(content))
		}
	}
}
