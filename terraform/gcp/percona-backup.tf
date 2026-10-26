resource "google_storage_bucket" "mongo-backups" {
  count                       = var.create_backup_bucket ? 1 : 0
  name                        = local.bucket_name
  location                    = var.region
  force_destroy               = true
  uniform_bucket_level_access = true

  lifecycle_rule {
    condition {
      age = var.backup_retention
    }
    action {
      type = "Delete"
    }
  }
}

resource "google_service_account" "mongo-backup-service-account" {
  count        = var.create_backup_bucket ? 1 : 0
  account_id   = "${var.prefix}-mongo-backup-sa"
  display_name = "Mongo Backup Service Account"
}

resource "google_storage_hmac_key" "mongo-backup-service-account" {
  count                 = var.create_backup_bucket ? 1 : 0
  service_account_email = google_service_account.mongo-backup-service-account[0].email
}

resource "google_storage_bucket_iam_binding" "binding" {
  count  = var.create_backup_bucket ? 1 : 0
  bucket = google_storage_bucket.mongo-backups[0].name
  role   = "roles/storage.admin"
  members = [
    "serviceAccount:${google_service_account.mongo-backup-service-account[0].email}",
  ]
}

locals {
  # PBM storage is configured when a bucket is created here or an existing one is provided.
  use_backup_storage = var.create_backup_bucket || var.existing_bucket_name != ""
  backup_bucket      = var.create_backup_bucket ? google_storage_bucket.mongo-backups[0].name : var.existing_bucket_name
  backup_access_key  = var.create_backup_bucket ? google_storage_hmac_key.mongo-backup-service-account[0].access_id : var.existing_bucket_access_key
  backup_secret_key  = var.create_backup_bucket ? google_storage_hmac_key.mongo-backup-service-account[0].secret : var.existing_bucket_secret_key
}

output "access_key" {
  value = local.backup_access_key
}

output "secret_key" {
  value     = local.backup_secret_key
  sensitive = true
}
