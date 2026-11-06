# Initialize replica set 
resource "null_resource" "initiate_replset" {
  depends_on = [docker_container.rs]

  # Run rs.initiate()
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --eval '
        rs.initiate({
          "_id": "${lookup({ for label in docker_container.rs[local.replset_member_keys[0]].labels : label.label => label.value }, "replsetName", null)}",
          "members": [
            { "_id": 0, "host": "${docker_container.rs[local.replset_member_keys[0]].name}:${var.replset_port}", "priority": 2 },
            ${join(",", [for i in range(1, var.data_nodes_per_replset) : "{ _id: ${i}, host: \"${docker_container.rs[local.replset_member_keys[i]].name}:${docker_container.rs[local.replset_member_keys[i]].ports[0].internal}\" }"])}
            ${join(",", [for i in range(var.arbiters_per_replset) : ",{ _id: ${var.data_nodes_per_replset + i}, host: \"${docker_container.arbiter[local.arbiter_member_keys[i]].name}:${docker_container.arbiter[local.arbiter_member_keys[i]].ports[0].internal}\", arbiterOnly: true }"])}
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
        primary=$(docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --quiet --eval "db.hello().isWritablePrimary")
        
        if test "$primary" = "true"; then
          echo "Primary has been elected in replica set"
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

  # Create root user on the rs servers
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --eval '
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

  # Create user for PBM on rs servers
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --eval '
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

  # Create user for mongot on rs servers
  provisioner "local-exec" {
    command = <<-EOT
      if test "${var.enable_mongot}" = "true"; then
        docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --eval '
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

  # Create user for PMM on rs servers
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --eval '
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

# Set the global write concern to 1. This is needed when using arbiters
resource "null_resource" "change_default_write_concern" {
  count = length(docker_container.arbiter) > 0 ? 1 : 0
  depends_on = [
    null_resource.initiate_replset
  ]
  provisioner "local-exec" {
    command = <<-EOT
      docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --eval '
        db.adminCommand({
          "setDefaultRWConcern" : 1,
          "defaultWriteConcern" : { "w" : 1 },
          "defaultReadConcern" : { "level" : "local" }
        })
      '
    EOT
  }
}

# Add newly-created data-bearing containers to an existing replica set.
# The check makes this safe on first deployment because rs.initiate already
# registers the initial members.
resource "null_resource" "add_new_replset_members" {
  triggers = {
    member_ids   = join(",", [for key in local.replset_member_keys : docker_container.rs[key].id])
    member_hosts = join(",", [for key in local.replset_member_keys : "${docker_container.rs[key].name}:${docker_container.rs[key].ports[0].internal}"])
  }

  depends_on = [
    null_resource.initiate_replset,
    docker_container.rs
  ]

  provisioner "local-exec" {
    command = <<-EOT
      primary=""
      for attempt in $(seq 1 30); do
        primary=$(docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --port ${docker_container.rs[local.replset_member_keys[0]].ports[0].internal} --quiet --eval 'db.hello().primary')
        if [ -n "$primary" ] && [ "$(docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --host "$primary" --quiet --eval 'db.hello().isWritablePrimary')" = "true" ]; then
          break
        fi
        primary=""
        echo "Waiting for a writable primary (attempt $attempt/30)..."
        sleep 5
      done
      if [ -z "$primary" ]; then
        echo "No writable primary; cannot add replica set members."
        exit 1
      fi
      docker exec -i ${docker_container.rs[local.replset_member_keys[0]].name} mongosh ${local.mongo_tls_flags} admin -u ${var.mongodb_root_user} -p ${var.mongodb_root_password} --host "$primary" --eval '
        const existing = rs.status().members.map(m => m.name);
        const desired = [${join(",", [for key in local.replset_member_keys : "\"${docker_container.rs[key].name}:${docker_container.rs[key].ports[0].internal}\""])}];
        desired.forEach(member => {
          if (!existing.includes(member)) {
            print("Adding replica set member " + member);
            rs.add(member);
          } else {
            print("Replica set member " + member + " already exists, skipping.");
          }
        });
      '
    EOT
  }
}

# Configure PBM
resource "null_resource" "configure_pbm" {
  count = var.enable_pbm ? 1 : 0

  depends_on = [
    null_resource.initiate_replset,
    null_resource.add_new_replset_members,
    docker_container.rs,
    docker_container.pbm_rs
  ]
  provisioner "local-exec" {
    command = <<-EOT
      sleep 5
      cat ${path.module}/pbm-storage.conf.${var.rs_name} | docker exec -i ${docker_container.pbm_rs[local.replset_member_keys[0]].name} pbm config --file=-
    EOT
  }
}
