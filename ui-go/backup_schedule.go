package main

import (
	"fmt"
	"regexp"
	"strings"
)

var cronFieldRE = regexp.MustCompile(`^[0-9*/,\-]+$`)

// backupScheduleAnsibleVars returns the Ansible variables set by the backup
// schedule and retention settings. Unset values keep the group_vars defaults.
func backupScheduleAnsibleVars(cfg Config) map[string]string {
	vars := map[string]string{}
	if cfg.BackupSchedule != "" {
		vars["pbm_backup_schedule"] = cfg.BackupSchedule
	}
	if cfg.BackupPurgeSchedule != "" {
		vars["pbm_purge_schedule"] = cfg.BackupPurgeSchedule
	}
	if cfg.BackupRetention > 0 {
		vars["pbm_retention_days"] = fmt.Sprint(cfg.BackupRetention)
	}
	return vars
}

// backupActive reports whether PBM backups are enabled on a cloud/CHAOS platform.
func backupActive(platform string, cfg Config) bool {
	switch platform {
	case "chaos":
		return chaosBackupStorage(cfg) != chaosBackupNone
	case "aws", "gcp", "azure":
		return backupEnabled(cfg)
	}
	return false
}

// validateCronSchedule checks for a numeric 5-field cron expression
// (minute hour day-of-month month day-of-week). The Ansible role enforces the
// same rule before writing the cron files.
func validateCronSchedule(label, schedule string) error {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return fmt.Errorf("%s must be a 5-field cron expression (minute hour day-of-month month day-of-week), got %q", label, schedule)
	}
	for _, field := range fields {
		if !cronFieldRE.MatchString(field) {
			return fmt.Errorf("%s field %q is invalid: use only digits and the characters * / , -", label, field)
		}
	}
	return nil
}

// normalizeAndValidateBackupSchedule cleans up and validates the backup
// schedules. Schedules are cleared when backups are disabled, since the form
// keeps submitting hidden inputs, and rejected on platforms without PBM cron
// jobs (Docker).
func normalizeAndValidateBackupSchedule(platform string, cfg *Config) error {
	cfg.BackupSchedule = strings.Join(strings.Fields(cfg.BackupSchedule), " ")
	cfg.BackupPurgeSchedule = strings.Join(strings.Fields(cfg.BackupPurgeSchedule), " ")
	if platform != "aws" && platform != "gcp" && platform != "azure" && platform != "chaos" {
		if cfg.BackupSchedule != "" || cfg.BackupPurgeSchedule != "" {
			return fmt.Errorf("backup schedules are supported only on AWS, GCP, Azure and CHAOS environments")
		}
		return nil
	}
	if !backupActive(platform, *cfg) {
		cfg.BackupSchedule = ""
		cfg.BackupPurgeSchedule = ""
		return nil
	}
	if cfg.BackupRetention < 0 {
		return fmt.Errorf("backup retention must be at least 1 day")
	}
	for key, schedule := range map[string]string{"pbm_backup_schedule": cfg.BackupSchedule, "pbm_purge_schedule": cfg.BackupPurgeSchedule} {
		if schedule == "" {
			continue
		}
		if _, exists := cfg.AnsibleVars[key]; exists {
			return fmt.Errorf("Ansible variable %q is managed by the backup schedule settings and cannot be overridden", key)
		}
	}
	if cfg.BackupSchedule != "" {
		if err := validateCronSchedule("backup schedule", cfg.BackupSchedule); err != nil {
			return err
		}
	}
	if cfg.BackupPurgeSchedule != "" {
		if err := validateCronSchedule("backup cleanup schedule", cfg.BackupPurgeSchedule); err != nil {
			return err
		}
	}
	return nil
}
