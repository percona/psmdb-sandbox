locals {
  image_dir = "${path.module}/image"
}

resource "docker_image" "client" {
  platform = var.docker_platform
  name     = "${var.client_name}-os"
  triggers = {
    build_args = jsonencode([var.base_image, var.pmm_client_repo, var.pmm_client_version])
    sources    = sha1(join("", [for f in fileset(local.image_dir, "*") : filesha1("${local.image_dir}/${f}")]))
  }
  build {
    platform = var.docker_platform
    context  = local.image_dir
    build_args = {
      BASE_IMAGE         = var.base_image
      PMM_CLIENT_REPO    = var.pmm_client_repo
      PMM_CLIENT_VERSION = var.pmm_client_version
    }
  }
}

resource "docker_container" "client" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name     = var.client_name
  hostname = var.client_name
  image    = docker_image.client.image_id

  # systemd runs as PID 1, and the Nomad client in pmm-agent manages cgroups for its tasks.
  cgroupns_mode = "host"
  security_opts = ["apparmor:unconfined", "seccomp:unconfined"]
  capabilities {
    add = ["SYS_ADMIN"]
  }
  tmpfs = {
    "/run"      = ""
    "/run/lock" = ""
  }
  mounts {
    type   = "bind"
    target = "/sys/fs/cgroup"
    source = "/sys/fs/cgroup"
  }

  upload {
    file    = "/etc/pmm-agent-register.env"
    content = <<-EOT
      PMM_SERVER_ADDRESS='${var.pmm_host}:${var.pmm_port}'
      PMM_SERVER_USERNAME='${var.pmm_server_user}'
      PMM_SERVER_PASSWORD='${var.pmm_server_pwd}'
    EOT
  }

  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  healthcheck {
    test         = ["CMD", "pmm-admin", "status"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "60s"
  }
  wait         = true
  wait_timeout = 300
  restart      = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.client]
  }
}
