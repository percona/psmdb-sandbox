# Plan-only tests: no Docker daemon, registry access, builds, or provisioners.
mock_provider "docker" {
  mock_data "docker_registry_image" {
    defaults = {
      sha256_digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  }
}
mock_provider "local" {}
mock_provider "null" {}
mock_provider "random" {}
mock_provider "http" {}

run "pmm_native" {
  command = plan
  module {
    source = "./modules/pmm_server"
  }
  assert {
    condition     = alltrue([for image in [docker_image.pmm, docker_image.renderer] : image.platform == null || image.platform == ""])
    error_message = "PMM images must leave platform selection to Docker by default."
  }
  assert {
    condition     = docker_image.watchtower.platform == "linux/amd64"
    error_message = "Watchtower must retain its amd64 default until Percona publishes an ARM64 image."
  }
}

run "pmm_arm64" {
  command = plan
  module {
    source = "./modules/pmm_server"
  }
  variables {
    docker_platform   = "linux/arm64"
    enable_extensions = true
  }
  assert {
    condition     = alltrue([for image in [docker_image.pmm, docker_image.watchtower, docker_image.renderer, docker_image.extensions[0]] : image.platform == "linux/arm64"])
    error_message = "All PMM images, including extensions, must honor the override."
  }
}

run "replset_native" {
  command = plan
  module {
    source = "./modules/mongodb_replset"
  }
  assert {
    condition     = alltrue([for image in [docker_image.psmdb, docker_image.pbm, docker_image.base_os, docker_image.mongot, docker_image.pmm_client, docker_image.pbm_mongod_rs] : image.platform == null || image.platform == ""])
    error_message = "Replica-set pulls must default to native platform selection."
  }
  assert {
    condition     = one(docker_image.pbm_mongod_rs.build).platform == null || one(docker_image.pbm_mongod_rs.build).platform == ""
    error_message = "The PBM build must also default to native platform selection."
  }
}

run "replset_amd64" {
  command = plan
  module {
    source = "./modules/mongodb_replset"
  }
  variables {
    docker_platform = "linux/amd64"
  }
  assert {
    condition     = alltrue([for image in [docker_image.psmdb, docker_image.pbm, docker_image.base_os, docker_image.mongot, docker_image.pmm_client, docker_image.pbm_mongod_rs] : image.platform == "linux/amd64"])
    error_message = "Replica-set pulls must honor the override."
  }
  assert {
    condition     = one(docker_image.pbm_mongod_rs.build).platform == "linux/amd64"
    error_message = "The PBM multi-stage build must use the same platform as its pulls."
  }
  assert {
    condition     = strcontains(local.pbm_mongod_image_dockerfile_content, "@${data.docker_registry_image.psmdb.sha256_digest}") && strcontains(local.pbm_mongod_image_dockerfile_content, "@${data.docker_registry_image.pbm.sha256_digest}") && strcontains(local.pbm_mongod_image_dockerfile_content, "@${data.docker_registry_image.base_os.sha256_digest}")
    error_message = "PBM build inputs must remain pinned by registry digest."
  }
}

run "cluster_arm64" {
  command = plan
  module {
    source = "./modules/mongodb_cluster"
  }
  variables {
    docker_platform = "linux/arm64"
  }
  assert {
    condition     = alltrue([for image in [docker_image.psmdb, docker_image.pbm, docker_image.base_os, docker_image.mongot, docker_image.pmm_client, docker_image.pbm_mongod] : image.platform == "linux/arm64"])
    error_message = "Sharded-cluster pulls must honor the override."
  }
  assert {
    condition     = one(docker_image.pbm_mongod.build).platform == "linux/arm64"
    error_message = "The sharded-cluster PBM build must honor the override."
  }
}

run "signer_arm64" {
  command = plan
  module {
    source = "./modules/certificate_authority"
  }
  variables {
    name            = "platform-test"
    environment_id  = "platform-test"
    docker_platform = "linux/arm64"
  }
  assert {
    condition     = docker_image.signer.platform == "linux/arm64" && one(docker_image.signer.build).platform == "linux/arm64"
    error_message = "The TLS signer must honor the override for both pulls and builds."
  }
}

run "bare_client_amd64" {
  command = plan
  module {
    source = "./modules/pmm_bare_client"
  }
  variables {
    client_name     = "platform-test"
    pmm_host        = "pmm-test"
    docker_platform = "linux/amd64"
  }
  assert {
    condition     = docker_image.client.platform == "linux/amd64" && one(docker_image.client.build).platform == "linux/amd64"
    error_message = "Bare-client builds must honor the override."
  }
}

run "root_native" {
  command = plan
  variables {
    clusters          = {}
    replsets          = {}
    pmm_servers       = {}
    seaweedfs_servers = {}
    ldap_servers      = {}
    enable_pcsm       = false
  }
  assert {
    condition     = var.docker_platform == null
    error_message = "The root module must default to native image selection."
  }
}

run "root_arm64_with_plgm_exception" {
  command = plan
  variables {
    docker_platform   = "linux/arm64"
    clusters          = {}
    replsets          = {}
    pmm_servers       = {}
    seaweedfs_servers = {}
    ldap_servers      = {}
    enable_plgm       = true
  }
  override_data {
    target = data.http.plgm_latest_release[0]
    values = {
      status_code = 200
      response_body = jsonencode({
        tag_name = "v0.0.0"
        assets = [{
          name   = "plgm-linux-amd64.tar.gz"
          digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        }]
      })
    }
  }
  assert {
    condition     = docker_image.plgm_os[0].platform == "linux/amd64" && one(docker_image.plgm[0].build).platform == "linux/amd64"
    error_message = "plgm must retain its amd64-only binary exception."
  }
}

run "reject_non_linux_platform" {
  command = plan
  variables {
    docker_platform   = "darwin/arm64"
    clusters          = {}
    replsets          = {}
    pmm_servers       = {}
    seaweedfs_servers = {}
    ldap_servers      = {}
  }
  expect_failures = [var.docker_platform]
}
