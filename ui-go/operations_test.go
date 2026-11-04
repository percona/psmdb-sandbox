package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func environmentRequest(handler http.HandlerFunc, method, envID, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/environment/"+envID, strings.NewReader(body))
	req.SetPathValue("env_id", envID)
	handler(rec, req)
	return rec
}

func waitForOperationTest(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for test job")
}

func TestEnvironmentGuards(t *testing.T) {
	useTempState(t)
	if err := saveState(map[string]*Environment{"envone": {Platform: "docker", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	release, ok := tryEnvironmentOperation("envone")
	if !ok {
		t.Fatal("could not acquire lock")
	}
	for _, handler := range []http.HandlerFunc{saveEnvironmentHandler, deleteEnvironmentHandler, environmentActionHandler, clusterSyncActionHandler} {
		rec := environmentRequest(handler, http.MethodPost, "envone", `{"env_id":"envone","platform":"docker","config":{},"action":"stop"}`)
		if rec.Code != http.StatusConflict {
			t.Errorf("expected operation conflict, got %d: %s", rec.Code, rec.Body.String())
		}
	}
	rec := environmentRequest(envStatusHandler, http.MethodGet, "envone", "")
	if !strings.Contains(rec.Body.String(), `"operation_in_progress":true`) {
		t.Fatalf("status did not report active operation: %s", rec.Body.String())
	}
	release()
	rec = environmentRequest(saveEnvironmentHandler, http.MethodPost, "envone", `{"env_id":"envone","platform":"aws","config":{}}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "platform") {
		t.Fatalf("platform changed: %d %s", rec.Code, rec.Body.String())
	}
	if err := updateState(func(state map[string]*Environment) error { state["envone"].Status = "deploy_in_progress"; return nil }); err != nil {
		t.Fatal(err)
	}
	rec = environmentRequest(envStatusHandler, http.MethodGet, "envone", "")
	if !strings.Contains(rec.Body.String(), `"operation_in_progress":false`) {
		t.Fatalf("stale status reported an active lock: %s", rec.Body.String())
	}
	// A saved status is not an active lock after a UI restart. Do not make
	// interrupted environments permanently uneditable or impossible to retry.
	rec = environmentRequest(saveEnvironmentHandler, http.MethodPost, "envone", `{"env_id":"envone","platform":"docker","config":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("stale status blocked edits without an active operation: %d %s", rec.Code, rec.Body.String())
	}
}

func TestConcurrentDeploysUseIsolatedTerraformMetadata(t *testing.T) {
	useTempState(t)
	oldJobs, oldSettings := jobsDir, settingsFile
	jobsDir, settingsFile = t.TempDir(), ""
	t.Cleanup(func() { jobsDir, settingsFile = oldJobs, oldSettings })
	bin := t.TempDir()
	stub := `#!/bin/sh
set -eu
case "$1" in
  init)
    for arg in "$@"; do
      case "$arg" in -backend-config=path=*) printf '%s' "${arg#-backend-config=path=}" > "$TF_DATA_DIR/backend";; esac
    done
    ;;
  apply)
    test "$TF_WORKSPACE" = default
    printf '%s' "$TF_DATA_DIR" > "$TF_DATA_DIR/started"
    for n in $(seq 1 500); do
      test ! -f "$TF_DATA_DIR/release" || exit 0
      sleep 0.02
    done
    exit 99
    ;;
  *) exit 98;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "terraform"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TF_DATA_DIR", filepath.Join(t.TempDir(), "must-not-use"))
	t.Setenv("TF_WORKSPACE", "must-not-use")
	if err := saveState(map[string]*Environment{
		"first":  {Platform: "docker", Config: Config{Prefix: "first"}, Status: "configured"},
		"second": {Platform: "docker", Config: Config{Prefix: "second"}, Status: "configured"},
	}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	var dirs []string
	// Always unblock and wait for jobs before test globals are restored.
	defer func() {
		for _, dir := range dirs {
			_ = os.WriteFile(filepath.Join(dir, "release"), nil, 0600)
		}
		for _, id := range ids {
			waitForOperationTest(t, func() bool {
				release, ok := tryEnvironmentOperation(id)
				if ok {
					release()
				}
				return ok
			})
		}
	}()
	for _, id := range []string{"first", "second"} {
		dir, err := prepareTerraformDataDir(id, "docker")
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
		rec := environmentRequest(environmentActionHandler, http.MethodPost, id, `{"action":"deploy"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("dispatch: %d %s", rec.Code, rec.Body.String())
		}
		ids = append(ids, id)
		waitForOperationTest(t, func() bool { _, err := os.Stat(filepath.Join(dir, "started")); return err == nil })
		backend, err := os.ReadFile(filepath.Join(dir, "backend"))
		if err != nil || string(backend) != tfstatePath(id, "docker") || !filepath.IsAbs(string(backend)) {
			t.Fatalf("wrong backend: %q %v", backend, err)
		}
		var response struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.JobID == "" {
			t.Fatal("missing job ID")
		}
		for _, handler := range []http.HandlerFunc{environmentActionHandler, deleteEnvironmentHandler, saveEnvironmentHandler} {
			body := `{"env_id":"` + id + `","platform":"docker","config":{},"action":"destroy"}`
			conflict := environmentRequest(handler, http.MethodPost, id, body)
			if conflict.Code != http.StatusConflict {
				t.Fatalf("overlapping operation accepted: %d %s", conflict.Code, conflict.Body.String())
			}
		}
	}
	if dirs[0] == dirs[1] {
		t.Fatal("jobs share Terraform metadata")
	}
	for _, dir := range dirs {
		if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	waitForOperationTest(t, func() bool {
		state, err := loadState()
		return err == nil && state["first"].Status == "running" && state["second"].Status == "running"
	})
	state, _ := loadState()
	for _, id := range ids {
		if state[id].LastAppliedConfig == nil || state[id].LastAppliedConfig.Prefix != id || len(state[id].History) != 1 {
			t.Fatalf("lost job completion for %s: %+v", id, state[id])
		}
	}
}

func TestJobInitializationFailureFinalizesOperation(t *testing.T) {
	oldJobs := jobsDir
	jobsDir = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() { jobsDir = oldJobs })
	release, ok := tryEnvironmentOperation("broken")
	if !ok {
		t.Fatal("lock unavailable")
	}
	called := false
	runJob("broken", []string{"does-not-run"}, "", nil, func(status string) {
		called = true
		if status != "error" {
			t.Errorf("status = %s", status)
		}
		release()
	})
	if !called {
		t.Fatal("completion callback omitted")
	}
	release, ok = tryEnvironmentOperation("broken")
	if !ok {
		t.Fatal("operation lock leaked")
	}
	release()
}

func TestActionInitializationFailureDoesNotLeaveEnvironmentLocked(t *testing.T) {
	useTempState(t)
	oldJobs, oldSettings := jobsDir, settingsFile
	jobsDir, settingsFile = filepath.Join(t.TempDir(), "missing"), ""
	t.Cleanup(func() { jobsDir, settingsFile = oldJobs, oldSettings })
	if err := saveState(map[string]*Environment{"broken": {Platform: "docker", Status: "configured", Config: Config{Prefix: "broken"}}}); err != nil {
		t.Fatal(err)
	}
	rec := environmentRequest(environmentActionHandler, http.MethodPost, "broken", `{"action":"deploy"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("dispatch: %d %s", rec.Code, rec.Body.String())
	}
	waitForOperationTest(t, func() bool { return !environmentOperationActive("broken") })
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if state["broken"].Status != "deploy_failed" || len(state["broken"].History) != 1 {
		t.Fatalf("job failure was not persisted: %+v", state["broken"])
	}
}

func TestSaveTfvarsFailureDoesNotCommitConfiguration(t *testing.T) {
	useTempState(t)
	if err := saveState(map[string]*Environment{"envone": {Platform: "docker", Status: "configured", Config: Config{Prefix: "envone"}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(terraformDir, "docker"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	rec := environmentRequest(saveEnvironmentHandler, http.MethodPost, "envone", `{"env_id":"envone","platform":"docker","config":{"region":"changed"}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if state["envone"].Config.Region != "" {
		t.Fatal("failed save committed configuration")
	}
	if environmentOperationActive("envone") {
		t.Fatal("failed save leaked lock")
	}
}

func TestCloneConfigIsDeep(t *testing.T) {
	on := true
	source := Config{
		Replsets:    map[string]ReplsetConfig{"rs": {EnablePbm: &on}},
		PmmServers:  map[string]PmmServerConfig{"pmm": {PmmPort: 443}},
		AnsibleVars: map[string]string{"test": "original"},
		ClusterSync: ClusterSyncConfig{IncludeNamespaces: []string{"db.original"}},
	}
	copy := cloneConfig(source)
	*source.Replsets["rs"].EnablePbm = false
	source.PmmServers["pmm"] = PmmServerConfig{PmmPort: 8443}
	source.AnsibleVars["test"] = "changed"
	source.ClusterSync.IncludeNamespaces[0] = "db.changed"
	if !*copy.Replsets["rs"].EnablePbm || copy.PmmServers["pmm"].PmmPort != 443 || copy.AnsibleVars["test"] != "original" || copy.ClusterSync.IncludeNamespaces[0] != "db.original" {
		t.Fatal("job snapshot shares mutable configuration")
	}
}
