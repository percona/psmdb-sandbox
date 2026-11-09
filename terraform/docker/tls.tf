variable "enable_ca" {
  type        = bool
  default     = false
  description = "Provision an environment-scoped offline certificate signer. Required by TLS-enabled topologies."
}

variable "ca_placement" {
  type        = string
  default     = "dedicated"
  description = "Docker supports dedicated offline signing only; no PMM container modifications are required."
  validation {
    condition     = var.ca_placement == "dedicated"
    error_message = "Docker uses a dedicated offline signing container; PMM CA placement is not supported."
  }
}

resource "terraform_data" "tls_validation" {
  lifecycle {
    precondition {
      condition     = alltrue(concat([for t in values(var.clusters) : !t.use_tls || var.enable_ca], [for t in values(var.replsets) : !t.use_tls || var.enable_ca]))
      error_message = "Provision a certificate authority (enable_ca=true) before enabling TLS."
    }
    precondition {
      condition     = alltrue(concat([for t in values(var.clusters) : !t.use_tls || !t.enable_mongot], [for t in values(var.replsets) : !t.use_tls || !t.enable_mongot]))
      error_message = "Search/Vector Search cannot be used with managed TLS yet."
    }
  }
}

module "certificate_authority" {
  docker_platform = var.docker_platform
  count           = var.enable_ca ? 1 : 0
  source          = "./modules/certificate_authority"
  name            = "${local.name_prefix}mongodb"
  environment_id  = var.prefix
  depends_on      = [terraform_data.tls_validation]
}
