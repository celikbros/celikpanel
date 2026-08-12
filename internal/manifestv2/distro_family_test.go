package manifestv2

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDistroFamilySchemaContract(t *testing.T) {
	if AgentSchemaVersion != 2 {
		t.Fatalf("AgentSchemaVersion = %d, want 2 for distro_family", AgentSchemaVersion)
	}

	encoded, err := json.Marshal(PlatformSelector{
		OSFamily:     "linux",
		DistroFamily: "debian",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); got != `{"os_family":"linux","distro_family":"debian"}` {
		t.Fatalf("selector JSON = %s", got)
	}

	var selector PlatformSelector
	if err := strictJSON(encoded, &selector); err != nil {
		t.Fatalf("strictJSON valid distro_family: %v", err)
	}
	if selector.DistroFamily != "debian" {
		t.Fatalf("decoded distro family = %q", selector.DistroFamily)
	}

	for _, field := range []string{"platform_family", "distroFamily", "unknown_family_field"} {
		data := []byte(`{"os_family":"linux","` + field + `":"debian"}`)
		if err := strictJSON(data, &selector); err == nil {
			t.Fatalf("strictJSON accepted unknown selector field %q", field)
		}
	}
}

func TestValidateSelectorDistroFamiliesAndCombinations(t *testing.T) {
	for _, family := range []string{"debian", " RHEL ", "ArCh"} {
		selector := PlatformSelector{OSFamily: " LINUX ", DistroFamily: family}
		if err := validateSelector("test.recipe", selector); err != nil {
			t.Fatalf("validateSelector rejected distro_family %q: %v", family, err)
		}
	}

	for _, family := range []string{"alpine", "suse", "nixos", "unknown"} {
		err := validateSelector("test.recipe", PlatformSelector{
			OSFamily:     "linux",
			DistroFamily: family,
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported distro_family") {
			t.Fatalf("validateSelector distro_family %q error = %v", family, err)
		}
	}

	validCombination := PlatformSelector{
		OSFamily:       "linux",
		DistroFamily:   "debian",
		DistroID:       "ubuntu",
		PackageManager: "apt",
		ServiceManager: "systemd",
	}
	if err := validateSelector("test.recipe", validCombination); err != nil {
		t.Fatalf("validateSelector exact distro/family/manager combination: %v", err)
	}

	invalidCombinations := []PlatformSelector{
		{OSFamily: "linux", DistroID: "ubuntu", DistroLike: "debian"},
		{OSFamily: "linux", DistroFamily: "debian", DistroLike: "debian"},
		{OSFamily: "linux", PackageManager: "apt"},
		{OSFamily: "linux", ServiceManager: "systemd"},
	}
	for _, selector := range invalidCombinations {
		if err := validateSelector("test.recipe", selector); err == nil {
			t.Fatalf("validateSelector accepted invalid combination %#v", selector)
		}
	}
}

func TestSelectorSpecificityDistroFamilyPrecedence(t *testing.T) {
	host := HostProfile{
		OSFamily:       " LINUX ",
		DistroFamily:   " DeBiAn ",
		DistroID:       " Ubuntu ",
		DistroLike:     []string{" Debian "},
		Version:        "24.04",
		Architecture:   " AMD64 ",
		PackageManager: " APT ",
		ServiceManager: " SYSTEMD ",
	}
	tests := []struct {
		name     string
		selector PlatformSelector
		want     int
	}{
		{
			name: "exact distro dominates family and managers",
			selector: PlatformSelector{
				OSFamily:       "linux",
				DistroFamily:   "debian",
				DistroID:       "ubuntu",
				Version:        ">=24,<25",
				Architectures:  []string{"amd64"},
				PackageManager: "apt",
				ServiceManager: "systemd",
			},
			want: 730,
		},
		{
			name: "distro family dominates managers",
			selector: PlatformSelector{
				OSFamily:       "linux",
				DistroFamily:   "debian",
				Version:        ">=24,<25",
				Architectures:  []string{"amd64"},
				PackageManager: "apt",
				ServiceManager: "systemd",
			},
			want: 530,
		},
		{
			name: "distro like dominates managers",
			selector: PlatformSelector{
				OSFamily:       "linux",
				DistroLike:     "debian",
				Version:        ">=24,<25",
				Architectures:  []string{"amd64"},
				PackageManager: "apt",
				ServiceManager: "systemd",
			},
			want: 430,
		},
		{
			name: "manager pair dominates OS family",
			selector: PlatformSelector{
				OSFamily:       "linux",
				Version:        ">=24,<25",
				Architectures:  []string{"amd64"},
				PackageManager: "apt",
				ServiceManager: "systemd",
			},
			want: 230,
		},
		{
			name: "OS family fallback",
			selector: PlatformSelector{
				OSFamily:      "linux",
				Version:       ">=24,<25",
				Architectures: []string{"amd64"},
			},
			want: 130,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateSelector("test.recipe", test.selector); err != nil {
				t.Fatalf("validateSelector: %v", err)
			}
			got, matches, err := selectorSpecificity(test.selector, host)
			if err != nil || !matches || got != test.want {
				t.Fatalf("selectorSpecificity = (%d, %v, %v), want (%d, true, nil)", got, matches, err, test.want)
			}
		})
	}
}

func TestSelectorSpecificityDistroFamilyFailsClosed(t *testing.T) {
	selector := PlatformSelector{OSFamily: "linux", DistroFamily: "debian"}

	for _, family := range []string{"rhel", "arch"} {
		score, matches, err := selectorSpecificity(selector, HostProfile{
			OSFamily:     "linux",
			DistroFamily: family,
		})
		if err != nil || matches || score != 0 {
			t.Fatalf("host distro_family %q = (%d, %v, %v), want no match", family, score, matches, err)
		}
	}

	_, _, err := selectorSpecificity(selector, HostProfile{OSFamily: "linux"})
	if err == nil || !strings.Contains(err.Error(), "distro_family is required") {
		t.Fatalf("missing host distro family error = %v", err)
	}

	_, _, err = selectorSpecificity(
		PlatformSelector{OSFamily: "linux"},
		HostProfile{OSFamily: "linux", DistroFamily: "suse"},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported distro_family") {
		t.Fatalf("unknown host distro family error = %v", err)
	}
}

func TestDistroFamilyRequiresAgentSchemaTwo(t *testing.T) {
	doc := testCatalogDocument("release-key-1")
	doc.Metadata.MinimumAgentSchema = 1
	doc.Recipes[0].Selector.DistroFamily = "debian"
	if err := validateDocument(doc); err == nil || !strings.Contains(err.Error(), "minimum_agent_schema") {
		t.Fatalf("distro_family with agent schema 1 error = %v", err)
	}

	for index := range doc.Recipes {
		doc.Recipes[index].Selector.DistroFamily = ""
	}
	if err := validateDocument(doc); err != nil {
		t.Fatalf("legacy selector with agent schema 1: %v", err)
	}
}

func TestAgentTwoOpensLegacyCatalogWithoutDistroFamily(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	doc.Metadata.MinimumAgentSchema = 1
	for index := range doc.Recipes {
		doc.Recipes[index].Selector.DistroFamily = ""
	}
	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	catalog, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err != nil {
		t.Fatalf("agent schema 2 rejected legacy catalog: %v", err)
	}
	defer catalog.Close()
}

func TestResolveRejectsEqualDistroFamilySpecificity(t *testing.T) {
	requireSecureCatalogTestFilesystem(t)
	doc := testCatalogDocument("release-key-1")
	base := doc.Recipes[2]
	base.ID = "memcached.debian-family.install"
	base.PlatformKey = "debian-family"
	base.Selector.DistroFamily = "debian"
	alternate := base
	alternate.ID = "memcached.debian-family.alternate"
	alternate.PlatformKey = "debian-family-alternate"
	doc.Recipes = append(doc.Recipes, base, alternate)

	path, signature, publicKey := buildSignedTestCatalog(t, doc)
	catalog, err := OpenVerified(
		context.Background(),
		path,
		signature,
		map[string]ed25519.PublicKey{"release-key-1": publicKey},
		testOpenPolicy(),
	)
	if err != nil {
		t.Fatalf("OpenVerified: %v", err)
	}
	defer catalog.Close()

	_, err = catalog.Resolve(context.Background(), "memcached", "install", HostProfile{
		OSFamily:       "linux",
		DistroFamily:   "debian",
		DistroID:       "linuxmint",
		DistroLike:     []string{"debian"},
		Version:        "22",
		Architecture:   "amd64",
		PackageManager: "apt",
		ServiceManager: "systemd",
	})
	if !errors.Is(err, ErrRecipeAmbiguous) {
		t.Fatalf("Resolve distro-family ambiguity error = %v", err)
	}
}
