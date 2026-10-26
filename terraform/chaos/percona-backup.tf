resource "chaos_instance" "seaweedfs" {
  count             = var.enable_seaweedfs ? 1 : 0
  name              = local.seaweedfs_host
  os                = local.default_os_image
  cpu_cores         = var.seaweedfs_cpu_cores
  memory            = var.seaweedfs_memory_gb
  disk              = var.seaweedfs_volume_size
  ssh_user          = var.my_ssh_user
  description       = "${var.prefix} - SeaweedFS S3-compatible backup storage"
  delete_after_days = var.delete_after_days

  user_data = <<-CLOUDINIT
    #cloud-config
    runcmd:
      - hostnamectl set-hostname "${local.seaweedfs_host}"
      - echo "127.0.0.1 $(hostname) localhost" > /etc/hosts
      - mkdir -p /data/seaweedfs
  CLOUDINIT

  firewall_rules = toset(concat(
    var.firewall_rules,
    length(var.firewall_rules) == 0 && var.source_ranges != "" ? [
      {
        source   = var.source_ranges
        port     = tostring(var.seaweedfs_port)
        protocol = "tcp"
        comment  = "Allow SeaweedFS S3 access"
      },
      {
        source   = var.source_ranges
        port     = tostring(var.seaweedfs_admin_port)
        protocol = "tcp"
        comment  = "Allow SeaweedFS admin access"
      },
      {
        source   = var.source_ranges
        port     = "8888"
        protocol = "tcp"
        comment  = "Allow SeaweedFS object browser access"
      },
    ] : [],
    [
      {
        source   = "10.30.0.0/16"
        port     = tostring(var.seaweedfs_port)
        protocol = "tcp"
        comment  = "Allow SeaweedFS S3 access from subnet"
      },
      {
        source   = "10.30.0.0/16"
        port     = tostring(var.seaweedfs_admin_port)
        protocol = "tcp"
        comment  = "Allow SeaweedFS admin access from subnet"
      },
      {
        source   = "10.30.0.0/16"
        port     = "8888"
        protocol = "tcp"
        comment  = "Allow SeaweedFS object browser access from subnet"
      },
    ]
  ))
}

locals {
  # PBM storage is either the SeaweedFS VM deployed here or an existing
  # S3-compatible bucket.
  use_existing_bucket     = !var.enable_seaweedfs && var.existing_bucket_name != ""
  use_backup_storage      = var.enable_seaweedfs || local.use_existing_bucket
  backup_storage_provider = local.use_existing_bucket ? "s3" : "minio"
  backup_bucket           = local.use_existing_bucket ? var.existing_bucket_name : local.bucket_name
  backup_region           = local.use_existing_bucket && var.existing_bucket_region != "" ? var.existing_bucket_region : "us-east-1"
  backup_endpoint         = local.use_existing_bucket ? var.existing_bucket_endpoint : (var.enable_seaweedfs ? "http://${chaos_instance.seaweedfs[0].ip_address}:${var.seaweedfs_port}" : "")
  backup_access_key       = local.use_existing_bucket ? var.existing_bucket_access_key : var.seaweedfs_access_key
  backup_secret_key       = local.use_existing_bucket ? var.existing_bucket_secret_key : var.seaweedfs_secret_key
}

output "seaweedfs_access_key" {
  value       = var.seaweedfs_access_key
  description = "SeaweedFS access key for backup configuration"
}

output "seaweedfs_secret_key" {
  value       = var.seaweedfs_secret_key
  sensitive   = true
  description = "SeaweedFS secret key for backup configuration"
}

output "seaweedfs_endpoint" {
  value       = var.enable_seaweedfs ? "http://${chaos_instance.seaweedfs[0].ip_address}:${var.seaweedfs_port}" : ""
  description = "SeaweedFS S3-compatible endpoint URL for backup configuration"
}
