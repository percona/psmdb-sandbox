variable "environment_id" {
  description = "Exact environment identifier used for container selection."
  type        = string
  default     = ""
}

variable "seaweedfs_server" {
  type    = string
  default = "seaweedfs"
}

variable "seaweedfs_image" {
  type        = string
  description = "SeaweedFS Docker image"
  default     = "chrislusf/seaweedfs:latest"
}

variable "seaweedfs_port" {
  type    = number
  default = 8333
}

variable "seaweedfs_admin_port" {
  type    = number
  default = 9333
}

variable "seaweedfs_access_key" {
  type      = string
  sensitive = true
  default   = "seaweedfs"
}

variable "seaweedfs_secret_key" {
  type      = string
  sensitive = true
  default   = "seaweedfs-secret"
}

variable "bucket_name" {
  type    = string
  default = "mongo-backups"
}

variable "network_name" {
  type    = string
  default = "mongo-terraform"
}

variable "bind_to_localhost" {
  type    = bool
  default = true
}
