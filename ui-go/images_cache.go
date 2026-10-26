package main

// Machine image cache for the configure page.
//
// Listing images runs several cloud CLI calls (7 for AWS, 6 for GCP), which
// takes seconds. Images change rarely, so results are kept for a long time,
// refreshed in the background (stale-while-revalidate), and preloaded at
// startup for the default region and every region used by a saved
// environment. The configure page embeds whatever is cached, so the image
// selector is usually filled without any request.

import (
	"log/slog"
	"sort"
	"sync"
	"time"
)

const (
	// Cached images are served as-is for this long...
	imageCacheFresh = 1 * time.Hour
	// ...and then still served while a background refresh runs, up to this age.
	imageCacheMaxAge = 24 * time.Hour
	// How often the background loop re-warms the preloaded regions.
	imagePrefetchInterval = 50 * time.Minute
)

// Default region per platform; also used by the configure page.
var defaultCloudRegion = map[string]string{
	"aws":   "us-east-1",
	"gcp":   "northamerica-northeast1",
	"azure": "eastus",
}

// Platforms whose images are listed in the configure page.
var imagePlatforms = []string{"aws", "gcp"}

type imageCacheEntry struct {
	images map[string][]CloudImage
	ts     time.Time
}

var (
	imageCacheMu   sync.Mutex
	imageCache     = map[string]imageCacheEntry{}
	imageInflight  = map[string]*sync.WaitGroup{}
	imageFetchFunc = fetchCloudImages // replaced in tests
)

// imageCacheKey: GCP images are global, AWS AMIs are per region.
func imageCacheKey(platform, region string) string {
	if platform == "gcp" {
		return "gcp"
	}
	return platform + ":" + region
}

func fetchCloudImages(platform, region string) (map[string][]CloudImage, error) {
	switch platform {
	case "aws":
		return getAWSImages(region)
	case "gcp":
		return getGCPImages(region)
	case "azure":
		return getAzureImages(region)
	}
	return map[string][]CloudImage{}, nil
}

// loadImagesNow fetches images, sharing one CLI run between concurrent callers.
// Empty results (missing credentials, CLI errors) are not cached, so images
// appear as soon as credentials are configured.
func loadImagesNow(platform, region string) map[string][]CloudImage {
	key := imageCacheKey(platform, region)
	imageCacheMu.Lock()
	if wg, ok := imageInflight[key]; ok {
		imageCacheMu.Unlock()
		wg.Wait()
		imageCacheMu.Lock()
		defer imageCacheMu.Unlock()
		return imageCache[key].images
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	imageInflight[key] = wg
	imageCacheMu.Unlock()

	images, err := imageFetchFunc(platform, region)
	if err != nil {
		slog.Warn("cloud images fetch failed", "platform", platform, "region", region, "err", err)
	}

	imageCacheMu.Lock()
	defer imageCacheMu.Unlock()
	if len(images) > 0 {
		imageCache[key] = imageCacheEntry{images: images, ts: time.Now()}
	} else if prev, ok := imageCache[key]; ok {
		images = prev.images // keep serving the last good list
	}
	delete(imageInflight, key)
	wg.Done()
	if images == nil {
		images = map[string][]CloudImage{}
	}
	return images
}

// getCloudImages returns images for a region. Cached results are returned
// immediately; stale ones trigger a background refresh. refresh forces a new
// listing (the "Refresh" link in the UI).
func getCloudImages(platform, region string, refresh bool) map[string][]CloudImage {
	if platform != "aws" && platform != "gcp" && platform != "azure" {
		return map[string][]CloudImage{}
	}
	if !refresh {
		if images, ok := cachedCloudImages(platform, region); ok {
			return images
		}
	}
	return loadImagesNow(platform, region)
}

// cachedCloudImages never runs a CLI command. Entries older than
// imageCacheFresh are returned and refreshed in the background.
func cachedCloudImages(platform, region string) (map[string][]CloudImage, bool) {
	key := imageCacheKey(platform, region)
	imageCacheMu.Lock()
	entry, ok := imageCache[key]
	_, inflight := imageInflight[key]
	imageCacheMu.Unlock()
	if !ok {
		return nil, false
	}
	age := time.Since(entry.ts)
	if age > imageCacheMaxAge {
		return nil, false
	}
	if age > imageCacheFresh && !inflight {
		go loadImagesNow(platform, region)
	}
	return entry.images, true
}

// preloadedImages returns every cached image list for a platform, keyed by
// region, for embedding in the configure page.
func preloadedImages(platform string) map[string]map[string][]CloudImage {
	out := map[string]map[string][]CloudImage{}
	for _, region := range imagePrefetchRegions(platform) {
		if images, ok := cachedCloudImages(platform, region); ok {
			out[region] = images
		}
	}
	return out
}

// imagePrefetchRegions: the default region plus every region used by a saved
// (non-deleted) environment of the platform.
func imagePrefetchRegions(platform string) []string {
	set := map[string]bool{}
	if r := defaultCloudRegion[platform]; r != "" {
		set[r] = true
	}
	if state, err := loadState(); err == nil {
		for _, env := range state {
			if env == nil || env.Platform != platform || env.Status == "deleted" {
				continue
			}
			if r := env.Config.Region; r != "" {
				set[r] = true
			}
		}
	}
	regions := make([]string, 0, len(set))
	for r := range set {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	return regions
}

// prefetchCloudImages warms the cache for one platform. Platforms without
// configured credentials are skipped.
func prefetchCloudImages(platform string) {
	if _, err := configuredProviderAuthEnv(platform); err != nil {
		return
	}
	regions := imagePrefetchRegions(platform)
	if platform == "gcp" && len(regions) > 1 {
		regions = regions[:1] // GCP images are global
	}
	var wg sync.WaitGroup
	for _, region := range regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			images := loadImagesNow(platform, region)
			slog.Info("preloaded machine images", "platform", platform, "region", region, "groups", len(images))
		}(region)
	}
	wg.Wait()
}

// startImagePrefetch preloads images at startup and keeps them warm.
func startImagePrefetch() {
	go func() {
		for {
			for _, p := range imagePlatforms {
				prefetchCloudImages(p)
			}
			time.Sleep(imagePrefetchInterval)
		}
	}()
}
