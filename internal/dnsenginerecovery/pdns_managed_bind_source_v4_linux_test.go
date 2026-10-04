//go:build linux

package dnsenginerecovery

import (
	"context"
	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/binddns"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestManagedBINDSourceV4RefusesIncompleteReadOnlyProof(t *testing.T) {
	validGeneration := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name       string
		ctx        context.Context
		generation string
		epoch      int64
		gid        uint32
	}{
		{"nil-context", nil, validGeneration, 1, 100},
		{"invalid-generation", context.Background(), "foreign", 1, 100},
		{"missing-epoch", context.Background(), validGeneration, 0, 100},
		{"missing-group", context.Background(), validGeneration, 1, 0},
		{"oversized-group", context.Background(), validGeneration, 1, 1 << 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CaptureManagedBINDSourceProofV4(tc.ctx, tc.generation, tc.epoch, tc.gid); err == nil {
				t.Fatal("incomplete BIND source proof reached native file observation")
			}
		})
	}
	if err := VerifyManagedBINDSourceProofV4(context.Background(), dnsengineartifact.ManagedBINDSourceProofV4{}, 100); err == nil {
		t.Fatal("empty managed BIND source proof accepted")
	}
}

func TestManagedBINDSourceV4CapturesExactNativeConfigAndRejectsDrift(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned native fixture requires root")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "etc", "bind")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	const gid uint32 = 12345
	local, err := bindconfig.ManagedZoneInclude("// owner local\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"named.conf":               "include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n",
		"named.conf.default-zones": "// Debian defaults\n",
		"named.conf.local":         local,
		"named.conf.options":       "options { recursion no; };\n",
	}
	for name, body := range files {
		path := filepath.Join(parent, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, 0, int(gid)); err != nil {
			t.Fatal(err)
		}
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	generation := strings.Repeat("a", 64)
	receipt := binddns.Receipt{Generation: generation, EngineEpoch: 7}
	load := func() (binddns.Receipt, error) { return receipt, nil }
	proof, err := captureManagedBINDSourceAtV4(context.Background(), fd, load, generation, 7, gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.ConfigBefore) != 4 || proof.ConfigBefore[1].Path != "/etc/bind/named.conf.default-zones" || proof.ReceiptSHA256 == "" {
		t.Fatalf("incomplete source proof: %+v", proof)
	}
	calls := 0
	driftReceipt := func() (binddns.Receipt, error) {
		calls++
		next := receipt
		if calls == 2 {
			next.EngineEpoch++
		}
		return next, nil
	}
	if _, err := captureManagedBINDSourceAtV4(context.Background(), fd, driftReceipt, generation, 7, gid); err == nil {
		t.Fatal("receipt change during source capture accepted")
	}
	calls = 0
	driftConfig := func() (binddns.Receipt, error) {
		calls++
		if calls == 2 {
			path := filepath.Join(parent, "named.conf.options")
			if err := os.WriteFile(path, []byte("options { recursion yes; };\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 0, int(gid)); err != nil {
				t.Fatal(err)
			}
		}
		return receipt, nil
	}
	if _, err := captureManagedBINDSourceAtV4(context.Background(), fd, driftConfig, generation, 7, gid); err == nil {
		t.Fatal("native config change during source capture accepted")
	}
}
