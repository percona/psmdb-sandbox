locals {
  storage_config_content = templatefile("${path.module}/pbm-storage.tmpl", {
    minio_region     = var.minio_region
    bucket_name      = var.bucket_name
    cluster_name     = var.cluster_name
    minio_server     = var.minio_server
    minio_port       = var.minio_port
    minio_access_key = var.minio_access_key
    minio_secret_key = var.minio_secret_key
  })
}

# Create the PBM configuration file
resource "local_file" "storage_config" {
  filename = "${path.module}/pbm-storage.conf.${var.cluster_name}"
  content  = local.storage_config_content
}

# PBM CLI container
resource "docker_container" "pbm_cli" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name    = "${var.cluster_name}-${var.pbm_cli_container_suffix}"
  count   = var.enable_pbm ? 1 : 0
  image   = docker_image.pbm.image_id
  command = ["/bin/sh", "-c", "while true; do sleep 86400; done;"]
  env     = ["PBM_MONGODB_URI=mongodb://${var.mongodb_pbm_user}:${var.mongodb_pbm_password}@${docker_container.cfg[local.cfg_member_keys[0]].name}:${var.configsvr_port}${local.pbm_tls_query}"]
  dynamic "mounts" {
    for_each = var.use_tls ? [module.tls_certificate[local.cfg_member_keys[0]].volume] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-tls"
      read_only = true
    }
  }
  mounts {
    source = abspath(local_file.storage_config.filename)
    target = "/etc/pbm-storage.conf"
    type   = "bind"
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  healthcheck {
    test         = ["CMD-SHELL", "pbm version"]
    interval     = "10s"
    timeout      = "5s"
    retries      = 5
    start_period = "30s"
  }
  wait    = true
  restart = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.pbm]
  }
}
