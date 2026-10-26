package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func hwTopology() Config {
	return Config{
		Clusters:   map[string]ClusterConfig{"cl01": {}},
		Replsets:   map[string]ReplsetConfig{"rs01": {}},
		EnablePlgm: true,
	}
}

func hwPresetConfig(t *testing.T, p HardwarePreset) Config {
	t.Helper()
	cfg := hwTopology()
	b, err := json.Marshal(p.Fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("preset %s fields do not map onto Config: %v", p.ID, err)
	}
	return cfg
}

func hwExpectErr(t *testing.T, errs []string, want string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Errorf("expected an error containing %q, got %q", want, errs)
}

func TestHardwarePresetsAreValid(t *testing.T) {
	for _, platform := range []string{"aws", "gcp"} {
		presets := hardwarePresets(platform)
		if len(presets) < 7 {
			t.Fatalf("%s: expected dev, 5 Atlas-like and 2 production presets, got %d", platform, len(presets))
		}
		for _, p := range presets {
			for f := range p.Fields {
				if !containsString(hardwareFormFields, f) {
					t.Errorf("%s/%s: field %q is not in hardwareFormFields", platform, p.ID, f)
				}
			}
			cfg := hwPresetConfig(t, p)
			if platform == "aws" && cfg.SubnetCount == 0 {
				cfg.SubnetCount = 3
			}
			if errs := validateHardwareConfig(platform, cfg); len(errs) > 0 {
				t.Errorf("%s/%s preset is invalid: %q", platform, p.ID, errs)
			}
		}
	}
}

func TestValidateHardwareGCPRules(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Config)
		want string
	}{
		{"iops on pd-ssd", func(c *Config) { c.DataDiskType, c.DataDiskIops = "pd-ssd", 5000 }, "does not accept provisioned IOPS"},
		{"iops out of range", func(c *Config) {
			c.ShardsvrType, c.ReplsetSvrType, c.ConfigsvrType, c.MongosType, c.ArbiterType, c.PlgmType = "c3-standard-8", "c3-standard-8", "c3-standard-4", "c3-standard-4", "c3-standard-4", "c3-standard-4"
			c.BootDiskType, c.DataDiskType, c.DataDiskIops = "pd-balanced", "hyperdisk-balanced", 1000
		}, "IOPS must be between"},
		{"iops per GB", func(c *Config) {
			c.DataDiskType, c.DataDiskIops, c.ShardsvrType = "hyperdisk-balanced", 20000, "c3-standard-8"
		}, "IOPS needs a hyperdisk-balanced disk of at least 40 GB"},
		{"hyperdisk on e2", func(c *Config) { c.DataDiskType = "hyperdisk-balanced" }, "E2 machines do not support Hyperdisk"},
		{"pd on c4", func(c *Config) {
			c.ShardsvrType, c.DataDiskType, c.BootDiskType = "c4-standard-8", "pd-ssd", "hyperdisk-balanced"
		}, "C4 machines only support Hyperdisk"},
		{"c4 needs boot type", func(c *Config) { c.ShardsvrType, c.DataDiskType = "c4-standard-8", "hyperdisk-balanced" }, "needs an explicit boot disk type (hyperdisk-balanced)"},
		{"mixed series boot disk", func(c *Config) {
			c.ShardsvrType, c.DataDiskType, c.BootDiskType = "c4-standard-8", "hyperdisk-balanced", "hyperdisk-balanced"
		}, "arbiter e2-medium: E2 machines do not support Hyperdisk"},
		{"cpu platform on e2", func(c *Config) { c.MinCpuPlatform = "Intel Ice Lake" }, "fixed CPU platform"},
		{"amd platform on n2", func(c *Config) {
			c.ShardsvrType, c.ConfigsvrType, c.MongosType, c.ArbiterType, c.ReplsetSvrType, c.PlgmType = "n2-standard-4", "n2-standard-2", "n2-standard-2", "n2-standard-2", "n2-standard-2", "n2-standard-2"
			c.MinCpuPlatform = "AMD Milan"
		}, `cannot use CPU platform "AMD Milan"`},
		{"tier1 needs gvnic", func(c *Config) { c.Tier1Networking = true }, "requires NIC type GVNIC"},
		{"compact needs single zone", func(c *Config) { c.CompactPlacement = true }, "requires zone mode single"},
		{"compact on e2", func(c *Config) { c.CompactPlacement, c.ZoneMode = true, "single" }, "does not support compact placement"},
		{"zone outside region", func(c *Config) { c.Region, c.Zones = "us-central1", []string{"europe-west1-b"} }, "not in region us-central1"},
		{"bad label", func(c *Config) { c.ResourceLabels = map[string]string{"Bench Run": "x"} }, `label key "Bench Run"`},
		{"unknown disk", func(c *Config) { c.DataDiskType = "gp3" }, `unknown data disk type "gp3"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := hwTopology()
			tc.mod(&cfg)
			hwExpectErr(t, validateHardwareConfig("gcp", cfg), tc.want)
		})
	}
	if errs := validateHardwareConfig("gcp", hwTopology()); len(errs) > 0 {
		t.Errorf("defaults should be valid, got %q", errs)
	}
}

func TestValidateHardwareRolesFollowTopology(t *testing.T) {
	// Without arbiters or a plgm client, their (default E2) machine types must not block Hyperdisk.
	cfg := Config{
		Replsets:       map[string]ReplsetConfig{"rs01": {ArbitersPerReplset: intPtr(0)}},
		ReplsetSvrType: "c4-standard-8", DataDiskType: "hyperdisk-balanced", BootDiskType: "hyperdisk-balanced",
	}
	if errs := validateHardwareConfig("gcp", cfg); len(errs) > 0 {
		t.Errorf("unexpected errors: %q", errs)
	}
}

func TestValidateHardwareAWSRules(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*Config)
		want string
	}{
		{"throughput on io2", func(c *Config) { c.DataDiskType, c.DataDiskThroughput = "io2", 500 }, "does not accept provisioned throughput"},
		{"gp3 iops range", func(c *Config) { c.DataDiskType, c.DataDiskIops = "gp3", 20000 }, "gp3 IOPS must be between 3000 and 16000"},
		{"io1 ratio uses default iops", func(c *Config) { c.DataDiskType = "io1" }, "3000 IOPS needs a io1 disk of at least 60 GB (config server disk is 20 GB)"},
		{"st1 min size", func(c *Config) { c.DataDiskType = "st1" }, "st1 volumes must be at least 125 GB"},
		{"st1 not boot", func(c *Config) { c.BootDiskType = "st1" }, "cannot be used as a boot disk type"},
		{"placement group needs one subnet", func(c *Config) { c.CompactPlacement = true }, "requires Subnet Count = 1"},
		{"gcp-only fields", func(c *Config) { c.NicType = "GVNIC" }, "only available on GCP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := hwTopology()
			tc.mod(&cfg)
			hwExpectErr(t, validateHardwareConfig("aws", cfg), tc.want)
		})
	}
	ok := hwTopology()
	ok.DataDiskType, ok.ConfigsvrDiskType, ok.DataDiskIops, ok.CompactPlacement, ok.SubnetCount = "io2", "gp3", 20000, true, 1
	ok.ShardsvrVolumeSize, ok.ReplsetSvrVolumeSize = 500, 500
	if errs := validateHardwareConfig("aws", ok); len(errs) > 0 {
		t.Errorf("expected a valid config, got %q", errs)
	}
}

func TestNormalizeHardwareDropsForeignSettings(t *testing.T) {
	cfg := Config{NicType: " GVNIC ", Zones: []string{" a ", ""}, DataDiskIops: 5, ResourceLabels: map[string]string{" k ": " v "}}
	normalizeHardwareConfig("gcp", &cfg)
	if cfg.NicType != "GVNIC" || len(cfg.Zones) != 1 || cfg.Zones[0] != "a" || cfg.ResourceLabels["k"] != "v" {
		t.Errorf("gcp normalization failed: %+v", cfg)
	}
	normalizeHardwareConfig("aws", &cfg)
	if cfg.NicType != "" || cfg.Zones != nil || cfg.DataDiskIops != 5 {
		t.Errorf("aws should drop GCP-only settings: %+v", cfg)
	}
	normalizeHardwareConfig("azure", &cfg)
	if cfg.DataDiskIops != 0 || cfg.ResourceLabels != nil {
		t.Errorf("azure should drop benchmark settings: %+v", cfg)
	}
}

func writeTestTfvars(t *testing.T, platform string, cfg Config) string {
	t.Helper()
	dir := t.TempDir()
	orig := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = orig })
	cfg.Prefix = "hw"
	if err := writeTfvars("hw", platform, cfg); err != nil {
		t.Fatalf("writeTfvars: %v", err)
	}
	b, err := os.ReadFile(tfvarsPath("hw", platform))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteTfvarsHardwareGCP(t *testing.T) {
	cfg := hwTopology()
	cfg.DataDiskType, cfg.DataDiskIops, cfg.DataDiskThroughput = "hyperdisk-balanced", 16000, 400
	cfg.ConfigsvrDiskType, cfg.BootDiskType, cfg.BootDiskSize = "pd-ssd", "pd-balanced", 30
	cfg.MinCpuPlatform, cfg.NicType, cfg.Tier1Networking = "Intel Ice Lake", "GVNIC", true
	cfg.ZoneMode, cfg.Zones, cfg.CompactPlacement = "single", []string{"us-central1-a"}, true
	cfg.PlgmType, cfg.ResourceLabels = "n2-highcpu-16", map[string]string{"z": "1", "bench_run": "r1"}
	out := writeTestTfvars(t, "gcp", cfg)
	for _, want := range []string{
		`data_disk_type = "hyperdisk-balanced"`, `data_disk_iops = 16000`, `data_disk_throughput = 400`,
		`configsvr_disk_type = "pd-ssd"`, `boot_disk_type = "pd-balanced"`, `boot_disk_size = 30`,
		`min_cpu_platform = "Intel Ice Lake"`, `nic_type = "GVNIC"`, `tier1_networking = true`,
		`zone_mode = "single"`, `zones = ["us-central1-a"]`, `compact_placement = true`,
		`plgm_type = "n2-highcpu-16"`, "labels = {\n  \"bench_run\" = \"r1\"\n  \"z\" = \"1\"\n}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("gcp tfvars missing %q:\n%s", want, out)
		}
	}
}

func TestWriteTfvarsHardwareAWSAndDefaults(t *testing.T) {
	cfg := hwTopology()
	cfg.DataDiskType, cfg.DataDiskIops, cfg.DataDiskThroughput, cfg.CompactPlacement = "gp3", 6000, 250, true
	out := writeTestTfvars(t, "aws", cfg)
	for _, want := range []string{`data_disk_iops = 6000`, `data_disk_throughput = 250`, `compact_placement = true`} {
		if !strings.Contains(out, want) {
			t.Errorf("aws tfvars missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"nic_type", "zone_mode", "min_cpu_platform"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("aws tfvars must not contain %q", unwanted)
		}
	}
	// Empty settings keep every Terraform default.
	out = writeTestTfvars(t, "gcp", hwTopology())
	for _, unwanted := range []string{"data_disk_iops", "boot_disk", "compact_placement", "labels", "zones", "plgm_type"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("default gcp tfvars must not contain %q:\n%s", unwanted, out)
		}
	}
}

func TestConfigureRendersComputeFieldsOnce(t *testing.T) {
	for _, platform := range []string{"gcp", "aws", "azure"} {
		cfg := Config{
			Clusters: map[string]ClusterConfig{"cl01": {}, "cl02": {}},
			Replsets: map[string]ReplsetConfig{"rs01": {}, "rs02": {}},
		}
		if platform != "azure" {
			cfg.DataDiskType, cfg.HardwarePreset = map[string]string{"gcp": "pd-ssd", "aws": "gp3"}[platform], "atlas-m50"
		}
		page := renderConfigure(t, ConfigureData{
			Platform: platform, Config: cfg,
			SortedClusters: sortedClusters(cfg.Clusters), SortedReplsets: sortedReplsets(cfg.Replsets),
		})
		// Count rendered markup only (JS block builders hold CHAOS-only copies).
		form := page[strings.Index(page, `<form id="config-form"`):strings.Index(page, `</form>`)]
		for _, field := range []string{"shardsvr_type", "configsvr_type", "mongos_type", "arbiter_type", "replsetsvr_type", "data_disk_type", "shardsvr_volume_size", "plgm_type"} {
			if n := strings.Count(form, `name="`+field+`"`); n != 1 {
				t.Errorf("%s: field %s rendered %d times, want 1", platform, field, n)
			}
		}
		hasPreset := strings.Contains(page, `id="hardware_preset"`)
		if hasPreset != (platform != "azure") {
			t.Errorf("%s: hardware preset selector presence = %v", platform, hasPreset)
		}
		if platform != "azure" {
			if !strings.Contains(page, `<option value="atlas-m50" selected>`) {
				t.Errorf("%s: saved preset not selected", platform)
			}
			if !strings.Contains(page, `<option value="`+cfg.DataDiskType+`" selected>`) {
				t.Errorf("%s: saved data disk type not selected", platform)
			}
		}
		hasGCPOnly := strings.Contains(page, `name="min_cpu_platform"`)
		if hasGCPOnly != (platform == "gcp") {
			t.Errorf("%s: GCP-only fields presence = %v", platform, hasGCPOnly)
		}
	}
	page := renderConfigure(t, ConfigureData{Platform: "gcp"})
	if !strings.Contains(page, `name="use_spot_instances"`) {
		t.Error("gcp: spot checkbox missing")
	}
	page = renderConfigure(t, ConfigureData{Platform: "chaos"})
	if strings.Contains(page, `id="compute-storage-section"`) {
		t.Error("chaos must keep its own compute fields")
	}
}

func TestAPIHardwareValidate(t *testing.T) {
	body := `{"clusters":{"c":{}},"data_disk_type":"pd-ssd","data_disk_iops":5000}`
	req := httptest.NewRequest(http.MethodPost, "/api/hardware/validate?platform=gcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	apiHardwareValidateHandler(rec, req)
	var resp struct{ Errors []string }
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	hwExpectErr(t, resp.Errors, "does not accept provisioned IOPS")

	req = httptest.NewRequest(http.MethodGet, "/api/hardware/presets?platform=aws", nil)
	rec = httptest.NewRecorder()
	apiHardwarePresetsHandler(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"atlas-m30"`) {
		t.Fatalf("presets endpoint: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHardwareDetailsCollapsedUnlessCustomized(t *testing.T) {
	isOpen := func(page string) bool {
		i := strings.Index(page, `id="hardware-details"`)
		if i < 0 {
			t.Fatal("hardware details panel missing")
		}
		return !strings.HasPrefix(page[i:], `id="hardware-details" style="display:none"`)
	}
	for _, platform := range []string{"gcp", "aws", "azure"} {
		page := renderConfigure(t, ConfigureData{Platform: platform})
		if isOpen(page) || !strings.Contains(page, "▼ Customize</button>") {
			t.Errorf("%s: new environment should start collapsed", platform)
		}
	}
	page := renderConfigure(t, ConfigureData{Platform: "gcp", Config: Config{HardwarePreset: "atlas-m50", ShardsvrType: "n2-standard-8"}})
	if isOpen(page) {
		t.Error("preset-based settings should start collapsed")
	}
	page = renderConfigure(t, ConfigureData{Platform: "gcp", Config: Config{ShardsvrType: "n2-standard-8"}})
	if !isOpen(page) {
		t.Error("hand-edited settings should start expanded")
	}
	// The preset selector and validation errors stay outside the panel.
	i, j := strings.Index(page, `id="hardware_preset"`), strings.Index(page, `id="hardware-details"`)
	k, end := strings.Index(page, `id="hardware-errors"`), strings.Index(page, `<!-- #hardware-details -->`)
	if !(i < j && end < k) {
		t.Error("preset selector and errors must be outside the collapsible panel")
	}
}

func TestCloudNetworkAndPlgmPanelsCollapsedByDefault(t *testing.T) {
	panelOpen := func(t *testing.T, page, id string) bool {
		t.Helper()
		i := strings.Index(page, `id="`+id+`"`)
		if i < 0 {
			t.Fatalf("panel %s missing", id)
		}
		return !strings.HasPrefix(page[i:], `id="`+id+`" style="display:none"`)
	}
	for _, platform := range []string{"gcp", "aws", "azure"} {
		page := renderConfigure(t, ConfigureData{Platform: platform})
		if panelOpen(t, page, "cloud-network-details") || panelOpen(t, page, "plgm-client-details") {
			t.Errorf("%s: network and plgm client panels should start collapsed", platform)
		}
		// Region and machine image stay visible above the panel.
		net := strings.Index(page, `id="cloud-network-details"`)
		if strings.Index(page, `name="subnet_cidr"`) < net || strings.Index(page, `name="use_spot_instances"`) < net {
			t.Errorf("%s: subnet and spot settings must be inside the network panel", platform)
		}
		if platform != "azure" && strings.Index(page, `id="machine_image"`) > net {
			t.Errorf("%s: machine image must stay outside the network panel", platform)
		}
		if !strings.Contains(page, `id="plgm-client-wrap" style="display:none"`) {
			t.Errorf("%s: plgm client settings should be hidden while plgm is disabled", platform)
		}
	}
	for _, cfg := range []Config{{DefaultVpcName: "bench"}, {SubnetCIDR: "10.1.0.0/20"}, {UseSpotInstances: true}, {SubnetCount: 1}} {
		if page := renderConfigure(t, ConfigureData{Platform: "aws", Config: cfg}); !panelOpen(t, page, "cloud-network-details") {
			t.Errorf("non-default network settings %+v should start expanded", cfg)
		}
	}
	if page := renderConfigure(t, ConfigureData{Platform: "gcp", Config: Config{DefaultVpcName: "mongo", SubnetCIDR: "10.128.0.0/20"}}); panelOpen(t, page, "cloud-network-details") {
		t.Error("default network values should keep the panel collapsed")
	}
	page := renderConfigure(t, ConfigureData{Platform: "gcp", Config: Config{EnablePlgm: true, PlgmType: "n2-highcpu-8"}})
	if !panelOpen(t, page, "plgm-client-details") || strings.Contains(page, `id="plgm-client-wrap" style="display:none"`) {
		t.Error("a hand-picked plgm machine type should be visible")
	}
	page = renderConfigure(t, ConfigureData{Platform: "gcp", Config: Config{EnablePlgm: true, PlgmType: "n2-highcpu-8", HardwarePreset: "atlas-m50"}})
	if panelOpen(t, page, "plgm-client-details") {
		t.Error("a preset plgm machine type should stay collapsed")
	}
}
