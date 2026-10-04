//go:build linux

package dnspeerenrollment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
)

func ownerWriterRoot(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned enrollment test")
	}
	root, err := os.MkdirTemp("/root", "dns-peer-owner-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	private := filepath.Join(root, "var/lib/celikpanel-agent-private")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func ownerActivationFor(t *testing.T, prepared PreparedOwner) OwnerActivation {
	t.Helper()
	catalog, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	return OwnerActivation{
		CredentialID: prepared.CredentialID,
		PrimaryIP:    "192.0.2.10", PeerIP: "192.0.2.11",
		CatalogName: catalog, SSHUsername: "celikpeer",
		HostKeySHA256: strings.Repeat("a", 64),
	}
}

func TestOwnerPrepareActivateRevokeExactEnrollment(t *testing.T) {
	root := ownerWriterRoot(t)
	prepared, err := prepareOwnerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.CredentialID) != 32 || len(prepared.PublicKeySHA256) != 64 ||
		!strings.HasPrefix(prepared.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("unexpected public preparation: %+v", prepared)
	}
	keyPath := ownerRootPath(root, CredentialDir+"/"+prepared.CredentialID+".key")
	info, err := os.Lstat(keyPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("private credential has unsafe storage: %v %v", info, err)
	}
	input := ownerActivationFor(t, prepared)
	record, err := activateOwnerAt(root, input)
	if err != nil {
		t.Fatal(err)
	}
	if record.ClientPublicKeySHA256 != prepared.PublicKeySHA256 ||
		record.CredentialID != prepared.CredentialID {
		t.Fatal("active record did not bind the prepared private key")
	}
	snapshot, err := readAt(root)
	if err != nil || snapshot.Record != record {
		t.Fatalf("existing production reader rejected owner enrollment: %v", err)
	}
	if _, err := activateOwnerAt(root, input); err == nil {
		t.Fatal("a second activation overwrote an active enrollment")
	}
	if err := revokeOwnerAt(root); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		t.Fatalf("revocation left the inspection credential active: %v", err)
	}
	archive := ownerRootPath(root, "/var/lib/celikpanel-agent-private/dns-peer-inspection-revoked-"+record.EnrollmentID+".json")
	if _, err := os.Lstat(archive); err != nil {
		t.Fatal("revocation did not retain its exact owner record")
	}
	if err := revokeOwnerAt(root); err == nil {
		t.Fatal("repeat revocation rewrote unknown state")
	}
}

func TestOwnerActivationRejectsWrongPairAndUnsafeState(t *testing.T) {
	root := ownerWriterRoot(t)
	prepared, err := prepareOwnerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	input := ownerActivationFor(t, prepared)
	input.CatalogName = "other.example.test"
	if _, err := activateOwnerAt(root, input); err == nil {
		t.Fatal("unrelated catalog activated")
	}
	input = ownerActivationFor(t, prepared)
	input.HostKeySHA256 = "not-a-reviewed-host-key"
	if _, err := activateOwnerAt(root, input); err == nil {
		t.Fatal("unreviewed host key activated")
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		t.Fatalf("failed activation published a record: %v", err)
	}
	recordPath := ownerRootPath(root, EnrollmentPath)
	if err := os.WriteFile(recordPath, []byte("unsafe"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareOwnerAt(root); err == nil {
		t.Fatal("unsafe existing enrollment was ignored")
	}
	if _, err := activateOwnerAt(root, ownerActivationFor(t, prepared)); err == nil {
		t.Fatal("unsafe existing enrollment was replaced")
	}
}
