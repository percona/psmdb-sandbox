# Initialize Config server replica set 
resource "null_resource" "initiate_cfg_replset" {
  depends_on = [docker_container.cfg]

  # Run rs.initiate()
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.cfg[local.cfg_member_keys[0]].name} mongosh --port ${var.configsvr_port} --eval '
        rs.initiate({
          "_id": "${lookup({ for label in docker_container.cfg[local.cfg_member_keys[0]].labels : label.label => label.value }, "replsetName", null)}",
          "configsvr": true,
          "members": [
            { "_id": 0, "host": "${docker_container.cfg[local.cfg_member_keys[0]].name}:${var.configsvr_port}", "priority": 2 },
            ${join(",", [for i in range(1, var.configsvr_count) : "{ _id: ${i}, host: \"${docker_container.cfg[local.cfg_member_keys[i]].name}:${var.configsvr_port}\" }"])}
          ]
        });
      '
    EOT
  }

  # Wait for primary to be elected
  provisioner "local-exec" {
    command = <<-EOT
      retries=30
      success=false
      while [ $retries -gt 0 ]; do
        # rs.status() shows PRIMARY before the node accepts writes, and the users are created on this member
        primary=$(docker exec -i ${docker_container.cfg[local.cfg_member_keys[0]].name} mongosh --port ${var.configsvr_port} --quiet --eval "db.hello().isWritablePrimary")
        
        if test "$primary" = "true"; then
          echo "Primary has been elected in config server replica set"
          success=true
          break
        fi
        
        echo "Waiting for primary to be elected... retries left: $retries"
        retries=$((retries - 1))
        sleep 5
      done

      if test "$success" = "false"; then
        echo "Primary not elected after maximum retries. Exiting."
        exit 1
      fi
    EOT
  }

  # Create root user on the config servers
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.cfg[local.cfg_member_keys[0]].name} mongosh admin --port ${var.configsvr_port} --eval '
        db.createUser({
          "user": "${var.mongodb_root_user}",
          "pwd": "${var.mongodb_root_password}",
          "roles": [
            { "role": "root", "db": "admin" }
          ]
        });
      '
    EOT
  }

  # Create user for PBM on config servers
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.cfg[local.cfg_member_keys[0]].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${var.configsvr_port} --eval '
        db.createRole({
          "role": "pbmAnyAction",
          "privileges": [
            { "resource": { "anyResource": true }, "actions": [ "anyAction" ] }
          ],
          roles: []
        });
        db.createUser( {
          "user": "${var.mongodb_pbm_user}",
          "pwd": "${var.mongodb_pbm_password}",
          "roles": [         
            { "db" : "admin", "role" : "readWrite", "collection": "" },
            { "db" : "admin", "role" : "backup" },
            { "db" : "admin", "role" : "clusterMonitor" },
            { "db" : "admin", "role" : "restore" },
            { "db" : "admin", "role" : "pbmAnyAction" } 
          ]
        });
      '
    EOT
  }

  # Create user for mongot router authentication on config servers
  provisioner "local-exec" {
    command = <<-EOT
      if test "${var.enable_mongot}" = "true"; then
        docker exec -i ${docker_container.cfg[local.cfg_member_keys[0]].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${var.configsvr_port} --eval '
          if (!db.getUser("${var.mongodb_mongot_user}")) {
            db.createUser({
              user: "${var.mongodb_mongot_user}",
              pwd: "${var.mongodb_mongot_password}",
              roles: [ { role: "searchCoordinator", db: "admin" } ]
            });
          }
        '
      fi
    EOT
  }

  # Create user for PMM on config servers
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.cfg[local.cfg_member_keys[0]].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${var.configsvr_port} --eval '
        db.createRole({
          role: "pmmMonitor",
          privileges: [{
            "resource": { "db": "", "collection": "" },
            "actions": [ "listIndexes", "listCollections", "dbStats", "dbHash", "collStats", "indexStats", "find" ]
          }, 
          {
            "resource": { "db": "", "collection": "system.profile" },
            "actions": [ "dbStats","indexStats","collStats" ], 
          },
          {
            "resource": { "db": "", "collection": "system.version" },
            "actions": [ "find" ]
          }],
          roles: []
        });
        db.createUser({
          "user": "${var.mongodb_pmm_user}",
          "pwd": "${var.mongodb_pmm_password}",
          "roles": [ 
            { "role": "pmmMonitor", "db": "admin" },
            { "role": "read", "db": "local" },
            { "db" : "admin", "role" : "readWrite", "collection": "" },
            { "db" : "admin", "role" : "backup" },
            { "db" : "admin", "role" : "clusterMonitor" },
            { "db" : "admin", "role" : "restore" },
            { "db" : "admin", "role" : "pbmAnyAction" } 
          ]
        });
      '
    EOT
  }
}

# Initiate the shards replica sets 
resource "null_resource" "initiate_shard_replset" {
  depends_on = [docker_container.arbiter, docker_container.shard]

  for_each = toset([for i in range(var.shard_count) : tostring(i)])

  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.shard["shard${each.key}svr0"].name} mongosh --port ${var.shardsvr_port} --eval '
        rs.initiate({
          _id: "${lookup({ for label in docker_container.shard["shard${each.key}svr0"].labels : label.label => label.value }, "replsetName", null)}",
          members: [
            { _id: 0, host: "${docker_container.shard["shard${each.key}svr0"].name}:${var.shardsvr_port}", priority: 2 },
            ${join(",", [for i in range(1, var.shardsvr_replicas) : "{ _id: ${i}, host: \"${docker_container.shard["shard${each.key}svr${i}"].name}:${var.shardsvr_port}\" }"])}
            ${join(",", [for i in range(var.arbiters_per_replset) : ",{ _id: ${var.shardsvr_replicas + i}, host: \"${docker_container.arbiter["shard${each.key}arb${i}"].name}:${var.shardsvr_port}\", arbiterOnly: true }"])}
          ]
        });
      '
    EOT
  }
}

# Check primary elected
resource "null_resource" "check_primary" {
  depends_on = [null_resource.initiate_shard_replset]

  for_each = toset([for i in range(var.shard_count) : tostring(i)])

  provisioner "local-exec" {
    command = <<-EOT
      retries=30
      success=false
      while [ $retries -gt 0 ]; do
        # rs.status() shows PRIMARY before the node accepts writes, and the users are created on this member
        primary=$(docker exec -i ${docker_container.shard["shard${each.key}svr0"].name} mongosh --port ${var.shardsvr_port} --quiet --eval "db.hello().isWritablePrimary")
        
        if test "$primary" = "true"; then
          echo "Primary has been elected in shard ${each.key}"
          success=true
          break
        fi
        
        echo "Waiting for primary to be elected in shard ${each.key}... retries left: $retries"
        retries=$((retries - 1))
        sleep 5
      done

      if test "$success" = "false" ; then
        echo "Primary not elected in shard ${each.key} after maximum retries. Exiting."
        exit 1
      fi
    EOT
  }
}

# Create users
resource "null_resource" "create_users" {
  depends_on = [null_resource.check_primary]

  for_each = toset([for i in range(var.shard_count) : tostring(i)])

  # Create the root user on the shards
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.shard["shard${each.key}svr0"].name} mongosh admin --port ${var.shardsvr_port} --eval '
        db.createUser({
          "user": "${var.mongodb_root_user}",
          "pwd": "${var.mongodb_root_password}",
          "roles": [
            { "role": "root", "db": "admin" }
          ]
        });
      '        
    EOT
  }

  # Create user for PBM on the shards
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.shard["shard${each.key}svr0"].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${var.shardsvr_port} --eval '
        db.createRole({
          "role": "pbmAnyAction",
          "privileges": [
            { "resource": { "anyResource": true }, "actions": ["anyAction"] }
          ],
          "roles": []
        });
        db.createUser( {
          "user": "${var.mongodb_pbm_user}",
          "pwd": "${var.mongodb_pbm_password}",
          "roles": [         
            { "db" : "admin", "role" : "readWrite", "collection": "" },
            { "db" : "admin", "role" : "backup" },
            { "db" : "admin", "role" : "clusterMonitor" },
            { "db" : "admin", "role" : "restore" },
            { "db" : "admin", "role" : "pbmAnyAction" } 
          ]
        });
      '
    EOT
  }

  # Create user for mongot on the shards
  provisioner "local-exec" {
    command = <<-EOT
      if test "${var.enable_mongot}" = "true"; then
        docker exec -i ${docker_container.shard["shard${each.key}svr0"].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${var.shardsvr_port} --eval '
          if (!db.getUser("${var.mongodb_mongot_user}")) {
            db.createUser({
              user: "${var.mongodb_mongot_user}",
              pwd: "${var.mongodb_mongot_password}",
              roles: [ { role: "searchCoordinator", db: "admin" } ]
            });
          }
        '
      fi
    EOT
  }

  # Create user for PMM on the shards
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.shard["shard${each.key}svr0"].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${var.shardsvr_port} --eval '
        db.createRole({
          "role": "pmmMonitor",
          "privileges": [{
            "resource": { "db": "", "collection": "" },
            "actions": ["listIndexes","listCollections","dbStats","dbHash","collStats","indexStats","find"]
          }, 
          {
            "resource": { "db": "", "collection": "system.profile" },
            "actions": ["dbStats","indexStats","collStats"]
          }, 
          {
            "resource": { "db": "", "collection": "system.version" },
            "actions": ["find"]
          }],
          "roles": []
        });
        db.createUser({
          "user": "${var.mongodb_pmm_user}",
          "pwd": "${var.mongodb_pmm_password}",
          "roles": [ 
            { "role": "pmmMonitor", "db": "admin" },
            { "role": "read", "db": "local" },
            { "db" : "admin", "role" : "readWrite", "collection": "" },
            { "db" : "admin", "role" : "backup" },
            { "db" : "admin", "role" : "clusterMonitor" },
            { "db" : "admin", "role" : "restore" },
            { "db" : "admin", "role" : "pbmAnyAction" } 
          ]
        });
      '
    EOT
  }
}

# Set the global write concern to 1. This is needed when using arbiters
resource "null_resource" "change_default_write_concern" {
  count = length(docker_container.arbiter) > 0 ? 1 : 0
  depends_on = [
    docker_container.mongos,
    null_resource.initiate_cfg_replset,
    null_resource.initiate_shard_replset
  ]
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.mongos[local.mongos_member_keys[0]].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --eval '
        db.adminCommand({
          "setDefaultRWConcern" : 1,
          "defaultWriteConcern" : { "w" : 1 },
          "defaultReadConcern" : { "level" : "local" }
        })
      '
    EOT
  }
}

# Add the shards to the cluster. This is keyed per shard so increasing
# shard_count registers only the newly-created shard.
resource "null_resource" "add_shard" {
  for_each = toset([for i in range(var.shard_count) : tostring(i)])

  triggers = {
    mongos_id        = docker_container.mongos[local.mongos_member_keys[0]].id
    shard_primary_id = docker_container.shard["shard${each.key}svr0"].id
    shard_name       = lookup({ for label in docker_container.shard["shard${each.key}svr0"].labels : label.label => label.value }, "replsetName", "")
    shard_host       = "${docker_container.shard["shard${each.key}svr0"].name}:${var.shardsvr_port}"
  }

  depends_on = [
    docker_container.mongos,
    null_resource.initiate_cfg_replset,
    null_resource.initiate_shard_replset,
    null_resource.change_default_write_concern
  ]
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.mongos[local.mongos_member_keys[0]].name} mongosh admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --eval '
        const shardName = "${self.triggers.shard_name}";
        const shardConn = shardName + "/${self.triggers.shard_host}";
        if (!db.adminCommand({ listShards: 1 }).shards.some(s => s._id === shardName)) {
          sh.addShard(shardConn);
        } else {
          print("Shard " + shardName + " is already registered, skipping.");
        }
      '
    EOT
  }
}

# Configure PBM Storage
resource "null_resource" "configure_pbm" {
  count = var.enable_pbm ? 1 : 0

  depends_on = [
    null_resource.add_shard,
    docker_container.cfg,
    docker_container.shard,
    docker_container.pbm_shard,
    docker_container.pbm_cfg
  ]
  provisioner "local-exec" {
    command = <<-EOT
      sleep 5
      cat ${path.module}/pbm-storage.conf.${var.cluster_name} | docker exec -i ${docker_container.pbm_cfg[local.cfg_member_keys[0]].name} pbm config --file=-
    EOT
  }
}
