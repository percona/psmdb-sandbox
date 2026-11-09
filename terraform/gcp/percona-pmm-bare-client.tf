variable "pmm_bare_clients" {
  description = "Hosts with only a PMM client, using the environment machine image and replica set machine type."
  type = map(object({
    pmm_client_repo    = optional(string, "")
    pmm_client_version = optional(string, "")
  }))
  default = {}

  validation {
    condition     = length(var.pmm_bare_clients) == 0 || var.enable_pmm
    error_message = "PMM clients without MongoDB need enable_pmm = true."
  }
  validation {
    condition     = !contains(keys(var.pmm_bare_clients), var.default_pmm_host)
    error_message = "A PMM client cannot have the same name as the PMM server."
  }
}

resource "google_compute_instance" "pmm_bare_client" {
  for_each     = var.pmm_bare_clients
  name         = "${var.prefix}-${each.key}"
  machine_type = var.replsetsvr_type
  zone         = local.zones[0]
  labels       = var.labels
  tags         = ["${var.prefix}-pmm-clients"]

  boot_disk {
    initialize_params { image = var.image }
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
  metadata_startup_script = <<-EOT
    #!/bin/bash
    hostnamectl set-hostname "${var.prefix}-${each.key}"
  EOT
}

resource "google_compute_firewall" "pmm_bare_client_ssh" {
  count         = length(var.pmm_bare_clients) > 0 ? 1 : 0
  name          = "${var.prefix}-pmm-clients-ssh"
  network       = google_compute_network.vpc-network.name
  direction     = "INGRESS"
  source_ranges = [var.source_ranges]
  target_tags   = ["${var.prefix}-pmm-clients"]
  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
}

locals {
  pmm_bare_client_ips = { for client in google_compute_instance.pmm_bare_client : client.name => client.network_interface[0].access_config[0].nat_ip }
  pmm_bare_client_hosts = {
    for key, client in google_compute_instance.pmm_bare_client : client.name => {
      ip                 = client.network_interface[0].access_config[0].nat_ip
      pmm_client_repo    = lookup(local.pmm_client_inventory_repos, var.pmm_bare_clients[key].pmm_client_repo, var.pmm_bare_clients[key].pmm_client_repo)
      pmm_client_version = var.pmm_bare_clients[key].pmm_client_version
    }
  }
}

resource "local_file" "AnsibleInventoryPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0
  content = templatefile("${path.module}/../modules/pmm_clients/inventory.tmpl", {
    hostname_pmm         = local.pmm_host
    ip_pmm               = google_compute_instance.pmm[0].network_interface[0].access_config[0].nat_ip
    pmm_bare_clients     = local.pmm_bare_client_hosts
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    pmm_image            = var.pmm_image
    pmm_client_version   = var.pmm_client_version
    pmm_client_repo      = lookup(local.pmm_client_inventory_repos, var.pmm_client_repo, var.pmm_client_repo)
  })
  filename = "${var.prefix}_inventory_pmm_clients"
}

resource "local_file" "SSHConfigPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0
  content = templatefile("${path.module}/../modules/pmm_clients/ssh_config.tmpl", {
    include_pmm          = length(var.clusters) + length(var.replsets) == 0
    hostname_pmm         = local.pmm_host
    public_ip_pmm        = google_compute_instance.pmm[0].network_interface[0].access_config[0].nat_ip
    pmm_bare_clients     = local.pmm_bare_client_ips
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    ssh_gateway_name     = var.ssh_gateway_name
    enable_ssh_gateway   = var.enable_ssh_gateway
    port_to_forward      = var.port_to_forward
    pmm_port             = var.pmm_port
  })
  filename = "${var.prefix}_ssh_config_pmm_clients"
}
