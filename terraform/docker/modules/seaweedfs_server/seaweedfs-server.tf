data "docker_registry_image" "seaweedfs" {
  name = var.seaweedfs_image
}

locals {
  seaweedfs_repository = replace(data.docker_registry_image.seaweedfs.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
}

resource "docker_image" "seaweedfs" {
  platform      = var.docker_platform
  name          = "${local.seaweedfs_repository}@${data.docker_registry_image.seaweedfs.sha256_digest}"
  pull_triggers = [data.docker_registry_image.seaweedfs.sha256_digest]
  keep_locally  = true
}

resource "docker_volume" "seaweedfs_data" {
  name = "${var.seaweedfs_server}-data"
}

resource "docker_container" "seaweedfs" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name     = var.seaweedfs_server
  hostname = var.seaweedfs_server
  image    = docker_image.seaweedfs.image_id
  command = [
    "mini",
    "-dir=/data",
    "-s3.port=${var.seaweedfs_port}",
    "-master.port=${var.seaweedfs_admin_port}",
  ]
  network_mode = "bridge"
  restart      = "on-failure"

  env = [
    "AWS_ACCESS_KEY_ID=${var.seaweedfs_access_key}",
    "AWS_SECRET_ACCESS_KEY=${var.seaweedfs_secret_key}",
    "S3_BUCKET=${var.bucket_name}",
  ]

  volumes {
    volume_name    = docker_volume.seaweedfs_data.name
    container_path = "/data"
  }

  ports {
    internal = var.seaweedfs_port
    external = var.seaweedfs_port
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }

  ports {
    internal = var.seaweedfs_admin_port
    external = var.seaweedfs_admin_port
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }

  # The filer UI provides the bucket and object browser.
  ports {
    internal = 8888
    external = 8888
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }

  networks_advanced {
    name = var.network_name
  }

  healthcheck {
    test         = ["CMD-SHELL", "wget -q -O- http://localhost:${var.seaweedfs_admin_port}/cluster/status > /dev/null"]
    interval     = "10s"
    timeout      = "5s"
    retries      = 5
    start_period = "30s"
  }

  wait = true

  lifecycle {
    replace_triggered_by = [docker_image.seaweedfs]
  }
}
