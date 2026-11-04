import os
from pathlib import Path
import shutil
import shlex
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "install-prerequisites.sh"


class PrerequisiteInstallerTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.bin = Path(self.directory.name)
        self.log = self.bin / "calls.log"
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.defpath,
                        INSTALL_TEST_LOG=str(self.log))
        # Simulate Homebrew on every host. Every installer/package-manager
        # command is stubbed; these tests never install software.
        self.stub("uname", "printf 'Darwin\\n'")
        self.stub("brew", 'printf "brew %s\\n" "$*" >> "$INSTALL_TEST_LOG"')
        self.stub("terraform", "printf 'Terraform v1.9.0\\n'")
        self.stub("ansible", "printf 'ansible [core 2.14.0]\\n'")
        self.stub("go", "printf 'go version go1.22.0 darwin/arm64\\n'")
        self.stub("python3", 'exec ' + shlex.quote(sys.executable) + ' "$@"')
        self.stub("ansible-galaxy", 'printf "galaxy %s\\n" "$*" >> "$INSTALL_TEST_LOG"')

    def stub(self, name, script):
        path = self.bin / name
        path.write_text("#!/bin/sh\n" + script + "\n")
        path.chmod(0o700)

    def run_installer(self, *args):
        return subprocess.run([shutil.which("bash"), str(SCRIPT), *args],
                              cwd=self.bin, env=self.env, text=True, capture_output=True)

    def test_installs_collections_using_repository_absolute_path(self):
        result = self.run_installer("--ui")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.log.read_text()
        self.assertIn("galaxy collection install --requirements-file " +
                      str(SCRIPT.parents[1] / "ansible" / "requirements.yml"), calls)
        self.assertIn("brew install go", calls)

    def test_dry_run_does_not_execute_installers(self):
        result = self.run_installer("--dry-run", "--ui", "--docker")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("ansible-galaxy collection install", result.stdout)
        self.assertFalse(self.log.exists())

    def test_old_versions_stop_with_actionable_errors(self):
        cases = [
            ("terraform", "Terraform v1.8.9", (), "Terraform 1.9+"),
            ("ansible", "ansible [core 2.13.0]", (), "Ansible 2.14+"),
            ("go", "go version go1.21.0 darwin/arm64", ("--ui",), "Go 1.22+"),
        ]
        for tool, output, args, expected in cases:
            with self.subTest(tool=tool):
                path = self.bin / tool
                original = path.read_text()
                self.stub(tool, "printf '%s\\n' '" + output + "'")
                result = self.run_installer(*args)
                path.write_text(original)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)


if __name__ == "__main__":
    unittest.main()
