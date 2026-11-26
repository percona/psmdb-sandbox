package main

import (
	"regexp"
	"strconv"
	"strings"
)

// TopologyDiagramState is the input of buildTopologyGraph in
// static/topology-diagram.js. The configuration editor builds it from the form;
// the environment page builds it from the saved configuration.
type TopologyDiagramState struct {
	Platform   string                    `json:"platform"`
	Topologies []TopologyDiagramTopology `json:"topologies"`
	Services   TopologyDiagramServices   `json:"services"`
}

type TopologyDiagramTopology struct {
	Ref          string `json:"ref"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Distribution string `json:"distribution"`
	Shards       int    `json:"shards,omitempty"`
	Members      int    `json:"members"`
	Arbiters     int    `json:"arbiters"`
	Configs      int    `json:"configs,omitempty"`
	Routers      int    `json:"routers,omitempty"`
	Monitoring   bool   `json:"monitoring"`
	Backups      bool   `json:"backups"`
	TLS          bool   `json:"tls"`
	Search       bool   `json:"search"`
	Toolkit      bool   `json:"toolkit"`
	Audit        bool   `json:"audit"`
}

type TopologyDiagramServices struct {
	Monitoring  bool   `json:"monitoring"`
	Backups     bool   `json:"backups"`
	BackupMode  string `json:"backupMode,omitempty"`
	Load        bool   `json:"load"`
	Sync        bool   `json:"sync"`
	SyncSource  string `json:"syncSource,omitempty"`
	SyncTarget  string `json:"syncTarget,omitempty"`
	Clients     int    `json:"clients"`
	Vault       bool   `json:"vault"`
	CA          bool   `json:"ca"`
	CAPlacement string `json:"caPlacement,omitempty"`
	LDAP        bool   `json:"ldap"`
}

var twoDigitRelease = regexp.MustCompile(`^(\d)(\d)$`)

// diagramVersion matches the version line of the configuration editor.
func diagramVersion(platform string, cfg Config, image, distribution, release, version string) string {
	if platform == "docker" {
		return strDefault(image, "PSMDB (default image)")
	}
	label := map[string]string{"psmdb": "PSMDB", "community": "Community", "enterprise": "Enterprise"}[distribution]
	if label == "" {
		label = distribution
	}
	if version == "" {
		version = twoDigitRelease.ReplaceAllString(strings.TrimPrefix(strDefault(release, cfg.MongoRelease), "psmdb-"), "$1.$2")
	}
	return strings.TrimSpace(label + " " + version)
}

func diagramDistribution(platform string, cfg Config, distribution string) string {
	if platform == "docker" {
		return "psmdb"
	}
	return strDefault(distribution, strDefault(cfg.MongoDBDistribution, "psmdb"))
}

// environmentTopologyDiagram describes a saved environment for the topology
// diagram. Refs follow the editor's order so they can select the same blocks.
func environmentTopologyDiagram(platform string, cfg Config) TopologyDiagramState {
	state := TopologyDiagramState{Platform: platform, Topologies: []TopologyDiagramTopology{}}
	for i, nc := range sortedClusters(cfg.Clusters) {
		c := nc.Config
		distribution := diagramDistribution(platform, cfg, c.MongoDBDistribution)
		state.Topologies = append(state.Topologies, TopologyDiagramTopology{
			Ref: "cluster:" + strconv.Itoa(i), Kind: "cluster", Name: nc.Name,
			Version:      diagramVersion(platform, cfg, c.PsmdbImage, distribution, c.MongoRelease, c.MongoVersion),
			Distribution: distribution,
			Shards:       intDefault(c.ShardCount, 2), Members: intDefault(c.ShardsvrReplicas, 2),
			Arbiters: intPtrDefault(c.ArbitersPerReplset, 1), Configs: intDefault(c.ConfigsvrCount, 3),
			Routers:    intDefault(c.MongosCount, 2),
			Monitoring: boolDefault(c.EnablePmm, false), Backups: boolDefault(c.EnablePbm, false),
			TLS: boolDefault(c.UseTLS, cfg.UseTLS), Search: boolDefault(c.EnableMongot, false),
			Toolkit: platform != "docker" && boolDefault(c.EnableToolkit, false), Audit: boolDefault(c.EnableAudit, false),
		})
	}
	for i, nr := range sortedReplsets(cfg.Replsets) {
		r := nr.Config
		distribution := diagramDistribution(platform, cfg, r.MongoDBDistribution)
		state.Topologies = append(state.Topologies, TopologyDiagramTopology{
			Ref: "replset:" + strconv.Itoa(i), Kind: "replset", Name: nr.Name,
			Version:      diagramVersion(platform, cfg, r.PsmdbImage, distribution, r.MongoRelease, r.MongoVersion),
			Distribution: distribution,
			Members:      intDefault(r.DataNodesPerReplset, 2), Arbiters: intPtrDefault(r.ArbitersPerReplset, 1),
			Monitoring: boolDefault(r.EnablePmm, false), Backups: boolDefault(r.EnablePbm, false),
			TLS: boolDefault(r.UseTLS, cfg.UseTLS), Search: boolDefault(r.EnableMongot, false),
			Toolkit: platform != "docker" && boolDefault(r.EnableToolkit, false), Audit: boolDefault(r.EnableAudit, false),
		})
	}

	svc := TopologyDiagramServices{
		Load:        cfg.EnablePlgm,
		Clients:     len(cfg.PmmBareClients),
		Vault:       cfg.VaultEncryption,
		CA:          boolDefault(cfg.EnableCA, false),
		CAPlacement: strDefault(cfg.CAPlacement, "dedicated"),
		LDAP:        len(cfg.LdapServers) > 0,
	}
	switch platform {
	case "docker":
		svc.Monitoring = len(cfg.PmmServers) > 0
		svc.Backups = len(cfg.SeaweedFSServers) > 0
	case "chaos":
		svc.Monitoring = boolDefault(cfg.EnablePmm, false)
		svc.BackupMode = chaosBackupStorage(cfg)
		svc.Backups = svc.BackupMode != chaosBackupNone
	default:
		svc.Monitoring = boolDefault(cfg.EnablePmm, false)
		svc.Backups = backupEnabled(cfg)
		svc.BackupMode = "existing"
		if boolDefault(cfg.CreateBackupBucket, true) {
			svc.BackupMode = "create"
		}
	}
	if cs := cfg.ClusterSync; cs.Enabled {
		svc.Sync = true
		svc.SyncSource = cs.SourceKind + ":" + cs.SourceName
		svc.SyncTarget = cs.TargetKind + ":" + cs.TargetName
	}
	state.Services = svc
	return state
}
