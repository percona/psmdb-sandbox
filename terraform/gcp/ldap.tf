locals {
  ldap_hosts = { for name, server in var.ldap_servers : name => "${var.prefix}-${name}" }
}

resource "google_compute_instance" "ldap" {
  for_each     = var.ldap_servers
  name         = local.ldap_hosts[each.key]
  machine_type = each.value.machine_type
  zone         = local.zones[0]
  labels       = var.labels
  tags         = ["${local.ldap_hosts[each.key]}"]
  boot_disk {
    initialize_params {
      image = var.image
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
}

resource "google_compute_firewall" "ldap" {
  for_each      = var.ldap_servers
  name          = "${local.ldap_hosts[each.key]}-firewall"
  network       = google_compute_network.vpc-network.name
  direction     = "INGRESS"
  source_ranges = [var.subnet_cidr]
  target_tags   = [local.ldap_hosts[each.key]]
  allow {
    protocol = "tcp"
    ports    = ["389", "80"]
  }
}
