package main

import (
	"os"
	"testing"
)

// Unit tests must not depend on the Docker daemon or on which ports are free
// on the machine running them.
func TestMain(m *testing.M) {
	listDockerPublishedPorts = func() ([]dockerPublishedPort, error) { return nil, nil }
	hostPortAvailable = func(int) bool { return true }
	os.Exit(m.Run())
}
