package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"
)

type selectionContainer struct{ ID, Names, Environment string }

func (c selectionContainer) Label(key string) string {
	if key == dockerEnvironmentLabel {
		return c.Environment
	}
	return ""
}

func TestDockerSelectionDoesNotMatchOtherEnvironments(t *testing.T) {
	containers := []selectionContainer{
		{"one", "db-rs0", "db"},
		{"two", "db-legacy", ""},
		{"three", "mydb-rs0", "mydb"},
		{"four", "mydb-legacy", ""},
		{"five", "db-conflicting-label", "other"},
		{"six", "renamed-container", "db"},
	}
	var selected []string
	for _, args := range dockerEnvironmentQueries("db", true, "{{.ID}}") {
		filter, format := args[len(args)-3], args[len(args)-1]
		tmpl, err := template.New("docker").Parse(format)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range containers {
			matches := false
			if strings.HasPrefix(filter, "label=") {
				matches = filter == "label="+dockerEnvironmentLabel+"="+c.Environment
			} else {
				matches, err = regexp.MatchString(strings.TrimPrefix(filter, "name="), "/"+c.Names)
				if err != nil {
					t.Fatal(err)
				}
			}
			if matches {
				var out bytes.Buffer
				if err := tmpl.Execute(&out, c); err != nil {
					t.Fatal(err)
				}
				if out.Len() > 0 {
					selected = append(selected, out.String())
				}
			}
		}
	}
	if strings.Join(selected, ",") != "one,six,two" {
		t.Fatalf("wrong containers selected: %v", selected)
	}
}

func TestDockerActionShellPropagatesSelectionFailure(t *testing.T) {
	bin := t.TempDir()
	stub := "#!/bin/sh\nif [ \"$1\" = ps ]; then exit 17; fi\necho unexpected mutation >&2\nexit 99\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, action := range []string{"stop", "restart"} {
		cmd := exec.Command("bash", "-c", dockerEnvironmentActionShell("db", action))
		if out, err := cmd.CombinedOutput(); err == nil || strings.Contains(string(out), "unexpected mutation") {
			t.Fatalf("selection failure masked or executed mutation: %s %v", out, err)
		}
	}
}

func TestEveryDockerContainerHasEnvironmentLabel(t *testing.T) {
	root := filepath.Join("..", "terraform", "docker")
	resource := regexp.MustCompile(`(?m)^resource "[^"]+" "[^"]+" \{`)
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".tf" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		matches := resource.FindAllStringIndex(text, -1)
		for i, match := range matches {
			if !strings.HasPrefix(text[match[0]:match[1]], `resource "docker_container"`) {
				continue
			}
			end := len(text)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			if !strings.Contains(text[match[0]:end], `label = "psmdb-sandbox.environment"`) {
				t.Errorf("missing environment label: %s %s", path, text[match[0]:match[1]])
			}
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 31 {
		t.Fatalf("unexpected container coverage: %d", count)
	}
}
