package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const dockerReplsetPortStep = 20
const defaultChaosAPIURL = "https://chaos.int.percona.com/api/compute/proxmox-instances"

func chaosTokenSecretsDir() string {
	return filepath.Join(dataDir, "secrets", "chaos")
}

func chaosTokenUploadPath(filename string) string {
	return filepath.Join(chaosTokenSecretsDir(), filename)
}

func sshSecretsDir() string {
	return filepath.Join(dataDir, "secrets", "ssh")
}

func sshKeyUploadPath(filename string) string {
	return filepath.Join(sshSecretsDir(), filename)
}

func managedUploadFilename(rawName, fallback string) string {
	name := safeFilenameRe.ReplaceAllString(filepath.Base(rawName), "_")
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	return name
}

func sshUploadFilename(kind, rawName string) string {
	fallback := "settings_" + kind
	name := managedUploadFilename(rawName, fallback)
	if kind == "public" {
		if strings.ToLower(filepath.Ext(name)) != ".pub" {
			name += ".pub"
		}
		return name
	}
	if filepath.Ext(name) == "" || strings.EqualFold(filepath.Ext(name), ".pub") {
		name += ".key"
	}
	return name
}

func currentLocalUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func isManagedChaosTokenFile(path string) bool {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return false
	}
	cleanedPath := filepath.Clean(trimmedPath)
	managedDir := filepath.Clean(chaosTokenSecretsDir())
	if cleanedPath == managedDir || !strings.HasPrefix(cleanedPath, managedDir+string(os.PathSeparator)) {
		return false
	}
	return true
}

func removeManagedChaosTokenFile(path string) {
	if !isManagedChaosTokenFile(path) {
		return
	}
	cleanedPath := filepath.Clean(strings.TrimSpace(path))
	_ = os.Remove(cleanedPath)
}

func loadChaosTokenFromPath(path string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return "", nil
	}
	data, err := os.ReadFile(trimmedPath)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("CHAOS token file %q is empty", trimmedPath)
	}
	return token, nil
}

func configuredChaosTokenPath(env *Environment) (string, error) {
	settings, err := loadAppSettings()
	if err != nil {
		return "", err
	}
	if settings.ChaosApiTokenPath != "" {
		return settings.ChaosApiTokenPath, nil
	}
	if env != nil && !isManagedChaosTokenFile(env.Config.ChaosApiTokenPath) {
		return strings.TrimSpace(env.Config.ChaosApiTokenPath), nil
	}
	return "", nil
}

func dropStoredChaosTokensInState(state map[string]*Environment) error {
	changed := false
	settings, _ := loadAppSettings()
	settingsPath := filepath.Clean(strings.TrimSpace(settings.ChaosApiTokenPath))
	for _, env := range state {
		if env == nil || env.Platform != "chaos" {
			continue
		}
		if strings.TrimSpace(env.Config.LegacyChaosAPIToken) != "" {
			env.Config.LegacyChaosAPIToken = ""
			changed = true
		}
		if isManagedChaosTokenFile(env.Config.ChaosApiTokenPath) {
			if filepath.Clean(strings.TrimSpace(env.Config.ChaosApiTokenPath)) != settingsPath {
				removeManagedChaosTokenFile(env.Config.ChaosApiTokenPath)
			}
			env.Config.ChaosApiTokenPath = ""
			changed = true
		}
	}
	if changed {
		return saveState(state)
	}
	return nil
}

// cleanupDockerModuleArtifacts removes files and directories that Terraform
// created inside the docker module directories (modules/mongodb_replset and
// modules/mongodb_cluster) for the given environment. These include:
//   - pbm-storage.conf.{name} files
//   - {name}-*.Dockerfile files (new sanitized naming)
//   - {name}-percona/ subdirectories (old naming, created when the image name
//     contained a "/" which Terraform mistakenly turned into a path separator)
//
// This is safe to call even when the files do not exist.
func cleanupDockerModuleArtifacts(cfg Config) {
	namePrefix := cfg.Prefix
	if namePrefix != "" {
		namePrefix += "-"
	}

	replsetDir := filepath.Join(terraformDir, "docker", "modules", "mongodb_replset")
	clusterDir := filepath.Join(terraformDir, "docker", "modules", "mongodb_cluster")

	for key := range cfg.Replsets {
		cleanupModuleResourceFiles(replsetDir, namePrefix+key)
	}
	for key := range cfg.Clusters {
		cleanupModuleResourceFiles(clusterDir, namePrefix+key)
	}
}

// cleanupModuleResourceFiles removes Terraform-generated artifacts for a single
// named resource (replica-set or cluster) inside a module directory.
func cleanupModuleResourceFiles(moduleDir, resourceName string) {
	// pbm-storage.conf.{resourceName}
	os.Remove(filepath.Join(moduleDir, "pbm-storage.conf."+resourceName))
	os.Remove(filepath.Join(moduleDir, resourceName+"-mongot-password"))
	if matches, _ := filepath.Glob(filepath.Join(moduleDir, resourceName+"-*-mongot.yml")); matches != nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}

	// {resourceName}-*.Dockerfile (sanitized image name)
	if matches, _ := filepath.Glob(filepath.Join(moduleDir, resourceName+"-*.Dockerfile")); matches != nil {
		for _, f := range matches {
			os.Remove(f)
		}
	}

	// Old-style subdirectory created when the image name contained "/" and
	// Terraform interpreted it as a directory separator (e.g. {resourceName}-percona/).
	if entries, _ := filepath.Glob(filepath.Join(moduleDir, resourceName+"-*")); entries != nil {
		for _, entry := range entries {
			if info, err := os.Stat(entry); err == nil && info.IsDir() {
				os.RemoveAll(entry)
			}
		}
	}
}

func chaosAPIReachabilityError() string {
	return "CHAOS API is unreachable from this UI host. Connect to Percona VPN and try again."
}

func chaosAPIProbeURL() string {
	// Match the Terraform provider's API URL so a publicly reachable web UI
	// cannot make the deployment preflight pass while the private API is down.
	if v := strings.TrimSpace(os.Getenv("CHAOS_API_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("CHAOS_API_PROBE_URL")); v != "" {
		return v
	}
	return defaultChaosAPIURL
}

func canReachChaosAPI() error {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodHead, chaosAPIProbeURL(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("unexpected CHAOS API probe status %d", resp.StatusCode)
	}
	return nil
}

// GET /
func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	state, err := loadState()
	if err != nil {
		http.Error(w, "State error: "+err.Error(), 500)
		return
	}
	if err := dropStoredChaosTokensInState(state); err != nil {
		http.Error(w, "State error: "+err.Error(), 500)
		return
	}
	settings, err := loadAppSettings()
	if err != nil {
		http.Error(w, "Settings error: "+err.Error(), 500)
		return
	}
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]EnvEntry, 0, len(ids))
	hasDeleted := false
	for _, id := range ids {
		entries = append(entries, EnvEntry{id, state[id]})
		if state[id].Status == "deleted" {
			hasDeleted = true
		}
	}
	renderPage(w, "index", IndexData{Environments: entries, HasDeleted: hasDeleted, Settings: settings, LocalSSHUser: currentLocalUser()})
}

// GET /new
func newEnvironmentHandler(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "new_environment", NewEnvData{Platforms: platforms})
}

// GET /configure/{platform}
func configureHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if !validPlatform(platform) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	envID := r.URL.Query().Get("env_id")
	if envID == "" && (platform == "aws" || platform == "gcp" || platform == "azure") {
		if err := requireProviderAuth(platform); err != nil {
			http.Redirect(w, r, "/?settings="+platform, http.StatusFound)
			return
		}
	}
	var cfg Config
	dockerDefaultPmmExternalPort := 8443
	dockerDefaultSeaweedFSPort := 8333
	dockerDefaultSeaweedFSAdminPort := 9333
	state, _ := loadState()
	if err := dropStoredChaosTokensInState(state); err != nil {
		http.Error(w, "State error: "+err.Error(), 500)
		return
	}
	images := defaultImages
	if envID != "" {
		if env, ok := state[envID]; ok {
			cfg = env.Config
			images = builtinDefaultImages
		}
	}
	normalizeTopologyUseTLS(&cfg)
	normalizeCAProvisioning(&cfg)
	if platform == "chaos" && envID == "" {
		cfg.DeleteAfterDays = 7
	}
	if cfg.EnableBackup == nil && envID == "" {
		// New environments start with backups off; ticking the toggle turns on
		// the PBM agent everywhere.
		off := false
		cfg.EnableBackup = &off
	}
	if cfg.CAPlacement == "" {
		cfg.CAPlacement = "dedicated"
	}
	if platform == "docker" {
		occupied := dockerOccupiedServicePorts(state, envID)
		dockerDefaultPmmExternalPort = nextFreeDockerPort(8443, occupied)
		dockerDefaultSeaweedFSPort, dockerDefaultSeaweedFSAdminPort = nextFreeSeaweedFSPorts(8333, occupied)
	}

	osUser := currentLocalUser()

	renderPage(w, "configure", ConfigureData{
		Platform:                        platform,
		EnvID:                           envID,
		Config:                          cfg,
		Regions:                         defaultRegions(platform),
		PreloadedImages:                 preloadedImages(platform),
		DefaultAuditFilter:              defaultAuditFilter,
		OSUser:                          osUser,
		DockerDefaultPmmExternalPort:    dockerDefaultPmmExternalPort,
		DefaultImages:                   images,
		DockerDefaultSeaweedFSPort:      dockerDefaultSeaweedFSPort,
		DockerDefaultSeaweedFSAdminPort: dockerDefaultSeaweedFSAdminPort,
		PSMDBVersions:                   cachedPSMDBVersions(),
		OfficialMongoDBVersions:         defaultMongoDBOfficialVersions,
		PBMVersions:                     cachedPBMVersions(),
		PSMDBMinorVersions:              cachedPSMDBMinorVersionsByMajor(),
		PCSMVersions:                    cachedPCSMVersions(),
		PMMImages:                       cachedPMMServerImages(),
		PSMDBImages:                     cachedPSMDBImages(),
		PBMImages:                       cachedPBMImages(),
		PMMClientImages:                 cachedPMMClientImages(),
		MongotImages:                    cachedMongotImages(),
		SortedClusters:                  sortedClusters(cfg.Clusters),
		SortedReplsets:                  sortedReplsets(cfg.Replsets),
		SortedPmmServers:                sortedPmmServers(cfg.PmmServers),
		SortedBareClients:               sortedPmmBareClients(cfg.PmmBareClients),
		SortedSeaweedFS:                 sortedSeaweedFSServers(cfg.SeaweedFSServers),
		SortedLdap:                      sortedLdapServers(cfg.LdapServers),
	})
}

func clusterSyncDeploymentReady(env *Environment) bool {
	return env != nil && env.Config.ClusterSync.Enabled && env.LastAppliedConfig != nil && env.LastAppliedConfig.ClusterSync.Enabled && (env.Status == "running" || env.Status == "deploy_success")
}

// GET /environment/{env_id}
func environmentHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	state, _ := loadState()
	if err := dropStoredChaosTokensInState(state); err != nil {
		http.Error(w, "State error: "+err.Error(), 500)
		return
	}
	env, ok := state[envID]
	if !ok {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	renderPage(w, "environment", EnvironmentData{
		EnvID:                envID,
		Env:                  env,
		SortedClusters:       sortedClusters(env.Config.Clusters),
		SortedReplsets:       sortedReplsets(env.Config.Replsets),
		ServiceURLs:          configServiceURLs(envID, env),
		ClusterSync:          normalizedClusterSyncConfig(env.Config.ClusterSync),
		ClusterSyncAvailable: clusterSyncDeploymentReady(env),
	})
}

// GET /api/versions
func apiVersionsHandler(w http.ResponseWriter, r *http.Request) {
	minorVersions := getPSMDBMinorVersionsByMajor()
	writeJSON(w, 200, map[string]interface{}{
		"psmdb_versions":       availablePSMDBReleases(getPSMDBVersions(), minorVersions),
		"pbm_versions":         getPBMVersions(),
		"pmm_server_images":    getPMMServerImages(),
		"psmdb_images":         getPSMDBImages(),
		"pbm_images":           getPBMImages(),
		"pmm_client_images":    getPMMClientImages(),
		"mongot_images":        getMongotImages(),
		"psmdb_minor_versions": minorVersions,
		"pcsm_versions":        getPCSMVersions(),
	})
}

// GET /api/package-versions?product=psmdb&distribution=psmdb&channel=release&os_image=Ubuntu%2022.04
func apiPackageVersionsHandler(w http.ResponseWriter, r *http.Request) {
	product := strings.TrimSpace(r.URL.Query().Get("product"))
	distribution := strings.TrimSpace(r.URL.Query().Get("distribution"))
	if distribution == "" {
		distribution = "psmdb"
	}
	channel := normalizedRepoChannel(r.URL.Query().Get("channel"))
	osImage := strings.TrimSpace(r.URL.Query().Get("os_image"))
	chaosDefaultOS := r.URL.Query().Get("platform") == "chaos" && osImage == ""
	if chaosDefaultOS {
		osImage = "Ubuntu 24.04"
	}

	switch product {
	case "psmdb":
		var minorVersions map[string][]string
		var majorVersions []string
		if distribution == "community" || distribution == "enterprise" {
			minorVersions = getOfficialMongoDBMinorVersionsByMajorFor(distribution, osImage)
			if chaosDefaultOS {
				// The default CHAOS image for official MongoDB 7.0 is jammy;
				// other releases use the regular noble default.
				minorVersions["7.0"] = officialMongoDBPackageVersions(distribution, "7.0", "Ubuntu 22.04")
			}
			for _, release := range defaultMongoDBOfficialVersions {
				if len(minorVersions[release]) > 0 {
					majorVersions = append(majorVersions, release)
				}
			}
		} else {
			minorVersions = getPSMDBMinorVersionsByMajorFor(channel, osImage)
			for _, release := range getPSMDBVersions() {
				if len(minorVersions[release]) > 0 {
					majorVersions = append(majorVersions, release)
				}
			}
		}
		writeJSON(w, 200, map[string]interface{}{
			"major_versions": majorVersions,
			"minor_versions": minorVersions,
		})
	case "pbm":
		writeJSON(w, 200, map[string]interface{}{"versions": getPBMVersionsFor(channel, osImage)})
	case "pmm_client":
		writeJSON(w, 200, map[string]interface{}{"versions": getPMMClientVersionsFor(channel, osImage)})
	case "ps4m":
		writeJSON(w, 200, map[string]interface{}{"versions": getPerconaSearchVersionsFor(channel, osImage)})
	case "pcsm":
		writeJSON(w, 200, map[string]interface{}{"versions": getPCSMVersionsFor(channel, osImage)})
	default:
		jsonError(w, 400, "product must be one of: psmdb, pbm, pmm_client, ps4m, pcsm")
	}
}

// GET /api/docker-tags?namespace=percona&repo=pmm-client
func apiDockerTagsHandler(w http.ResponseWriter, r *http.Request) {
	namespace := strings.Trim(strings.TrimSpace(r.URL.Query().Get("namespace")), "/")
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	if namespace == "" || repo == "" {
		jsonError(w, 400, "namespace and repo are required")
		return
	}
	if strings.Contains(namespace, " ") || strings.Contains(repo, " ") || strings.Contains(repo, "/") {
		jsonError(w, 400, "invalid namespace or repo")
		return
	}
	tags := getDockerHubTags(namespace, repo, 50)
	if len(tags) == 0 {
		switch repo {
		case "percona-server-mongodb":
			tags = defaultPSMDBImages
		case "percona-backup-mongodb":
			tags = defaultPBMImages
		case "pmm-client":
			tags = defaultPMMClientImages
		case "pmm-server":
			tags = defaultPMMServerImages
		case "mongodb-community-search":
			tags = defaultMongotImages
		case "percona-server-mongodb-mongot":
			tags = defaultMongotImages
		}
	}
	if repo == "mongodb-community-search" {
		tags = filterMongotTags(tags)
	}
	writeJSON(w, 200, map[string]interface{}{"tags": tags})
}

// GET /api/regions/{platform}
func apiRegionsHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	regions := getCloudRegions(platform, r.URL.Query().Get("refresh") == "1")
	writeJSON(w, 200, map[string]interface{}{
		"regions":         regions,
		"grouped_regions": groupRegionsByGeo(platform, regions),
	})
}

// POST /api/upload-ssh-key/{platform}
func apiUploadSSHKeyHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if !validPlatform(platform) || platform == "docker" {
		jsonError(w, 400, "invalid platform")
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		jsonError(w, 400, "failed to parse upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("ssh_key_file")
	if err != nil {
		jsonError(w, 400, "ssh_key_file field missing: "+err.Error())
		return
	}
	defer file.Close()

	name := safeFilenameRe.ReplaceAllString(filepath.Base(header.Filename), "_")
	if name == "" {
		name = "id_rsa.pub"
	}

	destDir := filepath.Clean(filepath.Join(terraformDir, platform))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		jsonError(w, 500, "cannot create upload dir: "+err.Error())
		return
	}
	destPath := filepath.Clean(filepath.Join(destDir, name))
	if filepath.Dir(destPath) != destDir {
		jsonError(w, 400, "invalid filename")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, 500, "read failed: "+err.Error())
		return
	}
	if err := os.WriteFile(destPath, data, 0600); err != nil {
		jsonError(w, 500, "write failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": name})
}

// POST /api/upload-settings-ssh-key/{kind}
func apiUploadSettingsSSHKeyHandler(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "public" && kind != "private" {
		jsonError(w, 400, "invalid SSH key kind")
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		jsonError(w, 400, "failed to parse upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("ssh_key_file")
	if err != nil {
		jsonError(w, 400, "ssh_key_file field missing: "+err.Error())
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, 500, "read failed: "+err.Error())
		return
	}
	if strings.TrimSpace(string(data)) == "" {
		jsonError(w, 400, "uploaded SSH key file is empty")
		return
	}
	if err := os.MkdirAll(sshSecretsDir(), 0700); err != nil {
		jsonError(w, 500, "cannot create secrets dir: "+err.Error())
		return
	}
	storedFilename := sshUploadFilename(kind, header.Filename)
	storedPath := sshKeyUploadPath(storedFilename)
	mode := os.FileMode(0600)
	if err := os.WriteFile(storedPath, data, mode); err != nil {
		jsonError(w, 500, "write failed: "+err.Error())
		return
	}

	writeJSON(w, 200, map[string]string{
		"path":     storedPath,
		"filename": storedFilename,
	})
}

func apiSettingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := loadAppSettings()
		if err != nil {
			jsonError(w, 500, "settings load failed: "+err.Error())
			return
		}
		writeJSON(w, 200, settings)
	case http.MethodPost:
		var settings AppSettings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			jsonError(w, 400, "invalid JSON: "+err.Error())
			return
		}
		settings.ChaosApiTokenPath = strings.TrimSpace(settings.ChaosApiTokenPath)
		settings.SSHUser = strings.TrimSpace(settings.SSHUser)
		settings.AWSSSHUser = strings.TrimSpace(settings.AWSSSHUser)
		settings.GCPSSHUser = strings.TrimSpace(settings.GCPSSHUser)
		settings.AzureSSHUser = strings.TrimSpace(settings.AzureSSHUser)
		settings.ChaosSSHUser = strings.TrimSpace(settings.ChaosSSHUser)
		settings.SSHPublicKeyPath = strings.TrimSpace(settings.SSHPublicKeyPath)
		settings.SSHPrivateKeyPath = strings.TrimSpace(settings.SSHPrivateKeyPath)
		settings.AWSProfile = strings.TrimSpace(settings.AWSProfile)
		settings.AWSRegion = strings.TrimSpace(settings.AWSRegion)
		settings.GCPProjectID = strings.TrimSpace(settings.GCPProjectID)
		settings.GCPServiceAccountKey = strings.TrimSpace(settings.GCPServiceAccountKey)
		settings.AzureTenantID = strings.TrimSpace(settings.AzureTenantID)
		settings.AzureSubscriptionID = strings.TrimSpace(settings.AzureSubscriptionID)
		settings.AzureClientID = strings.TrimSpace(settings.AzureClientID)
		if settings.TerraformParallelism < 0 || settings.TerraformParallelism > 256 {
			jsonError(w, 400, "terraform_parallelism must be between 1 and 256, or empty")
			return
		}
		if err := saveAppSettings(settings); err != nil {
			jsonError(w, 500, "settings save failed: "+err.Error())
			return
		}
		writeJSON(w, 200, settings)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func apiUploadChaosTokenHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		jsonError(w, 400, "failed to parse upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("chaos_token_file")
	if err != nil {
		jsonError(w, 400, "chaos_token_file field missing: "+err.Error())
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, 500, "read failed: "+err.Error())
		return
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		jsonError(w, 400, "uploaded token file is empty")
		return
	}
	if err := os.MkdirAll(chaosTokenSecretsDir(), 0700); err != nil {
		jsonError(w, 500, "cannot create secrets dir: "+err.Error())
		return
	}
	storedFilename := managedUploadFilename(header.Filename, "chaos_api.token")
	storedPath := chaosTokenUploadPath(storedFilename)
	if err := os.WriteFile(storedPath, []byte(token+"\n"), 0600); err != nil {
		jsonError(w, 500, "write failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{
		"path":     storedPath,
		"filename": storedFilename,
	})
}

// GET /api/images/{platform}?region={region}
func apiImagesHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	region := r.URL.Query().Get("region")
	if region == "" {
		region = r.URL.Query().Get("location")
	}
	images := getCloudImages(platform, region, r.URL.Query().Get("refresh") == "1")
	writeJSON(w, 200, map[string]interface{}{"images": images})
}

func normalizePackageDistribution(distribution string) string {
	switch strings.TrimSpace(distribution) {
	case "", "psmdb":
		return "psmdb"
	case "community", "enterprise":
		return strings.TrimSpace(distribution)
	default:
		return ""
	}
}

// parseMajorMinor extracts the major and minor version numbers from a version
// string like "8.2.1" or "8.2". Returns (-1, -1) if parsing fails.
func parseMajorMinor(v string) (int, int) {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return -1, -1
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return -1, -1
	}
	return major, minor
}

// mongotVersionOk returns true if the given major.minor satisfies the mongot
// minimum requirement of mongod >= 8.2.
func mongotVersionOk(major, minor int) bool {
	return major > 8 || (major == 8 && minor >= 2)
}

// mongotVersionFromImage extracts the version string from a Docker image
// reference like "percona/percona-server-mongodb:8.2.1" (returns "8.2.1").
func mongotVersionFromImage(image string) string {
	if idx := strings.LastIndex(image, ":"); idx >= 0 && idx < len(image)-1 {
		return image[idx+1:]
	}
	return ""
}

// validateMongotVersionCompatibility checks that every cluster/replset that
// has mongot enabled is running mongod >= 8.2. For Docker environments the
// version is derived from the PsmdbImage tag; for all other platforms it is
// taken from MongoVersion.
func validateMongotVersionCompatibility(platform string, cfg *Config) error {
	type entry struct {
		kind          string
		name          string
		enableMongot  *bool
		mongoVersion  string
		mongoRelease  string
		distribution  string
		mongotSource  string
		mongotRepo    string
		mongotVersion string
		psmdbImage    string
	}

	var entries []entry
	for name, c := range cfg.Clusters {
		entries = append(entries, entry{"cluster", name, c.EnableMongot, c.MongoVersion, c.MongoRelease, c.MongoDBDistribution, c.MongotSource, c.MongotRepo, c.MongotVersion, c.PsmdbImage})
	}
	for name, r := range cfg.Replsets {
		entries = append(entries, entry{"replica set", name, r.EnableMongot, r.MongoVersion, r.MongoRelease, r.MongoDBDistribution, r.MongotSource, r.MongotRepo, r.MongotVersion, r.PsmdbImage})
	}

	for _, e := range entries {
		if e.enableMongot == nil || !*e.enableMongot {
			continue
		}
		e.mongoRelease = strDefault(e.mongoRelease, cfg.MongoRelease)
		e.mongoVersion = strDefault(e.mongoVersion, cfg.MongoVersion)
		e.distribution = strDefault(e.distribution, cfg.MongoDBDistribution)
		version := e.mongoVersion
		if platform == "docker" {
			version = e.psmdbImage
		} else if version == "" {
			version = e.mongoRelease
		}
		major, _, _, _, _ := parseMongoVersion(version)
		releaseMajor, _, _, _, _ := parseMongoVersion(e.mongoRelease)
		if major >= 9 || (platform != "docker" && releaseMajor >= 9) {
			return fmt.Errorf("%s %q: Search compatibility with MongoDB 9.0 has not been verified; disable mongot for this release", e.kind, e.name)
		}
		source := strings.TrimSpace(e.mongotSource)
		if source == "" {
			source = "auto"
		}
		if source != "auto" && source != "percona_package" && source != "community_tarball" {
			return fmt.Errorf("%s %q has invalid mongot_source %q", e.kind, e.name, source)
		}
		perconaSearch := source == "percona_package" || (source == "auto" && normalizePackageDistribution(e.distribution) == "psmdb" && e.mongoRelease == "psmdb-83")
		mongotRepo := strings.TrimSpace(e.mongotRepo)
		if perconaSearch && mongotRepo != "" && normalizedRepoChannel(mongotRepo) != mongotRepo {
			return fmt.Errorf("%s %q has invalid mongot_repo %q", e.kind, e.name, e.mongotRepo)
		}
		if platform != "docker" && perconaSearch && (normalizePackageDistribution(e.distribution) != "psmdb" || e.mongoRelease != "psmdb-83") {
			return fmt.Errorf("%s %q: Percona Search requires PSMDB 8.3 (psmdb-83), but release %s is selected", e.kind, e.name, e.mongoRelease)
		}
		if platform != "docker" && perconaSearch && e.mongoRelease == "psmdb-83" {
			if e.mongotVersion != "" && e.mongotVersion != "latest" {
				major, minor := parseMajorMinor(e.mongotVersion)
				if major < 1 || minor < 0 || (major == 1 && minor < 70) {
					return fmt.Errorf("%s %q: PSMDB 8.3 requires Percona Search 1.70 or newer, but version %s is selected", e.kind, e.name, e.mongotVersion)
				}
			}
		}
		var versionStr string
		if platform == "docker" {
			versionStr = mongotVersionFromImage(e.psmdbImage)
		} else {
			versionStr = strDefault(e.mongoVersion, e.mongoRelease)
		}
		if versionStr == "" {
			// Cannot determine version — skip (other validators will catch missing fields).
			continue
		}
		major, minor, _, _, ok := parseMongoVersion(versionStr)
		if !ok {
			continue // unparseable — skip
		}
		if !mongotVersionOk(major, minor) {
			return fmt.Errorf("%s %q: mongot requires mongod 8.2+, but version %s is selected", e.kind, e.name, versionStr)
		}
	}
	return nil
}

func validatePackageBlock(kind, name string, distribution, release string, enableAudit bool) error {
	distribution = normalizePackageDistribution(distribution)
	if distribution == "" {
		return fmt.Errorf("%s %q has invalid mongodb_distribution", kind, name)
	}
	if distribution == "community" && enableAudit {
		return fmt.Errorf("%s %q uses MongoDB Community, which does not support audit logging", kind, name)
	}
	if release == "" {
		return nil
	}
	if distribution == "psmdb" && !strings.HasPrefix(release, "psmdb-") {
		return fmt.Errorf("%s %q uses PSMDB but mongo_release %q is not a psmdb-* release", kind, name, release)
	}
	if distribution != "psmdb" && strings.HasPrefix(release, "psmdb-") {
		return fmt.Errorf("%s %q uses MongoDB %s but mongo_release %q is a PSMDB release", kind, name, distribution, release)
	}
	return nil
}

func validateChaosMongoOS(kind, name, distribution, release, osImage string) error {
	if distribution != "psmdb" && release == "7.0" && osImage == "Ubuntu 24.04" {
		return fmt.Errorf("%s %q: MongoDB %s 7.0 has no Ubuntu 24.04 repository; select Ubuntu 22.04 or another supported OS", kind, name, distribution)
	}
	return nil
}

func normalizeAndValidatePackageConfig(platform string, cfg *Config) error {
	if platform == "docker" {
		for name, cluster := range cfg.Clusters {
			if err := validateMongoDB9Backup("cluster", name, cluster.PsmdbImage, boolDefault(cluster.EnablePbm, true)); err != nil {
				return err
			}
		}
		for name, replset := range cfg.Replsets {
			if err := validateMongoDB9Backup("replica set", name, replset.PsmdbImage, boolDefault(replset.EnablePbm, true)); err != nil {
				return err
			}
		}
		return nil
	}
	defaultDistribution := normalizePackageDistribution(cfg.MongoDBDistribution)
	if defaultDistribution == "" {
		return fmt.Errorf("invalid mongodb_distribution %q", cfg.MongoDBDistribution)
	}
	cfg.MongoDBDistribution = strings.TrimSpace(cfg.MongoDBDistribution)
	if defaultDistribution != "psmdb" && cfg.MongoRelease == "" {
		cfg.MongoRelease = "8.0"
	}
	for name, cluster := range cfg.Clusters {
		distribution := normalizePackageDistribution(cluster.MongoDBDistribution)
		if distribution == "psmdb" && cluster.MongoDBDistribution == "" {
			distribution = defaultDistribution
		}
		if cluster.MongoDBDistribution == "" && distribution != "psmdb" {
			cluster.MongoDBDistribution = distribution
		}
		if distribution != "psmdb" && cluster.MongoRelease == "" && cfg.MongoRelease == "" {
			cluster.MongoRelease = "8.0"
		}
		release := cluster.MongoRelease
		if release == "" {
			release = cfg.MongoRelease
		}
		if err := validatePackageBlock("cluster", name, distribution, release, boolDefault(cluster.EnableAudit, false)); err != nil {
			return err
		}
		if platform == "chaos" {
			osImage := strDefault(cluster.OsImage, cfg.OsImage)
			if err := validateChaosMongoOS("cluster", name, distribution, release, osImage); err != nil {
				return err
			}
		}
		if err := validateMongoDB9Backup("cluster", name, release, boolDefault(cluster.EnablePbm, true)); err != nil {
			return err
		}
		cfg.Clusters[name] = cluster
	}
	for name, replset := range cfg.Replsets {
		distribution := normalizePackageDistribution(replset.MongoDBDistribution)
		if distribution == "psmdb" && replset.MongoDBDistribution == "" {
			distribution = defaultDistribution
		}
		if replset.MongoDBDistribution == "" && distribution != "psmdb" {
			replset.MongoDBDistribution = distribution
		}
		if distribution != "psmdb" && replset.MongoRelease == "" && cfg.MongoRelease == "" {
			replset.MongoRelease = "8.0"
		}
		release := replset.MongoRelease
		if release == "" {
			release = cfg.MongoRelease
		}
		if err := validatePackageBlock("replica set", name, distribution, release, boolDefault(replset.EnableAudit, false)); err != nil {
			return err
		}
		if platform == "chaos" {
			osImage := strDefault(replset.OsImage, cfg.OsImage)
			if err := validateChaosMongoOS("replica set", name, distribution, release, osImage); err != nil {
				return err
			}
		}
		if err := validateMongoDB9Backup("replica set", name, release, boolDefault(replset.EnablePbm, true)); err != nil {
			return err
		}
		cfg.Replsets[name] = replset
	}
	return nil
}

func validateMongoDB9Backup(kind, name, release string, enabled bool) error {
	major, _, _, _, _ := parseMongoVersion(release)
	if enabled && major >= 9 {
		return fmt.Errorf("%s %q: PBM does not yet support MongoDB 9.0 backup/restore; disable PBM for this release", kind, name)
	}
	return nil
}

// backupEnabled reports whether PBM backup storage is enabled on AWS/GCP/Azure.
// Environments saved before the global toggle existed (EnableBackup == nil)
// keep their previous behaviour: on when a bucket is created or an existing
// bucket is named.
func backupEnabled(cfg Config) bool {
	if cfg.EnableBackup != nil {
		return *cfg.EnableBackup
	}
	return boolDefault(cfg.CreateBackupBucket, true) || strings.TrimSpace(cfg.ExistingBucketName) != ""
}

// CHAOS backup storage modes returned by chaosBackupStorage.
const (
	chaosBackupNone      = ""
	chaosBackupSeaweedFS = "seaweedfs"
	chaosBackupExisting  = "existing"
)

// chaosBackupStorage returns where PBM stores backups on CHAOS: a SeaweedFS
// VM deployed with the environment, an existing S3-compatible bucket, or
// nothing. Environments saved before the global toggle existed
// (EnableBackup == nil) only knew the SeaweedFS checkbox.
func chaosBackupStorage(cfg Config) string {
	if cfg.EnableBackup == nil {
		if boolDefault(cfg.EnableSeaweedFS, false) {
			return chaosBackupSeaweedFS
		}
		return chaosBackupNone
	}
	if !*cfg.EnableBackup {
		return chaosBackupNone
	}
	if boolDefault(cfg.CreateBackupBucket, true) {
		return chaosBackupSeaweedFS
	}
	return chaosBackupExisting
}

// normalizeChaosBackup keeps the legacy EnableSeaweedFS flag in line with the
// global backup toggle so Terraform deploys SeaweedFS only when it is used.
func normalizeChaosBackup(cfg *Config) {
	if cfg.EnableBackup == nil {
		return
	}
	seaweedfs := chaosBackupStorage(*cfg) == chaosBackupSeaweedFS
	cfg.EnableSeaweedFS = &seaweedfs
}

// validateBackupBucketConfig ensures that when an existing bucket is used for
// PBM on AWS/GCP/Azure/CHAOS, its name and the credentials needed to access it
// are provided.
func validateBackupBucketConfig(platform string, cfg Config) error {
	if platform == "chaos" {
		if chaosBackupStorage(cfg) != chaosBackupExisting {
			return nil
		}
		missing := []string{}
		if strings.TrimSpace(cfg.ExistingBucketName) == "" {
			missing = append(missing, "bucket name")
		}
		if strings.TrimSpace(cfg.ExistingBucketAccessKey) == "" {
			missing = append(missing, "access key")
		}
		if cfg.ExistingBucketSecretKey == "" {
			missing = append(missing, "secret key")
		}
		if ep := strings.TrimSpace(cfg.ExistingBucketEndpoint); ep != "" && !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
			missing = append(missing, "endpoint URL starting with http:// or https://")
		}
		if len(missing) > 0 {
			return fmt.Errorf("existing backup bucket requires: %s", strings.Join(missing, ", "))
		}
		return nil
	}
	if platform != "aws" && platform != "gcp" && platform != "azure" {
		return nil
	}
	if !backupEnabled(cfg) || boolDefault(cfg.CreateBackupBucket, true) {
		return nil
	}
	if strings.TrimSpace(cfg.ExistingBucketName) == "" {
		if platform == "azure" {
			return fmt.Errorf("existing backup container requires a container name")
		}
		return fmt.Errorf("existing backup bucket requires a bucket name")
	}
	missing := []string{}
	switch platform {
	case "aws", "gcp":
		if strings.TrimSpace(cfg.ExistingBucketAccessKey) == "" {
			missing = append(missing, "access key")
		}
		if cfg.ExistingBucketSecretKey == "" {
			missing = append(missing, "secret key")
		}
	case "azure":
		if strings.TrimSpace(cfg.ExistingStorageAccount) == "" {
			missing = append(missing, "storage account")
		}
		if cfg.ExistingBucketSecretKey == "" {
			missing = append(missing, "storage account key")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("existing backup bucket %q requires: %s", cfg.ExistingBucketName, strings.Join(missing, ", "))
	}
	return nil
}

func requireSettingsFile(label, path string, required bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		if required {
			return fmt.Errorf("%s is required in Settings", label)
		}
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s %q is not readable: %w", label, path, err)
	}
	return nil
}

func applySSHSettingsToNewEnvironment(platform string, cfg *Config, settings AppSettings) error {
	if platform == "docker" {
		return nil
	}
	sshUser := settingsSSHUserForPlatform(settings, platform)
	if sshUser == "" {
		return fmt.Errorf("SSH user is required in Settings")
	}
	if err := requireSettingsFile("SSH private key file", settings.SSHPrivateKeyPath, false); err != nil {
		return err
	}

	cfg.MySSHUser = sshUser
	cfg.SSHPrivateKeyPath = strings.TrimSpace(settings.SSHPrivateKeyPath)

	if platform == "chaos" {
		cfg.SSHPublicKeyPath = ""
		cfg.SSHUsers = nil
		return nil
	}
	if err := requireSettingsFile("SSH public key file", settings.SSHPublicKeyPath, true); err != nil {
		return err
	}
	cfg.SSHPublicKeyPath = strings.TrimSpace(settings.SSHPublicKeyPath)
	if platform == "gcp" || platform == "azure" {
		if cfg.SSHUsers == nil {
			cfg.SSHUsers = map[string]string{}
		}
		cfg.SSHUsers[cfg.MySSHUser] = cfg.SSHPublicKeyPath
	}
	return nil
}

func settingsSSHUserForPlatform(settings AppSettings, platform string) string {
	fallback := strDefault(settings.SSHUser, currentLocalUser())
	switch platform {
	case "aws":
		return strDefault(settings.AWSSSHUser, fallback)
	case "gcp":
		return strDefault(settings.GCPSSHUser, fallback)
	case "azure":
		return strDefault(settings.AzureSSHUser, fallback)
	case "chaos":
		return strDefault(settings.ChaosSSHUser, fallback)
	default:
		return fallback
	}
}

func normalizeAndValidateUseTLSConfig(platform string, cfg *Config) error {
	normalizeTopologyUseTLS(cfg)
	normalizeCAProvisioning(cfg)
	if cfg.CAPlacement == "" {
		cfg.CAPlacement = "dedicated"
	}
	if cfg.CAPlacement != "dedicated" && cfg.CAPlacement != "pmm" {
		return fmt.Errorf("CA placement must be dedicated or pmm")
	}
	for _, key := range []string{"use_tls", "certificateKeyFile", "CAFile", "ca_staging_dir"} {
		if _, exists := cfg.AnsibleVars[key]; exists {
			return fmt.Errorf("Ansible variable %q is managed by the TLS configuration and cannot be overridden", key)
		}
	}
	caEnabled := boolDefault(cfg.EnableCA, false)
	if !caEnabled {
		cfg.CAPlacement = "dedicated"
		if cfg.UseTLS {
			return fmt.Errorf("provision a certificate authority when TLS is enabled for a cluster or replica set")
		}
		return nil
	}
	if platform != "aws" && platform != "gcp" && platform != "azure" && platform != "chaos" {
		return fmt.Errorf("managed TLS is supported only for AWS, GCP, Azure, and CHAOS environments")
	}
	if cfg.CAPlacement == "pmm" && !boolDefault(cfg.EnablePmm, false) {
		return fmt.Errorf("the PMM server must be enabled when CA placement is pmm")
	}
	if platform == "chaos" && cfg.CAPlacement == "dedicated" {
		cfg.CACpuCores = intDefault(cfg.CACpuCores, 2)
		cfg.CAMemoryGb = intDefault(cfg.CAMemoryGb, 4)
		cfg.CAVolumeSize = intDefault(cfg.CAVolumeSize, 20)
		if !containsInt([]int{2, 4, 8, 16, 32}, cfg.CACpuCores) {
			return fmt.Errorf("CHAOS CA vCPU must be one of 2, 4, 8, 16, or 32")
		}
		if !containsInt([]int{4, 8, 16}, cfg.CAMemoryGb) {
			return fmt.Errorf("CHAOS CA memory must be one of 4, 8, or 16 GB")
		}
		if !containsInt([]int{20, 40, 60, 80, 100}, cfg.CAVolumeSize) {
			return fmt.Errorf("CHAOS CA disk must be one of 20, 40, 60, 80, or 100 GB")
		}
	}
	for name, cluster := range cfg.Clusters {
		if boolDefault(cluster.UseTLS, false) && boolDefault(cluster.EnableMongot, false) {
			return fmt.Errorf("cluster %q: mongot cannot be enabled with managed TLS yet", name)
		}
	}
	for name, replset := range cfg.Replsets {
		if boolDefault(replset.UseTLS, false) && boolDefault(replset.EnableMongot, false) {
			return fmt.Errorf("replica set %q: mongot cannot be enabled with managed TLS yet", name)
		}
	}
	return nil
}

func normalizeTopologyUseTLS(cfg *Config) {
	defaultUseTLS := cfg.UseTLS
	anyUsesTLS := false
	for name, cluster := range cfg.Clusters {
		if cluster.UseTLS == nil {
			useTLS := defaultUseTLS
			cluster.UseTLS = &useTLS
		}
		anyUsesTLS = anyUsesTLS || *cluster.UseTLS
		cfg.Clusters[name] = cluster
	}
	for name, replset := range cfg.Replsets {
		if replset.UseTLS == nil {
			useTLS := defaultUseTLS
			replset.UseTLS = &useTLS
		}
		anyUsesTLS = anyUsesTLS || *replset.UseTLS
		cfg.Replsets[name] = replset
	}
	cfg.UseTLS = anyUsesTLS
}

func normalizeCAProvisioning(cfg *Config) {
	if cfg.EnableCA == nil {
		enabled := cfg.UseTLS
		cfg.EnableCA = &enabled
	}
}

// pmmClientsInventory is the CHAOS inventory of the PMM server and the PMM clients without MongoDB.
const pmmClientsInventory = "pmm_clients"

// cloudInventoryNames lists the Ansible inventories Terraform generates for an environment.
func cloudInventoryNames(cfg Config) []string {
	names := make([]string, 0, len(cfg.Clusters)+len(cfg.Replsets)+1)
	for name := range cfg.Clusters {
		names = append(names, name)
	}
	for name := range cfg.Replsets {
		names = append(names, name)
	}
	if len(cfg.PmmBareClients) > 0 {
		names = append(names, pmmClientsInventory)
	}
	sort.Strings(names)
	return names
}

func topologyUsesTLS(cfg Config, name string) bool {
	if cluster, ok := cfg.Clusters[name]; ok {
		return boolDefault(cluster.UseTLS, cfg.UseTLS)
	}
	if replset, ok := cfg.Replsets[name]; ok {
		return boolDefault(replset.UseTLS, cfg.UseTLS)
	}
	return false
}

func topologyAnsibleVars(cfg Config, platform, name string, extraVars map[string]string) map[string]interface{} {
	vars := make(map[string]interface{}, len(cfg.AnsibleVars)+len(extraVars)+2)
	for key, value := range cfg.AnsibleVars {
		vars[key] = value
	}
	if strings.TrimSpace(cfg.PmmImage) != "" {
		vars["pmm_image"] = strings.TrimSpace(cfg.PmmImage)
	}
	for key, value := range extraVars {
		vars[key] = value
	}
	if platform == "aws" || platform == "gcp" || platform == "azure" || platform == "chaos" {
		vars["use_tls"] = topologyUsesTLS(cfg, name)
		if len(vaultTopologies(cfg)) > 0 {
			vars["vault_controller_dir"] = vaultControllerDir(cfg.Prefix)
		}
		if cfg.VaultVersion != "" {
			vars["vault_version"] = cfg.VaultVersion
		}
	}
	return vars
}

func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// POST /api/environment
func saveEnvironmentHandler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		EnvID    string `json:"env_id"`
		Platform string `json:"platform"`
		Config   Config `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	payload.EnvID = strings.TrimSpace(payload.EnvID)
	if payload.EnvID == "" {
		payload.EnvID = "e" + secureID(4)
	}
	if !envNameRe.MatchString(payload.EnvID) {
		jsonError(w, 400, "invalid env_id: use lowercase letters and digits only (max 16 chars)")
		return
	}
	if !validPlatform(payload.Platform) {
		jsonError(w, 400, "invalid platform")
		return
	}
	if payload.Platform == "gcp" && !gcpEnvNameRe.MatchString(payload.EnvID) {
		jsonError(w, 400, "invalid GCP environment name: it must start with a lowercase letter and contain only lowercase letters and digits (max 14 chars)")
		return
	}
	if payload.Platform == "gcp" {
		settings, err := loadAppSettings()
		if err != nil {
			jsonError(w, 500, "settings load failed: "+err.Error())
			return
		}
		if settings.GCPProjectID == "" {
			jsonError(w, 400, "GCP project ID is required in Settings")
			return
		}
		payload.Config.ProjectID = settings.GCPProjectID
	}
	// Resource names use the environment name as their prefix.
	payload.Config.Prefix = payload.EnvID

	// For Docker deployments, auto-assign unique port ranges to replica sets
	// that don't yet have a port assigned, to prevent collisions.
	state, err := loadState()
	if err != nil {
		jsonError(w, 500, "state load failed: "+err.Error())
		return
	}
	if err := dropStoredChaosTokensInState(state); err != nil {
		jsonError(w, 500, "state load failed: "+err.Error())
		return
	}
	existing := state[payload.EnvID]
	if existing == nil && payload.Platform != "docker" {
		settings, err := loadAppSettings()
		if err != nil {
			jsonError(w, 500, "settings load failed: "+err.Error())
			return
		}
		if err := applySSHSettingsToNewEnvironment(payload.Platform, &payload.Config, settings); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
	}

	if payload.Platform == "docker" {
		assignDockerReplsetPorts(&payload.Config)
		if err := validateDockerPortConflicts(payload.EnvID, payload.Config, state); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
	} else if err := normalizeAndValidatePackageConfig(payload.Platform, &payload.Config); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := normalizeAndValidateUseTLSConfig(payload.Platform, &payload.Config); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := validateVaultConfig(payload.Platform, payload.Config); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := validateBackupBucketConfig(payload.Platform, payload.Config); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if payload.Platform == "chaos" {
		normalizeChaosBackup(&payload.Config)
	}
	normalizeHardwareConfig(payload.Platform, &payload.Config)
	if errs := validateHardwareConfig(payload.Platform, payload.Config); len(errs) > 0 {
		jsonError(w, 400, "invalid compute & storage settings: "+strings.Join(errs, "; "))
		return
	}

	if err := validateMongotVersionCompatibility(payload.Platform, &payload.Config); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := normalizeAndValidateClusterSync(payload.Platform, &payload.Config); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if payload.Platform == "chaos" {
		payload.Config.LegacyChaosAPIToken = ""
		payload.Config.ChaosApiTokenPath = strings.TrimSpace(payload.Config.ChaosApiTokenPath)
		settings, err := loadAppSettings()
		if err != nil {
			jsonError(w, 500, "settings load failed: "+err.Error())
			return
		}
		tokenPath := settings.ChaosApiTokenPath
		if tokenPath != "" {
			payload.Config.ChaosApiTokenPath = ""
		} else {
			if payload.Config.ChaosApiTokenPath == "" && existing != nil {
				payload.Config.ChaosApiTokenPath = strings.TrimSpace(existing.Config.ChaosApiTokenPath)
			}
			if isManagedChaosTokenFile(payload.Config.ChaosApiTokenPath) {
				payload.Config.ChaosApiTokenPath = ""
			}
			tokenPath = payload.Config.ChaosApiTokenPath
		}
		if tokenPath == "" {
			jsonError(w, 400, "CHAOS API token file path is required in Settings")
			return
		}
	}
	if existing != nil {
		oldCS, newCS := existing.Config.ClusterSync, payload.Config.ClusterSync
		if existing.LastAppliedConfig != nil {
			oldCS = existing.LastAppliedConfig.ClusterSync
		}
		if oldCS.Enabled && newCS.Enabled && (oldCS.SourceKind != newCS.SourceKind || oldCS.SourceName != newCS.SourceName || oldCS.TargetKind != newCS.TargetKind || oldCS.TargetName != newCS.TargetName) {
			jsonError(w, 400, "changing a deployed ClusterSync relationship is not supported; disable and deploy it first")
			return
		}
		var baseline *Config
		if existing.LastAppliedConfig != nil {
			baseline = existing.LastAppliedConfig
		} else if existing.Status != "" && existing.Status != "configured" && existing.Status != "deleted" {
			// Backward-compatible guard for environments created before
			// LastAppliedConfig existed. If any action has materialized the
			// environment, treat the currently saved config as the topology that
			// cannot be mutated with unsupported operations.
			baseline = &existing.Config
		}
		if baseline != nil {
			prior := *baseline
			normalizeTopologyUseTLS(&prior)
			if prior.UseTLS && payload.Config.UseTLS && prior.CAPlacement != payload.Config.CAPlacement {
				jsonError(w, 400, "changing CA placement after deployment is not supported; create a new environment")
				return
			}
			for name, cluster := range prior.Clusters {
				if desired, ok := payload.Config.Clusters[name]; ok && boolDefault(cluster.UseTLS, false) != boolDefault(desired.UseTLS, false) {
					jsonError(w, 400, fmt.Sprintf("changing TLS for cluster %q after deployment is not supported; create a new environment", name))
					return
				}
			}
			for name, replset := range prior.Replsets {
				if desired, ok := payload.Config.Replsets[name]; ok && boolDefault(replset.UseTLS, false) != boolDefault(desired.UseTLS, false) {
					jsonError(w, 400, fmt.Sprintf("changing TLS for replica set %q after deployment is not supported; create a new environment", name))
					return
				}
			}
			_, unsupported := analyseTopologyChange(prior, payload.Config)
			if len(unsupported) > 0 {
				jsonError(w, 400, "unsupported topology change: "+strings.Join(unsupported, "; "))
				return
			}
		}
	}
	if payload.Config.ClusterSync.Enabled {
		if err := ensureClusterSyncSecrets(payload.EnvID, payload.Platform, &payload.Config); err != nil {
			jsonError(w, 500, "ClusterSync secret setup failed: "+err.Error())
			return
		}
	}
	if payload.Platform == "chaos" && existing != nil && existing.Config.ChaosApiTokenPath != payload.Config.ChaosApiTokenPath {
		removeManagedChaosTokenFile(existing.Config.ChaosApiTokenPath)
	}
	status := "configured"
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if existing != nil {
		status = existing.Status
		createdAt = existing.CreatedAt
	}
	env := &Environment{
		Platform:  payload.Platform,
		Config:    payload.Config,
		Status:    status,
		CreatedAt: createdAt,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if existing != nil {
		env.LastJobID = existing.LastJobID
		env.LastAppliedConfig = existing.LastAppliedConfig
		env.HostIPs = existing.HostIPs
		env.History = existing.History
	}
	state[payload.EnvID] = env
	if err := saveState(state); err != nil {
		jsonError(w, 500, "state save failed: "+err.Error())
		return
	}

	if err := writeTfvars(payload.EnvID, payload.Platform, payload.Config); err != nil {
		jsonError(w, 500, "tfvars write failed: "+err.Error())
		return
	}

	writeJSON(w, 200, map[string]string{"env_id": payload.EnvID, "status": "configured"})
}

// DELETE /api/environment/{env_id}
func deleteEnvironmentHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	if !envIDRe.MatchString(envID) {
		jsonError(w, 400, "invalid environment ID")
		return
	}
	state, _ := loadState()
	env := state[envID]
	delete(state, envID)
	saveState(state)

	if env != nil {
		if env.Platform == "chaos" {
			removeManagedChaosTokenFile(env.Config.ChaosApiTokenPath)
		}
		removeClusterSyncSecrets(envID)
		p := tfvarsPath(envID, env.Platform)
		os.Remove(p)
		os.Remove(tfstatePath(envID, env.Platform))
		os.Remove(tfstateBackupPath(envID, env.Platform))
		if env.Platform == "docker" {
			cleanupDockerModuleArtifacts(env.Config)
		}
	} else {
		for _, pl := range platforms {
			os.Remove(tfvarsPath(envID, pl))
			os.Remove(tfstatePath(envID, pl))
			os.Remove(tfstateBackupPath(envID, pl))
		}
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

// DELETE /api/environments/deleted
func purgeDeletedEnvironmentsHandler(w http.ResponseWriter, r *http.Request) {
	state, err := loadState()
	if err != nil {
		jsonError(w, 500, "state error: "+err.Error())
		return
	}
	count := 0
	for id, env := range state {
		if env.Status == "deleted" {
			if env.Platform == "chaos" {
				removeManagedChaosTokenFile(env.Config.ChaosApiTokenPath)
			}
			delete(state, id)
			os.Remove(tfvarsPath(id, env.Platform))
			os.Remove(tfstatePath(id, env.Platform))
			os.Remove(tfstateBackupPath(id, env.Platform))
			if env.Platform == "docker" {
				cleanupDockerModuleArtifacts(env.Config)
			}
			count++
		}
	}
	saveState(state)
	writeJSON(w, 200, map[string]interface{}{"removed": count})
}

// GET /api/environment/{env_id}/tfvars
func getTfvarsHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	if !envIDRe.MatchString(envID) {
		jsonError(w, 400, "invalid environment ID")
		return
	}
	state, _ := loadState()
	if err := dropStoredChaosTokensInState(state); err != nil {
		jsonError(w, 500, "state error: "+err.Error())
		return
	}
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}
	p := tfvarsPath(envID, env.Platform)
	content, err := os.ReadFile(p)
	if err != nil {
		writeJSON(w, 200, map[string]string{"content": "", "message": "tfvars file not yet generated"})
		return
	}
	writeJSON(w, 200, map[string]string{"content": string(content), "filename": filepath.Base(p)})
}

// GET /api/environment/{env_id}/inventory
func getInventoryHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	if !envIDRe.MatchString(envID) {
		jsonError(w, 400, "invalid environment ID")
		return
	}
	state, _ := loadState()
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}
	if env.Platform == "docker" {
		writeJSON(w, 200, map[string]interface{}{"files": []interface{}{}})
		return
	}

	tfDir := filepath.Join(terraformDir, env.Platform)

	names := cloudInventoryNames(env.Config)

	type invFile struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	filePrefix := strDefault(env.Config.Prefix, envID)
	var files []invFile
	for _, name := range names {
		p := filepath.Join(tfDir, filePrefix+"_inventory_"+name)
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		files = append(files, invFile{Name: filePrefix + "_inventory_" + name, Content: string(content)})
	}
	if env.Config.ClusterSync.Enabled {
		name := "pcsm"
		p := filepath.Join(tfDir, filePrefix+"_inventory_"+name)
		if content, err := os.ReadFile(p); err == nil {
			files = append(files, invFile{Name: filePrefix + "_inventory_" + name, Content: string(content)})
		}
	}

	if len(files) == 0 {
		writeJSON(w, 200, map[string]interface{}{
			"files":   []interface{}{},
			"message": "No inventory files found. Run Provision or Deploy first.",
		})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"files": files})
}

// GET /api/environment/{env_id}/hosts
func getHostsHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	if !envIDRe.MatchString(envID) {
		jsonError(w, 400, "invalid environment ID")
		return
	}
	state, _ := loadState()
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}

	var hosts []HostInfo
	var serviceURLs []ServiceURL
	var mongoConns []MongoConnInfo
	var msg string

	if env.Platform == "docker" {
		hosts, serviceURLs, mongoConns, msg = collectDockerHosts(envID, env)
		// Persist any newly discovered IPs so they survive container stop.
		if len(hosts) > 0 {
			updated := false
			if env.HostIPs == nil {
				env.HostIPs = make(map[string]string)
			}
			for _, h := range hosts {
				if h.IP != "" && h.IP != "—" {
					if env.HostIPs[h.Name] != h.IP {
						env.HostIPs[h.Name] = h.IP
						updated = true
					}
				}
			}
			if updated {
				state[envID] = env
				saveState(state) //nolint:errcheck
			}
		}
	} else {
		hosts, mongoConns, msg = collectCloudHosts(envID, env)
		serviceURLs = configServiceURLs(envID, env)
	}

	writeJSON(w, 200, map[string]interface{}{
		"hosts":        hosts,
		"service_urls": serviceURLs,
		"mongo_conns":  mongoConns,
		"message":      msg,
	})
}

// POST /api/environment/{env_id}/action
func environmentActionHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	if !envIDRe.MatchString(envID) {
		jsonError(w, 400, "invalid environment ID")
		return
	}
	state, _ := loadState()
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}
	if env.Config.ClusterSync.Enabled {
		if err := ensureClusterSyncSecrets(envID, env.Platform, &env.Config); err != nil {
			jsonError(w, 500, "ClusterSync secret setup failed: "+err.Error())
			return
		}
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	action := body.Action
	if action == "deploy" || action == "provision" || action == "configure" || action == "restart" {
		if err := validateVaultConfig(env.Platform, env.Config); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		if env.Platform == "docker" && len(vaultTopologies(env.Config)) > 0 {
			for _, tool := range []string{"python3", "openssl"} {
				if !toolInstalled(tool) {
					jsonError(w, 400, "Vault encryption requires controller-side "+tool+"; run scripts/install-prerequisites.sh")
					return
				}
			}
		}
		if env.LastAppliedConfig != nil {
			_, unsupported := analyseTopologyChange(*env.LastAppliedConfig, env.Config)
			if len(unsupported) > 0 {
				jsonError(w, 400, "unsupported topology change: "+strings.Join(unsupported, "; "))
				return
			}
		}
	}
	if strings.TrimSpace(action) == "" {
		jsonError(w, 400, "missing action")
		return
	}
	platform := env.Platform
	tfDir := filepath.Join(terraformDir, platform)
	varfile := tfvarsPath(envID, platform)

	if platform == "chaos" {
		switch action {
		case "deploy", "provision", "destroy":
			if !chaosProviderInstalled() {
				jsonError(w, http.StatusBadRequest, chaosProviderMissingError())
				return
			}
		}
		switch action {
		case "deploy", "stop", "restart", "destroy":
			if err := canReachChaosAPI(); err != nil {
				slog.Warn("chaos api unreachable", "env", envID, "action", action, "err", err)
				jsonError(w, http.StatusBadGateway, chaosAPIReachabilityError())
				return
			}
		}
	}
	if platform == "aws" || platform == "gcp" || platform == "azure" {
		switch action {
		case "deploy", "provision", "destroy":
			if err := requireProviderAuth(platform); err != nil {
				jsonError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}

	if _, err := os.Stat(varfile); os.IsNotExist(err) {
		if wErr := writeTfvars(envID, platform, env.Config); wErr != nil {
			jsonError(w, 500, "tfvars not found and could not be regenerated: "+wErr.Error())
			return
		}
	}

	invNames := cloudInventoryNames(env.Config)

	// filePrefix is prepended to generated inventory and ssh_config filenames so
	// that multiple environments sharing the same Terraform directory do not
	// overwrite each other's files (e.g. "myenv_inventory_cl01").
	filePrefix := strDefault(env.Config.Prefix, envID)

	cloudAnsibleCmdFor := func(playbookPath string, waitForSSH bool, names []string, extraVars map[string]string) string {
		var b strings.Builder
		for _, name := range names {
			extraVarsArg := ""
			effectiveVars := topologyAnsibleVars(env.Config, platform, name, extraVars)
			if len(vaultTopologies(env.Config)) > 0 {
				effectiveVars["vault_controller_dir"] = vaultControllerDir(envID)
			}
			if len(effectiveVars) > 0 {
				encoded, _ := json.Marshal(effectiveVars)
				extraVarsArg = " --extra-vars " + shellQuote(string(encoded))
			}
			inv := shellQuote(filePrefix + "_inventory_" + name)
			if len(vaultTopologies(env.Config)) > 0 && filepath.Base(playbookPath) != "vault_server.yml" {
				extraVarsArg += " --skip-tags vault"
			}
			b.WriteString(fmt.Sprintf(
				`{ [ -f %[1]s ] || { printf "ERROR: inventory file %%s not found\n" %[1]s; exit 1; }; `,
				inv,
			))
			if waitForSSH {
				b.WriteString(fmt.Sprintf(
					`printf "Waiting for SSH on %%s (up to 10 min)…\n" %[1]s; `+
						`_ssh_ready=false; `+
						`for _n in $(seq 1 20); do `+
						`ansible -i %[1]s all -m ping --timeout=10 2>&1 && { _ssh_ready=true; break; }; `+
						`printf "  attempt %%s/20 – not ready yet, retrying in 10s…\n" "$_n"; `+
						`[ "$_n" -lt 20 ] && sleep 10; done; `+
						`$_ssh_ready || { printf "ERROR: timed out waiting for SSH (%%s)\n" %[1]s; exit 1; }; `,
					inv,
				))
			}
			b.WriteString(fmt.Sprintf(
				`printf "==> ansible-playbook -i %%s\n" %[1]s; ansible-playbook -i %[1]s %[2]s%[3]s || exit $?; }`,
				inv, shellQuote(playbookPath), extraVarsArg,
			))
			b.WriteString(" && ")
		}
		s := strings.TrimSuffix(b.String(), " && ")
		if s == "" {
			return `printf "WARNING: no clusters or replica sets configured – nothing to run\n"`
		}
		return s
	}
	cloudAnsibleCmd := func(playbookPath string, waitForSSH bool) string {
		return cloudAnsibleCmdFor(playbookPath, waitForSSH, invNames, nil)
	}
	cloudVaultCmd := func(waitForSSH bool) string {
		return cloudAnsibleCmdFor(filepath.Join(ansibleDir, "vault_server.yml"), waitForSSH, []string{"vault"}, map[string]string{
			"vault_controller_dir": vaultControllerDir(envID),
		})
	}
	cloudPCSMCmd := func(waitForSSH bool) string {
		inv := shellQuote(filePrefix + "_inventory_pcsm")
		vars, _ := json.Marshal(map[string]string{"pcsm_env_file_source": clusterSyncEnvPath(envID)})
		var b strings.Builder
		b.WriteString(fmt.Sprintf(`{ [ -f %[1]s ] || { printf "ERROR: inventory file %%s not found\n" %[1]s; exit 1; }; `, inv))
		if waitForSSH {
			b.WriteString(fmt.Sprintf(
				`printf "Waiting for SSH on %%s (up to 10 min)…\n" %[1]s; `+
					`_ssh_ready=false; `+
					`for _n in $(seq 1 20); do `+
					`ansible -i %[1]s all -m ping --timeout=10 2>&1 && { _ssh_ready=true; break; }; `+
					`printf "  attempt %%s/20 – not ready yet, retrying in 10s…\n" "$_n"; `+
					`[ "$_n" -lt 20 ] && sleep 10; done; `+
					`$_ssh_ready || { printf "ERROR: timed out waiting for SSH (%%s)\n" %[1]s; exit 1; }; `,
				inv,
			))
		}
		b.WriteString(fmt.Sprintf(`printf "==> ansible-playbook -i %%s\n" %[1]s; ansible-playbook -i %[1]s %[2]s --extra-vars %[3]s || exit $?; }`, inv, shellQuote(filepath.Join(ansibleDir, "pcsm.yml")), shellQuote(string(vars))))
		return b.String()
	}
	cloudCertificateCmdFor := func(names []string, waitForSSH bool) string {
		steps := make([]string, 0, len(names))
		for _, name := range names {
			if !topologyUsesTLS(env.Config, name) {
				continue
			}
			stagingDir := filepath.Join("/tmp/mongo_terraform_ansible", envID, name, "cert_setup")
			steps = append(steps, cloudAnsibleCmdFor(filepath.Join(ansibleDir, "cert_setup.yml"), waitForSSH, []string{name}, map[string]string{
				"ca_staging_dir": stagingDir,
			}))
		}
		return strings.Join(steps, " && ")
	}
	cloudConfigureCmdFor := func(names []string, waitForSSH bool) string {
		steps := make([]string, 0, 2)
		if certCmd := cloudCertificateCmdFor(names, waitForSSH); certCmd != "" {
			steps = append(steps, certCmd)
		}
		steps = append(steps, cloudAnsibleCmdFor(filepath.Join(ansibleDir, "main.yml"), waitForSSH, names, nil))
		if env.Config.ClusterSync.Enabled {
			steps = append(steps, cloudPCSMCmd(waitForSSH))
		}
		return strings.Join(steps, " && ")
	}

	sshConfigInjectShell := func() string {
		if platform == "docker" || len(invNames) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString(`{ _sshcfg="${HOME}/.ssh/config"; `)
		b.WriteString(`mkdir -p "${HOME}/.ssh" && chmod 700 "${HOME}/.ssh"; `)
		b.WriteString(`[ -f "${_sshcfg}" ] || touch "${_sshcfg}"; `)
		b.WriteString(`chmod 600 "${_sshcfg}"; `)
		for _, name := range invNames {
			src := shellQuote(filePrefix + "_ssh_config_" + name)
			begin := shellQuote("# BEGIN psmdb-sandbox:" + envID + ":" + name)
			end := shellQuote("# END psmdb-sandbox:" + envID + ":" + name)
			b.WriteString(fmt.Sprintf(
				`if [ -f %[1]s ]; then `+
					`awk -v b=%[2]s -v e=%[3]s '$0==b{skip=1;next} skip&&$0==e{skip=0;next} !skip' "${_sshcfg}" > "${_sshcfg}.psmdb-sandbox_tmp" && mv "${_sshcfg}.psmdb-sandbox_tmp" "${_sshcfg}"; `+
					`printf '\n%%s\n' %[2]s >> "${_sshcfg}"; `+
					`cat %[1]s >> "${_sshcfg}"; `+
					`printf '%%s\n' %[3]s >> "${_sshcfg}"; `+
					`printf '==> Added SSH config block for %[4]s to %%s\n' "${_sshcfg}"; `+
					`fi; `,
				src, begin, end, name,
			))
		}
		b.WriteString("}")
		return " && " + b.String()
	}

	sshConfigRemoveShell := func() string {
		if platform == "docker" || len(invNames) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString(`{ _sshcfg="${HOME}/.ssh/config"; `)
		b.WriteString(`if [ -f "${_sshcfg}" ]; then `)
		for _, name := range invNames {
			begin := shellQuote("# BEGIN psmdb-sandbox:" + envID + ":" + name)
			end := shellQuote("# END psmdb-sandbox:" + envID + ":" + name)
			b.WriteString(fmt.Sprintf(
				`awk -v b=%[1]s -v e=%[2]s '$0==b{skip=1;next} skip&&$0==e{skip=0;next} !skip' "${_sshcfg}" > "${_sshcfg}.psmdb-sandbox_tmp" && mv "${_sshcfg}.psmdb-sandbox_tmp" "${_sshcfg}"; `+
					`printf '==> Removed SSH config block for %[3]s from %%s\n' "${_sshcfg}"; `,
				begin, end, name,
			))
		}
		b.WriteString("fi; }")
		return " && " + b.String()
	}

	// Per-environment Terraform state file: stored alongside the tfvars file in
	// the platform directory so different environments do not share state and
	// can be operated concurrently.
	envStateFile := envID + ".tfstate" // relative to tfDir (the CWD for terraform)
	backendPathArg := shellQuote("path=" + envStateFile)
	terraformApplyFlags := "-auto-approve -input=false"
	terraformParallelism := 0
	if settings, err := loadAppSettings(); err == nil {
		terraformParallelism = settings.TerraformParallelism
	}
	if terraformParallelism == 0 && platform == "chaos" {
		terraformParallelism = 5
	}
	if terraformParallelism > 0 {
		terraformApplyFlags += fmt.Sprintf(" -parallelism=%d", terraformParallelism)
	}
	terraformDestroyFlags := terraformApplyFlags

	var cmd []string
	configAtStart := cloneConfig(env.Config)
	appliedConfig := env.LastAppliedConfig
	if appliedConfig == nil && (env.Status == "running" || env.Status == "deploy_success") {
		// Backward-compatible fallback for state files created before
		// LastAppliedConfig existed. Treat the current saved config as applied so
		// unchanged redeploys do not look like first deploys.
		appliedConfig = cloneConfig(env.Config)
	}
	switch action {
	case "deploy":
		shellCmd := fmt.Sprintf(
			"terraform init -reconfigure -input=false -backend-config=%s && terraform apply %s -var-file=%s",
			backendPathArg,
			terraformApplyFlags,
			shellQuote(varfile),
		)
		if platform != "docker" {
			if appliedConfig != nil {
				if len(vaultTopologies(env.Config)) > 0 {
					shellCmd += " && " + cloudVaultCmd(true)
				}
				plan, unsupported := analyseTopologyChange(*appliedConfig, env.Config)
				if len(unsupported) > 0 {
					jsonError(w, 400, "unsupported topology change: "+strings.Join(unsupported, "; "))
					return
				}
				if plan.hasChanges() {
					shellCmd += sshConfigInjectShell()
					var ansibleSteps []string
					for _, name := range plan.NewClusters {
						ansibleSteps = append(ansibleSteps, cloudConfigureCmdFor([]string{name}, true))
					}
					for _, name := range plan.NewReplsets {
						ansibleSteps = append(ansibleSteps, cloudConfigureCmdFor([]string{name}, true))
					}
					clusterNames := make([]string, 0, len(plan.AddedShards))
					for name := range plan.AddedShards {
						clusterNames = append(clusterNames, name)
					}
					sort.Strings(clusterNames)
					for _, name := range clusterNames {
						if certCmd := cloudCertificateCmdFor([]string{name}, true); certCmd != "" {
							ansibleSteps = append(ansibleSteps, certCmd)
						}
						for _, shardIndex := range plan.AddedShards[name] {
							ansibleSteps = append(ansibleSteps, cloudAnsibleCmdFor(filepath.Join(ansibleDir, "add_shard.yml"), true, []string{name}, map[string]string{
								"new_shard_group": fmt.Sprintf("shard%d", shardIndex),
							}))
						}
					}
					replsetNames := make([]string, 0, len(plan.AddedReplsetNodes))
					for name := range plan.AddedReplsetNodes {
						replsetNames = append(replsetNames, name)
					}
					sort.Strings(replsetNames)
					for _, name := range replsetNames {
						if certCmd := cloudCertificateCmdFor([]string{name}, true); certCmd != "" {
							ansibleSteps = append(ansibleSteps, certCmd)
						}
						ansibleSteps = append(ansibleSteps, cloudAnsibleCmdFor(filepath.Join(ansibleDir, "add_replset_member.yml"), true, []string{name}, map[string]string{
							"target_replset": name,
						}))
					}
					if len(ansibleSteps) > 0 {
						shellCmd += " && " + strings.Join(ansibleSteps, " && ")
					}
				} else {
					shellCmd += sshConfigInjectShell()
					shellCmd += " && " + cloudConfigureCmdFor(invNames, true)
				}
			} else {
				shellCmd += sshConfigInjectShell()
				if len(vaultTopologies(env.Config)) > 0 {
					shellCmd += " && " + cloudVaultCmd(true)
				}
				shellCmd += " && " + cloudConfigureCmdFor(invNames, true)
			}
		} else if appliedConfig != nil {
			_, unsupported := analyseTopologyChange(*appliedConfig, env.Config)
			if len(unsupported) > 0 {
				jsonError(w, 400, "unsupported topology change: "+strings.Join(unsupported, "; "))
				return
			}
		}
		if postDeploy := clusterSyncPostDeployShell(envID, env); postDeploy != "" {
			shellCmd += " && " + postDeploy
		}
		if disable := clusterSyncDisableShell(envID, env, appliedConfig); disable != "" {
			shellCmd += " && " + disable
		}
		cmd = []string{"bash", "-c", shellCmd}

	case "provision":
		if platform == "docker" {
			jsonError(w, 400, "provision action is not applicable to Docker environments")
			return
		}
		if appliedConfig != nil {
			plan, unsupported := analyseTopologyChange(*appliedConfig, env.Config)
			if len(unsupported) > 0 {
				jsonError(w, 400, "unsupported topology change: "+strings.Join(unsupported, "; "))
				return
			}
			if plan.hasChanges() {
				jsonError(w, 400, "topology expansion must be run with Deploy so Terraform is followed by the required Ansible scale-out playbook")
				return
			}
		}
		shellCmd := fmt.Sprintf(
			"terraform init -reconfigure -input=false -backend-config=%s && terraform apply %s -var-file=%s",
			backendPathArg,
			terraformApplyFlags,
			shellQuote(varfile),
		)
		shellCmd += sshConfigInjectShell()
		cmd = []string{"bash", "-c", shellCmd}

	case "configure":
		if platform == "docker" {
			jsonError(w, 400, "configure action is not applicable to Docker environments")
			return
		}
		configureCmd := cloudConfigureCmdFor(invNames, true)
		if len(vaultTopologies(env.Config)) > 0 {
			configureCmd = cloudVaultCmd(true) + " && " + configureCmd
		}
		if postDeploy := clusterSyncPostDeployShell(envID, env); postDeploy != "" {
			configureCmd += " && " + postDeploy
		}
		cmd = []string{"bash", "-c", configureCmd}

	case "reset":
		if platform == "docker" {
			jsonError(w, 400, "reset action is not applicable to Docker environments")
			return
		}
		cmd = []string{"bash", "-c",
			cloudAnsibleCmd(filepath.Join(ansibleDir, "reset.yml"), false),
		}

	case "destroy":
		shellCmd := fmt.Sprintf(
			"terraform init -reconfigure -input=false -backend-config=%s && terraform destroy %s -var-file=%s",
			backendPathArg,
			terraformDestroyFlags,
			shellQuote(varfile),
		)
		if platform != "docker" {
			shellCmd += sshConfigRemoveShell()
		}
		cmd = []string{"bash", "-c", shellCmd}

	case "stop":
		if platform == "docker" {
			prefix := sanitiseShellArg(strDefault(env.Config.Prefix, envID))
			if len(vaultTopologies(env.Config)) > 0 {
				cmd = []string{"bash", "-c", vaultDockerStopShell(prefix)}
				break
			}
			cmd = []string{"bash", "-c",
				fmt.Sprintf("docker ps -q --filter 'name=%s-' | xargs -r docker stop", prefix),
			}
		} else {
			cmd = []string{"bash", "-c",
				cloudAnsibleCmd(filepath.Join(ansibleDir, "stop.yml"), false),
			}
		}

	case "restart":
		if platform == "docker" {
			if len(vaultTopologies(env.Config)) > 0 {
				cmd = []string{"bash", "-c", vaultDockerRestartShell(envID, env.Config)}
				break
			}
			prefix := sanitiseShellArg(strDefault(env.Config.Prefix, envID))
			cmd = []string{"bash", "-c",
				fmt.Sprintf("docker ps -aq --filter 'name=%s-' | xargs -r docker restart", prefix),
			}
		} else {
			cmd = []string{"bash", "-c",
				cloudAnsibleCmd(filepath.Join(ansibleDir, "restart.yml"), false),
			}
			if len(vaultTopologies(env.Config)) > 0 {
				cmd[2] = cloudVaultCmd(true) + " && " + cmd[2]
			}
		}

	default:
		jsonError(w, 400, "unknown action: "+action)
		return
	}

	startedAtTime := time.Now()
	startedAt := startedAtTime.UTC().Format(time.RFC3339)

	onComplete := func(status string) {
		durationSecs := int64(time.Since(startedAtTime).Seconds())
		st, _ := loadState()
		e, exists := st[envID]
		if !exists {
			return
		}
		// Record the event in the environment's history.
		outcomeStatus := "success"
		if status != "success" {
			outcomeStatus = "failed"
		}
		e.History = append(e.History, HistoryEvent{
			Action:       action,
			StartedAt:    startedAt,
			Status:       outcomeStatus,
			DurationSecs: durationSecs,
		})
		if action == "destroy" {
			if status == "success" {
				e.Status = "deleted"
				os.Remove(varfile)
				os.Remove(tfstatePath(envID, platform))
				os.Remove(tfstateBackupPath(envID, platform))
				if platform == "docker" {
					cleanupDockerModuleArtifacts(e.Config)
				}
				removeClusterSyncSecrets(envID)
				os.RemoveAll(vaultControllerDir(envID))
			} else {
				e.Status = "destroy_failed"
			}
			e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			st[envID] = e
			saveState(st)
			return
		}
		if status == "success" {
			switch action {
			case "restart":
				e.Status = "running"
			case "stop":
				e.Status = "stopped"
			case "reset":
				e.Status = "provisioned"
			case "deploy", "configure":
				e.Status = "running"
				if action == "deploy" {
					e.LastAppliedConfig = cloneConfig(*configAtStart)
					if !e.Config.ClusterSync.Enabled {
						removeClusterSyncSecrets(envID)
					}
				}
			default:
				e.Status = action + "_success"
			}
		} else {
			e.Status = action + "_failed"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		e.UpdatedAt = now
		st[envID] = e
		saveState(st)
	}

	extraEnv := map[string]string{
		"ANSIBLE_CONFIG": filepath.Join(ansibleDir, "ansible.cfg"),
	}
	if (platform == "aws" || platform == "gcp" || platform == "azure") && (action == "deploy" || action == "provision" || action == "destroy") {
		providerEnv, err := configuredProviderAuthEnv(platform)
		if err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		for k, v := range providerEnv {
			extraEnv[k] = v
		}
	}
	// For CHAOS environments, pass the API token via an environment variable so
	// it is never written to the tfvars file on disk.
	if platform == "chaos" {
		tokenPath, err := configuredChaosTokenPath(env)
		if err != nil {
			jsonError(w, 500, "settings load failed: "+err.Error())
			return
		}
		if tokenPath == "" {
			jsonError(w, 400, "CHAOS API token file path is required in Settings")
			return
		}
		token, err := loadChaosTokenFromPath(tokenPath)
		if err != nil {
			jsonError(w, 500, "failed to read CHAOS token file: "+err.Error())
			return
		}
		if token != "" {
			extraEnv["CHAOS_API_TOKEN"] = token
		}
	}

	jobID := startJob(cmd, tfDir, extraEnv, onComplete)

	env.Status = action + "_in_progress"
	env.LastJobID = jobID
	state[envID] = env
	saveState(state)

	slog.Info("action dispatched", "action", action, "env", envID, "platform", platform, "job", jobID)
	writeJSON(w, 200, map[string]string{"job_id": jobID, "status": env.Status})
}

// GET /api/environment/{env_id}/chaos/reachability
func chaosReachabilityHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	state, _ := loadState()
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}
	if env.Platform != "chaos" {
		jsonError(w, 400, "reachability check is only available for chaos environments")
		return
	}
	if err := canReachChaosAPI(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"reachable": false,
			"error":     chaosAPIReachabilityError(),
		})
		return
	}
	writeJSON(w, 200, map[string]bool{"reachable": true})
}

// GET /api/environment/{env_id}/history
func envHistoryHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	state, _ := loadState()
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}
	history := env.History
	if history == nil {
		history = []HistoryEvent{}
	}
	writeJSON(w, 200, history)
}

// GET /api/environment/{env_id}/status
func envStatusHandler(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("env_id")
	state, _ := loadState()
	env, ok := state[envID]
	if !ok {
		jsonError(w, 404, "environment not found")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"status":                 env.Status,
		"updated_at":             env.UpdatedAt,
		"cluster_sync_available": clusterSyncDeploymentReady(env),
	})
}

// GET /api/job/{job_id}/status
func jobStatusHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	data, err := os.ReadFile(jobStatusPath(jobID))
	if err != nil {
		writeJSON(w, 200, map[string]string{"status": "unknown"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": strings.TrimSpace(string(data))})
}

// GET /api/job/{job_id}/stream  (Server-Sent Events)
func jobStreamHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	logPath := jobLogPath(jobID)
	statusPath := jobStatusPath(jobID)

	for i := 0; i < 20; i++ {
		if _, err := os.Stat(logPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	var pos int64
	ctx := r.Context()
	for {
		if f, err := os.Open(logPath); err == nil {
			f.Seek(pos, io.SeekStart)
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := stripAnsi(scanner.Text())
				if data, err := json.Marshal(line); err == nil {
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
			}
			newPos, _ := f.Seek(0, io.SeekCurrent)
			f.Close()
			if newPos > pos {
				pos = newPos
				flusher.Flush()
			}
		}

		statusBytes, _ := os.ReadFile(statusPath)
		status := strings.TrimSpace(string(statusBytes))
		if status != "" && status != "running" {
			statusJSON, _ := json.Marshal(status)
			fmt.Fprintf(w, "event: done\ndata: %s\n\n", statusJSON)
			flusher.Flush()
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// GET /api/job/{job_id}/log
func jobLogHandler(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("job_id")
	logPath := jobLogPath(jobID)
	content, err := os.ReadFile(logPath)
	if err != nil {
		writeJSON(w, 200, map[string]string{"log": "", "status": "unknown"})
		return
	}
	statusBytes, _ := os.ReadFile(jobStatusPath(jobID))
	status := strings.TrimSpace(string(statusBytes))
	if status == "" {
		status = "unknown"
	}
	writeJSON(w, 200, map[string]string{"log": stripAnsi(string(content)), "status": status})
}

// assignDockerReplsetPorts auto-assigns unique port ranges to Docker replica
// sets that don't yet have a replset_port set, to avoid collisions when
// multiple replica sets are deployed in the same Docker environment.
// Each replica set receives a block of 20 ports (enough for 7 data nodes + 3
// arbiters). Existing non-zero port assignments are preserved; only zero-valued
// ones receive a new assignment starting above the current maximum.
func assignDockerReplsetPorts(cfg *Config) {
	if len(cfg.Replsets) == 0 {
		return
	}

	occupied := make(map[int]struct{})
	maxPort := 27017 - dockerReplsetPortStep

	for _, nr := range sortedReplsets(cfg.Replsets) {
		rs := nr.Config
		if rs.ReplsetPort > maxPort {
			maxPort = rs.ReplsetPort
		}
		if rs.ReplsetPort == 0 {
			continue
		}
		for _, port := range dockerReplsetPorts(rs) {
			occupied[port] = struct{}{}
		}
	}

	for _, svc := range cfg.PmmServers {
		port := svc.PmmExternalPort
		if port == 0 {
			port = svc.PmmPort
		}
		if port != 0 {
			occupied[port] = struct{}{}
		}
	}
	for _, svc := range cfg.SeaweedFSServers {
		if svc.SeaweedFSPort != 0 {
			occupied[svc.SeaweedFSPort] = struct{}{}
		}
		if svc.SeaweedFSAdminPort != 0 {
			occupied[svc.SeaweedFSAdminPort] = struct{}{}
		}
	}
	for _, svc := range cfg.LdapServers {
		if svc.LdapPort != 0 {
			occupied[svc.LdapPort] = struct{}{}
		}
	}

	nextPort := maxPort + dockerReplsetPortStep
	if nextPort < 27017 {
		nextPort = 27017
	}
	for _, nr := range sortedReplsets(cfg.Replsets) {
		rs := cfg.Replsets[nr.Name]
		if rs.ReplsetPort == 0 {
			for !dockerReplsetPortRangeFree(nextPort, rs, occupied) {
				nextPort += dockerReplsetPortStep
			}
			rs.ReplsetPort = nextPort
			rs.ArbiterPort = nextPort
			rs.ArbiterBasePort = nextPort + 10
			for _, port := range dockerReplsetPorts(rs) {
				occupied[port] = struct{}{}
			}
			nextPort += dockerReplsetPortStep
		} else if rs.ArbiterPort == 0 {
			rs.ArbiterPort = rs.ReplsetPort
			if rs.ArbiterBasePort == 0 {
				rs.ArbiterBasePort = rs.ReplsetPort + 10
			}
		} else if rs.ArbiterBasePort == 0 {
			rs.ArbiterBasePort = rs.ArbiterPort + intDefault(rs.DataNodesPerReplset, 2)
		}
		cfg.Replsets[nr.Name] = rs
	}
}

func dockerReplsetPortRangeFree(startPort int, rs ReplsetConfig, occupied map[int]struct{}) bool {
	candidate := rs
	candidate.ReplsetPort = startPort
	candidate.ArbiterPort = startPort
	candidate.ArbiterBasePort = startPort + 10
	for _, port := range dockerReplsetPorts(candidate) {
		if _, exists := occupied[port]; exists {
			return false
		}
	}
	return true
}

func dockerReplsetPorts(rs ReplsetConfig) []int {
	if rs.ReplsetPort == 0 {
		return nil
	}

	dataNodes := rs.DataNodesPerReplset
	if dataNodes <= 0 {
		dataNodes = 2
	}
	arbiterBase := rs.ArbiterBasePort
	if arbiterBase == 0 && rs.ArbiterPort != 0 {
		arbiterBase = rs.ArbiterPort + dataNodes
	}
	if arbiterBase == 0 {
		arbiterBase = rs.ReplsetPort + 10
	}
	arbiters := 0
	if rs.ArbitersPerReplset != nil {
		arbiters = *rs.ArbitersPerReplset
	}
	if arbiters < 0 {
		arbiters = 0
	}

	ports := make([]int, 0, dataNodes+arbiters)
	for i := 0; i < dataNodes; i++ {
		ports = append(ports, rs.ReplsetPort+i)
	}
	for i := 0; i < arbiters; i++ {
		ports = append(ports, arbiterBase+i)
	}
	return ports
}

func dockerOccupiedServicePorts(state map[string]*Environment, excludeEnvID string) map[int]struct{} {
	occupied := make(map[int]struct{})
	for envID, env := range state {
		if envID == excludeEnvID || env == nil || env.Platform != "docker" || env.Status != "running" {
			continue
		}
		for _, svc := range env.Config.PmmServers {
			port := svc.PmmExternalPort
			if port == 0 {
				port = svc.PmmPort
			}
			if port > 0 {
				occupied[port] = struct{}{}
			}
		}
		for _, svc := range env.Config.SeaweedFSServers {
			if svc.SeaweedFSPort > 0 {
				occupied[svc.SeaweedFSPort] = struct{}{}
			}
			if svc.SeaweedFSAdminPort > 0 {
				occupied[svc.SeaweedFSAdminPort] = struct{}{}
			}
		}
	}
	return occupied
}

func nextFreeDockerPort(start int, occupied map[int]struct{}) int {
	port := start
	for {
		if _, exists := occupied[port]; !exists {
			return port
		}
		port++
	}
}

func nextFreeDockerPortPair(start int, occupied map[int]struct{}) (int, int) {
	port := start
	for {
		if _, exists := occupied[port]; exists {
			port++
			continue
		}
		if _, exists := occupied[port+1]; exists {
			port++
			continue
		}
		return port, port + 1
	}
}

func nextFreeSeaweedFSPorts(start int, occupied map[int]struct{}) (int, int) {
	apiPort := start
	for {
		adminPort := apiPort + 1000
		if _, exists := occupied[apiPort]; !exists {
			if _, exists := occupied[adminPort]; !exists {
				return apiPort, adminPort
			}
		}
		apiPort++
	}
}

func addDockerConfigPortUsers(cfg Config, portUsers map[int][]string, labelPrefix string) {
	addPortUser := func(port int, user string) {
		if port <= 0 {
			return
		}
		portUsers[port] = append(portUsers[port], user)
	}

	for _, nr := range sortedReplsets(cfg.Replsets) {
		for _, port := range dockerReplsetPorts(nr.Config) {
			addPortUser(port, fmt.Sprintf("%sreplica set", labelPrefix))
		}
	}
	for _, ns := range sortedPmmServers(cfg.PmmServers) {
		port := ns.Config.PmmExternalPort
		if port == 0 {
			port = ns.Config.PmmPort
		}
		addPortUser(port, fmt.Sprintf("%sPMM server", labelPrefix))
	}
	for _, ns := range sortedSeaweedFSServers(cfg.SeaweedFSServers) {
		addPortUser(ns.Config.SeaweedFSPort, fmt.Sprintf("%sSeaweedFS API", labelPrefix))
		addPortUser(ns.Config.SeaweedFSAdminPort, fmt.Sprintf("%sSeaweedFS admin", labelPrefix))
	}
	for _, ns := range sortedLdapServers(cfg.LdapServers) {
		addPortUser(ns.Config.LdapPort, fmt.Sprintf("%sLDAP server", labelPrefix))
	}
}

func validateDockerPortConflicts(envID string, cfg Config, state map[string]*Environment) error {
	portUsers := make(map[int][]string)
	addDockerConfigPortUsers(cfg, portUsers, "this environment ")

	for otherEnvID, env := range state {
		if otherEnvID == envID || env == nil || env.Platform != "docker" || env.Status != "running" {
			continue
		}
		addDockerConfigPortUsers(env.Config, portUsers, fmt.Sprintf("environment %s ", otherEnvID))
	}

	var conflicts []string
	for port, users := range portUsers {
		if len(users) < 2 {
			continue
		}
		sort.Strings(users)
		conflicts = append(conflicts, fmt.Sprintf("port %d is used by %s", port, strings.Join(users, " and ")))
	}
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return fmt.Errorf("docker port conflicts detected:\n- %s", strings.Join(conflicts, "\n- "))
}
