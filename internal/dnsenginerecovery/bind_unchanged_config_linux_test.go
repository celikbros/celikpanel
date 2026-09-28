//go:build linux

package dnsenginerecovery

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestBINDUnchangedConfigSecureEvidence(t *testing.T) {
	for _, kind := range []string{"exact", "root-hints", "root-hints-edit", "main-zone", "main-include", "default-edit", "default-include", "mode", "group", "symlink", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			root, fd, p, j := bindSwitchConfigProbeFixture(t)
			// The envelope is supported only for a managed PowerDNS source.
			p, _, j, _ = bindStateRestoreFixture(t)
			parent := filepath.Join(root, "etc/bind")
			if err := os.Chown(parent, 0, 42); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0o2755); err != nil {
				t.Fatal(err)
			}
			main := filepath.Join(root, "etc/bind/named.conf")
			defaults := filepath.Join(root, "etc/bind/named.conf.default-zones")
			raw := []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")
			if kind == "root-hints" || kind == "root-hints-edit" {
				defaults = filepath.Join(root, "etc/bind/named.conf.root-hints")
				raw = []byte(strings.ReplaceAll(string(raw), "named.conf.default-zones", "named.conf.root-hints"))
			}
			if err := os.WriteFile(main, raw, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(defaults, []byte("// defaults\n"), 0644); err != nil {
				t.Fatal(err)
			}
			frozen, err := captureBINDUnchangedConfigAtV2(context.Background(), fd, bindroot.APT, 42)
			if err != nil {
				t.Fatal(err)
			}
			after := append([]dnsengineartifact.FileSnapshot(nil), j.InversePlan.ConfigAfter...)
			managed, err := bindconfig.ManagedZoneInclude(string(j.ConfigBefore[0].Data), "/var/cache/bind/celikpanel/current/zones.conf")
			if err != nil {
				t.Fatal(err)
			}
			after[0].Data = []byte(managed)
			after[0].SHA256 = dnsengineartifact.DigestBytes(after[0].Data)
			j.Schema = dnsengineartifact.SwitchJournalSchemaV1
			j.Phase = dnsengineartifact.SwitchPhaseIntent
			j.InversePlan = nil
			j, err = p.BuildBINDSwitchInverseJournalV2(j, "apt", after, dnsengineartifact.BINDSwitchSourceProofV2{BINDUnchangedConfig: frozen})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "main-zone":
				err = os.WriteFile(main, append(raw, []byte("zone \"owner.test\" { type primary; file \"owner.zone\"; };\n")...), 0644)
			case "main-include":
				err = os.WriteFile(main, append(raw, []byte("include \"/etc/bind/owner.conf\";\n")...), 0644)
			case "default-edit", "root-hints-edit":
				err = os.WriteFile(defaults, []byte("// owner updated defaults\n"), 0644)
			case "default-include":
				err = os.WriteFile(defaults, []byte("include \"/etc/bind/owner.conf\";\n"), 0644)
			case "mode":
				err = os.Chmod(defaults, 0600)
			case "group":
				err = os.Chown(defaults, 0, 42)
			case "symlink":
				if err = os.Remove(defaults); err == nil {
					err = os.Symlink(main, defaults)
				}
			case "legacy":
				j.InversePlan.BINDUnchangedConfig = nil
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(defaults)
			err = verifyBINDUnchangedConfigAtV2(context.Background(), fd, p, j, bindroot.APT, 42)
			if (err == nil) != (kind == "exact" || kind == "root-hints") {
				t.Fatalf("kind %s verification: %v", kind, err)
			}
			preserved, _ := os.ReadFile(defaults)
			if !reflect.DeepEqual(before, preserved) {
				t.Fatal("verification modified owner bytes")
			}
		})
	}
}
