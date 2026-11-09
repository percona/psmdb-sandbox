package main

import "testing"

func TestPMMClientVersionsDoNotSubstituteReleaseTagsForOtherRepositories(t *testing.T) {
	for _, tc := range []struct{ channel, osImage, key string }{
		{"testing", "", "percona_apt:pmm3-client:testing:jammy:pmm-client"},
		{"experimental", "", "percona_apt:pmm3-client:experimental:jammy:pmm-client"},
		{"release", "Ubuntu 24.04", "percona_apt:pmm3-client:main:noble:pmm-client"},
		{"testing", "Rocky Linux 9", "percona_yum:pmm3-client:testing:9:pmm-client"},
	} {
		t.Run(tc.channel+tc.osImage, func(t *testing.T) {
			cacheSet(tc.key, []string{})
			dockerKey := "dh:percona/pmm-client:limit=20:name="
			cacheSet(dockerKey, []string{"3.10.0"})
			t.Cleanup(func() { cacheDelete(tc.key); cacheDelete(dockerKey) })
			if got := getPMMClientVersionsFor(tc.channel, tc.osImage); len(got) != 0 {
				t.Fatalf("unavailable packages must not be replaced with Docker release tags: %v", got)
			}
		})
	}
}
