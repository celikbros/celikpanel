//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func TestBINDAdoptionInstallReceiptRetirement(t *testing.T) {
	for _, variant := range []string{"exact", "absent", "wrong-request", "wrong-owner", "wrong-qualifier", "installed-package", "unknown-field", "rolled-back-reappeared", "unsafe-mode", "hardlink", "symlink", "engine-ownership", "other-install"} {
		t.Run(variant, func(t *testing.T) {
			policy, j := runningBINDJournalFixture(t)
			j.Phase = dnsengineartifact.SwitchPhaseRollingBack
			owner := servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}
			path := filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-install-ownership-bind.json")
			raw := bindAdoptionExpectedInstallReceipt(j)
			switch variant {
			case "wrong-request":
				raw = bytes.ReplaceAll(raw, []byte(j.MutationRequestID), []byte(strings.Repeat("e", 32)))
			case "wrong-owner":
				raw = bytes.ReplaceAll(raw, []byte(j.MutationOwnerID), []byte(strings.Repeat("f", 32)))
			case "wrong-qualifier":
				raw = bytes.ReplaceAll(raw, []byte(j.ManifestQualifier), []byte("foreign"))
			case "installed-package":
				raw = bytes.ReplaceAll(raw, []byte(`"missing_before":[]`), []byte(`"missing_before":["bind9"]`))
			case "unknown-field":
				raw = bytes.Replace(raw, []byte("{"), []byte(`{"unexpected":true,`), 1)
			case "rolled-back-reappeared":
				j.Phase = dnsengineartifact.SwitchPhaseRolledBack
			}
			if variant != "absent" {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch variant {
			case "unsafe-mode":
				if err := os.Chmod(path, 0666); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".other"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "engine-ownership", "other-install":
				name := "dns-engine-ownership-bind.json"
				if variant == "other-install" {
					name = "dns-engine-install-ownership-pdns.json"
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(path), name), []byte("{}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := RemoveExactBINDAdoptionInstallReceipt(policy, owner, j)
			if variant == "exact" || variant == "absent" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("receipt remained")
				}
				j.Phase = dnsengineartifact.SwitchPhaseRolledBack
				if err := RemoveExactBINDAdoptionInstallReceipt(policy, owner, j); err != nil {
					t.Fatal("durable retry:", err)
				}
			} else {
				if err == nil {
					t.Fatal("foreign or unsafe receipt accepted")
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(got, raw) {
					t.Fatal("refusal changed receipt")
				}
			}
		})
	}
}
