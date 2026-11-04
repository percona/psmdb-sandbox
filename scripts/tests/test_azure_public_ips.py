from pathlib import Path
import re
import unittest


AZURE_DIR = Path(__file__).resolve().parents[2] / "terraform" / "azure"


class AzurePublicIPTests(unittest.TestCase):
    def test_all_public_ips_use_standard_sku_and_static_allocation(self):
        directories = [AZURE_DIR, *sorted((AZURE_DIR / "modules").iterdir())]
        for directory in directories:
            resources = []
            for path in sorted(directory.glob("*.tf")):
                # Public IP resources have no nested blocks. Anchor the closing
                # brace to avoid matching braces in interpolated resource names.
                for match in re.finditer(
                    r'^resource "azurerm_public_ip" "([^"]+)" \{\n(.*?)^\}',
                    path.read_text(),
                    re.MULTILINE | re.DOTALL,
                ):
                    resources.append((path, match.group(1), match.group(2)))

            with self.subTest(directory=directory.relative_to(AZURE_DIR)):
                self.assertTrue(resources, "Expected Azure public IP resources")
            for path, name, body in resources:
                with self.subTest(path=path.relative_to(AZURE_DIR), resource=name):
                    self.assertRegex(body, r'(?m)^\s*sku\s*=\s*"Standard"\s*$')
                    self.assertRegex(
                        body, r'(?m)^\s*allocation_method\s*=\s*"Static"\s*$'
                    )


if __name__ == "__main__":
    unittest.main()
