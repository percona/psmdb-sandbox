package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerTLSConfiguration(t *testing.T) {
	cfg := Config{
		EnableCA: boolPtr(true), CAPlacement: "dedicated",
		Clusters: map[string]ClusterConfig{"secure": {UseTLS: boolPtr(true)}},
		Replsets: map[string]ReplsetConfig{"plain": {UseTLS: boolPtr(false)}},
	}
	page := renderConfigure(t, ConfigureData{Platform: "docker", Config: cfg, DefaultImages: defaultImages,
		SortedClusters: sortedClusters(cfg.Clusters), SortedReplsets: sortedReplsets(cfg.Replsets)})
	for _, want := range []string{`name="cluster_use_tls[]"`, `name="replset_use_tls[]"`, `id="enable_ca"`, "Dedicated offline signing container", "plaintext clients remain allowed"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing Docker TLS control: %s", want)
		}
	}
	if strings.Contains(page, `<option value="pmm"`) {
		t.Error("Docker must not offer PMM CA placement")
	}
	orig := terraformDir
	terraformDir = t.TempDir()
	t.Cleanup(func() { terraformDir = orig })
	if err := writeTfvars("tls", "docker", cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(terraformDir, "docker", "tls.tfvars"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"enable_ca = true", `ca_placement = "dedicated"`, "use_tls = true", "use_tls = false"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("tfvars missing %q", want)
		}
	}
}

func TestDockerTLSConnections(t *testing.T) {
	cfg := Config{Prefix: "test", Replsets: map[string]ReplsetConfig{
		"secure": {UseTLS: boolPtr(true), ReplsetPort: 29017},
		"plain":  {UseTLS: boolPtr(false), ReplsetPort: 30017},
	}}
	for _, conn := range buildDockerMongoConns("test", &Environment{Config: cfg}) {
		u, err := url.Parse(conn.ConnString)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if conn.Name == "plain" {
			if q.Get("tls") != "" || conn.Instructions != "" || len(conn.Steps) != 0 {
				t.Error("plaintext connection changed")
			}
			continue
		}
		if q.Get("tls") != "true" || q.Get("tlsCAFile") != "test-secure-test-ca.pem" || q.Get("directConnection") != "true" {
			t.Errorf("invalid TLS connection: %s", conn.ConnString)
		}
		if strings.Contains(conn.ConnString, "Invalid") || !strings.Contains(conn.Instructions, "may not be primary") {
			t.Errorf("missing verified connection guidance: %+v", conn)
		}
		if len(conn.Steps) != 3 {
			t.Fatalf("got %d connection steps, want CA export, host connection, and Docker connection", len(conn.Steps))
		}
		for i, prefix := range []string{"docker exec ", "mongosh ", "docker exec -it "} {
			wantScope := "host"
			if i == 2 {
				wantScope = "docker"
			}
			if conn.Steps[i].Scope != wantScope {
				t.Errorf("step %d scope = %q, want %q", i, conn.Steps[i].Scope, wantScope)
			}
			if conn.Steps[i].Label == "" || !strings.HasPrefix(conn.Steps[i].Command, prefix) || strings.Contains(conn.Steps[i].Command, "\n") {
				t.Errorf("step %d must have a label and a standalone copyable command: %+v", i, conn.Steps[i])
			}
		}
		if conn.Steps[1].Command != "mongosh "+shellQuote(conn.ConnString) {
			t.Error("host command must use the displayed TLS connection string")
		}
		if !strings.HasSuffix(conn.Steps[2].Command, " -u "+shellQuote(conn.ConnUser)+" -p "+shellQuote(conn.ConnPass)) {
			t.Error("Docker command must include the same credentials as the host connection")
		}
	}
	secure, _ := url.Parse(clusterSyncURI("test", "docker", cfg, "replset", "secure", "u", "p"))
	plain, _ := url.Parse(clusterSyncURI("test", "docker", cfg, "replset", "plain", "u", "p"))
	if secure.Query().Get("tlsCAFile") != "/etc/mongodb-tls/test-ca.pem" || plain.Query().Get("tls") != "" {
		t.Fatal("ClusterSync must configure TLS independently for each topology")
	}
}

func TestDockerFeatureCheckboxOrder(t *testing.T) {
	cfg := Config{
		Clusters: map[string]ClusterConfig{"cl01": {}},
		Replsets: map[string]ReplsetConfig{"rs01": {}},
	}
	page := renderConfigure(t, ConfigureData{Platform: "docker", Config: cfg, DefaultImages: defaultImages,
		SortedClusters: sortedClusters(cfg.Clusters), SortedReplsets: sortedReplsets(cfg.Replsets)})
	for _, prefix := range []string{"cluster", "replset", "${type}"} {
		previous := -1
		for _, feature := range []string{"enable_pmm", "enable_pbm", "enable_mongot", "use_tls", "bind_to_localhost"} {
			field := `name="` + prefix + `_` + feature + `[]"`
			index := strings.Index(page, field)
			if index < 0 || index <= previous {
				t.Fatalf("%s checkbox order: %s is missing or out of order", prefix, feature)
			}
			previous = index
		}
	}
}

func TestDockerClusterSyncBootstrapUsesTopologyTLS(t *testing.T) {
	old := pcsmSecretsDir
	pcsmSecretsDir = t.TempDir()
	t.Cleanup(func() { pcsmSecretsDir = old })
	cfg := clusterSyncTestConfig()
	source := cfg.Replsets["source"]
	source.UseTLS = boolPtr(true)
	cfg.Replsets["source"] = source
	if err := ensureClusterSyncSecrets("test", "docker", &cfg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{clusterSyncBootstrapPath("test"), clusterSyncCleanupPath("test")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, " mongosh ") {
				continue
			}
			usesTLS := strings.Contains(line, "--tlsCAFile /etc/mongodb-tls/test-ca.pem")
			if usesTLS != strings.Contains(line, "test-source-svr0") {
				t.Errorf("bootstrap/cleanup must enable TLS only for the TLS topology: %s", line)
			}
		}
	}
}
