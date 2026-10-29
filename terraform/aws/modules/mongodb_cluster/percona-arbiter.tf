resource "aws_instance" "arbiter" {
  for_each = local.arbiter_members
  tags = {
    Name          = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"
    ansible-group = tostring(each.value.shard_index)
    ansible-index = tostring(each.value.arbiter_index)
    environment   = var.env_tag
  }
  instance_type   = var.arbiter_type
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
  subnet_id                   = data.aws_subnet.details[(var.shardsvr_replicas + each.value.arbiter_index) % var.subnet_count].id
  ami                         = lookup(var.image, var.region)
  associate_public_ip_address = true
  key_name                    = var.my_key_pair
  vpc_security_group_ids      = [aws_security_group.mongodb-arbiter-sg.id]
  user_data                   = <<-EOT
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
    hostnamectl set-hostname "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"

    # Update /etc/hosts to reflect the hostname change
    echo "127.0.0.1 $(hostname).${data.aws_route53_zone.private_zone.name} $(hostname) localhost" > /etc/hosts    
  EOT
}

resource "aws_security_group" "mongodb-arbiter-sg" {
  name        = "${var.prefix}-${var.cluster_name}-${var.arbiter_tag}-sg"
  description = "Allow traffic to MongoDB arbiter instances"
  vpc_id      = data.aws_vpc.vpc-network.id

  tags = {
    Name = "${var.prefix}-${var.cluster_name}-${var.arbiter_tag}-sg"
  }
}

resource "aws_security_group_rule" "mongodb-arbiter-ingress" {
  for_each          = toset([for port in var.arbiter_ports : tostring(port)])
  type              = "ingress"
  from_port         = each.value
  to_port           = each.value
  protocol          = "tcp"
  security_group_id = aws_security_group.mongodb-arbiter-sg.id
  cidr_blocks       = [var.subnet_cidr]
}

# Ingress rule (SSH from anywhere)
resource "aws_security_group_rule" "mongodb-arbiter-ssh_inbound" {
  type              = "ingress"
  from_port         = 22
  to_port           = 22
  protocol          = "tcp"
  cidr_blocks       = ["0.0.0.0/0"]
  security_group_id = aws_security_group.mongodb-arbiter-sg.id
  description       = "SSH from anywhere"
}

# Ingress rule for ICMP (ping) traffic
resource "aws_security_group_rule" "mongodb-arbiter-icmp-ingress" {
  type              = "ingress"
  from_port         = 8 # Type 8 for echo request (ping)
  to_port           = 0
  protocol          = "icmp"
  security_group_id = aws_security_group.mongodb-arbiter-sg.id
  cidr_blocks       = ["0.0.0.0/0"] # Allow from any IP address; adjust based on your needs
}

# Egress rule allowing all traffic
resource "aws_security_group_rule" "mongodb-arbiter-egress" {
  type              = "egress"
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  security_group_id = aws_security_group.mongodb-arbiter-sg.id
  cidr_blocks       = ["0.0.0.0/0"] # Allow all outbound IPv4 traffic
  ipv6_cidr_blocks  = ["::/0"]      # Allow all outbound IPv6 traffic
}

resource "aws_route53_record" "arbiter_dns_record" {
  for_each = local.arbiter_members
  zone_id  = data.aws_route53_zone.private_zone.zone_id
  name     = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"
  type     = "A"
  ttl      = "300"
  records  = [aws_instance.arbiter[each.key].private_ip]
}
