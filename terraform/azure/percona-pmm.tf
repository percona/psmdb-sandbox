# Public IP for PMM
resource "azurerm_public_ip" "pmm" {
  count               = var.enable_pmm ? 1 : 0
  name                = "${local.pmm_host}-public-ip"
  location            = var.location
  resource_group_name = local.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  depends_on          = [time_sleep.wait_after_rg]
}

# PMM NIC
resource "azurerm_network_interface" "pmm_nic" {
  count               = var.enable_pmm ? 1 : 0
  name                = "${local.pmm_host}-nic"
  location            = var.location
  resource_group_name = local.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.subnet.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.pmm[0].id
  }
  depends_on = [time_sleep.wait_after_rg]
}

# Network Security Group
resource "azurerm_network_security_group" "pmm_nsg" {
  count               = var.enable_pmm ? 1 : 0
  name                = "${local.pmm_host}-nsg"
  location            = var.location
  resource_group_name = local.resource_group_name

  security_rule {
    name                       = "Allow-PMM-Port"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_ranges    = [var.pmm_port]
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

  depends_on = [time_sleep.wait_after_rg]
}

# Associate NSG with NIC
resource "azurerm_network_interface_security_group_association" "pmm_nsg_assoc" {
  count                     = var.enable_pmm ? 1 : 0
  network_interface_id      = azurerm_network_interface.pmm_nic[0].id
  network_security_group_id = azurerm_network_security_group.pmm_nsg[0].id
}

# Disk for PMM
resource "azurerm_managed_disk" "pmm_data_disk" {
  count                = var.enable_pmm ? 1 : 0
  name                 = "${local.pmm_host}-data"
  location             = var.location
  resource_group_name  = local.resource_group_name
  storage_account_type = var.pmm_disk_type
  create_option        = "Empty"
  disk_size_gb         = var.pmm_volume_size

  depends_on = [time_sleep.wait_after_rg]
}

# VM
resource "azurerm_linux_virtual_machine" "pmm" {
  count               = var.enable_pmm ? 1 : 0
  name                = local.pmm_host
  location            = var.location
  resource_group_name = local.resource_group_name
  size                = var.pmm_type
  admin_username      = var.my_ssh_user
  network_interface_ids = [
    azurerm_network_interface.pmm_nic[0].id,
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

  custom_data = base64encode(<<EOT
#!/bin/bash
hostnamectl set-hostname "${local.pmm_host}"
echo "127.0.0.1 $(hostname) localhost" > /etc/hosts

DEVICE="/dev/sdc"
mkfs.xfs $DEVICE
mkdir -p /var/lib/docker
mount $DEVICE /var/lib/docker
UUID=$(blkid -s UUID -o value "$DEVICE")
echo "UUID=$UUID /var/lib/docker xfs defaults,noatime,nofail 0 2" >> /etc/fstab
EOT
  )

  tags = {
    role = "pmm"
  }

  depends_on = [
    azurerm_managed_disk.pmm_data_disk[0],
    azurerm_network_interface_security_group_association.pmm_nsg_assoc[0]
  ]
}

# Disk attachment
resource "azurerm_virtual_machine_data_disk_attachment" "pmm_data_attach" {
  count              = var.enable_pmm ? 1 : 0
  managed_disk_id    = azurerm_managed_disk.pmm_data_disk[0].id
  virtual_machine_id = azurerm_linux_virtual_machine.pmm[0].id
  lun                = 0
  caching            = "ReadWrite"
}
