// main.go – PSMDB Sandbox (Go rewrite)
// Runs as a standalone binary.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
)

// uiAssets contains the read-only files needed to serve the web UI.
// Terraform and Ansible remain on disk because external tools execute them.
//
//go:embed templates static
var uiAssets embed.FS

// ─── Constants ───────────────────────────────────────────────────────────────

var platforms = []string{"aws", "gcp", "azure", "chaos", "docker"}

var ansiRe = regexp.MustCompile(`\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)
var safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// defaultPSMDBVersions is used as a fallback when the Percona repo is unreachable.
var defaultPSMDBVersions = []string{"psmdb-83", "psmdb-80", "psmdb-70", "psmdb-60", "psmdb-50", "psmdb-44", "psmdb-42", "psmdb-40", "psmdb-36"}
var defaultPCSMVersions = []string{"0.9.0"}
var defaultMongoDBOfficialVersions = []string{"9.0", "8.3", "8.2", "8.0", "7.0", "6.0"}

// Default Docker image tags used when Docker Hub is unreachable.
var defaultPSMDBImages = []string{
	"8.0.4-1", "8.0.3-1", "7.0.14-9", "7.0.12-7", "7.0.11-6",
	"6.0.19-16", "6.0.18-15", "5.0.30-27", "latest",
}
var defaultPBMImages = []string{
	"2.7.0", "2.6.0", "2.5.0", "2.4.1", "2.3.1", "latest",
}
var defaultPMMServerImages = []string{
	"3.3.0", "3.2.0", "3.1.0", "3.0.0", "2.43.2", "2.43.1", "latest",
}
var defaultPMMClientImages = []string{
	"3.3.0", "3.2.0", "3.1.0", "3.0.0", "2.43.2", "2.43.1", "latest",
}
var defaultMongotImages = []string{"1.70.3-1", "latest"}

const defaultAuditFilter = `{ atype: "authCheck", "param.command": { $in: [ "insert", "update", "delete", "findandmodify" ] }, "users.user": { $not: /^__/ } }`

// ─── Globals ──────────────────────────────────────────────────────────────────

var (
	// Application directories (set in main)
	repoDir        string
	dataDir        string
	terraformDir   string
	ansibleDir     string
	stateFile      string
	settingsFile   string
	jobsDir        string
	pcsmSecretsDir string
)

// ─── Template function map ────────────────────────────────────────────────────

var funcMap = template.FuncMap{
	// Emit a Go value as a JS-safe JSON literal.
	"json": func(v interface{}) (template.JS, error) {
		b, err := json.Marshal(v)
		return template.JS(b), err
	},
	"upper":         strings.ToUpper,
	"hasPrefix":     strings.HasPrefix,
	"join":          strings.Join,
	"defaultRegion": func(platform string) string { return defaultCloudRegion[platform] },
	// Benchmark hardware tunables (see hardware.go).
	"hardwarePresets":    hardwarePresets,
	"hardwareFormFields": func() []string { return hardwareFormFields },
	"diskTypes":          diskTypesFor,
	"machineTypes":       suggestedMachineTypes,
	"cpuPlatforms":       func() []string { return gcpCPUPlatforms },
	// Render a label/tag map as "key=value" lines (sorted) for a textarea.
	"labelsText": func(m map[string]string) string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		lines := make([]string, len(keys))
		for i, k := range keys {
			lines[i] = k + "=" + m[k]
		}
		return strings.Join(lines, "\n")
	},
	"contains": func(values []string, value string) bool {
		for _, candidate := range values {
			if candidate == value {
				return true
			}
		}
		return false
	},
	"preferredPSMDBRelease": func(versions []string) string {
		for _, version := range versions {
			if version == "psmdb-80" {
				return version
			}
		}
		if len(versions) > 0 {
			return versions[0]
		}
		return "psmdb-80"
	},
	// Return s if non-empty, otherwise def.
	"strDefault": func(s, def string) string {
		if s == "" {
			return def
		}
		return s
	},
	// Return n if non-zero, otherwise def.
	"intDefault": func(n, def int) int {
		if n == 0 {
			return def
		}
		return n
	},
	// Return *n if non-nil, otherwise def.
	"intPtrDefault": func(n *int, def int) int {
		if n == nil {
			return def
		}
		return *n
	},
	// Return *b if non-nil, otherwise def.
	"boolDefault": boolDefault,
	// True when PBM backup storage is enabled (AWS/GCP/Azure).
	"backupEnabled": backupEnabled,
	// CHAOS backup storage: "", "seaweedfs" or "existing".
	"chaosBackupStorage": chaosBackupStorage,
	// Return true when b is explicitly set to false.
	"ptrBoolFalse": func(b *bool) bool {
		return b != nil && !*b
	},
	// True if the stored image value matches the given tag (with or without prefix).
	"tagSelected": func(stored, prefix, tag string) bool {
		if stored == "" {
			return tag == "latest"
		}
		if stored == prefix+":"+tag || stored == tag {
			return true
		}
		if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
			return strings.HasSuffix(stored, "/"+prefix[slash+1:]+":"+tag)
		}
		return false
	},
	// Extract the version tag from a Docker image string (the part after the last
	// colon).  Returns "—" for an empty input.
	"imageTag": func(image string) string {
		if image == "" {
			return "—"
		}
		if idx := strings.LastIndex(image, ":"); idx >= 0 && idx < len(image)-1 {
			return image[idx+1:]
		}
		return image
	},
	"imageNamespace": func(image, def string) string {
		if image == "" {
			return def
		}
		withoutTag := image
		if idx := strings.LastIndex(withoutTag, ":"); idx >= 0 && idx > strings.LastIndex(withoutTag, "/") {
			withoutTag = withoutTag[:idx]
		}
		if idx := strings.LastIndex(withoutTag, "/"); idx >= 0 {
			return withoutTag[:idx]
		}
		return def
	},
	"imageRepository": func(image, def string) string {
		if image == "" {
			return def
		}
		withoutTag := image
		if idx := strings.LastIndex(withoutTag, ":"); idx >= 0 && idx > strings.LastIndex(withoutTag, "/") {
			withoutTag = withoutTag[:idx]
		}
		withoutTag = strings.TrimPrefix(withoutTag, "docker.io/")
		if idx := strings.LastIndex(withoutTag, "/"); idx >= 0 && idx < len(withoutTag)-1 {
			return withoutTag[idx+1:]
		}
		return def
	},
	"imageSource": func(image, def string) string {
		if image == "" {
			return def
		}
		withoutTag := image
		if idx := strings.LastIndex(withoutTag, ":"); idx >= 0 && idx > strings.LastIndex(withoutTag, "/") {
			withoutTag = withoutTag[:idx]
		}
		if strings.HasPrefix(withoutTag, "docker.io/") {
			withoutTag = strings.TrimPrefix(withoutTag, "docker.io/")
		}
		if idx := strings.LastIndex(withoutTag, "/"); idx >= 0 {
			return withoutTag[:idx]
		}
		return def
	},
	"pmmServerImageCustom": func(image string) bool {
		if image == "" {
			return false
		}
		withoutTag := image
		if idx := strings.LastIndex(withoutTag, ":"); idx >= 0 && idx > strings.LastIndex(withoutTag, "/") {
			withoutTag = withoutTag[:idx]
		}
		withoutTag = strings.TrimPrefix(withoutTag, "docker.io/")
		return withoutTag != "percona/pmm-server" && withoutTag != "perconalab/pmm-server"
	},
	"mongodbPackageLabel": mongodbPackageLabel,
	"pbmPackageLabel":     pbmPackageLabel,
	"namespaceCustom": func(ns string) bool {
		return ns != "" && ns != "percona" && ns != "perconalab"
	},
	"mongotNamespaceCustom": func(ns string) bool {
		return ns != "" && ns != "percona" && ns != "perconalab" && ns != "mongodb"
	},
	"formatDate": func(value string) string {
		if value == "" {
			return "—"
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return value
		}
		return parsed.UTC().Format("2006-01-02 15:04 UTC")
	},
	// dockerPsmdbImage returns the first PSMDB container image configured for a
	// Docker environment (checked across all replsets, then clusters).
	"dockerPsmdbImage": func(cfg Config) string {
		for _, rc := range cfg.Replsets {
			if rc.PsmdbImage != "" {
				return rc.PsmdbImage
			}
		}
		for _, cc := range cfg.Clusters {
			if cc.PsmdbImage != "" {
				return cc.PsmdbImage
			}
		}
		return ""
	},
	// dockerPbmImage returns the first PBM container image configured for a
	// Docker environment (checked across all replsets, then clusters).
	"dockerPbmImage": func(cfg Config) string {
		for _, rc := range cfg.Replsets {
			if boolDefault(rc.EnablePbm, false) && rc.PbmImage != "" {
				return rc.PbmImage
			}
		}
		for _, cc := range cfg.Clusters {
			if boolDefault(cc.EnablePbm, false) && cc.PbmImage != "" {
				return cc.PbmImage
			}
		}
		return ""
	},
	// dockerPmmImage returns the first PMM server container image configured for
	// a Docker environment.
	"dockerPmmImage": func(cfg Config) string {
		for _, ns := range cfg.PmmServers {
			if ns.PmmServerImage != "" {
				return ns.PmmServerImage
			}
		}
		return ""
	},
	// title-cases a status string, with human-friendly overrides for action
	// statuses that differ from the button label (e.g. "configure" → "Install").
	"statusLabel": func(s string) string {
		switch s {
		case "configure_in_progress":
			return "Installing…"
		case "configure_failed":
			return "Install Failed"
		case "deleted":
			return "Destroyed"
		}
		words := strings.ReplaceAll(s, "_", " ")
		var out []rune
		capNext := true
		for _, r := range words {
			if unicode.IsSpace(r) {
				capNext = true
				out = append(out, r)
			} else if capNext {
				out = append(out, unicode.ToUpper(r))
				capNext = false
			} else {
				out = append(out, r)
			}
		}
		return string(out)
	},
	// Returns the CSS class suffix for a status string.
	"statusClass": func(s string) string {
		if idx := strings.Index(s, ":"); idx >= 0 {
			s = s[:idx]
		}
		return s
	},
	"len": func(v interface{}) int {
		switch t := v.(type) {
		case map[string]ClusterConfig:
			return len(t)
		case map[string]ReplsetConfig:
			return len(t)
		case []NamedCluster:
			return len(t)
		case []NamedReplset:
			return len(t)
		}
		return 0
	},
}

// ─── renderPage ───────────────────────────────────────────────────────────────

func renderPage(w http.ResponseWriter, page string, data interface{}) {
	t, err := template.New("").Funcs(funcMap).ParseFS(uiAssets,
		"templates/layout.html",
		"templates/"+page+".html",
	)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("template execute", "page", page, "err", err)
	}
}

func absolutePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	return filepath.Abs(filepath.Clean(path))
}

func configurePaths() error {
	repo := os.Getenv("UI_REPO_DIR")
	if repo == "" {
		return fmt.Errorf("UI_REPO_DIR is required")
	}
	var err error
	if repoDir, err = absolutePath(repo); err != nil {
		return fmt.Errorf("resolve UI_REPO_DIR: %w", err)
	}
	data := os.Getenv("UI_DATA_DIR")
	if data == "" {
		data = "./data"
	}
	dataDir, err = absolutePath(data)
	if err != nil {
		return fmt.Errorf("resolve UI_DATA_DIR: %w", err)
	}

	terraformDir = filepath.Join(repoDir, "terraform")
	ansibleDir = filepath.Join(repoDir, "ansible")
	if err := loadDefaultImages(filepath.Join(repoDir, ".env")); err != nil {
		return fmt.Errorf("read default images: %w", err)
	}
	for _, path := range []struct {
		name string
		path string
	}{
		{"terraform", terraformDir},
		{"ansible", ansibleDir},
	} {
		info, statErr := os.Stat(path.path)
		if statErr != nil {
			return fmt.Errorf("UI_REPO_DIR must contain %s/: %w", path.name, statErr)
		}
		if !info.IsDir() {
			return fmt.Errorf("UI_REPO_DIR/%s is not a directory", path.name)
		}
	}

	stateFile = filepath.Join(dataDir, "environments.json")
	settingsFile = filepath.Join(dataDir, "settings.json")
	jobsDir = filepath.Join(dataDir, "jobs")
	pcsmSecretsDir = filepath.Join(dataDir, "secrets", "pcsm")
	if err := os.MkdirAll(jobsDir, 0755); err != nil {
		return fmt.Errorf("create jobs directory: %w", err)
	}
	if err := os.MkdirAll(pcsmSecretsDir, 0700); err != nil {
		return fmt.Errorf("create PCSM secrets directory: %w", err)
	}
	return nil
}

// ─── JSON helpers ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ─── Small value helpers ─────────────────────────────────────────────────────

func strDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func intDefault(n, def int) int {
	if n == 0 {
		return def
	}
	return n
}

func intPtrDefault(n *int, def int) int {
	if n == nil {
		return def
	}
	return *n
}

func boolDefault(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

func mongodbPackageLabel(cfg Config) string {
	labels := map[string]bool{}
	for _, rc := range cfg.Replsets {
		labels[mongodbPackageLabelFor(cfg.MongoDBDistribution, cfg.MongoRelease, cfg.MongoVersion, rc.MongoDBDistribution, rc.MongoRelease, rc.MongoVersion)] = true
	}
	for _, cc := range cfg.Clusters {
		labels[mongodbPackageLabelFor(cfg.MongoDBDistribution, cfg.MongoRelease, cfg.MongoVersion, cc.MongoDBDistribution, cc.MongoRelease, cc.MongoVersion)] = true
	}
	if len(labels) == 0 {
		return mongodbPackageLabelFor("", "", "", cfg.MongoDBDistribution, cfg.MongoRelease, cfg.MongoVersion)
	}
	if len(labels) == 1 {
		for label := range labels {
			return label
		}
	}
	parts := make([]string, 0, len(labels))
	for label := range labels {
		parts = append(parts, label)
	}
	sort.Strings(parts)
	return "Mixed: " + strings.Join(parts, ", ")
}

func pbmPackageLabel(cfg Config) string {
	labels := map[string]bool{}
	addVersion := func(enabled *bool, version string) {
		if !boolDefault(enabled, false) {
			return
		}
		version = strings.TrimSpace(version)
		if version == "" {
			version = strings.TrimSpace(cfg.PbmVersion)
		}
		if version == "" {
			version = "latest"
		}
		labels[version] = true
	}
	for _, rc := range cfg.Replsets {
		addVersion(rc.EnablePbm, rc.PbmVersion)
	}
	for _, cc := range cfg.Clusters {
		addVersion(cc.EnablePbm, cc.PbmVersion)
	}
	if len(labels) == 0 {
		return "—"
	}
	if len(labels) == 1 {
		for label := range labels {
			return label
		}
	}
	parts := make([]string, 0, len(labels))
	for label := range labels {
		parts = append(parts, label)
	}
	sort.Strings(parts)
	return "Mixed: " + strings.Join(parts, ", ")
}

func mongodbPackageLabelFor(defaultDistribution, defaultRelease, defaultVersion, distribution, release, version string) string {
	distribution = strings.TrimSpace(distribution)
	if distribution == "" {
		distribution = strings.TrimSpace(defaultDistribution)
	}
	distribution = normalizePackageDistribution(distribution)
	if distribution == "" {
		distribution = "psmdb"
	}

	release = strings.TrimSpace(release)
	if release == "" {
		release = strings.TrimSpace(defaultRelease)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = strings.TrimSpace(defaultVersion)
	}

	selectedVersion := version
	if selectedVersion == "" {
		selectedVersion = release
	}
	if selectedVersion == "" {
		if distribution == "psmdb" {
			selectedVersion = "psmdb-80"
		} else {
			selectedVersion = "8.0"
		}
	}

	switch distribution {
	case "community":
		return "MongoDB Community " + selectedVersion
	case "enterprise":
		return "MongoDB Enterprise " + selectedVersion
	default:
		return "PSMDB " + selectedVersion
	}
}

// ─── ANSI stripping ───────────────────────────────────────────────────────────

func stripAnsi(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// ─── Shell helpers ────────────────────────────────────────────────────────────

// shellQuote wraps s in single-quotes, escaping any single-quotes inside.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// sanitiseShellArg removes characters that are not safe for direct shell interpolation.
func sanitiseShellArg(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// validPlatform reports whether p is a known platform name.
func validPlatform(p string) bool {
	for _, pl := range platforms {
		if p == pl {
			return true
		}
	}
	return false
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	if err := configurePaths(); err != nil {
		log.Fatal(err)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("starting PSMDB Sandbox", "repoDir", repoDir, "dataDir", filepath.Dir(stateFile))

	go prefetchVersions()
	startImagePrefetch()

	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("GET /", indexHandler)
	mux.HandleFunc("GET /new", newEnvironmentHandler)
	mux.HandleFunc("GET /configure/{platform}", configureHandler)
	mux.HandleFunc("GET /environment/{env_id}", environmentHandler)

	// API
	mux.HandleFunc("GET /api/versions", apiVersionsHandler)
	mux.HandleFunc("GET /api/package-versions", apiPackageVersionsHandler)
	mux.HandleFunc("GET /api/docker-tags", apiDockerTagsHandler)
	mux.HandleFunc("GET /api/regions/{platform}", apiRegionsHandler)
	mux.HandleFunc("GET /api/images/{platform}", apiImagesHandler)
	mux.HandleFunc("GET /api/hardware/presets", apiHardwarePresetsHandler)
	mux.HandleFunc("POST /api/hardware/validate", apiHardwareValidateHandler)
	mux.HandleFunc("GET /api/prerequisites/{platform}", apiPrerequisitesHandler)
	mux.HandleFunc("GET /api/settings", apiSettingsHandler)
	mux.HandleFunc("POST /api/settings", apiSettingsHandler)
	mux.HandleFunc("POST /api/upload-ssh-key/{platform}", apiUploadSSHKeyHandler)
	mux.HandleFunc("POST /api/upload-settings-ssh-key/{kind}", apiUploadSettingsSSHKeyHandler)
	mux.HandleFunc("POST /api/upload-chaos-token", apiUploadChaosTokenHandler)
	mux.HandleFunc("POST /api/upload-provider-credential/{platform}", uploadProviderCredentialHandler)
	mux.HandleFunc("GET /api/provider-auth/{platform}", providerAuthHandler)
	mux.HandleFunc("POST /api/provider-auth/{platform}", providerAuthHandler)
	mux.HandleFunc("GET /api/provider-auth/{platform}/status", providerAuthStatusHandler)
	mux.HandleFunc("POST /api/environment", saveEnvironmentHandler)
	mux.HandleFunc("DELETE /api/environment/{env_id}", deleteEnvironmentHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/tfvars", getTfvarsHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/inventory", getInventoryHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/hosts", getHostsHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/chaos/reachability", chaosReachabilityHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/history", envHistoryHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/status", envStatusHandler)
	mux.HandleFunc("POST /api/environment/{env_id}/action", environmentActionHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/clustersync/status", clusterSyncStatusHandler)
	mux.HandleFunc("GET /api/environment/{env_id}/clustersync/log", clusterSyncLogHandler)
	mux.HandleFunc("POST /api/environment/{env_id}/clustersync/{action}", clusterSyncActionHandler)
	mux.HandleFunc("GET /api/job/{job_id}/status", jobStatusHandler)
	mux.HandleFunc("GET /api/job/{job_id}/stream", jobStreamHandler)
	mux.HandleFunc("GET /api/job/{job_id}/log", jobLogHandler)
	mux.HandleFunc("GET /api/job/{job_id}/parts", jobPartsHandler)
	mux.HandleFunc("POST /api/job/{job_id}/cancel", jobCancelHandler)

	// Static files
	staticFS, err := fs.Sub(uiAssets, "static")
	if err != nil {
		log.Fatal("load embedded static assets:", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	addr := ":5001"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	host := "127.0.0.1"
	if os.Getenv("UI_HOST") != "" {
		host = os.Getenv("UI_HOST")
	}

	srv := &http.Server{
		Addr:    host + addr,
		Handler: mux,
	}
	// Jobs run in their own process group, so stop them explicitly when the
	// UI is interrupted, as the terminal would have done before.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("shutting down, stopping running jobs", "signal", sig.String())
		killAllJobs()
		os.Exit(130)
	}()

	slog.Info("listening", "addr", "http://"+host+addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
