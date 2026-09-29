//go:build linux

package dnspeerenrollment

import (
	"os"
	"strings"
	"testing"
)

// A PowerDNS owner enrollment blocks BIND preparation and activation; the
// Agent would otherwise see two enrollments and keep every deletion pending.
func TestOwnerBINDEnrollmentRefusesWhilePowerDNSEnrollmentExists(t *testing.T) {
	root := ownerWriterRoot(t)
	prepared, err := prepareOwnerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	other := ownerRootPath(root, PowerDNSEnrollmentPath)
	if err := os.WriteFile(other, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareOwnerAt(root); err == nil || !strings.Contains(err.Error(), "--engine pdns") {
		t.Fatalf("BIND preparation ignored PowerDNS enrollment: %v", err)
	}
	if _, err := activateOwnerAt(root, ownerActivationFor(t, prepared)); err == nil || !strings.Contains(err.Error(), "primary-revoke --engine pdns") {
		t.Fatalf("BIND activation ignored PowerDNS enrollment: %v", err)
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		t.Fatalf("refused activation published a BIND record: %v", err)
	}
	raw, err := os.ReadFile(other)
	if err != nil || string(raw) != "{}" {
		t.Fatal("PowerDNS enrollment was changed")
	}
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	if _, err := activateOwnerAt(root, ownerActivationFor(t, prepared)); err != nil {
		t.Fatalf("activation after the owner removed PowerDNS enrollment: %v", err)
	}
}
