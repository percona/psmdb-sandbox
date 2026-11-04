resource "docker_image" "vault" {
  name         = var.image
  keep_locally = true
}

resource "docker_volume" "data" { name = "${var.name}-data" }
resource "docker_volume" "config" { name = "${var.name}-config" }
resource "docker_volume" "credentials" {
  for_each = var.topologies
  name     = "${var.name}-${each.key}-credentials"
}

resource "terraform_data" "prepare" {
  triggers_replace = [docker_volume.config.id, docker_volume.data.id, docker_image.vault.image_id]
  provisioner "local-exec" {
    command = "python3 \"${abspath(path.module)}/../../../../scripts/vault-docker.py\" prepare"
    environment = {
      VAULT_CONTAINER      = var.name
      VAULT_IMAGE          = var.image
      VAULT_CONTROLLER_DIR = var.controller_dir
    }
  }
}

resource "docker_container" "vault" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  name       = var.name
  hostname   = var.name
  image      = docker_image.vault.image_id
  user       = "100:1000"
  entrypoint = ["/bin/sh", "-c"]
  command = [<<-SH
    vault server -config=/vault/config/vault.hcl &
    pid=$!
    (
      export VAULT_ADDR=https://127.0.0.1:8200 VAULT_CACERT=/vault/config/vault.crt
      while kill -0 "$pid" 2>/dev/null; do
        for file in /vault/data/tokens/*.token; do
          [ -f "$file" ] || continue
          VAULT_TOKEN="$(cat "$file")" vault token renew >/dev/null 2>&1 || true
        done
        sleep 3600
      done
    ) &
    trap 'kill "$pid"; wait "$pid"' TERM INT
    wait "$pid"
  SH
  ]
  mounts {
    type   = "volume"
    source = docker_volume.data.name
    target = "/vault/data"
  }
  mounts {
    type      = "volume"
    source    = docker_volume.config.name
    target    = "/vault/config"
    read_only = true
  }
  networks_advanced { name = var.network_name }
  restart    = "no"
  depends_on = [terraform_data.prepare]
}

resource "terraform_data" "bootstrap" {
  # Recheck readiness and renew credentials on every apply, including scale-out.
  triggers_replace = [timestamp(), docker_container.vault.id, jsonencode(var.topologies)]
  provisioner "local-exec" {
    command = "python3 \"${abspath(path.module)}/../../../../scripts/vault-docker.py\" bootstrap"
    environment = {
      VAULT_CONTAINER      = var.name
      VAULT_IMAGE          = var.image
      VAULT_CONTROLLER_DIR = var.controller_dir
      VAULT_CREDENTIALS    = jsonencode({ for t in var.topologies : t => docker_volume.credentials[t].name })
    }
  }
}

output "credentials" {
  value      = { for t, volume in docker_volume.credentials : t => volume.name }
  depends_on = [terraform_data.bootstrap]
}
