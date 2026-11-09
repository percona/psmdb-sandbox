# Create a Docker container for the Grafana renderer
data "docker_registry_image" "renderer" {
  name = var.renderer_image
}

locals {
  renderer_repository   = replace(data.docker_registry_image.renderer.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
  watchtower_repository = replace(data.docker_registry_image.watchtower.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
  pmm_repository        = replace(data.docker_registry_image.pmm.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
}

resource "docker_image" "renderer" {
  platform      = var.docker_platform
  name          = "${local.renderer_repository}@${data.docker_registry_image.renderer.sha256_digest}"
  pull_triggers = [data.docker_registry_image.renderer.sha256_digest]
  keep_locally  = true
}

resource "docker_container" "renderer" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name         = "${var.pmm_host}-${var.renderer_tag}"
  hostname     = "${var.pmm_host}-${var.renderer_tag}"
  image        = docker_image.renderer.image_id
  env          = ["IGNORE_HTTPS_ERRORS=true"]
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  wait    = true
  restart = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.renderer]
  }
}

data "docker_registry_image" "watchtower" {
  name = var.watchtower_image
}

resource "docker_image" "watchtower" {
  name = "${local.watchtower_repository}@${data.docker_registry_image.watchtower.sha256_digest}"
  # percona/watchtower:latest currently publishes amd64 only. An explicit
  # override allows using a replacement image that supports another platform.
  platform      = var.docker_platform != null ? var.docker_platform : "linux/amd64"
  pull_triggers = [data.docker_registry_image.watchtower.sha256_digest]
  keep_locally  = true
}

# Create a Docker container for Watchtower
resource "docker_container" "watchtower" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name     = "${var.pmm_host}-${var.watchtower_tag}"
  hostname = "${var.pmm_host}-${var.watchtower_tag}"
  image    = docker_image.watchtower.image_id
  env      = ["WATCHTOWER_HTTP_API_TOKEN=${var.watchtower_token}", "WATCHTOWER_HTTP_API_UPDATE=1"]
  mounts {
    target = var.docker_socket
    source = var.docker_socket
    type   = "bind"
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  wait    = true
  restart = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.watchtower]
  }
}

# Create a Docker volume for PMM Server data
resource "docker_volume" "pmm_volume" {
  name = "${var.pmm_host}-data"
}

data "docker_registry_image" "pmm" {
  name = var.pmm_server_image
}

resource "docker_image" "pmm" {
  name          = "${local.pmm_repository}@${data.docker_registry_image.pmm.sha256_digest}"
  platform      = var.docker_platform
  pull_triggers = [data.docker_registry_image.pmm.sha256_digest]
  keep_locally  = true
}

locals {
  # Extract version tag, e.g., "2.38.1" from "percona/pmm-server:2.38.1"
  image_parts = split(":", var.pmm_server_image)
  image_tag   = length(local.image_parts) > 1 ? local.image_parts[length(local.image_parts) - 1] : "latest"

  is_v2 = startswith(local.image_tag, "2")

  readiness_path = local.is_v2 ? "/graph/login" : "/v1/readyz"

  test_cmd = [
    "CMD",
    "curl",
    "-k",
    "-f",
    "https://${var.pmm_host}:${var.pmm_port}${local.readiness_path}"
  ]
}

# Create a Docker container for the PMM server
resource "docker_container" "pmm" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name       = var.pmm_host
  hostname   = var.pmm_host
  domainname = var.domain_name
  depends_on = [
    docker_container.renderer
  ]
  image = docker_image.pmm.image_id
  env   = concat(["PMM_ENABLE_ACCESS_CONTROL=${var.pmm_access_control}", "GF_RENDERING_SERVER_URL=http://${docker_container.renderer.name}:${var.renderer_port}/render", "GF_RENDERING_CALLBACK_URL=https://${var.pmm_host}:${var.pmm_port}/graph/", "PMM_WATCHTOWER_HOST=http://${docker_container.watchtower.name}:${var.watchtower_port}", "PMM_WATCHTOWER_TOKEN=${var.watchtower_token}"], local.pmm_extensions_env)
  mounts {
    type   = "volume"
    target = "/srv"
    source = docker_volume.pmm_volume.name
  }
  dynamic "mounts" {
    for_each = docker_volume.extensions_secrets
    content {
      type   = "volume"
      target = "/srv/extensions"
      source = mounts.value.name
    }
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  ports {
    internal = var.pmm_port
    external = var.pmm_external_port
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }
  healthcheck {
    test         = local.test_cmd
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  # First boot initialises PMM's databases in an empty volume, which takes longer than 60s.
  wait         = true
  wait_timeout = 300
  restart      = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.pmm]
  }
}

resource "null_resource" "change_pmm_admin_password" {
  depends_on = [
    docker_container.pmm
  ]

  provisioner "local-exec" {
    command = <<-EOT
      docker exec -t ${var.pmm_host} bash -c  "grafana cli --homepath /usr/share/grafana --config=/etc/grafana/grafana.ini admin reset-admin-password ${var.pmm_server_pwd}"
    EOT
  }
}
