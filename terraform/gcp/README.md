# Deploy MongoDB infrastructure on Google Cloud

For fresh MongoDB 9.0 package selections and integration compatibility, see
[MongoDB 9.0 deployment notes](../../docs/mongodb-9.0.md).

This Terraform module creates:

- a custom VPC, regional subnet, and firewall rules
- Compute Engine instances and persistent disks for MongoDB and optional supporting services
- a Cloud Storage backup bucket and lifecycle rule
- a PBM service account, HMAC key, and bucket IAM binding
- generated Ansible inventory and SSH configuration files

## Prerequisites

- Terraform 1.9 or newer
- Ansible and OpenSSH
- an existing SSH key pair
- a Google Cloud project with billing enabled
- a project administrator who can enable APIs, create service accounts and keys,
  and grant project IAM roles

Install the local tools and Google Cloud CLI from the repository root:

```bash
./scripts/install-prerequisites.sh --gcp
```

## Create the GCP deployment service account

Run the following bootstrap commands as a project administrator. Replace the
project ID and choose a unique service-account name if necessary.

1. Select the project and enable the APIs used by the Terraform resources:

```bash
export PROJECT_ID=my-gcp-project
export DEPLOY_SA=mongodb-terraform-deployer

gcloud config set project "$PROJECT_ID"
gcloud services enable \
  compute.googleapis.com \
  storage.googleapis.com \
  iam.googleapis.com \
  --project "$PROJECT_ID"
```

2. Create the deployment service account:

```bash
gcloud iam service-accounts create "$DEPLOY_SA" \
  --display-name="MongoDB Terraform deployer" \
  --project "$PROJECT_ID"

export DEPLOY_SA_EMAIL="${DEPLOY_SA}@${PROJECT_ID}.iam.gserviceaccount.com"
```

3. Grant the project roles required by the resources in this module:

```bash
for role in \
  roles/browser \
  roles/compute.admin \
  roles/storage.admin \
  roles/iam.serviceAccountAdmin \
  roles/storage.hmacKeyAdmin
do
  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:${DEPLOY_SA_EMAIL}" \
    --role="$role"
done
```

These roles let Terraform inspect the project; create networks, firewalls,
instances, and disks; create the backup bucket and IAM binding; and create the
separate PBM service account and HMAC key. If organizational policy prohibits
service-account keys or requires custom roles, use an approved workload identity
with equivalent permissions for manual Terraform runs. The Web UI currently
requires a service-account JSON key.

4. Create a JSON key in a protected location and authenticate:

```bash
gcloud iam service-accounts keys create "$HOME/.config/gcloud/mongodb-terraform.json" \
  --iam-account="$DEPLOY_SA_EMAIL" \
  --project "$PROJECT_ID"

export GOOGLE_APPLICATION_CREDENTIALS="$HOME/.config/gcloud/mongodb-terraform.json"
gcloud auth activate-service-account "$DEPLOY_SA_EMAIL" \
  --key-file="$GOOGLE_APPLICATION_CREDENTIALS" \
  --project="$PROJECT_ID"
gcloud auth list --filter=status:ACTIVE
gcloud auth print-access-token >/dev/null
```

For the Web UI, upload this JSON key and enter `PROJECT_ID` in **Settings**. The
UI stores the key under `UI_DATA_DIR/secrets/cloud/gcp/`, uses an isolated Cloud SDK
configuration, and sets `GOOGLE_APPLICATION_CREDENTIALS` for Terraform.

For an interactive manual deployment, Application Default Credentials also work:

```bash
gcloud auth application-default login
```

## Configure the deployment

Change into the GCP Terraform directory:

```bash
cd terraform/gcp
```

Review `variables.tf`, then put overrides in `terraform.tfvars` or another tfvars
file. At minimum, review:

- `project_id`, `prefix`, `region`, `clusters`, and `replsets`
- `gce_ssh_users`, `ssh_private_key_path`, and `my_ssh_user`
- machine types, image, disk sizes, and `use_spot_instances`
- backup bucket naming and retention
- `source_ranges` for inbound access
- optional PMM, CA/TLS, LDAP, and plgm load generator settings

### Minimum `tfvars`

The checked-in [`minimum.tfvars`](./minimum.tfvars) is the smallest standalone
replica-set example. The SSH user must match a key in `gce_ssh_users`. Supply
GCP credentials through Application Default Credentials or the provider
environment, not this file.

```hcl
project_id           = "my-gcp-project"
prefix               = "myenv"
my_ssh_user          = "ubuntu"
gce_ssh_users        = { ubuntu = "/absolute/path/to/id_ed25519.pub" }
ssh_private_key_path = "/absolute/path/to/id_ed25519"

clusters   = {}
enable_pmm = false

replsets = {
  rs01 = {
    enable_pmm = false
    enable_pbm = false
  }
}
```

Save the example as `minimum.tfvars` or use the checked-in file and pass
`-var-file=minimum.tfvars` to Terraform commands.

Resource names must be unique in the project. The VPC, subnet and shared
firewall rules are named after `prefix`, so use a distinct `prefix` (and
cluster/replica set names) for each environment deployed in the same project.

### Hardware for benchmarks

All settings are global to the deployment (every cluster and replica set) and
optional; unset values keep the defaults. Boot disk, CPU platform, NIC and
placement settings apply to MongoDB nodes and the plgm client only.

| Variable | Default | Notes |
|---|---|---|
| `data_disk_type` | `pd-standard` | Also `pd-balanced`, `pd-ssd`, `pd-extreme`, `hyperdisk-balanced`, `hyperdisk-extreme`, `hyperdisk-throughput` |
| `data_disk_iops` | unset | `pd-extreme`, `hyperdisk-balanced`, `hyperdisk-extreme` only |
| `data_disk_throughput` | unset | MiB/s; `hyperdisk-balanced`, `hyperdisk-throughput` only |
| `configsvr_disk_type` | `data_disk_type` | IOPS/throughput only apply when it equals `data_disk_type` |
| `boot_disk_type`, `boot_disk_size` | image default | Required for C3/C4/N4 (`pd-balanced` or `hyperdisk-balanced`). MongoDB logs live here |
| `min_cpu_platform` | unset | e.g. `Intel Ice Lake`; not for E2/N4/C3/C4 |
| `nic_type`, `tier1_networking` | default, `false` | Tier_1 requires `GVNIC` |
| `zone_mode`, `zones` | `spread`, all zones | `single` puts every instance in the first zone |
| `compact_placement` | `false` | Collocated placement policy; requires `zone_mode = "single"` |
| `plgm_type` | `e2-medium` | Size it so the client is not the bottleneck |
| `plgm_local_port` | `29999` | Local port forwarded by the generated ssh_config to the plgm Web UI |
| `labels` | `{}` | Extra labels on every instance and disk |

Example (C4 + Hyperdisk, single zone):

```hcl
shardsvr_type         = "c4-standard-16"
configsvr_type        = "c4-standard-4"
mongos_type           = "c4-standard-8"
arbiter_type          = "c4-standard-2"
shardsvr_volume_size  = 500
configsvr_volume_size = 50
data_disk_type        = "hyperdisk-balanced"
data_disk_iops        = 16000
data_disk_throughput  = 400
boot_disk_type        = "hyperdisk-balanced"
nic_type              = "GVNIC"
zone_mode             = "single"
plgm_type             = "c4-highcpu-16"
labels                = { bench_run = "r1" }
```

Spot VMs (`use_spot_instances`) are not suitable for benchmarks; they never
apply to the plgm client or service hosts.

## Deploy

Initialize and review the Terraform plan before applying it:

```bash
terraform init
terraform plan
terraform apply
```

When using a non-default variable file, pass it consistently:

```bash
terraform plan -var-file=my-deployment.tfvars
terraform apply -var-file=my-deployment.tfvars
```

Terraform creates one inventory and SSH config per topology:

- `<prefix>_inventory_<cluster-or-replset>`
- `<prefix>_ssh_config_<cluster-or-replset>`

Optionally append a generated SSH configuration to your local configuration:

```bash
cat myenv_ssh_config_cl01 >> ~/.ssh/config
```

Run Ansible directly against the generated inventory from this directory:

```bash
ansible-playbook -i myenv_inventory_cl01 ../../ansible/main.yml
```

Typical provisioning takes about 1 minute for the infrastructure and about 15
minutes for Ansible to configure a 2-shard cluster.

## Connecting

If you appended the generated SSH configuration, connect by host alias:

```bash
ssh my-cluster-name-mongodb-cfg01
```

## Percona ClusterSync

Percona ClusterSync is disabled by default. Set `enable_pcsm=true` to create one dedicated `e2-small` VM in the environment VPC. Only SSH is allowed inbound; API port `2242` is not exposed. Terraform never receives PCSM connection URIs or passwords. The package version defaults to `pcsm_version="0.9.0"`.

Set source and target kinds to `cluster` or `replset`; they must match and the names must differ. Terraform writes one normal inventory per topology plus `<prefix>_inventory_pcsm`. Run Ansible for each selected topology, then run `pcsm.yml` against that PCSM inventory. Store the required PCSM URIs and passwords in an owner-only environment file. See the [Ansible ClusterSync instructions](../../ansible/README.md#percona-clustersync) for the complete procedure.

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

This minimal example creates two PSMDB replica sets and a PCSM VM. `prefix` must start with a lowercase letter, contain only lowercase letters and digits, and be at most 14 characters. The key in `gce_ssh_users` must match `my_ssh_user`. GCP credentials are supplied through the Google provider environment or application-default credentials, not this file.
Save it as `pcsm.tfvars` and pass `-var-file=pcsm.tfvars` to `terraform plan` and `terraform apply`.

```hcl
project_id           = "my-gcp-project"
prefix               = "myenv"
my_ssh_user          = "ubuntu"
gce_ssh_users        = { ubuntu = "/absolute/path/to/id_ed25519.pub" }
ssh_private_key_path = "/absolute/path/to/id_ed25519"

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

## Backup credentials and sensitive files

Terraform creates a separate PBM service account named
`<prefix>-mongo-backup-sa`, grants it access to the created bucket, and creates
an HMAC key. This is not the deployment service account. Inspect outputs with:

```bash
terraform output -json
```

The JSON deployment key and PBM HMAC secret are sensitive. The PBM secret is
stored in Terraform state and generated inventory files. Keep keys, state,
tfvars, inventories, SSH configuration, and UI secret directories out of source
control and restrict their filesystem permissions.

## Network security

Instances receive public IP addresses. The default `source_ranges` value is
`0.0.0.0/0`, and ICMP is publicly allowed. Before deployment, restrict SSH source
ranges to trusted addresses and review all firewall rules. The defaults are
intended for disposable test environments, not production.

## Destroy

Destroy with the same variables used for the apply:

```bash
terraform destroy
# Or: terraform destroy -var-file=my-deployment.tfvars
```

The backup bucket uses `force_destroy`, so destroying the deployment also deletes
its backup objects. Remove generated inventory and SSH files after destruction if
they are no longer needed.

## Expanding existing deployments

Supported scale-out changes are additive only:

- Increase `shard_count` to add shards to an existing sharded cluster.
- Increase `data_nodes_per_replset` to add data-bearing members to a standalone replica set.
- Add a new sharded cluster or standalone replica set.

After editing variables, run `terraform apply` to create the new instances and
regenerate inventory files. Then run the matching Ansible scale-out playbook from
the repository root:

```bash
ansible-playbook -i terraform/gcp/myenv_inventory_cl01 ansible/add_shard.yml \
  --extra-vars "new_shard_group=shard3"

ansible-playbook -i terraform/gcp/myenv_inventory_rs01 ansible/add_replset_member.yml \
  --extra-vars "target_replset=rs01"
```

For an entirely new cluster or replica set, run `ansible/main.yml` against its
generated inventory. Reducing topology size, changing `configsvr_count`, changing
`shardsvr_replicas`, and changing arbiter counts are not implemented.
