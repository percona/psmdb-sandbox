package main

import (
	"html/template"
	"os"
	"strings"
	"testing"
)

func TestWriteTfvarsCloudCreateBackupBucket(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cases := []struct {
		name   string
		enable *bool
		val    *bool
		want   string
	}{
		{"default", nil, nil, "create_backup_bucket = true"},
		{"enabled", nil, boolPtr(true), "create_backup_bucket = true"},
		{"disabled", nil, boolPtr(false), "create_backup_bucket = false"},
		{"backup on", boolPtr(true), nil, "create_backup_bucket = true"},
		{"backup off", boolPtr(false), boolPtr(true), "create_backup_bucket = false"},
	}
	for _, platform := range []string{"aws", "gcp", "azure"} {
		for _, tc := range cases {
			envID := platform + "-bucket-" + strings.ReplaceAll(tc.name, " ", "-")
			cfg := Config{
				EnableBackup:       tc.enable,
				CreateBackupBucket: tc.val,
				Clusters:           map[string]ClusterConfig{"cl01": {EnvTag: "test"}},
			}
			if err := writeTfvars(envID, platform, cfg); err != nil {
				t.Fatalf("%s: writeTfvars failed: %v", envID, err)
			}
			content, err := os.ReadFile(tfvarsPath(envID, platform))
			if err != nil {
				t.Fatalf("%s: read tfvars failed: %v", envID, err)
			}
			if !strings.Contains(string(content), tc.want) {
				t.Fatalf("%s: expected %q in tfvars:\n%s", envID, tc.want, content)
			}
		}
	}
}

func TestWriteTfvarsExistingBackupBucket(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cases := map[string]struct {
		cfg     Config
		want    []string
		notWant []string
	}{
		"aws": {
			cfg: Config{ExistingBucketName: "my-bkp", ExistingBucketRegion: "eu-west-1",
				ExistingBucketAccessKey: "AKIA123", ExistingBucketSecretKey: "s3cr3t", ExistingStorageAccount: "ignored"},
			want: []string{`existing_bucket_name = "my-bkp"`, `existing_bucket_region = "eu-west-1"`,
				`existing_bucket_access_key = "AKIA123"`, `existing_bucket_secret_key = "s3cr3t"`},
			notWant: []string{"existing_storage_account"},
		},
		"gcp": {
			cfg: Config{ExistingBucketName: "my-bkp", ExistingBucketRegion: "ignored",
				ExistingBucketAccessKey: "GOOG123", ExistingBucketSecretKey: "s3cr3t"},
			want:    []string{`existing_bucket_name = "my-bkp"`, `existing_bucket_access_key = "GOOG123"`, `existing_bucket_secret_key = "s3cr3t"`},
			notWant: []string{"existing_bucket_region", "existing_storage_account"},
		},
		"azure": {
			cfg: Config{ExistingBucketName: "backups", ExistingStorageAccount: "myacct",
				ExistingBucketAccessKey: "ignored", ExistingBucketSecretKey: "acctkey=="},
			want:    []string{`existing_bucket_name = "backups"`, `existing_storage_account = "myacct"`, `existing_bucket_secret_key = "acctkey=="`},
			notWant: []string{"existing_bucket_region", "existing_bucket_access_key"},
		},
	}
	for platform, tc := range cases {
		cfg := tc.cfg
		cfg.CreateBackupBucket = boolPtr(false)
		cfg.Clusters = map[string]ClusterConfig{"cl01": {EnvTag: "test"}}
		envID := platform + "-existing-bucket"
		if err := writeTfvars(envID, platform, cfg); err != nil {
			t.Fatalf("%s: writeTfvars failed: %v", platform, err)
		}
		content, err := os.ReadFile(tfvarsPath(envID, platform))
		if err != nil {
			t.Fatalf("%s: read tfvars failed: %v", platform, err)
		}
		tfvars := string(content)
		for _, w := range append(tc.want, "create_backup_bucket = false") {
			if !strings.Contains(tfvars, w) {
				t.Fatalf("%s: expected %q in tfvars:\n%s", platform, w, tfvars)
			}
		}
		for _, nw := range tc.notWant {
			if strings.Contains(tfvars, nw) {
				t.Fatalf("%s: did not expect %q in tfvars:\n%s", platform, nw, tfvars)
			}
		}

		// Existing-bucket settings are ignored when backups are disabled.
		cfg.EnableBackup = boolPtr(false)
		if err := writeTfvars(envID, platform, cfg); err != nil {
			t.Fatalf("%s: writeTfvars failed: %v", platform, err)
		}
		content, _ = os.ReadFile(tfvarsPath(envID, platform))
		if strings.Contains(string(content), "existing_") || !strings.Contains(string(content), "create_backup_bucket = false") {
			t.Fatalf("%s: expected no backup storage when backups are disabled:\n%s", platform, content)
		}

		// Existing-bucket settings are ignored when the bucket is created.
		cfg.EnableBackup = boolPtr(true)
		cfg.CreateBackupBucket = boolPtr(true)
		if err := writeTfvars(envID, platform, cfg); err != nil {
			t.Fatalf("%s: writeTfvars failed: %v", platform, err)
		}
		content, _ = os.ReadFile(tfvarsPath(envID, platform))
		if strings.Contains(string(content), "existing_") {
			t.Fatalf("%s: did not expect existing_* vars when creating bucket:\n%s", platform, content)
		}
	}
}

func TestBackupEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"legacy default", Config{}, true},
		{"legacy existing bucket", Config{CreateBackupBucket: boolPtr(false), ExistingBucketName: "b"}, true},
		{"legacy no storage", Config{CreateBackupBucket: boolPtr(false)}, false},
		{"explicit on", Config{EnableBackup: boolPtr(true), CreateBackupBucket: boolPtr(false)}, true},
		{"explicit off", Config{EnableBackup: boolPtr(false)}, false},
	}
	for _, tc := range cases {
		if got := backupEnabled(tc.cfg); got != tc.want {
			t.Errorf("%s: backupEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestValidateBackupBucketConfig(t *testing.T) {
	off := boolPtr(false)
	cases := []struct {
		name     string
		platform string
		cfg      Config
		wantErr  bool
	}{
		{"create default", "aws", Config{}, false},
		{"no bucket", "aws", Config{CreateBackupBucket: off}, false},
		{"aws complete", "aws", Config{CreateBackupBucket: off, ExistingBucketName: "b", ExistingBucketAccessKey: "a", ExistingBucketSecretKey: "s"}, false},
		{"aws missing secret", "aws", Config{CreateBackupBucket: off, ExistingBucketName: "b", ExistingBucketAccessKey: "a"}, true},
		{"gcp missing access", "gcp", Config{CreateBackupBucket: off, ExistingBucketName: "b", ExistingBucketSecretKey: "s"}, true},
		{"azure complete", "azure", Config{CreateBackupBucket: off, ExistingBucketName: "c", ExistingStorageAccount: "acct", ExistingBucketSecretKey: "k"}, false},
		{"azure missing account", "azure", Config{CreateBackupBucket: off, ExistingBucketName: "c", ExistingBucketSecretKey: "k"}, true},
		{"chaos ignored", "chaos", Config{CreateBackupBucket: off, ExistingBucketName: "b"}, false},
		{"enabled existing without name", "aws", Config{EnableBackup: boolPtr(true), CreateBackupBucket: off}, true},
		{"disabled existing without name", "aws", Config{EnableBackup: off, CreateBackupBucket: off}, false},
		{"disabled ignores incomplete bucket", "gcp", Config{EnableBackup: off, CreateBackupBucket: off, ExistingBucketName: "b"}, false},
	}
	for _, tc := range cases {
		err := validateBackupBucketConfig(tc.platform, tc.cfg)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestWriteTfvarsChaosOmitsCreateBackupBucket(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	cfg := Config{
		CreateBackupBucket: boolPtr(false),
		Clusters:           map[string]ClusterConfig{"cl01": {EnvTag: "test"}},
	}
	if err := writeTfvars("chaos-bucket", "chaos", cfg); err != nil {
		t.Fatalf("writeTfvars failed: %v", err)
	}
	content, err := os.ReadFile(tfvarsPath("chaos-bucket", "chaos"))
	if err != nil {
		t.Fatalf("read tfvars failed: %v", err)
	}
	if strings.Contains(string(content), "create_backup_bucket") {
		t.Fatalf("did not expect create_backup_bucket in CHAOS tfvars:\n%s", content)
	}
}

func TestConfigureTemplateRendersBackupBucketOptions(t *testing.T) {
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(uiAssets, "templates/layout.html", "templates/configure.html")
	if err != nil {
		t.Fatal(err)
	}
	expect := map[string][]string{
		"aws":   {`id="existing_bucket_region"`, `id="existing_bucket_access_key"`, "Secret Access Key"},
		"gcp":   {"HMAC Access ID", "HMAC Secret"},
		"azure": {`id="existing_storage_account"`, "Storage Account Key", "Container Name"},
	}
	for platform, wants := range expect {
		for _, enable := range []bool{true, false} {
			for _, create := range []bool{true, false} {
				cfg := Config{EnableBackup: boolPtr(enable), CreateBackupBucket: boolPtr(create), ExistingBucketName: "my-existing-bkp"}
				data := ConfigureData{Platform: platform, Config: cfg, DefaultImages: defaultImages}
				var out strings.Builder
				if err := tmpl.ExecuteTemplate(&out, "layout", data); err != nil {
					t.Fatalf("%s: execute: %v", platform, err)
				}
				page := out.String()
				wants := append([]string{`id="enable_backup"`, `id="backup_bucket_mode"`, `value="my-existing-bkp"`}, wants...)
				for _, want := range wants {
					if !strings.Contains(page, want) {
						t.Errorf("%s (enable=%v create=%v): expected %q in the rendered page", platform, enable, create, want)
					}
				}
				hidden := `<div id="existing-bucket-body" data-backup-mode="existing" style="display:none">`
				if create != strings.Contains(page, hidden) {
					t.Errorf("%s (enable=%v create=%v): unexpected existing-bucket visibility", platform, enable, create)
				}
				hiddenSettings := `<div id="backup-settings-body" style="display:none">`
				if enable == strings.Contains(page, hiddenSettings) {
					t.Errorf("%s (enable=%v create=%v): unexpected backup settings visibility", platform, enable, create)
				}
				existingSelected := `<option value="existing" selected>`
				if create == strings.Contains(page, existingSelected) {
					t.Errorf("%s (enable=%v create=%v): unexpected bucket mode selection", platform, enable, create)
				}
			}
		}
	}
	data := ConfigureData{Platform: "chaos", Config: Config{}, DefaultImages: defaultImages}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "layout", data); err != nil {
		t.Fatalf("chaos: execute: %v", err)
	}
	// CHAOS has its own section: SeaweedFS instead of a cloud bucket.
	if strings.Contains(out.String(), `id="backup-bucket-body"`) || strings.Contains(out.String(), `<option value="create"`) {
		t.Error("chaos: did not expect the cloud bucket options")
	}
}

func TestChaosBackupStorage(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"legacy off", Config{}, chaosBackupNone},
		{"legacy seaweedfs", Config{EnableSeaweedFS: boolPtr(true)}, chaosBackupSeaweedFS},
		{"off", Config{EnableBackup: boolPtr(false), EnableSeaweedFS: boolPtr(true)}, chaosBackupNone},
		{"seaweedfs default", Config{EnableBackup: boolPtr(true)}, chaosBackupSeaweedFS},
		{"existing", Config{EnableBackup: boolPtr(true), CreateBackupBucket: boolPtr(false)}, chaosBackupExisting},
	}
	for _, tc := range cases {
		if got := chaosBackupStorage(tc.cfg); got != tc.want {
			t.Errorf("%s: chaosBackupStorage = %q, want %q", tc.name, got, tc.want)
		}
	}

	cfg := Config{EnableBackup: boolPtr(true), CreateBackupBucket: boolPtr(false), EnableSeaweedFS: boolPtr(true)}
	normalizeChaosBackup(&cfg)
	if cfg.EnableSeaweedFS == nil || *cfg.EnableSeaweedFS {
		t.Errorf("existing bucket: expected EnableSeaweedFS=false after normalize")
	}
	cfg = Config{EnableBackup: boolPtr(true)}
	normalizeChaosBackup(&cfg)
	if cfg.EnableSeaweedFS == nil || !*cfg.EnableSeaweedFS {
		t.Errorf("seaweedfs: expected EnableSeaweedFS=true after normalize")
	}
}

func TestValidateChaosExistingBucket(t *testing.T) {
	on, off := boolPtr(true), boolPtr(false)
	complete := Config{EnableBackup: on, CreateBackupBucket: off, ExistingBucketName: "b", ExistingBucketAccessKey: "a", ExistingBucketSecretKey: "s"}
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"complete", func(*Config) {}, false},
		{"with endpoint", func(c *Config) { c.ExistingBucketEndpoint = "https://minio.example.com:9000" }, false},
		{"bad endpoint", func(c *Config) { c.ExistingBucketEndpoint = "minio.example.com:9000" }, true},
		{"missing name", func(c *Config) { c.ExistingBucketName = "" }, true},
		{"missing access", func(c *Config) { c.ExistingBucketAccessKey = "" }, true},
		{"missing secret", func(c *Config) { c.ExistingBucketSecretKey = "" }, true},
		{"seaweedfs ignores bucket", func(c *Config) { c.CreateBackupBucket = on; c.ExistingBucketName = "" }, false},
		{"backup off ignores bucket", func(c *Config) { c.EnableBackup = off; c.ExistingBucketName = "" }, false},
	}
	for _, tc := range cases {
		cfg := complete
		tc.mutate(&cfg)
		err := validateBackupBucketConfig("chaos", cfg)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestWriteTfvarsChaosBackupStorage(t *testing.T) {
	dir := t.TempDir()
	origTerraformDir := terraformDir
	terraformDir = dir
	t.Cleanup(func() { terraformDir = origTerraformDir })

	on, off := boolPtr(true), boolPtr(false)
	cases := []struct {
		name    string
		cfg     Config
		want    []string
		notWant []string
	}{
		{
			name:    "off",
			cfg:     Config{EnableBackup: off, ExistingBucketName: "b"},
			want:    []string{"enable_seaweedfs = false"},
			notWant: []string{"existing_", "seaweedfs_port"},
		},
		{
			name:    "seaweedfs",
			cfg:     Config{EnableBackup: on, ExistingBucketName: "b"},
			want:    []string{"enable_seaweedfs = true", "seaweedfs_port = 8333"},
			notWant: []string{"existing_"},
		},
		{
			name: "existing",
			cfg: Config{EnableBackup: on, CreateBackupBucket: off, ExistingBucketName: "my-bkp",
				ExistingBucketEndpoint: "https://minio.example.com:9000", ExistingBucketRegion: "eu-west-1",
				ExistingBucketAccessKey: "AK", ExistingBucketSecretKey: "s3cr3t", SeaweedFSPort: 8333},
			want: []string{"enable_seaweedfs = false", `existing_bucket_name = "my-bkp"`,
				`existing_bucket_endpoint = "https://minio.example.com:9000"`, `existing_bucket_region = "eu-west-1"`,
				`existing_bucket_access_key = "AK"`, `existing_bucket_secret_key = "s3cr3t"`},
			notWant: []string{"seaweedfs_port", "create_backup_bucket"},
		},
	}
	for _, tc := range cases {
		cfg := tc.cfg
		cfg.Clusters = map[string]ClusterConfig{"cl01": {EnvTag: "test"}}
		envID := "chaos-backup-" + tc.name
		if err := writeTfvars(envID, "chaos", cfg); err != nil {
			t.Fatalf("%s: writeTfvars failed: %v", tc.name, err)
		}
		content, err := os.ReadFile(tfvarsPath(envID, "chaos"))
		if err != nil {
			t.Fatalf("%s: read tfvars failed: %v", tc.name, err)
		}
		tfvars := string(content)
		for _, w := range tc.want {
			if !strings.Contains(tfvars, w) {
				t.Errorf("%s: expected %q in tfvars:\n%s", tc.name, w, tfvars)
			}
		}
		for _, nw := range tc.notWant {
			if strings.Contains(tfvars, nw) {
				t.Errorf("%s: did not expect %q in tfvars:\n%s", tc.name, nw, tfvars)
			}
		}
	}
}

func TestConfigureTemplateRendersChaosBackupOptions(t *testing.T) {
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(uiAssets, "templates/layout.html", "templates/configure.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, enable := range []bool{true, false} {
		for _, seaweedfs := range []bool{true, false} {
			cfg := Config{EnableBackup: boolPtr(enable), CreateBackupBucket: boolPtr(seaweedfs),
				ExistingBucketName: "my-existing-bkp", ExistingBucketEndpoint: "https://s3.example.com"}
			var out strings.Builder
			if err := tmpl.ExecuteTemplate(&out, "layout", ConfigureData{Platform: "chaos", Config: cfg, DefaultImages: defaultImages}); err != nil {
				t.Fatalf("execute: %v", err)
			}
			page := out.String()
			for _, want := range []string{`id="enable_backup"`, `id="backup_bucket_mode"`, `<option value="seaweedfs"`,
				`id="existing_bucket_endpoint"`, `value="my-existing-bkp"`, `value="https://s3.example.com"`, `id="seaweedfs_port"`} {
				if !strings.Contains(page, want) {
					t.Errorf("enable=%v seaweedfs=%v: expected %q", enable, seaweedfs, want)
				}
			}
			if strings.Contains(page, `id="enable_seaweedfs"`) {
				t.Errorf("enable=%v seaweedfs=%v: the SeaweedFS checkbox should be replaced by the storage selector", enable, seaweedfs)
			}
			if enable == strings.Contains(page, `<div id="backup-settings-body" style="display:none">`) {
				t.Errorf("enable=%v seaweedfs=%v: unexpected backup settings visibility", enable, seaweedfs)
			}
			if seaweedfs != strings.Contains(page, `<div id="existing-bucket-body" data-backup-mode="existing" style="display:none">`) {
				t.Errorf("enable=%v seaweedfs=%v: unexpected existing-bucket visibility", enable, seaweedfs)
			}
			if seaweedfs == strings.Contains(page, `<div id="seaweedfs-vm-body" data-backup-mode="seaweedfs" style="display:none">`) {
				t.Errorf("enable=%v seaweedfs=%v: unexpected SeaweedFS visibility", enable, seaweedfs)
			}
		}
	}
}
