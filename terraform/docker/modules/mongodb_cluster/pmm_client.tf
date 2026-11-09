data "docker_registry_image" "pmm_client" {
  name = var.pmm_client_image
}

locals {
  pmm_client_repository = replace(data.docker_registry_image.pmm_client.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
}

resource "docker_image" "pmm_client" {
  platform      = var.docker_platform
  name          = "${local.pmm_client_repository}@${data.docker_registry_image.pmm_client.sha256_digest}"
  pull_triggers = [data.docker_registry_image.pmm_client.sha256_digest]
  keep_locally  = true
}
