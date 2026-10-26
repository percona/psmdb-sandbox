terraform {
  required_version = ">= 1.9"

  backend "local" {}

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.68.0"
    }
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = var.labels
  }
}

resource "aws_placement_group" "cluster" {
  count    = var.compact_placement ? 1 : 0
  name     = "${var.prefix}-cluster-placement"
  strategy = "cluster"
}

locals {
  placement_group           = var.compact_placement ? aws_placement_group.cluster[0].id : null
  configsvr_disk_type       = coalesce(var.configsvr_disk_type, var.data_disk_type)
  configsvr_disk_iops       = local.configsvr_disk_type == var.data_disk_type ? var.data_disk_iops : null
  configsvr_disk_throughput = local.configsvr_disk_type == var.data_disk_type ? var.data_disk_throughput : null
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
  region                = var.region
  subnet_cidr           = var.subnet_cidr
  subnet_count          = var.subnet_count
  my_key_pair           = local.my_key_pair
  my_ssh_user           = var.my_ssh_user
  ssh_public_key_path   = var.ssh_public_key_path
  image                 = var.image
  use_spot_instances    = var.use_spot_instances
  data_disk_type        = var.data_disk_type
  shardsvr_type         = var.shardsvr_type
  shardsvr_volume_size  = var.shardsvr_volume_size
  configsvr_type        = var.configsvr_type
  configsvr_volume_size = var.configsvr_volume_size
  mongos_type           = var.mongos_type
  arbiter_type          = var.arbiter_type

  data_disk_iops            = var.data_disk_iops
  data_disk_throughput      = var.data_disk_throughput
  configsvr_disk_type       = local.configsvr_disk_type
  configsvr_disk_iops       = local.configsvr_disk_iops
  configsvr_disk_throughput = local.configsvr_disk_throughput
  boot_disk_type            = var.boot_disk_type
  boot_disk_size            = var.boot_disk_size
  placement_group           = local.placement_group

  depends_on = [aws_s3_bucket.mongo_backups, aws_subnet.vpc-subnet, aws_key_pair.my_key_pair, aws_route53_zone.private_zone]
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
  region                 = var.region
  subnet_cidr            = var.subnet_cidr
  subnet_count           = var.subnet_count
  my_key_pair            = local.my_key_pair
  my_ssh_user            = var.my_ssh_user
  ssh_public_key_path    = var.ssh_public_key_path
  image                  = var.image
  use_spot_instances     = var.use_spot_instances
  data_disk_type         = var.data_disk_type
  replsetsvr_type        = var.replsetsvr_type
  replsetsvr_volume_size = var.replsetsvr_volume_size
  arbiter_type           = var.arbiter_type

  data_disk_iops       = var.data_disk_iops
  data_disk_throughput = var.data_disk_throughput
  boot_disk_type       = var.boot_disk_type
  boot_disk_size       = var.boot_disk_size
  placement_group      = local.placement_group

  depends_on = [aws_s3_bucket.mongo_backups, aws_subnet.vpc-subnet, aws_key_pair.my_key_pair, aws_route53_zone.private_zone]
}
