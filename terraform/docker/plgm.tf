#############
# plgm (Percona Load Generator for MongoDB)
# https://github.com/Percona-Lab/percona-load-generator-mongodb
#
# The container runs the plgm Web UI with no preconfigured workload. plgm binds
# the UI to 127.0.0.1 only, so socat inside the container forwards
# plgm_proxy_port to it, and that port is published on the host loopback as
# http://localhost:<plgm_external_port>. Connect plgm to the cluster from its UI.
#############

# Resolve the latest plgm release and the SHA-256 digest GitHub publishes for
# its linux-amd64 asset
data "http" "plgm_latest_release" {
  count = var.enable_plgm ? 1 : 0
  url   = "https://api.github.com/repos/Percona-Lab/percona-load-generator-mongodb/releases/latest"
  request_headers = {
    Accept = "application/vnd.github+json"
  }

  lifecycle {
    postcondition {
      condition     = self.status_code == 200
      error_message = "Could not look up the latest plgm release (HTTP ${self.status_code})."
    }
  }
}

locals {
  plgm_release = var.enable_plgm ? jsondecode(data.http.plgm_latest_release[0].response_body) : null
  plgm_asset   = var.enable_plgm ? one([for a in local.plgm_release.assets : a if a.name == "plgm-linux-amd64.tar.gz"]) : null
  plgm_version = var.enable_plgm ? local.plgm_release.tag_name : ""
  plgm_sha256  = var.enable_plgm ? trimprefix(try(local.plgm_asset.digest, ""), "sha256:") : ""
}

locals {
  plgm_platform        = "linux/amd64" # plgm only publishes linux-amd64 binaries
  plgm_webui_port      = 9999
  plgm_proxy_port      = 9998
  plgm_dockerfile_name = replace(var.plgm_image, "/", "-")
  plgm_os_repository   = var.enable_plgm ? replace(data.docker_registry_image.plgm_os[0].name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "") : ""
  plgm_dockerfile_content = templatefile("${path.module}/plgm.Dockerfile.tmpl", {
    plgm_os_image   = var.enable_plgm ? "${local.plgm_os_repository}@${data.docker_registry_image.plgm_os[0].sha256_digest}" : ""
    plgm_version    = local.plgm_version
    plgm_sha256     = local.plgm_sha256
    plgm_webui_port = local.plgm_webui_port
    plgm_proxy_port = local.plgm_proxy_port
  })
}

# Write plgm Dockerfile to disk
resource "local_file" "plgm_dockerfile_content" {
  count    = var.enable_plgm ? 1 : 0
  filename = "${path.module}/${local.plgm_dockerfile_name}.Dockerfile"
  content  = local.plgm_dockerfile_content
}

# Resolve the base OS image digest
data "docker_registry_image" "plgm_os" {
  count = var.enable_plgm ? 1 : 0
  name  = var.plgm_os_image
}

resource "docker_image" "plgm_os" {
  count         = var.enable_plgm ? 1 : 0
  name          = "${local.plgm_os_repository}@${data.docker_registry_image.plgm_os[0].sha256_digest}"
  platform      = local.plgm_platform
  pull_triggers = [data.docker_registry_image.plgm_os[0].sha256_digest]
  keep_locally  = true
}

# Build plgm Docker image
resource "docker_image" "plgm" {
  count = var.enable_plgm ? 1 : 0
  depends_on = [
    docker_image.plgm_os,
    local_file.plgm_dockerfile_content,
  ]
  name         = var.plgm_image
  keep_locally = true
  triggers = {
    os_digest    = data.docker_registry_image.plgm_os[0].sha256_digest
    plgm_version = local.plgm_version
    plgm_sha256  = local.plgm_sha256
  }
  build {
    context    = path.module
    dockerfile = "${local.plgm_dockerfile_name}.Dockerfile"
    platform   = local.plgm_platform
  }

  lifecycle {
    precondition {
      condition     = can(regex("^[0-9a-f]{64}$", local.plgm_sha256))
      error_message = "plgm release ${local.plgm_version} has no plgm-linux-amd64.tar.gz asset with a SHA-256 digest."
    }
  }
}

# Create plgm container
resource "docker_container" "plgm" {
  count        = var.enable_plgm ? 1 : 0
  name         = "${local.name_prefix}${var.plgm_container_suffix}"
  hostname     = "${local.name_prefix}${var.plgm_container_suffix}"
  image        = docker_image.plgm[0].image_id
  network_mode = "bridge"
  networks_advanced {
    name = "${local.name_prefix}${var.network_name}"
  }
  ports {
    internal = local.plgm_proxy_port
    external = var.plgm_external_port
    ip       = "127.0.0.1"
  }
  healthcheck {
    test         = ["CMD-SHELL", "wget -q -O /dev/null http://127.0.0.1:${local.plgm_proxy_port}/ || exit 1"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  wait    = false
  restart = "on-failure"

  depends_on = [docker_network.mongo_network]
}
