resource "docker_volume" "arb_volume" {
  for_each = local.arbiter_members
  name     = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}-data"
}

resource "docker_container" "arbiter" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each   = local.arbiter_members
  name       = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"
  hostname   = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"
  domainname = var.domain_name
  image      = docker_image.psmdb.image_id
  env        = local.mongod_env
  mounts {
    source    = docker_volume.keyfile_volume.name
    target    = var.keyfile_path
    type      = "volume"
    read_only = true
  }
  command = concat(
    [
      "mongod",
      "--replSet", "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}",
      "--bind_ip_all",
      "--port", "${var.arbiter_port}",
      "--shardsvr",
      "--keyFile", "${var.keyfile_path}/${var.keyfile_name}"
    ],
    var.enable_audit ? [
      "--auditDestination", "file",
      "--auditFormat", "JSON",
      "--auditPath", "/var/log/mongodb-audit.json",
      "--auditFilter", "${var.audit_filter}",
      "--setParameter", "auditAuthorizationSuccess=true"
    ] : []
  )
  ports {
    internal = var.arbiter_port
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }
  user = var.uid
  labels {
    label = "replsetName"
    value = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}"
  }
  labels {
    label = "environment"
    value = var.env_tag
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  mounts {
    type   = "volume"
    target = "/data/db"
    source = docker_volume.arb_volume[each.key].name
  }
  healthcheck {
    test         = ["CMD-SHELL", "mongosh --port ${var.arbiter_port} --eval 'db.runCommand({ ping: 1 })'"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  wait       = true
  restart    = "no"
  depends_on = [null_resource.init_keyfile]

  lifecycle {
    replace_triggered_by = [docker_image.psmdb]
  }
}
