# Deploy MongoDB infrastructure with the CHAOS provider

For fresh MongoDB 9.0 package selections and integration compatibility, see
[MongoDB 9.0 deployment notes](../../docs/mongodb-9.0.md).

Creates the following resources:

- VM instances for each MongoDB component (using the `chaos` provider)
- A dedicated VM running [SeaweedFS](https://github.com/seaweedfs/seaweedfs) for S3-compatible backup storage
- Firewall rules per instance
- Ansible inventory
- SSH configuration

Unlike public cloud providers (AWS, GCP, Azure), the CHAOS provider does not offer managed object storage. A SeaweedFS server VM is provisioned instead to provide an S3-compatible backup endpoint for [Percona Backup for MongoDB (PBM)](https://docs.percona.com/percona-backup-mongodb/index.html).

## Prerequisites

1. From the repository root, install Terraform and Ansible:

   ```bash
   ./scripts/install-prerequisites.sh
   ```

2. Ensure the privately distributed `percona/chaos` Terraform provider is available
   in the standard Terraform plugin directory. The installer cannot obtain this provider.

3. Change into this directory

    ```
    cd mongo_terraform_ansible/terraform/chaos
    ```

4. Initialize Terraform

    ```
    terraform init
    ```

## Quick Start

1. Review `variables.tf` and adjust the values you need.

    ```
    vi variables.tf
    ```

2. Run Terraform to create the resources

    ```
    terraform apply
    ```

3. Append the generated SSH configuration locally if desired:

    ```
    cat ssh_config* >> ~/.ssh/config
    ```

4. (Optional) Copy the generated inventories to the [ansible](../../ansible) folder

    ```
    cp inventory* ../../ansible/
    ```

5. Run the Ansible playbooks from [../../ansible](../../ansible) to complete the software installation.

- You can run `terraform output` to see the SeaweedFS endpoint and credentials generated for backup storage

## Connecting

If you copied the generated configuration to ssh_config, no parameters should be needed. Example:

```
    ssh my-cluster-name-mongodb-cfg00
```

## Key Variables

Review these first before deploying:

- **prefix**

    Prefix to be applied to the resources created, change it to avoid collisions with other users environments

- **clusters**

    By default we deploy 1 sharded cluster, but more can be added. Make sure to change the default name to avoid duplicates. The configuration for each cluster can be customized by adding the optional values listed.

- **replsets**

    If you want to provision any replica sets (non-sharded), set this variable. Make sure to change the default name to avoid duplicates.

- **os_image**

    When unset, CHAOS uses Ubuntu 24.04 for infrastructure and most MongoDB topologies. A Community or Enterprise MongoDB 7.0 cluster or replica set instead uses Ubuntu 22.04, because the official 7.0 apt repository has no Ubuntu 24.04 release. A topology's `os_image` overrides this choice; an explicit Ubuntu 24.04 selection for official MongoDB 7.0 is rejected before provisioning.

- **pmm_bare_clients**

    VMs with only a PMM client and no MongoDB, keyed by name, for example `{ client01 = {}, client02 = { os_image = "Rocky Linux 9" } }`. Needs `enable_pmm`. A client without its own `os_image` uses the environment's `os_image`, and `cpu_cores`, `memory_gb` and `volume_size` default to 2, 4 and 20. Each client can set `pmm_client_repo` and `pmm_client_version` and otherwise uses the environment's. They get their own inventory, `<prefix>_inventory_pmm_clients`, which also works without any cluster or replica set. They accept MongoDB connections from the subnet so that PMM Extensions can bootstrap a replica set on them.

- **enable_extensions**

    Set to `true` to run the PMM Extensions side-car on the PMM server VM. It needs `pmm_image` to be PMM Server 3.10 or newer, for example `perconalab/pmm-server:3.10.0-rc`. The side-car image is `extensions_image`. PMM Server's embedded Nomad server is enabled and its port 4647 is opened to the subnet, so the PMM clients on the other VMs can run PMM Extensions tasks.

 - **my_ssh_user**

    Your own SSH user name. This is used to generate an SSH config file for you to login easily.
     SSH key management is handled automatically by the CHAOS platform — no public key injection is needed.

### Minimum `tfvars`

The checked-in [`minimum.tfvars`](./minimum.tfvars) is the smallest standalone
replica-set example. Export `CHAOS_API_TOKEN` before running Terraform and set
`my_ssh_user` to your CHAOS user. SSH key management is handled by CHAOS.

```hcl
prefix        = "myenv"
my_ssh_user   = "your_chaos_username"
clusters      = {}
enable_pmm    = false
enable_seaweedfs  = false

replsets = {
  rs01 = {
    enable_pmm = false
    enable_pbm = false
  }
}
```

Save the example as `minimum.tfvars` or use the checked-in file and pass
`-var-file=minimum.tfvars` to Terraform commands.

- **delete_after_days**

    Number of days before instances are automatically deleted (default: 14). Useful for temporary lab environments.

- **enable_plgm**

    Optional dedicated [plgm](https://github.com/Percona-Lab/percona-load-generator-mongodb) load generator instance. Its Web UI is reached through the SSH tunnel defined in the generated ssh_config (`plgm_local_port`, default `29999`).

- **enable_audit** and **audit_filter**

    Optional per-cluster or per-replset PSMDB audit settings inside `clusters` or `replsets`.

## Expanding Existing Deployments

Supported scale-out changes are additive only:

- Increase `shard_count` to add shards to an existing sharded cluster.
- Increase `data_nodes_per_replset` to add data-bearing members to an existing standalone replica set.
- Add a new sharded cluster or standalone replica set.

After editing variables, run `terraform apply` to create the new instances and regenerate inventory files. For added shards or replica set members, run the matching Ansible scale-out playbook from the repository root:

```bash
ansible-playbook -i terraform/chaos/<prefix>_inventory_<cluster> ansible/add_shard.yml \
  --extra-vars "new_shard_group=shard<N>"

ansible-playbook -i terraform/chaos/<prefix>_inventory_<rs> ansible/add_replset_member.yml \
  --extra-vars "target_replset=<rs>"
```

For entirely new clusters or replica sets, run `ansible/main.yml` against their generated inventory.

Reducing topology size, changing `configsvr_count`, changing `shardsvr_replicas`, and changing arbiter counts are not implemented.

## Backup Storage (SeaweedFS)

Since the CHAOS environment does not feature managed object storage, a dedicated VM running [SeaweedFS](https://github.com/seaweedfs/seaweedfs) is provisioned as an S3-compatible backup endpoint.

The SeaweedFS server:
- Listens on port `8333` (S3 API), `8888` (object browser), and `9333` (Admin UI)
- Uses credentials defined by `seaweedfs_access_key` and `seaweedfs_secret_key`
- Is **installed and configured via Ansible** (`seaweedfs_install.yml`) — the Terraform `user_data` only sets the hostname
- Runs the single-node `weed mini` mode
- Is referenced in the generated Ansible inventory as the `endpointUrl` for PBM with `storage_provider=minio`; PBM keeps using its MinIO driver because SeaweedFS provides an S3-compatible endpoint

To access the SeaweedFS Admin UI, use SSH port forwarding:

```
ssh -L 9333:localhost:9333 <seaweedfs-server-hostname>
```

Then open `http://localhost:8888/buckets/` in your browser to browse buckets and objects. The S3 endpoint is available on port `8333`, and the Admin UI is available on port `9333`.

## Percona ClusterSync

Percona ClusterSync is disabled by default. Set `enable_pcsm=true` to create one dedicated 2-vCPU/4-GB VM in the environment network. Only SSH is allowed inbound; API port `2242` is not exposed. Terraform never receives PCSM connection URIs or passwords. The package version defaults to `pcsm_version="0.9.0"`.

Set source and target kinds to `cluster` or `replset`; they must match and the names must differ. Terraform writes `<prefix>_inventory_pcsm` in addition to the normal topology inventories. Run Ansible for each selected topology, then run `pcsm.yml` against that PCSM inventory. Store the required PCSM URIs and passwords in an owner-only environment file. See the [Ansible ClusterSync instructions](../../ansible/README.md#percona-clustersync) for the complete procedure.

For example, after applying the Terraform configuration, run Ansible for both
selected topologies and create a controller-side environment file with mode
`0600`:

```bash
ansible-playbook -i myenv_inventory_rs-source ../../ansible/main.yml
ansible-playbook -i myenv_inventory_rs-target ../../ansible/main.yml

umask 077
cat > /secure/myenv/pcsm.env <<'EOF'
PCSM_SOURCE_URI=...
PCSM_TARGET_URI=...
PCSM_SOURCE_PASSWORD=...
PCSM_TARGET_PASSWORD=...
EOF

ansible-playbook -i myenv_inventory_pcsm ../../ansible/pcsm.yml \
  -e pcsm_env_file_source=/secure/myenv/pcsm.env
```

### Minimum PCSM tfvars

This minimal example creates two PSMDB replica sets and a PCSM VM. Export `CHAOS_API_TOKEN` before running Terraform and set `my_ssh_user` to your CHAOS user. SSH key management is handled by CHAOS.
Save it as `pcsm.tfvars` and pass `-var-file=pcsm.tfvars` to `terraform plan` and `terraform apply`.

```hcl
prefix      = "myenv"
my_ssh_user = "your_chaos_username"

clusters   = {}
enable_pmm = false

replsets = {
  "rs-source" = { enable_pmm = false, enable_pbm = false }
  "rs-target" = { enable_pmm = false, enable_pbm = false }
}

enable_pcsm      = true
pcsm_source_kind = "replset"
pcsm_source_name = "rs-source"
pcsm_target_kind = "replset"
pcsm_target_name = "rs-target"
```
