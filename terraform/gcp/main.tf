terraform {
  required_version = ">= 1.9"

  backend "local" {}

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }

  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

module "mongodb_clusters" {
  source                = "./modules/mongodb_cluster"
  for_each              = var.clusters
  cluster_name          = each.key
  prefix                = var.prefix
  env_tag               = each.value.env_tag
  configsvr_count       = each.value.configsvr_count
  shard_count           = each.value.shard_count
  shardsvr_replicas     = each.value.shardsvr_replicas
  arbiters_per_replset  = each.value.arbiters_per_replset
  mongos_count          = each.value.mongos_count
  enable_audit          = each.value.enable_audit
  enable_mongot         = each.value.enable_mongot
  audit_filter          = each.value.audit_filter
  vpc                   = local.vpc
  subnet_name           = google_compute_subnetwork.vpc-subnet.name
  region                = var.region
  subnet_cidr           = var.subnet_cidr
  gce_ssh_users         = var.gce_ssh_users
  my_ssh_user           = var.my_ssh_user
  image                 = var.image
  use_spot_instances    = var.use_spot_instances
  data_disk_type        = var.data_disk_type
  shardsvr_type         = var.shardsvr_type
  shardsvr_volume_size  = var.shardsvr_volume_size
  configsvr_type        = var.configsvr_type
  configsvr_volume_size = var.configsvr_volume_size
  mongos_type           = var.mongos_type
  arbiter_type          = var.arbiter_type

  zones                     = local.zones
  data_disk_iops            = var.data_disk_iops
  data_disk_throughput      = var.data_disk_throughput
  configsvr_disk_type       = local.configsvr_disk_type
  configsvr_disk_iops       = local.configsvr_disk_iops
  configsvr_disk_throughput = local.configsvr_disk_throughput
  boot_disk_type            = var.boot_disk_type
  boot_disk_size            = var.boot_disk_size
  min_cpu_platform          = var.min_cpu_platform
  nic_type                  = var.nic_type
  tier1_networking          = var.tier1_networking
  resource_policies         = local.placement_policies
  labels                    = var.labels

  depends_on = [google_storage_bucket.mongo-backups, google_compute_subnetwork.vpc-subnet]
}

module "mongodb_replsets" {
  source                 = "./modules/mongodb_replset"
  for_each               = var.replsets
  rs_name                = each.key
  prefix                 = var.prefix
  env_tag                = each.value.env_tag
  data_nodes_per_replset = each.value.data_nodes_per_replset
  arbiters_per_replset   = each.value.arbiters_per_replset
  enable_audit           = each.value.enable_audit
  audit_filter           = each.value.audit_filter
  vpc                    = local.vpc
  subnet_name            = google_compute_subnetwork.vpc-subnet.name
  subnet_cidr            = var.subnet_cidr
  region                 = var.region
  my_ssh_user            = var.my_ssh_user
  gce_ssh_users          = var.gce_ssh_users
  image                  = var.image
  use_spot_instances     = var.use_spot_instances
  data_disk_type         = var.data_disk_type
  replsetsvr_type        = var.replsetsvr_type
  replsetsvr_volume_size = var.replsetsvr_volume_size
  arbiter_type           = var.arbiter_type

  zones                = local.zones
  data_disk_iops       = var.data_disk_iops
  data_disk_throughput = var.data_disk_throughput
  boot_disk_type       = var.boot_disk_type
  boot_disk_size       = var.boot_disk_size
  min_cpu_platform     = var.min_cpu_platform
  nic_type             = var.nic_type
  tier1_networking     = var.tier1_networking
  resource_policies    = local.placement_policies
  labels               = var.labels

  depends_on = [google_storage_bucket.mongo-backups, google_compute_subnetwork.vpc-subnet]
}
