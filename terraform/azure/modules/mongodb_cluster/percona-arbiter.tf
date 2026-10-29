# Public IP
resource "azurerm_public_ip" "arbiter" {
  for_each            = local.arbiter_members
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}-nic-public-ip"
  location            = var.location
  resource_group_name = var.resource_group_name
  allocation_method   = "Dynamic"
}

resource "azurerm_linux_virtual_machine" "arbiter" {
  for_each            = local.arbiter_members
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"
  location            = var.location
  resource_group_name = var.resource_group_name
  size                = var.arbiter_type
  admin_username      = var.my_ssh_user
  network_interface_ids = [
    azurerm_network_interface.arbiter[each.key].id,
  ]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
    name                 = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}-os-disk"
  }

  source_image_reference {
    publisher = var.image.publisher
    offer     = var.image.offer
    sku       = var.image.sku
    version   = try(var.image.version, "latest")
  }

  admin_ssh_key {
    username   = var.my_ssh_user
    public_key = file(var.ssh_users[var.my_ssh_user])
  }

  disable_password_authentication = true

  tags = {
    ansible-group = tostring(each.value.shard_index)
    ansible-index = tostring(each.value.arbiter_index)
    environment   = var.env_tag
  }

  custom_data = base64encode(<<EOT
    #!/bin/bash
    hostnamectl set-hostname "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}"
    echo "127.0.0.1 $(hostname) localhost" > /etc/hosts
  EOT
  )
}

resource "azurerm_network_interface" "arbiter" {
  for_each            = local.arbiter_members
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}arb${each.value.arbiter_index}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.arbiter[each.key].id
  }
}

resource "azurerm_network_security_group" "arbiter" {
  name                = "${var.prefix}-${var.cluster_name}-${var.arbiter_tag}-nsg"
  location            = var.location
  resource_group_name = var.resource_group_name

  security_rule {
    name                       = "allow-arbiter-port"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_ranges    = [var.arbiter_port]
    source_address_prefix      = var.subnet_cidr
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "Allow-ICMP"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Icmp"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "0.0.0.0/0"
    destination_address_prefix = "*"
  }

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
}

resource "azurerm_network_interface_security_group_association" "arbiter" {
  for_each                  = local.arbiter_members
  network_interface_id      = azurerm_network_interface.arbiter[each.key].id
  network_security_group_id = azurerm_network_security_group.arbiter.id
}
