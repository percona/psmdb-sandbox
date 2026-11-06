mock_provider "docker" {
  mock_data "docker_registry_image" {
    defaults = {
      sha256_digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  }
}
mock_provider "local" {}
mock_provider "null" {}
mock_provider "http" {
  mock_data "http" {
    defaults = {
      status_code   = 200
      response_body = "{\"tag_name\":\"v1.0.0\",\"assets\":[{\"name\":\"plgm-linux-amd64.tar.gz\",\"digest\":\"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}]}"
    }
  }
}

variables {
  prefix            = "tls-test"
  enable_ca         = true
  pmm_servers       = {}
  seaweedfs_servers = {}
  ldap_servers      = {}
  clusters          = {}
  replsets = {
    secure = { use_tls = true, enable_pmm = true, enable_pbm = true }
    plain  = { use_tls = false, enable_pmm = false, enable_pbm = false, replset_port = 29017 }
  }
}

run "mixed_replica_sets" {
  command = plan
  assert {
    condition     = length(module.certificate_authority) == 1
    error_message = "TLS topologies must share one environment CA."
  }
}

run "sharded_cluster" {
  command = plan
  variables {
    replsets = {}
    clusters = { secure = { use_tls = true, enable_pmm = true, enable_pbm = true } }
  }
  assert {
    condition     = length(module.mongodb_clusters) == 1
    error_message = "TLS must support sharded clusters."
  }
}

run "tls_requires_ca" {
  command = plan
  variables {
    enable_ca = false
  }
  expect_failures = [terraform_data.tls_validation]
}

run "tls_rejects_search" {
  command = plan
  variables {
    replsets = { secure = { use_tls = true, enable_mongot = true } }
  }
  expect_failures = [terraform_data.tls_validation]
}

run "ca_only" {
  command = plan
  variables {
    replsets = {}
  }
  assert {
    condition     = length(module.certificate_authority) == 1
    error_message = "The CA can be provisioned before enabling TLS."
  }
}

run "plaintext_default" {
  command = plan
  variables {
    enable_ca = false
    replsets  = { plain = { enable_pmm = false, enable_pbm = false } }
  }
  assert {
    condition     = length(module.certificate_authority) == 0
    error_message = "Plaintext Docker environments must not create a CA by default."
  }
}

run "clustersync_trust_only" {
  command = plan
  variables {
    enable_pcsm      = true
    pcsm_env_file    = "tls.tftest.hcl"
    pmm_servers      = { pmm-server = {} }
    pcsm_source_kind = "replset"
    pcsm_source_name = "secure"
    pcsm_target_kind = "replset"
    pcsm_target_name = "plain"
  }
  assert {
    condition = anytrue([for m in docker_container.pcsm[0].mounts :
      m.target == "/etc/mongodb-tls" && m.read_only && endswith(m.source, "-ca-trust")
    ])
    error_message = "ClusterSync must receive only the public environment trust bundle."
  }
}

run "load_generator_client_only" {
  command = plan
  variables {
    enable_plgm = true
  }
  assert {
    condition = anytrue([for m in docker_container.plgm[0].mounts :
      m.target == "/etc/mongodb-tls" && m.read_only && endswith(m.source, "-plgm-tls-client")
    ])
    error_message = "The load generator needs its own client identity, never a member private key."
  }
}
