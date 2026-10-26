terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.68.0"
    }
  }
}

# Get the VPC object using its name
data "aws_vpc" "vpc-network" {
  filter {
    name   = "tag:Name"
    values = [var.vpc]
  }
}

# Get all subnets in the VPC
data "aws_subnets" "subnets" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.vpc-network.id]
  }
}

# Get details for subnet
data "aws_subnet" "details" {
  count = var.subnet_count
  id    = data.aws_subnets.subnets.ids[count.index]
}

# Get the existing DNS zone
data "aws_route53_zone" "private_zone" {
  name         = var.vpc
  private_zone = true
}

locals {
  # EBS performance settings only apply to the volume types that accept them.
  # io1/io2 require an IOPS value, so fall back to 3000.
  data_disk_iops = var.data_disk_type == "gp3" ? var.data_disk_iops : (
    contains(["io1", "io2"], var.data_disk_type) ? coalesce(var.data_disk_iops, 3000) : null
  )
  data_disk_throughput = var.data_disk_type == "gp3" ? var.data_disk_throughput : null
}

locals {
  configsvr_disk_iops = var.configsvr_disk_type == "gp3" ? var.configsvr_disk_iops : (
    contains(["io1", "io2"], var.configsvr_disk_type) ? coalesce(var.configsvr_disk_iops, 3000) : null
  )
  configsvr_disk_throughput = var.configsvr_disk_type == "gp3" ? var.configsvr_disk_throughput : null
}
