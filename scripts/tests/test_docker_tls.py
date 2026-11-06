import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("docker_tls", Path(__file__).parents[1] / "docker-tls.py")
tls = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tls)


class DockerTLSTests(unittest.TestCase):
    def test_names_reject_shell_and_openssl_config_injection(self):
        for name in ["../other", "-bad", "node,IP:1.2.3.4", "a\nb", "a/b", "$(id)"]:
            with self.assertRaises(ValueError):
                tls.validate_name(name)
        self.assertEqual(tls.validate_name("env-rs-svr0.example.test"), "env-rs-svr0.example.test")

    def test_signer_is_offline_and_never_receives_private_keys_in_arguments(self):
        with patch.dict(os.environ, {"TLS_IMAGE": "signer"}), patch.object(tls.subprocess, "run") as run:
            run.return_value.returncode = 0
            tls.run_script("private-script", [("test-ca", "/ca")])
            args = run.call_args.args[0]
            self.assertEqual(args[args.index("--network") + 1], "none")
            self.assertNotIn("private-script", args)
            self.assertEqual(run.call_args.kwargs["input"], b"private-script")

    def test_errors_do_not_disclose_subprocess_output(self):
        with patch.dict(os.environ, {"TLS_IMAGE": "signer"}), patch.object(tls.subprocess, "run") as run:
            run.return_value.returncode = 1
            run.return_value.stderr = b"private key material"
            with self.assertRaisesRegex(RuntimeError, "Docker TLS operation failed") as error:
                tls.run_script("script", [])
            self.assertNotIn("private key", str(error.exception))

    def test_issuance_has_dns_and_ip_sans(self):
        env = {"TLS_CA_VOLUME": "ca", "TLS_VOLUME": "cert", "TLS_CLIENT_VOLUME": "client", "TLS_CLIENT_UID": "1002", "TLS_NAME": "node",
               "TLS_UID": "1001", "TLS_DNS_NAMES": json.dumps(["node", "node.test"])}
        with patch.dict(os.environ, env), patch.object(tls.sys, "argv", ["docker-tls.py", "issue"]), patch.object(tls, "run_script") as run:
            tls.main()
            issued = run.call_args.args[2]
            self.assertEqual(issued["TLS_SAN"], "DNS:localhost,DNS:node,DNS:node.test,IP:127.0.0.1,IP:::1")

    @unittest.skipUnless(shutil.which("openssl"), "OpenSSL is required")
    def test_real_certificates_idempotency_permissions_and_recovery(self):
        # Execute the exact signing scripts locally, replacing only mount paths
        # and ownership (unit tests need not run as root). Docker plumbing is
        # covered above and in the opt-in integration test.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ("ca", "trust", "certs", "client"):
                (root / name).mkdir()

            def run(script, extra=None, check=True):
                for name in ("ca", "trust", "certs", "client"):
                    script = script.replace("/" + name, str(root / name))
                script = re.sub(r'^\s*chown .*$', ': # ownership tested by Docker integration', script, flags=re.M)
                result = subprocess.run(["sh", "-s"], input=script.encode(), env={**os.environ, **(extra or {})},
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                if check and result.returncode:
                    self.fail(result.stderr.decode())
                return result

            run(tls.PREPARE)
            env = {"TLS_NAME": "test-rs-svr0", "TLS_UID": str(os.getuid()), "TLS_CLIENT_UID": str(os.getuid()),
                   "TLS_SAN": "DNS:test-rs-svr0,DNS:localhost,IP:127.0.0.1,IP:::1"}
            run(tls.ISSUE, env)
            pem = root / "certs/server.pem"
            original = pem.read_bytes()
            run(tls.PREPARE)
            run(tls.ISSUE, env)
            self.assertEqual(original, pem.read_bytes())
            self.assertEqual(pem.stat().st_mode & 0o777, 0o400)
            self.assertEqual((root / "certs").stat().st_mode & 0o777, 0o700)
            self.assertNotIn(b"PRIVATE KEY", (root / "trust/test-ca.pem").read_bytes())
            self.assertFalse((root / "client/server.pem").exists())
            issued = root / "ca/issued/test-rs-svr0"
            cmd = ["openssl", "verify", "-CAfile", str(root / "trust/test-ca.pem")]
            subprocess.run(cmd + ["-verify_hostname", "localhost", str(issued / "server.crt")], check=True, capture_output=True)
            wrong = subprocess.run(cmd + ["-verify_hostname", "wrong.test", str(issued / "server.crt")], capture_output=True)
            self.assertNotEqual(wrong.returncode, 0)
            text = subprocess.check_output(["openssl", "x509", "-in", str(issued / "server.crt"), "-text", "-noout"]).decode()
            self.assertIn("TLS Web Server Authentication, TLS Web Client Authentication", text)
            client = subprocess.check_output(["openssl", "x509", "-in", str(issued / "client.crt"), "-subject", "-noout"]).decode()
            self.assertIn("MongoDB Automation", client)
            self.assertNotIn("MyOrganization", client)
            self.assertNotEqual(run(tls.ISSUE, {**env, "TLS_SAN": "DNS:changed.test"}, check=False).returncode, 0)
            run(tls.ISSUE, {**env, "TLS_NAME": "plgm", "TLS_SERVER": "false"})
            self.assertFalse((root / "ca/issued/plgm/server.pem").exists())
            self.assertFalse((root / "client/server.pem").exists())
            (root / "ca/intermediate.key").unlink()
            self.assertNotEqual(run(tls.PREPARE, check=False).returncode, 0)
            self.assertEqual(original, pem.read_bytes())

    def test_data_mode_guard_rejects_tls_conversion(self):
        with tempfile.TemporaryDirectory() as directory:
            script = tls.CHECK_DATA.replace("/data/db", directory)
            script = re.sub(r'^chown .*$', ':', script, flags=re.M)
            env = {**os.environ, "TLS_UID": str(os.getuid()), "TLS_ENABLED": "false"}
            subprocess.run(["sh", "-s"], input=script.encode(), env=env, check=True, capture_output=True)
            changed = subprocess.run(["sh", "-s"], input=script.encode(), env={**env, "TLS_ENABLED": "true"}, capture_output=True)
            self.assertNotEqual(changed.returncode, 0)
            self.assertIn(b"Changing TLS", changed.stderr)
            (Path(directory) / ".framework-network-tls").unlink()
            (Path(directory) / "WiredTiger").touch()
            legacy = subprocess.run(["sh", "-s"], input=script.encode(), env={**env, "TLS_ENABLED": "true"}, capture_output=True)
            self.assertNotEqual(legacy.returncode, 0)


if __name__ == "__main__":
    unittest.main()
