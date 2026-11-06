locals {
  replset_members = {
    for member_index in range(var.data_nodes_per_replset) : "svr${member_index}" => {
      member_index = member_index
    }
  }

  replset_member_keys = [for member_index in range(var.data_nodes_per_replset) : "svr${member_index}"]

  arbiter_members = {
    for arbiter_index in range(var.arbiters_per_replset) : "arb${arbiter_index}" => {
      arbiter_index = arbiter_index
    }
  }

  arbiter_member_keys = [for arbiter_index in range(var.arbiters_per_replset) : "arb${arbiter_index}"]
}

resource "docker_volume" "rs_volume" {
  for_each = local.replset_members
  name     = "${var.rs_name}-${var.replset_tag}${each.value.member_index}-data"
}

resource "docker_container" "rs" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each   = local.replset_members
  name       = "${var.rs_name}-${var.replset_tag}${each.value.member_index}${var.domain_name != "" ? ".${var.domain_name}" : ""}"
  hostname   = "${var.rs_name}-${var.replset_tag}${each.value.member_index}"
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
      "--replSet", "${var.rs_name}",
      "--bind_ip_all",
      "--port", "${var.replset_port + each.value.member_index}",
      "--oplogSize", "200",
      "--wiredTigerCacheSizeGB", "0.25",
      "--profile", "2",
      "--slowms", "200",
      "--rateLimit", "100"
    ],
    local.mongo_auth_flags,
    var.vault_encryption ? [
      "--enableEncryption", "--vaultServerName", var.vault_server,
      "--vaultPort", "8200", "--vaultTokenFile", "/etc/mongodb-vault/token",
      "--vaultServerCAFile", "/etc/mongodb-vault/vault.crt",
      "--vaultSecret", "secret/data/${var.vault_topology}/${each.key}"
    ] : [],
    var.enable_audit ? [
      "--auditDestination", "file",
      "--auditFormat", "JSON",
      "--auditPath", "/var/log/mongodb-audit.json",
      "--auditFilter", "${var.audit_filter}",
      "--setParameter", "auditAuthorizationSuccess=true"
    ] : [],
    var.enable_mongot ? [
      "--setParameter", "searchIndexManagementHostAndPort=${var.rs_name}-${var.replset_tag}${each.value.member_index}-mongot:${var.mongot_port}",
      "--setParameter", "mongotHost=${var.rs_name}-${var.replset_tag}${each.value.member_index}-mongot:${var.mongot_port}",
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
  dynamic "mounts" {
    for_each = var.vault_encryption ? [var.vault_credentials] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-vault"
      read_only = true
    }
  }
  ports {
    internal = var.replset_port + each.value.member_index
    external = var.replset_port + each.value.member_index
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
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
    source = docker_volume.rs_volume[each.key].name
  }
  healthcheck {
    test         = ["CMD-SHELL", "mongosh ${local.mongo_tls_flags} --port ${var.replset_port + each.value.member_index} --eval 'db.runCommand({ ping: 1 })'"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  wait       = true
  restart    = "no"
  depends_on = [null_resource.init_keyfile, terraform_data.vault_data_mode, terraform_data.tls_data_mode]

  lifecycle {
    replace_triggered_by = [docker_image.psmdb]
  }
}
