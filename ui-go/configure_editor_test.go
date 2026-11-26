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
				`id="editor-errors"`, `/static/configure-editor.js`, `<script src="/static/topology-diagram.js"></script>`,
				`Saves configuration only. Deploy from the environment page.`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("missing editor markup %q", want)
				}
			}
			if platform == "docker" && strings.Contains(page, `data-editor-group="resources"`) {
				t.Error("Docker must not show a cloud resource panel")
			}
			for _, name := range []string{`name="cluster_enable_toolkit[]"`, `name="replset_enable_toolkit[]"`} {
				if has := strings.Contains(page, name); has == (platform == "docker") {
					t.Errorf("%s: Percona Toolkit checkbox %s present=%v", platform, name, has)
				}
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
assert.match(topologySummary({kind: 'replset', toolkit: true}).features, /Toolkit on/);
assert.doesNotMatch(topologySummary({kind: 'replset'}).features, /Toolkit/);
assert.equal(topologySummary({kind: 'cluster'}).nodes, 11);
assert.equal(topologySummary({kind: 'replset', members: '', arbiters: ''}).nodes, 3);
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("topology summary: %v\n%s", err, out)
	}
}

func TestTopologyDiagramGraph(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	cmd := exec.Command(node, "-e", `
const assert = require('node:assert/strict');
const {buildTopologyGraph, layoutTopologyGraph, describeTopologyGraph, renderTopologySVG} = require('./static/topology-diagram.js');
const edges = (g, family) => g.edges.filter(e => !family || e.family === family).map(e => e.from + '>' + e.to + ':' + e.label);
const members = set => set.members.map(m => m.kind);

// Replica set only: members, replication, no services.
let g = buildTopologyGraph({platform: 'docker', topologies: [{kind: 'replset', name: 'rs01', members: '3', arbiters: '1'}]});
assert.deepEqual(members(g.topologies[0].sets[0]), ['primary', 'secondary', 'secondary', 'arbiter']);
assert.deepEqual(edges(g), ['replset:0/rs/0>replset:0/rs/1:replication', 'replset:0/rs/0>replset:0/rs/2:replication']);
assert.equal(g.services.length + g.clients.length, 0);
assert.equal(g.frame, 'Docker network');
// Replication arrows arc below the members, nested so they never cross a box or each other.
{
  const layout = layoutTopologyGraph(g);
  const bottom = id => layout.boxes.get(id).y + layout.boxes.get(id).h;
  const set = layout.boxes.get('replset:0/rs');
  const svg = renderTopologySVG(g, layout);
  const arcs = [...svg.matchAll(/: replication<\/title><path class="td-edge-hit" d="M([\d.]+) ([\d.]+) C[\d.]+ ([\d.]+) [\d.]+ [\d.]+ ([\d.]+) ([\d.]+)"/g)].map(m => m.slice(1).map(Number));
  assert.equal(arcs.length, 2);
  for (const [, y0, depth, x1, y1] of arcs) {
    assert.ok(Math.abs(y0 - bottom('replset:0/rs/0')) < 0.1 && Math.abs(y1 - y0) < 0.1, 'replication arcs start and end at member bottoms');
    assert.ok(depth > y0 && depth < set.y + set.h, 'replication arcs stay inside the set box');
  }
  const [near, far] = arcs.sort((p, q) => p[3] - q[3]);
  assert.ok(far[0] < near[0] && far[2] > near[2], 'longer arc starts outside and runs deeper');
}
// Empty counts use the same defaults as the summary.
assert.deepEqual(members(buildTopologyGraph({topologies: [{kind: 'replset'}]}).topologies[0].sets[0]), ['primary', 'secondary', 'arbiter']);

// Sharded cluster: routers, config servers, shards and routing edges.
g = buildTopologyGraph({platform: 'gcp', topologies: [{kind: 'cluster', name: 'cl01', shards: '2', members: '3', arbiters: '0', configs: '3', routers: '2', monitoring: true, backups: true, search: true}],
  services: {monitoring: true, backups: true, backupMode: 'create'}});
const cluster = g.topologies[0];
assert.equal(cluster.routers.members.length, 2);
assert.deepEqual(cluster.sets.map(s => s.label), ['Config servers', 'Shard 1', 'Shard 2']);
assert.deepEqual(edges(g).filter(e => e.startsWith('cluster:0/mongos>')), ['cluster:0/mongos>cluster:0/cfg:cluster metadata', 'cluster:0/mongos>cluster:0/shard0:routed queries', 'cluster:0/mongos>cluster:0/shard1:routed queries']);
assert.deepEqual(edges(g, 'monitoring'), ['cluster:0>svc:monitoring:metrics']);
assert.deepEqual(edges(g, 'backup'), ['cluster:0>svc:backups:backups + oplog']);
assert.equal(g.services.find(s => s.id === 'svc:backups').label, 'GCS bucket');
// Each node lists its main process, then its components: PBM not on mongos,
// mongot only beside data-bearing shard nodes.
const processes = member => member.processes.map(p => p.name);
assert.deepEqual(processes(cluster.routers.members[0]), ['mongos', 'pmm-agent']);
assert.deepEqual(processes(cluster.sets[0].members[0]), ['mongod', 'pmm-agent', 'pbm-agent']);
assert.deepEqual(processes(cluster.sets[1].members[0]), ['mongod', 'pmm-agent', 'pbm-agent', 'mongot']);
assert.ok(cluster.sets[1].members[0].processes.every(p => !p.sidecar), 'VM components run on the host');
let lines = describeTopologyGraph(g);
assert.ok(lines.includes('cl01 routers mongos: mongos + pmm-agent'));
assert.ok(lines.includes('cl01 shard 1 primary, secondary: mongod + pmm-agent + pbm-agent + mongot'));
// Config servers sit beside the routers, sharing their row height; shards form the row below.
let layout = layoutTopologyGraph(g);
const box = id => layout.boxes.get(id);
assert.equal(box('cluster:0/cfg').y, box('cluster:0/mongos').y);
assert.equal(box('cluster:0/cfg').h, box('cluster:0/mongos').h);
assert.equal(box('cluster:0/cfg/0').h, box('cluster:0/mongos/0').h);
assert.ok(box('cluster:0/mongos').x + box('cluster:0/mongos').w < box('cluster:0/cfg').x);
assert.ok(box('cluster:0/shard0').y >= box('cluster:0/cfg').y + box('cluster:0/cfg').h);
assert.equal(box('cluster:0/shard0').y, box('cluster:0/shard1').y);
assert.ok(box('cluster:0').y + box('cluster:0').h >= box('cluster:0/shard0').y + box('cluster:0/shard0').h);
// The router to config server arrow runs sideways between the two boxes.
let svg = renderTopologySVG(g, layout);
const cfgEdge = svg.match(/cl01 mongos routers → cl01 config servers: cluster metadata<\/title><path class="td-edge-hit" d="M([\d.]+) ([\d.]+) C[^"]* ([\d.]+) ([\d.]+)"/);
assert.ok(cfgEdge, 'router to config server edge must be drawn');
assert.ok(Math.abs(Number(cfgEdge[1]) - (box('cluster:0/mongos').x + box('cluster:0/mongos').w)) < 0.1);
assert.ok(Math.abs(Number(cfgEdge[3]) - box('cluster:0/cfg').x) < 0.1);
assert.match(svg, /class="td-proc td-proc-backup"[^]*?>pbm-agent</);

// Arbiters get PMM but no PBM or mongot; Docker components are sidecar containers.
g = buildTopologyGraph({platform: 'aws', topologies: [{kind: 'replset', members: '1', arbiters: '1', monitoring: true, backups: true, search: true, toolkit: true}]});
assert.deepEqual(processes(g.topologies[0].sets[0].members[1]), ['mongod', 'pmm-agent', 'Percona Toolkit']);
g = buildTopologyGraph({platform: 'docker', topologies: [{kind: 'replset', name: 'rs01', members: '2', arbiters: '1', monitoring: true, backups: true}]});
assert.deepEqual(g.topologies[0].sets[0].members[0].processes.map(p => p.sidecar), [undefined, true, true]);
assert.ok(describeTopologyGraph(g).includes('rs01 arbiter: mongod + pmm-agent (sidecar container)'));
assert.match(renderTopologySVG(g), /td-proc-monitoring td-sidecar/);
// Large topologies collapse.
g = buildTopologyGraph({platform: 'aws', topologies: [{kind: 'cluster', shards: '10', members: '7', arbiters: '0', routers: '9'}]});
assert.equal(g.topologies[0].sets.length, 4);
assert.equal(g.topologies[0].sets[3].label, '+8 more shards');
assert.equal(g.topologies[0].sets[1].members.at(-1).label, '+3 more');
assert.equal(g.topologies[0].routers.members.at(-1).label, '+6 more');
assert.match(describeTopologyGraph(g)[0], /10 shards of 7 data nodes/);

// Security services appear only when enabled; Vault only for PSMDB.
g = buildTopologyGraph({platform: 'chaos', topologies: [
  {kind: 'replset', name: 'a', distribution: 'psmdb', tls: true}, {kind: 'replset', name: 'b', distribution: 'community'}],
  services: {vault: true, ca: true, caPlacement: 'dedicated', ldap: true, backups: true, backupMode: 'seaweedfs'}});
assert.deepEqual(g.services.map(s => s.id), ['svc:backups', 'svc:ca', 'svc:ldap', 'svc:vault/replset:0']);
assert.deepEqual(edges(g, 'security'), ['replset:0>svc:vault/replset:0:master key', 'svc:ca>replset:0:x.509 certificates', 'replset:0>svc:ldap:LDAP authentication', 'replset:1>svc:ldap:LDAP authentication']);
assert.equal(g.services[0].sub, 'S3 storage VM');
// Focusing a topology keeps the services it uses highlighted.
assert.deepEqual(g.services.find(s => s.id === 'svc:ldap').refs.sort(), ['replset:0', 'replset:1', 'svc:ldap']);
assert.ok(!g.services.find(s => s.id === 'svc:backups').refs.includes('replset:0'));
// CA on the PMM host is drawn as part of the PMM server; Docker cannot place it there.
g = buildTopologyGraph({platform: 'aws', topologies: [{kind: 'replset', tls: true}], services: {monitoring: true, ca: true, caPlacement: 'pmm'}});
assert.deepEqual(g.services.map(s => s.id), ['svc:monitoring']);
assert.deepEqual(edges(g, 'security'), ['svc:monitoring>replset:0:x.509 certificates']);
g = buildTopologyGraph({platform: 'docker', topologies: [{kind: 'replset', tls: true}], services: {monitoring: true, ca: true, caPlacement: 'pmm'}});
assert.deepEqual(g.services.map(s => s.id), ['svc:monitoring', 'svc:ca']);

// ClusterSync, load generator and PMM clients.
g = buildTopologyGraph({platform: 'gcp', topologies: [{kind: 'replset', name: 'src'}, {kind: 'replset', name: 'dst'}, {kind: 'cluster', name: 'cl'}],
  services: {sync: true, syncSource: 'replset:src', syncTarget: 'replset:dst', load: true, clients: 6, monitoring: true}});
assert.deepEqual(edges(g).filter(e => e.includes('svc:sync')), ['replset:0>svc:sync:clone + change stream', 'svc:sync>replset:1:apply writes']);
assert.ok(edges(g).includes('svc:load>cluster:0/mongos:workload'));
assert.equal(g.clients.filter(c => c.ref === 'svc:clients').length, 4);
assert.equal(g.clients.at(-1).label, '+3 PMM clients');
assert.equal(edges(g, 'monitoring').length, 4);

// Rendering is self-contained SVG with escaped names and one target per editable element.
svg = renderTopologySVG(buildTopologyGraph({topologies: [{kind: 'replset', name: '<rs>'}], services: {monitoring: true}}));
assert.match(svg, /^<svg[^>]*viewBox="0 0 [\d.]+ [\d.]+"/);
assert.ok(!svg.includes('<rs>') && svg.includes('&lt;rs&gt;'));
assert.deepEqual(Array.from(svg.matchAll(/data-ref="([^"]+)"/g), m => m[1]), ['replset:0', 'svc:monitoring']);
assert.match(renderTopologySVG(buildTopologyGraph({topologies: []})), /Add a cluster or replica set/);
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("topology diagram: %v\n%s", err, out)
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
