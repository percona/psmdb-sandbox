resource "docker_container" "pmm_cfg" {
  labels {
    label = "psmdb-sandbox.environment"
    value = var.environment_id
  }

  for_each     = var.enable_pmm ? local.cfg_members : {}
  name         = "${var.cluster_name}-${var.configsvr_tag}0${each.value}-${var.pmm_client_container_suffix}"
  image        = docker_image.pmm_client.image_id
  env          = ["PMM_AGENT_SETUP=1", "PMM_AGENT_SETUP_FORCE=1", "PMM_AGENT_SETUP_NODE_NAME=${var.cluster_name}-${var.configsvr_tag}0${each.value}", "PMM_AGENT_SETUP_NODE_TYPE=container", "PMM_AGENT_SERVER_ADDRESS=${var.pmm_host}:${var.pmm_port}", "PMM_AGENT_SERVER_USERNAME=${var.pmm_server_user}", "PMM_AGENT_SERVER_PASSWORD=${var.pmm_server_pwd}", "PMM_AGENT_SERVER_INSECURE_TLS=1", "PMM_AGENT_CONFIG_FILE=config/pmm-agent.yaml", "PMM_AGENT_PRERUN_SCRIPT=pmm-admin status --wait=10s; pmm-admin add mongodb --environment=${var.env_tag} --cluster=${var.cluster_name} --username=${var.mongodb_pmm_user} --password=${var.mongodb_pmm_password} --host=${var.cluster_name}-${var.configsvr_tag}0${each.value} --port=${var.configsvr_port} --service-name=${var.cluster_name}-${var.configsvr_tag}0${each.value}-mongodb --skip-connection-check --tls-skip-verify --enable-all-collectors"]
  network_mode = "bridge"
  networks_advanced {
    name = var.network_name
  }
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
    replace_triggered_by = [docker_image.pmm_client]
  }
}
