variable "pmm_bare_clients" {
  description = "Hosts with only a PMM client, using the environment machine image and replica set machine type."
  type = map(object({
    pmm_client_repo    = optional(string, "")
    pmm_client_version = optional(string, "")
  }))
  default = {}

  validation {
    condition     = length(var.pmm_bare_clients) == 0 || var.enable_pmm
    error_message = "PMM clients without MongoDB need enable_pmm = true."
  }
  validation {
    condition     = !contains(keys(var.pmm_bare_clients), var.default_pmm_host)
    error_message = "A PMM client cannot have the same name as the PMM server."
  }
}

resource "aws_instance" "pmm_bare_client" {
  for_each               = var.pmm_bare_clients
  ami                    = lookup(var.image, var.region)
  instance_type          = var.replsetsvr_type
  key_name               = aws_key_pair.my_key_pair.key_name
  subnet_id              = aws_subnet.vpc-subnet[0].id
  vpc_security_group_ids = [aws_security_group.pmm_bare_client[0].id]
  tags                   = { Name = "${var.prefix}-${each.key}", role = "pmm-client" }

  user_data = <<-EOT
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
    hostnamectl set-hostname "${var.prefix}-${each.key}"
  EOT
}

resource "aws_security_group" "pmm_bare_client" {
  count  = length(var.pmm_bare_clients) > 0 ? 1 : 0
  name   = "${var.prefix}-pmm-clients-sg"
  vpc_id = aws_vpc.vpc-network.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.source_ranges]
  }
  # PMM Extensions may bootstrap MongoDB on these hosts.
  ingress {
    from_port   = 27017
    to_port     = 27017
    protocol    = "tcp"
    cidr_blocks = [var.subnet_cidr]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

locals {
  pmm_bare_client_ips = { for key, client in aws_instance.pmm_bare_client : "${var.prefix}-${key}" => client.public_ip }
  pmm_bare_client_hosts = {
    for key, client in aws_instance.pmm_bare_client : "${var.prefix}-${key}" => {
      ip                 = client.public_ip
      pmm_client_repo    = lookup(local.pmm_client_inventory_repos, var.pmm_bare_clients[key].pmm_client_repo, var.pmm_bare_clients[key].pmm_client_repo)
      pmm_client_version = var.pmm_bare_clients[key].pmm_client_version
    }
  }
}

resource "local_file" "AnsibleInventoryPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0
  content = templatefile("${path.module}/../modules/pmm_clients/inventory.tmpl", {
    hostname_pmm         = local.pmm_host
    ip_pmm               = aws_instance.pmm[0].public_ip
    pmm_bare_clients     = local.pmm_bare_client_hosts
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    pmm_image            = var.pmm_image
    pmm_client_version   = var.pmm_client_version
    pmm_client_repo      = lookup(local.pmm_client_inventory_repos, var.pmm_client_repo, var.pmm_client_repo)
  })
  filename = "${var.prefix}_inventory_pmm_clients"
}

resource "local_file" "SSHConfigPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0
  content = templatefile("${path.module}/../modules/pmm_clients/ssh_config.tmpl", {
    include_pmm          = length(var.clusters) + length(var.replsets) == 0
    hostname_pmm         = local.pmm_host
    public_ip_pmm        = aws_instance.pmm[0].public_ip
    pmm_bare_clients     = local.pmm_bare_client_ips
    my_ssh_user          = var.my_ssh_user
    ssh_private_key_path = var.ssh_private_key_path
    ssh_gateway_name     = var.ssh_gateway_name
    enable_ssh_gateway   = var.enable_ssh_gateway
    port_to_forward      = var.port_to_forward
    pmm_port             = var.pmm_port
  })
  filename = "${var.prefix}_ssh_config_pmm_clients"
}
