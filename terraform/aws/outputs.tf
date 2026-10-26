locals {
  pmm_client_inventory_repos = {
    release      = "pmm3-client"
    testing      = "pmm3-client testing"
    experimental = "pmm3-client experimental"
  }

  pcsm_source = var.enable_pcsm ? (var.pcsm_source_kind == "cluster" ? {
    hostname   = module.mongodb_clusters[var.pcsm_source_name].hostname_mongos[0]
    ip         = module.mongodb_clusters[var.pcsm_source_name].ip_mongos[0]
    mongo_port = 27017
    use_tls    = var.clusters[var.pcsm_source_name].use_tls
    } : {
    hostname   = module.mongodb_replsets[var.pcsm_source_name].hostname_replsets[0]
    ip         = module.mongodb_replsets[var.pcsm_source_name].ip_replsets[0]
    mongo_port = 27017
    use_tls    = var.replsets[var.pcsm_source_name].use_tls
  }) : { hostname = "", ip = "", mongo_port = 27017, use_tls = false }

  pcsm_target = var.enable_pcsm ? (var.pcsm_target_kind == "cluster" ? {
    hostname   = module.mongodb_clusters[var.pcsm_target_name].hostname_mongos[0]
    ip         = module.mongodb_clusters[var.pcsm_target_name].ip_mongos[0]
    mongo_port = 27017
    use_tls    = var.clusters[var.pcsm_target_name].use_tls
    } : {
    hostname   = module.mongodb_replsets[var.pcsm_target_name].hostname_replsets[0]
    ip         = module.mongodb_replsets[var.pcsm_target_name].ip_replsets[0]
    mongo_port = 27017
    use_tls    = var.replsets[var.pcsm_target_name].use_tls
  }) : { hostname = "", ip = "", mongo_port = 27017, use_tls = false }
}

resource "local_file" "AnsibleInventoryPCSM" {
  count = var.enable_pcsm ? 1 : 0

  content = templatefile("${path.module}/pcsm_inventory.tmpl", {
    hostname_pcsm        = local.pcsm_host
    ip_pcsm              = aws_instance.pcsm[0].public_ip
    source               = local.pcsm_source
    target               = local.pcsm_target
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    pcsm_version         = var.pcsm_version
    hostname_pmm         = var.enable_pmm ? local.pmm_host : ""
    ip_pmm               = var.enable_pmm ? aws_instance.pmm[0].public_ip : ""
  })

  filename = "${var.prefix}_inventory_pcsm"
}

resource "local_file" "AnsibleInventoryCluster" {
  for_each = module.mongodb_clusters

  content = templatefile("cluster_inventory.tmpl", {
    ansible_group_shards = each.value.ansible_group_shards
    ansible_group_index  = each.value.ansible_group_index
    hostname_shards      = each.value.hostname_shards
    ip_shards            = each.value.ip_shards

    ansible_group_cfg = each.value.ansible_group_cfg
    hostname_cfg      = each.value.hostname_cfg
    ip_cfg            = each.value.ip_cfg

    ansible_group_mongos = each.value.ansible_group_mongos
    hostname_mongos      = each.value.hostname_mongos
    ip_mongos            = each.value.ip_mongos

    ansible_group_arbiters = each.value.ansible_group_arbiters
    hostname_arbiters      = each.value.hostname_arbiters
    ip_arbiters            = each.value.ip_arbiters

    number_of_shards = each.value.number_of_shards

    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    hostname_pmm         = var.enable_pmm ? aws_instance.pmm[0].tags["Name"] : ""
    ip_pmm               = var.enable_pmm ? aws_instance.pmm[0].public_ip : ""
    hostname_ca          = var.enable_ca ? (var.ca_placement == "dedicated" ? local.ca_host : local.pmm_host) : ""
    ip_ca                = var.enable_ca ? (var.ca_placement == "dedicated" ? try(aws_instance.ca[0].public_ip, "") : try(aws_instance.pmm[0].public_ip, "")) : ""
    hostname_plgm        = var.enable_plgm ? aws_instance.plgm[0].tags["Name"] : ""
    ip_plgm              = var.enable_plgm ? aws_instance.plgm[0].public_ip : ""
    hostname_pcsm        = var.enable_pcsm ? local.pcsm_host : ""
    ip_pcsm              = var.enable_pcsm ? aws_instance.pcsm[0].public_ip : ""
    pcsm_version         = var.pcsm_version
    pcsm_is_source       = var.enable_pcsm && var.pcsm_source_kind == "cluster" && var.pcsm_source_name == each.key
    pcsm_is_target       = var.enable_pcsm && var.pcsm_target_kind == "cluster" && var.pcsm_target_name == each.key
    bucket               = local.backup_bucket
    region               = local.backup_region
    endpointUrl          = local.backup_endpoint
    use_backup_storage   = local.use_backup_storage
    cluster              = each.value.cluster
    env_tag              = each.value.env_tag
    enable_pmm           = var.enable_pmm
    use_tls              = var.clusters[each.key].use_tls
    vault_inventory      = "${local.vault_inventory}\n[all:vars]\nvault_encryption=${var.vault_encryption}\nvault_topology=cluster-${each.key}\n"
    enable_pmm_agent     = var.enable_pmm && var.clusters[each.key].enable_pmm
    pmm_image            = var.pmm_image
    enable_pbm           = var.clusters[each.key].enable_pbm
    enable_mongot        = var.clusters[each.key].enable_mongot
    mongot_source        = var.clusters[each.key].mongot_source
    mongot_repo          = var.clusters[each.key].mongot_repo != "" ? var.clusters[each.key].mongot_repo : var.mongot_repo
    mongot_version       = var.clusters[each.key].mongot_version
    enable_audit         = each.value.enable_audit
    audit_filter         = each.value.audit_filter
    enable_ldap          = var.clusters[each.key].ldap_server != ""
    ldap_hostname        = var.clusters[each.key].ldap_server != "" ? local.ldap_hosts[var.clusters[each.key].ldap_server] : ""
    ldap_ip              = var.clusters[each.key].ldap_server != "" ? aws_instance.ldap[var.clusters[each.key].ldap_server].public_ip : ""
    ldap_endpoint        = var.clusters[each.key].ldap_server != "" ? aws_instance.ldap[var.clusters[each.key].ldap_server].private_ip : ""
    ldap_domain          = var.clusters[each.key].ldap_server != "" ? var.ldap_servers[var.clusters[each.key].ldap_server].domain : ""
    ldap_org             = var.clusters[each.key].ldap_server != "" ? var.ldap_servers[var.clusters[each.key].ldap_server].organization : ""
    ldap_base_dn         = var.clusters[each.key].ldap_server != "" ? "dc=${replace(var.ldap_servers[var.clusters[each.key].ldap_server].domain, ".", ",dc=")}" : ""
    ldap_users_dn        = var.clusters[each.key].ldap_server != "" ? "ou=people,dc=${replace(var.ldap_servers[var.clusters[each.key].ldap_server].domain, ".", ",dc=")}" : ""
    ldap_admin_dn        = var.clusters[each.key].ldap_server != "" ? "cn=admin,dc=${replace(var.ldap_servers[var.clusters[each.key].ldap_server].domain, ".", ",dc=")}" : ""
    ldap_admin_password  = var.clusters[each.key].ldap_server != "" ? var.ldap_servers[var.clusters[each.key].ldap_server].admin_password : ""
    ldap_users           = var.clusters[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.clusters[each.key].ldap_server].users) : "[]"
    ldap_mongodb_users   = var.clusters[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.clusters[each.key].ldap_server].mongodb_users) : "[]"

    access_key           = local.backup_access_key
    secret_access_key    = local.backup_secret_key
    mongodb_distribution = var.clusters[each.key].mongodb_distribution != "" ? var.clusters[each.key].mongodb_distribution : var.mongodb_distribution
    mongo_release        = var.clusters[each.key].mongo_release != "" ? var.clusters[each.key].mongo_release : var.mongo_release
    mongo_version        = var.clusters[each.key].mongo_version != "" ? var.clusters[each.key].mongo_version : var.mongo_version
    mongo_repo           = var.clusters[each.key].mongo_repo != "" ? var.clusters[each.key].mongo_repo : var.mongo_repo
    pbm_version          = var.clusters[each.key].pbm_version != "" ? var.clusters[each.key].pbm_version : var.pbm_version
    pbm_repo             = var.clusters[each.key].pbm_repo != "" ? var.clusters[each.key].pbm_repo : var.pbm_repo
    pmm_client_version   = var.clusters[each.key].pmm_client_version != "" ? var.clusters[each.key].pmm_client_version : var.pmm_client_version
    pmm_client_repo      = lookup(local.pmm_client_inventory_repos, var.clusters[each.key].pmm_client_repo != "" ? var.clusters[each.key].pmm_client_repo : var.pmm_client_repo, var.clusters[each.key].pmm_client_repo != "" ? var.clusters[each.key].pmm_client_repo : var.pmm_client_repo)
  })

  filename = "${var.prefix}_inventory_${each.key}"
}


resource "local_file" "SSHConfigCluster" {
  for_each = module.mongodb_clusters

  content = templatefile("cluster_ssh_config.tmpl", {
    ansible_group_shards = each.value.ansible_group_shards
    hostname_shards      = each.value.hostname_shards
    ip_shards            = each.value.ip_shards
    ansible_group_cfg    = each.value.ansible_group_cfg
    hostname_cfg         = each.value.hostname_cfg
    ip_cfg               = each.value.ip_cfg
    ansible_group_mongos = each.value.ansible_group_mongos
    hostname_mongos      = each.value.hostname_mongos
    ip_mongos            = each.value.ip_mongos
    hostname_arbiters    = each.value.hostname_arbiters
    ip_arbiters          = each.value.ip_arbiters
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    enable_ssh_gateway   = var.enable_ssh_gateway
    port_to_forward      = var.port_to_forward
    plgm_local_port      = var.plgm_local_port
    ssh_gateway_name     = var.ssh_gateway_name
    hostname_pmm         = var.enable_pmm ? aws_instance.pmm[0].tags["Name"] : ""
    public_ip_pmm        = var.enable_pmm ? aws_instance.pmm[0].public_ip : ""
    hostname_plgm        = var.enable_plgm ? aws_instance.plgm[0].tags["Name"] : ""
    public_ip_plgm       = var.enable_plgm ? aws_instance.plgm[0].public_ip : ""
    hostname_pcsm        = var.enable_pcsm ? local.pcsm_host : ""
    public_ip_pcsm       = var.enable_pcsm ? aws_instance.pcsm[0].public_ip : ""
    pmm_port             = var.pmm_port
  })

  filename = "${var.prefix}_ssh_config_${each.key}"
}

resource "local_file" "AnsibleInventoryRS" {
  for_each = module.mongodb_replsets

  content = templatefile("replset_inventory.tmpl", {
    ansible_group_replsets = each.value.ansible_group_replsets
    hostname_replsets      = each.value.hostname_replsets
    ip_replsets            = each.value.ip_replsets

    ansible_group_arbiters = each.value.ansible_group_arbiters
    hostname_arbiters      = each.value.hostname_arbiters
    ip_arbiters            = each.value.ip_arbiters

    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    rs_name              = each.value.rs_name
    env_tag              = each.value.env_tag
    enable_pmm           = var.enable_pmm
    use_tls              = var.replsets[each.key].use_tls
    vault_inventory      = "${local.vault_inventory}\n[all:vars]\nvault_encryption=${var.vault_encryption}\nvault_topology=replset-${each.key}\n"
    enable_pmm_agent     = var.enable_pmm && var.replsets[each.key].enable_pmm
    pmm_image            = var.pmm_image
    enable_pbm           = var.replsets[each.key].enable_pbm
    enable_mongot        = var.replsets[each.key].enable_mongot
    mongot_source        = var.replsets[each.key].mongot_source
    mongot_repo          = var.replsets[each.key].mongot_repo != "" ? var.replsets[each.key].mongot_repo : var.mongot_repo
    mongot_version       = var.replsets[each.key].mongot_version
    enable_audit         = each.value.enable_audit
    audit_filter         = each.value.audit_filter
    enable_ldap          = var.replsets[each.key].ldap_server != ""
    ldap_hostname        = var.replsets[each.key].ldap_server != "" ? local.ldap_hosts[var.replsets[each.key].ldap_server] : ""
    ldap_ip              = var.replsets[each.key].ldap_server != "" ? aws_instance.ldap[var.replsets[each.key].ldap_server].public_ip : ""
    ldap_endpoint        = var.replsets[each.key].ldap_server != "" ? aws_instance.ldap[var.replsets[each.key].ldap_server].private_ip : ""
    ldap_domain          = var.replsets[each.key].ldap_server != "" ? var.ldap_servers[var.replsets[each.key].ldap_server].domain : ""
    ldap_org             = var.replsets[each.key].ldap_server != "" ? var.ldap_servers[var.replsets[each.key].ldap_server].organization : ""
    ldap_base_dn         = var.replsets[each.key].ldap_server != "" ? "dc=${replace(var.ldap_servers[var.replsets[each.key].ldap_server].domain, ".", ",dc=")}" : ""
    ldap_users_dn        = var.replsets[each.key].ldap_server != "" ? "ou=people,dc=${replace(var.ldap_servers[var.replsets[each.key].ldap_server].domain, ".", ",dc=")}" : ""
    ldap_admin_dn        = var.replsets[each.key].ldap_server != "" ? "cn=admin,dc=${replace(var.ldap_servers[var.replsets[each.key].ldap_server].domain, ".", ",dc=")}" : ""
    ldap_admin_password  = var.replsets[each.key].ldap_server != "" ? var.ldap_servers[var.replsets[each.key].ldap_server].admin_password : ""
    ldap_users           = var.replsets[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.replsets[each.key].ldap_server].users) : "[]"
    ldap_mongodb_users   = var.replsets[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.replsets[each.key].ldap_server].mongodb_users) : "[]"

    region             = local.backup_region
    hostname_pmm       = var.enable_pmm ? aws_instance.pmm[0].tags["Name"] : ""
    ip_pmm             = var.enable_pmm ? aws_instance.pmm[0].public_ip : ""
    hostname_ca        = var.enable_ca ? (var.ca_placement == "dedicated" ? local.ca_host : local.pmm_host) : ""
    ip_ca              = var.enable_ca ? (var.ca_placement == "dedicated" ? try(aws_instance.ca[0].public_ip, "") : try(aws_instance.pmm[0].public_ip, "")) : ""
    hostname_plgm      = var.enable_plgm ? aws_instance.plgm[0].tags["Name"] : ""
    ip_plgm            = var.enable_plgm ? aws_instance.plgm[0].public_ip : ""
    hostname_pcsm      = var.enable_pcsm ? local.pcsm_host : ""
    ip_pcsm            = var.enable_pcsm ? aws_instance.pcsm[0].public_ip : ""
    pcsm_version       = var.pcsm_version
    pcsm_is_source     = var.enable_pcsm && var.pcsm_source_kind == "replset" && var.pcsm_source_name == each.key
    pcsm_is_target     = var.enable_pcsm && var.pcsm_target_kind == "replset" && var.pcsm_target_name == each.key
    bucket             = local.backup_bucket
    endpointUrl        = local.backup_endpoint
    use_backup_storage = local.use_backup_storage

    access_key           = local.backup_access_key
    secret_access_key    = local.backup_secret_key
    mongodb_distribution = var.replsets[each.key].mongodb_distribution != "" ? var.replsets[each.key].mongodb_distribution : var.mongodb_distribution
    mongo_release        = var.replsets[each.key].mongo_release != "" ? var.replsets[each.key].mongo_release : var.mongo_release
    mongo_version        = var.replsets[each.key].mongo_version != "" ? var.replsets[each.key].mongo_version : var.mongo_version
    mongo_repo           = var.replsets[each.key].mongo_repo != "" ? var.replsets[each.key].mongo_repo : var.mongo_repo
    pbm_version          = var.replsets[each.key].pbm_version != "" ? var.replsets[each.key].pbm_version : var.pbm_version
    pbm_repo             = var.replsets[each.key].pbm_repo != "" ? var.replsets[each.key].pbm_repo : var.pbm_repo
    pmm_client_version   = var.replsets[each.key].pmm_client_version != "" ? var.replsets[each.key].pmm_client_version : var.pmm_client_version
    pmm_client_repo      = lookup(local.pmm_client_inventory_repos, var.replsets[each.key].pmm_client_repo != "" ? var.replsets[each.key].pmm_client_repo : var.pmm_client_repo, var.replsets[each.key].pmm_client_repo != "" ? var.replsets[each.key].pmm_client_repo : var.pmm_client_repo)
  })

  filename = "${var.prefix}_inventory_${each.key}"
}

resource "local_file" "SSHConfigRS" {
  for_each = module.mongodb_replsets

  content = templatefile("rs_ssh_config.tmpl", {
    ansible_group_replsets = each.value.ansible_group_replsets
    hostname_replsets      = each.value.hostname_replsets
    ip_replsets            = each.value.ip_replsets
    ansible_group_arbiters = each.value.ansible_group_arbiters
    hostname_arbiters      = each.value.hostname_arbiters
    ip_arbiters            = each.value.ip_arbiters
    my_ssh_user            = var.my_ssh_user
    ssh_private_key_path   = var.ssh_private_key_path
    enable_ssh_gateway     = var.enable_ssh_gateway
    port_to_forward        = var.port_to_forward
    plgm_local_port        = var.plgm_local_port
    ssh_gateway_name       = var.ssh_gateway_name
    hostname_pmm           = var.enable_pmm ? aws_instance.pmm[0].tags["Name"] : ""
    public_ip_pmm          = var.enable_pmm ? aws_instance.pmm[0].public_ip : ""
    hostname_plgm          = var.enable_plgm ? aws_instance.plgm[0].tags["Name"] : ""
    public_ip_plgm         = var.enable_plgm ? aws_instance.plgm[0].public_ip : ""
    hostname_pcsm          = var.enable_pcsm ? local.pcsm_host : ""
    public_ip_pcsm         = var.enable_pcsm ? aws_instance.pcsm[0].public_ip : ""
    pmm_port               = var.pmm_port
  })

  filename = "${var.prefix}_ssh_config_${each.key}"
}
