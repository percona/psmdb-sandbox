locals {
  pmm_bare_client_ips = { for client in chaos_instance.pmm_bare_client : client.name => client.ip_address }
  pmm_bare_client_hosts = {
    for key, client in chaos_instance.pmm_bare_client : client.name => {
      ip                 = client.ip_address
      pmm_client_repo    = lookup(local.pmm_client_inventory_repos, var.pmm_bare_clients[key].pmm_client_repo, var.pmm_bare_clients[key].pmm_client_repo)
      pmm_client_version = var.pmm_bare_clients[key].pmm_client_version
    }
  }

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
    ip_pcsm              = chaos_instance.pcsm[0].ip_address
    source               = local.pcsm_source
    target               = local.pcsm_target
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    pcsm_version         = var.pcsm_version
    hostname_pmm         = var.enable_pmm ? local.pmm_host : ""
    ip_pmm               = var.enable_pmm ? chaos_instance.pmm[0].ip_address : ""
  })

  filename = "${var.prefix}_inventory_pcsm"
}

resource "local_file" "AnsibleInventoryCluster" {
  for_each = module.mongodb_clusters

  content = templatefile("cluster_inventory.tmpl",
    {
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
      cluster              = each.value.cluster
      env_tag              = each.value.env_tag
      enable_pmm           = var.enable_pmm
      use_tls              = var.clusters[each.key].use_tls
      vault_inventory      = "${local.vault_inventory}\n[all:vars]\nvault_encryption=${var.vault_encryption}\nvault_topology=cluster-${each.key}\n"
      enable_pmm_agent     = var.enable_pmm && var.clusters[each.key].enable_pmm
      pmm_image            = var.pmm_image
      enable_extensions    = var.enable_pmm && var.enable_extensions
      extensions_image     = var.extensions_image
      enable_pbm           = var.clusters[each.key].enable_pbm
      enable_mongot        = var.clusters[each.key].enable_mongot
      mongot_source        = var.clusters[each.key].mongot_source
      mongot_repo          = var.clusters[each.key].mongot_repo != "" ? var.clusters[each.key].mongot_repo : var.mongot_repo
      mongot_version       = var.clusters[each.key].mongot_version
      enable_audit         = each.value.enable_audit
      audit_filter         = each.value.audit_filter
      enable_ldap          = var.clusters[each.key].ldap_server != ""
      ldap_hostname        = var.clusters[each.key].ldap_server != "" ? local.ldap_hosts[var.clusters[each.key].ldap_server] : ""
      ldap_ip              = var.clusters[each.key].ldap_server != "" ? chaos_instance.ldap[var.clusters[each.key].ldap_server].ip_address : ""
      ldap_endpoint        = var.clusters[each.key].ldap_server != "" ? chaos_instance.ldap[var.clusters[each.key].ldap_server].ip_address : ""
      ldap_domain          = var.clusters[each.key].ldap_server != "" ? var.ldap_servers[var.clusters[each.key].ldap_server].domain : ""
      ldap_org             = var.clusters[each.key].ldap_server != "" ? var.ldap_servers[var.clusters[each.key].ldap_server].organization : ""
      ldap_base_dn         = var.clusters[each.key].ldap_server != "" ? "dc=${replace(var.ldap_servers[var.clusters[each.key].ldap_server].domain, ".", ",dc=")}" : ""
      ldap_users_dn        = var.clusters[each.key].ldap_server != "" ? "ou=people,dc=${replace(var.ldap_servers[var.clusters[each.key].ldap_server].domain, ".", ",dc=")}" : ""
      ldap_admin_dn        = var.clusters[each.key].ldap_server != "" ? "cn=admin,dc=${replace(var.ldap_servers[var.clusters[each.key].ldap_server].domain, ".", ",dc=")}" : ""
      ldap_admin_password  = var.clusters[each.key].ldap_server != "" ? var.ldap_servers[var.clusters[each.key].ldap_server].admin_password : ""
      ldap_users           = var.clusters[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.clusters[each.key].ldap_server].users) : "[]"
      ldap_mongodb_users   = var.clusters[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.clusters[each.key].ldap_server].mongodb_users) : "[]"

      hostname_pmm         = var.enable_pmm ? local.pmm_host : ""
      ip_pmm               = var.enable_pmm ? chaos_instance.pmm[0].ip_address : ""
      hostname_ca          = var.enable_ca ? (var.ca_placement == "dedicated" ? local.ca_host : local.pmm_host) : ""
      ip_ca                = var.enable_ca ? (var.ca_placement == "dedicated" ? try(chaos_instance.ca[0].ip_address, "") : try(chaos_instance.pmm[0].ip_address, "")) : ""
      hostname_plgm        = var.enable_plgm ? local.plgm_host : ""
      ip_plgm              = var.enable_plgm ? chaos_instance.plgm[0].ip_address : ""
      hostname_pcsm        = var.enable_pcsm ? local.pcsm_host : ""
      ip_pcsm              = var.enable_pcsm ? chaos_instance.pcsm[0].ip_address : ""
      pcsm_version         = var.pcsm_version
      use_backup_storage   = local.use_backup_storage
      storage_provider     = local.backup_storage_provider
      bucket               = local.backup_bucket
      region               = local.backup_region
      endpointUrl          = local.backup_endpoint
      minio_hostname       = local.seaweedfs_host
      minio_ip             = var.enable_seaweedfs ? chaos_instance.seaweedfs[0].ip_address : ""
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
    }
  )

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
    hostname_pmm         = var.enable_pmm ? local.pmm_host : ""
    public_ip_pmm        = var.enable_pmm ? chaos_instance.pmm[0].ip_address : ""
    hostname_plgm        = var.enable_plgm ? local.plgm_host : ""
    public_ip_plgm       = var.enable_plgm ? chaos_instance.plgm[0].ip_address : ""
    hostname_pcsm        = var.enable_pcsm ? local.pcsm_host : ""
    public_ip_pcsm       = var.enable_pcsm ? chaos_instance.pcsm[0].ip_address : ""
    pmm_port             = var.pmm_port
  })

  filename = "${var.prefix}_ssh_config_${each.key}"
}

resource "local_file" "AnsibleInventoryRS" {
  for_each = module.mongodb_replsets

  content = templatefile("replset_inventory.tmpl",
    {
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
      enable_extensions    = var.enable_pmm && var.enable_extensions
      extensions_image     = var.extensions_image
      enable_pbm           = var.replsets[each.key].enable_pbm
      enable_mongot        = var.replsets[each.key].enable_mongot
      mongot_source        = var.replsets[each.key].mongot_source
      mongot_repo          = var.replsets[each.key].mongot_repo != "" ? var.replsets[each.key].mongot_repo : var.mongot_repo
      mongot_version       = var.replsets[each.key].mongot_version
      enable_audit         = each.value.enable_audit
      audit_filter         = each.value.audit_filter
      enable_ldap          = var.replsets[each.key].ldap_server != ""
      ldap_hostname        = var.replsets[each.key].ldap_server != "" ? local.ldap_hosts[var.replsets[each.key].ldap_server] : ""
      ldap_ip              = var.replsets[each.key].ldap_server != "" ? chaos_instance.ldap[var.replsets[each.key].ldap_server].ip_address : ""
      ldap_endpoint        = var.replsets[each.key].ldap_server != "" ? chaos_instance.ldap[var.replsets[each.key].ldap_server].ip_address : ""
      ldap_domain          = var.replsets[each.key].ldap_server != "" ? var.ldap_servers[var.replsets[each.key].ldap_server].domain : ""
      ldap_org             = var.replsets[each.key].ldap_server != "" ? var.ldap_servers[var.replsets[each.key].ldap_server].organization : ""
      ldap_base_dn         = var.replsets[each.key].ldap_server != "" ? "dc=${replace(var.ldap_servers[var.replsets[each.key].ldap_server].domain, ".", ",dc=")}" : ""
      ldap_users_dn        = var.replsets[each.key].ldap_server != "" ? "ou=people,dc=${replace(var.ldap_servers[var.replsets[each.key].ldap_server].domain, ".", ",dc=")}" : ""
      ldap_admin_dn        = var.replsets[each.key].ldap_server != "" ? "cn=admin,dc=${replace(var.ldap_servers[var.replsets[each.key].ldap_server].domain, ".", ",dc=")}" : ""
      ldap_admin_password  = var.replsets[each.key].ldap_server != "" ? var.ldap_servers[var.replsets[each.key].ldap_server].admin_password : ""
      ldap_users           = var.replsets[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.replsets[each.key].ldap_server].users) : "[]"
      ldap_mongodb_users   = var.replsets[each.key].ldap_server != "" ? jsonencode(var.ldap_servers[var.replsets[each.key].ldap_server].mongodb_users) : "[]"
      hostname_pmm         = var.enable_pmm ? local.pmm_host : ""
      ip_pmm               = var.enable_pmm ? chaos_instance.pmm[0].ip_address : ""
      hostname_ca          = var.enable_ca ? (var.ca_placement == "dedicated" ? local.ca_host : local.pmm_host) : ""
      ip_ca                = var.enable_ca ? (var.ca_placement == "dedicated" ? try(chaos_instance.ca[0].ip_address, "") : try(chaos_instance.pmm[0].ip_address, "")) : ""
      hostname_plgm        = var.enable_plgm ? local.plgm_host : ""
      ip_plgm              = var.enable_plgm ? chaos_instance.plgm[0].ip_address : ""
      pcsm_version         = var.pcsm_version
      use_backup_storage   = local.use_backup_storage
      storage_provider     = local.backup_storage_provider
      bucket               = local.backup_bucket
      region               = local.backup_region
      endpointUrl          = local.backup_endpoint
      minio_hostname       = local.seaweedfs_host
      minio_ip             = var.enable_seaweedfs ? chaos_instance.seaweedfs[0].ip_address : ""
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
    }
  )

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
    ssh_gateway_name       = var.ssh_gateway_name
    enable_ssh_gateway     = var.enable_ssh_gateway
    port_to_forward        = var.port_to_forward
    plgm_local_port        = var.plgm_local_port
    hostname_pmm           = var.enable_pmm ? local.pmm_host : ""
    public_ip_pmm          = var.enable_pmm ? chaos_instance.pmm[0].ip_address : ""
    hostname_plgm          = var.enable_plgm ? local.plgm_host : ""
    public_ip_plgm         = var.enable_plgm ? chaos_instance.plgm[0].ip_address : ""
    hostname_pcsm          = var.enable_pcsm ? local.pcsm_host : ""
    public_ip_pcsm         = var.enable_pcsm ? chaos_instance.pcsm[0].ip_address : ""
    pmm_port               = var.pmm_port
  })

  filename = "${var.prefix}_ssh_config_${each.key}"
}

# The PMM clients without MongoDB are configured once per environment, from their own inventory.
resource "local_file" "AnsibleInventoryPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0

  content = templatefile("pmm_clients_inventory.tmpl", {
    hostname_pmm         = local.pmm_host
    ip_pmm               = chaos_instance.pmm[0].ip_address
    pmm_bare_clients     = local.pmm_bare_client_hosts
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    pmm_image            = var.pmm_image
    enable_extensions    = var.enable_extensions
    extensions_image     = var.extensions_image
    pmm_client_version   = var.pmm_client_version
    pmm_client_repo      = lookup(local.pmm_client_inventory_repos, var.pmm_client_repo, var.pmm_client_repo)
  })

  filename = "${var.prefix}_inventory_pmm_clients"
}

resource "local_file" "SSHConfigPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0

  content = templatefile("pmm_clients_ssh_config.tmpl", {
    include_pmm          = length(var.clusters) + length(var.replsets) == 0
    hostname_pmm         = local.pmm_host
    public_ip_pmm        = chaos_instance.pmm[0].ip_address
    pmm_bare_clients     = local.pmm_bare_client_ips
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    ssh_gateway_name     = var.ssh_gateway_name
    enable_ssh_gateway   = var.enable_ssh_gateway
    port_to_forward      = var.port_to_forward
    plgm_local_port      = var.plgm_local_port
    pmm_port             = var.pmm_port
  })

  filename = "${var.prefix}_ssh_config_pmm_clients"
}
