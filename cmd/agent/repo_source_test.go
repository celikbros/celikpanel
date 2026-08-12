package main

import (
	"strings"
	"testing"
)

func TestRenderNetdataSourceUsesExactDistroTree(t *testing.T) {
	repo := repoFromCatalogue("netdata")
	if repo == nil {
		t.Fatal("netdata repository is missing from the catalogue")
	}
	tests := []struct {
		distro, codename, want string
	}{
		{
			distro:   "debian",
			codename: "trixie",
			want:     "deb https://repository.netdata.cloud/repos/stable/debian/ trixie/",
		},
		{
			distro:   "ubuntu",
			codename: "noble",
			want:     "deb https://repository.netdata.cloud/repos/stable/ubuntu/ noble/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.distro, func(t *testing.T) {
			got, err := renderRepoSource(repo, tt.distro, tt.codename)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("source = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderNetdataSourceRejectsUnknownAptDerivative(t *testing.T) {
	repo := repoFromCatalogue("netdata")
	_, err := renderRepoSource(repo, "linuxmint", "wilma")
	if err == nil || !strings.Contains(err.Error(), "not offered on distribution linuxmint") {
		t.Fatalf("error = %v, want explicit unsupported-distribution error", err)
	}
}

func TestRenderSurySourceUsesSupportedDebianAndUbuntuSuites(t *testing.T) {
	repo := repoFromCatalogue("sury")
	if repo == nil {
		t.Fatal("Sury repository is missing from the catalogue")
	}
	tests := []struct {
		distro, codename, want string
	}{
		{distro: "debian", codename: "bookworm", want: "deb https://packages.sury.org/php/ bookworm main"},
		{distro: "ubuntu", codename: "noble", want: "deb https://packages.sury.org/php/ noble main"},
	}
	for _, tt := range tests {
		t.Run(tt.distro, func(t *testing.T) {
			got, err := renderRepoSource(repo, tt.distro, tt.codename)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("source = %q, want %q", got, tt.want)
			}
		})
	}
	if _, err := renderRepoSource(repo, "linuxmint", "wilma"); err == nil || !strings.Contains(err.Error(), "not offered on distribution linuxmint") {
		t.Fatalf("derivative error = %v, want explicit unsupported-distribution error", err)
	}
}

func TestRepoRecipeStatusRejectsStaleNetdataHostname(t *testing.T) {
	repo := repoFromCatalogue("netdata")
	expected, err := renderRepoSource(repo, "debian", "trixie")
	if err != nil {
		t.Fatal(err)
	}
	staleBase := "deb https://repo.netdata.cloud/repos/stable/debian/ trixie/"
	stale := signedRepoSource(staleBase, repoKeyringPath("netdata", false))
	healthy, reason := repoRecipeStatus(repo, expected, stale, func(string) ([]byte, error) {
		return []byte{0xc6}, nil
	})
	if healthy || !strings.Contains(reason, "source line") {
		t.Fatalf("healthy = %v, reason = %q; want stale source drift", healthy, reason)
	}
}

func TestRepoRecipeStatusAcceptsCurrentSourceAndKeyring(t *testing.T) {
	repo := repoFromCatalogue("netdata")
	expected, err := renderRepoSource(repo, "debian", "trixie")
	if err != nil {
		t.Fatal(err)
	}
	key, fingerprint := testRepoPublicKey(t, false, 0)
	testRepo := *repo
	testRepo.KeyFingerprint = fingerprint
	keyring := repoKeyringPath("netdata", false)
	actual := signedRepoSource(expected, keyring)
	healthy, reason := repoRecipeStatus(&testRepo, expected, actual, func(path string) ([]byte, error) {
		if path != keyring {
			t.Fatalf("read keyring %q, want %q", path, keyring)
		}
		return key, nil
	})
	if !healthy || reason != "" {
		t.Fatalf("healthy = %v, reason = %q", healthy, reason)
	}
}

func TestCatalogueRepoPackagePatternRejectsReposWithoutVersionMenu(t *testing.T) {
	if _, _, err := catalogueRepoPackagePattern("netdata"); err == nil {
		t.Fatal("Netdata produced a package enumeration pattern")
	}
	pattern, matcher, err := catalogueRepoPackagePattern("pgdg")
	if err != nil {
		t.Fatalf("PGDG package pattern: %v", err)
	}
	if pattern == "" || matcher.FindString("postgresql-17") != "postgresql-17" {
		t.Fatalf("PGDG pattern = %q does not match postgresql-17", pattern)
	}
}

func TestRenderRepoSourceRejectsOSReleaseInjection(t *testing.T) {
	repo := repoFromCatalogue("netdata")
	for _, codename := range []string{"noble\nmalicious", "noble main", "{codename}"} {
		if _, err := renderRepoSource(repo, "ubuntu", codename); err == nil {
			t.Fatalf("codename %q was accepted", codename)
		}
	}
}

func TestParseOSReleaseValue(t *testing.T) {
	data := []byte("NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_CODENAME='noble'\n")
	if got := parseOSReleaseValue(data, "ID"); got != "ubuntu" {
		t.Fatalf("ID = %q, want ubuntu", got)
	}
	if got := parseOSReleaseValue(data, "VERSION_CODENAME"); got != "noble" {
		t.Fatalf("VERSION_CODENAME = %q, want noble", got)
	}
}

func TestParseOSReleaseValueFailsClosedOnDuplicateOrMalformedData(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("ID=debian\nID=ubuntu\nVERSION_CODENAME=noble\n"),
		[]byte("ID=ubuntu;echo-owned\nVERSION_CODENAME=noble\n"),
	} {
		if got := parseOSReleaseValue(data, "ID"); got != "" {
			t.Fatalf("malformed os-release produced ID %q", got)
		}
	}
}
