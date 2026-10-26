# Create an S3 bucket for MongoDB backups
resource "aws_s3_bucket" "mongo_backups" {
  count         = var.create_backup_bucket ? 1 : 0
  bucket        = local.bucket_name
  force_destroy = true
  tags = {
    Name = local.bucket_name
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "mongo_backups_lifecycle" {
  count  = var.create_backup_bucket ? 1 : 0
  bucket = aws_s3_bucket.mongo_backups[0].id

  rule {
    id     = "BackupLifecycleRule"
    status = "Enabled"

    expiration {
      days = var.backup_retention
    }
  }
}

resource "aws_s3_bucket_versioning" "mongo_backups_versioning" {
  count  = var.create_backup_bucket ? 1 : 0
  bucket = aws_s3_bucket.mongo_backups[0].id

  versioning_configuration {
    status = "Enabled"
  }
}

# Create an IAM role for MongoDB backup service
resource "aws_iam_role" "mongo_backup_service_role" {
  count = var.create_backup_bucket ? 1 : 0
  name  = "${local.bucket_name}-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })
}

# Attach the S3 access policy to the IAM role
resource "aws_iam_role_policy" "mongo_backup_policy" {
  count = var.create_backup_bucket ? 1 : 0
  name  = "${local.bucket_name}-policy"
  role  = aws_iam_role.mongo_backup_service_role[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject",
          "s3:GetObject",
          "s3:ListBucket",
          "s3:DeleteObject"
        ]
        Resource = [
          "${aws_s3_bucket.mongo_backups[0].arn}",
          "${aws_s3_bucket.mongo_backups[0].arn}/*"
        ]
      }
    ]
  })
}

# Create an IAM user for the MongoDB backup service
resource "aws_iam_user" "mongo_backup_service_account" {
  count = var.create_backup_bucket ? 1 : 0
  name  = "${local.bucket_name}-user"
}

# Attach an access policy to the IAM user
resource "aws_iam_user_policy" "mongo_backup_user_policy" {
  count = var.create_backup_bucket ? 1 : 0
  name  = "${local.bucket_name}-user-policy"
  user  = aws_iam_user.mongo_backup_service_account[0].name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject",
          "s3:GetObject",
          "s3:ListBucket",
          "s3:DeleteObject"
        ]
        Resource = [
          "${aws_s3_bucket.mongo_backups[0].arn}",
          "${aws_s3_bucket.mongo_backups[0].arn}/*"
        ]
      }
    ]
  })
}

# Create access keys for the MongoDB backup service user
resource "aws_iam_access_key" "mongo_backup_access_key" {
  count = var.create_backup_bucket ? 1 : 0
  user  = aws_iam_user.mongo_backup_service_account[0].name
}

locals {
  # PBM storage is configured when a bucket is created here or an existing one is provided.
  use_backup_storage = var.create_backup_bucket || var.existing_bucket_name != ""
  backup_bucket      = var.create_backup_bucket ? aws_s3_bucket.mongo_backups[0].bucket : var.existing_bucket_name
  backup_region      = var.create_backup_bucket ? aws_s3_bucket.mongo_backups[0].region : (var.existing_bucket_region != "" ? var.existing_bucket_region : var.region)
  backup_endpoint    = "https://s3.${local.backup_region}.amazonaws.com"
  backup_access_key  = var.create_backup_bucket ? aws_iam_access_key.mongo_backup_access_key[0].id : var.existing_bucket_access_key
  backup_secret_key  = var.create_backup_bucket ? aws_iam_access_key.mongo_backup_access_key[0].secret : var.existing_bucket_secret_key
}

output "access_key" {
  value = local.backup_access_key
}

output "secret_key" {
  value     = local.backup_secret_key
  sensitive = true
}
