package main

import "sort"

// ChaosFirewallRule is a single ingress rule for CHAOS deployments.
type ChaosFirewallRule struct {
	CIDR    string `json:"cidr"`
	Port    string `json:"port"`
	Comment string `json:"comment,omitempty"`
}

// ClusterConfig maps to the Terraform "clusters" map object type.
type ClusterConfig struct {
	EnvTag             string `json:"env_tag"`
	ConfigsvrCount     int    `json:"configsvr_count"`
	ShardCount         int    `json:"shard_count"`
	ShardsvrReplicas   int    `json:"shardsvr_replicas"`
	ArbitersPerReplset *int   `json:"arbiters_per_replset,omitempty"`
	MongosCount        int    `json:"mongos_count"`
	// Non-Docker package source overrides. Empty values inherit Config defaults.
	MongoDBDistribution string `json:"mongodb_distribution,omitempty"`
	MongoRelease        string `json:"mongo_release,omitempty"`
	MongoVersion        string `json:"mongo_version,omitempty"`
	MongoRepo           string `json:"mongo_repo,omitempty"`
	PbmVersion          string `json:"pbm_version,omitempty"`
	PbmRepo             string `json:"pbm_repo,omitempty"`
	PmmClientVersion    string `json:"pmm_client_version,omitempty"`
	PmmClientRepo       string `json:"pmm_client_repo,omitempty"`
	OsImage             string `json:"os_image,omitempty"`
	// Per-topology PMM/PBM toggles. Docker uses them for containers; non-Docker uses them for agents.
	EnablePmm     *bool  `json:"enable_pmm,omitempty"`
	EnablePbm     *bool  `json:"enable_pbm,omitempty"`
	EnableMongot  *bool  `json:"enable_mongot,omitempty"`
	UseTLS        *bool  `json:"use_tls,omitempty"`
	MongotSource  string `json:"mongot_source,omitempty"`
	MongotRepo    string `json:"mongot_repo,omitempty"`
	MongotVersion string `json:"mongot_version,omitempty"`
	// Docker-only
	PsmdbImage      string `json:"psmdb_image,omitempty"`
	PbmImage        string `json:"pbm_image,omitempty"`
	PmmClientImage  string `json:"pmm_client_image,omitempty"`
	MongotImage     string `json:"mongot_image,omitempty"`
	BindToLocalhost bool   `json:"bind_to_localhost,omitempty"`
	EnableAudit     *bool  `json:"enable_audit,omitempty"`
	AuditFilter     string `json:"audit_filter,omitempty"`
	LdapServer      string `json:"ldap_server,omitempty"`
}

// ReplsetConfig maps to the Terraform "replsets" map object type.
type ReplsetConfig struct {
	EnvTag              string `json:"env_tag"`
	DataNodesPerReplset int    `json:"data_nodes_per_replset"`
	ArbitersPerReplset  *int   `json:"arbiters_per_replset,omitempty"`
	// Non-Docker package source overrides. Empty values inherit Config defaults.
	MongoDBDistribution string `json:"mongodb_distribution,omitempty"`
	MongoRelease        string `json:"mongo_release,omitempty"`
	MongoVersion        string `json:"mongo_version,omitempty"`
	MongoRepo           string `json:"mongo_repo,omitempty"`
	PbmVersion          string `json:"pbm_version,omitempty"`
	PbmRepo             string `json:"pbm_repo,omitempty"`
	PmmClientVersion    string `json:"pmm_client_version,omitempty"`
	PmmClientRepo       string `json:"pmm_client_repo,omitempty"`
	OsImage             string `json:"os_image,omitempty"`
	// Docker-only port assignment: starting port for data nodes and arbiters.
	// Auto-assigned on save to avoid collisions between multiple replica sets.
	ReplsetPort     int `json:"replset_port,omitempty"`
	ArbiterPort     int `json:"arbiter_port,omitempty"`
	ArbiterBasePort int `json:"arbiter_base_port,omitempty"`
	// Per-topology PMM/PBM toggles. Docker uses them for containers; non-Docker uses them for agents.
	EnablePmm     *bool  `json:"enable_pmm,omitempty"`
	EnablePbm     *bool  `json:"enable_pbm,omitempty"`
	EnableMongot  *bool  `json:"enable_mongot,omitempty"`
	UseTLS        *bool  `json:"use_tls,omitempty"`
	MongotSource  string `json:"mongot_source,omitempty"`
	MongotRepo    string `json:"mongot_repo,omitempty"`
	MongotVersion string `json:"mongot_version,omitempty"`
	// Docker-only
	PsmdbImage      string `json:"psmdb_image,omitempty"`
	PbmImage        string `json:"pbm_image,omitempty"`
	PmmClientImage  string `json:"pmm_client_image,omitempty"`
	MongotImage     string `json:"mongot_image,omitempty"`
	BindToLocalhost bool   `json:"bind_to_localhost,omitempty"`
	EnableAudit     *bool  `json:"enable_audit,omitempty"`
	AuditFilter     string `json:"audit_filter,omitempty"`
	LdapServer      string `json:"ldap_server,omitempty"`
}

// PmmServerConfig maps to the Docker pmm_servers map object type.
type PmmServerConfig struct {
	EnvTag           string `json:"env_tag"`
	PmmServerImage   string `json:"pmm_server_image,omitempty"`
	PmmPort          int    `json:"pmm_port,omitempty"`
	PmmExternalPort  int    `json:"pmm_external_port,omitempty"`
	PmmServerUser    string `json:"pmm_server_user,omitempty"`
	PmmServerPwd     string `json:"pmm_server_pwd,omitempty"`
	BindToLocalhost  bool   `json:"bind_to_localhost,omitempty"`
	EnableExtensions bool   `json:"enable_extensions,omitempty"`
	ExtensionsImage  string `json:"extensions_image,omitempty"`
}

// PmmBareClientConfig maps to the pmm_bare_clients map object type: a host
// with only a PMM client. BaseImage is Docker's, OsImage is CHAOS's.
type PmmBareClientConfig struct {
	BaseImage        string `json:"base_image,omitempty"`
	PmmClientRepo    string `json:"pmm_client_repo,omitempty"`
	PmmClientVersion string `json:"pmm_client_version,omitempty"`
	OsImage          string `json:"os_image,omitempty"`
}

// defaultExtensionsImage matches the extensions_image default of the Terraform targets.
const defaultExtensionsImage = "percona/pmm-extensions:3.10.0-rc1"

// SeaweedFSServerConfig maps to the Docker seaweedfs_servers map object type.
type SeaweedFSServerConfig struct {
	EnvTag             string `json:"env_tag"`
	SeaweedFSImage     string `json:"seaweedfs_image,omitempty"`
	SeaweedFSPort      int    `json:"seaweedfs_port,omitempty"`
	SeaweedFSAdminPort int    `json:"seaweedfs_admin_port,omitempty"`
	SeaweedFSAccessKey string `json:"seaweedfs_access_key,omitempty"`
	SeaweedFSSecretKey string `json:"seaweedfs_secret_key,omitempty"`
	BucketName         string `json:"bucket_name,omitempty"`
	BackupRetention    int    `json:"backup_retention,omitempty"`
	BindToLocalhost    bool   `json:"bind_to_localhost,omitempty"`
}

// LdapServerConfig maps to the ldap_servers map object type.
type LdapServerConfig struct {
	EnvTag            string `json:"env_tag"`
	LdapImage         string `json:"ldap_image,omitempty"`
	LdapPort          int    `json:"ldap_port,omitempty"`
	LdapDomain        string `json:"ldap_domain,omitempty"`
	LdapAdminPassword string `json:"ldap_admin_password,omitempty"`
	BindToLocalhost   bool   `json:"bind_to_localhost,omitempty"`
}

// ClusterSyncConfig configures one PCSM source-to-target relationship.
type ClusterSyncConfig struct {
	Enabled                  bool     `json:"enabled,omitempty"`
	SourceKind               string   `json:"source_kind,omitempty"`
	SourceName               string   `json:"source_name,omitempty"`
	TargetKind               string   `json:"target_kind,omitempty"`
	TargetName               string   `json:"target_name,omitempty"`
	Version                  string   `json:"version,omitempty"`
	Repo                     string   `json:"repo,omitempty"`
	Image                    string   `json:"image,omitempty"`
	InstanceType             string   `json:"instance_type,omitempty"`
	CPUs                     int      `json:"cpus,omitempty"`
	MemoryMB                 int      `json:"memory_mb,omitempty"`
	IncludeNamespaces        []string `json:"include_namespaces,omitempty"`
	ExcludeNamespaces        []string `json:"exclude_namespaces,omitempty"`
	CloneParallelCollections int      `json:"clone_parallel_collections,omitempty"`
	CloneReadWorkers         int      `json:"clone_read_workers,omitempty"`
	CloneInsertWorkers       int      `json:"clone_insert_workers,omitempty"`
	CloneSegmentSize         string   `json:"clone_segment_size,omitempty"`
	ReplicationWorkers       int      `json:"replication_workers,omitempty"`
	ChangeStreamBatchSize    int      `json:"change_stream_batch_size,omitempty"`
	EventQueueSize           int      `json:"event_queue_size,omitempty"`
	WorkerQueueSize          int      `json:"worker_queue_size,omitempty"`
	BulkOpsSize              int      `json:"bulk_ops_size,omitempty"`
}

// Config holds all user-configurable settings for an environment.
type Config struct {
	// General
	Prefix              string            `json:"prefix"`
	MongoRelease        string            `json:"mongo_release,omitempty"`
	MongoDBDistribution string            `json:"mongodb_distribution,omitempty"`
	MongoVersion        string            `json:"mongo_version,omitempty"`
	MongoRepo           string            `json:"mongo_repo,omitempty"`
	PbmVersion          string            `json:"pbm_version,omitempty"`
	PbmRepo             string            `json:"pbm_repo,omitempty"`
	PmmClientVersion    string            `json:"pmm_client_version,omitempty"`
	PmmClientRepo       string            `json:"pmm_client_repo,omitempty"`
	EnablePlgm          bool              `json:"enable_plgm,omitempty"`
	UseTLS              bool              `json:"use_tls,omitempty"`
	EnableCA            *bool             `json:"enable_ca,omitempty"`
	CAPlacement         string            `json:"ca_placement,omitempty"`
	PlgmImage           string            `json:"plgm_image,omitempty"`
	PlgmOsImage         string            `json:"plgm_os_image,omitempty"`
	PlgmContainerSuffix string            `json:"plgm_container_suffix,omitempty"`
	PlgmExternalPort    int               `json:"plgm_external_port,omitempty"` // Docker: host port of the plgm Web UI
	PlgmLocalPort       string            `json:"plgm_local_port,omitempty"`    // Cloud: local SSH-forwarded port of the plgm Web UI
	ClusterSync         ClusterSyncConfig `json:"cluster_sync,omitempty"`
	VaultEncryption     bool              `json:"vault_encryption,omitempty"`
	VaultVersion        string            `json:"vault_version,omitempty"`
	VaultImage          string            `json:"vault_image,omitempty"`
	VaultType           string            `json:"vault_type,omitempty"`
	VaultVolumeSize     int               `json:"vault_volume_size,omitempty"`
	VaultCPUCores       int               `json:"vault_cpu_cores,omitempty"`
	VaultMemoryGB       int               `json:"vault_memory_gb,omitempty"`

	// Cloud credentials / settings
	ProjectID         string `json:"project_id,omitempty"`
	Region            string `json:"region,omitempty"`
	Location          string `json:"location,omitempty"`
	SubnetCIDR        string `json:"subnet_cidr,omitempty"`
	SubnetCount       int    `json:"subnet_count,omitempty"`
	SourceRanges      string `json:"source_ranges,omitempty"`
	MySSHUser         string `json:"my_ssh_user,omitempty"`
	SSHPublicKeyPath  string `json:"ssh_public_key_path,omitempty"`
	SSHPrivateKeyPath string `json:"ssh_private_key_path,omitempty"`
	DefaultKeyPair    string `json:"default_key_pair,omitempty"`
	EnableSSHGateway  bool   `json:"enable_ssh_gateway,omitempty"`
	SSHGatewayName    string `json:"ssh_gateway_name,omitempty"`
	PortToForward     string `json:"port_to_forward,omitempty"`
	UseSpotInstances  bool   `json:"use_spot_instances,omitempty"`
	DefaultVpcName    string `json:"default_vpc_name,omitempty"`

	// SSH users map — key=username, value=path to public key file.
	SSHUsers map[string]string `json:"ssh_users,omitempty"`

	// PMM (cloud)
	EnablePmm       *bool  `json:"enable_pmm,omitempty"`
	PmmType         string `json:"pmm_type,omitempty"`
	PmmVolumeSize   int    `json:"pmm_volume_size,omitempty"`
	PmmPort         int    `json:"pmm_port,omitempty"`
	PmmImage        string `json:"pmm_image,omitempty"`
	PmmImageChannel string `json:"pmm_image_channel,omitempty"`
	PmmDiskType     string `json:"pmm_disk_type,omitempty"`

	// PMM Extensions side-car on the PMM server (CHAOS)
	EnableExtensions bool   `json:"enable_extensions,omitempty"`
	ExtensionsImage  string `json:"extensions_image,omitempty"`

	// Backup
	// EnableBackup is the global PBM backup toggle on AWS/GCP/Azure/CHAOS.
	// When off, no backup storage is created or used. nil means the legacy
	// behaviour (see backupEnabled and chaosBackupStorage).
	EnableBackup *bool `json:"enable_backup,omitempty"`
	// CreateBackupBucket controls whether backup storage is created (true) or
	// an existing bucket is used (false). On AWS/GCP/Azure "create" means a
	// cloud bucket (S3/GCS/Azure Blob); on CHAOS it means a SeaweedFS VM.
	// nil means true (legacy default).
	CreateBackupBucket *bool  `json:"create_backup_bucket,omitempty"`
	DefaultBucketName  string `json:"default_bucket_name,omitempty"`
	BackupRetention    int    `json:"backup_retention,omitempty"`
	// Existing bucket used for PBM when CreateBackupBucket is false.
	// AWS: S3 bucket + region + access key ID/secret.
	// GCP: GCS bucket + HMAC access ID/secret.
	// Azure: blob container + storage account + account key (secret).
	// CHAOS: S3-compatible bucket + endpoint URL + region + access key ID/secret.
	ExistingBucketName      string `json:"existing_bucket_name,omitempty"`
	ExistingBucketEndpoint  string `json:"existing_bucket_endpoint,omitempty"`
	ExistingBucketRegion    string `json:"existing_bucket_region,omitempty"`
	ExistingBucketAccessKey string `json:"existing_bucket_access_key,omitempty"`
	ExistingBucketSecretKey string `json:"existing_bucket_secret_key,omitempty"`
	ExistingStorageAccount  string `json:"existing_storage_account,omitempty"`

	// Machine image / AMI selected for cloud instances.
	MachineImage string `json:"machine_image,omitempty"`

	// CHAOS-specific settings
	ChaosApiTokenPath   string `json:"chaos_api_token_path,omitempty"`
	LegacyChaosAPIToken string `json:"chaos_api_token,omitempty"`
	EnableSeaweedFS     *bool  `json:"enable_seaweedfs,omitempty"`
	DeleteAfterDays     int    `json:"delete_after_days,omitempty"`
	OsImage             string `json:"os_image,omitempty"`
	// FirewallRules replaces the old SourceRanges single string for CHAOS.
	// Each entry is an independent ingress rule with its own CIDR and port.
	FirewallRules       []ChaosFirewallRule `json:"firewall_rules,omitempty"`
	ShardsvrCpuCores    int                 `json:"shardsvr_cpu_cores,omitempty"`
	ShardsvrMemoryGb    int                 `json:"shardsvr_memory_gb,omitempty"`
	ConfigsvrCpuCores   int                 `json:"configsvr_cpu_cores,omitempty"`
	ConfigsvrMemoryGb   int                 `json:"configsvr_memory_gb,omitempty"`
	MongosCpuCores      int                 `json:"mongos_cpu_cores,omitempty"`
	MongosMemoryGb      int                 `json:"mongos_memory_gb,omitempty"`
	ArbiterCpuCores     int                 `json:"arbiter_cpu_cores,omitempty"`
	ArbiterMemoryGb     int                 `json:"arbiter_memory_gb,omitempty"`
	ReplsetSvrCpuCores  int                 `json:"replsetsvr_cpu_cores,omitempty"`
	ReplsetSvrMemoryGb  int                 `json:"replsetsvr_memory_gb,omitempty"`
	SeaweedFSCpuCores   int                 `json:"seaweedfs_cpu_cores,omitempty"`
	SeaweedFSMemoryGb   int                 `json:"seaweedfs_memory_gb,omitempty"`
	SeaweedFSVolumeSize int                 `json:"seaweedfs_volume_size,omitempty"`
	SeaweedFSPort       int                 `json:"seaweedfs_port,omitempty"`
	SeaweedFSAdminPort  int                 `json:"seaweedfs_admin_port,omitempty"`
	SeaweedFSAccessKey  string              `json:"seaweedfs_access_key,omitempty"`
	SeaweedFSSecretKey  string              `json:"seaweedfs_secret_key,omitempty"`
	PmmCpuCores         int                 `json:"pmm_cpu_cores,omitempty"`
	PmmMemoryGb         int                 `json:"pmm_memory_gb,omitempty"`
	CACpuCores          int                 `json:"ca_cpu_cores,omitempty"`
	CAMemoryGb          int                 `json:"ca_memory_gb,omitempty"`
	CAVolumeSize        int                 `json:"ca_volume_size,omitempty"`

	// Per-component instance types and disk sizes (cloud platforms only).
	ShardsvrType         string `json:"shardsvr_type,omitempty"`
	ShardsvrVolumeSize   int    `json:"shardsvr_volume_size,omitempty"`
	ConfigsvrType        string `json:"configsvr_type,omitempty"`
	ConfigsvrVolumeSize  int    `json:"configsvr_volume_size,omitempty"`
	MongosType           string `json:"mongos_type,omitempty"`
	ArbiterType          string `json:"arbiter_type,omitempty"`
	ReplsetSvrType       string `json:"replsetsvr_type,omitempty"`
	ReplsetSvrVolumeSize int    `json:"replsetsvr_volume_size,omitempty"`
	DataDiskType         string `json:"data_disk_type,omitempty"`

	// Benchmark hardware tunables (AWS / GCP). Empty values keep Terraform defaults.
	HardwarePreset     string            `json:"hardware_preset,omitempty"`
	DataDiskIops       int               `json:"data_disk_iops,omitempty"`
	DataDiskThroughput int               `json:"data_disk_throughput,omitempty"`
	ConfigsvrDiskType  string            `json:"configsvr_disk_type,omitempty"`
	BootDiskType       string            `json:"boot_disk_type,omitempty"`
	BootDiskSize       int               `json:"boot_disk_size,omitempty"`
	MinCpuPlatform     string            `json:"min_cpu_platform,omitempty"` // GCP only
	NicType            string            `json:"nic_type,omitempty"`         // GCP only
	Tier1Networking    bool              `json:"tier1_networking,omitempty"` // GCP only
	ZoneMode           string            `json:"zone_mode,omitempty"`        // GCP only: spread | single
	Zones              []string          `json:"zones,omitempty"`            // GCP only
	CompactPlacement   bool              `json:"compact_placement,omitempty"`
	PlgmType           string            `json:"plgm_type,omitempty"`
	ResourceLabels     map[string]string `json:"labels,omitempty"`

	// Docker networking
	NetworkName string `json:"network_name,omitempty"`

	// Topology
	Clusters map[string]ClusterConfig `json:"clusters"`
	Replsets map[string]ReplsetConfig `json:"replsets"`

	// Docker-specific service servers
	PmmServers       map[string]PmmServerConfig       `json:"pmm_servers,omitempty"`
	PmmBareClients   map[string]PmmBareClientConfig   `json:"pmm_bare_clients,omitempty"`
	SeaweedFSServers map[string]SeaweedFSServerConfig `json:"seaweedfs_servers,omitempty"`
	LdapServers      map[string]LdapServerConfig      `json:"ldap_servers,omitempty"`

	// Ansible variable overrides passed via --extra-vars at playbook runtime.
	AnsibleVars map[string]string `json:"ansible_vars,omitempty"`
}

// HistoryEvent records a single user-initiated action and its outcome.
type HistoryEvent struct {
	Action       string `json:"action"`
	StartedAt    string `json:"started_at"`
	Status       string `json:"status"` // "success", "failed", "cancelled"
	DurationSecs int64  `json:"duration_secs,omitempty"`
}

// Environment is one record in the state file.
type Environment struct {
	Platform string `json:"platform"`
	Config   Config `json:"config"`
	// LastAppliedConfig is the topology/configuration from the last successful
	// deploy. It is used to distinguish supported scale-out changes from
	// unsupported topology mutations before running Terraform.
	LastAppliedConfig *Config        `json:"last_applied_config,omitempty"`
	Status            string         `json:"status"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
	LastJobID         string         `json:"last_job_id,omitempty"`
	History           []HistoryEvent `json:"history,omitempty"`
	// HostIPs caches the last-known IP address for each Docker container so
	// that the UI can continue to display addresses even when containers are
	// stopped (and docker inspect returns an empty IP).
	HostIPs map[string]string `json:"host_ips,omitempty"`
}

// ─── Named pair helpers (sorted map iteration for templates) ──────────────────

type NamedCluster struct {
	Name   string
	Config ClusterConfig
}
type NamedReplset struct {
	Name   string
	Config ReplsetConfig
}
type NamedPmmServer struct {
	Name   string
	Config PmmServerConfig
}
type NamedPmmBareClient struct {
	Name   string
	Config PmmBareClientConfig
}
type NamedSeaweedFSServer struct {
	Name   string
	Config SeaweedFSServerConfig
}
type NamedLdapServer struct {
	Name   string
	Config LdapServerConfig
}

func sortedClusters(m map[string]ClusterConfig) []NamedCluster {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NamedCluster, 0, len(keys))
	for _, k := range keys {
		out = append(out, NamedCluster{k, m[k]})
	}
	return out
}

func sortedReplsets(m map[string]ReplsetConfig) []NamedReplset {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NamedReplset, 0, len(keys))
	for _, k := range keys {
		out = append(out, NamedReplset{k, m[k]})
	}
	return out
}

func sortedPmmBareClients(m map[string]PmmBareClientConfig) []NamedPmmBareClient {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NamedPmmBareClient, 0, len(keys))
	for _, k := range keys {
		out = append(out, NamedPmmBareClient{k, m[k]})
	}
	return out
}

func sortedPmmServers(m map[string]PmmServerConfig) []NamedPmmServer {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NamedPmmServer, 0, len(keys))
	for _, k := range keys {
		out = append(out, NamedPmmServer{k, m[k]})
	}
	return out
}

func sortedSeaweedFSServers(m map[string]SeaweedFSServerConfig) []NamedSeaweedFSServer {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NamedSeaweedFSServer, 0, len(keys))
	for _, k := range keys {
		out = append(out, NamedSeaweedFSServer{k, m[k]})
	}
	return out
}

func sortedLdapServers(m map[string]LdapServerConfig) []NamedLdapServer {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]NamedLdapServer, 0, len(keys))
	for _, k := range keys {
		out = append(out, NamedLdapServer{k, m[k]})
	}
	return out
}

// ─── Template data structs ────────────────────────────────────────────────────

type EnvEntry struct {
	ID  string
	Env *Environment
}

type AppSettings struct {
	ChaosApiTokenPath    string `json:"chaos_api_token_path,omitempty"`
	SSHUser              string `json:"ssh_user,omitempty"`
	AWSSSHUser           string `json:"aws_ssh_user,omitempty"`
	GCPSSHUser           string `json:"gcp_ssh_user,omitempty"`
	AzureSSHUser         string `json:"azure_ssh_user,omitempty"`
	ChaosSSHUser         string `json:"chaos_ssh_user,omitempty"`
	SSHPublicKeyPath     string `json:"ssh_public_key_path,omitempty"`
	SSHPrivateKeyPath    string `json:"ssh_private_key_path,omitempty"`
	TerraformParallelism int    `json:"terraform_parallelism,omitempty"`
	AWSProfile           string `json:"aws_profile,omitempty"`
	AWSRegion            string `json:"aws_region,omitempty"`
	GCPProjectID         string `json:"gcp_project_id,omitempty"`
	GCPServiceAccountKey string `json:"gcp_service_account_key,omitempty"`
	AzureTenantID        string `json:"azure_tenant_id,omitempty"`
	AzureSubscriptionID  string `json:"azure_subscription_id,omitempty"`
	AzureClientID        string `json:"azure_client_id,omitempty"`
}

type IndexData struct {
	Environments []EnvEntry
	HasDeleted   bool
	Settings     AppSettings
	LocalSSHUser string
}

type NewEnvData struct {
	Platforms []string
}

type ConfigureData struct {
	Platform                     string
	EnvID                        string
	Config                       Config
	Regions                      []string
	DefaultAuditFilter           string
	OSUser                       string // current OS user, used as SSH user default
	DockerDefaultPmmExternalPort int
	DefaultImages                DefaultImages
	// Machine images already cached server-side, keyed by region (AWS/GCP).
	PreloadedImages                 map[string]map[string][]CloudImage
	DefaultRegion                   string
	DockerDefaultSeaweedFSPort      int
	DockerDefaultSeaweedFSAdminPort int
	PSMDBVersions                   []string
	OfficialMongoDBVersions         []string
	// PBMVersions holds a flat sorted-descending list of all available PBM package
	// versions (e.g. ["2.7.0", "2.6.1", "2.6.0", ...]). PBM uses a single Percona
	// repository so there is no per-major-version grouping.
	PBMVersions []string
	// PSMDBMinorVersions maps major release key → sorted minor versions
	// e.g. {"psmdb-70": ["7.0.12", "7.0.11", ...]}
	PSMDBMinorVersions map[string][]string
	PCSMVersions       []string
	PMMImages          []string
	PSMDBImages        []string
	PBMImages          []string
	PMMClientImages    []string
	MongotImages       []string
	// Pre-sorted for templates
	SortedClusters    []NamedCluster
	SortedReplsets    []NamedReplset
	SortedPmmServers  []NamedPmmServer
	SortedBareClients []NamedPmmBareClient
	SortedSeaweedFS   []NamedSeaweedFSServer
	SortedLdap        []NamedLdapServer
}

type EnvironmentData struct {
	EnvID                string
	Env                  *Environment
	SortedClusters       []NamedCluster
	SortedReplsets       []NamedReplset
	ServiceURLs          []ServiceURL
	ClusterSync          ClusterSyncConfig
	ClusterSyncAvailable bool
}

// HostInfo describes a single running host or container.
type HostInfo struct {
	Name       string `json:"name"`
	IP         string `json:"ip"`
	PrivateIP  string `json:"private_ip"`
	PublicIP   string `json:"public_ip"`
	Port       string `json:"port"`
	ConnectCmd string `json:"connect_cmd"`
	Role       string `json:"role"`
	Group      string `json:"group"`
}

// ServiceURL describes a web service console with an openable URL.
type ServiceURL struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	URL   string `json:"url"`
	// Hint is an optional tooltip shown on the link button, used when extra
	// steps are needed before the URL works (e.g. opening an SSH tunnel).
	Hint string `json:"hint,omitempty"`
}

// MongoConnInfo describes a MongoDB connection string for a cluster or replica set.
type MongoConnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	ConnString string `json:"conn_string"`
	ConnUser   string `json:"conn_user"`
	ConnPass   string `json:"conn_pass"`
}

// CloudImage represents a single machine image available for a region.
type CloudImage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PrereqTool describes a single prerequisite tool and whether it is installed.
type PrereqTool struct {
	Name        string   `json:"name"`
	Installed   bool     `json:"installed"`
	InstallDoc  string   `json:"install_doc"`  // short URL / reference
	InstallCmds []string `json:"install_cmds"` // copy-pasteable shell commands
}

// PrereqResult is the JSON response from GET /api/prerequisites/{platform}.
type PrereqResult struct {
	Platform string       `json:"platform"`
	OK       bool         `json:"ok"` // true when all tools are installed
	Tools    []PrereqTool `json:"tools"`
}
