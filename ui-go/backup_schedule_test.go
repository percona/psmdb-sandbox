package main

import (
	"strings"
	"testing"
)

func TestValidateCronSchedule(t *testing.T) {
	for _, valid := range []string{"2 0 * * *", "0 */6 * * *", "30 1 * * 0", "0,30 8-18 * * 1-5", "  5   4 1 * * "} {
		if err := validateCronSchedule("schedule", valid); err != nil {
			t.Errorf("%q should be valid: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "* * * *", "* * * * * *", "0 0 * * mon", "@daily", "0 0 * * *; rm -rf /", "$(id) 0 * * *"} {
		if err := validateCronSchedule("schedule", invalid); err == nil {
			t.Errorf("%q should be rejected", invalid)
		}
	}
}

func TestNormalizeAndValidateBackupSchedule(t *testing.T) {
	on, off := boolPtr(true), boolPtr(false)
	tests := []struct {
		name       string
		platform   string
		cfg        Config
		wantErr    string
		wantBackup string
		wantPurge  string
	}{
		{name: "valid aws", platform: "aws", cfg: Config{EnableBackup: on, BackupSchedule: " 0  */6 * * * ", BackupPurgeSchedule: "0 3 * * 0"}, wantBackup: "0 */6 * * *", wantPurge: "0 3 * * 0"},
		{name: "empty keeps defaults", platform: "gcp", cfg: Config{EnableBackup: on}},
		{name: "legacy env without toggle", platform: "azure", cfg: Config{BackupSchedule: "0 1 * * *"}, wantBackup: "0 1 * * *"},
		{name: "chaos seaweedfs", platform: "chaos", cfg: Config{EnableBackup: on, BackupSchedule: "15 2 * * *"}, wantBackup: "15 2 * * *"},
		{name: "chaos existing bucket", platform: "chaos", cfg: Config{EnableBackup: on, CreateBackupBucket: off, BackupSchedule: "15 2 * * *"}, wantBackup: "15 2 * * *"},
		{name: "backups off clears schedules", platform: "aws", cfg: Config{EnableBackup: off, BackupSchedule: "nonsense", BackupPurgeSchedule: "0 3 * * *"}},
		{name: "invalid backup schedule", platform: "aws", cfg: Config{EnableBackup: on, BackupSchedule: "every day"}, wantErr: "backup schedule"},
		{name: "invalid purge schedule", platform: "aws", cfg: Config{EnableBackup: on, BackupPurgeSchedule: "0 0 * * mon"}, wantErr: "cleanup schedule"},
		{name: "docker rejects schedule", platform: "docker", cfg: Config{BackupSchedule: "0 1 * * *"}, wantErr: "only on AWS"},
		{name: "docker without schedule", platform: "docker", cfg: Config{}},
		{name: "negative retention", platform: "aws", cfg: Config{EnableBackup: on, BackupRetention: -1}, wantErr: "retention"},
		{name: "raw override conflict", platform: "aws", cfg: Config{EnableBackup: on, BackupSchedule: "0 1 * * *", AnsibleVars: map[string]string{"pbm_backup_schedule": "0 2 * * *"}}, wantErr: "cannot be overridden"},
		{name: "raw override without ui value", platform: "aws", cfg: Config{EnableBackup: on, AnsibleVars: map[string]string{"pbm_backup_schedule": "0 2 * * *"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := normalizeAndValidateBackupSchedule(tt.platform, &tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.cfg.BackupSchedule != tt.wantBackup || tt.cfg.BackupPurgeSchedule != tt.wantPurge {
				t.Fatalf("schedules = %q / %q, want %q / %q", tt.cfg.BackupSchedule, tt.cfg.BackupPurgeSchedule, tt.wantBackup, tt.wantPurge)
			}
		})
	}
}

func TestTopologyAnsibleVarsBackupSchedule(t *testing.T) {
	on, off := boolPtr(true), boolPtr(false)
	cfg := Config{EnableBackup: on, BackupSchedule: "0 */6 * * *", BackupPurgeSchedule: "0 3 * * 0", BackupRetention: 7}
	for _, platform := range []string{"aws", "gcp", "azure", "chaos"} {
		vars := topologyAnsibleVars(cfg, platform, "rs01", nil)
		if vars["pbm_backup_schedule"] != "0 */6 * * *" || vars["pbm_purge_schedule"] != "0 3 * * 0" || vars["pbm_retention_days"] != "7" {
			t.Fatalf("%s vars = %#v", platform, vars)
		}
	}

	vars := topologyAnsibleVars(Config{EnableBackup: on}, "aws", "rs01", nil)
	for _, key := range []string{"pbm_backup_schedule", "pbm_purge_schedule", "pbm_retention_days"} {
		if _, ok := vars[key]; ok {
			t.Fatalf("%s must not be set when the UI leaves it empty", key)
		}
	}

	cfg.EnableBackup = off
	vars = topologyAnsibleVars(cfg, "aws", "rs01", nil)
	if _, ok := vars["pbm_backup_schedule"]; ok {
		t.Fatal("schedule must not be passed when backups are disabled")
	}

	if vars := topologyAnsibleVars(Config{BackupSchedule: "0 1 * * *"}, "docker", "rs01", nil); vars["pbm_backup_schedule"] != nil {
		t.Fatal("docker must not receive a backup schedule")
	}
}

func TestProfileKeepsBackupSchedule(t *testing.T) {
	cfg := profileConfig(Config{BackupSchedule: "0 */6 * * *", BackupPurgeSchedule: "0 3 * * 0", BackupRetention: 5})
	if cfg.BackupSchedule != "0 */6 * * *" || cfg.BackupPurgeSchedule != "0 3 * * 0" || cfg.BackupRetention != 5 {
		t.Fatalf("profile lost backup settings: %#v", cfg)
	}
}
