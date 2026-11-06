terraform {
  required_providers {
    docker = { source = "kreuzwerker/docker" }
  }
}

variable "name" { type = string }
variable "environment_id" { type = string }
variable "ca_volume" { type = string }
variable "image" { type = string }
variable "dns_names" { type = list(string) }
variable "uid" { type = number }
variable "client_uid" {
  type        = number
  default     = 1002
  description = "Owner of the client-only volume; current PMM images run pmm-agent as UID 1002."
}
variable "generate_server" {
  type        = bool
  default     = true
  description = "Issue a cluster member identity as well as an automation identity. Disable for client-only consumers such as plgm."
}

resource "docker_volume" "certificate" {
  name = "${var.name}-tls"
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }
}

resource "terraform_data" "issue" {
  triggers_replace = [docker_volume.certificate.id, docker_volume.client.id, var.ca_volume, var.image, var.dns_names, var.uid, var.client_uid, var.generate_server]
  provisioner "local-exec" {
    command = "python3 \"${abspath(path.module)}/../../../../scripts/docker-tls.py\" issue"
    environment = {
      TLS_CA_VOLUME     = var.ca_volume
      TLS_IMAGE         = var.image
      TLS_VOLUME        = docker_volume.certificate.name
      TLS_NAME          = var.name
      TLS_DNS_NAMES     = jsonencode(var.dns_names)
      TLS_UID           = tostring(var.uid)
      TLS_CLIENT_VOLUME = docker_volume.client.name
      TLS_CLIENT_UID    = tostring(var.client_uid)
      TLS_SERVER        = tostring(var.generate_server)
    }
  }
}

resource "docker_volume" "client" {
  name = "${var.name}-tls-client"
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }
}

output "client_volume" {
  value      = docker_volume.client.name
  depends_on = [terraform_data.issue]
}

output "volume" {
  value      = docker_volume.certificate.name
  depends_on = [terraform_data.issue]
}
