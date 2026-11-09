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

resource "azurerm_public_ip" "pmm_bare_client" {
  for_each            = var.pmm_bare_clients
  name                = "${var.prefix}-${each.key}-public-ip"
  location            = var.location
  resource_group_name = local.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  depends_on          = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface" "pmm_bare_client" {
  for_each            = var.pmm_bare_clients
  name                = "${var.prefix}-${each.key}-nic"
  location            = var.location
  resource_group_name = local.resource_group_name
  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.subnet.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.pmm_bare_client[each.key].id
  }
  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_network_security_group" "pmm_bare_client" {
  count               = length(var.pmm_bare_clients) > 0 ? 1 : 0
  name                = "${var.prefix}-pmm-clients-nsg"
  location            = var.location
  resource_group_name = local.resource_group_name
  security_rule {
    name                       = "Allow-SSH"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = var.source_ranges
    destination_address_prefix = "*"
  }
  # MongoDB access for PMM Extensions stays within the environment network.
  security_rule {
    name                       = "Allow-MongoDB-Internal"
    priority                   = 130
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "27017"
    source_address_prefix      = var.subnet_cidr
    destination_address_prefix = "*"
  }
  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface_security_group_association" "pmm_bare_client" {
  for_each                  = var.pmm_bare_clients
  network_interface_id      = azurerm_network_interface.pmm_bare_client[each.key].id
  network_security_group_id = azurerm_network_security_group.pmm_bare_client[0].id
}

resource "azurerm_linux_virtual_machine" "pmm_bare_client" {
  for_each              = var.pmm_bare_clients
  name                  = "${var.prefix}-${each.key}"
  location              = var.location
  resource_group_name   = local.resource_group_name
  size                  = var.replsetsvr_type
  admin_username        = var.my_ssh_user
  network_interface_ids = [azurerm_network_interface.pmm_bare_client[each.key].id]
  admin_ssh_key {
    username   = var.my_ssh_user
    public_key = file(var.ssh_users[var.my_ssh_user])
  }
  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }
  source_image_reference {
    publisher = var.image.publisher
    offer     = var.image.offer
    sku       = var.image.sku
    version   = try(var.image.version, "latest")
  }
  custom_data = base64encode(<<-EOT
    #!/bin/bash
    hostnamectl set-hostname "${var.prefix}-${each.key}"
  EOT
  )
  tags       = { role = "pmm-client" }
  depends_on = [azurerm_network_interface_security_group_association.pmm_bare_client]
}

locals {
  pmm_bare_client_ips = { for client in azurerm_linux_virtual_machine.pmm_bare_client : client.name => client.public_ip_address }
  pmm_bare_client_hosts = {
    for key, client in azurerm_linux_virtual_machine.pmm_bare_client : client.name => {
      ip                 = client.public_ip_address
      pmm_client_repo    = lookup(local.pmm_client_inventory_repos, var.pmm_bare_clients[key].pmm_client_repo, var.pmm_bare_clients[key].pmm_client_repo)
      pmm_client_version = var.pmm_bare_clients[key].pmm_client_version
    }
  }
}

resource "local_file" "AnsibleInventoryPmmClients" {
  count = length(var.pmm_bare_clients) > 0 ? 1 : 0
  content = templatefile("${path.module}/../modules/pmm_clients/inventory.tmpl", {
    hostname_pmm         = local.pmm_host
    ip_pmm               = azurerm_linux_virtual_machine.pmm[0].public_ip_address
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
    public_ip_pmm        = azurerm_linux_virtual_machine.pmm[0].public_ip_address
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
