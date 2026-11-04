locals {
  ldap_hosts = { for name, server in var.ldap_servers : name => "${var.prefix}-${name}" }
}

resource "azurerm_public_ip" "ldap" {
  for_each            = var.ldap_servers
  name                = "${local.ldap_hosts[each.key]}-public-ip"
  location            = var.location
  resource_group_name = local.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  depends_on          = [time_sleep.wait_after_rg]
}

resource "azurerm_network_security_group" "ldap" {
  for_each            = var.ldap_servers
  name                = "${local.ldap_hosts[each.key]}-nsg"
  location            = var.location
  resource_group_name = local.resource_group_name

  security_rule {
    name                       = "Allow-LDAP"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "389"
    source_address_prefix      = var.subnet_cidr
    destination_address_prefix = "*"
  }
  security_rule {
    name                       = "Allow-phpLDAPadmin"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "80"
    source_address_prefix      = var.subnet_cidr
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
  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface" "ldap" {
  for_each            = var.ldap_servers
  name                = "${local.ldap_hosts[each.key]}-nic"
  location            = var.location
  resource_group_name = local.resource_group_name
  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.subnet.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.ldap[each.key].id
  }
  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface_security_group_association" "ldap" {
  for_each                  = var.ldap_servers
  network_interface_id      = azurerm_network_interface.ldap[each.key].id
  network_security_group_id = azurerm_network_security_group.ldap[each.key].id
}

resource "azurerm_linux_virtual_machine" "ldap" {
  for_each            = var.ldap_servers
  name                = local.ldap_hosts[each.key]
  location            = var.location
  resource_group_name = local.resource_group_name
  size                = each.value.vm_size
  admin_username      = var.my_ssh_user
  network_interface_ids = [
    azurerm_network_interface.ldap[each.key].id,
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
  tags       = { role = "ldap" }
  depends_on = [azurerm_network_interface_security_group_association.ldap]
}
