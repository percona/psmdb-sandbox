package main

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesParse(t *testing.T) {
	pages := []string{"configure", "environment", "index", "new_environment"}
	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			_, err := template.New("").Funcs(funcMap).ParseFS(
				uiAssets,
				"templates/layout.html",
				"templates/"+page+".html",
			)
			if err != nil {
				t.Fatalf("parse %s template: %v", page, err)
			}
		})
	}
}

func TestEnvironmentPageHasNoLoadGeneratorControls(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "environment.html"))
	if err != nil {
		t.Fatalf("read environment template: %v", err)
	}
	template := strings.ToLower(string(content))
	// Workload configuration lives in the plgm Web UI, linked from the
	// service URL row; the sandbox page must not carry its own load controls.
	for _, unwanted := range []string{"ycsb", "plgm-panel", "/plgm/"} {
		if strings.Contains(template, unwanted) {
			t.Fatalf("environment template unexpectedly contains %q", unwanted)
		}
	}
	if !strings.Contains(template, "startenvstatuspolling();") {
		t.Fatal("environment status polling is missing")
	}
}

func TestClusterSyncPanelRequiresSuccessfulDeployment(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "environment.html"))
	if err != nil {
		t.Fatalf("read environment template: %v", err)
	}
	if !strings.Contains(string(content), `{{if .Env.Config.ClusterSync.Enabled}}`) || !strings.Contains(string(content), `{{if not .ClusterSyncAvailable}}style="display:none"{{end}}`) {
		t.Fatal("ClusterSync panel must be hidden until a successful environment deployment")
	}
}

func TestClusterSyncPanelRefreshesAfterDeployment(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "environment.html"))
	if err != nil {
		t.Fatalf("read environment template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		"async function refreshClusterSyncPanel()",
		"cluster_sync_available",
		"startClusterSyncStatusPolling()",
		"loadHosts();\n\t\trefreshClusterSyncPanel();",
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("ClusterSync post-deploy refresh is missing %q", want)
		}
	}
}

func TestClusterSyncActionsUseAPIFetchMethodContract(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "environment.html"))
	if err != nil {
		t.Fatalf("read environment template: %v", err)
	}
	if !strings.Contains(string(content), "clustersync/${action}`, 'POST', {from_failure: fromFailure}") {
		t.Fatal("ClusterSync actions must pass POST and body separately to apiFetch")
	}
}

func TestClusterSyncButtonsTrackPCSMState(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "environment.html"))
	if err != nil {
		t.Fatalf("read environment template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		`id="pcsm-start"`,
		`id="pcsm-pause"`,
		`id="pcsm-resume"`,
		`id="pcsm-resume-failure"`,
		`id="pcsm-finalize"`,
		`id="pcsm-reset"`,
		"let currentClusterSyncStatus = null",
		"function setClusterSyncButtonState(status)",
		"const paused = state === 'paused' || state.endsWith('_paused')",
		"const failed = state === 'failed' || state.endsWith('_failed') || state.includes('failure')",
		"(state === 'running' && status.initialSync?.completed === true)",
		"const running = state === 'running' ||",
		"buttons.resumeFailure.disabled = !failed",
		"buttons.finalize.disabled = !finalizable",
		"buttons.reset.disabled = running || finalizable",
	} {
		if !strings.Contains(template, want) {
			t.Errorf("ClusterSync state gating is missing %q", want)
		}
	}
}

func TestClusterSyncConfigurationWarnsAndExcludesSourceFromTarget(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		`id="pcsm-topology-warning"`,
		`Add at least two clusters or two replica sets`,
		`option.kind === source.kind && option.value !== source.value`,
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("ClusterSync selector is missing %q", want)
		}
	}
}

func TestClusterSyncTuningDisplaysEffectiveDefaults(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		`data-pcsm-default="parallel"`,
		`value="{{intDefault .Config.ClusterSync.CloneParallelCollections 2}}"`,
		`read: Math.max(Math.floor(cpus / 4), 1)`,
		`insert: cpus * 2`,
		`replication: cpus`,
		`batch: 10000`,
		`'event-queue': 5000`,
		`'worker-queue': 5000`,
		`bulk: 5000`,
		`input.dataset.usingDefault === 'true'`,
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("ClusterSync effective defaults are missing %q", want)
		}
	}
}

func TestClusterSyncVersionUsesPrefetchedDropdown(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		`<select id="pcsm_version" name="pcsm_version"`,
		`range .PCSMVersions`,
		`let PCSM_VERSIONS =`,
		`product: 'pcsm'`,
		`setPCSMVersionOptions(PCSM_VERSIONS)`,
		`id="pcsm_repo"`,
		`new URLSearchParams({product: 'pcsm', channel})`,
		`selectedPCSMOsImage() !== osImage`,
		`!== source`,
		`!== channel`,
		`Package Overrides`,
		`Compute Resources`,
		`class="package-block-body"`,
		`class="compute-block-body"`,
	} {
		if !strings.Contains(template, want) {
			t.Errorf("ClusterSync version selector is missing %q", want)
		}
	}
}

func TestClusterSyncCustomizeSectionsFollowOptions(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	template := string(content)
	start := strings.Index(template, `<section class="form-section">`)
	clusterSync := strings.Index(template[start:], `<h2>Percona ClusterSync</h2>`)
	if clusterSync < 0 {
		t.Fatal("ClusterSync section is missing")
	}
	clusterSyncStart := start + clusterSync
	end := strings.Index(template[clusterSyncStart:], `</section>`)
	if end < 0 {
		t.Fatal("ClusterSync section is not closed")
	}
	section := template[clusterSyncStart : clusterSyncStart+end]
	options := strings.Index(section, `name="pcsm_include_namespaces"`)
	packageOverrides := strings.Index(section, `Package Overrides`)
	computeResources := strings.Index(section, `Compute Resources`)
	if options < 0 || packageOverrides < options || computeResources < packageOverrides {
		t.Fatal("ClusterSync customize sections must follow the PCSM options")
	}
}

func TestPCSMPlaybookUsesDedicatedInventoryHosts(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "ansible", "pcsm.yml"))
	if err != nil {
		t.Fatalf("read PCSM playbook: %v", err)
	}
	playbook := string(content)
	for _, want := range []string{
		"hosts: pcsm-source",
		"hosts: pcsm-target",
		"| first) | default('')",
	} {
		if !strings.Contains(playbook, want) {
			t.Fatalf("PCSM playbook is missing %q", want)
		}
	}
	for _, unwanted := range []string{"pcsm_sync_source", "pcsm_sync_target", "hosts: pcsm_source", "hosts: pcsm_target"} {
		if strings.Contains(playbook, unwanted) {
			t.Fatalf("PCSM playbook references obsolete inventory name %q", unwanted)
		}
	}
}

func TestMongoDBDefaultReleaseUsesPSMDB80(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		`{{$defaultRel := preferredPSMDBRelease $.PSMDBVersions}}`,
		`const defaultMongoRelease = PSMDB_VERSIONS.includes('psmdb-80') ? 'psmdb-80' : PSMDB_VERSIONS[0];`,
		`PSMDB 8.3 and newer lines must be selected explicitly.`,
	} {
		if !strings.Contains(template, want) {
			t.Fatalf("MongoDB GA release default is missing %q", want)
		}
	}
	if got := funcMap["preferredPSMDBRelease"].(func([]string) string)([]string{"psmdb-83", "psmdb-80", "psmdb-70"}); got != "psmdb-80" {
		t.Fatalf("preferred PSMDB release = %q, want psmdb-80", got)
	}
}

func TestDockerMongotImageSelectorSupportsCustomRepository(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	template := string(content)
	for _, want := range []string{
		`{{$mongotNs := imageNamespace $curMongot "percona"}}{{$mongotRepo := imageRepository $curMongot "percona-search-mongodb"}}`,
		`class="docker-image-repository" value="{{$mongotRepo}}" placeholder="repository" aria-label="mongot image repository" {{if not (mongotNamespaceCustom $mongotNs)}}style="display:none"{{end}}`,
		`onDockerMongotNamespaceChange`,
		`/api/docker-tags?namespace=${encodeURIComponent(namespace)}&repo=${encodeURIComponent(repo)}`,
	} {
		if !strings.Contains(template, want) {
			t.Errorf("configure template missing %q", want)
		}
	}
}

func TestCloudTLSConfigurationFields(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "configure.html"))
	if err != nil {
		t.Fatalf("read configure template: %v", err)
	}
	page := string(content)
	for _, want := range []string{
		`name="cluster_use_tls[]"`,
		`name="replset_use_tls[]"`,
		`name="ca_placement"`,
		`name="enable_ca"`,
		`id="ca-settings-body"`,
		`config.enable_ca = fd.has('enable_ca')`,
		`config.use_tls = [...Object.values(config.clusters), ...Object.values(config.replsets)].some(item => item.use_tls)`,
		`config.ca_placement = fd.get('ca_placement') || 'dedicated'`,
		`<h2>Certificate Authority</h2>`,
		`['aws', 'gcp', 'azure', 'chaos'].includes(PLATFORM)`,
		`name="ca_cpu_cores"`,
		`name="ca_memory_gb"`,
		`name="ca_volume_size"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("configure template missing %q", want)
		}
	}
}

func TestMongoTemplatesPreferTLS(t *testing.T) {
	files := []string{
		"mongod-replicaset.conf.j2",
		"mongod-sharding.conf.j2",
		"mongod-cfgserver.conf.j2",
		"mongos.conf.j2",
	}
	for _, name := range files {
		content, err := os.ReadFile(filepath.Join("..", "ansible", "templates", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(content), "mode: preferTLS") {
			t.Errorf("%s does not configure preferTLS", name)
		}
		if strings.Contains(string(content), "mode: requireTLS") {
			t.Errorf("%s still configures requireTLS", name)
		}
	}
}

func TestCloudInventoriesUseCanonicalTLSVariable(t *testing.T) {
	sections := []string{
		"# Ansible connection",
		"# Deployment",
		"# MongoDB",
		"# TLS",
		"# Audit",
		"# LDAP",
		"# MongoDB Search",
		"# PMM",
		"# PBM",
	}
	for _, platform := range []string{"aws", "gcp", "azure", "chaos"} {
		for _, topology := range []string{"cluster", "replset"} {
			name := platform + "/" + topology
			t.Run(name, func(t *testing.T) {
				path := filepath.Join("..", "terraform", platform, topology+"_inventory.tmpl")
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				template := string(content)
				if strings.Count(template, "use_tls=${use_tls}") != 1 {
					t.Errorf("%s must emit exactly one canonical use_tls variable", path)
				}
				if strings.Contains(template, "enable_tls") {
					t.Errorf("%s still emits legacy enable_tls", path)
				}
				previousSection := -1
				for _, section := range sections {
					if strings.Count(template, section+"\n") != 1 {
						t.Errorf("%s must contain section %q exactly once", path, section)
					}
					sectionOffset := strings.Index(template, section+"\n")
					if sectionOffset <= previousSection {
						t.Errorf("%s has section %q out of order", path, section)
					}
					previousSection = sectionOffset
				}
				for _, deadVariable := range []string{"ca_placement=", "pbm_release="} {
					if strings.Contains(template, deadVariable) {
						t.Errorf("%s still emits dead variable %s", path, deadVariable)
					}
				}
				if strings.Count(template, "enable_pmm=${enable_pmm}") != 1 {
					t.Errorf("%s must emit enable_pmm exactly once", path)
				}
			})
		}
	}
}

func TestPCSMUsesDedicatedInventory(t *testing.T) {
	main, err := os.ReadFile(filepath.Join("..", "ansible", "main.yml"))
	if err != nil {
		t.Fatalf("read main.yml: %v", err)
	}
	if strings.Contains(string(main), "pcsm.yml") {
		t.Error("main.yml must not configure PCSM from topology inventories")
	}

	pcsm, err := os.ReadFile(filepath.Join("..", "ansible", "pcsm.yml"))
	if err != nil {
		t.Fatalf("read pcsm.yml: %v", err)
	}
	for _, want := range []string{"hosts: pcsm-source", "hosts: pcsm-target", "hosts: pcsm"} {
		if !strings.Contains(string(pcsm), want) {
			t.Errorf("pcsm.yml missing %q", want)
		}
	}
	for _, unwanted := range []string{"pcsm_sync_source", "pcsm_sync_target"} {
		if strings.Contains(string(pcsm), unwanted) {
			t.Errorf("pcsm.yml still references %q", unwanted)
		}
	}

	for _, platform := range []string{"aws", "gcp", "azure", "chaos"} {
		path := filepath.Join("..", "terraform", platform, "pcsm_inventory.tmpl")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		inventory := string(content)
		for _, want := range []string{"pcsm-source ansible_host=${source.ip}", "pcsm-target ansible_host=${target.ip}", "[pcsm]", "pcsm_use_tls=${source.use_tls || target.use_tls}", "pcsm_ca_staging_file="} {
			if !strings.Contains(inventory, want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
		for _, unwanted := range []string{"[pcsm_source]", "[pcsm_target]", "[replsets:children]"} {
			if strings.Contains(inventory, unwanted) {
				t.Errorf("%s still contains %q", path, unwanted)
			}
		}
	}
}

func TestCloudReadmesDocumentManualPCSMFlow(t *testing.T) {
	for _, platform := range []string{"aws", "gcp", "azure", "chaos"} {
		path := filepath.Join("..", "terraform", platform, "README.md")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		readme := string(content)
		for _, want := range []string{
			"ansible-playbook -i myenv_inventory_rs-source ../../ansible/main.yml",
			"ansible-playbook -i myenv_inventory_pcsm ../../ansible/pcsm.yml",
			"PCSM_SOURCE_URI=",
			"PCSM_TARGET_URI=",
			"PCSM_SOURCE_PASSWORD=",
			"PCSM_TARGET_PASSWORD=",
			"enable_pcsm      = true",
			"pcsm_source_name = \"rs-source\"",
			"pcsm_target_name = \"rs-target\"",
		} {
			if !strings.Contains(readme, want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
		if strings.Contains(readme, "[pcsm_source]") || strings.Contains(readme, "[pcsm_target]") {
			t.Errorf("%s documents obsolete PCSM inventory groups", path)
		}
	}
}

func TestMongoShellUsesDedicatedClientCertificate(t *testing.T) {
	playbooks := []string{
		"add_replset_member.yml",
		"add_shard.yml",
		"mongod_install.yml",
		"mongos_install.yml",
		"sharding_setup.yml",
	}
	for _, name := range playbooks {
		content, err := os.ReadFile(filepath.Join("..", "ansible", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(content), "clientCertificateKeyFile") {
			t.Errorf("%s does not use the dedicated client certificate", name)
		}
	}

	certSetup, err := os.ReadFile(filepath.Join("..", "ansible", "cert_setup.yml"))
	if err != nil {
		t.Fatalf("read cert_setup.yml: %v", err)
	}
	for _, want := range []string{
		"openssl-test-client.cnf.j2",
		`dest: "{{ clientCertificateKeyFile }}"`,
		`owner: "{{ ansible_user }}"`,
		`mode: "0600"`,
	} {
		if !strings.Contains(string(certSetup), want) {
			t.Errorf("cert_setup.yml missing %q", want)
		}
	}
}

func TestPBMUsesDedicatedClientCertificate(t *testing.T) {
	files := []string{
		"pbm_install.yml",
		"restart.yml",
		filepath.Join("templates", "pbm-agent.j2"),
	}
	for _, name := range files {
		content, err := os.ReadFile(filepath.Join("..", "ansible", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(content)
		if !strings.Contains(text, "tlsCertificateKeyFile={{ clientCertificateKeyFile }}") {
			t.Errorf("%s does not configure PBM with the client certificate", name)
		}
		if strings.Contains(text, "tlsCertificateKeyFile={{ certificateKeyFile }}") {
			t.Errorf("%s still configures PBM with the server certificate", name)
		}
	}

	pbmInstall, err := os.ReadFile(filepath.Join("..", "ansible", "pbm_install.yml"))
	if err != nil {
		t.Fatalf("read pbm_install.yml: %v", err)
	}
	for _, want := range []string{
		"Grant PBM access to the client TLS certificate",
		`group: "{{ pbm_os_user }}"`,
		`mode: "0640"`,
	} {
		if !strings.Contains(string(pbmInstall), want) {
			t.Errorf("pbm_install.yml missing %q", want)
		}
	}
}

func TestCloudInventoryUsesCanonicalAnsibleUser(t *testing.T) {
	for _, platform := range []string{"aws", "azure", "chaos", "gcp"} {
		for _, name := range []string{"cluster_inventory.tmpl", "replset_inventory.tmpl"} {
			path := filepath.Join("..", "terraform", platform, name)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			inventory := string(content)
			if !strings.Contains(inventory, "ansible_user=${my_ssh_user}") {
				t.Errorf("%s does not emit ansible_user", path)
			}
			if strings.Contains(inventory, "ansible_ssh_user") {
				t.Errorf("%s still emits legacy ansible_ssh_user", path)
			}
		}
	}
}
