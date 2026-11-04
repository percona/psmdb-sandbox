resource "docker_container" "pbm_rs" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each = var.enable_pbm ? local.replset_members : {}
  name     = "${var.rs_name}-${var.replset_tag}${each.value.member_index}-${var.pbm_container_suffix}"
  image    = docker_image.pbm_mongod_rs.image_id
  user     = var.uid
  # Physical restore starts mongod in the agent container using the node's
  # captured configuration, so the same key-service files must be available.
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
  env = concat(local.mongod_env, ["PBM_MONGODB_URI=${var.mongodb_pbm_user}:${var.mongodb_pbm_password}@${docker_container.rs[each.key].name}:${var.replset_port + each.value.member_index}"])
  mounts {
    type   = "volume"
    target = "/data/db"
    source = docker_volume.rs_volume[each.key].name
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
    replace_triggered_by = [docker_image.pbm_mongod_rs]
  }
}
