################
# Project
################

variable "prefix" {
  type        = string
  default     = "ig"
  description = "Prefix to be applied to the resources created, make sure to change it to avoid collisions with other users projects"
}

variable "rs_name" {
  description = "Name of the MongoDB cluster"
  default     = "rs01"
}

variable "env_tag" {
  default     = "qa"
  description = "Name of Environment"
}

variable "my_ssh_user" {
  default     = "ec2-user"
  description = "Used to auto-generate the ssh_config file. Each person running this code should set it to its own SSH user name"
}

variable "my_key_pair" {
  description = "Name of key pair to login via SSH"
  type        = string
  default     = "key"
}

variable "ssh_public_key_path" {
  description = "SSH public key file used to provision the configured SSH user"
  type        = string
}

##################
# MongoDB topology
##################

variable "data_nodes_per_replset" {
  default     = "2"
  description = "How many data bearing nodes per replset"
}

variable "arbiters_per_replset" {
  default     = "1"
  description = "Number of arbiters per replica set"
}

variable "enable_audit" {
  type        = bool
  default     = false
  description = "Enable audit logging for this deployment"
}

variable "audit_filter" {
  type        = string
  default     = ""
  description = "Optional audit filter override passed to Ansible inventory"
}

######################
# Data bearing members
######################

variable "replset_tag" {
  description = "Name of the replicaset servers"
  default     = "mongodb-svr"
}

variable "replsetsvr_ports" {
  type    = list(number)
  default = [27017]
}

variable "replsetsvr_volume_size" {
  default     = "100"
  description = "storage size for the replica set servers"
}

variable "replsetsvr_type" {
  default     = "t3.medium"
  description = "instance type of the replica set servers"
}

#############
# Arbiters
#############

variable "arbiter_tag" {
  description = "Name of the arbiter servers"
  default     = "mongodb-arb"
}

variable "arbiter_type" {
  default     = "t3.medium"
  description = "instance type of the arbiter server"
}

variable "arbiter_ports" {
  type    = list(number)
  default = [22, 27017]
}

#############
# Instances
#############

variable "image" {
  description = "Available images by region"
  default = {
    us-west-2 = "ami-0ad8bfd4b10994785"
  }
}

# Save money by running spot instances but they may be terminated by AWS at any time
variable "use_spot_instances" {
  type    = bool
  default = false
}

variable "data_disk_type" {
  default = "gp2"
}

#############
# Networking
#############

variable "region" {
  type    = string
  default = "us-west-2"
}

variable "vpc" {
  type    = string
  default = "mongo"
}

variable "subnet_count" {
  type        = number
  default     = 3
  description = "How many subnets to use"
}

variable "subnet_cidr" {
  type    = string
  default = "10.128.0.0/20"
}

#############################
# Benchmark hardware tunables
#############################

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

variable "placement_group" {
  type        = string
  default     = null
  description = "Cluster placement group for all instances"
}
