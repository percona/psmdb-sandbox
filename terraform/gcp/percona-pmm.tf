resource "google_compute_disk" "pmm_disk" {
  count  = var.enable_pmm ? 1 : 0
  name   = "${local.pmm_host}-data"
  type   = var.pmm_disk_type
  size   = var.pmm_volume_size
  zone   = local.zones[0]
  labels = var.labels
}

resource "google_compute_instance" "pmm" {
  count        = var.enable_pmm ? 1 : 0
  name         = local.pmm_host
  machine_type = var.pmm_type
  zone         = local.zones[0]
  labels       = var.labels
  tags         = ["${local.pmm_host}"]
  boot_disk {
    initialize_params {
      image = var.image
    }
  }
  attached_disk {
    source      = google_compute_disk.pmm_disk[0].name
    device_name = "data"
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
  metadata_startup_script = <<EOT
    #!/bin/bash
    # Set the hostname
    hostnamectl set-hostname "${local.pmm_host}"

    # Update /etc/hosts to reflect the hostname change
    echo "127.0.0.1 $(hostname) localhost" > /etc/hosts  

    DEVICE_LINK=/dev/disk/by-id/google-data
    for i in $(seq 1 60); do [ -b "$DEVICE_LINK" ] && break; sleep 2; done
    DEVICE=$(readlink -f "$DEVICE_LINK")

    # Format only on first boot; the startup script runs on every boot
    blkid "$DEVICE" >/dev/null 2>&1 || mkfs.xfs "$DEVICE"

    mkdir -p /var/lib/docker

    UUID=$(blkid -s UUID -o value "$DEVICE")
    grep -q "UUID=$UUID" /etc/fstab || echo "UUID=$UUID /var/lib/docker xfs defaults,noatime,nofail 0 2" >> /etc/fstab
    mountpoint -q /var/lib/docker || mount /var/lib/docker
  EOT
}

resource "google_compute_firewall" "percona-pmm-firewall" {
  count         = var.enable_pmm ? 1 : 0
  name          = "${local.pmm_host}-firewall"
  network       = google_compute_network.vpc-network.name
  direction     = "INGRESS"
  source_ranges = ["${var.subnet_cidr}"]
  target_tags   = ["${local.pmm_host}"]
  allow {
    protocol = "tcp"
    ports    = [var.pmm_port]
  }
}
