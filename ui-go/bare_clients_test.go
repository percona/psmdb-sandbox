package main

import (
	"html/template"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestWriteTfvarsWritesPmmBareClients(t *testing.T) {
	origTerraformDir := terraformDir
	terraformDir = t.TempDir()
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{PmmBareClients: map[string]PmmBareClientConfig{
		"client02": {BaseImage: "rockylinux:9", PmmClientRepo: "testing", PmmClientVersion: "3.10.0", OsImage: "Rocky Linux 9"},
		"client01": {BaseImage: "ubuntu:24.04"},
	}}
	for _, tc := range []struct {
		platform  string
		want      []string
		unwanted  []string
		wantOrder []string
	}{
		{
			platform:  "docker",
			want:      []string{`    base_image = "rockylinux:9"`, `    pmm_client_repo = "testing"`, `    pmm_client_version = "3.10.0"`},
			unwanted:  []string{"os_image"},
			wantOrder: []string{`"client01" = {`, `"client02" = {`},
		},
		{
			platform:  "chaos",
			want:      []string{`    os_image = "Rocky Linux 9"`, `    pmm_client_repo = "testing"`, `    pmm_client_version = "3.10.0"`},
			unwanted:  []string{"base_image"},
			wantOrder: []string{`"client01" = {`, `"client02" = {`},
		},
		{platform: "aws", want: []string{`    pmm_client_repo = "testing"`, `    pmm_client_version = "3.10.0"`}, unwanted: []string{"base_image", "os_image"}, wantOrder: []string{`"client01" = {`, `"client02" = {`}},
		{platform: "gcp", want: []string{`    pmm_client_repo = "testing"`, `    pmm_client_version = "3.10.0"`}, unwanted: []string{"base_image", "os_image"}, wantOrder: []string{`"client01" = {`, `"client02" = {`}},
		{platform: "azure", want: []string{`    pmm_client_repo = "testing"`, `    pmm_client_version = "3.10.0"`}, unwanted: []string{"base_image", "os_image"}, wantOrder: []string{`"client01" = {`, `"client02" = {`}},
	} {
		if err := writeTfvars("bare", tc.platform, cfg); err != nil {
			t.Fatalf("%s: writeTfvars: %v", tc.platform, err)
		}
		data, err := os.ReadFile(tfvarsPath("bare", tc.platform))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s: expected %q in tfvars:\n%s", tc.platform, want, text)
			}
		}
		for _, unwanted := range tc.unwanted {
			if strings.Contains(text, unwanted) {
				t.Errorf("%s: unexpected %q in tfvars:\n%s", tc.platform, unwanted, text)
			}
		}
		if strings.Index(text, tc.wantOrder[0]) > strings.Index(text, tc.wantOrder[1]) {
			t.Errorf("%s: clients are not sorted by name:\n%s", tc.platform, text)
		}
	}
}

func TestWriteTfvarsDockerWritesEmptyPmmBareClients(t *testing.T) {
	origTerraformDir := terraformDir
	terraformDir = t.TempDir()
	t.Cleanup(func() { terraformDir = origTerraformDir })

	if err := writeTfvars("nobare", "docker", Config{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tfvarsPath("nobare", "docker"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "pmm_bare_clients = {}") {
		t.Fatalf("expected an empty pmm_bare_clients map:\n%s", data)
	}
}

func TestConfigureTemplateRendersPmmClientsAndExtensions(t *testing.T) {
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(uiAssets, "templates/layout.html", "templates/configure.html")
	if err != nil {
		t.Fatal(err)
	}
	bare := map[string]PmmBareClientConfig{"client01": {BaseImage: "rockylinux:9", PmmClientRepo: "testing", OsImage: "Rocky Linux 9"}}
	servers := map[string]PmmServerConfig{"pmm-server": {EnableExtensions: true}}
	for _, platform := range []string{"docker", "chaos", "aws", "gcp", "azure"} {
		cfg := Config{PmmServers: servers, PmmBareClients: bare, EnableExtensions: true}
		data := ConfigureData{
			Platform:          platform,
			Config:            cfg,
			DefaultImages:     defaultImages,
			SortedPmmServers:  sortedPmmServers(cfg.PmmServers),
			SortedBareClients: sortedPmmBareClients(cfg.PmmBareClients),
		}
		var out strings.Builder
		if err := tmpl.ExecuteTemplate(io.MultiWriter(&out), "layout", data); err != nil {
			t.Fatalf("%s: execute: %v", platform, err)
		}
		for _, want := range []string{`SAVED_BARE_CLIENTS = [{"Name":"client01","Config":{"base_image":"rockylinux:9"`, `id="bare-clients-container"`} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: expected %q in the rendered page", platform, want)
			}
		}
	}
}

func TestCloudInventoryNamesAddsPmmClients(t *testing.T) {
	cfg := Config{
		Clusters: map[string]ClusterConfig{"cl01": {}},
		Replsets: map[string]ReplsetConfig{"rs01": {}},
	}
	if got, want := cloudInventoryNames(cfg), []string{"cl01", "rs01"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cloudInventoryNames = %v, want %v", got, want)
	}
	cfg.PmmBareClients = map[string]PmmBareClientConfig{"client01": {}}
	if got, want := cloudInventoryNames(cfg), []string{"cl01", "pmm_clients", "rs01"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cloudInventoryNames = %v, want %v", got, want)
	}
}

func TestParseInventoryHostsListsPmmClients(t *testing.T) {
	inv := "[pmm]\nt-pmm-server ansible_host=10.30.0.2\n\n[pmm_bare_client]\nt-client01 ansible_host=10.30.0.3\n\n[all:vars]\nansible_user=u\n"
	roles := map[string]string{}
	groups := map[string]string{}
	for _, h := range parseInventoryHosts(inv, pmmClientsInventory, "u", "") {
		roles[h.Name], groups[h.Name] = h.Role, h.Group
	}
	if roles["t-pmm-server"] != "pmm" || roles["t-client01"] != "pmm-client" || groups["t-client01"] != "PMM Clients" {
		t.Fatalf("unexpected roles %v and groups %v", roles, groups)
	}
}

func TestCloudPmmClientTemplates(t *testing.T) {
	terraform, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("Terraform is not installed")
	}
	for _, tc := range []struct {
		file, inputs string
		want         []string
	}{
		{
			file: "inventory.tmpl",
			inputs: `{
hostname_pmm = "test-pmm-server", ip_pmm = "192.0.2.1",
pmm_bare_clients = { "test-client01" = { ip = "192.0.2.2", pmm_client_repo = "pmm3-client testing", pmm_client_version = "3.10.0" } },
my_ssh_user = "tester", ssh_private_key_path = "/fixture/id_key", pmm_image = "percona/pmm-server:3.10.0",
pmm_client_version = "", pmm_client_repo = "pmm3-client"
}`,
			want: []string{"[pmm_bare_client]", `test-client01 ansible_host=192.0.2.2 pmm_client_repo="pmm3-client testing" pmm_client_version=3.10.0`, "enable_pmm=true", "ansible_user=tester"},
		},
		{
			file: "ssh_config.tmpl",
			inputs: `{
include_pmm = true, hostname_pmm = "test-pmm-server", public_ip_pmm = "192.0.2.1",
pmm_bare_clients = { "test-client01" = "192.0.2.2" }, my_ssh_user = "tester", ssh_private_key_path = "/fixture/id_key",
enable_ssh_gateway = true, ssh_gateway_name = "gateway", port_to_forward = "8443", pmm_port = "443"
}`,
			want: []string{"Host test-pmm-server", "Host test-client01", "Hostname 192.0.2.2", "IdentityFile /fixture/id_key", "LocalForward 8443 127.0.0.1:443", "ProxyCommand ssh -W %h:%p gateway"},
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			path, err := filepath.Abs(filepath.Join("..", "terraform", "modules", "pmm_clients", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(terraform, "console", "-no-color")
			// A fresh directory avoids reading any environment state or credentials.
			cmd.Dir = t.TempDir()
			cmd.Stdin = strings.NewReader("jsonencode(templatefile(" + strconv.Quote(path) + ", " + strings.ReplaceAll(tc.inputs, "\n", " ") + "))\n")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("render template: %v\n%s", err, out)
			}
			encoded, err := strconv.Unquote(strings.TrimSpace(string(out)))
			if err != nil {
				t.Fatal(err)
			}
			text, err := strconv.Unquote(encoded)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in rendered template:\n%s", want, text)
				}
			}
		})
	}
}
