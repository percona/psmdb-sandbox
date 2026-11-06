# Create Docker containers for MongoDB mongos
resource "docker_container" "mongos" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each   = local.mongos_members
  name       = "${var.cluster_name}-${var.mongos_tag}0${each.value}"
  hostname   = "${var.cluster_name}-${var.mongos_tag}0${each.value}"
  domainname = var.domain_name
  image      = docker_image.psmdb.image_id
  env        = local.mongod_env
  command = concat([
    "mongos",
    "--configdb", "${lookup({ for label in docker_container.cfg[local.cfg_member_keys[0]].labels : label.label => label.value }, "replsetName", null)}/${join(",", [for key in local.cfg_member_keys : "${docker_container.cfg[key].name}:${var.configsvr_port}"])}",
    "--bind_ip_all",
    "--port", "${var.mongos_port}",
    "--slowms", "200",
    "--rateLimit", "100",
    "--setParameter", "diagnosticDataCollectionDirectoryPath=/var/log/mongo/mongos.diagnostic.data/"
    ],
    local.mongo_auth_flags,
    var.enable_audit ? [
      "--auditDestination", "file",
      "--auditFormat", "JSON",
      "--auditPath", "/var/log/mongodb-audit.json",
      "--auditFilter", "${var.audit_filter}",
      "--setParameter", "auditAuthorizationSuccess=true"
    ] : [],
    var.enable_mongot ? [
      "--setParameter", "searchIndexManagementHostAndPort=${var.cluster_name}-${var.shardsvr_tag}00svr0-mongot:${var.mongot_port}",
      "--setParameter", "mongotHost=${var.cluster_name}-${var.shardsvr_tag}00svr0-mongot:${var.mongot_port}",
      "--setParameter", "skipAuthenticationToSearchIndexManagementServer=false",
      "--setParameter", "useGrpcForSearch=true",
      "--setParameter", "searchTLSMode=disabled"
    ] : [],
    var.enable_ldap ? [
      "--setParameter", "authenticationMechanisms=PLAIN,SCRAM-SHA-256",
      "--ldapQueryUser", "${var.ldap_bind_dn}",
      "--ldapQueryPassword", "${var.ldap_bind_pw}",
      "--ldapUserToDNMapping", "[{\"match\": \"(.+)\", \"ldapQuery\": \"${var.ldap_user_search_base}??sub?(uid={0})\"}]",
      "--ldapServers", "${var.ldap_servers}",
      "--ldapTransportSecurity", "none"
    ] : []
  )
  ports {
    internal = var.mongos_port
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
  mounts {
    source    = docker_volume.keyfile_volume.name
    target    = var.keyfile_path
    type      = "volume"
    read_only = true
  }
  labels {
    label = "environment"
    value = var.env_tag
  }
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  healthcheck {
    test         = ["CMD-SHELL", "mongosh ${local.mongo_tls_flags} --port ${var.mongos_port} --eval 'db.runCommand({ ping: 1 })'"]
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
