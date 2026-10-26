################
# Project
################

variable "prefix" {
  type        = string
  default     = "ig"
  description = "Prefix to be applied to the resources created, make sure to change it to avoid collisions with other users projects"
}

variable "vault_encryption" {
  type        = bool
  default     = false
  description = "Enable Vault-backed encryption at rest for every MongoDB topology in this environment."
}

# By default we deploy 1 sharded cluster, named ig-cl01. Make sure to change the default name and prefix (ig-cl01) to avoid duplicates. The configuration can be customized by adding the optional values listed.
variable "clusters" {
  description = "MongoDB clusters to deploy"
  type = map(object({
    env_tag              = optional(string, "test") # Name of Environment for the cluster
    configsvr_count      = optional(number, 3)      # Number of config servers to be used
    shard_count          = optional(number, 2)      # Number of shards to be used
    shardsvr_replicas    = optional(number, 2)      # How many data bearing nodes per shard
    arbiters_per_replset = optional(number, 1)      # Number of arbiters per replica set
    mongos_count         = optional(number, 2)      # Number of mongos to provision
    enable_audit         = optional(bool, false)    # Enable audit logging
    use_tls              = optional(bool, false)    # Use TLS for this cluster
    audit_filter         = optional(string, "")     # Optional audit filter override
    mongodb_distribution = optional(string, "")
    mongo_release        = optional(string, "")
    mongo_version        = optional(string, "")
    mongo_repo           = optional(string, "")
    pbm_version          = optional(string, "")
    pbm_repo             = optional(string, "")
    pmm_client_version   = optional(string, "")
    pmm_client_repo      = optional(string, "")
    enable_pmm           = optional(bool, true)
    enable_pbm           = optional(bool, true)
    enable_mongot        = optional(bool, false)
    mongot_source        = optional(string, "")
    mongot_repo          = optional(string, "")
    mongot_version       = optional(string, "")
    ldap_server          = optional(string, "")
  }))

  default = {
    ig-cl01 = {
      env_tag = "test"
    }
    #    ig-cl02 = {
    #      env_tag = "prod"
    #      mongos_count = 1
    #   }
  }
}

# By default, no replica sets are deployed (except those needed for the sharded clusters).
# If you want to provision separate replica sets, uncomment the default below. Make sure to change the default name and prefix (ig-rs01) to avoid duplicates. 
variable "replsets" {
  description = "MongoDB replica sets to deploy"
  type = map(object({
    env_tag                = optional(string, "test") # Name of Environment
    data_nodes_per_replset = optional(number, 2)      # Number of data bearing members per replset
    arbiters_per_replset   = optional(number, 1)      # Number of arbiters per replica set
    enable_audit           = optional(bool, false)    # Enable audit logging
    use_tls                = optional(bool, false)    # Use TLS for this replica set
    audit_filter           = optional(string, "")     # Optional audit filter override
    mongodb_distribution   = optional(string, "")
    mongo_release          = optional(string, "")
    mongo_version          = optional(string, "")
    mongo_repo             = optional(string, "")
    pbm_version            = optional(string, "")
    pbm_repo               = optional(string, "")
    pmm_client_version     = optional(string, "")
    pmm_client_repo        = optional(string, "")
    enable_pmm             = optional(bool, true)
    enable_pbm             = optional(bool, true)
    enable_mongot          = optional(bool, false)
    mongot_source          = optional(string, "")
    mongot_repo            = optional(string, "")
    mongot_version         = optional(string, "")
    ldap_server            = optional(string, "")
  }))

  default = {
    #    ig-rs01 = {
    #      env_tag = "test"
    #    }
    #    ig-rs02 = {
    #      env_tag = "prod"
    #    }
  }
}

variable "ldap_servers" {
  description = "LDAP servers to deploy. MongoDB clusters and replica sets select one with ldap_server."
  type = map(object({
    domain         = optional(string, "example.com")
    organization   = optional(string, "Example Inc")
    admin_password = optional(string, "admin")
    instance_type  = optional(string, "t3.small")
    users = optional(list(object({
      uid      = string
      cn       = string
      sn       = string
      password = string
    })), [])
    mongodb_users = optional(list(object({
      user  = string
      roles = list(string)
    })), [])
  }))
  default = {}
}

variable "ssh_public_key_path" {
  description = "SSH public key file to be added to authorized_keys"
  default     = "your_ssh_username.pub"
}

variable "my_ssh_user" {
  default = "ec2-user" # For Centos AMIs
  #default = "ubuntu" # For Ubuntu AMIs
  description = "Used to auto-generate the ssh_config file. Each person running this code should set it to its own SSH user name"
}

variable "ssh_private_key_path" {
  description = "Optional SSH private key file used by generated Ansible inventory and ssh_config. Empty uses ssh-agent/default SSH behavior."
  type        = string
  default     = ""
}

variable "default_key_pair" {
  description = "Base key pair name"
  type        = string
  default     = "key"
}

locals {
  my_key_pair = "${var.prefix}-${var.my_ssh_user}-${var.default_key_pair}"
}

variable "enable_ssh_gateway" {
  type        = bool
  default     = false
  description = "Adds proxycommand lines with a gateway/jump host to the generated ssh_config file"
}

variable "ssh_gateway_name" {
  type        = string
  default     = "gateway"
  description = "Name of your jump host to use for ssh_config"
}

variable "port_to_forward" {
  type        = string
  default     = "23443"
  description = "Local port number to forward via SSH to access PMM UI over localhost"
}

variable "plgm_local_port" {
  type        = string
  default     = "29999"
  description = "Local port forwarded via SSH to the plgm Web UI (127.0.0.1:9999 on the plgm host)"
}

#############
# PMM
#############

variable "default_pmm_host" {
  description = "Base PMM host name"
  type        = string
  default     = "pmm-server"
}

locals {
  pmm_host = "${var.prefix}-${var.default_pmm_host}"
}

variable "pmm_disk_type" {
  default = "gp2"
}

variable "pmm_type" {
  default     = "t3.large"
  description = "instance type of the PMM server"
}

variable "pmm_volume_size" {
  default     = "100"
  description = "storage size for the PMM server"
}

variable "pmm_port" {
  type    = number
  default = 8443
}

variable "pmm_image" {
  type        = string
  default     = "docker.io/percona/pmm-server:latest"
  description = "Docker image used by Ansible to run the PMM server container."
}

variable "enable_pmm" {
  type        = bool
  default     = true
  description = "Deploy a PMM monitoring server. Set to false to skip PMM entirely."
}

#############
# Percona ClusterSync
#############

variable "enable_pcsm" {
  type        = bool
  default     = false
  description = "Deploy one dedicated Percona ClusterSync VM for this environment."
}

variable "pcsm_version" {
  type        = string
  default     = "0.9.0"
  description = "Exact Percona ClusterSync package version installed by Ansible."
}

variable "pcsm_source_kind" {
  type        = string
  default     = ""
  description = "PCSM source topology kind: cluster (mongos hosts) or replset (electable members)."

  validation {
    condition     = !var.enable_pcsm || contains(["cluster", "replset"], var.pcsm_source_kind)
    error_message = "pcsm_source_kind must be cluster or replset when PCSM is enabled."
  }

  validation {
    condition     = !var.enable_pcsm || (var.pcsm_source_kind == "cluster" ? contains(keys(var.clusters), var.pcsm_source_name) : contains(keys(var.replsets), var.pcsm_source_name))
    error_message = "pcsm_source_name must reference an existing topology in the map selected by pcsm_source_kind."
  }
}

variable "pcsm_source_name" {
  type        = string
  default     = ""
  description = "Name of the source topology for PCSM."
}

variable "pcsm_target_kind" {
  type        = string
  default     = ""
  description = "PCSM target topology kind: cluster (mongos hosts) or replset (electable members)."

  validation {
    condition     = !var.enable_pcsm || contains(["cluster", "replset"], var.pcsm_target_kind)
    error_message = "pcsm_target_kind must be cluster or replset when PCSM is enabled."
  }

  validation {
    condition     = !var.enable_pcsm || (var.pcsm_target_kind == "cluster" ? contains(keys(var.clusters), var.pcsm_target_name) : contains(keys(var.replsets), var.pcsm_target_name))
    error_message = "pcsm_target_name must reference an existing topology in the map selected by pcsm_target_kind."
  }

  validation {
    condition     = !var.enable_pcsm || var.pcsm_source_kind == var.pcsm_target_kind
    error_message = "pcsm_source_kind and pcsm_target_kind must match."
  }

}

variable "pcsm_target_name" {
  type        = string
  default     = ""
  description = "Name of the target topology for PCSM."

  validation {
    condition     = !var.enable_pcsm || var.pcsm_source_name != var.pcsm_target_name
    error_message = "pcsm_source_name and pcsm_target_name must be different."
  }
}

variable "default_pcsm_host" {
  type    = string
  default = "pcsm"
}

variable "pcsm_type" {
  type        = string
  default     = "t3.small"
  description = "EC2 instance type for PCSM (default: 2 vCPU, 2 GiB)."
}

locals {
  pcsm_host = "${var.prefix}-${var.default_pcsm_host}"
}

#############
# TLS CA
#############

variable "enable_ca" {
  type        = bool
  default     = false
  description = "Provision a certificate authority host and add it to generated inventories."
}

variable "ca_placement" {
  type        = string
  default     = "dedicated"
  description = "Place the TLS certificate authority on a dedicated VM or the PMM host."

  validation {
    condition     = contains(["dedicated", "pmm"], var.ca_placement)
    error_message = "ca_placement must be either dedicated or pmm."
  }

  validation {
    condition     = !var.enable_ca || var.ca_placement != "pmm" || var.enable_pmm
    error_message = "enable_pmm must be true when CA provisioning is enabled with ca_placement set to pmm."
  }
}

variable "default_ca_host" {
  type        = string
  default     = "ca-server"
  description = "Base hostname for the dedicated CA VM."
}

variable "ca_type" {
  type        = string
  default     = "t3.micro"
  description = "EC2 instance type for the dedicated CA VM."
}

locals {
  ca_host = "${var.prefix}-${var.default_ca_host}"
}

variable "enable_plgm" {
  type        = bool
  default     = false
  description = "Deploy a dedicated plgm load generator instance."
}

variable "default_plgm_host" {
  description = "Base plgm host name"
  type        = string
  default     = "plgm"
}

locals {
  plgm_host = "${var.prefix}-${var.default_plgm_host}"
}

variable "plgm_type" {
  default     = "t3.medium"
  description = "EC2 instance type of the plgm server"
}

#############
# Backup
#############

variable "create_backup_bucket" {
  description = "Create an S3 bucket (plus IAM user/keys) for PBM backups. When false, set existing_bucket_* to use an existing bucket, or leave them empty to skip cloud backup storage."
  type        = bool
  default     = true
}

variable "existing_bucket_name" {
  description = "Name of an existing S3 bucket to use for PBM backups when create_backup_bucket = false. Leave empty to skip cloud backup storage."
  type        = string
  default     = ""
}

variable "existing_bucket_region" {
  description = "Region of the existing S3 bucket (defaults to var.region)."
  type        = string
  default     = ""
}

variable "existing_bucket_access_key" {
  description = "Access key ID with read/write access to the existing bucket."
  type        = string
  default     = ""
}

variable "existing_bucket_secret_key" {
  description = "Secret access key for the existing bucket."
  type        = string
  default     = ""
  sensitive   = true
}

variable "default_bucket_name" {
  description = "Base bucket name"
  type        = string
  default     = "mongo-bkp-storage"
}

locals {
  bucket_name = "${var.prefix}-${var.default_bucket_name}"
}

variable "backup_retention" {
  default     = "2"
  description = "days to keep backups in bucket"
}

#############
# Instances
#############

variable "image" {
  description = "Available images by region"
  default = {
    us-west-2 = "ami-0ad8bfd4b10994785" # Centos 9
    #us-west-2 = "ami-04999cd8f2624f834" # AL2023
    #us-west-2 = "ami-075686beab831bb7f" # Ubuntu 24.04
  }
}

# Save money by running spot instances but they may be terminated by AWS at any time
variable "use_spot_instances" {
  type    = bool
  default = false
}

variable "data_disk_type" {
  default     = "gp2"
  description = "EBS volume type for MongoDB data disks (gp2, gp3, io1, io2, st1, sc1)"
}

variable "data_disk_iops" {
  type        = number
  default     = null
  description = "Provisioned IOPS for data volumes. Applied to gp3, io1 and io2 (io1/io2 default to 3000 when unset)."
}

variable "data_disk_throughput" {
  type        = number
  default     = null
  description = "Provisioned throughput (MiB/s) for data volumes. Applied to gp3 only."
}

variable "configsvr_disk_type" {
  type        = string
  default     = null
  description = "EBS volume type for config server data volumes. Defaults to data_disk_type. Provisioned IOPS/throughput only apply when it equals data_disk_type."
}

variable "boot_disk_type" {
  type        = string
  default     = null
  description = "Root volume type for MongoDB nodes and the plgm client (e.g. gp3). Defaults to the AMI setting."
}

variable "boot_disk_size" {
  type        = number
  default     = null
  description = "Root volume size (GB) for MongoDB nodes and the plgm client. Defaults to the AMI size. MongoDB logs are written to the root volume."
}

variable "compact_placement" {
  type        = bool
  default     = false
  description = "Launch MongoDB nodes and the plgm client in a cluster placement group (single AZ, low latency). Requires subnet_count = 1."

  validation {
    condition     = !var.compact_placement || var.subnet_count == 1
    error_message = "compact_placement requires subnet_count = 1 (a cluster placement group lives in a single availability zone)."
  }
}

variable "labels" {
  type        = map(string)
  default     = {}
  description = "Extra tags applied to every resource created by this configuration (e.g. bench_run, mongo_version)."
}

################
# Instance types
################

variable "shardsvr_type" {
  default     = "t3.medium"
  description = "EC2 instance type for MongoDB shard servers"
}

variable "shardsvr_volume_size" {
  default     = 50
  description = "EBS volume size (GB) for MongoDB shard servers"
}

variable "configsvr_type" {
  default     = "t3.medium"
  description = "EC2 instance type for MongoDB config servers (CSRS)"
}

variable "configsvr_volume_size" {
  default     = 20
  description = "EBS volume size (GB) for MongoDB config servers"
}

variable "mongos_type" {
  default     = "t3.medium"
  description = "EC2 instance type for mongos router instances"
}

variable "arbiter_type" {
  default     = "t3.medium"
  description = "EC2 instance type for MongoDB arbiter nodes"
}

variable "replsetsvr_type" {
  default     = "t3.medium"
  description = "EC2 instance type for standalone replica set data-bearing nodes"
}

variable "replsetsvr_volume_size" {
  default     = 100
  description = "EBS volume size (GB) for standalone replica set data-bearing nodes"
}

#############
# Networking
#############

variable "region" {
  type    = string
  default = "us-west-2"
}

variable "default_vpc_name" {
  description = "Base VPC name"
  type        = string
  default     = "mongo"
}

locals {
  vpc = "${var.prefix}-${var.default_vpc_name}"
}

variable "subnet_count" {
  type        = number
  default     = 3
  description = "How many subnets to create"
}

variable "subnet_cidr" {
  type    = string
  default = "10.128.0.0/20"
}

# source IPs that will connect to the cluster from outside the VPC
variable "source_ranges" {
  type    = string
  default = "0.0.0.0/0"
}

#############
# Package Versions
#############

variable "mongo_release" {
  type        = string
  default     = ""
  description = "MongoDB release line (e.g. psmdb-80, 8.0, 8.3, 9.0). Empty string uses the default from group_vars."
}

variable "mongodb_distribution" {
  type        = string
  default     = ""
  description = "MongoDB distribution for non-Docker packages: psmdb, community, or enterprise. Empty string uses the default from group_vars."
}

variable "mongo_version" {
  type        = string
  default     = ""
  description = "Specific MongoDB version to install (e.g. 8.0.4). Empty string installs the latest available."
}

variable "mongo_repo" {
  type        = string
  default     = ""
  description = "Percona repository channel for MongoDB packages (release, testing, experimental). Empty string uses group_vars default."
}

variable "mongot_repo" {
  type        = string
  default     = ""
  description = "Percona repository channel for Percona Search packages (release, testing, experimental). Empty string uses group_vars default."
}

variable "pbm_version" {
  type        = string
  default     = ""
  description = "Specific PBM version to install (e.g. 2.4.0). Empty string installs the latest available."
}

variable "pbm_repo" {
  type        = string
  default     = ""
  description = "Percona repository channel for PBM packages (release, testing, experimental). Empty string uses group_vars default."
}

variable "pmm_client_version" {
  type        = string
  default     = ""
  description = "Specific PMM client version to install (e.g. 3.4.0). Empty string installs the latest available."
}

variable "pmm_client_repo" {
  type        = string
  default     = ""
  description = "Percona repository channel for PMM client packages (release, testing, experimental). Empty string uses group_vars default."
}
