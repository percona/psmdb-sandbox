variable "cluster_name" {
  description = "Name of the MongoDB cluster"
  default     = "cl01"
}

variable "env_tag" {
  description = "Name of the Environment"
  default     = "test"
}

variable "domain_name" {
  description = "Name of the DNS domain"
  default     = ""
}

##################
# MongoDB topology
##################

variable "configsvr_count" {
  default     = "3"
  description = "Number of config servers to be used"
}

variable "shard_count" {
  default     = "2"
  description = "Number of shards to be used"
}

variable "shardsvr_replicas" {
  default     = "2"
  description = "How many data bearing nodes per shard"
}

variable "arbiters_per_replset" {
  default     = "1"
  description = "Number of arbiters per replica set"
}

variable "mongos_count" {
  default     = "2"
  description = "Number of mongos to provision"
}

######################
# Security Credentials
######################

variable "keyfile_contents" {
  default     = "KYYVuRIooX+S2Ee6GDUpYiI6rpx879XYYwWD44tF/WtogW0o8Z4Ua0/Fs+Nez4GO"
  description = "Content of the keyfile for MongoDB replicaset member authentication"
  sensitive   = true
}

variable "keyfile_path" {
  default     = "/etc/mongo"
  description = "Path to the keyfile on MongoDB containers"
}

variable "keyfile_name" {
  default     = "mongodb-keyfile.key"
  description = "Name of the file containing the keyfile on MongoDB containers"
}

variable "mongodb_root_user" {
  default     = "root"
  description = "MongoDB user to be created with root perms"
}

variable "mongodb_root_password" {
  default     = "percona"
  description = "MongoDB root user password"
  sensitive   = true
}

variable "enable_audit" {
  type        = bool
  default     = false
  description = "Enable audit logging for this deployment"
}

variable "audit_filter" {
  type        = string
  default     = "{ atype: \"authCheck\", \"param.command\": { $in: [ \"insert\", \"update\", \"delete\", \"findandmodify\" ] }, \"users.user\": { $not: /^__/ } }"
  description = "Audit filter applied to mongod and mongos containers"
}

variable "enable_mongot" {
  type        = bool
  description = "Enable mongot sidecars for MongoDB Search and Vector Search on shard data-bearing nodes"
  default     = false
}

variable "mongot_image" {
  description = "MongoDB Search Community Docker image"
  default     = "mongodb/mongodb-community-search:1.70.1"
}

variable "mongot_port" {
  description = "mongot gRPC port"
  default     = "27028"
}

variable "mongot_metrics_port" {
  description = "mongot Prometheus metrics port"
  default     = "9946"
}

variable "mongot_health_port" {
  description = "mongot health check port"
  default     = "8080"
}

variable "mongodb_mongot_user" {
  default     = "mongotUser"
  description = "MongoDB user for mongot sync"
}

variable "mongodb_mongot_password" {
  default     = "percona"
  description = "MongoDB mongot sync user password"
  sensitive   = true
}

################
# Shards
################

variable "shardsvr_tag" {
  description = "Name of the shard servers"
  default     = "shard"
}

variable "shardsvr_port" {
  description = "Port of the mongod servers"
  default     = "27018"
}

################
# CSRS
################

variable "configsvr_tag" {
  description = "Name of the config servers"
  default     = "cfg"
}

variable "configsvr_port" {
  description = "Port of the mongod config servers"
  default     = "27019"
}

################
# Mongos routers
################

variable "mongos_tag" {
  description = "Name of the mongos router servers"
  default     = "mongos"
}

variable "mongos_port" {
  description = "Port of the mongos router servers"
  default     = "27017"
}

#############
# Arbiters
#############

variable "arbiter_tag" {
  description = "Name of the arbiter servers"
  default     = "arb"
}

variable "arbiter_port" {
  description = "Port of the arbiter servers"
  default     = "27018"
}

#############
# PMM
#############

variable "enable_pmm" {
  type        = bool
  description = "Enable PMM client"
  default     = false
}

variable "pmm_host" {
  description = "Name of the PMM server"
  default     = "pmm-server"
}

variable "pmm_server_user" {
  description = "Name of the PMM server admin user"
  default     = "admin"
  type        = string
}

variable "pmm_server_pwd" {
  description = "Password of the PMM server admin user"
  default     = "admin"
  type        = string
  sensitive   = true
}

variable "pmm_client_container_suffix" {
  default     = "pmm-client"
  description = "Suffix for PMM client container"
}

variable "pmm_port" {
  description = "Port of the PMM server inside docker network"
  # PMM 3 uses 8443. PMM 2 uses 443
  # default = "443"
  default = "8443"
}

variable "mongodb_pmm_user" {
  default     = "pmm"
  description = "MongoDB user to be created for PBM"
}

variable "mongodb_pmm_password" {
  default     = "percona"
  description = "MongoDB PBM user password"
  sensitive   = true
}

#############
# PBM
#############

variable "enable_pbm" {
  type        = bool
  description = "Enable PBM agent"
  default     = false
}

variable "pbm_container_suffix" {
  default     = "pbm-agent"
  description = "Suffix for PBM agent containers. Will be appended to each cluster component"
}

variable "pbm_cli_container_suffix" {
  default     = "pbm-cli"
  description = "Suffix for PBM CLI container"
}

variable "mongodb_pbm_user" {
  default     = "pbmuser"
  description = "MongoDB user to be created with for PBM"
}

variable "mongodb_pbm_password" {
  default     = "percona"
  description = "MongoDB PBM user password"
  sensitive   = true
}

#############
# Bucket
#############

variable "minio_region" {
  description = "Default MINIO region"
  default     = "us-east-1"
}

variable "minio_access_key" {
  default = "seaweedfs"
}

variable "minio_server" {
  default = "seaweedfs"
}

variable "minio_port" {
  default = "8333"
}

variable "minio_secret_key" {
  default   = "seaweedfs-secret"
  sensitive = true
}

variable "bucket_name" {
  default     = "mongo-backups"
  description = "S3-compatible storage to put backups"
}


###############
# Docker Images
###############

variable "base_os_image" {
  description = "Base OS image for the custom Docker image created with pbm-agent and mongod. Required for physical restores"
  #default = "quay.io/centos/centos:stream9"
  #default = "oraclelinux:8"
  default = "redhat/ubi9-minimal"
  #default = "redhat/ubi9"
}

variable "psmdb_image" {
  description = "Docker image for MongoDB"
  default     = "percona/percona-server-mongodb:latest"
}

variable "pbm_image" {
  description = "Docker image for PBM"
  default     = "percona/percona-backup-mongodb:latest"
}

variable "pmm_client_image" {
  description = "Docker image for PMM client"
  default     = "percona/pmm-client:latest"
  #default = "perconalab/pmm-client:3-dev-latest"
}

variable "pbm_mongod_image" {
  description = "Name of the local Docker image to be created for pbm-agent + current mongod version. Required for physical restores"
  default     = "percona/pbm-agent"
}

variable "mongos_image" {
  description = "Name of the local Docker image to be created for mongos router"
  #default = "percona/mongos"
  # Using this image until we can make the custom mongos image work
  default = "percona/percona-server-mongodb:latest"
}

variable "uid" {
  description = "The user id under which the main process runs in the container created for pbm-agent + current mongod version"
  default     = 1001
}

#############
# Networking
#############

variable "network_name" {
  type    = string
  default = "mongo-terraform"
}

variable "bind_to_localhost" {
  type        = bool
  default     = true
  description = "Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0"
}

######
# LDAP
######

variable "enable_ldap" {
  type        = bool
  description = "Enable LDAP authentication"
  default     = false
}

variable "ldap_servers" {
  type        = string
  description = "Name of the LDAP servers"
  default     = "ldap:389"
}

variable "ldap_bind_dn" {
  type        = string
  description = "LDAP bind DN for authentication"
  default     = "cn=admin,dc=example,dc=org"
}

variable "ldap_bind_pw" {
  type        = string
  description = "LDAP bind password"
  default     = "admin"
}

variable "ldap_user_search_base" {
  type        = string
  description = "Base DN used for user search in LDAP"
  default     = "dc=example,dc=org"
}

############
# TLS (reserved)
############

variable "use_tls" {
  type    = bool
  default = true
}
