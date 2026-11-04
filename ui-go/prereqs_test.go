package main

import (
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
		output string
		ok     bool
	}{
		{`{"/collections":{"ansible.posix":{"version":"1.6.2"},"community.general":{"version":"9.5.5"}}}`, true},
		{`{"/collections":{"ansible.posix":{"version":"2.0.0"},"community.general":{"version":"10.0.0"}}}`, true},
		{`{"/collections":{"ansible.posix":{"version":"1.6.1"},"community.general":{"version":"9.5.5"}}}`, false},
		{`{"/collections":{"ansible.posix":{"version":"1.6.2"}}}`, false},
		{`{}`, false}, {`not-json`, false},
	} {
		dir := t.TempDir()
		stubPrerequisite(t, dir, "ansible-galaxy", "printf '%s\\n' "+shellQuote(tc.output))
		t.Setenv("PATH", dir)
		if ok, detail := checkAnsibleCollections(); ok != tc.ok {
			t.Errorf("collections %s: %v %q, want %v", tc.output, ok, detail, tc.ok)
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
