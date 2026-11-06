resource "docker_volume" "cfg_volume" {
  for_each = local.cfg_members
  name     = "${var.cluster_name}-${var.configsvr_tag}0${each.value}-data"
}

resource "docker_container" "cfg" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each   = local.cfg_members
  name       = "${var.cluster_name}-${var.configsvr_tag}0${each.value}"
  hostname   = "${var.cluster_name}-${var.configsvr_tag}0${each.value}"
  domainname = var.domain_name
  image      = docker_image.psmdb.image_id
  env        = local.mongod_env
  mounts {
    source    = docker_volume.keyfile_volume.name
    target    = var.keyfile_path
    type      = "volume"
    read_only = true
  }
  command = concat([
    "mongod",
    "--replSet", "${var.cluster_name}-${var.configsvr_tag}",
    "--bind_ip_all",
    "--configsvr",
    "--port", "${var.configsvr_port}",
    "--dbpath", "/data/db",
    "--oplogSize", "200",
    "--wiredTigerCacheSizeGB", "0.25",
    "--profile", "2",
    "--slowms", "200",
    "--rateLimit", "100"
    ],
    local.mongod_auth_flags,
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
    internal = var.configsvr_port
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
  dynamic "mounts" {
    for_each = var.vault_encryption ? [var.vault_credentials] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-vault"
      read_only = true
    }
  }
  labels {
    label = "replsetName"
    value = "${var.cluster_name}-${var.configsvr_tag}"
  }
  labels {
    label = "environment"
    value = var.env_tag
  }
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
    test         = ["CMD-SHELL", "mongosh ${local.mongo_tls_flags} --port ${var.configsvr_port} --eval 'db.runCommand({ ping: 1 })'"]
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
