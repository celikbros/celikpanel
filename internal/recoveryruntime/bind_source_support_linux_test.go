//go:build linux

package recoveryruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func bindCapabilityFixture(t *testing.T, body string) runtimeFixture {
	t.Helper()
	fixture := newRuntimeFixture(t)
	path := filepath.Join(fixture.root, "bin/recovery")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture.root, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Files["bin/recovery"] = Digest(payload)
	raw, err = EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, ManifestName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.digest = Digest(raw)
	next := filepath.Join(fixture.config.runtimeRoot, fixture.digest)
	if err := os.Rename(fixture.root, next); err != nil {
		t.Fatal(err)
	}
	fixture.root = next
	selection, _ := EncodeSelection(fixture.digest)
	if err := os.WriteFile(fixture.config.selectionPath, selection, 0600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestSelectedBINDSourceCapabilityRequiresExactVerifiedReply(t *testing.T) {
	for _, name := range []string{"supported", "old-binary", "malformed", "oversized", "cancelled", "changed-selection", "changed-binary"} {
		t.Run(name, func(t *testing.T) {
			body := "test \"$1\" = check-bind-source-inverse-v1 || exit 2\nprintf 'celikpanel-bind-source-inverse/v1\\n'"
			switch name {
			case "old-binary":
				body = "exit 2"
			case "malformed":
				body = "printf 'supported\\n'"
			case "oversized":
				body = "head -c 1024 /dev/zero"
			case "cancelled":
				body = "exec /bin/sleep 10"
			}
			f := bindCapabilityFixture(t, body)
			runtime, err := resolveAt(f.config)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if name == "changed-selection" {
				selection, _ := EncodeSelection(strings.Repeat("f", 64))
				if err := os.WriteFile(f.config.selectionPath, selection, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "changed-binary" {
				if err := os.WriteFile(filepath.Join(f.root, "bin/recovery"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			err = CheckBINDSourceInverseSupport(ctx, runtime)
			if (err == nil) != (name == "supported") {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

func TestSelectedBINDAdoptionCapabilityIsDistinct(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		supported  bool
	}{
		{"adoption", "test \"$1\" = check-bind-adoption-inverse-v1 || exit 2\nprintf 'celikpanel-bind-adoption-inverse/v1\\n'", true},
		{"source-only", "test \"$1\" = check-bind-source-inverse-v1 || exit 2\nprintf 'celikpanel-bind-source-inverse/v1\\n'", false},
		{"wrong-marker", "printf 'celikpanel-bind-source-inverse/v1\\n'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := bindCapabilityFixture(t, tc.body)
			runtime, err := resolveAt(f.config)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			err = CheckBINDAdoptionInverseSupport(context.Background(), runtime)
			if (err == nil) != tc.supported {
				t.Fatalf("support=%v err=%v", tc.supported, err)
			}
		})
	}
}
