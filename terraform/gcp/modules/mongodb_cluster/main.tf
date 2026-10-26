terraform {
  required_version = ">= 1.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }
}

locals {
  # Disk types that accept provisioned performance settings
  iops_disk_types       = ["pd-extreme", "hyperdisk-balanced", "hyperdisk-extreme"]
  throughput_disk_types = ["hyperdisk-balanced", "hyperdisk-throughput"]

  data_disk_iops       = contains(local.iops_disk_types, var.data_disk_type) ? var.data_disk_iops : null
  data_disk_throughput = contains(local.throughput_disk_types, var.data_disk_type) ? var.data_disk_throughput : null

  # Spot and compact placement both require instances to terminate on host maintenance
  on_host_maintenance = var.use_spot_instances || length(var.resource_policies) > 0 ? "TERMINATE" : "MIGRATE"
}

locals {
  configsvr_disk_iops       = contains(local.iops_disk_types, var.configsvr_disk_type) ? var.configsvr_disk_iops : null
  configsvr_disk_throughput = contains(local.throughput_disk_types, var.configsvr_disk_type) ? var.configsvr_disk_throughput : null
}
