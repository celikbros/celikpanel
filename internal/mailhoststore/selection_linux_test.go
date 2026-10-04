//go:build linux

package mailhoststore

import (
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCurrentObservationDetectsOwnerReplacementAndKeepsSiblingStaging(t *testing.T) {
	for _, scenario := range []string{"same", "sibling-stage", "same-target-link", "same-byte-key", "same-byte-cert", "receipt-replacement", "mode-change", "missing-current"} {
		t.Run(scenario, func(t *testing.T) {
			root, version, fd, verify := storeFixture(t)
			before, err := ObserveCurrentAt(fd, verify)
			if err != nil {
				t.Fatal(err)
			}
			if before.Version != version || before.Receipt.Domain != "mail.example.test" || mailhostartifact.ValidateLeafSHA256(before.IdentitySHA256) != nil {
				t.Fatal("invalid observation")
			}
			switch scenario {
			case "sibling-stage":
				if e := os.Mkdir(filepath.Join(root, "unrelated-stage"), 0700); e != nil {
					t.Fatal(e)
				}
			case "same-target-link":
				if e := os.Symlink(version, filepath.Join(root, "next-link")); e != nil {
					t.Fatal(e)
				}
				if e := os.Rename(filepath.Join(root, "next-link"), filepath.Join(root, "current")); e != nil {
					t.Fatal(e)
				}
			case "same-byte-key", "same-byte-cert", "receipt-replacement":
				name := "privkey.pem"
				if scenario == "same-byte-cert" {
					name = "fullchain.pem"
				}
				if scenario == "receipt-replacement" {
					name = mailhostartifact.ReceiptName
				}
				path := filepath.Join(root, version, name)
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				replacement := filepath.Join(root, "replacement")
				if e = os.WriteFile(replacement, raw, 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(replacement, path); e != nil {
					t.Fatal(e)
				}
			case "mode-change":
				if e := os.Chmod(filepath.Join(root, version), 0700); e != nil {
					t.Fatal(e)
				}
			case "missing-current":
				if e := os.Remove(filepath.Join(root, "current")); e != nil {
					t.Fatal(e)
				}
			}
			after, e := ObserveCurrentAt(fd, verify)
			if scenario == "same" || scenario == "sibling-stage" {
				if e != nil || before != after {
					t.Fatal("stable selection changed", e)
				}
				return
			}
			if e == nil && before.IdentitySHA256 == after.IdentitySHA256 {
				t.Fatal("owner edit disappeared")
			}
			if scenario == "missing-current" && e == nil {
				t.Fatal("absence became selected evidence")
			}
		})
	}
}
func TestCurrentObservationRejectsChangesInsideVerification(t *testing.T) {
	for _, name := range []string{"current", "privkey.pem", "generation"} {
		t.Run(name, func(t *testing.T) {
			root, version, fd, verify := storeFixture(t)
			changed := false
			race := func(c, k []byte, d string) ([]byte, time.Time, error) {
				if !changed {
					changed = true
					switch name {
					case "current":
						if e := os.Symlink(version, filepath.Join(root, "next")); e != nil {
							t.Fatal(e)
						}
						if e := os.Rename(filepath.Join(root, "next"), filepath.Join(root, "current")); e != nil {
							t.Fatal(e)
						}
					case "privkey.pem":
						if e := os.Chmod(filepath.Join(root, version, name), 0400); e != nil {
							t.Fatal(e)
						}
					case "generation":
						if e := os.Chmod(filepath.Join(root, version), 0700); e != nil {
							t.Fatal(e)
						}
					}
				}
				return verify(c, k, d)
			}
			if _, e := ObserveCurrentAt(fd, race); e == nil {
				t.Fatal("concurrent edit accepted")
			}
		})
	}
}
