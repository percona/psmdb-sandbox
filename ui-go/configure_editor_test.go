package main

import (
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func editorFixture(platform string) ConfigureData {
	cfg := Config{
		Clusters: map[string]ClusterConfig{"cl01": {}},
		Replsets: map[string]ReplsetConfig{"rs01": {}},
	}
	return ConfigureData{
		Platform: platform, EnvID: "editortest", Config: cfg,
		DefaultImages: defaultImages, DefaultAuditFilter: defaultAuditFilter,
		DockerDefaultPmmExternalPort: 8443, DockerDefaultSeaweedFSPort: 8333, DockerDefaultSeaweedFSAdminPort: 9333,
		PSMDBVersions: []string{"psmdb-80"}, OfficialMongoDBVersions: []string{"8.0", "7.0"},
		PSMDBMinorVersions: map[string][]string{"psmdb-80": {"8.0.12-4"}},
		PSMDBImages:        []string{"8.0.12-4"}, PBMImages: []string{"2.11.0"},
		PBMVersions: []string{"2.11.0"},
		PMMImages:   []string{"3.10.0"}, PMMClientImages: []string{"3.10.0"},
		MongotImages: []string{"0.54.0"}, PCSMVersions: []string{"0.9.0"},
		SortedClusters: sortedClusters(cfg.Clusters), SortedReplsets: sortedReplsets(cfg.Replsets),
	}
}

func TestConfigureEditorMarkupAndScripts(t *testing.T) {
	node, _ := exec.LookPath("node")
	for _, platform := range []string{"docker", "aws", "gcp", "azure", "chaos"} {
		t.Run(platform, func(t *testing.T) {
			page := renderConfigure(t, editorFixture(platform))
			for _, want := range []string{
				`id="config-editor-navigation" hidden`, `id="config-editor-panels"`,
				`data-editor-group="basics"`, `data-editor-group="mongodb"`, `data-editor-group="security"`,
				`data-editor-group="profiles"`, `<h2 class="profiles-title">Templates `,
				`type="checkbox" id="enable-environment-profile"`, `id="environment-profile-fields" hidden`,
				`aria-label="Configuration template"`, `No starting point — configure manually`, `Environment Name`,
				`data-editor-service="monitoring"`, `data-editor-service="backups"`,
				`data-editor-service="load"`, `data-editor-service="sync"`,
				`id="editor-errors"`, `/static/configure-editor.js`,
				`Saves configuration only. Deploy from the environment page.`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("missing editor markup %q", want)
				}
			}
			if platform == "docker" && strings.Contains(page, `data-editor-group="resources"`) {
				t.Error("Docker must not show a cloud resource panel")
			}
			for _, unwanted := range []string{"Start a new configuration (optional)", "Standalone Replica Sets", "Templates &amp; reusable profiles", "Starting points are optional.", `id="apply-environment-starter"`, `id="environment-profile-to-delete"`, `id="delete-environment-profile"`} {
				if strings.Contains(page, unwanted) {
					t.Errorf("unexpected editor markup %q", unwanted)
				}
			}
			if node == "" {
				return
			}
			for _, script := range regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(page, -1) {
				cmd := exec.Command(node, "--check")
				cmd.Stdin = strings.NewReader(script[1])
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("invalid rendered script: %v\n%s", err, out)
				}
			}
		})
	}
}

func TestConfigureGCPImageDisplayName(t *testing.T) {
	data := editorFixture("gcp")
	data.Config.MachineImage = "https://www.googleapis.com/compute/v1/projects/ubuntu-os-cloud/global/images/ubuntu-2404-test"
	page := renderConfigure(t, data)
	want := `<option value="` + data.Config.MachineImage + `" selected>ubuntu-2404-test</option>`
	if !strings.Contains(page, want) {
		t.Error("GCP image selector must display only the image name while retaining the full value")
	}
}

func TestConfigureCustomizationPanelsCollapsedWithSavedValues(t *testing.T) {
	panels := regexp.MustCompile(`<div\b[^>]*(?:class="(?:customize-body|package-block-body|compute-block-body)"|id="(?:pmm-cloud-body|seaweedfs-details-body|credentials-body)")[^>]*>`)
	for _, platform := range []string{"docker", "aws", "gcp", "azure", "chaos"} {
		t.Run(platform, func(t *testing.T) {
			data := editorFixture(platform)
			data.Config.DefaultVpcName = "customnet"
			data.Config.UseSpotInstances = true
			data.Config.ShardsvrType = "custom-machine"
			data.Config.ShardsvrCpuCores = 8
			data.Config.ShardsvrVolumeSize = 250
			data.Config.ReplsetSvrCpuCores = 8
			data.Config.PmmImage = "percona/pmm-server:3.10.0"
			data.Config.PmmCpuCores = 4
			data.Config.SeaweedFSCpuCores = 4
			data.Config.EnablePlgm = true
			data.Config.PlgmType = "custom-machine"
			data.Config.ClusterSync = ClusterSyncConfig{Enabled: true, Version: "0.9.0", CPUs: 4}
			data.Config.Clusters["cl01"] = ClusterConfig{MongoRelease: "psmdb-80", MongoVersion: "8.0.12-4"}
			data.Config.Replsets["rs01"] = ReplsetConfig{MongoRelease: "psmdb-80", MongoVersion: "8.0.12-4"}
			data.SortedClusters = sortedClusters(data.Config.Clusters)
			data.SortedReplsets = sortedReplsets(data.Config.Replsets)
			page := renderConfigure(t, data)
			tags := panels.FindAllString(page, -1)
			if len(tags) == 0 {
				t.Fatal("no customization panels found")
			}
			for _, tag := range tags {
				if !strings.Contains(tag, `style="display:none"`) {
					t.Errorf("customization panel must start collapsed: %s", tag)
				}
			}
			if strings.Contains(page, "▲ Collapse</button>") {
				t.Error("initial toggle labels must match collapsed panels")
			}
			if !strings.Contains(page, `value="8.0.12-4"`) {
				t.Error("collapsed settings must retain saved version values")
			}
		})
	}
}

func TestConfigureEditorTopologySummary(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	cmd := exec.Command(node, "-e", `
const assert = require('node:assert/strict');
const {topologySummary} = require('./static/configure-editor.js');
let result = topologySummary({kind: 'replset', members: '3', arbiters: '0', tls: true});
assert.equal(result.nodes, 3);
assert.equal(result.description, '3 data nodes · 0 arbiters');
assert.equal(result.features, 'Monitoring off · Backups off · TLS on');
result = topologySummary({kind: 'cluster', shards: '2', members: '3', arbiters: '0', configs: '3', routers: '2', search: true, monitoring: true});
assert.equal(result.nodes, 11);
assert.match(result.description, /2 shards × 3 data nodes \+ 0 arbiters/);
assert.match(result.features, /Monitoring on.*Search on/);
assert.equal(topologySummary({kind: 'cluster'}).nodes, 11);
assert.equal(topologySummary({kind: 'replset', members: '', arbiters: ''}).nodes, 3);
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("topology summary: %v\n%s", err, out)
	}
}

func renderProfileSettings(t *testing.T) string {
	t.Helper()
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(uiAssets, "templates/layout.html", "templates/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "layout", IndexData{}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestSettingsProfileManagementMarkupAndScripts(t *testing.T) {
	page := renderProfileSettings(t)
	for _, want := range []string{`data-tab="profiles"`, `data-panel="profiles"`, `id="environment-profile-to-delete" disabled`, `id="delete-environment-profile" disabled`, `id="settings-profile-status" role="status"`} {
		if !strings.Contains(page, want) {
			t.Errorf("missing profile settings markup %q", want)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return
	}
	for _, script := range regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(page, -1) {
		cmd := exec.Command(node, "--check")
		cmd.Stdin = strings.NewReader(script[1])
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("invalid settings script: %v\n%s", err, out)
		}
	}
}

// Optional real-browser coverage, without adding a production dependency. The
// browser test intercepts all requests; it cannot save or deploy environments.
func TestConfigureEditorBrowser(t *testing.T) {
	if os.Getenv("UI_BROWSER_TESTS") != "1" {
		t.Skip("set UI_BROWSER_TESTS=1 with Playwright available through NODE_PATH")
	}
	fixtures := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixtures, "index.html"), []byte(renderProfileSettings(t)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"docker", "aws", "gcp", "azure", "chaos"} {
		page := renderConfigure(t, editorFixture(platform))
		if err := os.WriteFile(filepath.Join(fixtures, platform+".html"), []byte(page), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("node", "tests/configure-editor.browser.cjs", fixtures)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser tests: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
