# MongoDB 9.0 fresh deployments

MongoDB 9.0 is an explicit selection; existing default versions are retained.
These instructions create new environments. They do not implement in-place
major-version upgrades.

## AWS, GCP, Azure and CHAOS

All four targets accept the same global or per-topology package settings:

```hcl
mongodb_distribution = "community" # or "enterprise"
mongo_release        = "9.0"
mongo_version        = "9.0.2"     # optional; empty installs the latest in 9.0
```

Set `enable_pbm = false` and `enable_mongot = false` in every 9.0 topology.
Their compatibility checks reject unsupported combinations before installation.

Choose an OS with packages for the selected distribution and architecture.
The UI filters release and patch selections by the selected OS. Ubuntu 24.04
is a suitable starting point. Ansible uses the MongoDB 9 signing key
`https://pgp.mongodb.com/server-9.asc`; Debian repositories use `main`, while
Ubuntu repositories use `multiverse`.

For example, add these settings to the selected provider's minimum tfvars:

```hcl
clusters = {}
replsets = {
  rs01 = {
    mongodb_distribution = "community"
    mongo_release        = "9.0"
    mongo_version        = "9.0.2"
    enable_pmm           = false
    enable_pbm           = false
  }
}
```

For a sharded environment use the same package settings inside a `clusters`
entry instead. Apply Terraform, then run `ansible/main.yml` against the
generated inventory as described in the provider README.

## Percona Server for MongoDB

PSMDB uses `mongodb_distribution = "psmdb"` and `mongo_release = "psmdb-90"`.
Repository directories can appear before server packages are published. The
UI only exposes 9.x+ PSMDB releases after finding actual server versions;
OS/channel-specific discovery must also find packages for your selection.

At the initial 9.0 implementation, the inspected PSMDB 9.0 APT and RHEL 9
release repositories have no server packages. Do not treat the presence of a
`psmdb-90` directory as an available release.

## Docker

Docker remains Percona-image-only. Select a published, explicit
`percona/percona-server-mongodb:9.0.<patch>-<revision>` tag once available.
The UI discovers image tags from Docker Hub; no speculative 9.0 fallback tag
is provided. Both replica-set and sharded-cluster modules accept new Percona
image versions without changing the topology model.

Set `enable_pbm = false` and `enable_mongot = false` for a PSMDB 9.0 topology
until those integrations are verified. The modules enforce this for explicit
9.x+ image tags. Mutable tags such as `latest` do not identify a server version;
use explicit version tags for reproducible deployments.

## Libvirt/KVM

Libvirt provisions VMs and base access only. After the VMs are reachable,
create an Ansible inventory with replica-set or sharded-cluster host groups
and run:

```bash
ansible-playbook -i /path/to/inventory ansible/main.yml \
  -e mongodb_distribution=community -e mongo_release=9.0 \
  -e mongo_version=9.0.2 -e enable_pbm=false -e enable_pmm=false
```

The same command can select Enterprise. PSMDB requires published packages
for the guest OS and architecture.

## Optional integrations

| Integration | MongoDB 9.0 behavior |
| --- | --- |
| PBM, all distributions | Disabled for 9.x+. PBM 2.16.0 does not advertise 9.0 support, and a CHAOS logical restore failed on an index collation specification rejected by MongoDB 9.0. |
| PMM | Verified with PMM Server and Client 3.9.1, including received `mongodb_up` metrics. |
| ClusterSync | 9.0 endpoints are rejected; currently verified lines are 6.0, 7.0 and 8.0. |
| Search / mongot | Disabled for 9.x+ until compatibility is verified. |

On existing supported releases, Community/Enterprise uses logical backups and
PSMDB retains physical backups as its default.
An explicit Ansible `pbm_backup_type` still overrides the distribution-aware
default. Community does not support audit logging or encryption at rest.
Community/Enterprise do not support PSMDB's profiler `rateLimit`, so they profile
a 1% sample through `slowOpSampleRate` instead; compare benchmark results only
between deployments created with this setting.

References: [MongoDB 9.0 release notes](https://www.mongodb.com/docs/manual/release-notes/9.0/),
[PBM compatibility](https://docs.percona.com/percona-backup-mongodb/details/versions.html),
[PCSM version requirements](https://docs.percona.com/percona-clustersync-for-mongodb/deployment.html).

## CHAOS verification (2026-10-05)

Fresh Ubuntu 24.04 AMD64 environments were tested with MongoDB 9.0.2:

| Distribution | Replica set | Sharded cluster |
| --- | --- | --- |
| Community | Passed | Passed |
| Enterprise | Passed, audit enabled | Passed, audit enabled |

Checks covered installed version, replica-set health, shard registration,
authenticated reads/writes, PMM registration, and Enterprise audit output with
a custom filter. PMM received healthy metrics from all six MongoDB nodes in
the final Enterprise environment.

The sharded tests also verified that standalone official `mongos` packages
receive a system account and that their service uses the distribution-specific
user/group. Inventory-selected audit toggles and filters now override defaults;
JSON audit policies are rendered as valid YAML strings.

All test resources, including resources from failed exploratory runs, were
destroyed. Terraform test states were empty and the CHAOS API reported no
remaining instances with the test prefixes.

## Local verification

```bash
cd ui-go && go test ./...
cd ../ansible && ansible-playbook -i localhost, tests/mongodb_versions.yml
ansible-playbook -i tests/mongodb_inventory.ini tests/mongodb_inventory.yml
```

Live deployment validation uses isolated CHAOS state and a unique prefix.
Destroy the test environment with the same tfvars and state after testing,
including when provisioning or configuration fails.
