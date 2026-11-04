locals {
  mongot_password_file = abspath("${path.module}/${var.cluster_name}-mongot-password")
}

resource "local_sensitive_file" "mongot_password" {
  count           = var.enable_mongot ? 1 : 0
  filename        = local.mongot_password_file
  content         = var.mongodb_mongot_password
  file_permission = "0400"
}

resource "local_file" "mongot_config" {
  for_each = var.enable_mongot ? local.shard_members : {}
  filename = abspath("${path.module}/${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-mongot.yml")
  content  = <<-YAML
syncSource:
  replicaSet:
    hostAndPort: "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}:${var.shardsvr_port}"
    scramAuth:
      username: "${var.mongodb_mongot_user}"
      passwordFile: "/etc/mongot/passwordFile"
      authSource: "admin"
      tls:
        enabled: false
  router:
    hostAndPort: "${var.cluster_name}-${var.mongos_tag}00:${var.mongos_port}"
    scramAuth:
      username: "${var.mongodb_mongot_user}"
      passwordFile: "/etc/mongot/passwordFile"
      authSource: "admin"
      tls:
        enabled: false
  replicationReader:
    readPreference: "secondaryPreferred"
storage:
  dataPath: "/data/mongot"
server:
  grpc:
    address: "0.0.0.0:${var.mongot_port}"
    tls:
      mode: "disabled"
metrics:
  enabled: true
  address: "0.0.0.0:${var.mongot_metrics_port}"
healthCheck:
  address: "0.0.0.0:${var.mongot_health_port}"
logging:
  verbosity: INFO
YAML
}

resource "docker_volume" "mongot_volume" {
  for_each = var.enable_mongot ? local.shard_members : {}
  name     = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-mongot-data"
}

resource "docker_container" "mongot_shard" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each = var.enable_mongot ? local.shard_members : {}
  name     = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-mongot"
  hostname = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-mongot"
  image    = docker_image.mongot.image_id

  mounts {
    source    = local_file.mongot_config[each.key].filename
    target    = "/mongot-community/config.default.yml"
    type      = "bind"
    read_only = true
  }

  mounts {
    source    = local_sensitive_file.mongot_password[0].filename
    target    = "/etc/mongot/passwordFile"
    type      = "bind"
    read_only = true
  }

  mounts {
    type   = "volume"
    target = "/data/mongot"
    source = docker_volume.mongot_volume[each.key].name
  }

  networks_advanced {
    name = var.network_name
  }

  wait       = false
  restart    = "unless-stopped"
  depends_on = [null_resource.create_users, docker_container.mongos, local_file.mongot_config, local_sensitive_file.mongot_password]

  lifecycle {
    replace_triggered_by = [docker_image.mongot]
  }
}
