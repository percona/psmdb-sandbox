package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// This fixture uses only Terraform's built-in terraform_data resource. It never
// contacts Docker, a cloud provider, or the repository's live state directories.
func TestTerraformBackendIsolationWithRealCLI(t *testing.T) {
	terraform, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("Terraform CLI is not installed")
	}
	useTempState(t)
	source := t.TempDir()
	config := `terraform {
  required_version = ">= 1.9"
  backend "local" {}
}
variable "environment" { type = string }
resource "terraform_data" "test" { input = var.environment }
`
	if err := os.WriteFile(filepath.Join(source, "main.tf"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	type fixture struct{ id, data, state string }
	var fixtures []fixture
	for _, id := range []string{"first", "second"} {
		data, err := prepareTerraformDataDir(id, "docker")
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture{id, data, filepath.Join(source, id+".tfstate")})
	}
	run := func(f fixture, args ...string) error {
		cmd := exec.Command(terraform, args...)
		cmd.Dir = source
		cmd.Env = append(os.Environ(), "TF_DATA_DIR="+f.data, "TF_WORKSPACE=default", "TF_IN_AUTOMATION=1", "TF_CLI_ARGS=", "TF_CLI_ARGS_init=", "TF_CLI_ARGS_apply=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s %s: %w\n%s", f.id, strings.Join(args, " "), err, out)
		}
		return nil
	}
	// Finish both inits before either apply: shared metadata would now point
	// both applies at the last initialized backend and corrupt the isolation.
	for _, f := range fixtures {
		if err := run(f, "init", "-input=false", "-no-color", "-reconfigure", "-backend-config=path="+f.state); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, f := range fixtures {
		wg.Add(1)
		go func(f fixture) {
			defer wg.Done()
			if err := run(f, "apply", "-auto-approve", "-input=false", "-no-color", "-var=environment="+f.id); err != nil {
				t.Error(err)
			}
		}(f)
	}
	wg.Wait()
	for _, f := range fixtures {
		data, err := os.ReadFile(f.state)
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Resources []struct {
				Instances []struct {
					Attributes struct {
						Input struct {
							Value string `json:"value"`
						} `json:"input"`
					} `json:"attributes"`
				} `json:"instances"`
			} `json:"resources"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		if len(state.Resources) != 1 || len(state.Resources[0].Instances) != 1 || state.Resources[0].Instances[0].Attributes.Input.Value != f.id {
			t.Fatalf("state for %s belongs to another environment", f.id)
		}
	}
}
