package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type EnvironmentProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Config   Config `json:"config"`
}

var profilesMu sync.Mutex

// Positive allowlists deliberately exclude credentials, arbitrary Ansible vars,
// endpoints, host ports, resource identities, labels, and audit filter contents.
// New Config fields are not reusable until explicitly reviewed here.
func profileConfig(cfg Config) Config {
	data, _ := json.Marshal(cfg)
	var source map[string]json.RawMessage
	_ = json.Unmarshal(data, &source)
	clean := selectProfileFields(source, `mongo_release mongodb_distribution mongo_version mongo_repo pbm_version pbm_repo pmm_client_version pmm_client_repo
		enable_plgm use_tls enable_ca ca_placement plgm_image plgm_os_image vault_encryption vault_version vault_image vault_type vault_volume_size vault_cpu_cores vault_memory_gb
		region location subnet_cidr subnet_count use_spot_instances enable_pmm pmm_type pmm_volume_size pmm_image pmm_image_channel pmm_disk_type enable_extensions extensions_image
		enable_backup create_backup_bucket backup_retention machine_image enable_seaweedfs delete_after_days os_image
		shardsvr_cpu_cores shardsvr_memory_gb configsvr_cpu_cores configsvr_memory_gb mongos_cpu_cores mongos_memory_gb arbiter_cpu_cores arbiter_memory_gb replsetsvr_cpu_cores replsetsvr_memory_gb
		seaweedfs_cpu_cores seaweedfs_memory_gb seaweedfs_volume_size pmm_cpu_cores pmm_memory_gb ca_cpu_cores ca_memory_gb ca_volume_size
		shardsvr_type shardsvr_volume_size configsvr_type configsvr_volume_size mongos_type arbiter_type replsetsvr_type replsetsvr_volume_size data_disk_type
		hardware_preset data_disk_iops data_disk_throughput configsvr_disk_type boot_disk_type boot_disk_size min_cpu_platform nic_type tier1_networking zone_mode compact_placement plgm_type`)
	topologyFields := `env_tag mongodb_distribution mongo_release mongo_version mongo_repo pbm_version pbm_repo pmm_client_version pmm_client_repo os_image
		enable_pmm enable_pbm enable_mongot use_tls mongot_source mongot_repo mongot_version psmdb_image pbm_image pmm_client_image mongot_image bind_to_localhost enable_audit
		configsvr_count mongos_count shard_count shardsvr_replicas arbiters_per_replset data_nodes_per_replset`
	for key, fields := range map[string]string{
		"clusters": topologyFields, "replsets": topologyFields,
		"pmm_servers":       `env_tag pmm_server_image bind_to_localhost enable_extensions extensions_image`,
		"pmm_bare_clients":  `base_image pmm_client_repo pmm_client_version os_image`,
		"seaweedfs_servers": `env_tag seaweedfs_image backup_retention bind_to_localhost`,
		"ldap_servers":      `env_tag ldap_image bind_to_localhost`,
	} {
		var entries map[string]map[string]json.RawMessage
		if json.Unmarshal(source[key], &entries) != nil {
			continue
		}
		filtered := make(map[string]map[string]json.RawMessage, len(entries))
		for name, entry := range entries {
			filtered[name] = selectProfileFields(entry, fields)
		}
		clean[key], _ = json.Marshal(filtered)
	}
	var syncFields map[string]json.RawMessage
	if json.Unmarshal(source["cluster_sync"], &syncFields) == nil {
		clean["cluster_sync"], _ = json.Marshal(selectProfileFields(syncFields, `enabled source_kind source_name target_kind target_name version repo image instance_type cpus memory_mb clone_parallel_collections clone_read_workers clone_insert_workers clone_segment_size replication_workers change_stream_batch_size event_queue_size bulk_ops_size`))
	}
	data, _ = json.Marshal(clean)
	var result Config
	_ = json.Unmarshal(data, &result)
	return result
}

func selectProfileFields(source map[string]json.RawMessage, fields string) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage)
	for _, key := range strings.Fields(fields) {
		if value, ok := source[key]; ok {
			result[key] = value
		}
	}
	return result
}

func loadProfilesLocked() (map[string]EnvironmentProfile, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, "environment-profiles.json"))
	if os.IsNotExist(err) {
		return map[string]EnvironmentProfile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles map[string]EnvironmentProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	if profiles == nil {
		return nil, errors.New("profiles must be a JSON object")
	}
	return profiles, nil
}

func writeProfilesLocked(profiles map[string]EnvironmentProfile) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dataDir, "environment-profiles.json"), data, 0600)
}

func environmentProfilesHandler(w http.ResponseWriter, r *http.Request) {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := loadProfilesLocked()
	if err != nil {
		jsonError(w, 500, "Could not read environment profiles")
		return
	}
	if r.Method == http.MethodGet {
		list := make([]EnvironmentProfile, 0, len(profiles))
		for _, profile := range profiles {
			if platform := r.URL.Query().Get("platform"); platform != "" && profile.Platform != platform {
				continue
			}
			profile.Config = profileConfig(profile.Config)
			list = append(list, profile)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		writeJSON(w, 200, map[string]any{"profiles": list})
		return
	}
	var profile EnvironmentProfile
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		jsonError(w, 400, "Invalid profile")
		return
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" || len(profile.Name) > 80 || !validPlatform(profile.Platform) {
		jsonError(w, 400, "A profile name (1–80 characters) and a valid platform are required")
		return
	}
	for _, existing := range profiles {
		if existing.Platform == profile.Platform && strings.EqualFold(existing.Name, profile.Name) {
			jsonError(w, 409, "A profile with this name already exists; choose a new name")
			return
		}
	}
	profile.ID = secureID(8)
	profile.Config = profileConfig(profile.Config)
	profiles[profile.ID] = profile
	if err := writeProfilesLocked(profiles); err != nil {
		jsonError(w, 500, "Could not save environment profile")
		return
	}
	writeJSON(w, 201, profile)
}

func deleteEnvironmentProfileHandler(w http.ResponseWriter, r *http.Request) {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := loadProfilesLocked()
	if err != nil {
		jsonError(w, 500, "Could not read environment profiles")
		return
	}
	id := r.PathValue("profile_id")
	if _, exists := profiles[id]; !exists {
		jsonError(w, 404, "Profile not found")
		return
	}
	delete(profiles, id)
	if err := writeProfilesLocked(profiles); err != nil {
		jsonError(w, 500, "Could not delete environment profile")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func environmentProfile(id, platform string) (EnvironmentProfile, error) {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := loadProfilesLocked()
	if err != nil {
		return EnvironmentProfile{}, err
	}
	profile, ok := profiles[id]
	if !ok || profile.Platform != platform {
		return EnvironmentProfile{}, fmt.Errorf("profile not found for this platform")
	}
	profile.Config = profileConfig(profile.Config)
	return profile, nil
}

func starterEnvironment(kind, platform string) (Config, error) {
	zero, off := 0, false
	cfg := Config{Clusters: map[string]ClusterConfig{}, Replsets: map[string]ReplsetConfig{}, EnableBackup: &off}
	if platform == "chaos" {
		cfg.DeleteAfterDays = 7
	}
	if platform == "aws" || platform == "gcp" {
		cfg.HardwarePreset = "dev"
	}
	replica := ReplsetConfig{EnvTag: "test", DataNodesPerReplset: 3, ArbitersPerReplset: &zero}
	cluster := ClusterConfig{EnvTag: "test", ConfigsvrCount: 3, MongosCount: 2, ShardCount: 2, ShardsvrReplicas: 3, ArbitersPerReplset: &zero}
	if platform != "docker" {
		replica.MongoDBDistribution = "psmdb"
		cluster.MongoDBDistribution = "psmdb"
	}
	switch kind {
	case "replica-set":
		cfg.Replsets["rs01"] = replica
	case "sharded":
		cfg.Clusters["cl01"] = cluster
	case "benchmark":
		cfg.Replsets["rs01"], cfg.EnablePlgm = replica, true
	case "migration":
		cfg.Replsets["rs01"], cfg.Replsets["rs02"] = replica, replica
		cfg.ClusterSync = ClusterSyncConfig{Enabled: true, SourceKind: "replset", SourceName: "rs01", TargetKind: "replset", TargetName: "rs02"}
	default:
		return Config{}, fmt.Errorf("unknown starter template")
	}
	return cfg, nil
}

// Reusable profiles intentionally contain no host ports. Allocate fresh ranges
// before rendering so a multi-replica-set starter never displays duplicate ports.
func startingPointDockerPorts(cfg *Config, state map[string]*Environment) error {
	users := make(map[int][]string)
	for _, env := range state {
		if env != nil && env.Platform == "docker" && env.Status == "running" {
			addDockerConfigPortUsers(env.Config, users, "")
		}
	}
	occupied := make(map[int]struct{}, len(users))
	for port := range users {
		occupied[port] = struct{}{}
	}
	next := 27017
	for _, item := range sortedReplsets(cfg.Replsets) {
		rs := item.Config
		for next <= 65000 && !dockerReplsetPortRangeFree(next, rs, occupied) {
			next += dockerReplsetPortStep
		}
		if next > 65000 {
			return fmt.Errorf("no free Docker replica-set port range")
		}
		rs.ReplsetPort, rs.ArbiterPort, rs.ArbiterBasePort = next, next, next+10
		for _, port := range dockerReplsetPorts(rs) {
			occupied[port] = struct{}{}
		}
		cfg.Replsets[item.Name] = rs
		next += dockerReplsetPortStep
	}
	return nil
}
