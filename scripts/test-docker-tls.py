#!/usr/bin/env python3
"""Deploy and destroy an isolated Docker TLS fixture (never use UI environments)."""

import argparse
import json
from pathlib import Path
import re
import shutil
import socket
import subprocess
import tempfile
import time
import uuid
from urllib.parse import urlencode


ROOT = Path(__file__).resolve().parents[1]
PORTS = set()


def command(args, cwd=None, check=True):
    result = subprocess.run(args, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if check and result.returncode:
        raise RuntimeError("Command failed: " + " ".join(args) + "\n" + result.stdout[-12000:])
    return result


def free_port():
    while True:
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
            if port not in PORTS and port < 65533:
                PORTS.add(port)
                return port


def copy_sources(destination):
    paths = command(["git", "ls-files", "-co", "--exclude-standard", "--", "terraform/docker", "terraform/modules", "scripts"], ROOT).stdout.splitlines()
    for path in set(paths):
        source = ROOT / path
        if source.suffix not in {".tf", ".tmpl", ".py", ".sh"} and source.name not in {"Dockerfile", ".terraform.lock.hcl"}:
            continue
        target = destination / path
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cluster", action="store_true", help="Test a sharded cluster instead of a standalone replica set")
    parser.add_argument("--pbm", action="store_true", help="Enable PBM and test a logical backup")
    parser.add_argument("--pmm", action="store_true", help="Enable PMM and check MongoDB exporter registration")
    parser.add_argument("--scale", action="store_true", help="Add a data node or shard and verify existing certificates are unchanged")
    parser.add_argument("--pcsm", action="store_true", help="Test ClusterSync from the TLS replica set to the plaintext replica set")
    parser.add_argument("--physical", action="store_true", help="Also test PBM physical backup/restore (implies --pbm; replica sets only)")
    parser.add_argument("--vault", action="store_true", help="Combine MongoDB TLS with Vault-backed encryption at rest")
    args = parser.parse_args()
    if args.cluster and args.pcsm:
        parser.error("--pcsm requires the replica-set fixture")
    if args.cluster and args.physical:
        parser.error("--physical currently requires the replica-set fixture")
    args.pbm = args.pbm or args.physical
    args.pmm = args.pmm or args.pcsm  # PCSM's existing Terraform contract requires PMM.
    command(["docker", "info"])
    prefix = "tls-e2e-" + uuid.uuid4().hex[:8]
    base = Path(tempfile.gettempdir()) / "opencode"
    directory = Path(tempfile.mkdtemp(prefix=prefix + "-", dir=base if base.is_dir() else None))
    copy_sources(directory)
    tfdir = directory / "terraform/docker"
    common = {"use_tls": True, "enable_pmm": args.pmm, "enable_pbm": args.pbm,
              "psmdb_image": "percona/percona-server-mongodb:8.0"}
    cfg = {"prefix": prefix, "enable_ca": True, "vault_encryption": args.vault, "clusters": {}, "replsets": {},
           "pmm_servers": {}, "seaweedfs_servers": {}, "ldap_servers": {}}
    if args.cluster:
        cfg["clusters"] = {"secure": {**common, "configsvr_count": 1, "shard_count": 1,
                                      "shardsvr_replicas": 2, "arbiters_per_replset": 1, "mongos_count": 1}}
        node, port = prefix + "-secure-mongos00", 27017
        roles = [(prefix + "-secure-cfg00", 27019), (prefix + "-secure-shard00svr0", 27018),
                 (prefix + "-secure-shard00svr1", 27018), (prefix + "-secure-shard00arb0", 27018), (node, port)]
    else:
        port = free_port()
        # Replica-set ports are consecutive; retry if the adjacent port is busy.
        for _ in range(20):
            with socket.socket() as sock:
                try:
                    sock.bind(("127.0.0.1", port + 1))
                    PORTS.add(port + 1)
                    PORTS.add(port + 2)
                    break
                except OSError:
                    port = free_port()
        cfg["replsets"] = {"secure": {**common, "replset_port": port, "arbiter_base_port": free_port()},
                           "plain": {"enable_pmm": False, "enable_pbm": False, "data_nodes_per_replset": 1,
                                     "arbiters_per_replset": 0, "replset_port": free_port(),
                                     "psmdb_image": common["psmdb_image"]}}
        node = prefix + "-secure-svr0"
        roles = [(node, port), (prefix + "-secure-svr1", port + 1),
                 (prefix + "-secure-arb0", cfg["replsets"]["secure"]["arbiter_base_port"])]
    if args.pbm:
        cfg["seaweedfs_servers"] = {"seaweedfs": {"seaweedfs_port": free_port(), "seaweedfs_admin_port": free_port()}}
        topology = cfg["clusters" if args.cluster else "replsets"]["secure"]
        topology["seaweedfs_port"] = cfg["seaweedfs_servers"]["seaweedfs"]["seaweedfs_port"]
    if args.pmm:
        cfg["pmm_servers"] = {"pmm-server": {"pmm_external_port": free_port()}}
    if args.pcsm:
        envfile = directory / "pcsm.env"
        source_query = urlencode({"authSource": "admin", "replicaSet": prefix + "-secure",
                                  "tls": "true", "tlsCAFile": "/etc/mongodb-tls/test-ca.pem"})
        target_query = urlencode({"authSource": "admin", "replicaSet": prefix + "-plain"})
        # Fixture-only credentials; never use a real environment's users/files.
        source_uri = f"mongodb://pcsm-source:test-pcsm-source@{node}:{port}/?{source_query}"
        plain_port = cfg["replsets"]["plain"]["replset_port"]
        target_uri = f"mongodb://pcsm-target:test-pcsm-target@{prefix}-plain-svr0:{plain_port}/?{target_query}"
        envfile.write_text(f"PCSM_SOURCE_URI='{source_uri}'\nPCSM_TARGET_URI='{target_uri}'\n")
        envfile.chmod(0o600)
        cfg.update({"enable_pcsm": True, "pcsm_env_file": str(envfile),
                    "pcsm_source_kind": "replset", "pcsm_source_name": "secure",
                    "pcsm_target_kind": "replset", "pcsm_target_name": "plain"})
    varfile = tfdir / "test.tfvars.json"
    varfile.write_text(json.dumps(cfg))
    tfargs = ["-input=false", "-no-color", "-var-file=" + str(varfile)]
    initialized = False
    passed = False

    def mongo(host, service_port, js, tls=True, extra=None, check=True):
        options = ["--tls", "--tlsCAFile", "/etc/mongodb-tls/test-ca.pem"] if tls else []
        # mongosh does not expose the legacy mongo shell's assert helpers.
        prelude = 'const tlsAssert={commandWorked(r){if(r.ok!==1)throw new Error(JSON.stringify(r))},eq(a,b){if(a!==b)throw new Error(`${a} != ${b}`)}}; '
        return command(["docker", "exec", host, "mongosh", "admin", "--quiet", "--port", str(service_port),
                        *options, *(extra or []), "--eval", prelude + js.replace("assert.", "tlsAssert.")], check=check)

    try:
        print("Fixture: " + prefix + " (" + str(directory) + ")", flush=True)
        command(["terraform", "init", "-input=false", "-no-color"], tfdir)
        initialized = True
        command(["terraform", "apply", "-auto-approve", *tfargs], tfdir)
        assert "PRIVATE KEY" not in (tfdir / "terraform.tfstate").read_text(), "Private keys leaked into Terraform state"
        for host, service_port in roles:
            mongo(host, service_port, "assert.commandWorked(db.runCommand({ping:1}))")
            mongo(host, service_port, "assert.commandWorked(db.runCommand({ping:1}))", tls=False)
            cmd = json.loads(command(["docker", "inspect", "--format", "{{json .Config.Cmd}}", host]).stdout)
            assert "x509" in cmd and "preferTLS" in cmd and "--keyFile" not in cmd, host
        auth = ["-u", "root", "-p", "percona"]
        mongo(node, port, 'assert.commandWorked(db.adminCommand({getCmdLineOpts:1})); db.getSiblingDB("tls_test").data.insertOne({test:true})', extra=auth)
        if args.pcsm:
            mongo(node, port, 'db.createUser({user:"pcsm-source",pwd:"test-pcsm-source",roles:["backup","clusterMonitor","readAnyDatabase"]})', extra=auth)
            mongo(prefix + "-plain-svr0", plain_port, 'db.createUser({user:"pcsm-target",pwd:"test-pcsm-target",roles:["restore","clusterMonitor","clusterManager","readWriteAnyDatabase"]})', tls=False, extra=auth)
        assert mongo(node, port, "db.runCommand({ping:1})", extra=["--host", "127.0.0.2"], check=False).returncode != 0
        # A different CA must fail even though the hostname is valid.
        wrong = directory / "wrong-ca.pem"
        command(["openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048", "-days", "1", "-subj", "/CN=Wrong CA",
                 "-keyout", str(directory / "wrong.key"), "-out", str(wrong)])
        command(["docker", "cp", str(wrong), node + ":/tmp/wrong-ca.pem"])
        assert mongo(node, port, "db.runCommand({ping:1})", tls=False,
                     extra=["--tls", "--tlsCAFile", "/tmp/wrong-ca.pem"], check=False).returncode != 0
        if not args.cluster:
            plain = prefix + "-plain-svr0"
            mongo(plain, cfg["replsets"]["plain"]["replset_port"], "assert.commandWorked(db.runCommand({ping:1}))", tls=False)
        before = command(["docker", "exec", node, "sha256sum", "/etc/mongodb-tls/server.pem"]).stdout
        command(["terraform", "apply", "-auto-approve", *tfargs], tfdir)
        assert command(["docker", "exec", node, "sha256sum", "/etc/mongodb-tls/server.pem"]).stdout == before
        if args.scale:
            if args.cluster:
                cfg["clusters"]["secure"]["shard_count"] = 2
                new_node, new_port = prefix + "-secure-shard01svr0", 27018
                verify = "assert.eq(2, db.adminCommand({listShards:1}).shards.length)"
            else:
                cfg["replsets"]["secure"]["data_nodes_per_replset"] = 3
                new_node, new_port = prefix + "-secure-svr2", port + 2
                verify = "assert.eq(4, rs.status().members.length)"
            varfile.write_text(json.dumps(cfg))
            command(["terraform", "apply", "-auto-approve", *tfargs], tfdir)
            mongo(new_node, new_port, "assert.commandWorked(db.runCommand({ping:1}))")
            mongo(node, port, verify, extra=auth)
            roles.append((new_node, new_port))
            assert command(["docker", "exec", node, "sha256sum", "/etc/mongodb-tls/server.pem"]).stdout == before
        command(["docker", "restart", node])
        for _ in range(60):
            if mongo(node, port, "db.runCommand({ping:1})", check=False).returncode == 0:
                break
            time.sleep(2)
        mongo(node, port, "assert.commandWorked(db.runCommand({ping:1}))")
        if args.pbm:
            cli = prefix + "-secure-pbm-cli"
            command(["docker", "exec", cli, "pbm", "status"])
            command(["docker", "exec", cli, "pbm", "backup", "--type=logical", "--wait"])
            if args.physical:
                backup = command(["docker", "exec", cli, "pbm", "backup", "--type=physical", "--wait"])
                name = re.search(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", backup.stdout)
                if not name:
                    raise RuntimeError("Could not determine the physical backup name")
                command(["docker", "exec", cli, "pbm", "restore", name.group(), "--wait"])
                command(["docker", "start", *[host for host, _ in roles]])
                command(["docker", "restart", *[host + "-pbm-agent" for host, _ in roles if "-svr" in host]])
                discovery = [*auth, "--host", f"{prefix}-secure/{node}:{port}"]
                for _ in range(90):
                    restored = mongo(node, port, 'assert.eq(1, db.getSiblingDB("tls_test").data.countDocuments({test:true}))', extra=discovery, check=False)
                    if restored.returncode == 0:
                        break
                    time.sleep(2)
                else:
                    raise RuntimeError("TLS replica set did not recover the physical backup")
        if args.pmm:
            for host, _ in roles:
                status = command(["docker", "exec", host + "-pmm-client", "pmm-admin", "list"]).stdout
                assert "mongodb_exporter" in status, status
        if args.pcsm:
            pcsm = prefix + "-pcsm"
            command(["docker", "restart", pcsm])
            for _ in range(60):
                if command(["docker", "exec", pcsm, "pcsm", "status"], check=False).returncode == 0:
                    break
                time.sleep(2)
            command(["docker", "exec", pcsm, "pcsm", "start", "--include-namespaces", "tls_test.data"])
            for _ in range(120):
                result = mongo(prefix + "-plain-svr0", plain_port, 'assert.eq(1, db.getSiblingDB("tls_test").data.countDocuments({test:true}))', tls=False, extra=auth, check=False)
                if result.returncode == 0:
                    break
                time.sleep(2)
            else:
                raise RuntimeError("ClusterSync did not copy the TLS source document to the plaintext target")
            command(["docker", "exec", pcsm, "pcsm", "pause"])
        passed = True
        checks = ["verified TLS", "plaintext compatibility", "all roles", "negative trust/hostname checks", "restart", "reapply"]
        checks += [label for enabled, label in [(args.scale, "scale-out"), (args.pbm, "PBM logical backup"),
                   (args.physical, "PBM physical backup/restore"), (args.pmm, "PMM registration"), (args.pcsm, "ClusterSync migration"),
                   (args.vault, "Vault encryption")] if enabled]
        print("PASS: " + ", ".join(checks), flush=True)
    finally:
        if initialized:
            result = command(["terraform", "destroy", "-auto-approve", *tfargs], tfdir, check=False)
            if result.returncode:
                print("Cleanup failed; preserve fixture at " + str(directory) + "\n" + result.stdout[-8000:])
                raise RuntimeError("Could not destroy the disposable TLS fixture")
        if passed:
            shutil.rmtree(directory)
        else:
            print("Test failed; logs/state retained at " + str(directory))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
