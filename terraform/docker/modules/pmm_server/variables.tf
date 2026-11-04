variable "environment_id" {
  description = "Exact environment identifier used for container selection."
  type        = string
  default     = ""
}

#############
# PMM Server
#############

variable "pmm_host" {
  description = "Name of the PMM server"
  default     = "pmm-server"
  type        = string
}

variable "domain_name" {
  description = "Name of the DNS domain"
  default     = ""
}

variable "env_tag" {
  description = "Name of the Environment"
  default     = "test"
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

variable "renderer_tag" {
  description = "Name of the Grafana image renderer container"
  default     = "grafana-renderer"
  type        = string
}

variable "watchtower_tag" {
  description = "Name of the Watchtower container"
  default     = "watchtower"
  type        = string
}

variable "pmm_port" {
  description = "Port of the PMM server inside docker network"
  # PMM 3 uses 8443. PMM 2 uses 443
  # default = "443"
  default = "8443"
  type    = string
}

variable "pmm_external_port" {
  description = "Port of the PMM server as seen from outside docker"
  default     = "8443"
  type        = string
}

variable "renderer_port" {
  description = "Port of the Grafana renderer"
  default     = "8081"
  type        = string
}

variable "watchtower_port" {
  description = "Port of the Watchtower"
  default     = "8080"
  type        = string
}

variable "watchtower_token" {
  description = "Watchtower API token"
  default     = "1234567890"
  type        = string
}

variable "pmm_access_control" {
  description = "Enable access control"
  default     = "1"
  type        = string
}

#################
# PMM Extensions
#################

variable "enable_extensions" {
  description = "Run the PMM Extensions side-car next to this PMM server. Needs PMM Server 3.10 or newer"
  default     = false
  type        = bool
}

variable "extensions_image" {
  description = "Docker image for the PMM Extensions side-car"
  default     = "percona/pmm-extensions:3.10.0-rc1"
  type        = string
}

variable "extensions_inventory_sync_minutes" {
  description = "How often PMM Extensions syncs PMM's inventory, in minutes"
  default     = 1
  type        = number
}

###############
# Docker Images
###############

variable "renderer_image" {
  description = "Docker image for Grafana renderer container"
  default     = "grafana/grafana-image-renderer:latest"
  type        = string
}

variable "watchtower_image" {
  description = "Docker image for Watchtower container"
  default     = "percona/watchtower:latest"
  type        = string
}

variable "pmm_server_image" {
  description = "Docker image for PMM server"
  #default = "perconalab/pmm-server:3-dev-latest"
  #default = "percona/pmm-server:3.0"
  default = "percona/pmm-server:latest"
  type    = string
}

variable "docker_socket" {
  description = "Location of the socket file for docker"
  default     = "/var/run/docker.sock"
  type        = string
}

#############
# Networking
#############

variable "network_name" {
  type    = string
  default = "mongo-terraform"
}

variable "bind_to_localhost" {
  type        = bool
  default     = true
  description = "Bind container ports to localhost (127.0.0.1) if true, otherwise to 0.0.0.0"
}
