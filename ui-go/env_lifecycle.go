package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func environmentDeletionAllowed(envID string, env *Environment) error {
	switch env.Status {
	case "configured", "deleted", "destroy_success":
	default:
		return fmt.Errorf("destroy the environment successfully before removing its definition; Terraform state has been preserved")
	}
	// Never trust the UI status alone. Refuse both nonempty and unreadable state
	// files, including backups, rather than orphaning resources on disk.
	for _, path := range []string{tfstatePath(envID, env.Platform), tfstateBackupPath(envID, env.Platform)} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot verify Terraform state: %w", err)
		}
		var state struct {
			Version   int `json:"version"`
			Resources []struct {
				Mode      string            `json:"mode"`
				Instances []json.RawMessage `json:"instances"`
			} `json:"resources"`
		}
		if err := json.Unmarshal(data, &state); err != nil || state.Version != 4 {
			return fmt.Errorf("cannot verify Terraform state; preserve the files and destroy the environment first")
		}
		for _, resource := range state.Resources {
			if resource.Mode != "data" && len(resource.Instances) > 0 {
				return fmt.Errorf("Terraform state still contains managed resources; destroy the environment before removing its definition")
			}
		}
	}
	return nil
}

// markEnvironmentDestroyed turns a successfully destroyed environment into a
// kept, redeployable definition. The applied-config snapshot and cached host
// IPs describe infrastructure that no longer exists, so they are cleared to
// make the next deploy behave like a first deploy.
func markEnvironmentDestroyed(env *Environment) {
	env.Status = "deleted"
	env.LastAppliedConfig = nil
	env.HostIPs = nil
}

// withoutStaleAppliedConfig returns env with LastAppliedConfig dropped when
// the environment has been destroyed. Records destroyed before
// markEnvironmentDestroyed existed still carry the old snapshot.
func withoutStaleAppliedConfig(env *Environment) *Environment {
	if env == nil || env.Status != "deleted" || (env.LastAppliedConfig == nil && env.HostIPs == nil) {
		return env
	}
	copy := *env
	copy.LastAppliedConfig = nil
	copy.HostIPs = nil
	return &copy
}

// removeEnvironmentArtifacts deletes the on-disk files generated for an
// environment whose record has already been removed from the state map.
// env may be nil when the record was unknown.
func removeEnvironmentArtifacts(envID string, env *Environment) {
	if env == nil {
		return
	}
	if env.Platform == "chaos" {
		removeManagedChaosTokenFile(env.Config.ChaosApiTokenPath)
	}
	removeClusterSyncSecrets(envID)
	os.Remove(tfvarsPath(envID, env.Platform))
	os.Remove(tfstatePath(envID, env.Platform))
	os.Remove(tfstateBackupPath(envID, env.Platform))
	if env.Platform == "docker" {
		cleanupDockerModuleArtifacts(env.Config)
	}
}

// duplicateConfig returns a copy of src suitable for pre-filling the
// configure form of a new environment. Values bound to the source
// environment's identity are cleared; the save handler re-derives them for
// the new environment ID.
func duplicateConfig(src Config, platform string, state map[string]*Environment) Config {
	cfg := *cloneConfig(src)
	cfg.Prefix = ""
	cfg.LegacyChaosAPIToken = ""
	// CHAOS uses the token configured in Settings; never share another
	// environment's token file.
	cfg.ChaosApiTokenPath = ""

	if platform == "docker" {
		occupied := dockerOccupiedServicePorts(state, "")
		if len(src.PmmServers) > 0 {
			cfg.PmmServers = make(map[string]PmmServerConfig, len(src.PmmServers))
			for _, ns := range sortedPmmServers(src.PmmServers) {
				svc := ns.Config
				port := svc.PmmExternalPort
				if port == 0 {
					port = svc.PmmPort
				}
				if _, taken := occupied[port]; port != 0 && taken {
					svc.PmmExternalPort = nextFreeDockerPort(port, occupied)
					port = svc.PmmExternalPort
				}
				if port != 0 {
					occupied[port] = struct{}{}
				}
				cfg.PmmServers[ns.Name] = svc
			}
		}
		if len(src.SeaweedFSServers) > 0 {
			cfg.SeaweedFSServers = make(map[string]SeaweedFSServerConfig, len(src.SeaweedFSServers))
			for _, ns := range sortedSeaweedFSServers(src.SeaweedFSServers) {
				svc := ns.Config
				_, apiTaken := occupied[svc.SeaweedFSPort]
				_, adminTaken := occupied[svc.SeaweedFSAdminPort]
				if (svc.SeaweedFSPort != 0 && apiTaken) || (svc.SeaweedFSAdminPort != 0 && adminTaken) {
					svc.SeaweedFSPort, svc.SeaweedFSAdminPort = nextFreeSeaweedFSPorts(svc.SeaweedFSPort, occupied)
				}
				if svc.SeaweedFSPort != 0 {
					occupied[svc.SeaweedFSPort] = struct{}{}
				}
				if svc.SeaweedFSAdminPort != 0 {
					occupied[svc.SeaweedFSAdminPort] = struct{}{}
				}
				cfg.SeaweedFSServers[ns.Name] = svc
			}
		}
	}
	return cfg
}
