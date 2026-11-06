variable "tls_ca_volume" {
  type    = string
  default = ""
}
variable "tls_signer_image" {
  type    = string
  default = ""
}

locals {
  tls_nodes = merge(
    { for k, m in local.shard_members : k => "${var.cluster_name}-${var.shardsvr_tag}0${m.shard_index}svr${m.replica_index}" },
    { for k, m in local.arbiter_members : k => "${var.cluster_name}-${var.shardsvr_tag}0${m.shard_index}arb${m.arbiter_index}" },
    { for k, m in local.cfg_members : k => "${var.cluster_name}-${var.configsvr_tag}0${m}" },
    { for k, m in local.mongos_members : k => "${var.cluster_name}-${var.mongos_tag}0${m}" }
  )
  mongo_auth_flags = var.use_tls ? [
    "--clusterAuthMode", "x509", "--tlsMode", "preferTLS",
    "--tlsCertificateKeyFile", "/etc/mongodb-tls/server.pem",
    "--tlsCAFile", "/etc/mongodb-tls/test-ca.pem", "--tlsAllowConnectionsWithoutCertificates"
  ] : ["--keyFile", "${var.keyfile_path}/${var.keyfile_name}"]
  mongod_auth_flags = concat(local.mongo_auth_flags, var.use_tls ? ["--auth"] : [])
  mongo_tls_flags   = var.use_tls ? "--tls --tlsCAFile /etc/mongodb-tls/test-ca.pem --tlsCertificateKeyFile /etc/mongodb-tls/client.pem" : ""
  pbm_tls_query     = var.use_tls ? "/?tls=true&tlsCAFile=/etc/mongodb-tls/test-ca.pem&tlsCertificateKeyFile=/etc/mongodb-tls/client.pem" : ""
  pmm_tls_flags     = var.use_tls ? " --tls --tls-ca-file=/etc/mongodb-tls/test-ca.pem --tls-certificate-key-file=/etc/mongodb-tls/client.pem" : ""
}

module "tls_certificate" {
  for_each       = var.use_tls ? local.tls_nodes : {}
  source         = "../tls_certificate"
  name           = each.value
  environment_id = var.environment_id
  ca_volume      = var.tls_ca_volume
  image          = var.tls_signer_image
  uid            = var.uid
  dns_names      = distinct([each.value, "${each.value}${var.domain_name != "" ? ".${var.domain_name}" : ""}"])
}

resource "terraform_data" "tls_data_mode" {
  for_each         = merge(docker_volume.shard_volume, docker_volume.cfg_volume, docker_volume.arb_volume)
  triggers_replace = [var.use_tls, each.value.id]
  provisioner "local-exec" {
    command = "python3 \"${abspath(path.module)}/../../../../scripts/docker-tls.py\" check-data"
    environment = {
      TLS_IMAGE   = docker_image.psmdb.image_id
      TLS_VOLUME  = each.value.name
      TLS_ENABLED = tostring(var.use_tls)
      TLS_UID     = tostring(var.uid)
    }
  }
}
