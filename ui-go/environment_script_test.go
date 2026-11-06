package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestConnectionModalCommandSteps(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	content, err := os.ReadFile("templates/environment.html")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	start := strings.Index(source, "function openConnectionStringModal(")
	end := strings.Index(source, "function closeConnectionStringModal(")
	if start < 0 || end <= start {
		t.Fatal("connection modal functions not found")
	}
	script := `
const assert = require('node:assert/strict');
function element(tag) {
  return {
    tag, children: [], style: {}, textContent: '',
    append(...children) { this.children.push(...children); },
    replaceChildren() { this.children = []; },
    setAttribute(name, value) { this[name] = value; }
  };
}
const elements = {};
const document = {
  createElement: element,
  getElementById(id) { return elements[id] ||= element('div'); }
};
let copied;
function copyText(text) { copied = text; }
` + source[start:end] + `
const steps = [
  {scope: 'host', label: 'Export the CA:', command: "docker exec 'node' cat /etc/mongodb-tls/test-ca.pem > 'test-ca.pem'"},
  {scope: 'host', label: 'Connect from host:', command: "mongosh 'mongodb://u:p@localhost/?tls=true&tlsCAFile=test-ca.pem'"},
  {scope: 'docker', label: '<b>Connect inside Docker:</b>', command: "docker exec -it 'node' mongosh 'mongodb://node/admin?tls=true' -u 'u' -p 'p'"}
];
openConnectionStringModalFromButton({dataset: {
  mongoName: 'secure', connectionString: 'mongodb://localhost/?tls=true',
  connectionInstructions: 'The first member may not be primary.',
  connectionSteps: JSON.stringify(steps)
}});
const panel = elements['connection-string-steps'];
const dockerPanel = elements['connection-string-docker-steps'];
assert.equal(panel.children.length, 2);
assert.equal(dockerPanel.children.length, 1);
assert.equal(elements['connection-string-host-tls-help'].style.display, '');
assert.equal(elements['connection-string-docker-section'].style.display, '');
steps.forEach((step, i) => {
  const section = step.scope === 'docker' ? dockerPanel.children[0] : panel.children[i];
  const [label, row] = section.children;
  assert.equal(label.textContent, step.label);
  assert.equal(row.className, 'copy-cmd-row copy-cmd-row-wrap');
  const [code, button] = row.children;
  assert.equal(code.tag, 'code');
  assert.equal(code.className, 'copy-cmd-text copy-cmd-text-wrap');
  assert.equal(code.textContent, step.command);
  assert.equal(button.textContent, 'Copy to clipboard');
  button.onclick();
  assert.equal(copied, step.command);
});
assert.equal(elements['connection-string-instructions'].textContent, 'The first member may not be primary.');
assert.equal(elements['connection-string-modal'].style.display, 'flex');
elements['connection-string-copy'].onclick();
assert.equal(copied, "'mongodb://localhost/?tls=true'");
openConnectionStringModalFromButton({dataset: {mongoName: 'plain', connectionString: 'mongodb://localhost/'}});
assert.equal(panel.children.length, 0);
assert.equal(dockerPanel.children.length, 0);
assert.equal(elements['connection-string-host-tls-help'].style.display, 'none');
assert.equal(elements['connection-string-docker-section'].style.display, 'none');
assert.equal(elements['connection-string-instructions'].textContent, '');
assert.equal(elements['connection-string-instructions'].style.display, 'none');
`
	cmd := exec.Command(node)
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("connection modal command steps: %v\n%s", err, out)
	}
}

func TestConnectionModalExplainsConnectionMethods(t *testing.T) {
	for _, platform := range []string{"docker", "aws"} {
		t.Run(platform, func(t *testing.T) {
			rec := httptest.NewRecorder()
			renderPage(rec, "environment", EnvironmentData{
				EnvID: "testenv", Env: &Environment{Platform: platform},
			})
			if rec.Code != 200 {
				t.Fatalf("render: %d %s", rec.Code, rec.Body.String())
			}
			page := rec.Body.String()
			title := "Connect from your client machine"
			if platform == "docker" {
				title = "Connect from the Docker host"
			}
			for _, want := range []string{
				"<h3>" + title + "</h3>",
				"<h4>Connection URI</h4>",
				"Export the CA before using this TLS URI.",
				"Connect inside Docker (alternative)",
				"No CA export is needed",
			} {
				if !strings.Contains(page, want) {
					t.Errorf("connection guidance missing %q", want)
				}
			}
			if strings.Contains(page, "password when prompted") {
				t.Error("connection guidance must not describe a password prompt")
			}
		})
	}
}

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
