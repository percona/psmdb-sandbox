package main

import (
	"encoding/json"
	"fmt"
	"sort"
)

type topologyScaleOut struct {
	NewClusters       []string
	NewReplsets       []string
	AddedShards       map[string][]int
	AddedReplsetNodes map[string]int
}

func (t topologyScaleOut) hasChanges() bool {
	return len(t.NewClusters) > 0 || len(t.NewReplsets) > 0 || len(t.AddedShards) > 0 || len(t.AddedReplsetNodes) > 0
}

func analyseTopologyChange(previous, desired Config) (topologyScaleOut, []string) {
	plan := topologyScaleOut{
		AddedShards:       map[string][]int{},
		AddedReplsetNodes: map[string]int{},
	}
	var unsupported []string
	if previous.VaultEncryption != desired.VaultEncryption {
		unsupported = append(unsupported, "changing environment encryption at rest requires migration or recreation")
	}

	for name, next := range desired.Clusters {
		prior, exists := previous.Clusters[name]
		if !exists {
			plan.NewClusters = append(plan.NewClusters, name)
			continue
		}

		oldCfgCount := intDefault(prior.ConfigsvrCount, 3)
		newCfgCount := intDefault(next.ConfigsvrCount, 3)
		if oldCfgCount != newCfgCount {
			unsupported = append(unsupported, fmt.Sprintf("cluster %q: changing config server count from %d to %d is not supported", name, oldCfgCount, newCfgCount))
		}

		oldReplicas := intDefault(prior.ShardsvrReplicas, 2)
		newReplicas := intDefault(next.ShardsvrReplicas, 2)
		if oldReplicas != newReplicas {
			unsupported = append(unsupported, fmt.Sprintf("cluster %q: changing replicas per shard from %d to %d is not supported yet", name, oldReplicas, newReplicas))
		}

		oldArbiters := intPtrDefault(prior.ArbitersPerReplset, 1)
		newArbiters := intPtrDefault(next.ArbitersPerReplset, 1)
		if oldArbiters != newArbiters {
			unsupported = append(unsupported, fmt.Sprintf("cluster %q: changing arbiters per shard from %d to %d is not supported yet", name, oldArbiters, newArbiters))
		}

		oldShards := intDefault(prior.ShardCount, 2)
		newShards := intDefault(next.ShardCount, 2)
		switch {
		case newShards < oldShards:
			unsupported = append(unsupported, fmt.Sprintf("cluster %q: reducing shard count from %d to %d is not implemented", name, oldShards, newShards))
		case newShards > oldShards:
			for shardIndex := oldShards; shardIndex < newShards; shardIndex++ {
				plan.AddedShards[name] = append(plan.AddedShards[name], shardIndex)
			}
		}
	}

	for name := range previous.Clusters {
		if _, exists := desired.Clusters[name]; !exists {
			unsupported = append(unsupported, fmt.Sprintf("cluster %q: removing a sharded cluster is not supported as a topology change; use Destroy for the environment", name))
		}
	}

	for name, next := range desired.Replsets {
		prior, exists := previous.Replsets[name]
		if !exists {
			plan.NewReplsets = append(plan.NewReplsets, name)
			continue
		}

		oldNodes := intDefault(prior.DataNodesPerReplset, 2)
		newNodes := intDefault(next.DataNodesPerReplset, 2)
		switch {
		case newNodes < oldNodes:
			unsupported = append(unsupported, fmt.Sprintf("replica set %q: reducing data-bearing nodes from %d to %d is not implemented", name, oldNodes, newNodes))
		case newNodes > oldNodes:
			plan.AddedReplsetNodes[name] = newNodes - oldNodes
		}

		oldArbiters := intPtrDefault(prior.ArbitersPerReplset, 1)
		newArbiters := intPtrDefault(next.ArbitersPerReplset, 1)
		if oldArbiters != newArbiters {
			unsupported = append(unsupported, fmt.Sprintf("replica set %q: changing arbiters from %d to %d is not supported yet", name, oldArbiters, newArbiters))
		}
	}

	for name := range previous.Replsets {
		if _, exists := desired.Replsets[name]; !exists {
			unsupported = append(unsupported, fmt.Sprintf("replica set %q: removing a replica set is not supported as a topology change; use Destroy for the environment", name))
		}
	}

	sort.Strings(plan.NewClusters)
	sort.Strings(plan.NewReplsets)
	for _, shards := range plan.AddedShards {
		sort.Ints(shards)
	}
	return plan, unsupported
}

func cloneConfig(cfg Config) *Config {
	// Config contains maps, slices and pointers in several nested structures.
	// Copy all of them so a running job's snapshot cannot change underneath it.
	b, err := json.Marshal(cfg)
	if err != nil {
		panic(err) // Config contains only JSON-serializable fields.
	}
	var copy Config
	if err := json.Unmarshal(b, &copy); err != nil {
		panic(err)
	}
	return &copy
}
