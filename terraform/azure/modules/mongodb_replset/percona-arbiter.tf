# Public IP
resource "azurerm_public_ip" "arbiter" {
  for_each            = local.arbiter_members
  name                = "${var.prefix}-${var.rs_name}-${var.arbiter_tag}${each.value}-nic-public-ip"
  location            = var.location
  resource_group_name = var.resource_group_name
  allocation_method   = "Dynamic"
}

# NSG for arbiter nodes
resource "azurerm_network_security_group" "mongodb_arbiter_nsg" {
  name                = "${var.prefix}-${var.rs_name}-${var.arbiter_tag}-nsg"
  location            = var.location
  resource_group_name = var.resource_group_name

  security_rule {
    name                       = "AllowMongoDBArbiterPort"
    priority                   = 130
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = var.arbiter_port
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

# NIC for arbiter nodes
resource "azurerm_network_interface" "arbiter" {
  for_each            = local.arbiter_members
  name                = "${var.prefix}-${var.rs_name}-${var.arbiter_tag}${each.value}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.arbiter[each.key].id
  }
}

# Arbiter VMs
resource "azurerm_linux_virtual_machine" "arbiter" {
  for_each            = local.arbiter_members
  name                = "${var.prefix}-${var.rs_name}-${var.arbiter_tag}${each.value}"
  location            = var.location
  resource_group_name = var.resource_group_name
  size                = var.arbiter_type
  admin_username      = var.my_ssh_user
  tags = {
    ansible-group = var.replset_tag,
    environment   = var.env_tag
  }

  network_interface_ids = [
    azurerm_network_interface.arbiter[each.key].id
  ]

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

  priority        = var.use_spot_instances ? "Spot" : "Regular"
  eviction_policy = var.use_spot_instances ? "Deallocate" : null

  custom_data = base64encode(<<EOT
#!/bin/bash
hostnamectl set-hostname "${var.prefix}-${var.rs_name}-${var.arbiter_tag}${each.value}"
echo "127.0.0.1 $(hostname) localhost" > /etc/hosts
EOT
  )
}
