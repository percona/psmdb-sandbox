locals {
  shard_members = {
    for member in flatten([
      for shard_index in range(var.shard_count) : [
        for replica_index in range(var.shardsvr_replicas) : {
          key           = "shard${shard_index}svr${replica_index}"
          shard_index   = shard_index
          replica_index = replica_index
        }
      ]
    ]) : member.key => member
  }

  shard_member_keys = flatten([
    for shard_index in range(var.shard_count) : [
      for replica_index in range(var.shardsvr_replicas) : "shard${shard_index}svr${replica_index}"
    ]
  ])

  cfg_members        = { for cfg_index in range(var.configsvr_count) : "cfg${cfg_index}" => cfg_index }
  cfg_member_keys    = [for cfg_index in range(var.configsvr_count) : "cfg${cfg_index}"]
  mongos_members     = { for mongos_index in range(var.mongos_count) : "mongos${mongos_index}" => mongos_index }
  mongos_member_keys = [for mongos_index in range(var.mongos_count) : "mongos${mongos_index}"]

  arbiter_members = {
    for member in flatten([
      for shard_index in range(var.shard_count) : [
        for arbiter_index in range(var.arbiters_per_replset) : {
          key           = "shard${shard_index}arb${arbiter_index}"
          shard_index   = shard_index
          arbiter_index = arbiter_index
        }
      ]
    ]) : member.key => member
  }

  arbiter_member_keys = flatten([
    for shard_index in range(var.shard_count) : [
      for arbiter_index in range(var.arbiters_per_replset) : "shard${shard_index}arb${arbiter_index}"
    ]
  ])
}

resource "docker_volume" "shard_volume" {
  for_each = local.shard_members
  name     = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-data"
}

resource "docker_container" "shard" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each   = local.shard_members
  name       = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}"
  hostname   = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}"
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
    "--replSet", "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}",
    "--bind_ip_all",
    "--port", "${var.shardsvr_port}",
    "--shardsvr",
    "--oplogSize", "200",
    "--wiredTigerCacheSizeGB", "0.25",
    "--keyFile", "${var.keyfile_path}/${var.keyfile_name}",
    "--profile", "2",
    "--slowms", "200",
    "--rateLimit", "100"
    ],
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
      "--setParameter", "searchIndexManagementHostAndPort=${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-mongot:${var.mongot_port}",
      "--setParameter", "mongotHost=${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-mongot:${var.mongot_port}",
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
    for_each = var.vault_encryption ? [var.vault_credentials] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-vault"
      read_only = true
    }
  }
  ports {
    internal = var.shardsvr_port
    ip       = var.bind_to_localhost ? "127.0.0.1" : "0.0.0.0"
  }
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
    source = docker_volume.shard_volume[each.key].name
  }
  healthcheck {
    test         = ["CMD-SHELL", "mongosh --port ${var.shardsvr_port} --eval 'db.runCommand({ ping: 1 })'"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }
  wait       = true
  restart    = "no"
  depends_on = [null_resource.init_keyfile, terraform_data.vault_data_mode]

  lifecycle {
    replace_triggered_by = [docker_image.psmdb]
  }
}
