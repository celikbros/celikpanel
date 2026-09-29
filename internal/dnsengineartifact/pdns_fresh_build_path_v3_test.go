package dnsengineartifact

import (
	"strings"
	"testing"
)

func TestPDNSFreshCandidateBuildPathsV3(t *testing.T) {
	id := strings.Repeat("ab", 16)
	candidate := "/var/lib/celikpanel-agent-private/.celikpanel-switch-" + id + ".sqlite3"
	build, sidecars, err := PDNSFreshCandidateBuildPathsV3(candidate)
	want := "/var/lib/celikpanel-agent-private/.celikpanel-switch-" + id + ".building.sqlite3"
	if err != nil || build != want || len(sidecars) != 3 ||
		sidecars[0] != want+"-journal" || sidecars[1] != want+"-wal" || sidecars[2] != want+"-shm" {
		t.Fatalf("build=%q sidecars=%v err=%v", build, sidecars, err)
	}
	for _, bad := range []string{
		"relative/.celikpanel-switch-" + id + ".sqlite3",
		"/var/lib/x/../y/.celikpanel-switch-" + id + ".sqlite3",
		"/var/lib/powerdns/pdns.sqlite3",
		"/var/lib/p/.celikpanel-switch-" + strings.Repeat("A", 32) + ".sqlite3",
		"/var/lib/p/.celikpanel-switch-" + id[:31] + ".sqlite3",
		want,
	} {
		if _, _, err := PDNSFreshCandidateBuildPathsV3(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
