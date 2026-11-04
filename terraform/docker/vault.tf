variable "vault_image" {
  type    = string
  default = "hashicorp/vault:1.21.4"
}

variable "vault_controller_dir" {
  type        = string
  default     = ""
  description = "Private controller recovery directory; defaults to .vault/<prefix>."
}

module "vault_mode_guard" {
  source         = "../modules/vault_mode_guard"
  controller_dir = var.vault_controller_dir != "" ? abspath(var.vault_controller_dir) : abspath("${path.module}/.vault/${var.prefix != "" ? var.prefix : "default"}")
  topologies     = merge({ for n in keys(var.clusters) : "cluster-${n}" => var.vault_encryption }, { for n in keys(var.replsets) : "replset-${n}" => var.vault_encryption })
  enabled        = var.vault_encryption
}

locals {
  vault_topologies = var.vault_encryption ? toset(concat(
    [for name in keys(var.clusters) : "cluster-${name}"],
    [for name in keys(var.replsets) : "replset-${name}"]
  )) : toset([])
  vault_enabled = length(local.vault_topologies) > 0
}

resource "terraform_data" "vault_validation" {
  input = local.vault_topologies
  lifecycle {
    precondition {
      condition = alltrue(concat(
        [for t in values(var.clusters) : !var.vault_encryption || can(regex("(^|/)percona/percona-server-mongodb[:@]", t.psmdb_image))],
        [for t in values(var.replsets) : !var.vault_encryption || can(regex("(^|/)percona/percona-server-mongodb[:@]", t.psmdb_image))]
      ))
      error_message = "Vault encryption requires a percona/percona-server-mongodb image."
    }
    precondition {
      condition = alltrue(concat(
        [for t in values(var.clusters) : !var.vault_encryption || t.network_name == var.network_name],
        [for t in values(var.replsets) : !var.vault_encryption || t.network_name == var.network_name]
      ))
      error_message = "Encrypted topologies must use the environment network_name."
    }
  }
}

module "vault" {
  environment_id = var.prefix
  count          = local.vault_enabled ? 1 : 0
  source         = "./modules/vault_server"
  name           = "${local.name_prefix}vault"
  image          = var.vault_image
  network_name   = docker_network.mongo_network.name
  topologies     = local.vault_topologies
  controller_dir = var.vault_controller_dir != "" ? abspath(var.vault_controller_dir) : abspath("${path.module}/.vault/${var.prefix != "" ? var.prefix : "default"}")
  depends_on     = [terraform_data.vault_validation]
}

module "vault_recovery" {
  count          = local.vault_enabled ? 1 : 0
  source         = "../modules/vault_recovery"
  controller_dir = var.vault_controller_dir != "" ? var.vault_controller_dir : "${path.module}/.vault/${var.prefix != "" ? var.prefix : "default"}"
}
