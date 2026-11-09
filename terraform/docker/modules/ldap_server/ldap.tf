# LDAP Docker container image
data "docker_registry_image" "ldap" {
  name = var.ldap_image
}

locals {
  ldap_repository       = replace(data.docker_registry_image.ldap.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
  ldap_admin_repository = replace(data.docker_registry_image.ldap_admin.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
}

resource "docker_image" "ldap" {
  platform      = var.docker_platform
  name          = "${local.ldap_repository}@${data.docker_registry_image.ldap.sha256_digest}"
  pull_triggers = [data.docker_registry_image.ldap.sha256_digest]
  keep_locally  = true
}

# LDAP Server volume
resource "docker_volume" "ldap_data" {
  name = "${var.ldap_server}-data"
}

# LDAP Server container
resource "docker_container" "ldap_server" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name  = var.ldap_server
  image = docker_image.ldap.image_id

  env = [
    "LDAP_ORGANISATION=${var.ldap_org}",
    "LDAP_DOMAIN=${var.ldap_domain}",
    "LDAP_BASE_DN=dc=${replace(var.ldap_domain, ".", ",dc=")}",
    "LDAP_ADMIN_PASSWORD=${var.ldap_admin_password}",
    "LDAP_TLS=false"
  ]

  volumes {
    volume_name    = docker_volume.ldap_data.name
    container_path = "/var/lib/ldap"
  }

  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
  ports {
    internal = var.ldap_port
  }

  healthcheck {
    test     = ["CMD-SHELL", "ldapsearch -x -H ldap://localhost -D cn=admin,dc=${replace(var.ldap_domain, ".", ",dc=")} -w ${var.ldap_admin_password} -b dc=${replace(var.ldap_domain, ".", ",dc=")} || exit 1"]
    interval = "10s"
    timeout  = "5s"
    retries  = 5
  }
  wait    = true
  restart = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.ldap]
  }
}

# Creation of users
resource "null_resource" "ldap_users" {
  count = length(var.ldap_users)

  provisioner "local-exec" {
    command = <<EOT
      echo '${templatefile("${path.module}/ldap_user.ldif.tmpl", {
    uid      = var.ldap_users[count.index].uid,
    cn       = var.ldap_users[count.index].cn,
    sn       = var.ldap_users[count.index].sn,
    password = var.ldap_users[count.index].password,
    dc_base  = replace(var.ldap_domain, ".", ",dc=")
})}' | docker exec -i ${var.ldap_server} ldapadd -x -D "cn=admin,dc=${replace(var.ldap_domain, ".", ",dc=")}" -w "${var.ldap_admin_password}"
    EOT
}
depends_on = [docker_container.ldap_server]
}

# Admin container image
data "docker_registry_image" "ldap_admin" {
  name = var.ldap_admin_image
}

resource "docker_image" "ldap_admin" {
  platform      = var.docker_platform
  name          = "${local.ldap_admin_repository}@${data.docker_registry_image.ldap_admin.sha256_digest}"
  pull_triggers = [data.docker_registry_image.ldap_admin.sha256_digest]
  keep_locally  = true
}

# Admin interface
resource "docker_container" "phpldapadmin" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name  = "${var.ldap_server}-${var.ldap_admin_server}"
  image = docker_image.ldap_admin.image_id

  env = [
    "PHPLDAPADMIN_HTTPS=false",
    "PHPLDAPADMIN_LDAP_HOSTS=${var.ldap_server}"
  ]

  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }

  ports {
    internal = var.ldap_admin_port
    external = var.ldap_external_admin_port
  }

  healthcheck {
    test     = ["CMD-SHELL", "ps aux | grep -q '[a]pache2'"]
    interval = "10s"
    timeout  = "5s"
    retries  = 5
  }

  wait    = true
  restart = "on-failure"

  lifecycle {
    replace_triggered_by = [docker_image.ldap_admin]
  }

  depends_on = [docker_container.ldap_server]
}
