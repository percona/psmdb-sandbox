package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerPlatformTfvars(t *testing.T) {
	old := terraformDir
	terraformDir = t.TempDir()
	t.Cleanup(func() { terraformDir = old })
	for _, tc := range []struct {
		name, platform, architecture string
		want                         string
	}{
		{"native", "docker", "", ""},
		{"amd64", "docker", "linux/amd64", `docker_platform = "linux/amd64"`},
		{"arm64", "docker", "linux/arm64", `docker_platform = "linux/arm64"`},
		{"cloud", "aws", "linux/amd64", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := writeTfvars("platformtest", tc.platform, Config{DockerPlatform: tc.architecture}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(tfvarsPath("platformtest", tc.platform))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if tc.want == "" {
				if strings.Contains(text, "docker_platform") {
					t.Fatalf("native/cloud tfvars must omit docker_platform: %s", text)
				}
			} else if !strings.Contains(text, tc.want) {
				t.Fatalf("missing %q in tfvars: %s", tc.want, text)
			}
		})
	}
	if err := writeTfvars("invalid", "docker", Config{DockerPlatform: "darwin/arm64"}); err == nil {
		t.Fatal("accepted a non-Linux Docker platform")
	}
}

func TestDockerPlatformSaveAndRestore(t *testing.T) {
	oldState, oldTerraform, oldData := stateFile, terraformDir, dataDir
	dataDir = t.TempDir()
	stateFile = filepath.Join(dataDir, "environments.json")
	terraformDir = t.TempDir()
	t.Cleanup(func() { stateFile, terraformDir, dataDir = oldState, oldTerraform, oldData })

	for _, architecture := range []string{"linux/amd64", "linux/arm64", ""} {
		body, err := json.Marshal(map[string]any{
			"env_id": "platformtest", "platform": "docker", "config": Config{DockerPlatform: architecture},
		})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		saveEnvironmentHandler(rec, httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(string(body))))
		if rec.Code != http.StatusOK {
			t.Fatalf("save %q: %d: %s", architecture, rec.Code, rec.Body.String())
		}
		state, err := loadState()
		if err != nil {
			t.Fatal(err)
		}
		cfg := state["platformtest"].Config
		if cfg.DockerPlatform != architecture || profileConfig(cfg).DockerPlatform != architecture {
			t.Fatalf("architecture %q was not retained in the environment/profile", architecture)
		}
		page := editorFixture("docker")
		page.Config = cfg
		if !strings.Contains(renderConfigure(t, page), `value="`+architecture+`" selected>`) {
			t.Fatalf("saved architecture %q was not selected when editing", architecture)
		}
		data, err := os.ReadFile(tfvarsPath("platformtest", "docker"))
		if err != nil {
			t.Fatal(err)
		}
		if architecture == "" && strings.Contains(string(data), "docker_platform") {
			t.Fatal("switching back to Native did not clear the Terraform override")
		}
	}
}

func TestDockerPlatformInvalidSave(t *testing.T) {
	for _, architecture := range []string{"amd64", "darwin/arm64", "linux/invalid"} {
		rec := httptest.NewRecorder()
		body := `{"env_id":"platformtest","platform":"docker","config":{"docker_platform":"` + architecture + `"}}`
		saveEnvironmentHandler(rec, httptest.NewRequest(http.MethodPost, "/api/environment", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid Docker image architecture") {
			t.Fatalf("invalid architecture %q: %d: %s", architecture, rec.Code, rec.Body.String())
		}
	}
}

func TestDockerPlatformSelectorVisibility(t *testing.T) {
	for _, platform := range []string{"docker", "aws", "gcp", "azure", "chaos"} {
		page := renderConfigure(t, editorFixture(platform))
		if got := strings.Contains(page, `id="docker_platform"`); got != (platform == "docker") {
			t.Errorf("%s: Docker architecture selector visibility = %v", platform, got)
		}
		if platform == "docker" {
			if !strings.Contains(page, `class="field-tip" data-tip="Native selects the Docker daemon`) || strings.Contains(page, `id="docker-platform-help"`) {
				t.Error("architecture help must be a tooltip, not inline text")
			}
			for _, want := range []string{"Native — match the Docker host", "AMD64 — emulate on ARM hosts", "ARM64 — emulate on x86 hosts", "plgm always uses AMD64", "Watchtower defaults to AMD64"} {
				if !strings.Contains(page, want) {
					t.Errorf("missing architecture choice/help %q", want)
				}
			}
		}
	}
}
