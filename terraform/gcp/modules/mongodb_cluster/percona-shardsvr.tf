locals {
  shard_members = {
    for member in flatten([
      for shard_index in range(var.shard_count) : [
        for replica_index in range(var.shardsvr_replicas) : {
          key           = "shard${shard_index}svr${replica_index}"
          shard_index   = shard_index
          replica_index = replica_index
        }
      ]
    ]) : member.key => member
  }

  shard_member_keys = flatten([
    for shard_index in range(var.shard_count) : [
      for replica_index in range(var.shardsvr_replicas) : "shard${shard_index}svr${replica_index}"
    ]
  ])

  cfg_members        = { for cfg_index in range(var.configsvr_count) : "cfg${cfg_index}" => cfg_index }
  cfg_member_keys    = [for cfg_index in range(var.configsvr_count) : "cfg${cfg_index}"]
  mongos_members     = { for mongos_index in range(var.mongos_count) : "mongos${mongos_index}" => mongos_index }
  mongos_member_keys = [for mongos_index in range(var.mongos_count) : "mongos${mongos_index}"]

  arbiter_members = {
    for member in flatten([
      for shard_index in range(var.shard_count) : [
        for arbiter_index in range(var.arbiters_per_replset) : {
          key           = "shard${shard_index}arb${arbiter_index}"
          shard_index   = shard_index
          arbiter_index = arbiter_index
        }
      ]
    ]) : member.key => member
  }

  arbiter_member_keys = flatten([
    for shard_index in range(var.shard_count) : [
      for arbiter_index in range(var.arbiters_per_replset) : "shard${shard_index}arb${arbiter_index}"
    ]
  ])
}

resource "google_compute_disk" "shard_disk" {
  for_each               = local.shard_members
  name                   = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-data"
  type                   = var.data_disk_type
  provisioned_iops       = local.data_disk_iops
  provisioned_throughput = local.data_disk_throughput
  labels                 = var.labels
  size                   = var.shardsvr_volume_size
  zone                   = var.zones[each.value.replica_index % length(var.zones)]
}

resource "google_compute_instance" "shard" {
  for_each                  = local.shard_members
  name                      = "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}"
  machine_type              = var.shardsvr_type
  min_cpu_platform          = var.min_cpu_platform
  resource_policies         = var.resource_policies
  allow_stopping_for_update = true
  zone                      = var.zones[each.value.replica_index % length(var.zones)]
  tags                      = ["${var.cluster_name}-${var.shardsvr_tag}"]
  labels = merge(var.labels, {
    ansible-group = tostring(each.value.shard_index),
    ansible-index = tostring(each.value.replica_index),
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
    source      = google_compute_disk.shard_disk[each.key].self_link
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
    hostnamectl set-hostname "${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}"

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

resource "google_compute_firewall" "mongodb-shardsvr-firewall" {
  name          = "${var.cluster_name}-${var.shardsvr_tag}-firewall"
  network       = var.vpc
  direction     = "INGRESS"
  source_ranges = ["${var.subnet_cidr}"]
  target_tags   = ["${var.cluster_name}-${var.shardsvr_tag}"]
  allow {
    protocol = "tcp"
    ports    = [var.shard_port]
  }
}

resource "google_compute_firewall" "mongodb-shardsvr-mongot-firewall" {
  count         = var.enable_mongot ? 1 : 0
  name          = "${var.cluster_name}-${var.shardsvr_tag}-mongot-firewall"
  network       = var.vpc
  direction     = "INGRESS"
  source_ranges = [var.subnet_cidr]
  target_tags   = ["${var.cluster_name}-${var.shardsvr_tag}"]

  allow {
    protocol = "tcp"
    ports    = [tostring(var.mongot_port)]
  }
}
