package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
)

// aptListsAge decides whether an install is preceded by `apt-get update`; a
// wrong answer either slows every install or lets a stale list 404 through.
func TestAptListsAge(t *testing.T) {
	orig := aptListsDir
	defer func() { aptListsDir = orig }()

	// Missing directory → unknown, callers must treat as stale.
	aptListsDir = filepath.Join(t.TempDir(), "does-not-exist")
	if _, ok := aptListsAge(); ok {
		t.Error("missing lists dir must report ok=false")
	}

	// Empty directory (only subdirs, like partial/) → still unknown.
	dir := t.TempDir()
	aptListsDir = dir
	if err := os.Mkdir(filepath.Join(dir, "partial"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := aptListsAge(); ok {
		t.Error("dir with no list files must report ok=false")
	}

	// A fresh list file → small age, ok=true.
	f := filepath.Join(dir, "deb.debian.org_debian_dists_trixie_InRelease")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	age, ok := aptListsAge()
	if !ok || age > time.Minute {
		t.Errorf("fresh file: age=%v ok=%v, want recent + true", age, ok)
	}

	// Back-date it → age must reflect the newest file, and an even older
	// second file must not win.
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	age, ok = aptListsAge()
	if !ok || age < 2*time.Hour {
		t.Errorf("back-dated file: age=%v ok=%v, want ≥2h + true", age, ok)
	}
}

func TestPacmanInstallArgsUseOneFullUpgradeTransaction(t *testing.T) {
	packages := []string{"nginx", "php-fpm"}
	got := pacmanInstallArgs(packages)
	want := []string{"-Syu", "--noconfirm", "--needed", "nginx", "php-fpm"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pacman install args = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(packages, []string{"nginx", "php-fpm"}) {
		t.Fatalf("pacmanInstallArgs mutated caller packages: %q", packages)
	}
}

func TestPackageInstalledForUnknownFamilyFailsClosed(t *testing.T) {
	if packageInstalledForFamily("", "bash") {
		t.Fatal("unknown package family must not be treated as apt")
	}
	if packageInstalledForFamily("windows", "bash") {
		t.Fatal("unsupported package family must fail closed")
	}
}

func TestPackageInstalledForFamilyRejectsInvalidPackageBeforeExec(t *testing.T) {
	for _, family := range []string{"apt", "pacman", "dnf"} {
		if packageInstalledForFamily(family, "--help") {
			t.Fatalf("family %q accepted an invalid package name", family)
		}
	}
}

func TestDetectPkgFamilyUsesVerifiedHostProfile(t *testing.T) {
	original := detectHostPlatform
	defer func() { detectHostPlatform = original }()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{PackageManager: hostplatform.PackageManagerAPT}, nil
	}
	if got := detectPkgFamily(); got != "apt" {
		t.Fatalf("detectPkgFamily() = %q, want apt", got)
	}
}

func TestPkgFamilySurfacesDetectionError(t *testing.T) {
	original := detectHostPlatform
	defer func() { detectHostPlatform = original }()
	detectHostPlatform = func() (hostplatform.Profile, error) {
		return hostplatform.Profile{}, errors.New("systemd offline")
	}
	var reply string
	err := (&Agent{}).PkgFamily(&transport.Empty{}, &reply)
	if err == nil || !strings.Contains(err.Error(), "systemd offline") {
		t.Fatalf("PkgFamily error = %v, want platform detection detail", err)
	}
	if reply != "" {
		t.Fatalf("PkgFamily reply = %q, want fail-closed empty value", reply)
	}
	if got := detectPkgFamily(); got != "" {
		t.Fatalf("compatibility family = %q, want fail-closed empty value", got)
	}
}
