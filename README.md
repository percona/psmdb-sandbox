# MongoDB environments with Terraform and Ansible

Create and manage MongoDB environments on AWS, GCP, Azure, CHAOS, or local Docker
containers. Provision local Libvirt/KVM virtual machines for manual configuration
with Ansible.

Use the **PSMDB Sandbox web UI** to configure and manage environments in your
browser, or run Terraform and Ansible directly.

The project supports Percona Server for MongoDB (PSMDB), with MongoDB Community
and Enterprise package options for VM-based deployments. Optional integrations
include Percona Backup for MongoDB (PBM), Percona Monitoring and Management (PMM),
and Percona ClusterSync for MongoDB (PCSM).

## What you can build

- Replica sets and sharded clusters, including multiple topologies in one environment.
- Backup and restore environments using PBM and provider object storage or SeaweedFS.
- Monitoring environments with PMM.
- Cluster synchronization scenarios with PCSM.
- Workload experiments using [plgm](https://github.com/Percona-Lab/percona-load-generator-mongodb) (Percona Load Generator for MongoDB).
- Environments with TLS, audit logging, LDAP authentication, or Vault-backed PSMDB
  encryption at rest.

Feature availability depends on the deployment target, MongoDB distribution, and
version. See the linked guides for configuration and compatibility.

## Deployment targets

| Target | Infrastructure | Software configuration | Web UI |
|---|---|---|---|
| [AWS](terraform/aws/README.md) | Terraform | Ansible | Yes |
| [GCP](terraform/gcp/README.md) | Terraform | Ansible | Yes |
| [Azure](terraform/azure/README.md) | Terraform | Ansible | Yes |
| [CHAOS](terraform/chaos/README.md) | Terraform with the private CHAOS provider | Ansible | Yes |
| [Docker](terraform/docker/README.md) | Terraform | Container configuration through Terraform | Yes |
| [Libvirt/KVM](terraform/libvirt/README.md) | Terraform | Run Ansible manually after provisioning | No |

Docker deployments use Percona MongoDB images. AWS, GCP, Azure, and CHAOS support
PSMDB, MongoDB Community, and MongoDB Enterprise package selections, subject to
package availability.

## Quick start: local Docker with the web UI

### 1. Clone the repository

```bash
git clone https://github.com/percona/mongo_terraform_ansible.git
cd mongo_terraform_ansible
```

### 2. Install prerequisites

If the tools are not already installed:

```bash
./scripts/install-prerequisites.sh --docker --ui
```

The installer supports macOS with an existing Homebrew installation, Debian-family
Linux, and RHEL-family Linux. It installs common controller tools plus Docker and Go.

Start Docker Desktop on macOS. On Linux, ensure Docker is running and your user has
permission to access it; group changes require a new login session.

### 3. Start the web UI

```bash
cd ui-go
UI_REPO_DIR=.. go run .
```

Open **http://127.0.0.1:5001**.

### 4. Create an environment

1. Select **Docker**.
2. Configure a replica set or sharded cluster and select any supporting services.
3. Save the configuration and choose **Deploy**.
4. Follow deployment output in the browser.
5. Use **Hosts & Connections** to find connection commands and service URLs.
6. Choose **Destroy** when finished to remove the environment's resources.

The UI also supports cloud deployments, stop/restart operations, reset operations,
and supported topology expansion. It is written in Go with no third-party Go
dependencies; deployments use the external tools described below.

See the [web UI guide](ui-go/README.md) for provider settings, screenshots,
environment states, and binary builds.

## Prerequisites

Use **Terraform 1.9+** for all targets. VM-based MongoDB installation also requires
Ansible and SSH access. Running the UI from source requires **Go 1.22+**.

Install common controller tools from the repository root:

```bash
./scripts/install-prerequisites.sh
```

Select additional dependencies for your target:

```bash
./scripts/install-prerequisites.sh --aws
./scripts/install-prerequisites.sh --gcp
./scripts/install-prerequisites.sh --azure
./scripts/install-prerequisites.sh --libvirt
```

Add `--ui` to install Go for the web UI. Use `--all` to install every applicable
target dependency, or `--dry-run` to preview installation commands. The installer
requires `sudo` on Linux.

Additional setup:

- **AWS, GCP, and Azure:** configure provider credentials, account permissions, and
  SSH keys. The UI requires the corresponding cloud CLI; manual Terraform workflows
  can use the provider's supported authentication methods.
- **CHAOS:** install the privately distributed `percona/chaos` Terraform provider
  separately and configure provider credentials and SSH access.
- **Libvirt/KVM:** use a Linux host and follow the target guide for virtualization,
  images, and access configuration.

The prerequisite installer does not configure cloud credentials, generate SSH keys,
bootstrap Homebrew, or obtain the private CHAOS provider.

## Deploy without the web UI

Each target directory contains its own configuration examples and deployment
instructions:

- [AWS](terraform/aws/README.md)
- [GCP](terraform/gcp/README.md)
- [Azure](terraform/azure/README.md)
- [CHAOS](terraform/chaos/README.md)
- [Docker](terraform/docker/README.md)
- [Libvirt/KVM](terraform/libvirt/README.md)

For a minimal local Docker replica set, with Docker running, start from the
repository root:

```bash
cd terraform/docker
terraform init
terraform plan -var-file=minimum.tfvars
terraform apply -var-file=minimum.tfvars
```

The checked-in `minimum.tfvars` disables PMM, PBM, SeaweedFS, and LDAP.

Remove that environment with:

```bash
terraform destroy -var-file=minimum.tfvars
```

For AWS, GCP, Azure, and CHAOS, Terraform provisions infrastructure and generates
inventories; run the [Ansible playbooks](ansible/README.md) afterward to install and
configure the software. The web UI orchestrates both steps.

Libvirt provisions base VMs. Prepare an inventory and run Ansible manually.

## Guides

| Topic | Documentation |
|---|---|
| Web UI and environment lifecycle | [UI guide](ui-go/README.md) |
| MongoDB installation and operational playbooks | [Ansible guide](ansible/README.md) |
| Supported topology expansion | [UI expansion workflow](ui-go/README.md#topology-expansion), [Ansible playbooks](ansible/README.md#running), [Docker expansion](terraform/docker/README.md#expanding-existing-docker-topologies) |
| plgm workloads | [Ansible workflow](ansible/README.md#plgm-load-generator), [Docker workflow](terraform/docker/README.md#simulating-a-workload-with-plgm) |
| ClusterSync | [Ansible workflow](ansible/README.md#percona-clustersync), [Docker workflow](terraform/docker/README.md#percona-clustersync-for-mongodb) |
| Vault-backed PSMDB encryption | [Encryption guide](docs/vault-encryption.md) |

## License

Licensed under the [Apache License 2.0](LICENSE).

## Support

This code is not supported by Percona. It is provided solely as a
community-contributed example and is not covered under any Percona services
agreement.
