package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func vaultControllerDir(envID string) string {
	return filepath.Join(dataDir, "secrets", "vault", envID)
}

func vaultTopologies(cfg Config) []string {
	if !cfg.VaultEncryption {
		return nil
	}
	var names []string
	for name := range cfg.Clusters {
		names = append(names, "cluster-"+name)
	}
	for name := range cfg.Replsets {
		names = append(names, "replset-"+name)
	}
	sort.Strings(names)
	return names
}

var vaultTopologyNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var vaultPSMDBImageRE = regexp.MustCompile(`(^|/)percona/percona-server-mongodb[:@]`)

func validateVaultConfig(platform string, cfg Config) error {
	if !cfg.VaultEncryption {
		for key := range cfg.AnsibleVars {
			if strings.HasPrefix(key, "vault_") || (key == "keyfile_encryption" && cfg.VaultEncryption) {
				return fmt.Errorf("%s is managed by the environment encryption setting", key)
			}
		}
		return nil
	}
	if len(cfg.Clusters)+len(cfg.Replsets) == 0 {
		return fmt.Errorf("Vault encryption requires at least one cluster or replica set")
	}
	validate := func(kind, name, distribution, image string, enabled bool) error {
		if !vaultTopologyNameRE.MatchString(name) {
			return fmt.Errorf("%s %q: Vault encryption requires a name containing only letters, digits, underscores and hyphens", kind, name)
		}
		if platform == "docker" {
			if !vaultPSMDBImageRE.MatchString(strDefault(image, "percona/percona-server-mongodb:latest")) {
				return fmt.Errorf("%s %q: Vault encryption requires a PSMDB image", kind, name)
			}
		} else if normalizePackageDistribution(strDefault(distribution, cfg.MongoDBDistribution)) != "psmdb" {
			return fmt.Errorf("%s %q: Vault encryption is supported only for PSMDB", kind, name)
		}
		return nil
	}
	for name, t := range cfg.Clusters {
		if err := validate("cluster", name, t.MongoDBDistribution, t.PsmdbImage, true); err != nil {
			return err
		}
	}
	for name, t := range cfg.Replsets {
		if err := validate("replica set", name, t.MongoDBDistribution, t.PsmdbImage, true); err != nil {
			return err
		}
	}
	for key := range cfg.AnsibleVars {
		if strings.HasPrefix(key, "vault_") || key == "keyfile_encryption" {
			return fmt.Errorf("%s is managed by the environment encryption setting", key)
		}
	}
	return nil
}

func vaultDockerBootstrapShell(envID string, cfg Config) string {
	prefix := strDefault(cfg.Prefix, envID)
	credentials := make(map[string]string)
	for _, name := range vaultTopologies(cfg) {
		credentials[name] = prefix + "-vault-" + name + "-credentials"
	}
	encoded, _ := json.Marshal(credentials)
	return "VAULT_CONTAINER=" + shellQuote(prefix+"-vault") +
		" VAULT_IMAGE=" + shellQuote(strDefault(cfg.VaultImage, "hashicorp/vault:1.21.4")) +
		" VAULT_CONTROLLER_DIR=" + shellQuote(vaultControllerDir(envID)) +
		" VAULT_CREDENTIALS=" + shellQuote(string(encoded)) +
		" python3 " + shellQuote(filepath.Join(repoDir, "scripts", "vault-docker.py")) + " bootstrap"
}

func vaultDockerConsumerIDs(prefix string, all bool) string {
	format := fmt.Sprintf(`{{if ne .Names %q}}{{.ID}}{{end}}`, prefix+"-vault")
	return dockerEnvironmentListShell(prefix, all, format)
}

func vaultDockerStopShell(prefix string) string {
	return "set -o pipefail; " + vaultDockerConsumerIDs(prefix, false) + " | xargs -r docker stop && docker stop " + shellQuote(prefix+"-vault")
}

func vaultDockerRestartShell(envID string, cfg Config) string {
	prefix := strDefault(cfg.Prefix, envID)
	// Stop consumers before restarting Vault; unseal and refresh tokens first.
	return "set -o pipefail; " + vaultDockerConsumerIDs(prefix, false) + " | xargs -r docker stop && docker restart " + shellQuote(prefix+"-vault") +
		" && " + vaultDockerBootstrapShell(envID, cfg) + " && " + vaultDockerConsumerIDs(prefix, true) + " | xargs -r docker restart"
}
