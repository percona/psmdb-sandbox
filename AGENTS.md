# Project Guide

## Architecture

- `ui-go/`: Go web UI and orchestrator. Writes tfvars, runs Terraform and Ansible jobs, tracks environments.
- `terraform/<platform>/`: infrastructure per platform: `docker`, `aws`, `gcp`, `azure`, `libvirt`, `chaos`. Shared code in `terraform/modules/`.
- `ansible/`: configures non-Docker hosts. Playbooks at the top level, mongod/mongos config in `templates/`, defaults in `group_vars/`.
- State lives in files on disk:
  - `ui-go/data/environments.json`: environment registry
  - `terraform/<platform>/<env>.tfstate`, `<env>.tfvars`: per-environment infrastructure
  - `terraform/<platform>/<env>_inventory_<name>`, `<env>_ssh_config_<name>`: generated per replset/cluster
  - `ui-go/data/secrets/`: SSH keys, cloud credentials, Vault material. Never commit, print, or copy these.

## Platforms and distributions

- Docker runs **PSMDB only** and configures mongod with command-line flags in `terraform/docker/modules/*/*.tf`; it does not use the Ansible templates.
- Cloud/VM platforms support `mongodb_distribution` = `psmdb`, `community`, or `enterprise` (see `mongodb_distribution_effective` in `ansible/group_vars/all.yml`).
- PSMDB-only features include profiler `rateLimit`, encryption at rest, Vault, and physical PBM backups. Community also lacks audit logging.
- Any template branch on `mongodb_distribution_effective == 'psmdb'` needs an equivalent community/enterprise branch, or an explicit reason not to have one.
- A mongod setting change must be made in both the Ansible templates (`mongod-replicaset`, `mongod-sharding`, `mongod-cfgserver`, and `mongos` where relevant) and the Docker module flags.

## Conventions

- Follow existing code patterns.
- Keep functions small and focused.
- Avoid unnecessary dependencies.
- Never rewrite unrelated code.
- Make defaults overridable through `group_vars` rather than hardcoding values in templates.

## Testing

Run the tests relevant to the change:

- Go UI: `cd ui-go && go test ./...`
- Ansible (from `ansible/`):
  - `ansible-playbook -i localhost, tests/mongodb_versions.yml`
  - `ansible-playbook -i tests/mongodb_inventory.ini tests/mongodb_inventory.yml`
  - `ansible-playbook -i tests/mongot_inventory.ini tests/mongot_templates.yml`
  - `ansible-playbook -i localhost, tests/vault_templates.yml`
  - Judge results by `PLAY RECAP` (`failed=0`). `fatal` lines inside `rescued` blocks are expected negative cases.
- Scripts: `python3 -m unittest discover scripts/tests`
- Terraform: `terraform fmt -check` and `terraform validate` in the affected platform directory.
- For template changes, render the output for every affected distribution and check it.

## Live environments

- Environments listed in `ui-go/data/environments.json` may be running real workloads or benchmarks. Do not apply config, restart services, or run Terraform/Ansible against them without the user's explicit approval.
- Preview first: `ansible ... --playbook-dir . -m template ... --check --diff` (run from `ansible/` so `group_vars` load), or `ansible-playbook --check --diff`.
- `ansible/restart.yml` is **not** rolling: it restarts every mongod at once, plus pbm-agent and pmm-agent. For live replica sets, restart secondaries one at a time (wait for SECONDARY), then `rs.stepDown()` the primary and restart it.
- After a rolling restart the primary may no longer be the host marked `mongodb_primary=True` in the inventory.

## Workflow

- Inspect existing implementations before modifying them.
- Skip generated or vendored files (`.terraform/`, `*.tfstate`, `ui-go/data/`) unless the task needs them.
- Verify changes instead of assuming they work.
- Keep documentation updated when architecture or defaults change (`README.md`, `ansible/README.md`, `docs/`).
