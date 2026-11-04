package main

import (
	"os"
	"path/filepath"
)

// Keep backend metadata separate even when jobs share a platform source tree.
// Existing state files stay in terraform/<platform>/<env>.tfstate.
func prepareTerraformDataDir(envID, platform string) (string, error) {
	path, err := filepath.Abs(filepath.Join(filepath.Dir(stateFile), "terraform", platform, envID))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", err
	}
	return path, nil
}
