resource "aws_ebs_volume" "cfg_disk" {
  for_each          = local.cfg_members
  availability_zone = data.aws_subnet.details[each.value % var.subnet_count].availability_zone
  size              = var.configsvr_volume_size
  type              = var.configsvr_disk_type
  iops              = local.configsvr_disk_iops
  throughput        = local.configsvr_disk_throughput
  tags = {
    Name        = "${var.prefix}-${var.cluster_name}-${var.configsvr_tag}0${each.value}-data"
    environment = var.env_tag
  }
}

resource "aws_instance" "cfg" {
  for_each        = local.cfg_members
  ami             = lookup(var.image, var.region)
  instance_type   = var.configsvr_type
  placement_group = var.placement_group
  root_block_device {
    volume_type = var.boot_disk_type
    volume_size = var.boot_disk_size
  }
  dynamic "instance_market_options" {
    for_each = var.use_spot_instances ? [1] : []
    content {
      market_type = "spot"
      spot_options {
        instance_interruption_behavior = "terminate"
        spot_instance_type             = "one-time"
      }
    }
  }
  subnet_id = data.aws_subnet.details[each.value % var.subnet_count].id
  key_name  = var.my_key_pair
  tags = {
    Name          = "${var.prefix}-${var.cluster_name}-${var.configsvr_tag}0${each.value}"
    ansible-group = "cfg"
    environment   = var.env_tag
  }
  vpc_security_group_ids = [aws_security_group.mongodb_cfgsvr_sg.id]
  user_data              = <<-EOT
    #!/bin/bash
    id -u "${var.my_ssh_user}" >/dev/null 2>&1 || useradd -m -s /bin/bash "${var.my_ssh_user}"
    usermod -aG wheel "${var.my_ssh_user}" 2>/dev/null || usermod -aG sudo "${var.my_ssh_user}" 2>/dev/null || true
    echo "${var.my_ssh_user} ALL=(ALL) NOPASSWD:ALL" > "/etc/sudoers.d/${var.my_ssh_user}"
    chmod 440 "/etc/sudoers.d/${var.my_ssh_user}"
    home_dir="$(getent passwd "${var.my_ssh_user}" | cut -d: -f6)"
    install -d -m 700 -o "${var.my_ssh_user}" -g "${var.my_ssh_user}" "$home_dir/.ssh"
    printf '%s' '${base64encode(file(var.ssh_public_key_path))}' | base64 -d > "$home_dir/.ssh/authorized_keys"
    chown "${var.my_ssh_user}:${var.my_ssh_user}" "$home_dir/.ssh/authorized_keys"
    chmod 600 "$home_dir/.ssh/authorized_keys"

    # Set the hostname
    hostnamectl set-hostname "${var.prefix}-${var.cluster_name}-${var.configsvr_tag}0${each.value}"

    # Update /etc/hosts to reflect the hostname change
    echo "127.0.0.1 $(hostname).${data.aws_route53_zone.private_zone.name} $(hostname) localhost" > /etc/hosts    

    DEVICE="/dev/nvme1n1"
    while [ ! -b "$DEVICE" ]; do
      echo "Waiting for $DEVICE to be attached..."
      sleep 2
    done

    # Add a dash to lsblk output to match the Terraform volume ID 
    DEVICE=$(lsblk -o NAME,SERIAL | sed 's/l/l-/' | grep "${aws_ebs_volume.cfg_disk[each.key].id}" | awk '{print "/dev/" $1}')

    mkfs.xfs $DEVICE

    mkdir -p /var/lib/mongo

    mount $DEVICE /var/lib/mongo

    UUID=$(blkid -s UUID -o value "$DEVICE")
    echo "UUID=$UUID /var/lib/mongo xfs defaults,noatime,nofail 0 2" >> /etc/fstab
  EOT
}

resource "aws_volume_attachment" "cfg_volume_attachment" {
  for_each    = local.cfg_members
  device_name = "/dev/sdf" # Placeholder, not used for NVMe but required by Terraform
  volume_id   = aws_ebs_volume.cfg_disk[each.key].id
  instance_id = aws_instance.cfg[each.key].id
}

resource "aws_security_group" "mongodb_cfgsvr_sg" {
  name        = "${var.prefix}-${var.cluster_name}-${var.configsvr_tag}-sg"
  description = "Allow traffic to MongoDB configsvr instances"
  vpc_id      = data.aws_vpc.vpc-network.id

  tags = {
    Name = "${var.prefix}-${var.cluster_name}-${var.configsvr_tag}-sg"
  }
}

resource "aws_security_group_rule" "mongodb-configsvr-ingress" {
  for_each          = toset([for port in var.configsvr_ports : tostring(port)])
  type              = "ingress"
  from_port         = each.value
  to_port           = each.value
  protocol          = "tcp"
  security_group_id = aws_security_group.mongodb_cfgsvr_sg.id
  cidr_blocks       = [var.subnet_cidr]
}

# Ingress rule (SSH from anywhere)
resource "aws_security_group_rule" "mongodb-configsvr-ssh_inbound" {
  type              = "ingress"
  from_port         = 22
  to_port           = 22
  protocol          = "tcp"
  cidr_blocks       = ["0.0.0.0/0"]
  security_group_id = aws_security_group.mongodb_cfgsvr_sg.id
  description       = "SSH from anywhere"
}

# Ingress rule for ICMP (ping) traffic
resource "aws_security_group_rule" "mongodb-configsvr-icmp-ingress" {
  type              = "ingress"
  from_port         = 8 # Type 8 for echo request (ping)
  to_port           = 0
  protocol          = "icmp"
  security_group_id = aws_security_group.mongodb_cfgsvr_sg.id
  cidr_blocks       = ["0.0.0.0/0"] # Allow from any IP address; adjust based on your needs
}

# Egress rule allowing all traffic
resource "aws_security_group_rule" "mongodb-configsvr-egress" {
  type              = "egress"
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  security_group_id = aws_security_group.mongodb_cfgsvr_sg.id
  cidr_blocks       = ["0.0.0.0/0"] # Allow all outbound IPv4 traffic
  ipv6_cidr_blocks  = ["::/0"]      # Allow all outbound IPv6 traffic
}

resource "aws_route53_record" "cfgsvr_dns_record" {
  for_each = local.cfg_members
  zone_id  = data.aws_route53_zone.private_zone.zone_id
  name     = "${var.prefix}-${var.cluster_name}-${var.configsvr_tag}0${each.value}"
  type     = "A"
  ttl      = "300"
  records  = [aws_instance.cfg[each.key].private_ip]
}
