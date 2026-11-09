locals {
  extensions_host = "${var.pmm_host}-extensions"
  extensions_port = 9000

  pmm_extensions_env = var.enable_extensions ? [
    "PMM_ENABLE_EXTENSIONS=1",
    "PMM_EXTENSIONS_ADDRESS=${local.extensions_host}:${local.extensions_port}",
    "PMM_EXTENSIONS_URL=http://${local.extensions_host}:${local.extensions_port}",
    "PMM_EXTENSIONS_TOKEN=${random_password.extensions_token[0].result}",
    "PMM_ENABLE_NOMAD=1",
    "PMM_PUBLIC_ADDRESS=${var.pmm_host}",
  ] : []
}

# Shared bearer token for pmm-managed's calls into PMM Extensions.
resource "random_password" "extensions_token" {
  count   = var.enable_extensions ? 1 : 0
  length  = 64
  special = false
}

# PMM writes the side-car's SECRET_KEY and database password here.
resource "docker_volume" "extensions_secrets" {
  count = var.enable_extensions ? 1 : 0
  name  = "${var.pmm_host}-extensions-secrets"
}

# Holds the side-car's minted ENCRYPTION_KEY, which must survive a container recreate.
resource "docker_volume" "extensions_state" {
  count = var.enable_extensions ? 1 : 0
  name  = "${var.pmm_host}-extensions-state"
}

data "docker_registry_image" "extensions" {
  count = var.enable_extensions ? 1 : 0
  name  = var.extensions_image
}

resource "docker_image" "extensions" {
  platform      = var.docker_platform
  count         = var.enable_extensions ? 1 : 0
  name          = "${replace(data.docker_registry_image.extensions[0].name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")}@${data.docker_registry_image.extensions[0].sha256_digest}"
  pull_triggers = [data.docker_registry_image.extensions[0].sha256_digest]
  keep_locally  = true
}

resource "docker_container" "extensions" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  count    = var.enable_extensions ? 1 : 0
  name     = local.extensions_host
  hostname = local.extensions_host
  image    = docker_image.extensions[0].image_id
  # PMM publishes the secrets as group 0, mode 0640.
  group_add = ["0"]
  env = [
    "SECRETS_DIR=/run/secrets/extensions",
    "EXTENSIONS_INTERNAL_TOKEN=${random_password.extensions_token[0].result}",
    "EXTENSIONS_DB_HOST=${var.pmm_host}",
    "EXTENSIONS_PMM_ENDPOINT=https://${var.pmm_host}:${var.pmm_port}",
    "EXTENSIONS_NOMAD_ENDPOINT=https://${var.pmm_host}:${var.pmm_port}/nomad",
    "BASE_URL=https://${var.pmm_host}:${var.pmm_port}/extensions",
    "GF_SECURITY_ADMIN_USER=${var.pmm_server_user}",
    "GF_SECURITY_ADMIN_PASSWORD=${var.pmm_server_pwd}",
    "TASKS__INVENTORY_SYNC_INTERVAL=${var.extensions_inventory_sync_minutes} minutes",
  ]
  mounts {
    type      = "volume"
    target    = "/run/secrets/extensions"
    source    = docker_volume.extensions_secrets[0].name
    read_only = true
  }
  mounts {
    type   = "volume"
    target = "/home/extensions/state"
    source = docker_volume.extensions_state[0].name
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  # The image's own healthcheck allows 150s for PMM provisioning and migrations.
  wait         = true
  wait_timeout = 300
  restart      = "on-failure"

  # The side-car mints its Grafana token with the admin login set there.
  depends_on = [
    null_resource.change_pmm_admin_password
  ]

  lifecycle {
    replace_triggered_by = [docker_image.extensions, docker_container.pmm]
  }
}
