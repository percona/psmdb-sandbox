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
    { for k, m in local.replset_members : k => "${var.rs_name}-${var.replset_tag}${m.member_index}" },
    { for k, m in local.arbiter_members : k => "${var.rs_name}-${var.arbiter_tag}${m.arbiter_index}" }
  )
  mongo_auth_flags = var.use_tls ? [
    "--auth", "--clusterAuthMode", "x509", "--tlsMode", "preferTLS",
    "--tlsCertificateKeyFile", "/etc/mongodb-tls/server.pem",
    "--tlsCAFile", "/etc/mongodb-tls/test-ca.pem", "--tlsAllowConnectionsWithoutCertificates"
  ] : ["--keyFile", "${var.keyfile_path}/${var.keyfile_name}"]
  mongo_tls_flags = var.use_tls ? "--tls --tlsCAFile /etc/mongodb-tls/test-ca.pem --tlsCertificateKeyFile /etc/mongodb-tls/client.pem" : ""
  pbm_tls_query   = var.use_tls ? "/?tls=true&tlsCAFile=/etc/mongodb-tls/test-ca.pem&tlsCertificateKeyFile=/etc/mongodb-tls/client.pem" : ""
  pmm_tls_flags   = var.use_tls ? " --tls --tls-ca-file=/etc/mongodb-tls/test-ca.pem --tls-certificate-key-file=/etc/mongodb-tls/client.pem" : ""
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
  for_each         = merge(docker_volume.rs_volume, docker_volume.arb_volume)
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
