package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	pcsmSourceUser = "pcsm-source"
	pcsmTargetUser = "pcsm-target"
)

var mongoVersionLineRe = regexp.MustCompile(`(?:^|[:_-])(\d+)\.(\d+)(?:\.(\d+))?`)
var mongoReleaseLineRe = regexp.MustCompile(`^psmdb-(\d+)(\d)$`)

type clusterSyncSecrets struct {
	SourcePassword string `json:"source_password"`
	TargetPassword string `json:"target_password"`
}

type clusterSyncStatusSnapshot struct {
	State       string `json:"state"`
	InitialSync struct {
		Completed bool `json:"completed"`
	} `json:"initialSync"`
}

func normalizedClusterSyncConfig(cfg ClusterSyncConfig) ClusterSyncConfig {
	if cfg.Version == "" {
		cfg.Version = "0.9.0"
	}
	if cfg.Repo == "" {
		cfg.Repo = "release"
	}
	if cfg.Image == "" {
		cfg.Image = "percona/percona-clustersync-mongodb:" + cfg.Version
	}
	if cfg.CPUs == 0 {
		cfg.CPUs = 2
	}
	if cfg.MemoryMB == 0 {
		cfg.MemoryMB = 1024
	}
	return cfg
}

func normalizeAndValidateClusterSync(platform string, cfg *Config) error {
	cs := normalizedClusterSyncConfig(cfg.ClusterSync)
	cs.SourceKind = strings.TrimSpace(cs.SourceKind)
	cs.SourceName = strings.TrimSpace(cs.SourceName)
	cs.TargetKind = strings.TrimSpace(cs.TargetKind)
	cs.TargetName = strings.TrimSpace(cs.TargetName)
	cs.IncludeNamespaces = cleanNamespaces(cs.IncludeNamespaces)
	cs.ExcludeNamespaces = cleanNamespaces(cs.ExcludeNamespaces)
	cfg.ClusterSync = cs
	if !cs.Enabled {
		return nil
	}
	if cs.SourceKind != cs.TargetKind || (cs.SourceKind != "cluster" && cs.SourceKind != "replset") {
		return errors.New("ClusterSync requires cluster-to-cluster or replica-set-to-replica-set replication")
	}
	if cs.SourceName == cs.TargetName {
		return errors.New("ClusterSync source and target must be different")
	}
	if !topologyExists(*cfg, cs.SourceKind, cs.SourceName) || !topologyExists(*cfg, cs.TargetKind, cs.TargetName) {
		return errors.New("ClusterSync source and target must reference configured topologies")
	}
	sourceVersion, _, sourceMajor := clusterSyncTopologyPackage(*cfg, cs.SourceKind, cs.SourceName)
	targetVersion, targetDistribution, targetMajor := clusterSyncTopologyPackage(*cfg, cs.TargetKind, cs.TargetName)
	if platform != "docker" && targetDistribution != "psmdb" {
		return errors.New("ClusterSync target must run Percona Server for MongoDB")
	}
	if sourceMajor != 0 && targetMajor != 0 && sourceMajor > targetMajor {
		return errors.New("ClusterSync does not support replication to an older MongoDB major version")
	}
	if cs.SourceKind == "cluster" && sourceMajor != 0 && targetMajor != 0 && sourceMajor != targetMajor {
		return errors.New("sharded ClusterSync requires source and target to use the same MongoDB major version")
	}
	if err := validatePCSMMongoVersion(sourceVersion); err != nil {
		return fmt.Errorf("ClusterSync source: %w", err)
	}
	if err := validatePCSMMongoVersion(targetVersion); err != nil {
		return fmt.Errorf("ClusterSync target: %w", err)
	}
	if cs.CPUs < 1 || cs.MemoryMB < 1024 {
		return errors.New("ClusterSync requires at least 1 CPU and 1024 MB memory")
	}
	if cs.Version == "" || strings.ContainsAny(cs.Version, " \t\r\n") {
		return errors.New("invalid ClusterSync version")
	}
	if platform == "docker" && strings.TrimSpace(cs.Image) == "" {
		return errors.New("ClusterSync Docker image is required")
	}
	for _, value := range append(append([]string{}, cs.IncludeNamespaces...), cs.ExcludeNamespaces...) {
		if !validNamespacePattern(value) {
			return fmt.Errorf("invalid ClusterSync namespace pattern %q", value)
		}
	}
	for _, value := range []int{cs.CloneParallelCollections, cs.CloneReadWorkers, cs.CloneInsertWorkers, cs.ReplicationWorkers, cs.ChangeStreamBatchSize, cs.EventQueueSize, cs.WorkerQueueSize, cs.BulkOpsSize} {
		if value < 0 {
			return errors.New("ClusterSync tuning values cannot be negative")
		}
	}
	return nil
}

func clusterSyncTopologyPackage(cfg Config, kind, name string) (version, distribution string, major int) {
	version, distribution = strDefault(cfg.MongoVersion, cfg.MongoRelease), cfg.MongoDBDistribution
	if kind == "cluster" {
		topology := cfg.Clusters[name]
		if topology.MongoVersion != "" {
			version = topology.MongoVersion
		} else if topology.MongoRelease != "" {
			version = topology.MongoRelease
		}
		if topology.MongoDBDistribution != "" {
			distribution = topology.MongoDBDistribution
		}
		if version == "" {
			version = topology.PsmdbImage
		}
	} else {
		topology := cfg.Replsets[name]
		if topology.MongoVersion != "" {
			version = topology.MongoVersion
		} else if topology.MongoRelease != "" {
			version = topology.MongoRelease
		}
		if topology.MongoDBDistribution != "" {
			distribution = topology.MongoDBDistribution
		}
		if version == "" {
			version = topology.PsmdbImage
		}
	}
	distribution = normalizePackageDistribution(distribution)
	if distribution == "" {
		distribution = "psmdb"
	}
	major, _, _, _, _ = parseMongoVersion(version)
	if major == 0 && version == "" {
		major = 8
	}
	return
}

func validatePCSMMongoVersion(version string) error {
	major, minor, patch, hasPatch, ok := parseMongoVersion(version)
	if !ok {
		return nil
	}
	if minor != 0 {
		return fmt.Errorf("MongoDB %d.%d is not a supported PCSM version line", major, minor)
	}
	minimum, supported := map[int]int{6: 17, 7: 13, 8: 0}[major]
	if !supported {
		return fmt.Errorf("MongoDB %d.%d is not a verified PCSM version line; supported lines are 6.0, 7.0 and 8.0", major, minor)
	}
	if hasPatch && patch < minimum {
		return fmt.Errorf("MongoDB %d.0.%d is below the supported minimum %d.0.%d", major, patch, major, minimum)
	}
	return nil
}

func parseMongoVersion(version string) (major, minor, patch int, hasPatch, ok bool) {
	if match := mongoVersionLineRe.FindStringSubmatch(version); len(match) == 4 {
		major, _ = strconv.Atoi(match[1])
		minor, _ = strconv.Atoi(match[2])
		if match[3] != "" {
			patch, _ = strconv.Atoi(match[3])
			hasPatch = true
		}
		return major, minor, patch, hasPatch, true
	}
	if match := mongoReleaseLineRe.FindStringSubmatch(version); len(match) == 3 {
		major, _ = strconv.Atoi(match[1])
		minor, _ = strconv.Atoi(match[2])
		return major, minor, 0, false, true
	}
	return 0, 0, 0, false, false
}

func topologyExists(cfg Config, kind, name string) bool {
	if kind == "cluster" {
		_, ok := cfg.Clusters[name]
		return ok
	}
	_, ok := cfg.Replsets[name]
	return ok
}

func cleanNamespaces(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func validNamespacePattern(value string) bool {
	parts := strings.Split(value, ".")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.ContainsAny(value, " /\\\t\r\n")
}

func clusterSyncEnvPath(envID string) string {
	return filepath.Join(pcsmSecretsDir, envID, "pcsm.env")
}

func clusterSyncSecretPath(envID string) string {
	return filepath.Join(pcsmSecretsDir, envID, "credentials.json")
}

func clusterSyncBootstrapPath(envID string) string {
	return filepath.Join(pcsmSecretsDir, envID, "bootstrap.sh")
}

func clusterSyncCleanupPath(envID string) string {
	return filepath.Join(pcsmSecretsDir, envID, "cleanup.sh")
}

func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ensureClusterSyncSecrets(envID, platform string, cfg *Config) error {
	dir := filepath.Join(pcsmSecretsDir, envID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	secrets := clusterSyncSecrets{}
	if data, err := os.ReadFile(clusterSyncSecretPath(envID)); err == nil {
		if err := json.Unmarshal(data, &secrets); err != nil {
			return err
		}
	}
	var err error
	if secrets.SourcePassword == "" {
		secrets.SourcePassword, err = randomPassword()
		if err != nil {
			return err
		}
	}
	if secrets.TargetPassword == "" {
		secrets.TargetPassword, err = randomPassword()
		if err != nil {
			return err
		}
	}
	data, _ := json.MarshalIndent(secrets, "", "  ")
	if err := os.WriteFile(clusterSyncSecretPath(envID), data, 0600); err != nil {
		return err
	}
	cs := normalizedClusterSyncConfig(cfg.ClusterSync)
	sourceURI := clusterSyncURI(envID, platform, *cfg, cs.SourceKind, cs.SourceName, pcsmSourceUser, secrets.SourcePassword)
	targetURI := clusterSyncURI(envID, platform, *cfg, cs.TargetKind, cs.TargetName, pcsmTargetUser, secrets.TargetPassword)
	envData := "PCSM_SOURCE_URI=" + shellEnvQuote(sourceURI) + "\nPCSM_TARGET_URI=" + shellEnvQuote(targetURI) + "\n" +
		"PCSM_SOURCE_PASSWORD=" + shellEnvQuote(secrets.SourcePassword) + "\nPCSM_TARGET_PASSWORD=" + shellEnvQuote(secrets.TargetPassword) + "\n"
	if err := os.WriteFile(clusterSyncEnvPath(envID), []byte(envData), 0600); err != nil {
		return err
	}
	adminUser, adminPassword := mongoAdminCredentials(&Environment{Config: *cfg})
	if platform == "docker" {
		return writeDockerClusterSyncBootstrap(envID, *cfg, secrets, adminUser, adminPassword)
	}
	return nil
}

func topologyControlContainer(envID string, cfg Config, kind, name string) string {
	prefix := strDefault(cfg.Prefix, envID)
	if kind == "cluster" {
		return fmt.Sprintf("%s-%s-mongos00", prefix, name)
	}
	return fmt.Sprintf("%s-%s-svr0", prefix, name)
}

func topologyControlPort(cfg Config, kind, name string) int {
	if kind == "cluster" {
		return 27017
	}
	return intDefault(cfg.Replsets[name].ReplsetPort, 27017)
}

func writeDockerClusterSyncBootstrap(envID string, cfg Config, secrets clusterSyncSecrets, adminUser, adminPassword string) error {
	cs := cfg.ClusterSync
	envPath := filepath.Join(pcsmSecretsDir, envID, "bootstrap.env")
	content := "MONGO_ADMIN_USER=" + shellEnvQuote(adminUser) + "\nMONGO_ADMIN_PASSWORD=" + shellEnvQuote(adminPassword) + "\n" +
		"PCSM_SOURCE_PASSWORD=" + shellEnvQuote(secrets.SourcePassword) + "\nPCSM_TARGET_PASSWORD=" + shellEnvQuote(secrets.TargetPassword) + "\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		return err
	}
	source := topologyControlContainer(envID, cfg, cs.SourceKind, cs.SourceName)
	target := topologyControlContainer(envID, cfg, cs.TargetKind, cs.TargetName)
	js := `const admin=db.getSiblingDB("admin"); if(!admin.auth(process.env.MONGO_ADMIN_USER,process.env.MONGO_ADMIN_PASSWORD)) throw new Error("admin authentication failed"); const roles=process.env.PCSM_ROLES.split(",").map(role=>({role,db:"admin"})); if(admin.getUser(process.env.PCSM_USERNAME)){admin.updateUser(process.env.PCSM_USERNAME,{pwd:process.env.PCSM_PASSWORD,roles});}else{admin.createUser({user:process.env.PCSM_USERNAME,pwd:process.env.PCSM_PASSWORD,roles});}`
	var script strings.Builder
	script.WriteString("#!/bin/sh\nset -eu\nset -a\n. " + shellEnvQuote(envPath) + "\nset +a\n")
	writeUser := func(container string, port int, user, passwordVar, roles string) {
		script.WriteString("PCSM_USERNAME=" + shellEnvQuote(user) + " PCSM_PASSWORD=\"$" + passwordVar + "\" PCSM_ROLES=" + shellEnvQuote(roles) + " \\\n")
		script.WriteString("docker exec -e MONGO_ADMIN_USER -e MONGO_ADMIN_PASSWORD -e PCSM_USERNAME -e PCSM_PASSWORD -e PCSM_ROLES " + shellEnvQuote(container) + " mongosh --quiet --host localhost --port " + strconv.Itoa(port) + " --eval " + shellEnvQuote(js) + "\n")
	}
	writeUser(source, topologyControlPort(cfg, cs.SourceKind, cs.SourceName), pcsmSourceUser, "PCSM_SOURCE_PASSWORD", "backup,clusterMonitor,readAnyDatabase")
	writeUser(target, topologyControlPort(cfg, cs.TargetKind, cs.TargetName), pcsmTargetUser, "PCSM_TARGET_PASSWORD", "restore,clusterMonitor,clusterManager,readWriteAnyDatabase")
	script.WriteString("docker restart " + shellEnvQuote(strDefault(cfg.Prefix, envID)+"-pcsm") + " >/dev/null\n")
	script.WriteString("_ready=false\nfor _n in 1 2 3 4 5 6; do docker exec " + shellEnvQuote(strDefault(cfg.Prefix, envID)+"-pcsm") + " pcsm status >/dev/null 2>&1 && { _ready=true; break; }; sleep 5; done\n$_ready || { echo 'PCSM did not become ready' >&2; exit 1; }\n")
	if err := os.WriteFile(clusterSyncBootstrapPath(envID), []byte(script.String()), 0700); err != nil {
		return err
	}
	dropJS := `const admin=db.getSiblingDB("admin"); if(!admin.auth(process.env.MONGO_ADMIN_USER,process.env.MONGO_ADMIN_PASSWORD)) throw new Error("admin authentication failed"); if(admin.getUser(process.env.PCSM_USERNAME)) admin.dropUser(process.env.PCSM_USERNAME);`
	var cleanup strings.Builder
	cleanup.WriteString("#!/bin/sh\nset -eu\nset -a\n. " + shellEnvQuote(envPath) + "\nset +a\n")
	writeDrop := func(container string, port int, user string) {
		cleanup.WriteString("PCSM_USERNAME=" + shellEnvQuote(user) + " docker exec -e MONGO_ADMIN_USER -e MONGO_ADMIN_PASSWORD -e PCSM_USERNAME " + shellEnvQuote(container) + " mongosh --quiet --host localhost --port " + strconv.Itoa(port) + " --eval " + shellEnvQuote(dropJS) + "\n")
	}
	writeDrop(source, topologyControlPort(cfg, cs.SourceKind, cs.SourceName), pcsmSourceUser)
	writeDrop(target, topologyControlPort(cfg, cs.TargetKind, cs.TargetName), pcsmTargetUser)
	return os.WriteFile(clusterSyncCleanupPath(envID), []byte(cleanup.String()), 0700)
}

func clusterSyncPostDeployShell(envID string, env *Environment) string {
	if env == nil || !env.Config.ClusterSync.Enabled {
		return ""
	}
	if env.Platform == "docker" {
		return shellQuote(clusterSyncBootstrapPath(envID))
	}
	return ""
}

func clusterSyncDisableShell(envID string, env *Environment, applied *Config) string {
	if env == nil || applied == nil || !applied.ClusterSync.Enabled || env.Config.ClusterSync.Enabled {
		return ""
	}
	if env.Platform == "docker" {
		return shellQuote(clusterSyncCleanupPath(envID))
	}
	prefix := strDefault(env.Config.Prefix, envID)
	return fmt.Sprintf("ansible-playbook -i %s %s --tags pcsm --extra-vars pcsm_env_file_source=%s --extra-vars pcsm_state=absent", shellQuote(prefix+"_inventory_pcsm"), shellQuote(filepath.Join(ansibleDir, "main.yml")), shellQuote(clusterSyncEnvPath(envID)))
}

func shellEnvQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func clusterSyncURI(envID, platform string, cfg Config, kind, name, user, password string) string {
	prefix := strDefault(cfg.Prefix, envID)
	members := []string{}
	replicaSet := ""
	if kind == "cluster" {
		count := intDefault(cfg.Clusters[name].MongosCount, 2)
		for i := 0; i < count; i++ {
			host := fmt.Sprintf("%s-%s-mongodb-mongos0%d", prefix, name, i)
			if platform == "docker" {
				host = fmt.Sprintf("%s-%s-mongos0%d", prefix, name, i)
			}
			members = append(members, host+":27017")
		}
	} else {
		rs := cfg.Replsets[name]
		count := intDefault(rs.DataNodesPerReplset, 2)
		for i := 0; i < count; i++ {
			host, port := fmt.Sprintf("%s-%s-mongodb-svr%d", prefix, name, i), 27017
			if platform == "docker" {
				host, port = fmt.Sprintf("%s-%s-svr%d", prefix, name, i), intDefault(rs.ReplsetPort, 27017)+i
			}
			members = append(members, host+":"+strconv.Itoa(port))
		}
		replicaSet = name
		if platform == "docker" {
			replicaSet = prefix + "-" + name
		}
	}
	query := url.Values{"authSource": {"admin"}, "appName": {"pcsm"}}
	if replicaSet != "" {
		query.Set("replicaSet", replicaSet)
	}
	if platform != "docker" && topologyUsesTLS(cfg, name) {
		query.Set("tls", "true")
		query.Set("tlsCAFile", "/etc/ssl/test-ca.pem")
	}
	return "mongodb://" + url.QueryEscape(user) + ":" + url.QueryEscape(password) + "@" + strings.Join(members, ",") + "/?" + query.Encode()
}

func removeClusterSyncSecrets(envID string) {
	_ = os.RemoveAll(filepath.Join(pcsmSecretsDir, envID))
}

func redactClusterSyncOutput(envID, output string) string {
	data, err := os.ReadFile(clusterSyncSecretPath(envID))
	if err != nil {
		return output
	}
	var secrets clusterSyncSecrets
	if json.Unmarshal(data, &secrets) != nil {
		return output
	}
	for _, secret := range []string{secrets.SourcePassword, secrets.TargetPassword, url.QueryEscape(secrets.SourcePassword), url.QueryEscape(secrets.TargetPassword)} {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[REDACTED]")
		}
	}
	return output
}

func clusterSyncCommand(envID string, env *Environment, args ...string) (string, []string) {
	host := strDefault(env.Config.Prefix, envID) + "-pcsm"
	if env.Platform == "docker" {
		return "docker", append([]string{"exec", host, "pcsm"}, args...)
	}
	sshConfig := filepath.Join(terraformDir, env.Platform, strDefault(env.Config.Prefix, envID)+"_ssh_config_"+env.Config.ClusterSync.SourceName)
	return "ssh", append([]string{"-F", sshConfig, host, "pcsm"}, args...)
}

func loadClusterSyncEnvironment(w http.ResponseWriter, r *http.Request) (string, *Environment, bool) {
	envID := r.PathValue("env_id")
	if !envIDRe.MatchString(envID) {
		jsonError(w, 400, "invalid environment ID")
		return "", nil, false
	}
	state, err := loadState()
	if err != nil {
		jsonError(w, 500, "state load failed")
		return "", nil, false
	}
	env := state[envID]
	if env == nil {
		jsonError(w, 404, "environment not found")
		return "", nil, false
	}
	if !env.Config.ClusterSync.Enabled {
		jsonError(w, 400, "ClusterSync is not enabled")
		return "", nil, false
	}
	return envID, env, true
}

func clusterSyncStatusHandler(w http.ResponseWriter, r *http.Request) {
	envID, env, ok := loadClusterSyncEnvironment(w, r)
	if !ok {
		return
	}
	name, args := clusterSyncCommand(envID, env, "status")
	out, commandError, err := clusterSyncOutput(name, args...)
	if err != nil {
		jsonError(w, 502, "PCSM is unavailable: "+strings.TrimSpace(redactClusterSyncOutput(envID, commandError)))
		return
	}
	var status interface{}
	if err := json.Unmarshal(out, &status); err != nil {
		jsonError(w, 502, "invalid PCSM status response")
		return
	}
	writeJSON(w, 200, status)
}

func clusterSyncActionHandler(w http.ResponseWriter, r *http.Request) {
	envID, env, ok := loadClusterSyncEnvironment(w, r)
	if !ok {
		return
	}
	action := r.PathValue("action")
	var fromFailure bool
	if action == "resume" {
		var body struct {
			FromFailure bool `json:"from_failure"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fromFailure = body.FromFailure
	}
	if action != "reset" {
		if err := validateClusterSyncAction(envID, env, action, fromFailure); err != nil {
			jsonError(w, 409, err.Error())
			return
		}
	}
	args := []string{action}
	switch action {
	case "start":
		args = clusterSyncStartArgs(env.Config.ClusterSync)
	case "pause", "finalize":
	case "resume":
		if fromFailure {
			args = append(args, "--from-failure")
		}
	case "reset":
		out, err := resetClusterSync(envID, env)
		if err != nil {
			jsonError(w, 502, strings.TrimSpace(redactClusterSyncOutput(envID, out)))
			return
		}
		writeJSON(w, 200, map[string]interface{}{"ok": true, "output": strings.TrimSpace(out)})
		return
	default:
		jsonError(w, 400, "unknown ClusterSync action")
		return
	}
	name, commandArgs := clusterSyncCommand(envID, env, args...)
	out, commandError, err := clusterSyncOutput(name, commandArgs...)
	if err != nil {
		jsonError(w, 502, strings.TrimSpace(redactClusterSyncOutput(envID, commandError)))
		return
	}
	var result interface{}
	if json.Unmarshal(out, &result) != nil {
		result = map[string]interface{}{"ok": true, "output": strings.TrimSpace(string(out))}
	}
	writeJSON(w, 200, result)
}

func validateClusterSyncAction(envID string, env *Environment, action string, fromFailure bool) error {
	name, args := clusterSyncCommand(envID, env, "status")
	out, commandError, err := clusterSyncOutput(name, args...)
	if err != nil {
		message := strings.TrimSpace(redactClusterSyncOutput(envID, commandError))
		if message == "" {
			message = "PCSM status is unavailable"
		}
		return errors.New(message)
	}
	var status clusterSyncStatusSnapshot
	if err := json.Unmarshal(out, &status); err != nil {
		return errors.New("invalid PCSM status response")
	}
	state := strings.ToLower(strings.NewReplacer(" ", "_", "-", "_").Replace(status.State))
	unstarted := map[string]bool{"idle": true, "created": true, "ready": true, "not_started": true, "notstarted": true, "initial": true}
	paused := state == "paused" || strings.HasSuffix(state, "_paused")
	failed := state == "failed" || strings.HasSuffix(state, "_failed") || strings.Contains(state, "failure")
	finalized := state == "finalized" || state == "completed" || state == "complete" || state == "done" || strings.HasSuffix(state, "_finalized")
	finalizable := !finalized && (state == "finalizable" || state == "ready_to_finalize" || state == "ready_for_finalize" || (state == "running" && status.InitialSync.Completed))
	running := state == "running" || (!unstarted[state] && !paused && !failed && !finalized && !finalizable)

	allowed := false
	switch action {
	case "start":
		allowed = unstarted[state]
	case "pause":
		allowed = running
	case "resume":
		allowed = (paused && !fromFailure) || (failed && fromFailure)
	case "finalize":
		allowed = finalizable
	default:
		return errors.New("unknown PCSM action")
	}
	if !allowed {
		return fmt.Errorf("PCSM action %q is not valid in state %q", action, status.State)
	}
	return nil
}

func clusterSyncOutput(name string, args ...string) ([]byte, string, error) {
	cmd := exec.Command(name, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return out, stderr.String(), err
}

func resetClusterSync(envID string, env *Environment) (string, error) {
	host := strDefault(env.Config.Prefix, envID) + "-pcsm"
	if env.Platform == "docker" {
		if out, err := exec.Command("docker", "stop", host).CombinedOutput(); err != nil {
			return string(out), err
		}
		network := strDefault(env.Config.Prefix, envID) + "-" + strDefault(env.Config.NetworkName, "mongo-terraform")
		image := normalizedClusterSyncConfig(env.Config.ClusterSync).Image
		mount := "type=bind,source=" + clusterSyncEnvPath(envID) + ",target=/run/secrets/pcsm.env,readonly"
		script := `. /run/secrets/pcsm.env; exec pcsm reset --target "$PCSM_TARGET_URI"`
		out, resetErr := exec.Command("docker", "run", "--rm", "--user", "0:0", "--network", network, "--mount", mount, "--entrypoint", "/bin/sh", image, "-c", script).CombinedOutput()
		startOut, startErr := exec.Command("docker", "start", host).CombinedOutput()
		if resetErr != nil {
			return string(out), resetErr
		}
		if startErr != nil {
			return string(out) + "\n" + string(startOut), startErr
		}
		return string(out), nil
	}
	sshConfig := filepath.Join(terraformDir, env.Platform, strDefault(env.Config.Prefix, envID)+"_ssh_config_"+env.Config.ClusterSync.SourceName)
	ssh := func(command string) ([]byte, error) {
		return exec.Command("ssh", "-F", sshConfig, host, command).CombinedOutput()
	}
	if out, err := ssh("sudo systemctl stop pcsm"); err != nil {
		return string(out), err
	}
	command := "sudo sh -c " + shellQuote(`. /etc/pcsm/pcsm.env; exec pcsm reset --target "$PCSM_TARGET_URI"`)
	out, resetErr := ssh(command)
	startOut, startErr := ssh("sudo systemctl start pcsm")
	if resetErr != nil {
		return string(out), resetErr
	}
	if startErr != nil {
		return string(out) + "\n" + string(startOut), startErr
	}
	return string(out), nil
}

func clusterSyncStartArgs(cfg ClusterSyncConfig) []string {
	args := []string{"start"}
	add := func(flag, value string) {
		if value != "" {
			args = append(args, flag, value)
		}
	}
	add("--include-namespaces", strings.Join(cfg.IncludeNamespaces, ","))
	add("--exclude-namespaces", strings.Join(cfg.ExcludeNamespaces, ","))
	values := []struct {
		flag  string
		value int
	}{
		{"--clone-num-parallel-collections", cfg.CloneParallelCollections}, {"--clone-num-read-workers", cfg.CloneReadWorkers},
		{"--clone-num-insert-workers", cfg.CloneInsertWorkers}, {"--repl-num-workers", cfg.ReplicationWorkers},
		{"--repl-change-stream-batch-size", cfg.ChangeStreamBatchSize}, {"--repl-event-queue-size", cfg.EventQueueSize},
		{"--repl-worker-queue-size", cfg.WorkerQueueSize}, {"--repl-bulk-ops-size", cfg.BulkOpsSize},
	}
	for _, value := range values {
		if value.value > 0 {
			add(value.flag, strconv.Itoa(value.value))
		}
	}
	add("--clone-segment-size", cfg.CloneSegmentSize)
	return args
}

func clusterSyncLogHandler(w http.ResponseWriter, r *http.Request) {
	envID, env, ok := loadClusterSyncEnvironment(w, r)
	if !ok {
		return
	}
	var out []byte
	var err error
	if env.Platform == "docker" {
		out, err = exec.Command("docker", "logs", "--tail", "300", strDefault(env.Config.Prefix, envID)+"-pcsm").CombinedOutput()
	} else {
		sshConfig := filepath.Join(terraformDir, env.Platform, strDefault(env.Config.Prefix, envID)+"_ssh_config_"+env.Config.ClusterSync.SourceName)
		out, err = exec.Command("ssh", "-F", sshConfig, strDefault(env.Config.Prefix, envID)+"-pcsm", "sudo", "journalctl", "-u", "pcsm", "-n", "300", "--no-pager").CombinedOutput()
	}
	if err != nil {
		jsonError(w, 502, strings.TrimSpace(redactClusterSyncOutput(envID, string(out))))
		return
	}
	writeJSON(w, 200, map[string]string{"log": redactClusterSyncOutput(envID, string(out))})
}
