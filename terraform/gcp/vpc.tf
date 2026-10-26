# Gets the list of availability zones in selected gcp region
data "google_compute_zones" "available" {
  status = "UP"
}

locals {
  candidate_zones = length(var.zones) > 0 ? var.zones : data.google_compute_zones.available.names
  # Zones used for every instance. Members are spread round-robin over this list;
  # infrastructure hosts (PMM, plgm, CA, ...) use the first entry.
  zones = var.zone_mode == "single" ? [local.candidate_zones[0]] : local.candidate_zones

  placement_policies = var.compact_placement ? [google_compute_resource_policy.compact[0].self_link] : []

  # Provisioned performance only applies to disk types that support it
  configsvr_disk_type       = coalesce(var.configsvr_disk_type, var.data_disk_type)
  configsvr_disk_iops       = local.configsvr_disk_type == var.data_disk_type ? var.data_disk_iops : null
  configsvr_disk_throughput = local.configsvr_disk_type == var.data_disk_type ? var.data_disk_throughput : null
}

resource "google_compute_resource_policy" "compact" {
  count  = var.compact_placement ? 1 : 0
  name   = "${var.prefix}-compact-placement"
  region = var.region

  group_placement_policy {
    collocation = "COLLOCATED"
  }
}

# Set your desired VPC
resource "google_compute_network" "vpc-network" {
  name                    = local.vpc
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "vpc-subnet" {
  name          = "${var.prefix}-${var.subnet_name}"
  ip_cidr_range = var.subnet_cidr
  region        = var.region
  network       = google_compute_network.vpc-network.id
}

resource "google_compute_firewall" "internal" {
  name    = "${var.prefix}-allow-internal"
  network = google_compute_network.vpc-network.id

  allow {
    protocol = "tcp"
    ports    = ["0-65535"]
  }

  allow {
    protocol = "udp"
    ports    = ["0-65535"]
  }

  allow {
    protocol = "icmp"
  }

  source_ranges = [var.subnet_cidr]
  direction     = "INGRESS"
  priority      = 65534
}

resource "google_compute_firewall" "icmp" {
  name    = "${var.prefix}-allow-icmp"
  network = google_compute_network.vpc-network.id

  allow {
    protocol = "icmp"
  }

  source_ranges = ["0.0.0.0/0"]
  direction     = "INGRESS"
  priority      = 65534
}

resource "google_compute_firewall" "ssh" {
  name    = "${var.prefix}-allow-ssh"
  network = google_compute_network.vpc-network.id

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = [var.source_ranges]
  direction     = "INGRESS"
  priority      = 65534
}