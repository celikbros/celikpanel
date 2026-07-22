package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The two orderings a lexical sort gets wrong, pinned: a two-digit major
// (24 > 9) and a two-digit minor (8.10 > 8.9). ListNodeVersions shipped with
// sort.Strings and listed Node 9.9.9 above 24.x — the picker's first offer
// was the oldest runtime.
// Sözlük sıralamasının yanlış yaptığı iki sıra sabitlenir: iki basamaklı major
// (24 > 9) ve iki basamaklı minor (8.10 > 8.9). ListNodeVersions sort.Strings
// ile çıkmıştı ve Node 9.9.9'u 24.x'in üstünde listeliyordu — seçicinin ilk
// önerisi en eski runtime'dı.
func TestVersionLess(t *testing.T) {
	cases := []struct {
		older, newer string
	}{
		{"9.9.9", "24.18.0"},
		{"8.9", "8.10"},
		{"8.3", "8.4"},
		{"24.18.0", "24.18.1"},
		{"24.18", "24.18.0"}, // fewer segments = older on a tied prefix
	}
	for _, c := range cases {
		if !versionLess(c.older, c.newer) {
			t.Errorf("versionLess(%q, %q) = false, want true", c.older, c.newer)
		}
		if versionLess(c.newer, c.older) {
			t.Errorf("versionLess(%q, %q) = true, want false", c.newer, c.older)
		}
	}
	if versionLess("8.3", "8.3") {
		t.Error("a version must not be less than itself")
	}
}

func TestComposeUnitStatus(t *testing.T) {
	cases := []struct {
		active, sub, want string
	}{
		{"active", "running", "active (running)"},
		{"inactive", "dead", "inactive (dead)"},
		{"active", "exited", "active (exited)"}, // oneshot units are up too
		{"failed", "failed", "failed (failed)"},
		{"active", "", "active"},
		{"", "running", ""}, // no ActiveState → no invented status
	}
	for _, c := range cases {
		if got := composeUnitStatus(c.active, c.sub); got != c.want {
			t.Errorf("composeUnitStatus(%q, %q) = %q, want %q", c.active, c.sub, got, c.want)
		}
	}
}

// The banner regex must survive both producers: php-fpm's "PHP 8.4.11
// (fpm-fcgi)" and php-cli's "PHP 8.3.6 (cli)". Major.minor only — that is the
// unit plans grant and sites select (D-014).
// Banner regex'i iki üreticiden de sağ çıkmalı: php-fpm'in "PHP 8.4.11
// (fpm-fcgi)"ı ve php-cli'ın "PHP 8.3.6 (cli)"ı. Yalnız major.minor — planların
// verdiği ve sitelerin seçtiği birim odur (D-014).
func TestPHPVersionBanner(t *testing.T) {
	cases := []struct {
		banner, want string
	}{
		{"PHP 8.4.11 (fpm-fcgi) (built: Jul  4 2026 05:15:12)", "8.4"},
		{"PHP 8.3.6 (cli) (built: Mar 13 2026 11:12:13) (NTS)", "8.3"},
		{"no version here", ""},
	}
	for _, c := range cases {
		got := ""
		if m := phpVersionInBanner.FindSubmatch([]byte(c.banner)); m != nil {
			got = string(m[1])
		}
		if got != c.want {
			t.Errorf("banner %q → %q, want %q", c.banner, got, c.want)
		}
	}
}

// nodeInstances trusts only bin/node's existence: a version directory without
// the binary (interrupted install) must not be reported, and versions must
// come back newest first with Managed=true, Unit empty, real Path.
// nodeInstances yalnız bin/node'un varlığına güvenir: binary'siz sürüm dizini
// (yarıda kalmış kurulum) bildirilmemeli; sürümler en yeni önce, Managed=true,
// Unit boş ve gerçek Path ile dönmelidir.
func TestNodeInstancesFromDisk(t *testing.T) {
	oldBase := runtimesBaseDir
	runtimesBaseDir = t.TempDir()
	defer func() { runtimesBaseDir = oldBase }()

	mk := func(version string, withBin bool) {
		dir := filepath.Join(runtimesBaseDir, "node", version, "bin")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if withBin {
			if err := os.WriteFile(filepath.Join(dir, "node"), []byte("#!"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("9.9.9", true)
	mk("24.18.0", true)
	mk("22.1.0", false)     // no binary → not installed
	mk("not-a-version", true) // ignored by the semver gate

	got := nodeInstances()
	// A system PATH node (Managed=false) may or may not exist on the test
	// machine — assert only on the managed set.
	// Test makinesinde PATH'te node olabilir de olmayabilir de (Managed=false)
	// — yalnız yönetilen küme üzerinde doğrula.
	managed := []string{}
	for _, in := range got {
		if !in.Managed {
			continue
		}
		managed = append(managed, in.Version)
		if in.Unit != "" {
			t.Errorf("%s: a node instance must have no unit, got %q", in.Version, in.Unit)
		}
		if in.Path != filepath.Join(runtimesBaseDir, "node", in.Version) {
			t.Errorf("%s: path = %q", in.Version, in.Path)
		}
	}
	want := []string{"24.18.0", "9.9.9"}
	if len(managed) != len(want) || managed[0] != want[0] || managed[1] != want[1] {
		t.Errorf("managed versions = %v, want %v", managed, want)
	}
}
