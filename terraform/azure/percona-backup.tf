# Storage Account for MongoDB Backups
resource "azurerm_storage_account" "mongo_backups" {
  count                    = var.create_backup_bucket ? 1 : 0
  name                     = lower(replace("${var.prefix}mongobackup", "-", ""))
  resource_group_name      = local.resource_group_name
  location                 = var.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  depends_on               = [time_sleep.wait_after_rg]
}

# Management Policy to delete old backups
resource "azurerm_storage_management_policy" "mongo_backup_policy" {
  count              = var.create_backup_bucket ? 1 : 0
  storage_account_id = azurerm_storage_account.mongo_backups[0].id

  rule {
    name    = "cleanup-old-backups"
    enabled = true

    filters {
      blob_types   = ["blockBlob"]
      prefix_match = [""] # Empty string means all blobs
    }

    actions {
      base_blob {
        delete_after_days_since_modification_greater_than = var.backup_retention
      }
    }
  }
}

# Storage Container 
resource "azurerm_storage_container" "mongo_backups_container" {
  count                 = var.create_backup_bucket ? 1 : 0
  name                  = "mongo-backups"
  storage_account_name  = azurerm_storage_account.mongo_backups[0].name
  container_access_type = "private"
}

locals {
  # PBM storage is configured when a container is created here or an existing one is provided.
  use_backup_storage = var.create_backup_bucket || var.existing_bucket_name != ""
  backup_bucket      = var.create_backup_bucket ? azurerm_storage_container.mongo_backups_container[0].name : var.existing_bucket_name
  backup_account     = var.create_backup_bucket ? azurerm_storage_account.mongo_backups[0].name : var.existing_storage_account
  backup_account_key = var.create_backup_bucket ? azurerm_storage_account.mongo_backups[0].primary_access_key : var.existing_bucket_secret_key
  backup_endpoint    = local.backup_account != "" ? "https://${local.backup_account}.blob.core.windows.net" : ""
}

# Output Storage Access Keys
output "access_key" {
  value     = local.backup_account_key
  sensitive = true
}

output "secret_key" {
  value     = local.backup_account_key
  sensitive = true
}
