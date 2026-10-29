package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func clusterSyncTestConfig() Config {
	return Config{
		Prefix: "test",
		Replsets: map[string]ReplsetConfig{
			"source": {DataNodesPerReplset: 2, ReplsetPort: 27017, MongoVersion: "8.0.4"},
			"target": {DataNodesPerReplset: 2, ReplsetPort: 27117, MongoVersion: "8.0.4"},
		},
		Clusters:    map[string]ClusterConfig{},
		ClusterSync: ClusterSyncConfig{Enabled: true, SourceKind: "replset", SourceName: "source", TargetKind: "replset", TargetName: "target"},
	}
}

func TestNormalizeAndValidateClusterSync(t *testing.T) {
	cfg := clusterSyncTestConfig()
	if err := normalizeAndValidateClusterSync("docker", &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ClusterSync.Version != "0.9.0" || cfg.ClusterSync.CPUs != 2 || cfg.ClusterSync.MemoryMB != 1024 {
		t.Fatalf("defaults not applied: %+v", cfg.ClusterSync)
	}
	cfg.ClusterSync.TargetName = "source"
	if err := normalizeAndValidateClusterSync("docker", &cfg); err == nil {
		t.Fatal("expected identical endpoint error")
	}
}

func TestClusterSyncRejectsMixedAndDowngrade(t *testing.T) {
	cfg := clusterSyncTestConfig()
	cfg.ClusterSync.TargetKind = "cluster"
	cfg.ClusterSync.TargetName = "target-cluster"
	cfg.Clusters["target-cluster"] = ClusterConfig{MongoVersion: "8.0.4"}
	if err := normalizeAndValidateClusterSync("aws", &cfg); err == nil {
		t.Fatal("expected mixed topology error")
	}
	cfg = clusterSyncTestConfig()
	source := cfg.Replsets["source"]
	source.MongoVersion = "8.0.4"
	cfg.Replsets["source"] = source
	target := cfg.Replsets["target"]
	target.MongoVersion = "7.0.13"
	cfg.Replsets["target"] = target
	if err := normalizeAndValidateClusterSync("aws", &cfg); err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("expected downgrade error, got %v", err)
	}
}

func TestClusterSyncURI(t *testing.T) {
	cfg := clusterSyncTestConfig()
	uri := clusterSyncURI("test", "docker", cfg, "replset", "source", pcsmSourceUser, "secret")
	for _, expected := range []string{"test-source-svr0:27017", "test-source-svr1:27018", "replicaSet=test-source", "authSource=admin"} {
		if !strings.Contains(uri, expected) {
			t.Fatalf("URI %q missing %q", uri, expected)
		}
	}
}

func TestClusterSyncChaosURIUsesEnvironmentPrefix(t *testing.T) {
	cfg := clusterSyncTestConfig()
	uri := clusterSyncURI("test", "chaos", cfg, "replset", "source", pcsmSourceUser, "secret")
	if !strings.Contains(uri, "test-source-mongodb-svr0:27017") {
		t.Fatalf("unexpected CHAOS URI: %s", uri)
	}
}

func TestClusterSyncCloudURIUsesEnvironmentPrefix(t *testing.T) {
	cfg := clusterSyncTestConfig()
	for _, platform := range []string{"gcp", "aws", "azure"} {
		uri := clusterSyncURI("test", platform, cfg, "replset", "source", pcsmSourceUser, "secret")
		if !strings.Contains(uri, "test-source-mongodb-svr0:27017") || !strings.Contains(uri, "replicaSet=source") {
			t.Fatalf("unexpected %s URI: %s", platform, uri)
		}
	}
}

func TestClusterSyncRejectsUnsupportedMongoVersionLine(t *testing.T) {
	cfg := clusterSyncTestConfig()
	source := cfg.Replsets["source"]
	source.MongoVersion = "8.3.1"
	cfg.Replsets["source"] = source
	if err := normalizeAndValidateClusterSync("docker", &cfg); err == nil || !strings.Contains(err.Error(), "not a supported") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestClusterSyncStartArgs(t *testing.T) {
	got := clusterSyncStartArgs(ClusterSyncConfig{IncludeNamespaces: []string{"db.*"}, ReplicationWorkers: 4, CloneSegmentSize: "1GiB"})
	want := []string{"start", "--include-namespaces", "db.*", "--repl-num-workers", "4", "--clone-segment-size", "1GiB"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestEnsureClusterSyncSecrets(t *testing.T) {
	old := pcsmSecretsDir
	pcsmSecretsDir = t.TempDir()
	t.Cleanup(func() { pcsmSecretsDir = old })
	cfg := clusterSyncTestConfig()
	if err := ensureClusterSyncSecrets("test", "docker", &cfg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{clusterSyncEnvPath("test"), clusterSyncSecretPath("test"), clusterSyncBootstrapPath("test"), clusterSyncCleanupPath("test")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("%s has insecure mode %o", filepath.Base(path), info.Mode().Perm())
		}
	}
	if output, err := exec.Command("sh", "-n", clusterSyncBootstrapPath("test")).CombinedOutput(); err != nil {
		t.Fatalf("invalid bootstrap script: %v: %s", err, output)
	}
	if output, err := exec.Command("sh", "-n", clusterSyncCleanupPath("test")).CombinedOutput(); err != nil {
		t.Fatalf("invalid cleanup script: %v: %s", err, output)
	}
	bootstrap, _ := os.ReadFile(clusterSyncBootstrapPath("test"))
	if !strings.Contains(string(bootstrap), "--port 27117") {
		t.Fatal("bootstrap does not use the target replica set port")
	}
	env, _ := os.ReadFile(clusterSyncEnvPath("test"))
	state, _ := os.ReadFile(clusterSyncSecretPath("test"))
	if strings.Contains(string(env), "root") || strings.Contains(string(state), "mongodb://") {
		t.Fatal("secret artifacts contain unintended admin/URI data")
	}
	for _, key := range []string{"PCSM_SOURCE_URI=", "PCSM_TARGET_URI=", "PCSM_SOURCE_PASSWORD=", "PCSM_TARGET_PASSWORD="} {
		if !strings.Contains(string(env), key) {
			t.Fatalf("PCSM environment file is missing %q", key)
		}
	}
}

func TestRedactClusterSyncOutput(t *testing.T) {
	old := pcsmSecretsDir
	pcsmSecretsDir = t.TempDir()
	t.Cleanup(func() { pcsmSecretsDir = old })
	cfg := clusterSyncTestConfig()
	if err := ensureClusterSyncSecrets("test", "docker", &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(clusterSyncSecretPath("test"))
	var secrets clusterSyncSecrets
	if err := json.Unmarshal(data, &secrets); err != nil {
		t.Fatal(err)
	}
	got := redactClusterSyncOutput("test", "failed mongodb://user:"+secrets.SourcePassword+"@host")
	if strings.Contains(got, secrets.SourcePassword) || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("secret was not redacted: %s", got)
	}
}

func TestWriteTfvarsIncludesClusterSyncWithoutURIs(t *testing.T) {
	oldTerraformDir, oldSecretsDir := terraformDir, pcsmSecretsDir
	terraformDir, pcsmSecretsDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { terraformDir, pcsmSecretsDir = oldTerraformDir, oldSecretsDir })
	cfg := clusterSyncTestConfig()
	cfg.ClusterSync.Image = "percona/percona-clustersync-mongodb:0.9.0"
	if err := writeTfvars("test", "docker", cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tfvarsPath("test", "docker"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"enable_pcsm = true", `pcsm_source_kind = "replset"`, `pcsm_source_name = "source"`, `pcsm_target_name = "target"`, `pcsm_image = "percona/percona-clustersync-mongodb:0.9.0"`, "pcsm_env_file = "} {
		if !strings.Contains(text, expected) {
			t.Fatalf("tfvars missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "PCSM_SOURCE_URI") || strings.Contains(text, "pcsm-source:") {
		t.Fatal("tfvars contains ClusterSync credentials")
	}
}

func TestWriteTfvarsUsesValidChaosClusterSyncMemory(t *testing.T) {
	oldTerraformDir, oldSecretsDir := terraformDir, pcsmSecretsDir
	terraformDir, pcsmSecretsDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { terraformDir, pcsmSecretsDir = oldTerraformDir, oldSecretsDir })
	cfg := clusterSyncTestConfig()
	if err := writeTfvars("test", "chaos", cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tfvarsPath("test", "chaos"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "pcsm_memory_gb = 4") {
		t.Fatalf("expected valid CHAOS PCSM memory:\n%s", data)
	}
}
