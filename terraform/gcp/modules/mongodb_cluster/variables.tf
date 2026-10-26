################
# Project
################

variable "prefix" {
  type        = string
  default     = "ig"
  description = "Prefix to be applied to the resources created, make sure to change it to avoid collisions with other users projects"
}

variable "cluster_name" {
  description = "Name of the MongoDB cluster"
  default     = "cluster01"
}

variable "env_tag" {
  default     = "qa"
  description = "Name of Environment"
}

variable "gce_ssh_users" {
  description = "SSH user names, and the path to public key files on your machine to be added to authorized_keys"
  default = {
    your_ssh_username = "your_ssh_username.pub"
    #    ,user2 = "user2.pub"
  }
}

variable "my_ssh_user" {
  default     = "your_ssh_username"
  description = "Used to auto-generate the ssh_config file. Each person running this code should set it to its own SSH user name"
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
  default     = "1"
  description = "Number of mongos to provision"
}

variable "enable_audit" {
  type        = bool
  default     = false
  description = "Enable audit logging for this deployment"
}

variable "enable_mongot" {
  type        = bool
  default     = false
  description = "Enable MongoDB Search and Vector Search"
}

variable "mongot_port" {
  type        = number
  default     = 27028
  description = "mongot gRPC port"
}

variable "audit_filter" {
  type        = string
  default     = ""
  description = "Optional audit filter override passed to Ansible inventory"
}

################
# Shards
################

variable "shardsvr_tag" {
  description = "Name of the shard servers"
  default     = "mongodb-shard"
}

variable "shardsvr_type" {
  default     = "e2-medium"
  description = "instance type of the shard server"
}

variable "shardsvr_volume_size" {
  default     = "50"
  description = "storage size for the shard server"
}

variable "shard_port" {
  type    = number
  default = 27018
}

################
# CSRS
################

variable "configsvr_tag" {
  description = "Name of the config servers"
  default     = "mongodb-cfg"
}

variable "configsvr_type" {
  default     = "e2-medium"
  description = "instance type of the config server"
}

variable "configsvr_volume_size" {
  default     = "20"
  description = "storage size for the config server"
}

variable "configsvr_port" {
  type    = number
  default = 27019
}

################
# Mongos routers
################

variable "mongos_tag" {
  description = "Name of the mongos router servers"
  default     = "mongodb-mongos"
}

variable "mongos_type" {
  default     = "e2-medium"
  description = "instance type of the mongos servers"
}

variable "mongos_port" {
  type    = number
  default = 27017
}

#############
# Arbiters
#############

variable "arbiter_tag" {
  description = "Name of the arbiter servers"
  default     = "mongodb-arb"
}

variable "arbiter_type" {
  default     = "e2-medium"
  description = "instance type of the arbiter server"
}

variable "arbiter_port" {
  type    = number
  default = 27018
}

#############
# Instances
#############

variable "image" {
  description = "GCP machine image for all instances"
  type        = string
  default     = "projects/centos-cloud/global/images/centos-stream-9-v20231115"
}

# Save money by running spot instances but they may be terminated by google at any time
variable "use_spot_instances" {
  type    = bool
  default = false
}

variable "data_disk_type" {
  default = "pd-standard"
}

#############
# Networking
#############

variable "region" {
  type    = string
  default = "northamerica-northeast1"
}

variable "vpc" {
  type    = string
  default = "mongo-terraform"
}

variable "subnet_name" {
  type    = string
  default = "mongo-subnet"
}

variable "subnet_cidr" {
  type    = string
  default = "10.128.0.0/20"
}

#############################
# Benchmark hardware tunables
#############################

variable "zones" {
  type        = list(string)
  description = "Zones to spread members over (round-robin)"
}

variable "data_disk_iops" {
  type    = number
  default = null
}

variable "data_disk_throughput" {
  type    = number
  default = null
}

variable "boot_disk_type" {
  type    = string
  default = null
}

variable "boot_disk_size" {
  type    = number
  default = null
}

variable "min_cpu_platform" {
  type    = string
  default = null
}

variable "nic_type" {
  type    = string
  default = null
}

variable "tier1_networking" {
  type    = bool
  default = false
}

variable "resource_policies" {
  type        = list(string)
  default     = []
  description = "Resource policy self links (compact placement)"
}

variable "labels" {
  type    = map(string)
  default = {}
}

variable "configsvr_disk_type" {
  type    = string
  default = "pd-standard"
}

variable "configsvr_disk_iops" {
  type    = number
  default = null
}

variable "configsvr_disk_throughput" {
  type    = number
  default = null
}
