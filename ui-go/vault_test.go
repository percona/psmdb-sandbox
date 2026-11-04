package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultEnvironmentWideValidation(t *testing.T) {
	cfg := Config{VaultEncryption: true, MongoDBDistribution: "psmdb", Clusters: map[string]ClusterConfig{
		"cl01": {MongoDBDistribution: "psmdb"},
		"cl02": {MongoDBDistribution: "psmdb"},
	}, Replsets: map[string]ReplsetConfig{
		"rs01": {MongoDBDistribution: "psmdb"},
	}}
	if err := validateVaultConfig("aws", cfg); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(vaultTopologies(cfg), ","); got != "cluster-cl01,cluster-cl02,replset-rs01" {
		t.Fatalf("unexpected Vault consumers: %s", got)
	}
	for _, distribution := range []string{"community", "enterprise"} {
		cfg.Clusters["cl02"] = ClusterConfig{MongoDBDistribution: distribution}
		if err := validateVaultConfig("aws", cfg); err == nil {
			t.Fatalf("accepted environment Vault encryption with a %s cluster", distribution)
		}
	}
	cfg.Clusters["cl02"] = ClusterConfig{PsmdbImage: "mongodb/mongodb-enterprise-server:8.0"}
	if err := validateVaultConfig("docker", cfg); err == nil {
		t.Fatal("accepted a non-PSMDB Docker image in an encrypted environment")
	}
	cfg = Config{VaultEncryption: true}
	if err := validateVaultConfig("docker", cfg); err == nil {
		t.Fatal("accepted encryption with no MongoDB topologies")
	}
	cfg = Config{Clusters: map[string]ClusterConfig{"cl01": {}}}
	if len(vaultTopologies(cfg)) != 0 {
		t.Fatal("generated Vault consumers when environment encryption is disabled")
	}
	if err := validateVaultConfig("docker", cfg); err != nil {
		t.Fatal(err)
	}
}

func TestVaultModeChangesAndExpansion(t *testing.T) {
	prior := Config{VaultEncryption: true, Replsets: map[string]ReplsetConfig{"rs01": {DataNodesPerReplset: 2}}}
	next := *cloneConfig(prior)
	next.Replsets["rs01"] = ReplsetConfig{DataNodesPerReplset: 3}
	next.Replsets["rs02"] = ReplsetConfig{DataNodesPerReplset: 2}
	plan, unsupported := analyseTopologyChange(prior, next)
	if len(unsupported) != 0 || plan.AddedReplsetNodes["rs01"] != 1 || len(plan.NewReplsets) != 1 {
		t.Fatalf("encrypted expansion rejected: %+v %v", plan, unsupported)
	}
	next.VaultEncryption = false
	_, unsupported = analyseTopologyChange(prior, next)
	if len(unsupported) != 1 || !strings.Contains(unsupported[0], "encryption") {
		t.Fatalf("encryption change accepted: %v", unsupported)
	}
}

func TestVaultTfvarsIsolationAndFlags(t *testing.T) {
	oldTerraformDir, oldDataDir := terraformDir, dataDir
	terraformDir, dataDir = t.TempDir(), t.TempDir()
	t.Cleanup(func() { terraformDir, dataDir = oldTerraformDir, oldDataDir })
	cfg := Config{Prefix: "envone", MongoDBDistribution: "psmdb",
		VaultEncryption: true,
		Clusters:        map[string]ClusterConfig{"cl01": {}},
		Replsets:        map[string]ReplsetConfig{"rs01": {}}}
	for _, platform := range []string{"aws", "gcp", "azure", "chaos", "docker"} {
		if err := writeTfvars("envone", platform, cfg); err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(tfvarsPath("envone", platform))
		if err != nil {
			t.Fatal(err)
		}
		text := string(contents)
		for _, expected := range []string{"vault_encryption = true", "vault_controller_dir ="} {
			if !strings.Contains(text, expected) {
				t.Errorf("%s missing %s", platform, expected)
			}
		}
		if strings.Contains(text, "    vault_encryption =") {
			t.Errorf("%s still writes a per-topology Vault switch", platform)
		}
		if strings.Contains(text, "root_token") || strings.Contains(text, "unseal_keys") {
			t.Fatal("bootstrap secrets leaked into tfvars")
		}
	}
	if vaultControllerDir("envone") == vaultControllerDir("envtwo") {
		t.Fatal("environments share recovery directory")
	}
	if !strings.HasSuffix(vaultControllerDir("envone"), filepath.Join("vault", "envone")) {
		t.Fatal("incorrect recovery path")
	}
}

func TestVaultDockerRestartOrdering(t *testing.T) {
	ids := vaultDockerConsumerIDs("envone", true)
	if !strings.Contains(ids, "'label=psmdb-sandbox.environment=envone'") || !strings.Contains(ids, "'name=^/envone-'") || strings.Contains(ids, "--quiet") {
		t.Fatalf("consumer selection must use exact labels with an anchored legacy fallback and custom format: %s", ids)
	}
	command := vaultDockerRestartShell("envone", Config{Prefix: "envone", VaultEncryption: true, Replsets: map[string]ReplsetConfig{"rs01": {}}})
	stop, restartVault, unseal, restartConsumers := strings.Index(command, "docker stop"), strings.Index(command, "docker restart 'envone-vault'"), strings.Index(command, " bootstrap"), strings.LastIndex(command, "docker restart")
	if stop < 0 || !(stop < restartVault && restartVault < unseal && unseal < restartConsumers) {
		t.Fatalf("incorrect restart order: %s", command)
	}
	if !strings.Contains(command, `{{if ne .Names "envone-vault"}}`) {
		t.Fatal("Vault was not excluded from consumer restart")
	}
}
