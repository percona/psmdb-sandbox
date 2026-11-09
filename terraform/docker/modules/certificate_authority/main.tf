terraform {
  required_providers {
    docker = { source = "kreuzwerker/docker" }
  }
}

variable "name" { type = string }
variable "environment_id" { type = string }

resource "docker_image" "signer" {
  platform = var.docker_platform
  name     = "psmdb-sandbox-tls-signer:alpine-3.22"
  triggers = {
    dockerfile = filesha256("${path.module}/Dockerfile")
  }
  build {
    platform = var.docker_platform
    context  = path.module
  }
  keep_locally = true
}

resource "docker_volume" "ca" {
  name = "${var.name}-ca"
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }
}

resource "terraform_data" "prepare" {
  triggers_replace = [docker_volume.ca.id, docker_volume.trust.id, docker_image.signer.image_id]
  provisioner "local-exec" {
    command = "python3 \"${abspath(path.module)}/../../../../scripts/docker-tls.py\" prepare"
    environment = {
      TLS_CA_VOLUME    = docker_volume.ca.name
      TLS_IMAGE        = docker_image.signer.image_id
      TLS_TRUST_VOLUME = docker_volume.trust.name
    }
  }
}

resource "docker_volume" "trust" {
  name = "${var.name}-ca-trust"
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }
}

output "trust_volume" {
  value      = docker_volume.trust.name
  depends_on = [terraform_data.prepare]
}

output "volume" {
  value      = docker_volume.ca.name
  depends_on = [terraform_data.prepare]
}
output "image" { value = docker_image.signer.image_id }
