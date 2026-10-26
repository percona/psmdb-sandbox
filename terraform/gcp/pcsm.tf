resource "google_compute_instance" "pcsm" {
  count        = var.enable_pcsm ? 1 : 0
  name         = local.pcsm_host
  machine_type = var.pcsm_type
  zone         = local.zones[0]
  labels       = var.labels
  tags         = [local.pcsm_host]

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
    hostnamectl set-hostname "${local.pcsm_host}"
  EOT
}

resource "google_compute_firewall" "pcsm_ssh" {
  count         = var.enable_pcsm ? 1 : 0
  name          = "${local.pcsm_host}-ssh"
  network       = google_compute_network.vpc-network.name
  direction     = "INGRESS"
  source_ranges = [var.source_ranges]
  target_tags   = [local.pcsm_host]

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
}
