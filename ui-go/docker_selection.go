package main

import (
	"fmt"
	"regexp"
	"strings"
)

const dockerEnvironmentLabel = "psmdb-sandbox.environment"

// Prefer exact labels. The second query includes only unlabeled legacy
// containers whose names start with the complete environment prefix.
func dockerEnvironmentQueries(prefix string, all bool, format string) [][]string {
	base := []string{"ps"}
	if all {
		base = append(base, "-a")
	}
	labeled := append(append([]string{}, base...), "--filter", "label="+dockerEnvironmentLabel+"="+prefix, "--format", format)
	legacyFormat := fmt.Sprintf(`{{if eq (.Label %q) ""}}%s{{end}}`, dockerEnvironmentLabel, format)
	legacy := append(append([]string{}, base...), "--filter", "name=^/"+regexp.QuoteMeta(prefix)+"-", "--format", legacyFormat)
	return [][]string{labeled, legacy}
}

func dockerEnvironmentListShell(prefix string, all bool, format string) string {
	var commands []string
	for _, args := range dockerEnvironmentQueries(prefix, all, format) {
		command := "docker"
		for _, arg := range args {
			command += " " + shellQuote(arg)
		}
		commands = append(commands, command)
	}
	return "{ " + strings.Join(commands, " && ") + "; }"
}

func dockerEnvironmentActionShell(prefix, action string) string {
	return "set -o pipefail; " + dockerEnvironmentListShell(prefix, action == "restart", "{{.ID}}") + " | xargs -r docker " + action
}

func dockerEnvironmentContainerNames(prefix string) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	for _, args := range dockerEnvironmentQueries(prefix, true, "{{.Names}}") {
		out, err := execOutput("docker", args...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			name := strings.TrimPrefix(strings.TrimSpace(line), "/")
			if name != "" && !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	return names, nil
}
