# Prepare the template for PBM docker image with the MongoDB binary of the version in use (required for physical restore)
locals {
  psmdb_repository   = replace(data.docker_registry_image.psmdb.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
  pbm_repository     = replace(data.docker_registry_image.pbm.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
  base_os_repository = replace(data.docker_registry_image.base_os.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")
  mongot_repository  = replace(data.docker_registry_image.mongot.name, "/(@sha256:[a-f0-9]+|:[^/]+)$/", "")

  # mongod's bundled TCMalloc crashes at startup on Linux 6.19+ unless glibc owns rseq
  # (SERVER-121912). Drop once the PSMDB images ship the TCMalloc fix.
  mongod_env = ["GLIBC_TUNABLES=glibc.pthread.rseq=1"]

  pbm_mongod_image_dockerfile_content = templatefile("${path.module}/pbm-mongod.Dockerfile.tmpl", {
    psmdb_image   = "${local.psmdb_repository}@${data.docker_registry_image.psmdb.sha256_digest}"
    pbm_image     = "${local.pbm_repository}@${data.docker_registry_image.pbm.sha256_digest}"
    base_os_image = "${local.base_os_repository}@${data.docker_registry_image.base_os.sha256_digest}"
  })
}

data "docker_registry_image" "psmdb" {
  name = var.psmdb_image
}

resource "docker_image" "psmdb" {
  platform      = var.docker_platform
  name          = "${local.psmdb_repository}@${data.docker_registry_image.psmdb.sha256_digest}"
  pull_triggers = [data.docker_registry_image.psmdb.sha256_digest]
  keep_locally  = true

  lifecycle {
    precondition {
      condition     = length(regexall(":(9|[1-9][0-9]+)\\.", var.psmdb_image)) == 0 || (!var.enable_pbm && !var.enable_mongot)
      error_message = "PBM and Search compatibility with PSMDB 9.0 has not been verified. Set enable_pbm=false and enable_mongot=false for this release."
    }
  }
}

data "docker_registry_image" "pbm" {
  name = var.pbm_image
}

resource "docker_image" "pbm" {
  platform      = var.docker_platform
  name          = "${local.pbm_repository}@${data.docker_registry_image.pbm.sha256_digest}"
  pull_triggers = [data.docker_registry_image.pbm.sha256_digest]
  keep_locally  = true
}

data "docker_registry_image" "base_os" {
  name = var.base_os_image
}

resource "docker_image" "base_os" {
  platform      = var.docker_platform
  name          = "${local.base_os_repository}@${data.docker_registry_image.base_os.sha256_digest}"
  pull_triggers = [data.docker_registry_image.base_os.sha256_digest]
  keep_locally  = true
}

data "docker_registry_image" "mongot" {
  name = var.mongot_image
}

resource "docker_image" "mongot" {
  platform      = var.docker_platform
  name          = "${local.mongot_repository}@${data.docker_registry_image.mongot.sha256_digest}"
  pull_triggers = [data.docker_registry_image.mongot.sha256_digest]
  keep_locally  = true
}

# Write PBM Dockerfile to disk
resource "local_file" "pbm_mongod_image_dockerfile_content" {
  filename = "${path.module}/${var.rs_name}-${replace(var.pbm_mongod_image, "/", "-")}.Dockerfile"
  content  = local.pbm_mongod_image_dockerfile_content
}

# Build PBM custom Docker image 
resource "docker_image" "pbm_mongod_rs" {
  platform = var.docker_platform
  depends_on = [
    local_file.pbm_mongod_image_dockerfile_content,
    docker_image.psmdb,
    docker_image.pbm,
    docker_image.base_os
  ]
  name = "${var.rs_name}-${var.pbm_mongod_image}"
  triggers = {
    psmdb_digest   = data.docker_registry_image.psmdb.sha256_digest
    pbm_digest     = data.docker_registry_image.pbm.sha256_digest
    base_os_digest = data.docker_registry_image.base_os.sha256_digest
  }
  build {
    platform   = var.docker_platform
    context    = path.module
    dockerfile = "${var.rs_name}-${replace(var.pbm_mongod_image, "/", "-")}.Dockerfile"
  }
}
