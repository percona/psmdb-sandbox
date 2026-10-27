package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func useTempState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origStateFile := stateFile
	origTerraformDir := terraformDir
	stateFile = dir + "/environments.json"
	terraformDir = dir
	t.Cleanup(func() {
		stateFile = origStateFile
		terraformDir = origTerraformDir
	})
}

func TestMarkEnvironmentDestroyedKeepsDefinition(t *testing.T) {
	applied := &Config{Prefix: "env1"}
	env := &Environment{
		Platform:          "aws",
		Config:            Config{Prefix: "env1", Region: "us-west-2"},
		LastAppliedConfig: applied,
		Status:            "destroy_in_progress",
		HostIPs:           map[string]string{"h": "10.0.0.1"},
		History:           []HistoryEvent{{Action: "deploy", Status: "success"}},
	}
	markEnvironmentDestroyed(env)
	if env.Status != "deleted" {
		t.Fatalf("expected status deleted, got %q", env.Status)
	}
	if env.LastAppliedConfig != nil || env.HostIPs != nil {
		t.Fatal("expected applied config and host IPs to be cleared")
	}
	if env.Config.Region != "us-west-2" || len(env.History) != 1 {
		t.Fatal("expected config and history to be kept")
	}
}

func TestWithoutStaleAppliedConfig(t *testing.T) {
	applied := &Config{Prefix: "env1"}
	running := &Environment{Status: "running", LastAppliedConfig: applied}
	if got := withoutStaleAppliedConfig(running); got.LastAppliedConfig != applied {
		t.Fatal("running environment must keep its applied config")
	}

	legacy := &Environment{Status: "deleted", LastAppliedConfig: applied, HostIPs: map[string]string{"h": "1.2.3.4"}}
	got := withoutStaleAppliedConfig(legacy)
	if got.LastAppliedConfig != nil || got.HostIPs != nil {
		t.Fatal("destroyed environment must drop its stale applied config")
	}
	if legacy.LastAppliedConfig != applied {
		t.Fatal("original environment must not be mutated")
	}
}

func TestSaveDestroyedEnvironmentAllowsTopologyChange(t *testing.T) {
	useTempState(t)
	applied := Config{Prefix: "envone", Clusters: map[string]ClusterConfig{"cl01": {ConfigsvrCount: 3}}}
	if err := saveState(map[string]*Environment{
		"envone": {
			Platform:          "docker",
			Config:            applied,
			LastAppliedConfig: cloneConfig(applied),
			Status:            "deleted",
		},
	}); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	body := `{"env_id":"envone","platform":"docker","config":{"clusters":{"cl01":{"configsvr_count":5}}}}`
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	state, err := loadState()
	if err != nil {
		t.Fatalf("loadState failed: %v", err)
	}
	env := state["envone"]
	if env.Status != "deleted" {
		t.Fatalf("expected status to stay deleted, got %q", env.Status)
	}
	if env.LastAppliedConfig != nil {
		t.Fatal("expected stale applied config to be dropped on save")
	}
	if env.Config.Clusters["cl01"].ConfigsvrCount != 5 {
		t.Fatal("expected updated topology to be saved")
	}
}

func TestSaveCreateRejectsExistingEnvironment(t *testing.T) {
	useTempState(t)
	if err := saveState(map[string]*Environment{
		"envone": {Platform: "docker", Config: Config{Prefix: "envone"}, Status: "running"},
	}); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	body := `{"env_id":"envone","platform":"docker","create":true,"config":{}}`
	rec := httptest.NewRecorder()
	saveEnvironmentHandler(rec, httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp.Error, "already exists") {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
}

func TestDuplicateConfigClearsIdentityAndChaosToken(t *testing.T) {
	src := Config{
		Prefix:              "envone",
		ChaosApiTokenPath:   chaosTokenUploadPath("envone-token"),
		LegacyChaosAPIToken: "secret",
		DeleteAfterDays:     3,
		Replsets:            map[string]ReplsetConfig{"rs01": {DataNodesPerReplset: 3}},
	}
	cfg := duplicateConfig(src, "chaos", map[string]*Environment{})
	if cfg.Prefix != "" || cfg.ChaosApiTokenPath != "" || cfg.LegacyChaosAPIToken != "" {
		t.Fatalf("expected identity-bound fields to be cleared, got %+v", cfg)
	}
	if cfg.DeleteAfterDays != 3 || cfg.Replsets["rs01"].DataNodesPerReplset != 3 {
		t.Fatal("expected the rest of the definition to be copied")
	}
	if src.ChaosApiTokenPath == "" {
		t.Fatal("source config must not be mutated")
	}
}

func TestDuplicateConfigReassignsBusyDockerServicePorts(t *testing.T) {
	src := Config{
		Prefix:           "envone",
		PmmServers:       map[string]PmmServerConfig{"pmm-server": {PmmPort: 8443, PmmExternalPort: 8443}},
		SeaweedFSServers: map[string]SeaweedFSServerConfig{"seaweedfs": {SeaweedFSPort: 8333, SeaweedFSAdminPort: 9333}},
	}
	state := map[string]*Environment{
		"envone": {Platform: "docker", Config: src, Status: "running"},
	}
	cfg := duplicateConfig(src, "docker", state)
	if got := cfg.PmmServers["pmm-server"].PmmExternalPort; got == 8443 || got == 0 {
		t.Fatalf("expected PMM port to be reassigned, got %d", got)
	}
	sw := cfg.SeaweedFSServers["seaweedfs"]
	if sw.SeaweedFSPort == 8333 || sw.SeaweedFSAdminPort == 9333 {
		t.Fatalf("expected SeaweedFS ports to be reassigned, got %+v", sw)
	}
	if src.PmmServers["pmm-server"].PmmExternalPort != 8443 {
		t.Fatal("source config must not be mutated")
	}

	// A destroyed source does not hold its ports.
	state["envone"].Status = "deleted"
	cfg = duplicateConfig(src, "docker", state)
	if got := cfg.PmmServers["pmm-server"].PmmExternalPort; got != 8443 {
		t.Fatalf("expected PMM port to be kept, got %d", got)
	}
}

func TestConfigureDuplicatePrefillsNewEnvironment(t *testing.T) {
	useTempState(t)
	if err := saveState(map[string]*Environment{
		"envone": {Platform: "docker", Config: Config{Prefix: "envone", MongoRelease: "psmdb-80"}, Status: "deleted"},
	}); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/configure/chaos?from=envone", nil)
	req.SetPathValue("platform", "chaos")
	configureHandler(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/configure/docker?from=envone" {
		t.Fatalf("expected redirect to source platform, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/configure/docker?from=missing", nil)
	req.SetPathValue("platform", "docker")
	configureHandler(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("expected redirect home for unknown source, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
