locals {
  pcsm_pmm_server = try(var.pmm_servers[var.pcsm_pmm_host], null)
  pcsm_pmm_port   = try(local.pcsm_pmm_server.pmm_port, var.pcsm_pmm_port)
  pcsm_pmm_user   = try(local.pcsm_pmm_server.pmm_server_user, var.pcsm_pmm_server_user)
  pcsm_pmm_pwd    = try(local.pcsm_pmm_server.pmm_server_pwd, var.pcsm_pmm_server_pwd)
}

resource "docker_image" "pcsm" {
  platform     = var.docker_platform
  count        = var.enable_pcsm ? 1 : 0
  name         = var.pcsm_image
  keep_locally = true
}

data "docker_registry_image" "pcsm_pmm_client" {
  count = var.enable_pcsm ? 1 : 0
  name  = var.pcsm_pmm_client_image
}

resource "docker_image" "pcsm_pmm_client" {
  platform      = var.docker_platform
  count         = var.enable_pcsm ? 1 : 0
  name          = "${replace(data.docker_registry_image.pcsm_pmm_client[0].name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")}@${data.docker_registry_image.pcsm_pmm_client[0].sha256_digest}"
  pull_triggers = [data.docker_registry_image.pcsm_pmm_client[0].sha256_digest]
  keep_locally  = true
}

resource "docker_container" "pcsm" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.prefix
  }

  count = var.enable_pcsm ? 1 : 0

  name     = "${local.name_prefix}pcsm"
  hostname = "${local.name_prefix}pcsm"
  image    = docker_image.pcsm[0].image_id
  # The generated URI file is deliberately 0600 on the host. Running as root
  # avoids weakening those permissions for the image's unprivileged UID.
  user       = "0:0"
  entrypoint = ["/bin/sh", "-c"]
  command = [
    "set -a; . /run/secrets/pcsm.env; set +a; : \"$${PCSM_SOURCE_URI:?PCSM_SOURCE_URI is required}\"; : \"$${PCSM_TARGET_URI:?PCSM_TARGET_URI is required}\"; export PCSM_LISTEN_HOST=0.0.0.0; export PCSM_PORT=${var.pcsm_metrics_port}; exec /pcsm-entry.sh pcsm"
  ]

  cpus   = tostring(var.pcsm_cpus)
  memory = var.pcsm_memory_mb

  # Selectors are nonsecret operational metadata; URIs stay only in pcsm_env_file.
  labels {
    label = "pcsm.source_kind"
    value = var.pcsm_source_kind
  }

  labels {
    label = "pcsm.source_name"
    value = var.pcsm_source_name
  }

  labels {
    label = "pcsm.target_kind"
    value = var.pcsm_target_kind
  }

  labels {
    label = "pcsm.target_name"
    value = var.pcsm_target_name
  }

  network_mode = "bridge"
  networks_advanced {
    name = docker_network.mongo_network.name
  }

  mounts {
    type      = "bind"
    source    = abspath(var.pcsm_env_file)
    target    = "/run/secrets/pcsm.env"
    read_only = true
  }

  dynamic "mounts" {
    for_each = var.enable_ca ? [module.certificate_authority[0].trust_volume] : []
    content {
      type      = "volume"
      source    = mounts.value
      target    = "/etc/mongodb-tls"
      read_only = true
    }
  }

  healthcheck {
    test         = ["CMD-SHELL", "set -a; . /run/secrets/pcsm.env; set +a; pcsm status >/dev/null"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }

  # The UI creates least-privilege users after the MongoDB modules complete,
  # then restarts PCSM. Do not block the initial apply on PCSM connectivity.
  wait    = false
  restart = "unless-stopped"

  depends_on = [
    module.mongodb_clusters,
    module.mongodb_replsets,
  ]

  lifecycle {
    precondition {
      condition     = trimspace(var.pcsm_env_file) != "" && fileexists(var.pcsm_env_file)
      error_message = "pcsm_env_file must identify an existing host file when enable_pcsm is true."
    }

    precondition {
      condition     = !var.enable_pcsm || contains(keys(var.pmm_servers), var.pcsm_pmm_host)
      error_message = "pcsm_pmm_host must identify a configured PMM Server when PCSM is enabled."
    }

    precondition {
      condition     = var.pcsm_source_kind == var.pcsm_target_kind
      error_message = "PCSM requires cluster-to-cluster or replset-to-replset replication."
    }

    precondition {
      condition     = trimspace(var.pcsm_source_name) != "" && trimspace(var.pcsm_target_name) != "" && trimspace(var.pcsm_source_name) != trimspace(var.pcsm_target_name)
      error_message = "pcsm_source_name and pcsm_target_name must be different non-empty topology names."
    }

    precondition {
      condition     = var.pcsm_source_kind == "cluster" ? contains(keys(var.clusters), trimspace(var.pcsm_source_name)) : contains(keys(var.replsets), trimspace(var.pcsm_source_name))
      error_message = "pcsm_source_name must identify a configured topology of pcsm_source_kind."
    }

    precondition {
      condition     = var.pcsm_target_kind == "cluster" ? contains(keys(var.clusters), trimspace(var.pcsm_target_name)) : contains(keys(var.replsets), trimspace(var.pcsm_target_name))
      error_message = "pcsm_target_name must identify a configured topology of pcsm_target_kind."
    }
  }
}

resource "docker_container" "pcsm_pmm_client" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.prefix
  }

  count    = var.enable_pcsm ? 1 : 0
  name     = "${local.name_prefix}pcsm-pmm-client"
  hostname = "${local.name_prefix}pcsm-pmm-client"
  image    = docker_image.pcsm_pmm_client[0].image_id

  env = [
    "PMM_AGENT_SETUP=1",
    "PMM_AGENT_SETUP_FORCE=1",
    "PMM_AGENT_SETUP_NODE_NAME=${local.name_prefix}pcsm",
    "PMM_AGENT_SETUP_NODE_TYPE=container",
    "PMM_AGENT_SERVER_ADDRESS=${local.name_prefix}${var.pcsm_pmm_host}:${local.pcsm_pmm_port}",
    "PMM_AGENT_SERVER_USERNAME=${local.pcsm_pmm_user}",
    "PMM_AGENT_SERVER_PASSWORD=${local.pcsm_pmm_pwd}",
    "PMM_AGENT_SERVER_INSECURE_TLS=1",
    "PMM_AGENT_CONFIG_FILE=config/pmm-agent.yaml",
    "PMM_AGENT_PRERUN_SCRIPT=pmm-admin status --wait=10s; if ! pmm-admin list | grep -Fq '${local.name_prefix}pcsm-pcsm'; then until pmm-admin add external --service-name=${local.name_prefix}pcsm-pcsm --host=${local.name_prefix}pcsm --listen-port=${var.pcsm_metrics_port} --metrics-path=${var.pcsm_metrics_path} --scheme=${var.pcsm_metrics_scheme}; do sleep 5; done; fi",
  ]

  network_mode = "bridge"
  networks_advanced {
    name = docker_network.mongo_network.name
  }

  depends_on = [
    docker_container.pcsm,
    module.pmm_server,
  ]

  healthcheck {
    test         = ["CMD-SHELL", "pmm-admin status"]
    interval     = "10s"
    timeout      = "10s"
    retries      = 5
    start_period = "30s"
  }

  wait    = false
  restart = "unless-stopped"

  lifecycle {
    replace_triggered_by = [docker_image.pcsm_pmm_client]
  }
}
