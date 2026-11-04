resource "azurerm_public_ip" "pcsm" {
  count               = var.enable_pcsm ? 1 : 0
  name                = "${local.pcsm_host}-public-ip"
  location            = var.location
  resource_group_name = local.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  depends_on          = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface" "pcsm" {
  count               = var.enable_pcsm ? 1 : 0
  name                = "${local.pcsm_host}-nic"
  location            = var.location
  resource_group_name = local.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.subnet.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.pcsm[0].id
  }

  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_network_security_group" "pcsm" {
  count               = var.enable_pcsm ? 1 : 0
  name                = "${local.pcsm_host}-nsg"
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

  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface_security_group_association" "pcsm" {
  count                     = var.enable_pcsm ? 1 : 0
  network_interface_id      = azurerm_network_interface.pcsm[0].id
  network_security_group_id = azurerm_network_security_group.pcsm[0].id
}

resource "azurerm_linux_virtual_machine" "pcsm" {
  count               = var.enable_pcsm ? 1 : 0
  name                = local.pcsm_host
  location            = var.location
  resource_group_name = local.resource_group_name
  size                = var.pcsm_type
  admin_username      = var.my_ssh_user
  network_interface_ids = [
    azurerm_network_interface.pcsm[0].id,
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

  custom_data = base64encode(<<-EOT
    #!/bin/bash
    hostnamectl set-hostname "${local.pcsm_host}"
  EOT
  )

  tags = { role = "pcsm" }

  depends_on = [azurerm_network_interface_security_group_association.pcsm[0]]
}
