resource "docker_volume" "arb_volume" {
  for_each = local.arbiter_members
  name     = "${var.rs_name}-${var.arbiter_tag}${each.value.arbiter_index}-data"
}

resource "docker_container" "arbiter" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each   = local.arbiter_members
  name       = "${var.rs_name}-${var.arbiter_tag}${each.value.arbiter_index}${var.domain_name != "" ? ".${var.domain_name}" : ""}"
  hostname   = "${var.rs_name}-${var.arbiter_tag}${each.value.arbiter_index}"
  domainname = var.domain_name
  image      = docker_image.psmdb.image_id
  env        = local.mongod_env
  mounts {
    source    = docker_volume.keyfile_volume.id
    target    = var.keyfile_path
    type      = "volume"
    read_only = true
  }
  command = concat(
    [
      "mongod",
      "--replSet", "${var.rs_name}",
      "--bind_ip_all",
      "--port", "${var.arbiter_base_port + each.value.arbiter_index}"
    ],
    local.mongo_auth_flags,
    var.enable_audit ? [
      "--auditDestination", "file",
      "--auditFormat", "JSON",
      "--auditPath", "/var/log/mongodb-audit.json",
      "--auditFilter", "${var.audit_filter}",
      "--setParameter", "auditAuthorizationSuccess=true"
    ] : []
  )
  ports {
    internal = var.arbiter_base_port + each.value.arbiter_index
    external = var.arbiter_base_port + each.value.arbiter_index
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }
  user = var.uid
  dynamic "mounts" {
    for_each = var.use_tls ? [module.tls_certificate[each.key].volume] : []
    content {
      source    = mounts.value
      target    = "/etc/mongodb-tls"
      type      = "volume"
      read_only = true
    }
  }
  labels {
    label = "replsetName"
    value = var.rs_name
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
    test         = ["CMD-SHELL", "mongosh ${local.mongo_tls_flags} --port ${var.arbiter_base_port + each.value.arbiter_index} --eval 'db.runCommand({ ping: 1 })'"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  wait       = true
  restart    = "no"
  depends_on = [null_resource.init_keyfile, terraform_data.tls_data_mode]

  lifecycle {
    replace_triggered_by = [docker_image.psmdb]
  }
}
