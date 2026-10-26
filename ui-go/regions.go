package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

var awsImageOwners = []struct {
	Group       string
	OwnerID     string
	NamePattern string
}{
	{"Amazon Linux 2023", "137112412989", "al2023-ami-2023*x86_64"},
	{"Amazon Linux 2", "137112412989", "amzn2-ami-hvm-*-x86_64-gp2"},
	{"Ubuntu 24.04 LTS", "099720109477", "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"},
	{"Ubuntu 22.04 LTS", "099720109477", "ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"},
	{"CentOS Stream 9", "125523088429", "CentOS Stream 9*x86_64*"},
	{"Rocky Linux 9", "792107900819", "Rocky-9-EC2*x86_64*"},
	{"Debian 12", "136693071363", "debian-12-amd64-*"},
}

func getAWSImages(region string) (map[string][]CloudImage, error) {
	result := map[string][]CloudImage{}
	env, envErr := configuredProviderAuthEnv("aws")
	if envErr != nil {
		return result, nil
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, o := range awsImageOwners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := []string{
				"ec2", "describe-images",
				"--region", region,
				"--owners", o.OwnerID,
				"--filters",
				fmt.Sprintf("Name=name,Values=%s", o.NamePattern),
				"Name=state,Values=available",
				"Name=architecture,Values=x86_64",
				"Name=is-public,Values=true",
				"--query",
				"reverse(sort_by(Images,&CreationDate))[:5].{id:ImageId,name:Name,desc:Description}",
				"--output", "json",
			}
			out, err := execOutputEnv(env, "aws", args...)
			if err != nil {
				slog.Warn("aws describe-images failed", "group", o.Group, "region", region, "err", err)
				return
			}
			var imgs []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Desc string `json:"desc"`
			}
			if err := json.Unmarshal([]byte(out), &imgs); err != nil {
				return
			}
			group := make([]CloudImage, 0, len(imgs))
			for _, img := range imgs {
				group = append(group, CloudImage{ID: img.ID, Name: img.Name, Description: img.Desc})
			}
			if len(group) > 0 {
				mu.Lock()
				result[o.Group] = group
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return result, nil
}

var gcpImageProjects = []struct {
	Group   string
	Project string
	Family  string
}{
	{"CentOS Stream 9", "centos-cloud", "centos-stream-9"},
	{"Debian 12", "debian-cloud", "debian-12"},
	{"Ubuntu 24.04 LTS", "ubuntu-os-cloud", "ubuntu-2404-lts-amd64"},
	{"Ubuntu 22.04 LTS", "ubuntu-os-cloud", "ubuntu-2204-lts"},
	{"Rocky Linux 9", "rocky-linux-cloud", "rocky-linux-9"},
	{"RHEL 9", "rhel-cloud", "rhel-9"},
}

func getGCPImages(region string) (map[string][]CloudImage, error) {
	result := map[string][]CloudImage{}
	env, envErr := configuredProviderAuthEnv("gcp")
	if envErr != nil {
		return result, nil
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range gcpImageProjects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := []string{
				"compute", "images", "list",
				"--project", p.Project,
				"--filter", fmt.Sprintf("family=%s status=READY", p.Family),
				"--sort-by", "~creationTimestamp",
				"--limit", "5",
				"--format", "json(selfLink,name,description)",
			}
			out, err := execOutputEnv(env, "gcloud", args...)
			if err != nil {
				slog.Warn("gcloud images list failed", "group", p.Group, "err", err)
				return
			}
			var imgs []struct {
				SelfLink    string `json:"selfLink"`
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal([]byte(out), &imgs); err != nil {
				return
			}
			group := make([]CloudImage, 0, len(imgs))
			for _, img := range imgs {
				group = append(group, CloudImage{ID: img.SelfLink, Name: img.Name, Description: img.Description})
			}
			if len(group) > 0 {
				mu.Lock()
				result[p.Group] = group
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return result, nil
}

var azureImagePublishers = []struct {
	Group     string
	Publisher string
	Offer     string
}{
	{"Ubuntu 24.04 LTS", "Canonical", "ubuntu-24_04-lts"},
	{"Ubuntu 22.04 LTS", "Canonical", "0001-com-ubuntu-server-jammy"},
	{"Debian 12", "Debian", "debian-12"},
	{"AlmaLinux 9", "almalinux", "almalinux-x86_64"},
	{"Rocky Linux 9", "resf", "rockylinux-x86_64"},
}

func getAzureImages(location string) (map[string][]CloudImage, error) {
	result := map[string][]CloudImage{}
	env, envErr := configuredProviderAuthEnv("azure")
	if envErr != nil {
		return result, nil
	}
	for _, pub := range azureImagePublishers {
		args := []string{
			"vm", "image", "list",
			"--location", location,
			"--publisher", pub.Publisher,
			"--offer", pub.Offer,
			"--all",
			"--query", "reverse(sort_by([?osType=='Linux'],&version))[:5].{id:urn,name:skus,desc:offer}",
			"--output", "json",
		}
		out, err := execOutputEnv(env, "az", args...)
		if err != nil {
			slog.Warn("az vm image list failed", "group", pub.Group, "location", location, "err", err)
			continue
		}
		var imgs []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Desc string `json:"desc"`
		}
		if err := json.Unmarshal([]byte(out), &imgs); err != nil {
			continue
		}
		group := make([]CloudImage, 0, len(imgs))
		for _, img := range imgs {
			group = append(group, CloudImage{ID: img.ID, Name: img.Name, Description: img.Desc})
		}
		if len(group) > 0 {
			result[pub.Group] = group
		}
	}
	return result, nil
}

// getCloudRegions queries the cloud CLI for available regions, with a static
// fallback for each platform when the CLI is not available or returns an error.
func getCloudRegions(platform string, refresh bool) []string {
	key := "regions:" + platform
	if refresh {
		cacheDelete(key)
	}
	if v, ok := cacheGet(key); ok {
		return v.([]string)
	}
	var regions []string
	switch platform {
	case "aws":
		regions = getAWSRegions()
	case "gcp":
		regions = getGCPRegions()
	case "azure":
		regions = getAzureRegions()
	}
	if len(regions) == 0 {
		regions = defaultRegions(platform)
	}
	cacheSet(key, regions)
	return regions
}

func getAWSRegions() []string {
	env, envErr := configuredProviderAuthEnv("aws")
	if envErr != nil {
		return nil
	}
	out, err := execOutputEnv(env, "aws", "ec2", "describe-regions",
		"--query", "Regions[].RegionName", "--output", "json")
	if err != nil {
		slog.Warn("aws describe-regions failed", "err", err)
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		slog.Warn("aws describe-regions parse failed", "err", err)
		return nil
	}
	sort.Strings(names)
	return names
}

func getGCPRegions() []string {
	env, envErr := configuredProviderAuthEnv("gcp")
	if envErr != nil {
		return nil
	}
	out, err := execOutputEnv(env, "gcloud", "compute", "regions", "list",
		"--format=value(name)")
	if err != nil {
		slog.Warn("gcloud regions list failed", "err", err)
		return nil
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	sort.Strings(names)
	return names
}

func getAzureRegions() []string {
	env, envErr := configuredProviderAuthEnv("azure")
	if envErr != nil {
		return nil
	}
	out, err := execOutputEnv(env, "az", "account", "list-locations",
		"--query", "[].name", "--output", "json")
	if err != nil {
		slog.Warn("az list-locations failed", "err", err)
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		slog.Warn("az list-locations parse failed", "err", err)
		return nil
	}
	sort.Strings(names)
	return names
}

// defaultRegions returns a static fallback list for each platform.
func defaultRegions(platform string) []string {
	switch platform {
	case "aws":
		return []string{
			"ap-northeast-1", "ap-northeast-2", "ap-northeast-3",
			"ap-south-1", "ap-southeast-1", "ap-southeast-2",
			"ca-central-1", "eu-central-1", "eu-north-1",
			"eu-west-1", "eu-west-2", "eu-west-3",
			"sa-east-1",
			"us-east-1", "us-east-2", "us-west-1", "us-west-2",
		}
	case "gcp":
		return []string{
			"asia-east1", "asia-east2", "asia-northeast1", "asia-northeast2",
			"asia-northeast3", "asia-south1", "asia-southeast1", "asia-southeast2",
			"australia-southeast1",
			"europe-north1", "europe-west1", "europe-west2", "europe-west3",
			"europe-west4", "europe-west6",
			"northamerica-northeast1", "northamerica-northeast2",
			"southamerica-east1",
			"us-central1", "us-east1", "us-east4", "us-west1", "us-west2",
			"us-west3", "us-west4",
		}
	case "azure":
		return []string{
			"australiaeast", "australiasoutheast",
			"brazilsouth",
			"canadacentral", "canadaeast",
			"centralindia", "centralus",
			"eastasia", "eastus", "eastus2",
			"francecentral",
			"germanywestcentral",
			"japaneast", "japanwest",
			"koreacentral",
			"northeurope", "norwayeast",
			"southafricanorth",
			"southcentralus", "southeastasia", "southindia",
			"swedencentral",
			"switzerlandnorth",
			"uaenorth",
			"uksouth", "ukwest",
			"westeurope", "westus", "westus2", "westus3",
		}
	}
	return nil
}

// groupRegionsByGeo groups a flat region list into geographic buckets so the
// UI can render them as <optgroup> elements.
func groupRegionsByGeo(platform string, regions []string) map[string][]string {
	groups := map[string][]string{}
	for _, r := range regions {
		g := regionGeoGroup(platform, r)
		groups[g] = append(groups[g], r)
	}
	return groups
}

// regionGeoGroup returns a human-readable geographic group name for a region.
func regionGeoGroup(platform, region string) string {
	switch platform {
	case "aws":
		switch {
		case strings.HasPrefix(region, "us-"):
			return "US"
		case strings.HasPrefix(region, "ca-"):
			return "Canada"
		case strings.HasPrefix(region, "eu-") || strings.HasPrefix(region, "europe-"):
			return "Europe"
		case strings.HasPrefix(region, "ap-") || strings.HasPrefix(region, "asia-") || strings.HasPrefix(region, "australia-"):
			return "Asia Pacific"
		case strings.HasPrefix(region, "sa-") || strings.HasPrefix(region, "southamerica-"):
			return "South America"
		case strings.HasPrefix(region, "me-") || strings.HasPrefix(region, "af-"):
			return "Middle East & Africa"
		case strings.HasPrefix(region, "il-"):
			return "Middle East & Africa"
		}
	case "gcp":
		switch {
		case strings.HasPrefix(region, "us-") || strings.HasPrefix(region, "northamerica-"):
			return "North America"
		case strings.HasPrefix(region, "southamerica-"):
			return "South America"
		case strings.HasPrefix(region, "europe-"):
			return "Europe"
		case strings.HasPrefix(region, "asia-") || strings.HasPrefix(region, "australia-"):
			return "Asia Pacific"
		case strings.HasPrefix(region, "me-") || strings.HasPrefix(region, "africa-"):
			return "Middle East & Africa"
		}
	case "azure":
		if strings.HasPrefix(region, "australia") {
			return "Asia Pacific"
		}
		switch {
		case strings.Contains(region, "us") || strings.Contains(region, "canada"):
			return "North America"
		case strings.Contains(region, "brazil"):
			return "South America"
		case strings.Contains(region, "europe") || strings.Contains(region, "uk") || strings.Contains(region, "france") ||
			strings.Contains(region, "germany") || strings.Contains(region, "norway") || strings.Contains(region, "switzerland") || strings.Contains(region, "sweden"):
			return "Europe"
		case strings.Contains(region, "asia") || strings.Contains(region, "japan") || strings.Contains(region, "korea") ||
			strings.Contains(region, "india") || strings.Contains(region, "china"):
			return "Asia Pacific"
		case strings.Contains(region, "africa") || strings.Contains(region, "uae"):
			return "Middle East & Africa"
		}
	}
	return "Other"
}
