package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStateHostAddresses(t *testing.T) {
	state := `{"resources":[
	  {"mode":"managed","type":"google_compute_instance","instances":[{"attributes":{
	    "name":"rs-mongodb-svr0",
	    "network_interface":[{"network_ip":"10.128.0.5","access_config":[{"nat_ip":"34.47.44.183"}]}]}}]},
	  {"mode":"managed","type":"aws_instance","instances":[{"attributes":{
	    "tags":{"Name":"aws-plgm"},"private_ip":"10.0.0.7","public_ip":"3.3.3.3"}}]},
	  {"mode":"managed","type":"azurerm_linux_virtual_machine","instances":[{"attributes":{
	    "name":"az-pmm","private_ip_address":"10.1.0.4","public_ip_address":"20.0.0.1"}}]},
	  {"mode":"data","type":"google_compute_instance","instances":[{"attributes":{"name":"ignored"}}]}
	]}`
	path := filepath.Join(t.TempDir(), "env.tfstate")
	if err := os.WriteFile(path, []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readStateHostAddresses(path)
	want := map[string]hostAddresses{
		"rs-mongodb-svr0": {Private: "10.128.0.5", Public: "34.47.44.183"},
		"aws-plgm":        {Private: "10.0.0.7", Public: "3.3.3.3"},
		"az-pmm":          {Private: "10.1.0.4", Public: "20.0.0.1"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %#v", len(got), len(want), got)
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: got %#v, want %#v", name, got[name], w)
		}
	}

	if missing := readStateHostAddresses(filepath.Join(t.TempDir(), "missing.tfstate")); len(missing) != 0 {
		t.Errorf("missing state: got %#v, want empty", missing)
	}
}

func TestApplyHostAddresses(t *testing.T) {
	addrs := map[string]hostAddresses{
		"rs-mongodb-svr0": {Private: "10.128.0.5", Public: "34.47.44.183"},
		"no-public":       {Private: "10.128.0.9"},
	}
	hosts := []HostInfo{
		{Name: "rs-mongodb-svr0", IP: "34.47.44.183"},
		{Name: "no-public", IP: "10.128.0.9"},
		{Name: "pcsm-source", IP: "35.1.1.1"},
	}
	applyHostAddresses(hosts, "gcp", addrs)

	checks := []struct{ private, public string }{
		{"10.128.0.5", "34.47.44.183"},
		{"10.128.0.9", "—"},
		{"—", "35.1.1.1"},
	}
	for i, c := range checks {
		if hosts[i].PrivateIP != c.private || hosts[i].PublicIP != c.public {
			t.Errorf("%s: got private=%q public=%q, want %q/%q",
				hosts[i].Name, hosts[i].PrivateIP, hosts[i].PublicIP, c.private, c.public)
		}
	}

	// Platforms with a single address per host list it as the private IP.
	chaos := []HostInfo{{Name: "chaos-svr0", IP: "10.30.1.2"}}
	applyHostAddresses(chaos, "chaos", nil)
	if chaos[0].PrivateIP != "10.30.1.2" || chaos[0].PublicIP != "—" {
		t.Errorf("chaos: got private=%q public=%q", chaos[0].PrivateIP, chaos[0].PublicIP)
	}
}
