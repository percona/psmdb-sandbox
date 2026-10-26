package main

import (
	"encoding/json"
	"os"
	"strings"
)

// hostAddresses holds the private (VPC) and public addresses of an instance.
type hostAddresses struct {
	Private string
	Public  string
}

// cloudPlatformsWithPublicIPs lists the platforms whose inventories address
// hosts by public IP while the instances also have a separate private IP.
var cloudPlatformsWithPublicIPs = map[string]bool{
	"aws":   true,
	"gcp":   true,
	"azure": true,
}

// readStateHostAddresses extracts the private and public IPs of every compute
// instance recorded in a Terraform state file, keyed by instance name. A
// missing or unreadable state file yields an empty map.
func readStateHostAddresses(path string) map[string]hostAddresses {
	addrs := make(map[string]hostAddresses)
	data, err := os.ReadFile(path)
	if err != nil {
		return addrs
	}
	var state struct {
		Resources []struct {
			Mode      string `json:"mode"`
			Type      string `json:"type"`
			Instances []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return addrs
	}
	for _, r := range state.Resources {
		if r.Mode != "managed" {
			continue
		}
		for _, inst := range r.Instances {
			name, a, ok := instanceAddresses(r.Type, inst.Attributes)
			if ok && name != "" {
				addrs[name] = a
			}
		}
	}
	return addrs
}

// instanceAddresses returns the name and addresses of a supported compute
// instance resource.
func instanceAddresses(resourceType string, attrs map[string]any) (string, hostAddresses, bool) {
	switch resourceType {
	case "google_compute_instance":
		var a hostAddresses
		if nics, ok := attrs["network_interface"].([]any); ok && len(nics) > 0 {
			if nic, ok := nics[0].(map[string]any); ok {
				a.Private = attrString(nic, "network_ip")
				if acs, ok := nic["access_config"].([]any); ok && len(acs) > 0 {
					if ac, ok := acs[0].(map[string]any); ok {
						a.Public = attrString(ac, "nat_ip")
					}
				}
			}
		}
		return attrString(attrs, "name"), a, true
	case "aws_instance":
		name := ""
		if tags, ok := attrs["tags"].(map[string]any); ok {
			name = attrString(tags, "Name")
		}
		return name, hostAddresses{
			Private: attrString(attrs, "private_ip"),
			Public:  attrString(attrs, "public_ip"),
		}, true
	case "azurerm_linux_virtual_machine":
		return attrString(attrs, "name"), hostAddresses{
			Private: attrString(attrs, "private_ip_address"),
			Public:  attrString(attrs, "public_ip_address"),
		}, true
	}
	return "", hostAddresses{}, false
}

func attrString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

// applyHostAddresses fills PrivateIP and PublicIP for cloud hosts. Addresses
// from the Terraform state win; otherwise the inventory address is treated as
// public on platforms with separate public IPs and as private elsewhere.
// Hosts without a distinct public IP show only the private one.
func applyHostAddresses(hosts []HostInfo, platform string, addrs map[string]hostAddresses) {
	for i := range hosts {
		h := &hosts[i]
		a, ok := addrs[h.Name]
		if !ok {
			a, ok = addrs[strings.SplitN(h.Name, ".", 2)[0]]
		}
		if !ok {
			if cloudPlatformsWithPublicIPs[platform] {
				a = hostAddresses{Public: h.IP}
			} else {
				a = hostAddresses{Private: h.IP}
			}
		}
		if a.Public == a.Private {
			a.Public = ""
		}
		h.PrivateIP = strDefault(a.Private, "—")
		h.PublicIP = strDefault(a.Public, "—")
	}
}
