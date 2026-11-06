resource "docker_container" "pbm_cfg" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each = var.enable_pbm ? local.cfg_members : {}
  name     = "${var.cluster_name}-${var.configsvr_tag}0${each.value}-${var.pbm_container_suffix}"
  image    = docker_image.pbm_mongod.image_id
  user     = var.uid
  dynamic "mounts" {
    for_each = var.use_tls ? [module.tls_certificate[each.key].volume] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-tls"
      read_only = true
    }
  }
  dynamic "mounts" {
    for_each = var.vault_encryption ? [var.vault_credentials] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-vault"
      read_only = true
    }
  }
  dynamic "mounts" {
    for_each = var.vault_encryption ? [docker_volume.keyfile_volume.name] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = var.keyfile_path
      read_only = true
    }
  }
  command = [
    "pbm-agent"
  ]
  env = concat(local.mongod_env, ["PBM_MONGODB_URI=mongodb://${var.mongodb_pbm_user}:${var.mongodb_pbm_password}@${docker_container.cfg[each.key].name}:${var.configsvr_port}${local.pbm_tls_query}"])
  mounts {
    type   = "volume"
    target = "/data/db"
    source = docker_volume.cfg_volume[each.key].name
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  healthcheck {
    test         = ["CMD-SHELL", "pbm version"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  wait    = true
  restart = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.pbm_mongod]
  }
}
