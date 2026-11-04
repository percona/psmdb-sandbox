package main

import (
	"net/http/httptest"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestEnvironmentScriptSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	rec := httptest.NewRecorder()
	renderPage(rec, "environment", EnvironmentData{
		EnvID: "testenv", Env: &Environment{Platform: "docker", Status: "deploy_in_progress", LastJobID: "testjob"},
		OperationInProgress: true,
	})
	if rec.Code != 200 {
		t.Fatalf("render: %d %s", rec.Code, rec.Body.String())
	}
	scripts := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(rec.Body.String(), -1)
	if len(scripts) == 0 {
		t.Fatal("no inline scripts rendered")
	}
	for _, script := range scripts {
		cmd := exec.Command(node, "--check")
		cmd.Stdin = strings.NewReader(script[1])
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("invalid rendered JavaScript: %v\n%s", err, out)
		}
	}
}
