//go:build linux

package pdnspeerenrollment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnspeerenrollment"
)

func ownerWriterRoot(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned enrollment test")
	}
	root, err := os.MkdirTemp("/root", "pdns-peer-owner-")
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
		CatalogName: catalog, SSHUsername: OwnerSSHUsername,
		HostKeySHA256: strings.Repeat("a", 64),
	}
}

func TestOwnerPowerDNSPrepareActivateRevokeExactEnrollment(t *testing.T) {
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
	dirInfo, err := os.Lstat(ownerRootPath(root, CredentialDir))
	if err != nil || dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("credential directory mode: %v %v", dirInfo, err)
	}
	input := ownerActivationFor(t, prepared)
	record, err := activateOwnerAt(root, input)
	if err != nil {
		t.Fatal(err)
	}
	if record.ClientPublicKeySHA256 != prepared.PublicKeySHA256 || record.CredentialID != prepared.CredentialID ||
		record.Engine != "pdns" || record.Schema != SchemaV1 || record.View != defaultView {
		t.Fatal("active record did not bind the prepared private key and engine")
	}
	// The existing production reader must accept the written bytes exactly.
	raw, err := os.ReadFile(ownerRootPath(root, EnrollmentPath))
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(record)
	if string(raw) != string(canonical) {
		t.Fatalf("record is not the canonical reader encoding: %s", raw)
	}
	snapshot, err := readAt(root)
	if err != nil || snapshot.Record != record {
		t.Fatalf("existing production reader rejected owner enrollment: %v", err)
	}
	if snapshot.Transport.Username != OwnerSSHUsername || snapshot.Transport.HostKeySHA256 != input.HostKeySHA256 ||
		snapshot.Transport.PrivateKeyPath != CredentialDir+"/"+prepared.CredentialID+".key" {
		t.Fatalf("transport binding differs: %+v", snapshot.Transport)
	}
	if _, err := activateOwnerAt(root, input); err == nil {
		t.Fatal("a second activation overwrote an active enrollment")
	}
	if _, err := prepareOwnerAt(root); err == nil {
		t.Fatal("preparation ignored an active enrollment")
	}
	if err := revokeOwnerAt(root); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		t.Fatalf("revocation left the inspection credential active: %v", err)
	}
	archive := ownerRootPath(root, revokedPathPrefix+record.EnrollmentID+".json")
	if got, err := os.ReadFile(archive); err != nil || string(got) != string(canonical) {
		t.Fatal("revocation did not retain its exact owner record")
	}
	if err := revokeOwnerAt(root); err == nil {
		t.Fatal("repeat revocation rewrote unknown state")
	}
}

func TestOwnerPowerDNSActivationRejectsWrongPairAccountAndUnsafeState(t *testing.T) {
	root := ownerWriterRoot(t)
	prepared, err := prepareOwnerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*OwnerActivation){
		"catalog":    func(a *OwnerActivation) { a.CatalogName = "other.example.test" },
		"host key":   func(a *OwnerActivation) { a.HostKeySHA256 = "not-a-reviewed-host-key" },
		"account":    func(a *OwnerActivation) { a.SSHUsername = "root" },
		"same peer":  func(a *OwnerActivation) { a.PeerIP = a.PrimaryIP },
		"credential": func(a *OwnerActivation) { a.CredentialID = strings.Repeat("0", 32) },
		"path id":    func(a *OwnerActivation) { a.CredentialID = "../" + a.CredentialID },
	} {
		input := ownerActivationFor(t, prepared)
		edit(&input)
		if _, err := activateOwnerAt(root, input); err == nil {
			t.Fatalf("%s: invalid activation accepted", name)
		}
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
	if raw, _ := os.ReadFile(recordPath); string(raw) != "unsafe" {
		t.Fatal("owner evidence was overwritten")
	}
}

func TestOwnerPowerDNSEnrollmentRefusesWhileBINDEnrollmentExists(t *testing.T) {
	if dnspeerenrollment.PowerDNSEnrollmentPath != EnrollmentPath {
		t.Fatal("BIND writer checks a different PowerDNS enrollment path")
	}
	root := ownerWriterRoot(t)
	prepared, err := prepareOwnerAt(root)
	if err != nil {
		t.Fatal(err)
	}
	bind := ownerRootPath(root, dnspeerenrollment.EnrollmentPath)
	if err := os.WriteFile(bind, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareOwnerAt(root); err == nil || !strings.Contains(err.Error(), "primary-status --engine bind") {
		t.Fatalf("PowerDNS preparation ignored BIND enrollment: %v", err)
	}
	if _, err := activateOwnerAt(root, ownerActivationFor(t, prepared)); err == nil || !strings.Contains(err.Error(), "primary-revoke --engine bind") {
		t.Fatalf("PowerDNS activation ignored BIND enrollment: %v", err)
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		t.Fatalf("refused activation published a PowerDNS record: %v", err)
	}
	if raw, _ := os.ReadFile(bind); string(raw) != "{}" {
		t.Fatal("BIND enrollment was changed")
	}
	if err := os.Remove(bind); err != nil {
		t.Fatal(err)
	}
	if _, err := activateOwnerAt(root, ownerActivationFor(t, prepared)); err != nil {
		t.Fatalf("activation after the owner removed the BIND enrollment: %v", err)
	}
}
