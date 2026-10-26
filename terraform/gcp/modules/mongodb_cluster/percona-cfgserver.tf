resource "google_compute_disk" "cfg_disk" {
  for_each               = local.cfg_members
  name                   = "${var.cluster_name}-${var.configsvr_tag}0${each.value}-data"
  type                   = var.configsvr_disk_type
  provisioned_iops       = local.configsvr_disk_iops
  provisioned_throughput = local.configsvr_disk_throughput
  labels                 = var.labels
  size                   = var.configsvr_volume_size
  zone                   = var.zones[each.value % length(var.zones)]
}

resource "google_compute_instance" "cfg" {
  for_each                  = local.cfg_members
  name                      = "${var.cluster_name}-${var.configsvr_tag}0${each.value}"
  machine_type              = var.configsvr_type
  min_cpu_platform          = var.min_cpu_platform
  resource_policies         = var.resource_policies
  allow_stopping_for_update = true
  zone                      = var.zones[each.value % length(var.zones)]
  tags                      = ["${var.cluster_name}-${var.configsvr_tag}"]
  labels = merge(var.labels, {
    ansible-group = "cfg",
    environment   = var.env_tag
  })
  boot_disk {
    initialize_params {
      image = var.image
      size  = var.boot_disk_size
      type  = var.boot_disk_type
    }
  }
  attached_disk {
    source      = google_compute_disk.cfg_disk[each.key].self_link
    device_name = "data"
  }
  network_interface {
    network    = var.vpc
    subnetwork = var.subnet_name
    nic_type   = var.nic_type
    access_config {}
  }
  dynamic "network_performance_config" {
    for_each = var.tier1_networking ? [1] : []
    content {
      total_egress_bandwidth_tier = "TIER_1"
    }
  }
  metadata = {
    ssh-keys = join("\n", [for user, key_path in var.gce_ssh_users : "${user}:${file(key_path)}"])
  }
  scheduling {
    preemptible         = var.use_spot_instances
    automatic_restart   = var.use_spot_instances ? false : true
    provisioning_model  = var.use_spot_instances ? "SPOT" : "STANDARD"
    on_host_maintenance = local.on_host_maintenance
  }
  metadata_startup_script = <<EOT
    #!/bin/bash
    # Set the hostname
    hostnamectl set-hostname "${var.cluster_name}-${var.configsvr_tag}0${each.value}"

    # Update /etc/hosts to reflect the hostname change
    echo "127.0.0.1 $(hostname) localhost" > /etc/hosts    

    DEVICE_LINK=/dev/disk/by-id/google-data
    for i in $(seq 1 60); do [ -b "$DEVICE_LINK" ] && break; sleep 2; done
    DEVICE=$(readlink -f "$DEVICE_LINK")

    # Format only on first boot; the startup script runs on every boot
    blkid "$DEVICE" >/dev/null 2>&1 || mkfs.xfs "$DEVICE"

    mkdir -p /var/lib/mongo

    UUID=$(blkid -s UUID -o value "$DEVICE")
    grep -q "UUID=$UUID" /etc/fstab || echo "UUID=$UUID /var/lib/mongo xfs defaults,noatime,nofail 0 2" >> /etc/fstab
    mountpoint -q /var/lib/mongo || mount /var/lib/mongo
  EOT
}

resource "google_compute_firewall" "mongodb-cfgsvr-firewall" {
  name          = "${var.cluster_name}-${var.configsvr_tag}-firewall"
  network       = var.vpc
  direction     = "INGRESS"
  source_ranges = ["${var.subnet_cidr}"]
  target_tags   = ["${var.cluster_name}-${var.configsvr_tag}"]
  allow {
    protocol = "tcp"
    ports    = [var.configsvr_port]
  }
}
