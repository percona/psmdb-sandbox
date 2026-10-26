resource "google_compute_instance" "ca" {
  count        = var.enable_ca && var.ca_placement == "dedicated" ? 1 : 0
  name         = local.ca_host
  machine_type = var.ca_type
  zone         = local.zones[0]
  labels       = var.labels

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

  metadata_startup_script = <<-EOT
    #!/bin/bash
    hostnamectl set-hostname "${local.ca_host}"
    echo "127.0.0.1 $(hostname) localhost" > /etc/hosts
  EOT
}
