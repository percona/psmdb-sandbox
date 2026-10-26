package main

import (
	"strings"
	"testing"
)

// The configure page wires option dependencies (PMM agent → PMM server,
// PBM agent → SeaweedFS (Docker) or the global backup toggle, TLS → CA,
// CA on PMM → PMM server) through one registry. Every element the registry
// looks up must exist on each platform, otherwise a rule silently stops working.
func TestConfigureDependencyProvidersExist(t *testing.T) {
	cfg := Config{
		Clusters: map[string]ClusterConfig{"cl01": {}},
		Replsets: map[string]ReplsetConfig{"rs01": {}},
	}
	want := map[string][]string{
		"docker": {`id="enable-pmm-docker"`, `id="enable-seaweedfs-docker"`, `id="bare-clients-container"`, `id="vault-psmdb-warning"`},
		"chaos":  {`id="enable_pmm"`, `id="enable_backup"`, `id="enable_ca"`, `id="ca_placement"`, `id="bare-clients-container"`, `id="vault-psmdb-warning"`},
		"aws":    {`id="enable_pmm"`, `id="enable_backup"`, `id="enable_ca"`, `id="ca_placement"`, `id="vault-psmdb-warning"`},
		"gcp":    {`id="enable_pmm"`, `id="enable_backup"`, `id="enable_ca"`, `id="ca_placement"`, `id="vault-psmdb-warning"`},
		"azure":  {`id="enable_pmm"`, `id="enable_backup"`, `id="enable_ca"`, `id="ca_placement"`, `id="vault-psmdb-warning"`},
	}
	for platform, ids := range want {
		page := renderConfigure(t, ConfigureData{
			Platform:       platform,
			Config:         cfg,
			DefaultImages:  defaultImages,
			SortedClusters: sortedClusters(cfg.Clusters),
			SortedReplsets: sortedReplsets(cfg.Replsets),
		})
		for _, id := range ids {
			if !strings.Contains(page, id) {
				t.Errorf("%s: dependency element %s is missing", platform, id)
			}
		}
		for _, wantJS := range []string{
			"function dependencyRules()",
			"function refreshOptionConflicts()",
			"normalizeDependencies();",
			"configForm.addEventListener('change', e => onDependencyChange(e.target));",
		} {
			if !strings.Contains(page, wantJS) {
				t.Errorf("%s: expected %q in the rendered page", platform, wantJS)
			}
		}
		// The old one-off cascades were replaced by the registry.
		for _, unwanted := range []string{"syncDockerPmmDependencies", "syncDockerSeaweedFSDependencies", "ensureDockerPmmServer", ".bare-client-row').remove()"} {
			if strings.Contains(page, unwanted) {
				t.Errorf("%s: rendered page still contains %q", platform, unwanted)
			}
		}
	}
}

// Every provider id referenced by the JavaScript registry must be one of the
// ids checked above, so renaming a checkbox fails this test.
func TestConfigureDependencyRegistryReferencesKnownIDs(t *testing.T) {
	page := renderConfigure(t, ConfigureData{Platform: "docker", DefaultImages: defaultImages})
	start := strings.Index(page, "function dependencyRules()")
	end := strings.Index(page, "let dependencyBusy")
	if start < 0 || end < start {
		t.Fatal("dependencyRules() not found")
	}
	registry := page[start:end]
	for _, id := range []string{"enable-pmm-docker", "enable_pmm", "enable-seaweedfs-docker", "enable_backup", "enable_ca"} {
		if !strings.Contains(registry, "'"+id+"'") {
			t.Errorf("registry no longer references %q; update the dependency tests", id)
		}
	}
}
