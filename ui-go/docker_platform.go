package main

import "fmt"

func validateDockerPlatform(platform string, cfg Config) error {
	if platform != "docker" {
		return nil
	}
	switch cfg.DockerPlatform {
	case "", "linux/amd64", "linux/arm64":
		return nil
	default:
		return fmt.Errorf("invalid Docker image architecture: choose Native, AMD64 (linux/amd64), or ARM64 (linux/arm64)")
	}
}
