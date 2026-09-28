//go:build linux

package pdnspeerenrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"

	"golang.org/x/crypto/ssh"
)

type fixture struct {
	root, recordPath, credentialPath string
	record                           RecordV1
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned enrollment requires root")
	}
	root, err := os.MkdirTemp("/root", "pdns-peer-enrollment-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	privateDir := filepath.Join(root, "var/lib/celikpanel-agent-private")
	keyDir := filepath.Join(privateDir, "pdns-peer-inspection-keys")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	pub := sha256.Sum256(signer.PublicKey().Marshal())
	catalog, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	record := RecordV1{
		Schema: SchemaV1, Engine: "pdns", EnrollmentID: strings.Repeat("a", 32), Revision: 1,
		PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11",
		CatalogName: catalog, View: defaultView,
		SSHUsername: "dnsobserver", HostKeySHA256: strings.Repeat("b", 64),
		CredentialID:          strings.Repeat("c", 32),
		ClientPublicKeySHA256: hex.EncodeToString(pub[:]),
	}
	rp := filepath.Join(privateDir, filepath.Base(EnrollmentPath))
	kp := filepath.Join(keyDir, record.CredentialID+".key")
	if err := os.WriteFile(kp, key, 0600); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, rp, record)
	return fixture{root: root, recordPath: rp, credentialPath: kp, record: record}
}

func writeRecord(t *testing.T, name string, record RecordV1) {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentReadAndRecheck(t *testing.T) {
	f := newFixture(t)
	snapshot, err := readAt(f.root)
	if err != nil || snapshot.Record != f.record {
		t.Fatalf("read: %v %+v", err, snapshot.Record)
	}
	if snapshot.Transport.PrivateKeyPath != CredentialDir+"/"+f.record.CredentialID+".key" ||
		snapshot.Transport.PeerIP != f.record.PeerIP || snapshot.Transport.HostKeySHA256 != f.record.HostKeySHA256 {
		t.Fatal("transport not derived from fixed enrollment")
	}
	if err := recheckAt(f.root, snapshot); err != nil {
		t.Fatal(err)
	}
	f.record.Revision++
	writeRecord(t, f.recordPath, f.record)
	if !IsCode(recheckAt(f.root, snapshot), Changed) {
		t.Fatal("owner revision edit passed recheck")
	}
}

func TestEnrollmentMissingAndUnsafeStorage(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.recordPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Disabled) {
		t.Fatalf("missing record: %v", err)
	}
	writeRecord(t, f.recordPath, f.record)
	if err := os.Chmod(f.recordPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("public record: %v", err)
	}
	if err := os.Chmod(f.recordPath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(f.recordPath), 0770); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("writable parent: %v", err)
	}
}

func TestEnrollmentRejectsSymlinkHardlinkAndCredentialSwap(t *testing.T) {
	f := newFixture(t)
	snapshot, err := readAt(f.root)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(f.recordPath), "record-copy")
	if err := os.Rename(f.recordPath, other); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, f.recordPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Remove(f.recordPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(other, f.recordPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("hardlink: %v", err)
	}
	if err := os.Remove(f.recordPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, f.recordPath); err != nil {
		t.Fatal(err)
	}
	keyRaw, err := os.ReadFile(f.credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.credentialPath + ".new"
	if err := os.WriteFile(replacement, keyRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, f.credentialPath); err != nil {
		t.Fatal(err)
	}
	if !IsCode(recheckAt(f.root, snapshot), Changed) {
		t.Fatal("same-byte credential replacement passed")
	}
}

func TestEnrollmentRejectsWrongKeyAndNoncanonicalRecord(t *testing.T) {
	f := newFixture(t)
	f.record.ClientPublicKeySHA256 = strings.Repeat("d", 64)
	writeRecord(t, f.recordPath, f.record)
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("wrong public key: %v", err)
	}
	f = newFixture(t)
	raw, err := os.ReadFile(f.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.recordPath, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("noncanonical record: %v", err)
	}
}

func TestEnrollmentRejectsPeerAndCatalogMismatch(t *testing.T) {
	f := newFixture(t)
	f.record.PeerIP = f.record.PrimaryIP
	writeRecord(t, f.recordPath, f.record)
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("same peer: %v", err)
	}
	f.record.PeerIP = "192.0.2.11"
	f.record.CatalogName = "wrong.example.test"
	writeRecord(t, f.recordPath, f.record)
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("wrong catalog: %v", err)
	}
}

func TestEnrollmentRejectsCredentialAliasesAndParentSymlink(t *testing.T) {
	f := newFixture(t)
	copyPath := f.credentialPath + ".copy"
	if err := os.Rename(f.credentialPath, copyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(copyPath, f.credentialPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("credential symlink: %v", err)
	}
	if err := os.Remove(f.credentialPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(copyPath, f.credentialPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("credential hardlink: %v", err)
	}
	if err := os.Remove(f.credentialPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(copyPath, f.credentialPath); err != nil {
		t.Fatal(err)
	}
	private := filepath.Dir(f.recordPath)
	moved := private + ".moved"
	if err := os.Rename(private, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, private); err != nil {
		t.Fatal(err)
	}
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("parent symlink: %v", err)
	}
}

func TestEnrollmentRejectsInPlaceOwnerEditOnRecheck(t *testing.T) {
	f := newFixture(t)
	snapshot, err := readAt(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.record.Revision++
	writeRecord(t, f.recordPath, f.record)
	if !IsCode(recheckAt(f.root, snapshot), Changed) {
		t.Fatal("same-inode edit passed")
	}
}

func TestEnrollmentEngineAndPeerRevisionAreBound(t *testing.T) {
	f := newFixture(t)
	snapshot, err := readAt(f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.record.Engine = "bind"
	writeRecord(t, f.recordPath, f.record)
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("BIND engine enrollment accepted: %v", err)
	}
	f.record.Engine = "pdns"
	f.record.Schema = "celikpanel-dns-peer-inspection/v1"
	writeRecord(t, f.recordPath, f.record)
	if _, err := readAt(f.root); !IsCode(err, Unknown) {
		t.Fatalf("BIND schema enrollment accepted: %v", err)
	}
	f.record.Schema = SchemaV1
	f.record.PeerIP = "192.0.2.12"
	writeRecord(t, f.recordPath, f.record)
	if err := recheckAt(f.root, snapshot); !IsCode(err, Changed) {
		t.Fatalf("changed peer accepted by recheck: %v", err)
	}
}
