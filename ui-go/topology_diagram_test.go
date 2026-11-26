package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentTopologyDiagramDocker(t *testing.T) {
	on := true
	cfg := Config{
		EnableCA: &on, EnablePlgm: true, VaultEncryption: true,
		Clusters:         map[string]ClusterConfig{"cl01": {ShardCount: 3, EnablePmm: &on, EnablePbm: &on, EnableToolkit: &on, PsmdbImage: "percona/percona-server-mongodb:8.0.12-4"}},
		Replsets:         map[string]ReplsetConfig{"rs02": {}, "rs01": {DataNodesPerReplset: 3, UseTLS: &on}},
		PmmServers:       map[string]PmmServerConfig{"pmm": {}},
		SeaweedFSServers: map[string]SeaweedFSServerConfig{"s3": {}},
		PmmBareClients:   map[string]PmmBareClientConfig{"c1": {}, "c2": {}},
		ClusterSync:      ClusterSyncConfig{Enabled: true, SourceKind: "replset", SourceName: "rs01", TargetKind: "replset", TargetName: "rs02"},
	}
	got := environmentTopologyDiagram("docker", cfg)
	cluster := got.Topologies[0]
	if cluster.Ref != "cluster:0" || cluster.Shards != 3 || cluster.Members != 2 || cluster.Arbiters != 1 || cluster.Configs != 3 || cluster.Routers != 2 {
		t.Fatalf("cluster defaults = %+v", cluster)
	}
	if !cluster.Monitoring || !cluster.Backups || cluster.Toolkit || cluster.Version != "percona/percona-server-mongodb:8.0.12-4" {
		t.Fatalf("cluster features = %+v", cluster)
	}
	// Replica sets follow the editor's sorted order.
	if got.Topologies[1].Name != "rs01" || got.Topologies[1].Ref != "replset:0" || !got.Topologies[1].TLS || got.Topologies[1].Members != 3 {
		t.Fatalf("first replica set = %+v", got.Topologies[1])
	}
	if got.Topologies[2].Version != "PSMDB (default image)" {
		t.Fatalf("default image label = %q", got.Topologies[2].Version)
	}
	want := TopologyDiagramServices{Monitoring: true, Backups: true, Load: true, Sync: true, SyncSource: "replset:rs01", SyncTarget: "replset:rs02", Clients: 2, Vault: true, CA: true, CAPlacement: "dedicated"}
	if got.Services != want {
		t.Fatalf("services = %+v, want %+v", got.Services, want)
	}
}

func TestEnvironmentTopologyDiagramCloud(t *testing.T) {
	on, off := true, false
	cfg := Config{
		MongoDBDistribution: "community", MongoRelease: "psmdb-80", EnablePmm: &on, CreateBackupBucket: &off, ExistingBucketName: "b",
		CAPlacement: "pmm", EnableCA: &on,
		Replsets: map[string]ReplsetConfig{
			"rs01": {EnableToolkit: &on},
			"rs02": {MongoDBDistribution: "psmdb", MongoRelease: "psmdb-83", MongoVersion: "8.3.17"},
		},
		LdapServers: map[string]LdapServerConfig{"ldap": {}},
	}
	got := environmentTopologyDiagram("aws", cfg)
	if r := got.Topologies[0]; r.Version != "Community 8.0" || r.Distribution != "community" || !r.Toolkit {
		t.Fatalf("inherited replica set = %+v", r)
	}
	if r := got.Topologies[1]; r.Version != "PSMDB 8.3.17" || r.Distribution != "psmdb" {
		t.Fatalf("overridden replica set = %+v", r)
	}
	s := got.Services
	if !s.Monitoring || !s.Backups || s.BackupMode != "existing" || !s.LDAP || s.CAPlacement != "pmm" {
		t.Fatalf("services = %+v", s)
	}

	chaos := environmentTopologyDiagram("chaos", Config{EnableBackup: &on})
	if !chaos.Services.Backups || chaos.Services.BackupMode != chaosBackupSeaweedFS {
		t.Fatalf("CHAOS backups = %+v", chaos.Services)
	}
	if chaos := environmentTopologyDiagram("chaos", Config{EnableBackup: &off}); chaos.Services.Backups {
		t.Fatal("CHAOS backups must follow the global toggle")
	}
}

// The Go state must produce the same graph the editor would draw.
func TestEnvironmentTopologyDiagramRendersInJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	on := true
	state, err := json.Marshal(environmentTopologyDiagram("docker", Config{
		Replsets:    map[string]ReplsetConfig{"a": {}, "b": {EnablePmm: &on}},
		PmmServers:  map[string]PmmServerConfig{"pmm": {}},
		ClusterSync: ClusterSyncConfig{Enabled: true, SourceKind: "replset", SourceName: "a", TargetKind: "replset", TargetName: "b"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", `
const assert = require('node:assert/strict');
const {buildTopologyGraph, renderTopologySVG} = require('./static/topology-diagram.js');
const g = buildTopologyGraph(JSON.parse(process.argv[1]));
const edges = g.edges.filter(e => !e.quiet).map(e => e.from + '>' + e.to + ':' + e.label);
assert.deepEqual(edges, ['replset:1>svc:monitoring:metrics', 'replset:0>svc:sync:clone + change stream', 'svc:sync>replset:1:apply writes']);
assert.match(renderTopologySVG(g), /data-ref="replset:1"/);
`, string(state))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("environment diagram graph: %v\n%s", err, out)
	}
}

func TestEnvironmentPageMountsTopologyDiagram(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("templates", "environment.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(content)
	for _, want := range []string{
		`<div id="environment-topology-diagram"></div>`,
		`<script src="/static/topology-diagram.js"></script>`,
		`diagram?.update({{json .TopologyDiagram}});`,
		"#select=${encodeURIComponent(ref)}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("environment template missing %q", want)
		}
	}
	editor, err := os.ReadFile(filepath.Join("static", "configure-editor.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(editor), `location.hash.match(/^#select=(.+)$/)`) {
		t.Error("configuration editor does not select the diagram element from the URL")
	}
}
