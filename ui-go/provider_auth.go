package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type providerAuthRequest struct {
	AWSAccessKeyID     string `json:"aws_access_key_id,omitempty"`
	AWSSecretAccessKey string `json:"aws_secret_access_key,omitempty"`
	AWSProfile         string `json:"aws_profile,omitempty"`
	AWSRegion          string `json:"aws_region,omitempty"`

	GCPProjectID         string `json:"gcp_project_id,omitempty"`
	GCPServiceAccountKey string `json:"gcp_service_account_key,omitempty"`

	AzureTenantID       string `json:"azure_tenant_id,omitempty"`
	AzureSubscriptionID string `json:"azure_subscription_id,omitempty"`
	AzureClientID       string `json:"azure_client_id,omitempty"`
	AzureClientSecret   string `json:"azure_client_secret,omitempty"`
}

type providerAuthStatus struct {
	Platform string `json:"platform"`
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
	Identity string `json:"identity,omitempty"`
}

func cloudSecretsDir() string {
	return filepath.Join(dataDir, "secrets", "cloud")
}

func awsSecretsDir() string {
	return filepath.Join(cloudSecretsDir(), "aws")
}

func awsCredentialsPath() string {
	return filepath.Join(awsSecretsDir(), "credentials")
}

func awsConfigPath() string {
	return filepath.Join(awsSecretsDir(), "config")
}

func gcpSecretsDir() string {
	return filepath.Join(cloudSecretsDir(), "gcp")
}

func gcpServiceAccountUploadPath() string {
	return filepath.Join(gcpSecretsDir(), "service-account.json")
}

func gcpConfigDir() string {
	return filepath.Join(gcpSecretsDir(), "config")
}

func azureSecretsDir() string {
	return filepath.Join(cloudSecretsDir(), "azure")
}

func azureClientSecretPath() string {
	return filepath.Join(azureSecretsDir(), "client-secret")
}

func azureConfigDir() string {
	return filepath.Join(azureSecretsDir(), "config")
}

func runCommandOutput(name string, args []string, env map[string]string) (string, error) {
	cmd := execCommand(name, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}

func execOutputEnv(env map[string]string, name string, args ...string) (string, error) {
	out, err := runCommandOutput(name, args, env)
	if err != nil {
		return "", err
	}
	return out, nil
}

func writeFile0600(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func containsLineBreak(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

func validAWSProfileName(profile string) bool {
	if profile == "" {
		return false
	}
	for _, r := range profile {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func writeAWSCredentials(profile, region, accessKeyID, secretAccessKey string) error {
	if profile == "" {
		profile = "default"
	}
	if !validAWSProfileName(profile) {
		return fmt.Errorf("AWS profile may only contain letters, numbers, underscore, dash, and dot")
	}
	if region == "" {
		return fmt.Errorf("AWS region is required")
	}
	if accessKeyID == "" || secretAccessKey == "" {
		return fmt.Errorf("AWS access key ID and secret access key are required")
	}
	if containsLineBreak(region) || containsLineBreak(accessKeyID) || containsLineBreak(secretAccessKey) {
		return fmt.Errorf("AWS credential fields must not contain line breaks")
	}
	credentials := fmt.Sprintf("[%s]\naws_access_key_id = %s\naws_secret_access_key = %s\n", profile, accessKeyID, secretAccessKey)
	configProfile := profile
	if profile != "default" {
		configProfile = "profile " + profile
	}
	config := fmt.Sprintf("[%s]\nregion = %s\noutput = json\n", configProfile, region)
	if err := writeFile0600(awsCredentialsPath(), []byte(credentials)); err != nil {
		return err
	}
	return writeFile0600(awsConfigPath(), []byte(config))
}

func loadAzureClientSecret() (string, error) {
	data, err := os.ReadFile(azureClientSecretPath())
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("Azure client secret file is empty")
	}
	return secret, nil
}

func providerAuthEnv(platform string, settings AppSettings) (map[string]string, error) {
	env := map[string]string{}
	switch platform {
	case "aws":
		profile := settings.AWSProfile
		if profile == "" {
			profile = "default"
		}
		if _, err := os.Stat(awsCredentialsPath()); err != nil {
			return nil, fmt.Errorf("AWS credentials are not configured in Settings")
		}
		env["AWS_SHARED_CREDENTIALS_FILE"] = awsCredentialsPath()
		env["AWS_CONFIG_FILE"] = awsConfigPath()
		env["AWS_PROFILE"] = profile
		return env, nil
	case "gcp":
		keyPath := strings.TrimSpace(settings.GCPServiceAccountKey)
		if keyPath == "" {
			return nil, fmt.Errorf("GCP service account key is not configured in Settings")
		}
		if _, err := os.Stat(keyPath); err != nil {
			return nil, fmt.Errorf("GCP service account key %q is not readable: %w", keyPath, err)
		}
		env["GOOGLE_APPLICATION_CREDENTIALS"] = keyPath
		env["CLOUDSDK_CONFIG"] = gcpConfigDir()
		return env, nil
	case "azure":
		if settings.AzureClientID == "" || settings.AzureTenantID == "" || settings.AzureSubscriptionID == "" {
			return nil, fmt.Errorf("Azure service principal is not configured in Settings")
		}
		secret, err := loadAzureClientSecret()
		if err != nil {
			return nil, fmt.Errorf("Azure client secret is not configured in Settings: %w", err)
		}
		env["ARM_CLIENT_ID"] = settings.AzureClientID
		env["ARM_CLIENT_SECRET"] = secret
		env["ARM_TENANT_ID"] = settings.AzureTenantID
		env["ARM_SUBSCRIPTION_ID"] = settings.AzureSubscriptionID
		env["AZURE_CONFIG_DIR"] = azureConfigDir()
		return env, nil
	default:
		return env, nil
	}
}

func configuredProviderAuthEnv(platform string) (map[string]string, error) {
	settings, err := loadAppSettings()
	if err != nil {
		return nil, err
	}
	return providerAuthEnv(platform, settings)
}

func configureProviderAuth(platform string, req providerAuthRequest) (AppSettings, error) {
	settings, err := loadAppSettings()
	if err != nil {
		return settings, err
	}
	switch platform {
	case "aws":
		profile := strings.TrimSpace(req.AWSProfile)
		if profile == "" {
			profile = "default"
		}
		region := strings.TrimSpace(req.AWSRegion)
		if err := writeAWSCredentials(profile, region, strings.TrimSpace(req.AWSAccessKeyID), strings.TrimSpace(req.AWSSecretAccessKey)); err != nil {
			return settings, err
		}
		settings.AWSProfile = profile
		settings.AWSRegion = region
	case "gcp":
		projectID := strings.TrimSpace(req.GCPProjectID)
		keyPath := strings.TrimSpace(req.GCPServiceAccountKey)
		if projectID == "" {
			return settings, fmt.Errorf("GCP project ID is required")
		}
		if keyPath == "" {
			return settings, fmt.Errorf("GCP service account key file is required")
		}
		if _, err := os.Stat(keyPath); err != nil {
			return settings, fmt.Errorf("GCP service account key %q is not readable: %w", keyPath, err)
		}
		if err := os.MkdirAll(gcpConfigDir(), 0700); err != nil {
			return settings, err
		}
		gcpEnv := map[string]string{"CLOUDSDK_CONFIG": gcpConfigDir(), "GOOGLE_APPLICATION_CREDENTIALS": keyPath}
		if _, err := runCommandOutput("gcloud", []string{"auth", "activate-service-account", "--key-file", keyPath}, gcpEnv); err != nil {
			return settings, fmt.Errorf("gcloud service account activation failed: %w", err)
		}
		if _, err := runCommandOutput("gcloud", []string{"config", "set", "project", projectID}, gcpEnv); err != nil {
			return settings, fmt.Errorf("gcloud project configuration failed: %w", err)
		}
		settings.GCPProjectID = projectID
		settings.GCPServiceAccountKey = keyPath
	case "azure":
		tenantID := strings.TrimSpace(req.AzureTenantID)
		subscriptionID := strings.TrimSpace(req.AzureSubscriptionID)
		clientID := strings.TrimSpace(req.AzureClientID)
		clientSecret := strings.TrimSpace(req.AzureClientSecret)
		if tenantID == "" || subscriptionID == "" || clientID == "" || clientSecret == "" {
			return settings, fmt.Errorf("Azure tenant ID, subscription ID, client ID, and client secret are required")
		}
		if err := writeFile0600(azureClientSecretPath(), []byte(clientSecret+"\n")); err != nil {
			return settings, err
		}
		if err := os.MkdirAll(azureConfigDir(), 0700); err != nil {
			return settings, err
		}
		azureEnv := map[string]string{"AZURE_CONFIG_DIR": azureConfigDir()}
		if _, err := runCommandOutput("az", []string{"login", "--service-principal", "--username", clientID, "--password", clientSecret, "--tenant", tenantID}, azureEnv); err != nil {
			return settings, fmt.Errorf("az service principal login failed: %w", err)
		}
		if _, err := runCommandOutput("az", []string{"account", "set", "--subscription", subscriptionID}, azureEnv); err != nil {
			return settings, fmt.Errorf("az subscription selection failed: %w", err)
		}
		settings.AzureTenantID = tenantID
		settings.AzureSubscriptionID = subscriptionID
		settings.AzureClientID = clientID
	default:
		return settings, fmt.Errorf("provider auth is not supported for %s", platform)
	}
	if err := saveAppSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func checkProviderAuth(platform string) providerAuthStatus {
	settings, err := loadAppSettings()
	if err != nil {
		return providerAuthStatus{Platform: platform, OK: false, Message: "settings load failed: " + err.Error()}
	}
	env, err := providerAuthEnv(platform, settings)
	if err != nil {
		return providerAuthStatus{Platform: platform, OK: false, Message: err.Error()}
	}
	switch platform {
	case "aws":
		out, err := runCommandOutput("aws", []string{"sts", "get-caller-identity", "--output", "json"}, env)
		if err != nil {
			return providerAuthStatus{Platform: platform, OK: false, Message: err.Error()}
		}
		var payload struct {
			Arn     string `json:"Arn"`
			Account string `json:"Account"`
		}
		_ = json.Unmarshal([]byte(out), &payload)
		identity := payload.Arn
		if identity == "" {
			identity = payload.Account
		}
		return providerAuthStatus{Platform: platform, OK: true, Identity: identity}
	case "gcp":
		out, err := runCommandOutput("gcloud", []string{"auth", "list", "--filter=status:ACTIVE", "--format=value(account)"}, env)
		if err != nil {
			return providerAuthStatus{Platform: platform, OK: false, Message: err.Error()}
		}
		if tokenOut, err := runCommandOutput("gcloud", []string{"auth", "print-access-token"}, env); err != nil || strings.TrimSpace(tokenOut) == "" {
			return providerAuthStatus{Platform: platform, OK: false, Message: "gcloud access token check failed"}
		}
		identity := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
		return providerAuthStatus{Platform: platform, OK: true, Identity: identity}
	case "azure":
		out, err := runCommandOutput("az", []string{"account", "show", "--output", "json"}, env)
		if err != nil {
			return providerAuthStatus{Platform: platform, OK: false, Message: err.Error()}
		}
		var payload struct {
			Name string `json:"name"`
			ID   string `json:"id"`
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		}
		_ = json.Unmarshal([]byte(out), &payload)
		identity := payload.User.Name
		if identity == "" {
			identity = payload.Name
		}
		if identity == "" {
			identity = payload.ID
		}
		return providerAuthStatus{Platform: platform, OK: true, Identity: identity}
	default:
		return providerAuthStatus{Platform: platform, OK: true}
	}
}

func requireProviderAuth(platform string) error {
	if platform != "aws" && platform != "gcp" && platform != "azure" {
		return nil
	}
	status := checkProviderAuth(platform)
	if !status.OK {
		if status.Message == "" {
			status.Message = "provider credentials are not configured"
		}
		return fmt.Errorf("%s credentials are not ready: %s", strings.ToUpper(platform), status.Message)
	}
	return nil
}

func uploadProviderCredentialHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if platform != "gcp" {
		jsonError(w, 400, "credential file upload is only supported for GCP")
		return
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		jsonError(w, 400, "failed to parse upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("credential_file")
	if err != nil {
		jsonError(w, 400, "credential_file field missing: "+err.Error())
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		jsonError(w, 500, "read failed: "+err.Error())
		return
	}
	if !json.Valid(data) {
		jsonError(w, 400, "uploaded GCP service account key is not valid JSON")
		return
	}
	path := gcpServiceAccountUploadPath()
	if err := writeFile0600(path, data); err != nil {
		jsonError(w, 500, "write failed: "+err.Error())
		return
	}
	settings, err := loadAppSettings()
	if err != nil {
		jsonError(w, 500, "settings load failed: "+err.Error())
		return
	}
	settings.GCPServiceAccountKey = path
	if err := saveAppSettings(settings); err != nil {
		jsonError(w, 500, "settings save failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": path, "filename": filepath.Base(header.Filename)})
}

func providerAuthHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if platform != "aws" && platform != "gcp" && platform != "azure" {
		jsonError(w, 400, "provider auth is only supported for aws, gcp, and azure")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req providerAuthRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, 400, "invalid JSON: "+err.Error())
			return
		}
		if _, err := configureProviderAuth(platform, req); err != nil {
			jsonError(w, 400, err.Error())
			return
		}
		status := checkProviderAuth(platform)
		if status.OK {
			go prefetchCloudImages(platform) // warm the image selector for the new credentials
		}
		writeJSON(w, 200, status)
	case http.MethodGet:
		writeJSON(w, 200, checkProviderAuth(platform))
	default:
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func providerAuthStatusHandler(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")
	if platform != "aws" && platform != "gcp" && platform != "azure" {
		jsonError(w, 400, "provider auth is only supported for aws, gcp, and azure")
		return
	}
	writeJSON(w, 200, checkProviderAuth(platform))
}
