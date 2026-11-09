package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func getDockerHubTags(namespace, repo string, limit int) []string {
	tags := getDockerHubTagsMatching(namespace, repo, "", limit)
	if repo != "pmm-server" {
		return tags
	}
	// Timestamped nightly builds can fill the recent-tags page. Fetch versioned
	// tags separately so releases and RCs remain available in every namespace.
	// A dot matches version tags without matching the timestamp-only builds.
	versioned := getDockerHubTagsMatching(namespace, repo, ".", 1000)
	merged := append([]string{}, tags...)
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		seen[tag] = true
	}
	for _, tag := range versioned {
		if !seen[tag] {
			merged = append(merged, tag)
			seen[tag] = true
		}
	}
	return merged
}

func getDockerHubTagsMatching(namespace, repo, name string, limit int) []string {
	key := fmt.Sprintf("dh:%s/%s:limit=%d:name=%s", namespace, repo, limit, name)
	if v, ok := cacheGet(key); ok {
		return v.([]string)
	}
	pageSize := limit
	if pageSize > 100 {
		pageSize = 100
	}
	if pageSize <= 0 {
		return []string{}
	}
	query := url.Values{"page_size": {strconv.Itoa(pageSize)}, "ordering": {"last_updated"}}
	if name != "" {
		query.Set("name", name)
	}
	next := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/%s/tags/?%s", namespace, repo, query.Encode())
	client := &http.Client{Timeout: 8 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tags := make([]string, 0)
	// Bound both the result size and page count, including malformed empty pages.
	for page := 0; next != "" && len(tags) < limit && page < (limit+pageSize-1)/pageSize; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return tags
		}
		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("docker hub tags fetch failed", "repo", namespace+"/"+repo, "err", err)
			return tags
		}
		var result struct {
			Next    string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			slog.Warn("docker hub tags fetch failed", "repo", namespace+"/"+repo, "status", resp.StatusCode)
			return tags
		}
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			slog.Warn("docker hub tags decode failed", "repo", namespace+"/"+repo, "err", err)
			return tags
		}
		for _, r := range result.Results {
			if len(tags) == limit {
				break
			}
			tags = append(tags, r.Name)
		}
		next = result.Next
	}
	slog.Info("fetched docker hub tags", "repo", namespace+"/"+repo, "count", len(tags))
	cacheSet(key, tags)
	return tags
}

// fetchPerconaRepoPage fetches the Percona repository listing page and returns
// its body text. The result is cached to avoid redundant HTTP requests.
func fetchPerconaRepoPage() string {
	const key = "percona_repo_page"
	if v, ok := cacheGet(key); ok {
		return v.(string)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://repo.percona.com/")
	if err != nil {
		slog.Warn("percona repo page fetch failed", "err", err)
		cacheSet(key, "")
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		slog.Warn("percona repo page non-200", "status", resp.StatusCode)
		cacheSet(key, "")
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("percona repo page read failed", "err", err)
		cacheSet(key, "")
		return ""
	}
	page := string(body)
	cacheSet(key, page)
	return page
}

func getPSMDBVersions() []string {
	const key = "psmdb_versions"
	if v, ok := cacheGet(key); ok {
		return v.([]string)
	}
	var versions []string
	page := fetchPerconaRepoPage()
	if page != "" {
		re := regexp.MustCompile(`psmdb-\d+`)
		found := re.FindAllString(page, -1)
		seen := map[string]bool{}
		for _, v := range found {
			if !seen[v] {
				seen[v] = true
				versions = append(versions, v)
			}
		}
		sort.Slice(versions, func(i, j int) bool {
			imajor, iminor, _, _, _ := parseMongoVersion(versions[i])
			jmajor, jminor, _, _, _ := parseMongoVersion(versions[j])
			return imajor > jmajor || (imajor == jmajor && iminor > jminor)
		})
		slog.Info("fetched PSMDB versions", "count", len(versions))
	} else {
		slog.Warn("psmdb versions fetch failed – using defaults")
	}
	if len(versions) == 0 {
		versions = defaultPSMDBVersions
	}
	cacheSet(key, versions)
	return versions
}

// getPBMVersions returns a flat, sorted-descending list of all available
// percona-backup-mongodb package versions from the Percona "pbm" APT repository.
// PBM uses a single repository (no per-major-version repos), so there is no
// "release" concept for PBM — all versions live together in one place.
func getPBMVersions() []string {
	const key = "pbm_all_versions"
	if v, ok := cacheGet(key); ok {
		return v.([]string)
	}
	versions := fetchPerconaAPTPackageVersions("pbm", "percona-backup-mongodb")
	if len(versions) == 0 {
		slog.Warn("PBM versions fetch failed – using Docker Hub fallback")
		versions = pbmVersionsFromDockerHub()
	}
	cacheSet(key, versions)
	return versions
}

// pbmVersionsFromDockerHub derives a flat sorted-descending list of PBM versions
// from Docker Hub tags, used when the Percona APT repository is unreachable.
func pbmVersionsFromDockerHub() []string {
	tags := getDockerHubTags("percona", "percona-backup-mongodb", 100)
	re := regexp.MustCompile(`^(\d+\.\d+\.\d+)$`)
	seen := map[string]bool{}
	var versions []string
	for _, tag := range tags {
		if re.MatchString(tag) && !seen[tag] {
			seen[tag] = true
			versions = append(versions, tag)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return semverGreater(versions[i], versions[j]) })
	return versions
}

func getPMMServerImages() []string {
	tags := getDockerHubTags("percona", "pmm-server", 30)
	if len(tags) == 0 {
		return defaultPMMServerImages
	}
	return tags
}

// preferredPSMDBImage selects a GA version tag, excluding aliases and prereleases.
func preferredPSMDBImage(tags []string) string {
	stable := regexp.MustCompile(`^\d+\.\d+\.\d+(-\d+)?$`)
	best := ""
	for _, tag := range tags {
		if stable.MatchString(tag) && (best == "" || semverGreater(strings.ReplaceAll(tag, "-", "."), strings.ReplaceAll(best, "-", "."))) {
			best = tag
		}
	}
	if best == "" {
		return "latest"
	}
	return best
}

func getPSMDBImages() []string {
	tags := getDockerHubTags("percona", "percona-server-mongodb", 30)
	if len(tags) == 0 {
		return defaultPSMDBImages
	}
	return tags
}

func getPBMImages() []string {
	tags := getDockerHubTags("percona", "percona-backup-mongodb", 20)
	if len(tags) == 0 {
		return defaultPBMImages
	}
	return tags
}

func getPMMClientImages() []string {
	tags := getDockerHubTags("percona", "pmm-client", 20)
	if len(tags) == 0 {
		return defaultPMMClientImages
	}
	return tags
}

func getMongotImages() []string {
	return defaultMongotImages
}

func filterMongotTags(tags []string) []string {
	re := regexp.MustCompile(`^(latest|\d+\.\d+\.\d+(?:-\d+)?)$`)
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if re.MatchString(tag) {
			filtered = append(filtered, tag)
		}
	}
	if len(filtered) == 0 {
		return defaultMongotImages
	}
	return filtered
}

// semverGreater returns true if version string a is semantically greater than b.
// Versions are dot-separated numeric components (e.g., "7.0.12" vs "7.0.9").
// Lexicographic comparison fails for multi-digit patch numbers ("7.0.9" > "7.0.10"
// lexicographically but "7.0.10" is newer semantically).
func semverGreater(a, b string) bool {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(ap) {
			ai, _ = strconv.Atoi(ap[i])
		}
		if i < len(bp) {
			bi, _ = strconv.Atoi(bp[i])
		}
		if ai != bi {
			return ai > bi
		}
	}
	return false
}

// fetchPerconaAPTPackageVersions queries the Percona APT repository Packages index
// for repoName (e.g., "psmdb-70", "pbm") and returns a sorted-descending list of
// upstream version strings (e.g., ["7.0.12", "7.0.11"]) for the given packageName.
// It tries the gzip-compressed index first, then the plain-text index.
// The distro "jammy" (Ubuntu 22.04 LTS) is used because Percona supports it well
// and its version strings match those in other distributions.
func fetchPerconaAPTPackageVersions(repoName, packageName string) []string {
	return fetchPerconaAPTPackageVersionsFor(repoName, "main", "jammy", packageName)
}

func fetchPerconaAPTPackageVersionsFor(repoName, component, distro, packageName string) []string {
	if component == "" {
		component = "main"
	}
	if distro == "" {
		distro = "jammy"
	}
	cacheKey := "percona_apt:" + repoName + ":" + component + ":" + distro + ":" + packageName
	if v, ok := cacheGet(cacheKey); ok {
		return v.([]string)
	}

	baseURL := "https://repo.percona.com/" + repoName + "/apt/dists/" + distro + "/" + component + "/binary-amd64/"
	versions := readAPTPackages(baseURL+"Packages.gz", true, packageName)
	if len(versions) == 0 {
		versions = readAPTPackages(baseURL+"Packages", false, packageName)
	}

	slog.Info("fetched Percona APT package versions", "repo", repoName, "component", component, "distro", distro, "package", packageName, "count", len(versions))
	cacheSet(cacheKey, versions)
	return versions
}

func fetchPerconaYUMPackageVersions(repoName, channel, osMajor, packageName string) []string {
	if channel == "" {
		channel = "release"
	}
	if osMajor == "" {
		osMajor = "9"
	}
	cacheKey := "percona_yum:" + repoName + ":" + channel + ":" + osMajor + ":" + packageName
	if v, ok := cacheGet(cacheKey); ok {
		return v.([]string)
	}

	url := "https://repo.percona.com/" + repoName + "/yum/" + channel + "/" + osMajor + "/RPMS/x86_64/"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		slog.Warn("YUM repository listing fetch failed", "url", url, "err", err)
		cacheSet(cacheKey, []string{})
		return []string{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		cacheSet(cacheKey, []string{})
		return []string{}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("YUM repository listing read failed", "url", url, "err", err)
		cacheSet(cacheKey, []string{})
		return []string{}
	}
	versions := parseYUMPackageVersions(string(body), packageName)
	slog.Info("fetched Percona YUM package versions", "repo", repoName, "channel", channel, "os", osMajor, "package", packageName, "count", len(versions))
	cacheSet(cacheKey, versions)
	return versions
}

// readAPTPackages fetches a Debian Packages (or Packages.gz) file and returns
// the sorted-descending upstream version list for the named package.
func readAPTPackages(url string, gzipped bool, packageName string) []string {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		slog.Warn("APT Packages fetch failed", "url", url, "err", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}

	var reader io.Reader = resp.Body
	if gzipped {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			slog.Warn("APT Packages gzip open failed", "url", url, "err", err)
			return nil
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		slog.Warn("APT Packages read failed", "url", url, "err", err)
		return nil
	}

	return parseAPTPackageVersions(string(body), packageName)
}

// parseAPTPackageVersions parses a Debian Packages file and extracts unique
// upstream version strings for the given package name.
// A typical version field is "7.0.12-3.jammy"; this function strips the
// Debian revision suffix ("-3.jammy") and returns "7.0.12".
func parseAPTPackageVersions(content, packageName string) []string {
	seen := map[string]bool{}
	var versions []string

	var currentPackage string
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "Package: "):
			currentPackage = strings.TrimPrefix(line, "Package: ")
		case strings.HasPrefix(line, "Version: ") && currentPackage == packageName:
			fullVer := strings.TrimPrefix(line, "Version: ")
			// Strip Debian revision suffix: "7.0.12-3.jammy" → "7.0.12"
			upstream := strings.SplitN(fullVer, "-", 2)[0]
			if upstream != "" && !seen[upstream] {
				seen[upstream] = true
				versions = append(versions, upstream)
			}
		}
	}

	sort.Slice(versions, func(i, j int) bool { return semverGreater(versions[i], versions[j]) })
	return versions
}

func parseYUMPackageVersions(content, packageName string) []string {
	re := regexp.MustCompile(regexp.QuoteMeta(packageName) + `-([0-9]+\.[0-9]+\.[0-9]+)-[^"<>\s]*\.(?:el|rhel)[0-9]`)
	seen := map[string]bool{}
	var versions []string
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		versions = append(versions, m[1])
	}
	sort.Slice(versions, func(i, j int) bool { return semverGreater(versions[i], versions[j]) })
	return versions
}

type packagePlatform struct {
	Family  string
	Distro  string
	OSMajor string
}

func packagePlatformForOSImage(osImage string) packagePlatform {
	switch strings.TrimSpace(osImage) {
	case "Ubuntu 24.04":
		return packagePlatform{Family: "apt", Distro: "noble"}
	case "Debian 12":
		return packagePlatform{Family: "apt", Distro: "bookworm"}
	case "Rocky Linux 8", "Oracle Linux 8":
		return packagePlatform{Family: "yum", OSMajor: "8"}
	case "Rocky Linux 9", "CentOS 9", "Oracle Linux 9":
		return packagePlatform{Family: "yum", OSMajor: "9"}
	case "Ubuntu 22.04", "":
		fallthrough
	default:
		return packagePlatform{Family: "apt", Distro: "jammy"}
	}
}

func repoComponentForChannel(channel string) string {
	switch strings.TrimSpace(channel) {
	case "testing", "experimental":
		return channel
	default:
		return "main"
	}
}

func normalizedRepoChannel(channel string) string {
	switch strings.TrimSpace(channel) {
	case "testing", "experimental":
		return strings.TrimSpace(channel)
	default:
		return "release"
	}
}

func perconaPackageVersions(repoName, packageName, channel, osImage string) []string {
	platform := packagePlatformForOSImage(osImage)
	channel = normalizedRepoChannel(channel)
	if platform.Family == "yum" {
		return fetchPerconaYUMPackageVersions(repoName, channel, platform.OSMajor, packageName)
	}
	return fetchPerconaAPTPackageVersionsFor(repoName, repoComponentForChannel(channel), platform.Distro, packageName)
}

func officialMongoDBRepoHost(distribution string) string {
	if distribution == "enterprise" {
		return "repo.mongodb.com"
	}
	return "repo.mongodb.org"
}

func officialMongoDBRepoProduct(distribution string) string {
	if distribution == "enterprise" {
		return "mongodb-enterprise"
	}
	return "mongodb-org"
}

func officialMongoDBServerPackage(distribution string) string {
	if distribution == "enterprise" {
		return "mongodb-enterprise-server"
	}
	return "mongodb-org-server"
}

func officialAPTComponent(osImage string) string {
	if strings.HasPrefix(strings.TrimSpace(osImage), "Debian") {
		return "main"
	}
	return "multiverse"
}

func officialMongoDBAPTPackageVersions(distribution, major, distro, component, packageName string) []string {
	cacheKey := "mongodb_apt:" + distribution + ":" + major + ":" + component + ":" + distro + ":" + packageName
	if v, ok := cacheGet(cacheKey); ok {
		return v.([]string)
	}
	host := officialMongoDBRepoHost(distribution)
	product := officialMongoDBRepoProduct(distribution)
	baseURL := "https://" + host + "/apt/" + func() string {
		if distro == "bookworm" {
			return "debian"
		}
		return "ubuntu"
	}() + "/dists/" + distro + "/" + product + "/" + major + "/" + component + "/binary-amd64/"
	versions := readAPTPackages(baseURL+"Packages.gz", true, packageName)
	if len(versions) == 0 {
		versions = readAPTPackages(baseURL+"Packages", false, packageName)
	}
	slog.Info("fetched official MongoDB APT package versions", "distribution", distribution, "major", major, "distro", distro, "component", component, "package", packageName, "count", len(versions))
	cacheSet(cacheKey, versions)
	return versions
}

func officialMongoDBYUMPackageVersions(distribution, major, osMajor, packageName string) []string {
	cacheKey := "mongodb_yum_s3:" + distribution + ":" + major + ":" + osMajor + ":" + packageName
	if v, ok := cacheGet(cacheKey); ok {
		return v.([]string)
	}
	host := officialMongoDBRepoHost(distribution)
	product := officialMongoDBRepoProduct(distribution)
	prefix := "yum/redhat/" + osMajor + "/" + product + "/" + major + "/x86_64/RPMS/"
	url := "https://s3.amazonaws.com/" + host + "?list-type=2&prefix=" + prefix + "&delimiter=/"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		slog.Warn("official MongoDB YUM S3 listing fetch failed", "url", url, "err", err)
		cacheSet(cacheKey, []string{})
		return []string{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		cacheSet(cacheKey, []string{})
		return []string{}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("official MongoDB YUM S3 listing read failed", "url", url, "err", err)
		cacheSet(cacheKey, []string{})
		return []string{}
	}
	versions := parseYUMPackageVersions(string(body), packageName)
	slog.Info("fetched official MongoDB YUM package versions", "distribution", distribution, "major", major, "os", osMajor, "package", packageName, "count", len(versions))
	cacheSet(cacheKey, versions)
	return versions
}

func officialMongoDBPackageVersions(distribution, major, osImage string) []string {
	platform := packagePlatformForOSImage(osImage)
	packageName := officialMongoDBServerPackage(distribution)
	if platform.Family == "yum" {
		return officialMongoDBYUMPackageVersions(distribution, major, platform.OSMajor, packageName)
	}
	return officialMongoDBAPTPackageVersions(distribution, major, platform.Distro, officialAPTComponent(osImage), packageName)
}

func getOfficialMongoDBMinorVersionsByMajorFor(distribution, osImage string) map[string][]string {
	if distribution != "enterprise" {
		distribution = "community"
	}
	result := map[string][]string{}
	for _, major := range defaultMongoDBOfficialVersions {
		versions := officialMongoDBPackageVersions(distribution, major, osImage)
		if len(versions) > 0 {
			result[major] = versions
		}
	}
	return result
}

func getOfficialMongoDBVersionsFor(distribution, osImage string) []string {
	minorVersions := getOfficialMongoDBMinorVersionsByMajorFor(distribution, osImage)
	versions := make([]string, 0, len(minorVersions))
	for _, major := range defaultMongoDBOfficialVersions {
		if len(minorVersions[major]) > 0 {
			versions = append(versions, major)
		}
	}
	return versions
}

func getPSMDBMinorVersionsByMajorFor(channel, osImage string) map[string][]string {
	result := map[string][]string{}
	for _, release := range getPSMDBVersions() {
		versions := perconaPackageVersions(release, "percona-server-mongodb", channel, osImage)
		if len(versions) > 0 {
			result[release] = versions
		}
	}
	if len(result) == 0 && normalizedRepoChannel(channel) == "release" && strings.TrimSpace(osImage) == "" {
		return getPSMDBMinorVersionsByMajor()
	}
	return result
}

func getPSMDBVersionsFor(channel, osImage string) []string {
	minorVersions := getPSMDBMinorVersionsByMajorFor(channel, osImage)
	versions := make([]string, 0, len(minorVersions))
	for _, release := range getPSMDBVersions() {
		if len(minorVersions[release]) > 0 {
			versions = append(versions, release)
		}
	}
	if len(versions) == 0 && normalizedRepoChannel(channel) == "release" && strings.TrimSpace(osImage) == "" {
		return availablePSMDBReleases(getPSMDBVersions(), minorVersions)
	}
	return versions
}

func getPBMVersionsFor(channel, osImage string) []string {
	versions := perconaPackageVersions("pbm", "percona-backup-mongodb", channel, osImage)
	if len(versions) == 0 && normalizedRepoChannel(channel) == "release" && strings.TrimSpace(osImage) == "" {
		return getPBMVersions()
	}
	return versions
}

func getPMMClientVersionsFor(channel, osImage string) []string {
	versions := perconaPackageVersions("pmm3-client", "pmm-client", channel, osImage)
	// Docker release tags do not establish package availability in testing,
	// experimental, or a particular OS repository.
	if len(versions) == 0 && normalizedRepoChannel(channel) == "release" && strings.TrimSpace(osImage) == "" {
		versions = getPMMClientImages()
	}
	return versions
}

func getPerconaSearchVersionsFor(channel, osImage string) []string {
	return perconaPackageVersions("ps4m", "percona-search-mongodb", channel, osImage)
}

func getPCSMVersions() []string {
	const key = "pcsm_all_versions"
	if v, ok := cacheGet(key); ok {
		return v.([]string)
	}
	versions := getPCSMVersionsFor("release", "")
	if len(versions) == 0 {
		versions = defaultPCSMVersions
	}
	cacheSet(key, versions)
	return versions
}

func getPCSMVersionsFor(channel, osImage string) []string {
	versions := perconaPackageVersions("pcsm", "percona-clustersync-mongodb", channel, osImage)
	if len(versions) == 0 {
		return defaultPCSMVersions
	}
	return versions
}

// getPSMDBMinorVersionsByMajor returns a map from major release key (e.g. "psmdb-70")
// to a sorted-descending list of specific minor versions (e.g. ["7.0.12", "7.0.11"]).
// Versions are pulled from the Percona APT repository index for each major release.
// Falls back to Docker Hub image tags if the APT repo is unreachable.
func getPSMDBMinorVersionsByMajor() map[string][]string {
	const key = "psmdb_minor_by_major"
	if v, ok := cacheGet(key); ok {
		return v.(map[string][]string)
	}

	result := map[string][]string{}
	for _, release := range getPSMDBVersions() {
		versions := fetchPerconaAPTPackageVersions(release, "percona-server-mongodb")
		if len(versions) > 0 {
			result[release] = versions
		}
	}

	// Fall back to Docker Hub tags when the Percona repo is unreachable.
	if len(result) == 0 {
		result = psmdbMinorVersionsFromDockerHub()
	}

	cacheSet(key, result)
	return result
}

// psmdbMinorVersionsFromDockerHub derives PSMDB minor versions from Docker Hub image tags.
// Used as a fallback when the Percona APT repository is unreachable.
func psmdbMinorVersionsFromDockerHub() map[string][]string {
	result := map[string][]string{}
	tags := getDockerHubTags("percona", "percona-server-mongodb", 200)
	re := regexp.MustCompile(`^(\d+\.\d+)\.(\d+)`)
	seen := map[string]map[string]bool{}
	for _, tag := range tags {
		m := re.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		majorMinor := m[1]
		version := majorMinor + "." + m[2]
		parts := strings.SplitN(majorMinor, ".", 2)
		if len(parts) != 2 {
			continue
		}
		releaseKey := "psmdb-" + parts[0] + parts[1]
		if seen[releaseKey] == nil {
			seen[releaseKey] = map[string]bool{}
		}
		if !seen[releaseKey][version] {
			seen[releaseKey][version] = true
			result[releaseKey] = append(result[releaseKey], version)
		}
	}
	for k := range result {
		sort.Slice(result[k], func(i, j int) bool { return semverGreater(result[k][i], result[k][j]) })
	}
	return result
}

// prefetchVersions warms all image/version caches at startup in a goroutine.
func prefetchVersions() {
	slog.Info("prefetching container image tags and PSMDB versions…")
	getPSMDBVersions()
	getPBMVersions()
	getPMMServerImages()
	getPSMDBImages()
	getPBMImages()
	getPMMClientImages()
	getMongotImages()
	getPSMDBMinorVersionsByMajor()
	getPCSMVersions()
	slog.Info("version prefetch complete")
}

// ─── Cached-only getters ─────────────────────────────────────────────────────
// These variants return whatever is currently in cache (or safe defaults) without
// making any outbound HTTP requests. They are used to render the configure page
// immediately; the browser then refreshes the version dropdowns asynchronously
// via /api/versions once the page has loaded.

func cachedPSMDBVersions() []string {
	if v, ok := cacheGet("psmdb_versions"); ok {
		return availablePSMDBReleases(v.([]string), cachedPSMDBMinorVersionsByMajor())
	}
	return defaultPSMDBVersions
}

// New repository directories may be published before their server packages.
// Keep unverified 9.x+ candidates out of the initial UI; the OS/channel-specific
// package endpoint can expose them as soon as actual packages are available.
func availablePSMDBReleases(releases []string, packages map[string][]string) []string {
	available := make([]string, 0, len(releases))
	for _, release := range releases {
		major, _, _, _, _ := parseMongoVersion(release)
		if major < 9 || len(packages[release]) > 0 {
			available = append(available, release)
		}
	}
	return available
}

func cachedPBMVersions() []string {
	if v, ok := cacheGet("pbm_all_versions"); ok {
		return v.([]string)
	}
	return []string{}
}

func cachedPMMServerImages() []string {
	if v, ok := cacheGet("dh:percona/pmm-server"); ok {
		return v.([]string)
	}
	return defaultPMMServerImages
}

func cachedPSMDBImages() []string {
	if v, ok := cacheGet("dh:percona/percona-server-mongodb"); ok {
		return v.([]string)
	}
	return defaultPSMDBImages
}

func cachedPBMImages() []string {
	if v, ok := cacheGet("dh:percona/percona-backup-mongodb"); ok {
		return v.([]string)
	}
	return defaultPBMImages
}

func cachedPMMClientImages() []string {
	if v, ok := cacheGet("dh:percona/pmm-client"); ok {
		return v.([]string)
	}
	return defaultPMMClientImages
}

func cachedMongotImages() []string {
	if v, ok := cacheGet("dh:mongodb/mongodb-community-search"); ok {
		return filterMongotTags(v.([]string))
	}
	return defaultMongotImages
}

func cachedPSMDBMinorVersionsByMajor() map[string][]string {
	if v, ok := cacheGet("psmdb_minor_by_major"); ok {
		return v.(map[string][]string)
	}
	return map[string][]string{}
}

func cachedPCSMVersions() []string {
	if v, ok := cacheGet("pcsm_all_versions"); ok {
		return v.([]string)
	}
	return defaultPCSMVersions
}
