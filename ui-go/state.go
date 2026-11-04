package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var envIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
var envNameRe = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
var gcpEnvNameRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,13}$`)

var stateMu sync.Mutex
var settingsMu sync.Mutex

func loadState() (map[string]*Environment, error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadStateLocked()
}

func loadStateLocked() (map[string]*Environment, error) {
	data, err := os.ReadFile(stateFile)
	if os.IsNotExist(err) {
		return map[string]*Environment{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state map[string]*Environment
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("environment registry must be a JSON object")
	}
	return state, nil
}

func saveState(state map[string]*Environment) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return saveStateLocked(state)
}

// updateState serializes the entire read-modify-write transaction. Callbacks
// must not call loadState, saveState, or updateState themselves.
func updateState(update func(map[string]*Environment) error) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	state, err := loadStateLocked()
	if err != nil {
		return err
	}
	if err := update(state); err != nil {
		return err
	}
	return saveStateLocked(state)
}

func saveStateLocked(state map[string]*Environment) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(stateFile, b, 0600)
}

// atomicWriteFile leaves the previous file intact if writing or syncing fails.
// The temporary file must be on the same filesystem for rename to be atomic.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".psmdb-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func loadAppSettings() (AppSettings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if settingsFile == "" {
		return AppSettings{}, nil
	}
	data, err := os.ReadFile(settingsFile)
	if os.IsNotExist(err) {
		return AppSettings{}, nil
	}
	if err != nil {
		return AppSettings{}, err
	}
	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return AppSettings{}, err
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
	return settings, nil
}

func saveAppSettings(settings AppSettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if settingsFile == "" {
		return nil
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
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(settingsFile, b, 0600)
}
