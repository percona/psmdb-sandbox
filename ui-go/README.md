# PSMDB Sandbox

A portable, zero-dependency web frontend for **mongo_terraform_ansible** written in Go.

## Overview

The UI lets you configure, deploy, stop, restart, reset, and destroy environments without
editing `.tfvars` files manually. It writes Terraform variable files into the matching
`terraform/<platform>/` directory and streams job output live in the browser.

Supported platforms:

- AWS
- GCP
- Azure
- CHAOS
- Docker

MongoDB 9.0 is available as an explicit Community/Enterprise package selection
for fresh VM environments. PSMDB releases and Docker images depend on published
Percona artifacts. See [MongoDB 9.0 deployment notes](../docs/mongodb-9.0.md) for
integration compatibility and examples. Default versions are retained.

## Requirements

- **Go 1.22+** (install with `./scripts/install-prerequisites.sh --ui` from the repository root)

Optional, depending on the environment type you want to deploy:

- Terraform 1.9+ for all platforms. AWS, GCP, Azure, and CHAOS also need Ansible core 2.14+, controller Python 3.9+, OpenSSH, and the collections in `ansible/requirements.yml` (`./scripts/install-prerequisites.sh`)
- A reachable Docker Engine 20.10+ for Docker environments (`./scripts/install-prerequisites.sh --docker`)
- The corresponding cloud CLI for AWS, GCP, or Azure (`--aws`, `--gcp`, or `--azure`); the UI uses these commands to configure and validate credentials

The installer supports macOS with Homebrew already installed plus Debian- and
RHEL-family Linux. It does not configure provider credentials. See the root
[prerequisites](../README.md#prerequisites) for all target flags and requirements.

## Quick Start

```bash
cd ui-go
UI_REPO_DIR=.. go run .
```

Then open `http://127.0.0.1:5001` in your browser.

To build a binary:

```bash
cd ui-go
go build .
UI_REPO_DIR=/path/to/mongo_terraform_ansible ./psmdb-sandbox
```

## Environment Variables

| Variable      | Default           | Description                                                    |
|---------------|-------------------|----------------------------------------------------------------|
| `PORT`        | `5001`            | TCP port to listen on                                          |
| `UI_HOST`     | `127.0.0.1`       | Bind address; use `0.0.0.0` to listen on all interfaces        |
| `UI_REPO_DIR` | required | Repository root containing `terraform/` and `ansible/` |
| `UI_DATA_DIR` | `./data` | Writable directory for state, settings, jobs, and secrets |

The web UI templates and static assets are embedded in the PSMDB Sandbox binary. The
repository directory is still required because Terraform and Ansible files are executed
from disk.

### Default images

An optional `.env` file in the repository root sets the images the configure page starts
new environments and blocks with, for example to try a release candidate:

```
PMM_SERVER_IMAGE=perconalab/pmm-server:3.10.0-rc
PMM_CLIENT_IMAGE=perconalab/pmm-client:3.10.0-rc
PMM_EXTENSIONS_IMAGE=percona/pmm-extensions:3.10.0-rc1
```

It is read at startup and applies to new environments only; editing an existing environment
keeps the built-in defaults, so saving it does not change an image it never set.

## Screenshots

### Environment list

The environment list shows the platform, topology counts, component version,
location, creation time, and available management actions. **Settings** opens
provider credentials and SSH configuration.

![Environment list](static/readme/environment-list.png)

### Environment detail

The environment detail page provides deployment actions, topology summary,
Hosts & Connections, and links to service web consoles (PMM, LDAP, SeaweedFS, and
the plgm load generator Web UI when enabled).

![Environment detail](static/readme/environment-detail.png)

---

## Environment states

| Status                    | Platform        | Meaning                                                                                                           |
|---------------------------|-----------------|-------------------------------------------------------------------------------------------------------------------|
| **Configured**            | all             | Saved but no infrastructure exists yet.                                                                           |
| **Deploy In Progress**    | all             | Terraform (+ Ansible for cloud) running in the background.                                                        |
| **Running**               | all             | All resources up and healthy. Reached after `Deploy`, `Install`, or `Restart`.                                    |
| **Stopped**               | all             | Resources gracefully stopped (`docker stop` / Ansible stop playbook).                                             |
| **Provision In Progress** | cloud only      | Terraform provisioning infra (no Ansible yet).                                                                    |
| **Provision Success**     | cloud only      | Terraform done; Ansible `Install` step starting automatically.                                                    |
| **Provisioned**           | cloud only      | Infra exists but Ansible has not run yet. Set after `Reset` or configure failure — run `Install` to continue.     |
| **Configure In Progress** | cloud only      | Ansible playbooks running. On success → **Running** (there is no `Configure Success` state).                      |
| **Destroy In Progress**   | all             | `terraform destroy` running.                                                                                      |
| **Deleted**               | all             | All resources were destroyed. The record remains in the list until **Cleanup Deleted** permanently purges it.     |
| **\*\_Failed**            | all             | Any action may fail; the status becomes `<action>_failed`. Re-run the action to retry.                            |

## How it works

1. **Platform selection** – choose AWS, GCP, Azure, CHAOS or Docker.
2. **Configuration editor** – opens in Templates; move freely between Templates,
    Basics, Clusters & replica sets, Resources,
    Optional services, Security, and Advanced. Sections that do not apply to the platform
    are omitted (for example, Docker has no cloud Resources section).
    - **Customize** and advanced-settings panels start collapsed, including when
      editing saved configurations. Collapsing never clears their values.
      Validation can open a panel to reveal a setting that needs attention.
    - Collapsed topology cards summarize the distribution/image, member counts,
      monitoring, backups, Search, and TLS. **Edit** exposes the topology and
      distribution → major version → minor/package version. New clusters and
      replica sets open for editing and require those selections in that order.
      Docker offers PSMDB only, with versions derived from available image tags.
      Environment tags sit beside the topology name; member counts are grouped
      under **Topology**. Audit logging sits alongside the other feature checkboxes.
      **Advanced settings & packages** holds OS images, repository channels,
      agent package pins, custom images, audit filters, and port overrides.
      PMM/PBM major selectors are initialized from cached package versions while
      live metadata loads. Refreshes preserve valid version pins. Empty and failed
      lookups show an explanation and a Retry action instead of blank selectors;
      saving waits for current lookups and rejects enabled components with no
      available packages. PMM testing/experimental and OS-specific lookups never
      substitute Docker release tags for missing packages.
    - **Optional services → Add an optional service** enables the selected service
      and shows its checked control and settings. Uncheck it to disable the service
      and hide its section; select it again to re-enable it. Existing dependency
      confirmations still apply. Selecting PMM clients adds the first client;
      unchecking that service removes its clients. The info tooltip explains this
      and where to find Search and security settings. Search is configured
      per topology in Clusters & replica sets; TLS/CA, encryption, LDAP, and credentials are in Security.
    - CHAOS compute/storage controls appear once in Resources, since they are
      shared by node role across the environment. Network rules and lifetime are
      directly editable, without a redundant Customize toggle.
    - A persistent summary shows MongoDB node totals (including arbiters, config
      servers and routers), hardware selection, services, and relevant warnings.
      Supporting service hosts are not included in the MongoDB node total.
    - The sticky **Save & Configure** action saves every section, including hidden
      settings. Saving does not deploy infrastructure. Validation errors link back
      to the affected setting and open its section when a field can be identified.
    - The editor reuses the existing defaults and service dependency rules. PMM
      agents require a PMM server, PBM agents require backup storage, and TLS
      requires a CA. Dependency changes are still reported by the form.
    - Image tags are fetched live from Docker Hub on startup and cached for 5 minutes.
    - Docker deployments can change the Percona image namespace, for example from
      `percona` to `perconalab`, while still using the same tag dropdowns.
    - Percona package release identifiers (`psmdb-80`, …) are fetched from the Percona
      repository listing on startup.
    - Cloud deployments can select `release`, `testing`, or `experimental` Percona
      repository channels for MongoDB, PBM, and PMM client packages.
    - Each cluster and replica set includes audit plugin controls. Audit is disabled by
      default. Docker environments use the built-in write-only filter for non-system users
      unless you override it.
    - An optional [plgm](https://github.com/Percona-Lab/percona-load-generator-mongodb) load
      generator can be enabled for Docker and cloud environments. The environment page links
      to the plgm Web UI (Docker: `http://localhost:9999`; cloud: `http://127.0.0.1:29999`
      through the SSH tunnel from the generated ssh_config). Workloads are configured in plgm.
3. **Save** – writes `<env_id>.tfvars` inside the corresponding `../terraform/<platform>/`
   directory and records the environment in `environments.json`.
4. **Deploy** – runs `terraform init && terraform apply` (and Ansible for cloud platforms)
   in a background goroutine. Output is streamed live via Server-Sent Events.
   If an existing environment's topology changed, the UI compares the saved desired
   topology with the last successful deploy before running Terraform.
5. **Stop / Restart** – for Docker environments, uses `docker stop` / `docker restart`
   selected by the exact `psmdb-sandbox.environment` container label; unlabeled
   legacy containers use an anchored environment-name match. For cloud environments, runs the Ansible
   `stop.yml` / `restart.yml` playbooks.
6. **Destroy** – runs `terraform destroy`. On success, the environment is marked
   **Deleted** and its Terraform files are removed. Use **Cleanup Deleted** from the
   environments list to permanently remove the record.
7. **Hosts & Connections** – after a successful deploy the environment detail page shows
   every host or container with its IP address, a copy-pasteable connect command
   (`ssh user@host` or `docker exec -it <name> bash`), MongoDB connection strings for
   every replica set and cluster, and clickable **Open** buttons for PMM and SeaweedFS
    Console URLs. All PMM-related containers (server, Grafana renderer, Watchtower,
     and per-node PMM client sidecars) are grouped together under a single **PMM** section.

PCSM logs in the environment page are displayed as plain text, with terminal color
and formatting codes removed. This applies to Docker and cloud environments;
stored ClusterSync passwords remain redacted.

### Starter templates and environment profiles

The **Templates** menu section includes four editable starting
points. Choosing one is optional; configure the environment directly in the other
sections to start without a template or profile.

- **Three-node replica set**: three data-bearing nodes, no arbiters.
- **Two-shard sandbox**: two shards with three data nodes each, three config
  servers, two routers, no arbiters.
- **Benchmark lab**: a three-node replica set and a dedicated plgm load generator,
  with Spot disabled. Review hardware sizing before benchmarking.
- **Migration lab**: two three-node replica sets and a dedicated ClusterSync
  service. Starting replication is a separate action; it can replace target data.

Check **Save this configuration as a reusable profile** to reveal the profile name
and **Save profile** button, then save the validated configuration under a new name.
Profiles are platform-specific, shared by this UI instance, and stored in
`UI_DATA_DIR/environment-profiles.json` with private file permissions and atomic
writes. Duplicate names are rejected rather than silently overwritten. To delete a
profile, open **Settings → Profiles** on the home page, select the profile, click
**Delete profile**, and confirm. Profiles from all platforms are listed with their
platform names, including disabled platforms. Deletion takes effect immediately;
the Settings **Save** button is not required and **Cancel** does not undo it.
Existing environments are not modified. Starter templates cannot be deleted.

Profiles retain topology, versions, hardware, and service choices. A server-side
allowlist excludes credentials, SSH keys and paths, cloud account/project
identities, arbitrary Ansible overrides, existing storage endpoints and details,
custom audit filters, labels, firewall rules, environment prefix, and host ports.
Re-enter those environment-specific settings when using a profile. Existing
backup storage must be selected/configured again if the profile used it.

Selecting a template or saved profile immediately opens a fresh configuration
with a blank environment name, discarding unsaved form changes. Applying a template/profile never
saves, deploys, or modifies an existing environment. Changing the resulting form
does not update the original profile.
The info tooltip beside **Templates** summarizes profile
scope and exclusions; it is available on hover and keyboard focus.

### Configuration editor tests

Run `go test ./...` for the Go suite, rendered JavaScript syntax checks, and
Node-based topology summary tests (Node checks are skipped if Node.js is absent).
For optional Chromium browser coverage, install Playwright outside the repository:

```bash
# Run from ui-go/. Use a temporary directory; no Node dependency is added to the app.
browser_tools=$(mktemp -d)
npm install --prefix "$browser_tools" playwright
"$browser_tools/node_modules/.bin/playwright" install chromium
UI_BROWSER_TESTS=1 NODE_PATH="$browser_tools/node_modules" go test -run '^TestConfigureEditor' -v
```

The browser tests render synthetic fixtures for every platform and intercept all
requests. They check navigation, save-payload parity with the unenhanced form,
dynamic topology cards, explicit version selection, reusable-profile actions
(including deletion in home page Settings),
starter-template navigation, service dependencies, hidden-field validation, and mobile
layout. They do not read, save, or deploy real environments.

To explore the running GCP replica-set template with real, read-only API responses:

```bash
UI_LIVE_SOURCE=1 UI_LIVE_ASSERT=1 NODE_PATH="$browser_tools/node_modules" node tests/configure-editor.live.cjs
```

This targets `http://127.0.0.1:5001/configure/gcp?template=replica-set`. It blocks
all write requests except advisory hardware validation. `UI_LIVE_SOURCE=1`
overlays the current local browser helpers, profile tooltip, and static assets
without restarting the running UI; the rendered defaults and API responses still
come from that server. Backend changes require rebuilding/restarting the UI to
take effect. The test exercises distributions, agent versions and channels,
audit/PBM compatibility, presets, Spot warnings, tooltip focus, and mobile layout.

### State and operation safety

- On SIGINT/SIGTERM, the UI stops admitting jobs, terminates running job process
  groups, and waits up to 15 seconds for job status and environment history to be
  saved. Jobs stopped during shutdown are recorded as `interrupted` in history;
  their environment action is marked failed.
- On startup, abandoned jobs and unfinished environment actions are marked
  interrupted/failed. Recovery never assumes an apply or destroy succeeded,
  deletes infrastructure state, or retries an operation automatically. Inspect
  the logs and infrastructure before retrying. After a hard crash, external
  processes may still be running; ensure they have stopped first. Run only one
  UI instance per data directory.

- Every environment uses its own Terraform backend metadata directory at
  `UI_DATA_DIR/terraform/<platform>/<env_id>/` (`TF_DATA_DIR`). The existing state
  paths, `terraform/<platform>/<env_id>.tfstate`, are unchanged and passed to
  Terraform as absolute paths. UI jobs always use the default Terraform workspace.
- Registry updates are serialized read–modify–write transactions. Registry,
  settings, and generated tfvars files use synced temporary files and atomic
  replacement, with owner-only permissions.
- Only one modifying operation can run per environment, including configuration
  edits, lifecycle jobs, and ClusterSync actions. Conflicting requests return
  HTTP 409. Jobs use an independent configuration snapshot; changing an existing
  environment's platform is not supported. Different environments may run in parallel.
- Losing the browser's log stream does not unlock lifecycle actions. The page
  continues polling the server for completion.
- Removing a definition is allowed only before deployment or after successful
  destruction. If a state file or backup still contains managed resources, or
  cannot be verified, removal is refused. Destroy failed or partially deployed
  environments with **Destroy** before removing them; do not delete their state files.
- Permanently removing a definition also deletes its per-environment Terraform
  provider/module cache. Destroyed definitions kept for redeployment retain it.

Run only one UI process against a given repository/data directory. The registry
transactions and operation locks coordinate requests within that process, not
separate UI processes or manual Terraform/Ansible commands.

**Docker upgrade note:** all managed containers now receive the environment label.
The next **Deploy** of an existing Docker environment may replace containers to add
it, causing downtime. Data volumes remain separate, but back up valuable data and
inspect the Terraform plan before redeploying. Stop, Restart, and Hosts & Connections
continue working with unlabeled containers before that deployment.

## Cloud Provider Credentials

SSH-key uploads (including the platform upload API) are limited to a **1 MiB
multipart request** and validated with `ssh-keygen`. Public uploads must contain
one OpenSSH public key. Private uploads accept OpenSSH keys (including encrypted
OpenSSH keys) and unencrypted PEM keys supported by `ssh-keygen`; passphrase-based
SSH access still requires an appropriately configured SSH agent. Every upload
gets a unique, mode-0600 file under `UI_DATA_DIR/secrets/ssh/`; the original
filename is only a display label. The APIs return an absolute managed path and
never write uploads into Terraform source directories or overwrite earlier keys.

Unencrypted private-key payloads are decoded before acceptance. Encrypted OpenSSH
uploads have their container and public section checked; validating the encrypted
private payload requires the passphrase and is deferred to SSH/your agent.
Ansible readiness probes use the deployment's `ansible/ansible.cfg` and validate
the collection versions resolved by Ansible, including search-path precedence.

Open **Settings** from the environments page and configure credentials for the cloud provider you want to use:

- AWS: access key ID, secret access key, profile, and default region. The UI writes isolated AWS config files under `UI_DATA_DIR/secrets/cloud/aws/` and runs Terraform with `AWS_SHARED_CREDENTIALS_FILE`, `AWS_CONFIG_FILE`, and `AWS_PROFILE`.
- GCP: service account JSON file and project ID. The UI stores the uploaded key under `UI_DATA_DIR/secrets/cloud/gcp/`, uses an isolated `CLOUDSDK_CONFIG`, and runs Terraform with `GOOGLE_APPLICATION_CREDENTIALS`.
- Azure: service principal tenant ID, subscription ID, client ID, and client secret. The UI uses an isolated `AZURE_CONFIG_DIR` and runs Terraform with the matching `ARM_*` environment variables.

Use the provider-specific **Configure** button after entering credentials, then **Test** to validate them. Deploy, Provision, and Destroy validate provider credentials before Terraform runs.

## Compute & Storage (AWS, GCP, Azure)

Machine types, disk sizes and disk types are set once per environment in the
**Compute & Storage** section and apply to every cluster and replica set.

On AWS and GCP the section also offers benchmark tunables: provisioned disk
IOPS and throughput, a separate config server disk type, boot disk type and size,
compact placement, and extra labels/tags. GCP adds the minimum CPU platform,
gVNIC/Tier_1 networking and zone placement. The plgm client machine type is set
in the **Load Generator** section.

A **Hardware Preset** fills these fields with a production-like profile (Atlas-like
M30–M80, IO-heavy, and a latest-generation profile); every field stays editable.
The rules for machine-series and disk compatibility, value ranges and placement
live in `hardware.go`. The form checks them as you type (`POST /api/hardware/validate`)
and the server checks them again on save. Presets are also available from
`GET /api/hardware/presets?platform=aws|gcp`.

## Topology Expansion

The UI supports additive topology changes for deployed environments:

- Increase `shard_count` to add shards to an existing sharded cluster.
- Increase `data_nodes_per_replset` to add data-bearing members to an existing standalone replica set.
- Add a new sharded cluster or standalone replica set to an existing environment.

For cloud platforms, Deploy runs `terraform apply` first, then runs the matching Ansible scale-out playbook:

- `ansible/add_shard.yml` for each newly added shard.
- `ansible/add_replset_member.yml` for each expanded standalone replica set.
- `ansible/main.yml` for entirely new clusters or replica sets.

For Docker, Deploy runs Terraform only; the Docker Terraform modules run the supported MongoDB topology changes through their `null_resource` provisioners.

The UI refuses unsupported changes before Terraform runs:

- Reducing `shard_count` or `data_nodes_per_replset`.
- Changing `configsvr_count` after deployment.
- Changing `shardsvr_replicas` on existing shards.
- Changing `arbiters_per_replset` on existing sharded clusters or standalone replica sets.

Use **Deploy** for topology expansion. **Provision** is intentionally refused for topology expansion because it would only create infrastructure and would not run the required MongoDB reconfiguration playbook.

## Security note

This tool is intended for **local use only** (it binds to `127.0.0.1:5001` by default).
Do not expose it to the public internet without adding proper authentication.
