package main

// Benchmark hardware tunables for AWS and GCP: compatibility rules, validation,
// presets and tfvars output. The UI calls /api/hardware/validate for live
// feedback and saveEnvironmentHandler runs the same validation on save, so
// the rules below are the single source of truth.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// ── Disk types ──────────────────────────────────────────────────────────────

type diskTypeSpec struct {
	Name string `json:"name"`
	// Provisioned IOPS range (0,0 means IOPS cannot be set).
	MinIops int `json:"min_iops,omitempty"`
	MaxIops int `json:"max_iops,omitempty"`
	// Provisioned throughput range in MiB/s (0,0 means it cannot be set).
	MinThroughput int `json:"min_throughput,omitempty"`
	MaxThroughput int `json:"max_throughput,omitempty"`
	// Maximum IOPS per provisioned GB (0 = not checked).
	MaxIopsPerGB int `json:"max_iops_per_gb,omitempty"`
	// Minimum volume size in GB (0 = not checked).
	MinSizeGB int `json:"min_size_gb,omitempty"`
	// Usable as a boot disk.
	Boot bool   `json:"boot"`
	Hint string `json:"hint,omitempty"`
}

var gcpDiskTypes = []diskTypeSpec{
	{Name: "pd-standard", Boot: true, Hint: "HDD. Not supported by C3, C4 or N4."},
	{Name: "pd-balanced", Boot: true, Hint: "SSD, performance scales with size."},
	{Name: "pd-ssd", Boot: true, Hint: "SSD, ~30 IOPS per GB."},
	{Name: "pd-extreme", MinIops: 10000, MaxIops: 120000, Hint: "Provisioned IOPS. N2 (64+ vCPU) and M-series only."},
	{Name: "hyperdisk-balanced", MinIops: 3000, MaxIops: 160000, MinThroughput: 140, MaxThroughput: 2400, MaxIopsPerGB: 500, Boot: true, Hint: "Provisioned IOPS and throughput. Required by C4 and N4."},
	{Name: "hyperdisk-extreme", MinIops: 2500, MaxIops: 350000, MaxIopsPerGB: 1000, Hint: "Provisioned IOPS. Needs a large machine (typically 64+ vCPU)."},
	{Name: "hyperdisk-throughput", MinThroughput: 10, MaxThroughput: 2400, Hint: "Provisioned throughput, HDD-class latency."},
}

var awsDiskTypes = []diskTypeSpec{
	{Name: "gp2", Boot: true, Hint: "Burstable SSD, 3 IOPS per GB."},
	{Name: "gp3", MinIops: 3000, MaxIops: 16000, MinThroughput: 125, MaxThroughput: 1000, MaxIopsPerGB: 500, Boot: true, Hint: "Baseline 3000 IOPS / 125 MiB/s; both can be raised."},
	{Name: "io1", MinIops: 100, MaxIops: 64000, MaxIopsPerGB: 50, Boot: true, Hint: "Provisioned IOPS (defaults to 3000)."},
	{Name: "io2", MinIops: 100, MaxIops: 256000, MaxIopsPerGB: 1000, Boot: true, Hint: "Provisioned IOPS (defaults to 3000)."},
	{Name: "st1", MinSizeGB: 125, Hint: "Throughput-optimized HDD. Not suitable for MongoDB data in production."},
	{Name: "sc1", MinSizeGB: 125, Hint: "Cold HDD. Not suitable for MongoDB data."},
}

func diskTypesFor(platform string) []diskTypeSpec {
	switch platform {
	case "gcp":
		return gcpDiskTypes
	case "aws":
		return awsDiskTypes
	}
	return nil
}

func lookupDiskType(platform, name string) (diskTypeSpec, bool) {
	for _, d := range diskTypesFor(platform) {
		if d.Name == name {
			return d, true
		}
	}
	return diskTypeSpec{}, false
}

// ── GCP machine families ────────────────────────────────────────────────────

// gcpFamily returns the machine series of a GCP machine type, e.g. "n2" for
// "n2-standard-8" or "c4a" for "c4a-highmem-16". Custom types such as
// "custom-4-8192" are N1.
func gcpFamily(machineType string) string {
	mt := strings.ToLower(strings.TrimSpace(machineType))
	if mt == "" {
		return ""
	}
	if strings.HasPrefix(mt, "custom-") {
		return "n1"
	}
	return strings.SplitN(mt, "-", 2)[0]
}

// Machine series that only accept Hyperdisk.
var gcpHyperdiskOnlyFamilies = map[string]bool{"n4": true, "c4": true, "c4a": true, "c4d": true}

// Machine series that do not accept Hyperdisk at all.
var gcpNoHyperdiskFamilies = map[string]bool{"e2": true, "n1": true}

// Machine series that do not accept pd-standard.
var gcpNoPdStandardFamilies = map[string]bool{"c3": true, "c3d": true, "n4": true, "c4": true, "c4a": true, "c4d": true, "h3": true, "m3": true}

// Machine series with a single, fixed CPU platform: min_cpu_platform is not supported.
var gcpFixedCPUFamilies = map[string]bool{"e2": true, "n4": true, "c3": true, "c3d": true, "c4": true, "c4a": true, "c4d": true, "t2a": true, "t2d": true, "h3": true}

var gcpIntelFamilies = map[string]bool{"n1": true, "n2": true, "c2": true, "m1": true, "m2": true, "m3": true}
var gcpAMDFamilies = map[string]bool{"n2d": true, "c2d": true}

// Machine series that support Tier_1 networking (on large shapes).
var gcpTier1Families = map[string]bool{"n2": true, "n2d": true, "c2": true, "c2d": true, "c3": true, "c3d": true, "c4": true, "c4a": true, "c4d": true, "m3": true, "h3": true, "z3": true}

// GCP min_cpu_platform values offered in the UI.
var gcpCPUPlatforms = []string{
	"Intel Cascade Lake", "Intel Ice Lake", "Intel Sapphire Rapids",
	"AMD Rome", "AMD Milan", "AMD Genoa",
}

func gcpDiskSupported(family, diskType string) error {
	if family == "" {
		return nil
	}
	hyper := strings.HasPrefix(diskType, "hyperdisk-")
	switch {
	case hyper && gcpNoHyperdiskFamilies[family]:
		return fmt.Errorf("%s machines do not support Hyperdisk", strings.ToUpper(family))
	case !hyper && gcpHyperdiskOnlyFamilies[family]:
		return fmt.Errorf("%s machines only support Hyperdisk", strings.ToUpper(family))
	case diskType == "pd-standard" && gcpNoPdStandardFamilies[family]:
		return fmt.Errorf("%s machines do not support pd-standard", strings.ToUpper(family))
	}
	return nil
}

// ── Effective configuration ────────────────────────────────────────────────

// Terraform defaults, used when a field is left empty in the UI.
var hardwareDefaults = map[string]map[string]string{
	"gcp": {"node_type": "e2-medium", "plgm_type": "e2-medium", "data_disk_type": "pd-standard"},
	"aws": {"node_type": "t3.medium", "plgm_type": "t3.medium", "data_disk_type": "gp2"},
}

type hwRole struct {
	Label       string
	MachineType string
	DiskType    string // "" = no data disk
	DiskSizeGB  int
	HasTopology bool // role exists in the configured topology
}

// hardwareRoles lists every MongoDB role with its effective machine type and
// data disk, so rules can be checked against each one.
func hardwareRoles(platform string, cfg Config) []hwRole {
	def := hardwareDefaults[platform]
	nodeType := func(v string) string { return strDefault(v, def["node_type"]) }
	dataDisk := strDefault(cfg.DataDiskType, def["data_disk_type"])
	cfgDisk := strDefault(cfg.ConfigsvrDiskType, dataDisk)

	hasClusters := len(cfg.Clusters) > 0
	hasReplsets := len(cfg.Replsets) > 0
	hasArbiters := false
	for _, c := range cfg.Clusters {
		if intPtrDefault(c.ArbitersPerReplset, 1) > 0 {
			hasArbiters = true
		}
	}
	for _, r := range cfg.Replsets {
		if intPtrDefault(r.ArbitersPerReplset, 1) > 0 {
			hasArbiters = true
		}
	}
	return []hwRole{
		{"shard server", nodeType(cfg.ShardsvrType), dataDisk, intDefault(cfg.ShardsvrVolumeSize, 50), hasClusters},
		{"config server", nodeType(cfg.ConfigsvrType), cfgDisk, intDefault(cfg.ConfigsvrVolumeSize, 20), hasClusters},
		{"mongos", nodeType(cfg.MongosType), "", 0, hasClusters},
		{"replica set node", nodeType(cfg.ReplsetSvrType), dataDisk, intDefault(cfg.ReplsetSvrVolumeSize, 100), hasReplsets},
		{"arbiter", nodeType(cfg.ArbiterType), "", 0, hasArbiters},
		{"plgm client", strDefault(cfg.PlgmType, def["plgm_type"]), "", 0, cfg.EnablePlgm},
	}
}

// ── Validation ─────────────────────────────────────────────────────────────

var gcpLabelKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
var gcpLabelValueRe = regexp.MustCompile(`^[a-z0-9_-]{0,63}$`)
var gcpZoneRe = regexp.MustCompile(`^[a-z]+-[a-z]+[0-9]+-[a-z]$`)

// validateHardwareConfig checks the benchmark tunables of an AWS or GCP
// environment. It returns every problem found, so the UI can show them all.
func validateHardwareConfig(platform string, cfg Config) []string {
	if platform != "aws" && platform != "gcp" {
		return nil
	}
	var errs []string
	addf := func(format string, args ...interface{}) { errs = append(errs, fmt.Sprintf(format, args...)) }

	roles := hardwareRoles(platform, cfg)
	dataDisk := strDefault(cfg.DataDiskType, hardwareDefaults[platform]["data_disk_type"])
	cfgDisk := strDefault(cfg.ConfigsvrDiskType, dataDisk)

	// Disk types must be known.
	for _, d := range []struct{ field, value string }{
		{"data disk type", dataDisk}, {"config server disk type", cfgDisk},
	} {
		if _, ok := lookupDiskType(platform, d.value); !ok {
			addf("unknown %s %q", d.field, d.value)
		}
	}

	// Provisioned IOPS / throughput must be supported and within range.
	spec, specOK := lookupDiskType(platform, dataDisk)
	if specOK {
		if cfg.DataDiskIops != 0 {
			if spec.MaxIops == 0 {
				addf("data disk type %s does not accept provisioned IOPS", dataDisk)
			} else if cfg.DataDiskIops < spec.MinIops || cfg.DataDiskIops > spec.MaxIops {
				addf("%s IOPS must be between %d and %d", dataDisk, spec.MinIops, spec.MaxIops)
			}
		}
		if cfg.DataDiskThroughput != 0 {
			if spec.MaxThroughput == 0 {
				addf("data disk type %s does not accept provisioned throughput", dataDisk)
			} else if cfg.DataDiskThroughput < spec.MinThroughput || cfg.DataDiskThroughput > spec.MaxThroughput {
				addf("%s throughput must be between %d and %d MiB/s", dataDisk, spec.MinThroughput, spec.MaxThroughput)
			}
		}
	}

	// Per-volume size rules (IOPS-per-GB ratio, minimum size).
	effectiveIops := func(diskType string) int {
		if diskType != dataDisk {
			return 0 // provisioned values only apply to disks of data_disk_type
		}
		if cfg.DataDiskIops == 0 && platform == "aws" && (diskType == "io1" || diskType == "io2") {
			return 3000 // Terraform default for io1/io2
		}
		return cfg.DataDiskIops
	}
	for _, r := range roles {
		if !r.HasTopology || r.DiskType == "" {
			continue
		}
		s, ok := lookupDiskType(platform, r.DiskType)
		if !ok {
			continue
		}
		if s.MinSizeGB > 0 && r.DiskSizeGB < s.MinSizeGB {
			addf("%s volumes must be at least %d GB (%s disk is %d GB)", r.DiskType, s.MinSizeGB, r.Label, r.DiskSizeGB)
		}
		if iops := effectiveIops(r.DiskType); iops > 0 && s.MaxIopsPerGB > 0 && iops > s.MaxIopsPerGB*r.DiskSizeGB {
			addf("%d IOPS needs a %s disk of at least %d GB (%s disk is %d GB)", iops, r.DiskType, (iops+s.MaxIopsPerGB-1)/s.MaxIopsPerGB, r.Label, r.DiskSizeGB)
		}
	}

	// Boot disk.
	if cfg.BootDiskType != "" {
		if s, ok := lookupDiskType(platform, cfg.BootDiskType); !ok || !s.Boot {
			addf("%q cannot be used as a boot disk type", cfg.BootDiskType)
		}
	}
	if cfg.BootDiskSize != 0 && (cfg.BootDiskSize < 10 || cfg.BootDiskSize > 2000) {
		addf("boot disk size must be between 10 and 2000 GB")
	}

	// Labels / tags.
	for k, v := range cfg.ResourceLabels {
		if platform == "gcp" {
			if !gcpLabelKeyRe.MatchString(k) {
				addf("label key %q must start with a lowercase letter and contain only lowercase letters, digits, _ or - (max 63)", k)
			}
			if !gcpLabelValueRe.MatchString(v) {
				addf("label value %q for %q may only contain lowercase letters, digits, _ or - (max 63)", v, k)
			}
		} else if strings.TrimSpace(k) == "" || strings.HasPrefix(strings.ToLower(k), "aws:") || len(k) > 128 || len(v) > 256 {
			addf("invalid tag %q", k)
		}
	}

	if platform == "aws" {
		if cfg.MinCpuPlatform != "" || cfg.NicType != "" || cfg.Tier1Networking || cfg.ZoneMode == "single" || len(cfg.Zones) > 0 {
			addf("CPU platform, NIC type, Tier_1 networking and zone settings are only available on GCP")
		}
		if cfg.CompactPlacement && intDefault(cfg.SubnetCount, 3) != 1 {
			addf("a cluster placement group requires Subnet Count = 1 (single availability zone)")
		}
		return errs
	}

	// ── GCP-only rules ──
	for _, r := range roles {
		if !r.HasTopology {
			continue
		}
		fam := gcpFamily(r.MachineType)
		if r.DiskType != "" {
			if err := gcpDiskSupported(fam, r.DiskType); err != nil {
				addf("%s %s: %v (disk type %s)", r.Label, r.MachineType, err, r.DiskType)
			}
		}
		// Boot disk: C4/N4 need a Hyperdisk boot disk, which the GCP default (pd-standard) is not.
		boot := cfg.BootDiskType
		if boot == "" && (gcpHyperdiskOnlyFamilies[fam] || gcpNoPdStandardFamilies[fam]) {
			addf("%s %s needs an explicit boot disk type (%s)", r.Label, r.MachineType, map[bool]string{true: "hyperdisk-balanced", false: "pd-balanced or hyperdisk-balanced"}[gcpHyperdiskOnlyFamilies[fam]])
		} else if boot != "" {
			if err := gcpDiskSupported(fam, boot); err != nil {
				addf("%s %s: %v (boot disk type %s); use the same machine series for all roles", r.Label, r.MachineType, err, boot)
			}
		}
		if cfg.MinCpuPlatform != "" {
			switch {
			case gcpFixedCPUFamilies[fam]:
				addf("%s %s has a fixed CPU platform; leave Min CPU Platform empty", r.Label, r.MachineType)
			case strings.HasPrefix(cfg.MinCpuPlatform, "Intel") && gcpAMDFamilies[fam],
				strings.HasPrefix(cfg.MinCpuPlatform, "AMD") && gcpIntelFamilies[fam]:
				addf("%s %s cannot use CPU platform %q", r.Label, r.MachineType, cfg.MinCpuPlatform)
			}
		}
		if cfg.Tier1Networking && !gcpTier1Families[fam] {
			addf("%s %s does not support Tier_1 networking", r.Label, r.MachineType)
		}
		if cfg.CompactPlacement && (fam == "e2" || fam == "n1") {
			addf("%s %s does not support compact placement", r.Label, r.MachineType)
		}
	}
	if cfg.MinCpuPlatform != "" && !containsString(gcpCPUPlatforms, cfg.MinCpuPlatform) {
		addf("unknown CPU platform %q", cfg.MinCpuPlatform)
	}
	if cfg.NicType != "" && cfg.NicType != "GVNIC" && cfg.NicType != "VIRTIO_NET" {
		addf("NIC type must be GVNIC or VIRTIO_NET")
	}
	if cfg.Tier1Networking && cfg.NicType != "GVNIC" {
		addf("Tier_1 networking requires NIC type GVNIC")
	}
	if cfg.ZoneMode != "" && cfg.ZoneMode != "spread" && cfg.ZoneMode != "single" {
		addf("zone mode must be spread or single")
	}
	if cfg.CompactPlacement && cfg.ZoneMode != "single" {
		addf("compact placement requires zone mode single")
	}
	for _, z := range cfg.Zones {
		if !gcpZoneRe.MatchString(z) {
			addf("invalid zone %q", z)
		} else if cfg.Region != "" && !strings.HasPrefix(z, cfg.Region+"-") {
			addf("zone %s is not in region %s", z, cfg.Region)
		}
	}
	return errs
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// normalizeHardwareConfig trims values and drops settings that do not apply
// to the platform, so stale values never reach Terraform.
func normalizeHardwareConfig(platform string, cfg *Config) {
	trim := func(p *string) { *p = strings.TrimSpace(*p) }
	for _, p := range []*string{&cfg.DataDiskType, &cfg.ConfigsvrDiskType, &cfg.BootDiskType, &cfg.MinCpuPlatform, &cfg.NicType, &cfg.ZoneMode, &cfg.PlgmType, &cfg.HardwarePreset} {
		trim(p)
	}
	zones := cfg.Zones[:0]
	for _, z := range cfg.Zones {
		if z = strings.TrimSpace(z); z != "" {
			zones = append(zones, z)
		}
	}
	cfg.Zones = zones
	if len(cfg.ResourceLabels) > 0 {
		labels := map[string]string{}
		for k, v := range cfg.ResourceLabels {
			if k = strings.TrimSpace(k); k != "" {
				labels[k] = strings.TrimSpace(v)
			}
		}
		cfg.ResourceLabels = labels
	}
	if platform != "aws" && platform != "gcp" {
		cfg.DataDiskIops, cfg.DataDiskThroughput = 0, 0
		cfg.ConfigsvrDiskType, cfg.BootDiskType, cfg.BootDiskSize = "", "", 0
		cfg.CompactPlacement, cfg.ResourceLabels, cfg.HardwarePreset = false, nil, ""
	}
	if platform != "gcp" {
		cfg.MinCpuPlatform, cfg.NicType, cfg.Tier1Networking, cfg.ZoneMode, cfg.Zones = "", "", false, "", nil
	}
}

// ── tfvars ─────────────────────────────────────────────────────────────────

func writeHardwareTfvars(platform string, cfg Config, write func(string), writeVar func(string, interface{})) {
	optStr := func(name, v string) {
		if v != "" {
			writeVar(name, v)
		}
	}
	optInt := func(name string, v int) {
		if v != 0 {
			writeVar(name, v)
		}
	}
	optInt("data_disk_iops", cfg.DataDiskIops)
	optInt("data_disk_throughput", cfg.DataDiskThroughput)
	optStr("configsvr_disk_type", cfg.ConfigsvrDiskType)
	optStr("boot_disk_type", cfg.BootDiskType)
	optInt("boot_disk_size", cfg.BootDiskSize)
	if cfg.CompactPlacement {
		writeVar("compact_placement", true)
	}
	if platform == "gcp" {
		optStr("min_cpu_platform", cfg.MinCpuPlatform)
		optStr("nic_type", cfg.NicType)
		if cfg.Tier1Networking {
			writeVar("tier1_networking", true)
		}
		optStr("zone_mode", cfg.ZoneMode)
		if len(cfg.Zones) > 0 {
			quoted := make([]string, len(cfg.Zones))
			for i, z := range cfg.Zones {
				quoted[i] = formatHCLVal(z)
			}
			write("zones = [" + strings.Join(quoted, ", ") + "]")
		}
	}
	if len(cfg.ResourceLabels) > 0 {
		keys := make([]string, 0, len(cfg.ResourceLabels))
		for k := range cfg.ResourceLabels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		write("labels = {")
		for _, k := range keys {
			write(fmt.Sprintf("  %s = %s", formatHCLVal(k), formatHCLVal(cfg.ResourceLabels[k])))
		}
		write("}")
	}
}

// ── Presets ────────────────────────────────────────────────────────────────

// HardwarePreset fills the Compute & Storage form. Fields maps form field
// names to values; every field listed in hardwareFormFields that a preset
// does not set is cleared, so the result never depends on the previous preset.
type HardwarePreset struct {
	ID          string                 `json:"id"`
	Label       string                 `json:"label"`
	Description string                 `json:"description"`
	Fields      map[string]interface{} `json:"fields"`
}

// hardwareFormFields lists the form fields a preset controls.
var hardwareFormFields = []string{
	"shardsvr_type", "shardsvr_volume_size", "configsvr_type", "configsvr_volume_size",
	"mongos_type", "arbiter_type", "replsetsvr_type", "replsetsvr_volume_size",
	"data_disk_type", "data_disk_iops", "data_disk_throughput", "configsvr_disk_type",
	"boot_disk_type", "boot_disk_size", "plgm_type", "compact_placement",
	// GCP
	"min_cpu_platform", "nic_type", "tier1_networking", "zone_mode",
	// AWS
	"subnet_count",
}

type atlasTier struct {
	name   string
	vcpu   int
	diskGB int
}

var atlasTiers = []atlasTier{{"M30", 2, 40}, {"M40", 4, 80}, {"M50", 8, 160}, {"M60", 16, 320}, {"M80", 32, 750}}

var awsSizeByVCPU = map[int]string{2: "large", 4: "xlarge", 8: "2xlarge", 16: "4xlarge", 32: "8xlarge"}

func hardwarePresets(platform string) []HardwarePreset {
	presets := []HardwarePreset{{
		ID: "dev", Label: "Dev (Terraform defaults)",
		Description: "Small burstable machines and default disks. Not suitable for benchmarks.",
		Fields:      map[string]interface{}{},
	}}
	switch platform {
	case "gcp":
		for _, t := range atlasTiers {
			plgm := "n2-highcpu-8"
			if t.vcpu >= 16 {
				plgm = "n2-highcpu-16"
			}
			mt := fmt.Sprintf("n2-standard-%d", t.vcpu)
			presets = append(presets, HardwarePreset{
				ID: "atlas-" + strings.ToLower(t.name), Label: "Atlas-like " + t.name,
				Description: fmt.Sprintf("%d vCPU / %d GB RAM data nodes (%s), %d GB pd-ssd, Ice Lake, gVNIC, single zone.", t.vcpu, t.vcpu*4, mt, t.diskGB),
				Fields: map[string]interface{}{
					"shardsvr_type": mt, "replsetsvr_type": mt, "mongos_type": mt,
					"configsvr_type": "n2-standard-2", "arbiter_type": "n2-standard-2",
					"shardsvr_volume_size": t.diskGB, "replsetsvr_volume_size": t.diskGB, "configsvr_volume_size": 20,
					"data_disk_type": "pd-ssd", "boot_disk_type": "pd-balanced", "boot_disk_size": 30,
					"min_cpu_platform": "Intel Ice Lake", "nic_type": "GVNIC", "zone_mode": "single",
					"plgm_type": plgm,
				},
			})
		}
		presets = append(presets,
			HardwarePreset{
				ID: "prod-io-heavy", Label: "Prod IO-heavy (C3 + Hyperdisk)",
				Description: "c3-standard-22 data nodes, 500 GB hyperdisk-balanced at 20k IOPS / 600 MiB/s, pd-ssd config servers, gVNIC, single zone.",
				Fields: map[string]interface{}{
					"shardsvr_type": "c3-standard-22", "replsetsvr_type": "c3-standard-22", "mongos_type": "c3-standard-8",
					"configsvr_type": "c3-standard-4", "arbiter_type": "c3-standard-4",
					"shardsvr_volume_size": 500, "replsetsvr_volume_size": 500, "configsvr_volume_size": 50,
					"data_disk_type": "hyperdisk-balanced", "data_disk_iops": 20000, "data_disk_throughput": 600,
					"configsvr_disk_type": "pd-ssd", "boot_disk_type": "pd-balanced", "boot_disk_size": 30,
					"nic_type": "GVNIC", "zone_mode": "single", "plgm_type": "c3-highcpu-22",
				},
			},
			HardwarePreset{
				ID: "prod-hyperdisk-c4", Label: "Prod Hyperdisk (C4)",
				Description: "c4-standard-16 data nodes, 500 GB hyperdisk-balanced at 16k IOPS / 400 MiB/s, Hyperdisk boot, gVNIC, single zone.",
				Fields: map[string]interface{}{
					"shardsvr_type": "c4-standard-16", "replsetsvr_type": "c4-standard-16", "mongos_type": "c4-standard-8",
					"configsvr_type": "c4-standard-4", "arbiter_type": "c4-standard-2",
					"shardsvr_volume_size": 500, "replsetsvr_volume_size": 500, "configsvr_volume_size": 50,
					"data_disk_type": "hyperdisk-balanced", "data_disk_iops": 16000, "data_disk_throughput": 400,
					"boot_disk_type": "hyperdisk-balanced", "boot_disk_size": 30,
					"nic_type": "GVNIC", "zone_mode": "single", "plgm_type": "c4-highcpu-16",
				},
			},
		)
	case "aws":
		for _, t := range atlasTiers {
			mt := "m6i." + awsSizeByVCPU[t.vcpu]
			plgm := "c6i.2xlarge"
			iops, tp := 3000, 125
			if t.vcpu >= 16 {
				plgm, iops, tp = "c6i.4xlarge", 6000, 250
			}
			presets = append(presets, HardwarePreset{
				ID: "atlas-" + strings.ToLower(t.name), Label: "Atlas-like " + t.name,
				Description: fmt.Sprintf("%d vCPU / %d GB RAM data nodes (%s), %d GB gp3 at %d IOPS / %d MiB/s, single AZ.", t.vcpu, t.vcpu*4, mt, t.diskGB, iops, tp),
				Fields: map[string]interface{}{
					"shardsvr_type": mt, "replsetsvr_type": mt, "mongos_type": mt,
					"configsvr_type": "m6i.large", "arbiter_type": "m6i.large",
					"shardsvr_volume_size": t.diskGB, "replsetsvr_volume_size": t.diskGB, "configsvr_volume_size": 20,
					"data_disk_type": "gp3", "data_disk_iops": iops, "data_disk_throughput": tp,
					"boot_disk_type": "gp3", "boot_disk_size": 30, "subnet_count": 1,
					"plgm_type": plgm,
				},
			})
		}
		presets = append(presets,
			HardwarePreset{
				ID: "prod-io-heavy", Label: "Prod IO-heavy (r6i + io2)",
				Description: "r6i.4xlarge data nodes, 500 GB io2 at 20k IOPS, gp3 config servers, single AZ.",
				Fields: map[string]interface{}{
					"shardsvr_type": "r6i.4xlarge", "replsetsvr_type": "r6i.4xlarge", "mongos_type": "c6i.2xlarge",
					"configsvr_type": "m6i.large", "arbiter_type": "m6i.large",
					"shardsvr_volume_size": 500, "replsetsvr_volume_size": 500, "configsvr_volume_size": 50,
					"data_disk_type": "io2", "data_disk_iops": 20000, "configsvr_disk_type": "gp3",
					"boot_disk_type": "gp3", "boot_disk_size": 30, "subnet_count": 1, "plgm_type": "c6i.4xlarge",
				},
			},
			HardwarePreset{
				ID: "prod-gp3-max", Label: "Prod gp3 max (m7i)",
				Description: "m7i.4xlarge data nodes, 500 GB gp3 at 16k IOPS / 1000 MiB/s, single AZ.",
				Fields: map[string]interface{}{
					"shardsvr_type": "m7i.4xlarge", "replsetsvr_type": "m7i.4xlarge", "mongos_type": "c7i.2xlarge",
					"configsvr_type": "m7i.large", "arbiter_type": "m7i.large",
					"shardsvr_volume_size": 500, "replsetsvr_volume_size": 500, "configsvr_volume_size": 50,
					"data_disk_type": "gp3", "data_disk_iops": 16000, "data_disk_throughput": 1000,
					"boot_disk_type": "gp3", "boot_disk_size": 30, "subnet_count": 1, "plgm_type": "c7i.4xlarge",
				},
			},
		)
	default:
		return nil
	}
	return presets
}

// suggestedMachineTypes feeds the machine type datalists in the form.
func suggestedMachineTypes(platform string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(hardwareDefaults[platform]["node_type"])
	for _, p := range hardwarePresets(platform) {
		for _, f := range []string{"shardsvr_type", "configsvr_type", "mongos_type", "arbiter_type", "plgm_type"} {
			if v, ok := p.Fields[f].(string); ok {
				add(v)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ── HTTP ───────────────────────────────────────────────────────────────────

// GET /api/hardware/presets?platform=gcp
func apiHardwarePresetsHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	if platform != "aws" && platform != "gcp" {
		jsonError(w, 400, "hardware presets are only available for aws and gcp")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"presets":       hardwarePresets(platform),
		"fields":        hardwareFormFields,
		"disk_types":    diskTypesFor(platform),
		"machine_types": suggestedMachineTypes(platform),
		"cpu_platforms": gcpCPUPlatforms,
	})
}

// POST /api/hardware/validate?platform=gcp  (body: Config JSON)
func apiHardwareValidateHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	var cfg Config
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
		jsonError(w, 400, "invalid config: "+err.Error())
		return
	}
	normalizeHardwareConfig(platform, &cfg)
	errs := validateHardwareConfig(platform, cfg)
	if errs == nil {
		errs = []string{}
	}
	writeJSON(w, 200, map[string]interface{}{"errors": errs})
}
