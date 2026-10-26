package main

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubImageFetch replaces the CLI-backed fetcher and resets the cache.
func stubImageFetch(t *testing.T, fn func(platform, region string) (map[string][]CloudImage, error)) {
	t.Helper()
	orig := imageFetchFunc
	imageFetchFunc = fn
	imageCacheMu.Lock()
	imageCache = map[string]imageCacheEntry{}
	imageInflight = map[string]*sync.WaitGroup{}
	imageCacheMu.Unlock()
	t.Cleanup(func() {
		imageFetchFunc = orig
		imageCacheMu.Lock()
		imageCache = map[string]imageCacheEntry{}
		imageCacheMu.Unlock()
	})
}

func oneImage(id string) map[string][]CloudImage {
	return map[string][]CloudImage{"Rocky Linux 9": {{ID: id, Name: id}}}
}

func TestCloudImagesCachedAndDeduplicated(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	stubImageFetch(t, func(platform, region string) (map[string][]CloudImage, error) {
		calls.Add(1)
		<-release
		return oneImage("ami-" + region), nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); getCloudImages("aws", "us-east-1", false) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("concurrent requests ran the CLI %d times, want 1", n)
	}
	if got := getCloudImages("aws", "us-east-1", false); got["Rocky Linux 9"][0].ID != "ami-us-east-1" || calls.Load() != 1 {
		t.Fatalf("second request was not served from cache: %v (calls %d)", got, calls.Load())
	}
	getCloudImages("aws", "us-east-1", true)
	if calls.Load() != 2 {
		t.Fatal("refresh must bypass the cache")
	}
	// AMIs are per region; GCP images are global.
	getCloudImages("aws", "eu-west-1", false)
	getCloudImages("gcp", "us-central1", false)
	getCloudImages("gcp", "europe-west1", false)
	if calls.Load() != 4 {
		t.Fatalf("unexpected CLI calls: %d", calls.Load())
	}
}

func TestCloudImagesEmptyResultsNotCachedAndStaleServed(t *testing.T) {
	var result map[string][]CloudImage
	var calls atomic.Int32
	stubImageFetch(t, func(platform, region string) (map[string][]CloudImage, error) {
		calls.Add(1)
		return result, nil
	})
	getCloudImages("aws", "us-east-1", false)
	if _, ok := cachedCloudImages("aws", "us-east-1"); ok {
		t.Fatal("empty results (e.g. missing credentials) must not be cached")
	}

	result = oneImage("ami-1")
	getCloudImages("aws", "us-east-1", false)
	// A failed refresh keeps the last good list.
	result = nil
	if got := getCloudImages("aws", "us-east-1", true); len(got) == 0 {
		t.Fatal("a failed refresh dropped the cached images")
	}

	// Stale entries are served immediately and refreshed in the background.
	imageCacheMu.Lock()
	imageCache["aws:us-east-1"] = imageCacheEntry{images: oneImage("ami-old"), ts: time.Now().Add(-2 * imageCacheFresh)}
	imageCacheMu.Unlock()
	result = oneImage("ami-new")
	before := calls.Load()
	if got, ok := cachedCloudImages("aws", "us-east-1"); !ok || got["Rocky Linux 9"][0].ID != "ami-old" {
		t.Fatalf("stale entry not served: %v", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got, _ := cachedCloudImages("aws", "us-east-1"); got["Rocky Linux 9"][0].ID == "ami-new" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background refresh did not run (calls %d -> %d)", before, calls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Entries past the maximum age are not used.
	imageCacheMu.Lock()
	imageCache["aws:us-east-1"] = imageCacheEntry{images: oneImage("ami-ancient"), ts: time.Now().Add(-2 * imageCacheMaxAge)}
	imageCacheMu.Unlock()
	if _, ok := cachedCloudImages("aws", "us-east-1"); ok {
		t.Fatal("expired entry was served")
	}
}

func TestConfigureEmbedsPreloadedImagesAndDefaultRegion(t *testing.T) {
	stubImageFetch(t, func(platform, region string) (map[string][]CloudImage, error) {
		return oneImage("ami-0preloaded"), nil
	})
	getCloudImages("aws", defaultCloudRegion["aws"], false)

	page := renderConfigure(t, ConfigureData{Platform: "aws", PreloadedImages: preloadedImages("aws")})
	for _, want := range []string{
		`<option value="us-east-1" selected>us-east-1</option>`,
		`"us-east-1":{"Rocky Linux 9":[{"id":"ami-0preloaded"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("configure page missing %q", want)
		}
	}
	if strings.Contains(page, "— loading regions… —") {
		t.Error("the AWS region selector should be rendered server-side")
	}
}
