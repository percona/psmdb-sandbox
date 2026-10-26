##################
# Sharded Clusters
##################

variable "vault_encryption" {
  type        = bool
  default     = false
  description = "Enable Vault-backed encryption at rest for every MongoDB topology in this environment."
}

# By default we deploy 1 sharded cluster, named test01. The configuration can be customized by adding any of the optional values listed below.

variable "clusters" {
  description = "MongoDB clusters to deploy"
  type = map(object({
    env_tag               = optional(string, "test") # Name of the environment for the cluster
    configsvr_count       = optional(number, 3)      # Number of config servers to be used
    shard_count           = optional(number, 2)      # Number of shards to be created
    shardsvr_replicas     = optional(number, 2)      # How many data-bearing nodes for each shard's replica set
    arbiters_per_replset  = optional(number, 1)      # Number of arbiters for each shard's replica set
    mongos_count          = optional(number, 2)      # Number of mongos routers to provision
    psmdb_image           = optional(string, "percona/percona-server-mongodb:latest")
    mongodb_root_password = optional(string, "percona")
    domain_name           = optional(string, "") # DNS domain name
    network_name          = optional(string, "mongo-terraform")
    bind_to_localhost     = optional(bool, true) # Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0    
    enable_audit          = optional(bool, false)
    audit_filter          = optional(string, "{ atype: \"authCheck\", \"param.command\": { $in: [ \"insert\", \"update\", \"delete\", \"findandmodify\" ] }, \"users.user\": { $not: /^__/ } }")
    enable_mongot         = optional(bool, false)
    mongot_image          = optional(string, "mongodb/mongodb-community-search:1.70.1")
    # PMM
    enable_pmm       = optional(bool, true)
    pmm_host         = optional(string, "pmm-server")
    pmm_port         = optional(number, 8443)
    pmm_server_user  = optional(string, "admin")
    pmm_server_pwd   = optional(string, "admin")
    pmm_client_image = optional(string, "percona/pmm-client:latest")
    # PBM
    enable_pbm       = optional(bool, true)
    base_os_image    = optional(string, "redhat/ubi9-minimal")
    pbm_image        = optional(string, "percona/percona-backup-mongodb:latest")
    seaweedfs_server = optional(string, "seaweedfs")
    seaweedfs_port   = optional(number, 8333)
    bucket_name      = optional(string, "mongo-backups")
    # LDAP
    enable_ldap           = optional(bool, false)
    ldap_servers          = optional(string, "ldap:389")
    ldap_bind_dn          = optional(string, "cn=admin,dc=example,dc=org")
    ldap_bind_pw          = optional(string, "admin")
    ldap_user_search_base = optional(string, "dc=example,dc=org")
    #    use_tls                 = optional(bool, false)
    #    tls_cert_file           = optional(string,"./certs/mongo.crt")
    #    tls_key_file            = optional(string,"./certs/mongo.key")
    #    tls_ca_file             = optional(string,"./certs/ca.crt")
  }))

  default = {
    cl01 = {
      env_tag = "test"
    }
    #    cl02 = {
    #      env_tag = "prod"
    #      mongos_count = 1
    #       enable_ldap = true
    #   }
  }
}

###########################
# Replica Sets (standalone)
###########################

# By default, no replica sets are provisioned (except those needed for each shard of the sharded clusters).
# If you want to provision separate replica sets, change the default value of replsets below.

variable "replsets" {
  description = "MongoDB replica sets to deploy"
  type = map(object({
    env_tag                = optional(string, "test") # Name of the environment for the replica set
    data_nodes_per_replset = optional(number, 2)      # Number of data bearing members for the replica set
    arbiters_per_replset   = optional(number, 1)      # Number of arbiters for the replica set
    replset_port           = optional(number, 27017)  # Starting port for the replica set
    arbiter_port           = optional(number, 27017)  # Starting port for the replica set arbiters
    arbiter_base_port      = optional(number, 27117)  # Stable starting port for arbiters; does not change when data node count changes
    psmdb_image            = optional(string, "percona/percona-server-mongodb:latest")
    mongodb_root_password  = optional(string, "percona")
    domain_name            = optional(string, "") # DNS domain name
    network_name           = optional(string, "mongo-terraform")
    bind_to_localhost      = optional(bool, true) # Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0     
    enable_audit           = optional(bool, false)
    audit_filter           = optional(string, "{ atype: \"authCheck\", \"param.command\": { $in: [ \"insert\", \"update\", \"delete\", \"findandmodify\" ] }, \"users.user\": { $not: /^__/ } }")
    enable_mongot          = optional(bool, false)
    mongot_image           = optional(string, "mongodb/mongodb-community-search:1.70.1")
    # PMM
    enable_pmm       = optional(bool, true)
    pmm_host         = optional(string, "pmm-server")
    pmm_port         = optional(number, 8443)
    pmm_server_user  = optional(string, "admin")
    pmm_server_pwd   = optional(string, "admin")
    pmm_client_image = optional(string, "percona/pmm-client:latest")
    # PBM
    enable_pbm       = optional(bool, true)
    base_os_image    = optional(string, "redhat/ubi9-minimal")
    pbm_image        = optional(string, "percona/percona-backup-mongodb:latest")
    seaweedfs_server = optional(string, "seaweedfs")
    seaweedfs_port   = optional(number, 8333)
    bucket_name      = optional(string, "mongo-backups")
    # LDAP
    enable_ldap           = optional(bool, false)
    ldap_servers          = optional(string, "ldap:389")
    ldap_bind_dn          = optional(string, "cn=admin,dc=example,dc=org")
    ldap_bind_pw          = optional(string, "admin")
    ldap_user_search_base = optional(string, "dc=example,dc=org")
    #    use_tls                   = optional(bool, false)
    #    tls_cert_file             = optional(string,"./certs/mongo.crt")
    #    tls_key_file              = optional(string,"./certs/mongo.key")
    #    tls_ca_file               = optional(string,"./certs/ca.crt")    
  }))

  default = {
    #     rs01 = {
    #       env_tag = "test"
    #       data_nodes_per_replset = 3
    #       arbiters_per_replset = 0
    #       enable_ldap = true
    #     }
    #     rs02 = {
    #       env_tag = "prod"
    #       replset_port = 27020
    #       arbiter_port = 27027
    #     }
  }
}

#############
# PMM Servers
#############

variable "pmm_servers" {
  description = "PMM Servers to deploy"
  type = map(object({
    env_tag           = optional(string, "test") # Name of the environment
    domain_name       = optional(string, "")     # DNS domain name
    pmm_server_image  = optional(string, "percona/pmm-server:latest")
    pmm_port          = optional(number, 8443)
    pmm_external_port = optional(number, 8443) # Port of the PMM server as seen from outside docker
    watchtower_token  = optional(string, "1234567890")
    pmm_server_user   = optional(string, "admin")
    pmm_server_pwd    = optional(string, "admin")
    renderer_image    = optional(string, "grafana/grafana-image-renderer:latest")
    watchtower_image  = optional(string, "percona/watchtower:latest")
    network_name      = optional(string, "mongo-terraform")
    bind_to_localhost = optional(bool, true) # Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0     
    # PMM Extensions side-car; needs PMM Server 3.10 or newer
    enable_extensions                 = optional(bool, false)
    extensions_image                  = optional(string, "percona/pmm-extensions:3.10.0-rc1")
    extensions_inventory_sync_minutes = optional(number, 1)
  }))

  default = {
    pmm-server = {
      env_tag = "test"
    }
    #     pmm-server-prod = {
    #       env_tag = "prod"
    #     }
    #     pmm-server-old = {
    #       pmm_server_image = "percona/pmm-server:2.42"
    #       pmm_port = 443
    #     }
  }

  validation {
    condition = alltrue([
      for s in values(var.pmm_servers) : !s.enable_extensions || (
        can(regex(":[^/:]+$", s.pmm_server_image)) && !endswith(s.pmm_server_image, ":latest") &&
        try(tonumber(regex(":(\\d+)\\.\\d+", s.pmm_server_image)[0]) * 1000 + tonumber(regex(":\\d+\\.(\\d+)", s.pmm_server_image)[0]) >= 3010, true)
      )
    ])
    error_message = "PMM Extensions needs PMM Server 3.10 or newer: set pmm_server_image to such an image, for example perconalab/pmm-server:3.10.0-rc, rather than latest or an older version."
  }
}

##################
# Bare PMM Clients
##################

variable "pmm_bare_clients" {
  description = "Containers with only a PMM client, on a chosen OS with systemd"
  type = map(object({
    base_image         = optional(string, "ubuntu:24.04") # Debian, Ubuntu or RHEL-compatible image
    pmm_client_repo    = optional(string, "release")      # percona-release channel: release, testing or experimental
    pmm_client_version = optional(string, "")             # e.g. 3.10.0; empty installs the newest in the channel
    pmm_host           = optional(string, "pmm-server")
    pmm_port           = optional(number, 8443)
    pmm_server_user    = optional(string, "admin")
    pmm_server_pwd     = optional(string, "admin")
    network_name       = optional(string, "mongo-terraform")
  }))

  default = {
    #     client01 = {
    #       base_image = "rockylinux:9"
    #     }
  }

  validation {
    condition     = alltrue([for c in values(var.pmm_bare_clients) : contains(keys(var.pmm_servers), c.pmm_host)])
    error_message = "Each PMM client without MongoDB needs pmm_host to name an entry of pmm_servers."
  }
  validation {
    condition     = length(setintersection(keys(var.pmm_bare_clients), keys(var.pmm_servers))) == 0
    error_message = "A PMM client without MongoDB cannot have the same name as a PMM server."
  }
}

###############
# SeaweedFS Servers
###############

variable "seaweedfs_servers" {
  description = "SeaweedFS servers to deploy"
  type = map(object({
    env_tag              = optional(string, "test") # Name of the environment
    domain_name          = optional(string, "")     # DNS domain name
    seaweedfs_image      = optional(string, "chrislusf/seaweedfs:latest")
    seaweedfs_port       = optional(number, 8333)
    seaweedfs_admin_port = optional(number, 9333)
    seaweedfs_access_key = optional(string, "seaweedfs")
    seaweedfs_secret_key = optional(string, "seaweedfs-secret")
    bucket_name          = optional(string, "mongo-backups")
    backup_retention     = optional(number, 2) # Days to keep backups
    network_name         = optional(string, "mongo-terraform")
    bind_to_localhost    = optional(bool, true) # Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0
  }))

  default = {
    seaweedfs = {
      env_tag = "test"
    }
    #     seaweedfs-prod = {
    #       env_tag = "prod"
    #     }
  }
}

###############
# LDAP Servers
###############

variable "ldap_servers" {
  description = "LDAP Servers to deploy"
  type = map(object({
    env_tag             = optional(string, "test") # Name of the environment
    domain_name         = optional(string, "")     # DNS domain name
    ldap_image          = optional(string, "osixia/openldap:1.5.0")
    ldap_admin_image    = optional(string, "osixia/phpldapadmin:0.9.0")
    ldap_port           = optional(number, 389)
    ldap_admin_port     = optional(number, 80)
    ldap_domain         = optional(string, "example.com")
    ldap_org            = optional(string, "Example Inc")
    ldap_admin_password = optional(string, "admin")
    ldap_users = optional(list(object({
      uid      = string
      cn       = string
      sn       = string
      password = string
    })), [])
    network_name      = optional(string, "mongo-terraform")
    bind_to_localhost = optional(bool, true) # Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0     
  }))

  default = {
    #     ldap = {
    #       env_tag = "test"
    #       ldap_users  = [
    #         {
    #           uid      = "alice"
    #           cn       = "Alice"
    #           sn       = "Admin"
    #           password = "secret123"
    #         },
    #         {
    #           uid      = "bob"
    #           cn       = "Bob"
    #           sn       = "User"
    #           password = "supersecure"
    #         }
    #        ]
    #      }
    #     ldap-prod = {
    #       env_tag = "prod"
    #     }
  }
}

#############
# plgm (Percona Load Generator for MongoDB)
#############

variable "enable_plgm" {
  type        = bool
  default     = false
  description = "Deploy a dedicated plgm load generator container (Web UI published on 127.0.0.1:plgm_external_port)."
}

variable "plgm_container_suffix" {
  type        = string
  default     = "plgm"
  description = "Suffix for the plgm container"
}

variable "plgm_os_image" {
  type        = string
  description = "Base OS image (Alpine-based) for the custom Docker image created with plgm. Built for linux/amd64."
  default     = "alpine:3.22"
}

variable "plgm_image" {
  type        = string
  description = "Name of the local Docker image to be created for plgm"
  default     = "percona/plgm"
}

variable "plgm_external_port" {
  type        = number
  default     = 9999
  description = "Host port (bound to 127.0.0.1) that serves the plgm Web UI"
}

variable "prefix" {
  type        = string
  default     = ""
  description = "Prefix applied to every Docker container and volume. If non-empty a hyphen is appended automatically (e.g. \"ig\" → \"ig-\")."
}

############################
# Percona ClusterSync (PCSM)
############################

variable "enable_pcsm" {
  type        = bool
  default     = false
  description = "Deploy one Percona ClusterSync for MongoDB container in this environment."
}

variable "pcsm_image" {
  type        = string
  default     = "percona/percona-clustersync-mongodb:0.9.0"
  description = "Percona ClusterSync for MongoDB container image."
}

variable "pcsm_env_file" {
  type        = string
  default     = ""
  description = "Path to a host-generated 0600 shell env file containing PCSM_SOURCE_URI and PCSM_TARGET_URI."
}

variable "pcsm_pmm_host" {
  type        = string
  default     = "pmm-server"
  description = "PMM Server host selected by the PCSM PMM Client, following the same naming and prefix rules as MongoDB PMM clients."
}

variable "pcsm_pmm_port" {
  type        = number
  default     = 8443
  description = "Fallback PMM Server port used by the PCSM PMM Client when the selected pmm_servers entry does not define one."
}

variable "pcsm_pmm_server_user" {
  type        = string
  default     = "admin"
  description = "Fallback PMM Server user used by the PCSM PMM Client when the selected pmm_servers entry does not define one."
}

variable "pcsm_pmm_server_pwd" {
  type        = string
  default     = "admin"
  sensitive   = true
  description = "Fallback PMM Server password used by the PCSM PMM Client when the selected pmm_servers entry does not define one."
}

variable "pcsm_pmm_client_image" {
  type        = string
  default     = "percona/pmm-client:latest"
  description = "PMM Client image used by the PCSM sidecar."
}

variable "pcsm_metrics_port" {
  type        = number
  default     = 2242
  description = "PCSM Prometheus metrics port."
}

variable "pcsm_metrics_path" {
  type        = string
  default     = "/metrics"
  description = "PCSM Prometheus metrics path."
}

variable "pcsm_metrics_scheme" {
  type        = string
  default     = "http"
  description = "PCSM Prometheus metrics scheme."
}

variable "pcsm_source_kind" {
  type        = string
  default     = "cluster"
  description = "Source topology kind for PCSM: cluster or replset."

  validation {
    condition     = contains(["cluster", "replset"], var.pcsm_source_kind)
    error_message = "pcsm_source_kind must be either cluster or replset."
  }
}

variable "pcsm_source_name" {
  type        = string
  default     = ""
  description = "Source topology name from clusters or replsets, according to pcsm_source_kind."
}

variable "pcsm_target_kind" {
  type        = string
  default     = "cluster"
  description = "Target topology kind for PCSM: cluster or replset."

  validation {
    condition     = contains(["cluster", "replset"], var.pcsm_target_kind)
    error_message = "pcsm_target_kind must be either cluster or replset."
  }
}

variable "pcsm_target_name" {
  type        = string
  default     = ""
  description = "Target topology name from clusters or replsets, according to pcsm_target_kind."
}

variable "pcsm_cpus" {
  type        = number
  default     = 2
  description = "Maximum number of CPUs available to PCSM."

  validation {
    condition     = var.pcsm_cpus > 0
    error_message = "pcsm_cpus must be greater than zero."
  }
}

variable "pcsm_memory_mb" {
  type        = number
  default     = 1024
  description = "PCSM memory limit in MiB."

  validation {
    condition     = var.pcsm_memory_mb > 0
    error_message = "pcsm_memory_mb must be greater than zero."
  }
}

#############
# Networking
#############

variable "network_name" {
  type    = string
  default = "mongo-terraform"
}
