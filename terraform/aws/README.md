# Deploy MongoDB infrastructure on AWS

For fresh MongoDB 9.0 package selections and integration compatibility, see
[MongoDB 9.0 deployment notes](../../docs/mongodb-9.0.md).

This Terraform module creates:

- a VPC, public subnets, an internet gateway, routes, and security groups
- a private Route 53 zone and records
- EC2 instances and EBS volumes for MongoDB and optional supporting services
- an EC2 key pair
- an S3 backup bucket with lifecycle and versioning configuration
- a PBM IAM user, access key, role, and S3 access policies
- generated Ansible inventory and SSH configuration files

## Prerequisites

- Terraform 1.9 or newer
- Ansible and OpenSSH
- an existing SSH key pair
- an AWS account and an administrator who can create an IAM user, attach policies,
  and create an access key

Install the local tools and AWS CLI from the repository root:

```bash
./scripts/install-prerequisites.sh --aws
```

The AWS CLI is required by the Web UI. Manual Terraform usage can use any
authentication method supported by the AWS provider, including an AWS profile,
environment variables, IAM Identity Center, or an assumed role.

## Create the AWS deployment user

The following setup uses a dedicated IAM user because the Web UI accepts an AWS
access key. Run these commands as an AWS administrator. For manual deployments,
an IAM Identity Center or assumed-role profile with the same permissions is
preferred when available because it avoids a long-lived access key.

1. Create the deployment user:

```bash
export AWS_DEPLOY_USER=mongodb-terraform-deployer
aws iam create-user --user-name "$AWS_DEPLOY_USER"
```

2. Attach the managed policies needed for the resources declared by this module:

```bash
aws iam attach-user-policy --user-name "$AWS_DEPLOY_USER" \
  --policy-arn arn:aws:iam::aws:policy/AmazonEC2FullAccess
aws iam attach-user-policy --user-name "$AWS_DEPLOY_USER" \
  --policy-arn arn:aws:iam::aws:policy/AmazonVPCFullAccess
aws iam attach-user-policy --user-name "$AWS_DEPLOY_USER" \
  --policy-arn arn:aws:iam::aws:policy/AmazonRoute53FullAccess
aws iam attach-user-policy --user-name "$AWS_DEPLOY_USER" \
  --policy-arn arn:aws:iam::aws:policy/AmazonS3FullAccess
aws iam attach-user-policy --user-name "$AWS_DEPLOY_USER" \
  --policy-arn arn:aws:iam::aws:policy/IAMFullAccess
```

`IAMFullAccess` is required because Terraform creates and destroys a separate IAM
user, access key, role, and inline policies for PBM backup access. These managed
policies are broad. Use a custom least-privilege policy if your organization
requires tighter controls, ensuring that it covers all EC2, EBS, VPC, Route 53,
S3, and IAM resources declared under this directory.

3. Create an access key. The secret is displayed only once, so store it in your
   approved secret manager:

```bash
aws iam create-access-key --user-name "$AWS_DEPLOY_USER"
```

4. Configure and verify a local profile using the returned access key and the
   region configured in `variables.tf`:

```bash
aws configure --profile mongodb-terraform
export AWS_PROFILE=mongodb-terraform
aws sts get-caller-identity
```

For the Web UI, enter the access key, secret access key, region, and profile name
in **Settings**. The UI stores isolated AWS credential files under
`UI_DATA_DIR/secrets/cloud/aws/` and sets `AWS_SHARED_CREDENTIALS_FILE`,
`AWS_CONFIG_FILE`, and `AWS_PROFILE` for Terraform.

## Configure the deployment

Change into the AWS Terraform directory:

```bash
cd terraform/aws
```

Review `variables.tf`, then put overrides in `terraform.tfvars` or another tfvars
file. At minimum, review:

- `prefix`, `region`, `clusters`, and `replsets`
- `ssh_public_key_path`, `ssh_private_key_path`, and `my_ssh_user`
- instance types, AMI, EBS sizes, and `use_spot_instances`
- backup bucket naming and retention
- network rules, noting that `source_ranges` currently applies only to the dedicated CA host
- optional PMM, CA/TLS, LDAP, and plgm load generator settings

The checked-in `rs.tfvars` is an example standalone replica-set configuration.

### Minimum `tfvars`

The checked-in [`minimum.tfvars`](./minimum.tfvars) is the smallest standalone
replica-set example. Set the SSH paths and user for the selected AMI. Supply AWS
credentials through the AWS provider environment or profile, not this file.

```hcl
prefix               = "myenv"
my_ssh_user          = "ec2-user"
ssh_public_key_path  = "/absolute/path/to/id_ed25519.pub"
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

### Hardware for benchmarks

All settings are global to the deployment and optional; unset values keep the
defaults. Root volume and placement settings apply to MongoDB nodes and the
plgm client only.

| Variable | Default | Notes |
|---|---|---|
| `data_disk_type` | `gp2` | Also `gp3`, `io1`, `io2`, `st1`, `sc1` (`st1`/`sc1` need at least 125 GB) |
| `data_disk_iops` | unset | `gp3` (3000–16000), `io1`/`io2` (defaults to 3000) |
| `data_disk_throughput` | unset | MiB/s, `gp3` only (125–1000) |
| `configsvr_disk_type` | `data_disk_type` | IOPS/throughput only apply when it equals `data_disk_type` |
| `boot_disk_type`, `boot_disk_size` | AMI default | Root volume; MongoDB logs live here |
| `compact_placement` | `false` | Cluster placement group; requires `subnet_count = 1` |
| `use_spot_instances` | `false` | One-time spot for MongoDB nodes. Not suitable for benchmarks |
| `plgm_type` | `t3.medium` | Size it so the client is not the bottleneck |
| `plgm_local_port` | `29999` | Local port forwarded by the generated ssh_config to the plgm Web UI |
| `labels` | `{}` | Extra tags on every resource (provider `default_tags`) |

## Deploy

Initialize and review the Terraform plan before applying it:

```bash
terraform init
terraform plan
terraform apply
```

When using a non-default variable file, pass the same file to every command:

```bash
terraform plan -var-file=rs.tfvars
terraform apply -var-file=rs.tfvars
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

Percona ClusterSync is disabled by default. Set `enable_pcsm=true` to create one dedicated `t3.small` VM in the environment VPC. Only SSH is allowed inbound; API port `2242` is not exposed. Terraform never receives PCSM connection URIs or passwords. The package version defaults to `pcsm_version="0.9.0"`.

Set source and target kinds to `cluster` or `replset`; they must match and the names must differ. Terraform writes one normal inventory per topology plus `<prefix>_inventory_pcsm`. The PCSM inventory contains only `pcsm-source`, `pcsm-target`, and `[pcsm]`, so it cannot alter MongoDB replica-set membership.

For a manual deployment, run `main.yml` once for every selected topology after `terraform apply`, then run `pcsm.yml` once:

```bash
ansible-playbook -i myenv_inventory_rs-source ../../ansible/main.yml
ansible-playbook -i myenv_inventory_rs-target ../../ansible/main.yml
ansible-playbook -i myenv_inventory_pcsm ../../ansible/pcsm.yml \
  -e pcsm_env_file_source="$HOME/.config/psmdb-sandbox/myenv-pcsm.env"
```

Create the environment file before the final command with owner-only permissions. Use URL-encoded passwords in the URI; hexadecimal passwords generated below are already URL-safe. Replace the member placeholders with the private MongoDB hostnames listed in the generated source and target topology inventories.

```bash
mkdir -p "$HOME/.config/psmdb-sandbox"
umask 077
source_password="$(openssl rand -hex 24)"
target_password="$(openssl rand -hex 24)"
cat >"$HOME/.config/psmdb-sandbox/myenv-pcsm.env" <<EOF
PCSM_SOURCE_URI='mongodb://pcsm-source:${source_password}@source-member-0:27017,source-member-1:27017/?authSource=admin&appName=pcsm&replicaSet=rs-source'
PCSM_TARGET_URI='mongodb://pcsm-target:${target_password}@target-member-0:27017,target-member-1:27017/?authSource=admin&appName=pcsm&replicaSet=rs-target'
PCSM_SOURCE_PASSWORD='${source_password}'
PCSM_TARGET_PASSWORD='${target_password}'
EOF
chmod 600 "$HOME/.config/psmdb-sandbox/myenv-pcsm.env"
```

### Minimum PCSM tfvars

This minimal example creates two PSMDB replica sets and a PCSM VM. Set `my_ssh_user` for the selected AMI and provide the matching key pair. AWS credentials are supplied through the AWS provider environment or profile, not this file.
Save it as `pcsm.tfvars` and pass `-var-file=pcsm.tfvars` to `terraform plan` and `terraform apply`.

```hcl
prefix               = "myenv"
my_ssh_user          = "ubuntu"
ssh_public_key_path  = "/absolute/path/to/id_ed25519.pub"
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

Terraform creates a separate PBM IAM user and access key restricted to the
created S3 bucket. These are not the deployment user's credentials. Inspect
outputs when needed with:

```bash
terraform output -json
```

The PBM secret is stored in Terraform state and generated inventory files. Keep
state, tfvars, inventories, SSH configuration, and UI secret directories out of
source control and restrict their filesystem permissions.

## Network security

Instances receive public IP addresses. Most SSH and ICMP security-group rules are
currently hard-coded to `0.0.0.0/0`; `source_ranges` restricts only the dedicated
CA host. Restrict the security-group rules in the Terraform code or apply
account-level controls before using this module outside a disposable test
environment.

## Destroy

Destroy with the same variables used for the apply:

```bash
terraform destroy
# Or: terraform destroy -var-file=rs.tfvars
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
ansible-playbook -i terraform/aws/myenv_inventory_cl01 ansible/add_shard.yml \
  --extra-vars "new_shard_group=shard3"

ansible-playbook -i terraform/aws/myenv_inventory_rs01 ansible/add_replset_member.yml \
  --extra-vars "target_replset=rs01"
```

For an entirely new cluster or replica set, run `ansible/main.yml` against its
generated inventory. Reducing topology size, changing `configsvr_count`, changing
`shardsvr_replicas`, and changing arbiter counts are not implemented.
