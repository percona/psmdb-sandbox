package main

type chaosHardwareField struct {
	Name    string
	Label   string
	Value   int
	Options []int
}

// CHAOS resource settings are environment-wide, not per topology. Render each
// field once, including when there are no clusters/replica sets yet.
func chaosHardwareFields(cfg Config) []chaosHardwareField {
	fields := []chaosHardwareField{
		{"shardsvr_cpu_cores", "Shard server vCPU", cfg.ShardsvrCpuCores, []int{2, 4, 8}},
		{"shardsvr_memory_gb", "Shard server memory (GB)", cfg.ShardsvrMemoryGb, []int{4, 8, 16}},
		{"shardsvr_volume_size", "Shard server disk (GB)", cfg.ShardsvrVolumeSize, []int{20, 40, 60, 80, 100}},
		{"configsvr_cpu_cores", "Config server vCPU", cfg.ConfigsvrCpuCores, []int{2, 4, 8}},
		{"configsvr_memory_gb", "Config server memory (GB)", cfg.ConfigsvrMemoryGb, []int{4, 8, 16}},
		{"configsvr_volume_size", "Config server disk (GB)", cfg.ConfigsvrVolumeSize, []int{20, 40, 60, 80, 100}},
		{"mongos_cpu_cores", "Router vCPU", cfg.MongosCpuCores, []int{2, 4, 8}},
		{"mongos_memory_gb", "Router memory (GB)", cfg.MongosMemoryGb, []int{4, 8, 16}},
		{"arbiter_cpu_cores", "Arbiter vCPU", cfg.ArbiterCpuCores, []int{2, 4, 8}},
		{"arbiter_memory_gb", "Arbiter memory (GB)", cfg.ArbiterMemoryGb, []int{4, 8, 16}},
		{"replsetsvr_cpu_cores", "Replica set data node vCPU", cfg.ReplsetSvrCpuCores, []int{2, 4, 8}},
		{"replsetsvr_memory_gb", "Replica set data node memory (GB)", cfg.ReplsetSvrMemoryGb, []int{4, 8, 16}},
		{"replsetsvr_volume_size", "Replica set data node disk (GB)", cfg.ReplsetSvrVolumeSize, []int{20, 40, 60, 80, 100}},
	}
	for i := range fields {
		field := &fields[i]
		if field.Value == 0 {
			field.Value = field.Options[0]
		}
		found := false
		for _, option := range field.Options {
			if option == field.Value {
				found = true
			}
		}
		if !found {
			field.Options = append(field.Options, field.Value)
		}
	}
	return fields
}
