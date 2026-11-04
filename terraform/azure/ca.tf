resource "azurerm_public_ip" "ca" {
  count               = var.enable_ca && var.ca_placement == "dedicated" ? 1 : 0
  name                = "${local.ca_host}-public-ip"
  location            = var.location
  resource_group_name = local.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  depends_on          = [time_sleep.wait_after_rg]
}

resource "azurerm_network_interface" "ca" {
  count               = var.enable_ca && var.ca_placement == "dedicated" ? 1 : 0
  name                = "${local.ca_host}-nic"
  location            = var.location
  resource_group_name = local.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.subnet.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.ca[0].id
  }

  depends_on = [time_sleep.wait_after_rg]
}

resource "azurerm_linux_virtual_machine" "ca" {
  count               = var.enable_ca && var.ca_placement == "dedicated" ? 1 : 0
  name                = local.ca_host
  location            = var.location
  resource_group_name = local.resource_group_name
  size                = var.ca_type
  admin_username      = var.my_ssh_user
  network_interface_ids = [
    azurerm_network_interface.ca[0].id,
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
    hostnamectl set-hostname "${local.ca_host}"
    echo "127.0.0.1 $(hostname) localhost" > /etc/hosts
  EOT
  )

  tags = {
    role = "ca"
  }
}
