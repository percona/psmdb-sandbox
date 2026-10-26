variable "vault_type" {
  type    = string
  default = "e2-small"
}

variable "vault_volume_size" {
  type    = number
  default = 20
}

variable "vault_controller_dir" {
  type    = string
  default = ""
}

module "vault_mode_guard" {
  source         = "../modules/vault_mode_guard"
  controller_dir = var.vault_controller_dir != "" ? abspath(var.vault_controller_dir) : abspath("${path.module}/../../ansible/.vault/gcp/${var.prefix}")
  topologies     = merge({ for n in keys(var.clusters) : "cluster-${n}" => var.vault_encryption }, { for n in keys(var.replsets) : "replset-${n}" => var.vault_encryption })
  enabled        = var.vault_encryption
}

module "vault_recovery" {
  count          = local.vault_enabled ? 1 : 0
  source         = "../modules/vault_recovery"
  controller_dir = var.vault_controller_dir != "" ? var.vault_controller_dir : "${path.module}/../../ansible/.vault/gcp/${var.prefix}"
}

locals {
  vault_host    = "${var.prefix}-vault"
  vault_enabled = var.vault_encryption && length(var.clusters) + length(var.replsets) > 0
  vault_topologies = local.vault_enabled ? concat(
    [for name in keys(var.clusters) : "cluster-${name}"],
    [for name in keys(var.replsets) : "replset-${name}"]
  ) : []
  vault_inventory = local.vault_enabled ? "\n[vault]\n${local.vault_host} ansible_host=${google_compute_instance.vault[0].network_interface[0].access_config[0].nat_ip}\n\n[all:vars]\nvault_server=${google_compute_instance.vault[0].network_interface[0].network_ip}\nvault_environment=${var.prefix}\nvault_encryption=true\nvault_topologies_json='${jsonencode(local.vault_topologies)}'\nvault_controller_dir_override=${jsonencode(var.vault_controller_dir != "" ? abspath(var.vault_controller_dir) : abspath("${path.module}/../../ansible/.vault/gcp/${var.prefix}"))}\n" : ""
}

resource "google_compute_instance" "vault" {
  count        = local.vault_enabled ? 1 : 0
  name         = local.vault_host
  machine_type = var.vault_type
  zone         = local.zones[0]
  labels       = var.labels
  boot_disk {
    initialize_params {
      image = var.image
      size  = var.vault_volume_size
    }
  }
  network_interface {
    network    = google_compute_network.vpc-network.id
    subnetwork = google_compute_subnetwork.vpc-subnet.id
    access_config {}
  }
  metadata = {
    ssh-keys = join("\n", [for user, key_path in var.gce_ssh_users : "${user}:${file(key_path)}"])
  }
  scheduling {
    preemptible        = false
    automatic_restart  = true
    provisioning_model = "STANDARD"
  }
  lifecycle {
    precondition {
      condition = alltrue(concat(
        [for t in values(var.clusters) : (t.mongodb_distribution != "" ? t.mongodb_distribution : var.mongodb_distribution) == "psmdb"],
        [for t in values(var.replsets) : (t.mongodb_distribution != "" ? t.mongodb_distribution : var.mongodb_distribution) == "psmdb"]
      ))
      error_message = "Vault encryption is supported only for PSMDB topologies."
    }
  }
}

resource "local_file" "VaultInventory" {
  count    = local.vault_enabled ? 1 : 0
  filename = "${var.prefix}_inventory_vault"
  content  = "${local.vault_inventory}\n[all:vars]\nansible_user=${var.my_ssh_user}\nansible_ssh_private_key_file=${var.ssh_private_key_path}\nansible_ssh_common_args=-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null\n"
}
