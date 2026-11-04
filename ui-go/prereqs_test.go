package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubPrerequisite(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPrerequisiteVersions(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		ok           bool
	}{
		{"terraform", `{"terraform_version":"1.9.0"}`, true},
		{"terraform", `{"terraform_version":"1.8.9"}`, false},
		{"terraform", `not-json`, false},
		{"ansible", `ansible [core 2.14.0]`, true},
		{"ansible", `ansible [core 2.13.13]`, false},
		{"ansible", `ansible 2.9.27`, false},
		{"python3", `Python 3.9.0`, true},
		{"python3", `Python 3.8.10`, false},
		{"aws", `aws-cli/2.31.0 Python/3.13`, true},
		{"aws", `aws-cli/1.32.0 Python/3.13`, false},
		{"docker", `27.5.1`, true},
		{"docker", `19.03.0`, false},
	} {
		t.Run(tc.name+"/"+tc.output, func(t *testing.T) {
			dir := t.TempDir()
			stubPrerequisite(t, dir, tc.name, "printf '%s\\n' "+shellQuote(tc.output))
			stubPrerequisite(t, dir, "ansible-playbook", "exit 0")
			t.Setenv("PATH", dir)
			ok, detail := checkPrerequisite(tc.name)
			if ok != tc.ok || detail == "" {
				t.Fatalf("check: %v %q, want %v", ok, detail, tc.ok)
			}
		})
	}
}

func TestCheckPrerequisiteFailures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, name := range []string{"docker", "terraform", "ansible", "python3", "ssh", "ssh-keygen"} {
		if ok, _ := checkPrerequisite(name); ok {
			t.Fatalf("missing %s reported ready", name)
		}
	}
	stubPrerequisite(t, dir, "docker", "exit 1")
	if ok, detail := checkPrerequisite("docker"); ok || !strings.Contains(detail, "daemon") {
		t.Fatalf("daemon failure: %v %q", ok, detail)
	}
	stubPrerequisite(t, dir, "ansible", "printf 'ansible [core 2.18.0]\\n'")
	if ok, detail := checkPrerequisite("ansible"); ok || !strings.Contains(detail, "ansible-playbook") {
		t.Fatalf("missing playbook CLI: %v %q", ok, detail)
	}
}

func TestCheckAnsibleCollections(t *testing.T) {
	for _, tc := range []struct {
		posix, general string
		ok             bool
	}{
		{"1.6.2", "9.5.5", true},
		{"2.0.0", "10.0.0", true},
		{"1.6.1", "9.5.5", false},
		{"1.6.2", "", false},
		{"", "", false},
		{"invalid", "9.5.5", false},
	} {
		dir := t.TempDir()
		resolved := map[string]any{}
		for plugin, version := range map[string]string{"ansible.posix.mount": tc.posix, "community.general.ini_file": tc.general} {
			if version == "" {
				continue
			}
			name := plugin[:strings.LastIndex(plugin, ".")]
			root := filepath.Join(dir, name)
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "MANIFEST.json"), []byte(`{"collection_info":{"version":"`+version+`"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			resolved[plugin] = map[string]any{"doc": map[string]string{"collection": name, "filename": filepath.Join(root, "plugins", "modules", "probe.py")}}
		}
		output, err := json.Marshal(resolved)
		if err != nil {
			t.Fatal(err)
		}
		// A lower-priority installation is deliberately newer. It must not
		// override the version resolved by ansible-doc.
		stubPrerequisite(t, dir, "ansible-galaxy", `printf '%s\n' '{"/system":{"ansible.posix":{"version":"99.0.0"},"community.general":{"version":"99.0.0"}}}'`)
		stubPrerequisite(t, dir, "ansible-doc", "printf '%s\\n' "+shellQuote(string(output)))
		t.Setenv("PATH", dir)
		if ok, detail := checkAnsibleCollections(); ok != tc.ok {
			t.Errorf("collections %s/%s: %v %q, want %v", tc.posix, tc.general, ok, detail, tc.ok)
		}
	}
}

func TestAnsiblePrerequisiteUsesDeploymentConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANSIBLE_CONFIG", filepath.Join(dir, "unrelated.cfg"))
	for _, name := range []string{"ansible", "ansible-doc", "ansible-galaxy"} {
		stubPrerequisite(t, dir, name, `printf '%s' "$ANSIBLE_CONFIG"`)
	}
	t.Setenv("PATH", dir)
	for _, name := range []string{"ansible", "ansible-doc", "ansible-galaxy"} {
		out, err := prerequisiteOutput(name)
		if err != nil || string(out) != filepath.Join(ansibleDir, "ansible.cfg") {
			t.Fatalf("%s config: %q, %v", name, out, err)
		}
	}
}

func TestCheckAnsibleCollectionsRejectsInvalidResolution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, script := range []string{"exit 1", "printf 'not-json'", "printf '{}'"} {
		stubPrerequisite(t, dir, "ansible-doc", script)
		if ok, detail := checkAnsibleCollections(); ok {
			t.Fatalf("invalid resolution accepted: %s (%s)", script, detail)
		}
	}
}

func TestAnsibleCollectionPrecedenceWithRealCLI(t *testing.T) {
	if !toolInstalled("ansible-doc") {
		t.Skip("ansible-doc is required")
	}
	oldAnsibleDir := ansibleDir
	ansibleDir = t.TempDir()
	t.Cleanup(func() { ansibleDir = oldAnsibleDir })
	if err := os.WriteFile(filepath.Join(ansibleDir, "ansible.cfg"), []byte("[defaults]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	roots := []string{t.TempDir(), t.TempDir()}
	for i, root := range roots {
		for _, plugin := range []string{"ansible.posix.mount", "community.general.ini_file"} {
			parts := strings.Split(plugin, ".")
			collection := filepath.Join(root, "ansible_collections", parts[0], parts[1])
			modules := filepath.Join(collection, "plugins", "modules")
			if err := os.MkdirAll(modules, 0700); err != nil {
				t.Fatal(err)
			}
			version := requiredAnsibleCollections[parts[0]+"."+parts[1]]
			if i == 0 {
				version = "1.0.0"
			}
			manifest := fmt.Sprintf(`{"collection_info":{"namespace":%q,"name":%q,"version":%q}}`, parts[0], parts[1], version)
			if err := os.WriteFile(filepath.Join(collection, "MANIFEST.json"), []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			module := fmt.Sprintf("DOCUMENTATION = '''\nmodule: %s\nshort_description: Readiness test fixture\ndescription: Readiness test fixture\nauthor: Test\n'''\n", parts[2])
			if err := os.WriteFile(filepath.Join(modules, parts[2]+".py"), []byte(module), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("ANSIBLE_COLLECTIONS_SCAN_SYS_PATH", "false")
	for _, tc := range []struct {
		paths []string
		ok    bool
	}{{roots, false}, {[]string{roots[1], roots[0]}, true}} {
		paths := strings.Join(tc.paths, string(os.PathListSeparator))
		// Support the environment variable spelling in both core 2.14 and newer.
		t.Setenv("ANSIBLE_COLLECTIONS_PATH", paths)
		t.Setenv("ANSIBLE_COLLECTIONS_PATHS", paths)
		if ok, detail := checkAnsibleCollections(); ok != tc.ok {
			t.Fatalf("resolved collection precedence: %v %q, want %v", ok, detail, tc.ok)
		}
	}
}

func TestCollectionPinsMatchReadinessChecks(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "ansible", "requirements.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, version := range requiredAnsibleCollections {
		if !strings.Contains(string(data), "name: "+name+"\n    version: \""+version+"\"") {
			t.Errorf("collection requirement for %s must match readiness check %s", name, version)
		}
	}
}
