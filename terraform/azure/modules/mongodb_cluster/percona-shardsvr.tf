locals {
  shard_members = {
    for member in flatten([
      for shard_index in range(var.shard_count) : [
        for replica_index in range(var.shardsvr_replicas) : {
          key           = "shard${shard_index}svr${replica_index}"
          shard_index   = shard_index
          replica_index = replica_index
        }
      ]
    ]) : member.key => member
  }

  shard_member_keys = flatten([
    for shard_index in range(var.shard_count) : [
      for replica_index in range(var.shardsvr_replicas) : "shard${shard_index}svr${replica_index}"
    ]
  ])

  cfg_members        = { for cfg_index in range(var.configsvr_count) : "cfg${cfg_index}" => cfg_index }
  cfg_member_keys    = [for cfg_index in range(var.configsvr_count) : "cfg${cfg_index}"]
  mongos_members     = { for mongos_index in range(var.mongos_count) : "mongos${mongos_index}" => mongos_index }
  mongos_member_keys = [for mongos_index in range(var.mongos_count) : "mongos${mongos_index}"]

  arbiter_members = {
    for member in flatten([
      for shard_index in range(var.shard_count) : [
        for arbiter_index in range(var.arbiters_per_replset) : {
          key           = "shard${shard_index}arb${arbiter_index}"
          shard_index   = shard_index
          arbiter_index = arbiter_index
        }
      ]
    ]) : member.key => member
  }

  arbiter_member_keys = flatten([
    for shard_index in range(var.shard_count) : [
      for arbiter_index in range(var.arbiters_per_replset) : "shard${shard_index}arb${arbiter_index}"
    ]
  ])
}

# Public IP
resource "azurerm_public_ip" "shard" {
  for_each            = local.shard_members
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-nic-public-ip"
  location            = var.location
  resource_group_name = var.resource_group_name
  allocation_method   = "Dynamic"
}

# NSG for shard nodes
resource "azurerm_network_security_group" "mongodb_shard_nsg" {
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}-nsg"
  location            = var.location
  resource_group_name = var.resource_group_name

  security_rule {
    name                       = "AllowMongoDBShardPort"
    priority                   = 120
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = var.shard_port
    source_address_prefix      = var.subnet_cidr
    destination_address_prefix = "*"
  }

  dynamic "security_rule" {
    for_each = var.enable_mongot ? [1] : []
    content {
      name                       = "AllowMongoDBSearchPort"
      priority                   = 125
      direction                  = "Inbound"
      access                     = "Allow"
      protocol                   = "Tcp"
      source_port_range          = "*"
      destination_port_range     = tostring(var.mongot_port)
      source_address_prefix      = var.subnet_cidr
      destination_address_prefix = "*"
    }
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

# Data disks for each shard node
resource "azurerm_managed_disk" "shard_disk" {
  for_each             = local.shard_members
  name                 = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-data"
  location             = var.location
  resource_group_name  = var.resource_group_name
  storage_account_type = var.data_disk_type
  create_option        = "Empty"
  disk_size_gb         = var.shardsvr_volume_size
}

# NIC for shard node
resource "azurerm_network_interface" "shard" {
  for_each            = local.shard_members
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.shard[each.key].id
  }
}

# VM for each shard server
resource "azurerm_linux_virtual_machine" "shard" {
  for_each            = local.shard_members
  name                = "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}"
  location            = var.location
  resource_group_name = var.resource_group_name
  size                = var.shardsvr_type
  admin_username      = var.my_ssh_user
  tags = {
    ansible-group = tostring(each.value.shard_index),
    ansible-index = tostring(each.value.replica_index),
    environment   = var.env_tag
  }

  network_interface_ids = [
    azurerm_network_interface.shard[each.key].id
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
hostnamectl set-hostname "${var.prefix}-${var.cluster_name}-${var.shardsvr_tag}0${each.value.shard_index}svr${each.value.replica_index}"
echo "127.0.0.1 $(hostname) localhost" > /etc/hosts

# Wait for the disk to appear
DEVICE="/dev/sdc"
while [ ! -b "$DEVICE" ]; do
  echo "Waiting for $DEVICE to become available..."
  sleep 2
done

# Format and mount the disk
mkfs.xfs $DEVICE
mkdir -p /var/lib/mongo
mount $DEVICE /var/lib/mongo

# Add to fstab
UUID=$(blkid -s UUID -o value "$DEVICE")
echo "UUID=$UUID /var/lib/mongo xfs defaults,noatime,nofail 0 2" >> /etc/fstab
EOT
  )
}

# Attach data disk to each shard server
resource "azurerm_virtual_machine_data_disk_attachment" "shard_disk_attachment" {
  for_each           = local.shard_members
  managed_disk_id    = azurerm_managed_disk.shard_disk[each.key].id
  virtual_machine_id = azurerm_linux_virtual_machine.shard[each.key].id
  lun                = 0
  caching            = "ReadWrite"
}
