# Public IP
resource "azurerm_public_ip" "mongos" {
  for_each            = local.mongos_members
  name                = "${var.prefix}-${var.cluster_name}-${var.mongos_tag}0${each.value}-nic-public-ip"
  location            = var.location
  resource_group_name = var.resource_group_name
  allocation_method   = "Dynamic"
}

# NSG for mongos
resource "azurerm_network_security_group" "mongodb_mongos_nsg" {
  name                = "${var.prefix}-${var.cluster_name}-${var.mongos_tag}-nsg"
  location            = var.location
  resource_group_name = var.resource_group_name

  security_rule {
    name                       = "AllowMongoDBMongosPort"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = var.mongos_port
    source_address_prefix      = var.subnet_cidr
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "Allow-ICMP"
    priority                   = 120
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
    priority                   = 130
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = var.source_ranges
    destination_address_prefix = "*"
  }
}

# NICs for mongos nodes
resource "azurerm_network_interface" "mongos" {
  for_each            = local.mongos_members
  name                = "${var.prefix}-${var.cluster_name}-${var.mongos_tag}0${each.value}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.mongos[each.key].id

  }
}

# mongos VM instances
resource "azurerm_linux_virtual_machine" "mongos" {
  for_each            = local.mongos_members
  name                = "${var.prefix}-${var.cluster_name}-${var.mongos_tag}0${each.value}"
  location            = var.location
  resource_group_name = var.resource_group_name
  size                = var.mongos_type
  admin_username      = var.my_ssh_user
  tags = {
    ansible-group = "mongos",
    environment   = var.env_tag
  }

  network_interface_ids = [
    azurerm_network_interface.mongos[each.key].id
  ]

  admin_ssh_key {
    username   = var.my_ssh_user
    public_key = file(var.ssh_users[var.my_ssh_user])
  }

  source_image_reference {
    publisher = var.image.publisher
    offer     = var.image.offer
    sku       = var.image.sku
    version   = try(var.image.version, "latest")
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  priority                   = var.use_spot_instances ? "Spot" : "Regular"
  eviction_policy            = var.use_spot_instances ? "Deallocate" : null
  allow_extension_operations = true

  custom_data = base64encode(<<EOT
#!/bin/bash
hostnamectl set-hostname "${var.prefix}-${var.cluster_name}-${var.mongos_tag}0${each.value}"
echo "127.0.0.1 $(hostname) localhost" > /etc/hosts
EOT
  )
}
