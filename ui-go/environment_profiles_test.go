package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileConfigExcludesSecretsAndIdentity(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{
		"prefix":"secret-prefix","chaos_api_token":"secret-token","chaos_api_token_path":"secret-path",
		"ansible_vars":{"mongo_admin_password":"secret-password","arbitrary":"secret-other"},
		"ssh_private_key_path":"secret-key","ssh_users":{"secret-user":"secret-pubkey"},
		"existing_bucket_secret_key":"secret-bucket","existing_bucket_endpoint":"https://secret-endpoint",
		"existing_bucket_access_key":"secret-access","default_bucket_name":"secret-bucket-name",
		"seaweedfs_secret_key":"secret-storage","seaweedfs_access_key":"secret-storage-access",
		"labels":{"secret-label":"secret-value"},"firewall_rules":[{"cidr":"secret-cidr"}],
		"region":"us-east-1","hardware_preset":"atlas-m50","enable_plgm":true,
		"clusters":{"cl01":{"mongo_release":"psmdb-80","mongo_version":"8.0.12-4","shard_count":2,"audit_filter":"secret-filter","enable_audit":true}},
		"replsets":{"rs01":{"data_nodes_per_replset":3,"arbiters_per_replset":0,"replset_port":29999}},
		"pmm_servers":{"pmm":{"pmm_server_pwd":"secret-pmm","pmm_server_user":"secret-user","pmm_external_port":8443,"pmm_server_image":"percona/pmm-server:3.10.0"}},
		"seaweedfs_servers":{"s3":{"seaweedfs_secret_key":"secret-s3","seaweedfs_access_key":"secret-access","seaweedfs_image":"chrislusf/seaweedfs:latest"}},
		"ldap_servers":{"ldap":{"ldap_admin_password":"secret-ldap","ldap_domain":"secret-domain","ldap_image":"osixia/openldap:latest"}},
		"cluster_sync":{"enabled":true,"source_name":"rs01","target_name":"rs02","include_namespaces":["secret-namespace"]}
	}`), &cfg); err != nil {
		t.Fatal(err)
	}
	clean := profileConfig(cfg)
	data, _ := json.Marshal(clean)
	if strings.Contains(string(data), "secret-") {
		t.Fatalf("profile leaked a secret or environment identity: %s", data)
	}
	if clean.Region != "us-east-1" || !clean.EnablePlgm || clean.Clusters["cl01"].MongoVersion != "8.0.12-4" {
		t.Fatal("safe configuration was lost")
	}
	if clean.Replsets["rs01"].ReplsetPort != 0 || *clean.Replsets["rs01"].ArbitersPerReplset != 0 {
		t.Fatal("ports must be cleared while explicit zero arbiters survive")
	}
	if cfg.Prefix != "secret-prefix" {
		t.Fatal("sanitizing a profile mutated the original configuration")
	}
}

func TestEnvironmentProfilesAPI(t *testing.T) {
	oldData := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() { dataDir = oldData })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/environment-profiles", environmentProfilesHandler)
	mux.HandleFunc("POST /api/environment-profiles", environmentProfilesHandler)
	mux.HandleFunc("DELETE /api/environment-profiles/{profile_id}", deleteEnvironmentProfileHandler)
	request := func(method, target, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
		return rec
	}
	create := request("POST", "/api/environment-profiles", `{"name":"My lab","platform":"chaos","config":{"prefix":"secret-name","ansible_vars":{"password":"secret-password"},"replsets":{"rs01":{"data_nodes_per_replset":3}}}}`)
	if create.Code != 201 {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	var profile EnvironmentProfile
	_ = json.Unmarshal(create.Body.Bytes(), &profile)
	if profile.ID == "" {
		t.Fatal("missing profile ID")
	}
	stored, err := os.ReadFile(filepath.Join(dataDir, "environment-profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "secret-") {
		t.Fatal("profile file contains credentials")
	}
	info, _ := os.Stat(filepath.Join(dataDir, "environment-profiles.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("profile file must be private")
	}
	if rec := request("POST", "/api/environment-profiles", `{"name":"my LAB","platform":"chaos"}`); rec.Code != 409 {
		t.Fatal("duplicate names must not overwrite profiles")
	}
	if rec := request("POST", "/api/environment-profiles", `{"name":"invalid","platform":"unknown"}`); rec.Code != 400 {
		t.Fatal("invalid platform accepted")
	}
	if rec := request("GET", "/api/environment-profiles?platform=docker", ""); strings.Contains(rec.Body.String(), "My lab") {
		t.Fatal("profiles must be scoped to the platform")
	}
	if rec := request("GET", "/api/environment-profiles?platform=chaos", ""); !strings.Contains(rec.Body.String(), "My lab") {
		t.Fatal("saved profile is missing")
	}
	if _, err := environmentProfile(profile.ID, "docker"); err == nil {
		t.Fatal("must reject cross-platform use")
	}
	otherCreate := request("POST", "/api/environment-profiles", `{"name":"Keep this lab","platform":"docker","config":{"replsets":{"rs01":{"data_nodes_per_replset":3}}}}`)
	if otherCreate.Code != 201 {
		t.Fatalf("create other profile: %d %s", otherCreate.Code, otherCreate.Body.String())
	}
	var other EnvironmentProfile
	if err := json.Unmarshal(otherCreate.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	if rec := request("DELETE", "/api/environment-profiles/"+profile.ID, ""); rec.Code != 200 {
		t.Fatal("could not delete profile")
	}
	if _, err := environmentProfile(profile.ID, "chaos"); err == nil {
		t.Fatal("deleted profile must no longer be loadable")
	}
	if _, err := environmentProfile(other.ID, "docker"); err != nil {
		t.Fatalf("deleting a profile must preserve other profiles: %v", err)
	}
	stored, err = os.ReadFile(filepath.Join(dataDir, "environment-profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var remaining map[string]EnvironmentProfile
	if err := json.Unmarshal(stored, &remaining); err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[other.ID].Name != other.Name {
		t.Fatal("deletion must persist only the remaining profile")
	}
	if rec := request("DELETE", "/api/environment-profiles/"+profile.ID, ""); rec.Code != 404 {
		t.Fatal("deleting a missing profile must return 404")
	}
}

func TestStarterEnvironments(t *testing.T) {
	for _, platform := range []string{"docker", "aws", "gcp", "azure", "chaos"} {
		for _, kind := range []string{"replica-set", "sharded", "benchmark", "migration"} {
			cfg, err := starterEnvironment(kind, platform)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Prefix != "" || len(cfg.AnsibleVars) != 0 {
				t.Fatal("starter templates must not include an identity or credentials")
			}
			if kind == "sharded" {
				if len(cfg.Clusters) != 1 || cfg.Clusters["cl01"].ShardsvrReplicas != 3 || *cfg.Clusters["cl01"].ArbitersPerReplset != 0 {
					t.Fatal("invalid sharded starter topology")
				}
			} else if cfg.Replsets["rs01"].DataNodesPerReplset != 3 {
				t.Fatal("invalid replica-set starter topology")
			}
			if kind == "benchmark" && (!cfg.EnablePlgm || cfg.UseSpotInstances) {
				t.Fatal("benchmark needs a load generator without spot instances")
			}
			if kind == "migration" && (len(cfg.Replsets) != 2 || !cfg.ClusterSync.Enabled) {
				t.Fatal("migration starter needs two replica sets and ClusterSync")
			}
		}
	}
}

func TestConfigureLoadsStartingPointsWithoutSavingEnvironments(t *testing.T) {
	oldData, oldState := dataDir, stateFile
	dataDir = t.TempDir()
	stateFile = filepath.Join(dataDir, "environments.json")
	t.Cleanup(func() { dataDir, stateFile = oldData, oldState })
	profile := EnvironmentProfile{ID: "testprofile", Name: "Saved lab", Platform: "chaos", Config: Config{DeleteAfterDays: 30, Replsets: map[string]ReplsetConfig{"savedrs": {DataNodesPerReplset: 5}}}}
	if err := writeProfilesLocked(map[string]EnvironmentProfile{profile.ID: profile}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"template=replica-set", "template=sharded", "template=benchmark", "template=migration", "profile=testprofile"} {
		req := httptest.NewRequest("GET", "/configure/chaos?"+query, nil)
		req.SetPathValue("platform", "chaos")
		rec := httptest.NewRecorder()
		configureHandler(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", query, rec.Code, rec.Body.String())
		}
		if strings.Contains(query, "profile") && !strings.Contains(rec.Body.String(), `value="savedrs"`) {
			t.Fatal("profile did not prefill the configuration")
		}
		if strings.Contains(query, "profile") && !strings.Contains(rec.Body.String(), `value="30" selected>30 days`) {
			t.Fatal("profile lifetime must not be replaced by the new-environment default")
		}
		if !strings.Contains(rec.Body.String(), `id="env_id_field"`) || !strings.Contains(rec.Body.String(), `data-env-id=""`) {
			t.Fatal("starting points must create a fresh environment")
		}
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatal("loading a starting point must not save or modify environments")
	}
}

func TestChaosHardwarePreservesCustomValues(t *testing.T) {
	fields := chaosHardwareFields(Config{ShardsvrCpuCores: 16})
	if fields[0].Value != 16 {
		t.Fatal("custom hardware selection was lost")
	}
	found := false
	for _, value := range fields[0].Options {
		found = found || value == 16
	}
	if !found {
		t.Fatal("saved hardware value must be selectable")
	}
}

func TestStartingPointDockerPortsAvoidExistingAndSiblingRanges(t *testing.T) {
	cfg, _ := starterEnvironment("migration", "docker")
	state := map[string]*Environment{"busy": {Platform: "docker", Status: "running", Config: Config{Replsets: map[string]ReplsetConfig{"rs": {ReplsetPort: 27017, DataNodesPerReplset: 3}}}}}
	if err := startingPointDockerPorts(&cfg, state); err != nil {
		t.Fatal(err)
	}
	if cfg.Replsets["rs01"].ReplsetPort <= 27019 || cfg.Replsets["rs01"].ReplsetPort == cfg.Replsets["rs02"].ReplsetPort {
		t.Fatal("starting points must get distinct free port ranges")
	}
	if state["busy"].Config.Replsets["rs"].ReplsetPort != 27017 {
		t.Fatal("must not change existing environments")
	}
}
