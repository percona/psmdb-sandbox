variable "environment_id" {
  description = "Exact environment identifier used for container selection."
  type        = string
  default     = ""
}

variable "client_name" {
  description = "Name of the container, also used as its hostname and PMM node name"
  type        = string
}

variable "base_image" {
  description = "Debian, Ubuntu or RHEL-compatible image the client is built on"
  default     = "ubuntu:24.04"
  type        = string
}

variable "pmm_client_repo" {
  description = "percona-release channel to install pmm-client from: release, testing or experimental"
  default     = "release"
  type        = string
}

variable "pmm_client_version" {
  description = "pmm-client version to install, for example 3.10.0. Empty installs the newest in the channel"
  default     = ""
  type        = string
}

variable "pmm_host" {
  description = "Name of the PMM server container"
  type        = string
}

variable "pmm_port" {
  description = "Port of the PMM server inside the Docker network"
  default     = "8443"
  type        = string
}

variable "pmm_server_user" {
  description = "Name of the PMM server admin user"
  default     = "admin"
  type        = string
}

variable "pmm_server_pwd" {
  description = "Password of the PMM server admin user"
  default     = "admin"
  type        = string
  sensitive   = true
}

variable "network_name" {
  type    = string
  default = "mongo-terraform"
}
