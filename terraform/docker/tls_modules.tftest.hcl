mock_provider "docker" {
  mock_data "docker_registry_image" {
    defaults = {
      sha256_digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  }
}
mock_provider "local" {}
mock_provider "null" {}

run "replica_set_tls_wiring" {
  command = plan
  module { source = "./modules/mongodb_replset" }
  variables {
    use_tls           = true
    tls_ca_volume     = "test-ca"
    tls_signer_image  = "test-signer"
    enable_pmm        = true
    enable_pbm        = true
    vault_encryption  = true
    vault_credentials = "vault-credentials"
  }
  assert {
    condition = alltrue([for node in concat(values(docker_container.rs), values(docker_container.arbiter)) :
      contains(node.command, "x509") && contains(node.command, "preferTLS") && contains(node.command, "--auth") && !contains(node.command, "--keyFile") &&
      anytrue([for m in node.mounts : m.target == "/etc/mongodb-tls" && m.read_only]) &&
      strcontains(node.healthcheck[0].test[1], "--tlsCAFile")
    ])
    error_message = "All replica-set members, including arbiters, require verified TLS and X.509 member authentication."
  }
  assert {
    condition = alltrue([for agent in values(docker_container.pbm_rs) :
      anytrue([for m in agent.mounts : m.target == "/etc/mongodb-tls" && m.read_only]) &&
      anytrue([for m in agent.mounts : m.target == "/etc/mongodb-vault" && m.read_only]) &&
      anytrue([for e in agent.env : strcontains(e, "tlsCAFile=/etc/mongodb-tls/test-ca.pem")])
    ])
    error_message = "PBM physical restore needs TLS and Vault files at the mongod paths."
  }
  assert {
    condition = alltrue([for agent in concat(values(docker_container.pmm_rs), values(docker_container.pmm_arb)) :
      anytrue([for m in agent.mounts : m.target == "/etc/mongodb-tls" && endswith(m.source, "-tls-client")]) &&
      anytrue([for e in agent.env : strcontains(e, "--tls-ca-file=/etc/mongodb-tls/test-ca.pem")]) &&
      alltrue([for e in agent.env : !strcontains(e, "--tls-skip-verify")])
    ])
    error_message = "PMM MongoDB connections must verify the sandbox CA."
  }
}

run "replica_set_plaintext_wiring" {
  command = plan
  module { source = "./modules/mongodb_replset" }
  assert {
    condition = alltrue([for node in concat(values(docker_container.rs), values(docker_container.arbiter)) :
      contains(node.command, "--keyFile") && !contains(node.command, "--tlsMode") &&
      alltrue([for m in node.mounts : m.target != "/etc/mongodb-tls"])
    ])
    error_message = "Default replica sets must retain keyfile authentication without TLS mounts."
  }
}

run "cluster_tls_wiring" {
  command = plan
  module { source = "./modules/mongodb_cluster" }
  variables {
    use_tls          = true
    tls_ca_volume    = "test-ca"
    tls_signer_image = "test-signer"
    enable_pmm       = true
    enable_pbm       = true
  }
  assert {
    condition = alltrue([for node in concat(values(docker_container.shard), values(docker_container.cfg), values(docker_container.arbiter), values(docker_container.mongos)) :
      contains(node.command, "x509") && contains(node.command, "preferTLS") && !contains(node.command, "--keyFile") &&
      anytrue([for m in node.mounts : m.target == "/etc/mongodb-tls" && m.read_only]) &&
      strcontains(node.healthcheck[0].test[1], "--tlsCAFile")
    ])
    error_message = "Every sharded-cluster role must use verified TLS and X.509 member authentication."
  }
  assert {
    condition = alltrue([for agent in concat(values(docker_container.pbm_shard), values(docker_container.pbm_cfg), docker_container.pbm_cli) :
      anytrue([for m in agent.mounts : m.target == "/etc/mongodb-tls" && m.read_only]) &&
      anytrue([for e in agent.env : strcontains(e, "tlsCAFile=/etc/mongodb-tls/test-ca.pem")])
    ])
    error_message = "Every PBM agent and CLI needs verified TLS."
  }
  assert {
    condition = alltrue([for agent in concat(values(docker_container.pmm_shard), values(docker_container.pmm_cfg), values(docker_container.pmm_arb), values(docker_container.pmm_mongos)) :
      anytrue([for m in agent.mounts : m.target == "/etc/mongodb-tls" && endswith(m.source, "-tls-client")]) &&
      anytrue([for e in agent.env : strcontains(e, "--tls-ca-file=/etc/mongodb-tls/test-ca.pem")]) &&
      alltrue([for e in agent.env : !strcontains(e, "--tls-skip-verify")])
    ])
    error_message = "Every PMM MongoDB exporter must verify the sandbox CA."
  }
}

run "cluster_plaintext_wiring" {
  command = plan
  module { source = "./modules/mongodb_cluster" }
  assert {
    condition = alltrue([for node in concat(values(docker_container.shard), values(docker_container.cfg), values(docker_container.arbiter), values(docker_container.mongos)) :
      contains(node.command, "--keyFile") && !contains(node.command, "--tlsMode")
    ])
    error_message = "Default sharded clusters must retain keyfile authentication."
  }
}
