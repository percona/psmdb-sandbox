resource "google_compute_instance" "mongos" {
  for_each                  = local.mongos_members
  name                      = "${var.prefix}-${var.cluster_name}-${var.mongos_tag}0${each.value}"
  machine_type              = var.mongos_type
  min_cpu_platform          = var.min_cpu_platform
  resource_policies         = var.resource_policies
  allow_stopping_for_update = true
  zone                      = var.zones[each.value % length(var.zones)]
  tags                      = ["${var.prefix}-${var.cluster_name}-${var.mongos_tag}"]
  labels = merge(var.labels, {
    ansible-group = "mongos",
    environment   = var.env_tag
  })
  boot_disk {
    initialize_params {
      image = var.image
      size  = var.boot_disk_size
      type  = var.boot_disk_type
    }
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
    hostnamectl set-hostname "${var.prefix}-${var.cluster_name}-${var.mongos_tag}0${each.value}"

    # Update /etc/hosts to reflect the hostname change
    echo "127.0.0.1 $(hostname) localhost" > /etc/hosts    
  EOT
}

resource "google_compute_firewall" "mongodb-mongos-firewall" {
  name          = "${var.prefix}-${var.cluster_name}-${var.mongos_tag}-firewall"
  network       = var.vpc
  direction     = "INGRESS"
  source_ranges = ["${var.subnet_cidr}"]
  target_tags   = ["${var.prefix}-${var.cluster_name}-${var.mongos_tag}"]
  allow {
    protocol = "tcp"
    ports    = [var.mongos_port]
  }
}
