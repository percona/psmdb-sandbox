package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const chaosProviderExampleVersion = "1.0.0"

// toolInstalled returns true when the named executable can be found on PATH.
func toolInstalled(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func chaosProviderTarget() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}

func standardTerraformPluginRoot() (string, error) {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "terraform.d", "plugins"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".terraform.d", "plugins"), nil
}

func chaosProviderInstalledIn(pluginRoot string) bool {
	pattern := filepath.Join(
		pluginRoot,
		"registry.terraform.io",
		"percona",
		"chaos",
		"*",
		chaosProviderTarget(),
		"terraform-provider-chaos*",
	)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func chaosProviderInstalled() bool {
	pluginRoot, err := standardTerraformPluginRoot()
	if err != nil {
		return false
	}
	return chaosProviderInstalledIn(pluginRoot)
}

func chaosProviderInstallPath() (string, error) {
	pluginRoot, err := standardTerraformPluginRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(
		pluginRoot,
		"registry.terraform.io",
		"percona",
		"chaos",
		chaosProviderExampleVersion,
		chaosProviderTarget(),
	), nil
}

func chaosProviderMissingError() string {
	installPath, err := chaosProviderInstallPath()
	if err != nil {
		return "CHAOS Terraform provider percona/chaos is not installed and the standard Terraform plugin directory could not be determined: " + err.Error()
	}
	return fmt.Sprintf(
		"CHAOS Terraform provider percona/chaos is not installed for %s. Install terraform-provider-chaos under %s and retry.",
		chaosProviderTarget(),
		installPath,
	)
}

// platformPrereqs returns the prerequisite tool list for a given platform.
// Each entry has install guidance appropriate for the host OS.
func platformPrereqs(platform string) []PrereqTool {
	terraform := PrereqTool{
		Name:       "terraform",
		InstallDoc: "https://developer.hashicorp.com/terraform/install",
		InstallCmds: []string{
			"# macOS (Homebrew)",
			"brew tap hashicorp/tap && brew install hashicorp/tap/terraform",
			"",
			"# Linux (apt)",
			"wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg",
			`echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list`,
			"sudo apt-get update && sudo apt-get install -y terraform",
		},
	}
	ansible := PrereqTool{
		Name:       "ansible",
		InstallDoc: "https://docs.ansible.com/ansible/latest/installation_guide/",
		InstallCmds: []string{
			"# macOS (pip)",
			"pip3 install --user ansible",
			"",
			"# Linux (apt)",
			"sudo apt-get update && sudo apt-get install -y ansible",
			"",
			"# Linux (pip)",
			"pip3 install --user ansible",
		},
	}
	python := PrereqTool{
		Name: "python3", InstallDoc: "https://www.python.org/downloads/",
		InstallCmds: []string{"# From the repository root", "./scripts/install-prerequisites.sh"},
	}
	collections := PrereqTool{
		Name: "ansible-collections", InstallDoc: "https://docs.ansible.com/ansible/latest/collections_guide/collections_installing.html",
		InstallCmds: []string{"# From the repository root, as the UI user", "ansible-galaxy collection install -r ansible/requirements.yml"},
	}
	ssh := PrereqTool{
		Name: "ssh", InstallDoc: "https://www.openssh.com/",
		InstallCmds: []string{"# From the repository root", "./scripts/install-prerequisites.sh"},
	}
	keygen := ssh
	keygen.Name = "ssh-keygen"
	dockerCLI := PrereqTool{
		Name:       "docker",
		InstallDoc: "https://docs.docker.com/engine/install/",
		InstallCmds: []string{
			"# macOS – install Docker Desktop",
			"brew install --cask docker",
			"",
			"# Linux (convenience script)",
			"curl -fsSL https://get.docker.com | sh",
			"sudo usermod -aG docker $USER   # then log out / in",
		},
	}
	awsCLI := PrereqTool{
		Name:       "aws",
		InstallDoc: "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html",
		InstallCmds: []string{
			"# macOS (Homebrew)",
			"brew install awscli",
			"",
			"# Linux",
			`curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o awscliv2.zip`,
			"unzip awscliv2.zip && sudo ./aws/install",
		},
	}
	gcloudCLI := PrereqTool{
		Name:       "gcloud",
		InstallDoc: "https://cloud.google.com/sdk/docs/install",
		InstallCmds: []string{
			"# macOS (Homebrew)",
			"brew install --cask google-cloud-sdk",
			"",
			"# Linux",
			"curl https://sdk.cloud.google.com | bash",
			"exec -l $SHELL",
			"gcloud init",
		},
	}
	azureCLI := PrereqTool{
		Name:       "az",
		InstallDoc: "https://learn.microsoft.com/cli/azure/install-azure-cli",
		InstallCmds: []string{
			"# macOS (Homebrew)",
			"brew install azure-cli",
			"",
			"# Linux (apt)",
			"curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash",
		},
	}
	chaosProvider := PrereqTool{
		Name:       "terraform-provider-chaos",
		InstallDoc: "https://developer.hashicorp.com/terraform/cli/config/config-file#implied-local-mirror-directories",
		InstallCmds: []string{
			"Install terraform-provider-chaos in the standard local Terraform plugin path:",
			filepath.Join("~", ".terraform.d", "plugins", "registry.terraform.io", "percona", "chaos", chaosProviderExampleVersion, chaosProviderTarget()),
		},
	}

	var tools []PrereqTool
	switch platform {
	case "docker":
		tools = []PrereqTool{dockerCLI, terraform}
	case "aws":
		tools = []PrereqTool{terraform, ansible, python, collections, ssh, keygen, awsCLI}
	case "gcp":
		tools = []PrereqTool{terraform, ansible, python, collections, ssh, keygen, gcloudCLI}
	case "azure":
		tools = []PrereqTool{terraform, ansible, python, collections, ssh, keygen, azureCLI}
	case "chaos":
		tools = []PrereqTool{terraform, ansible, python, collections, ssh, keygen, chaosProvider}
	default:
		tools = []PrereqTool{terraform, ansible, python, collections, ssh, keygen}
	}

	var wg sync.WaitGroup
	for i := range tools {
		wg.Add(1)
		go func(tool *PrereqTool) {
			defer wg.Done()
			tool.Installed, tool.Detail = checkPrerequisite(tool.Name)
		}(&tools[i])
	}
	wg.Wait()
	return tools
}

var prerequisiteVersionRe = regexp.MustCompile(`\b([0-9]+)\.([0-9]+)(?:\.([0-9]+))?`)

func versionAtLeast(version, minimum string) bool {
	parse := func(value string) [3]int {
		var parts [3]int
		for i, part := range strings.Split(value, ".") {
			if i < len(parts) {
				parts[i], _ = strconv.Atoi(part)
			}
		}
		return parts
	}
	actual, required := parse(version), parse(minimum)
	for i := range actual {
		if actual[i] != required[i] {
			return actual[i] > required[i]
		}
	}
	return true
}

// Bound local probes: a broken CLI or unreachable Docker daemon must not hang
// the platform picker. Do not expose raw CLI output (it may contain credentials).
func prerequisiteOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func checkPrerequisite(name string) (bool, string) {
	if name == "terraform-provider-chaos" {
		if !chaosProviderInstalled() {
			return false, "CHAOS provider is not installed for " + chaosProviderTarget()
		}
		return true, "found"
	}
	if name == "ansible-collections" {
		return checkAnsibleCollections()
	}
	if !toolInstalled(name) {
		return false, "not found on PATH"
	}
	var args []string
	var minimum string
	switch name {
	case "terraform":
		args, minimum = []string{"version", "-json"}, "1.9"
	case "ansible":
		if !toolInstalled("ansible-playbook") {
			return false, "ansible-playbook is not found on PATH"
		}
		args, minimum = []string{"--version"}, "2.14"
	case "python3":
		args, minimum = []string{"--version"}, "3.9"
	case "aws":
		args, minimum = []string{"--version"}, "2.0"
	case "docker":
		args, minimum = []string{"info", "--format", "{{.ServerVersion}}"}, "20.10"
	default:
		return true, "found"
	}
	output, err := prerequisiteOutput(name, args...)
	if err != nil {
		if name == "docker" {
			return false, "Docker daemon is unavailable; start Docker and check socket permissions"
		}
		return false, "version check failed or timed out"
	}
	version := ""
	if name == "terraform" {
		var info struct {
			Version string `json:"terraform_version"`
		}
		if json.Unmarshal(output, &info) == nil {
			version = info.Version
		}
	} else {
		// Only inspect the first line, not unrelated Python/plugin versions.
		version = prerequisiteVersionRe.FindString(strings.SplitN(string(output), "\n", 2)[0])
	}
	if !prerequisiteVersionRe.MatchString(version) || !versionAtLeast(version, minimum) {
		return false, "requires " + minimum + "+ (installed version is missing, unsupported, or too old)"
	}
	return true, version
}

// These minimums match the tested pins in ansible/requirements.yml. Newer
// collections bundled with a full Ansible installation are accepted as well.
var requiredAnsibleCollections = map[string]string{
	"ansible.posix": "1.6.2", "community.general": "9.5.5",
}

func checkAnsibleCollections() (bool, string) {
	output, err := prerequisiteOutput("ansible-galaxy", "collection", "list", "--format", "json")
	if err != nil {
		return false, "cannot list collections; install ansible-core and ansible/requirements.yml"
	}
	var installed map[string]map[string]struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(output, &installed) != nil {
		return false, "cannot parse ansible-galaxy collection list"
	}
	for name, minimum := range requiredAnsibleCollections {
		found := false
		for _, collections := range installed {
			version := collections[name].Version
			if prerequisiteVersionRe.MatchString(version) && versionAtLeast(version, minimum) {
				found = true
			}
		}
		if !found {
			return false, "requires " + name + " " + minimum + "+; install ansible/requirements.yml"
		}
	}
	return true, "required collections found"
}

// GET /api/prerequisites/{platform}
func apiPrerequisitesHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if !validPlatform(platform) {
		jsonError(w, 400, "unknown platform: "+platform)
		return
	}
	tools := platformPrereqs(platform)
	ok := true
	for _, t := range tools {
		if !t.Installed {
			ok = false
			break
		}
	}
	result := PrereqResult{
		Platform: platform,
		OK:       ok,
		Tools:    tools,
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, result)
}
