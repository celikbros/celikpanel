//go:build linux

package mailhoststore

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"golang.org/x/sys/unix"
)

func publicationFixture(t *testing.T) (string, int, []byte, []byte, mailhostartifact.Receipt, PairVerifier) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("native root metadata required")
	}
	root := t.TempDir()
	if e := os.Chown(root, 0, 0); e != nil {
		t.Fatal(e)
	}
	fd, e := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { unix.Close(fd) })
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mail.example.test"}, DNSNames: []string{"mail.example.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour * 48), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalECPrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	private := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	receipt, e := mailhostartifact.NewReceipt(strings.Repeat("a", 32), "mhc1:"+strings.Repeat("b", 64), "mail.example.test", der)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	verify := func(c, k []byte, d string) ([]byte, time.Time, error) {
		return mailhostartifact.VerifyRetainedPair(c, k, d, roots, time.Now())
	}
	return root, fd, cert, private, receipt, verify
}

func onlyStage(t *testing.T, root string) string {
	t.Helper()
	entries, e := os.ReadDir(root)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), versionPrefix) {
			return entry.Name()
		}
	}
	t.Fatal("stage missing")
	return ""
}

func TestProducerPublishesArtifactItsActualReaderAccepts(t *testing.T) {
	root, fd, cert, key, receipt, verify := publicationFixture(t)
	stage, e := StageMaterialAt(fd, cert, key, receipt)
	if e != nil {
		t.Fatal(e)
	}
	// The stage must own copies rather than caller buffers.
	cert[0] = '!'
	key[0] = '!'
	if _, _, _, _, found, e := ReadCurrentAt(fd, verify); e != nil || found {
		t.Fatal("staging selected a generation")
	}
	published, e := stage.Publish()
	if e != nil || !published {
		t.Fatalf("publish: %v %v", published, e)
	}
	version, got, leaf, _, found, e := ReadCurrentAt(fd, verify)
	if e != nil || !found || got != receipt || mailhostartifact.LeafSHA256(leaf) != receipt.LeafSHA256 {
		t.Fatalf("producer/reader disagree: %v", e)
	}
	if e = stage.Close(); e != nil {
		t.Fatal(e)
	}
	if e = stage.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, version, "privkey.pem")); e != nil {
		t.Fatal("published key removed")
	}
	if _, e = stage.Publish(); e == nil {
		t.Fatal("closed stage published again")
	}
}

func TestUnpublishedCleanupAndCurrentSelectionPreservation(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpublished", true: "owner-selected"}[selected], func(t *testing.T) {
			root, fd, cert, key, receipt, _ := publicationFixture(t)
			stage, e := StageMaterialAt(fd, cert, key, receipt)
			if e != nil {
				t.Fatal(e)
			}
			version := onlyStage(t, root)
			if selected {
				if e = os.Symlink(version, filepath.Join(root, "current")); e != nil {
					t.Fatal(e)
				}
			}
			if e = stage.Close(); e != nil {
				t.Fatal(e)
			}
			_, e = os.Stat(filepath.Join(root, version))
			if selected && e != nil {
				t.Fatal("current material deleted")
			}
			if !selected && !os.IsNotExist(e) {
				t.Fatal("abandoned exact stage retained")
			}
		})
	}
}

func TestOwnerChangesPreventPublicationAndDestructiveCleanup(t *testing.T) {
	for _, change := range []string{"key", "extra-file", "mode", "version-replacement"} {
		t.Run(change, func(t *testing.T) {
			root, fd, cert, key, receipt, _ := publicationFixture(t)
			stage, e := StageMaterialAt(fd, cert, key, receipt)
			if e != nil {
				t.Fatal(e)
			}
			version := onlyStage(t, root)
			dir := filepath.Join(root, version)
			switch change {
			case "key":
				e = os.WriteFile(filepath.Join(dir, "privkey.pem"), []byte("owner changed material"), 0600)
			case "extra-file":
				e = os.WriteFile(filepath.Join(dir, "owner-note"), []byte("keep"), 0600)
			case "mode":
				e = os.Chmod(filepath.Join(dir, "privkey.pem"), 0640)
			case "version-replacement":
				e = os.Rename(dir, dir+"-retained")
				if e == nil {
					e = os.Mkdir(dir, 0750)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if published, e := stage.Publish(); e == nil || published {
				t.Fatal("changed material published")
			}
			if e = stage.Close(); e == nil {
				t.Fatal("changed material cleanup accepted")
			}
			if _, e = os.Stat(dir); e != nil {
				t.Fatal("owner directory removed")
			}
			if change == "key" {
				raw, e := os.ReadFile(filepath.Join(dir, "privkey.pem"))
				if e != nil || string(raw) != "owner changed material" {
					t.Fatal("owner key changed")
				}
			}
		})
	}
}

func TestLaterOwnerSelectionIsNotOverwritten(t *testing.T) {
	root, fd, cert, key, receipt, _ := publicationFixture(t)
	first, e := StageMaterialAt(fd, cert, key, receipt)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = first.Publish(); e != nil {
		t.Fatal(e)
	}
	if e = first.Close(); e != nil {
		t.Fatal(e)
	}
	initial, e := os.Readlink(filepath.Join(root, "current"))
	if e != nil {
		t.Fatal(e)
	}
	next, e := StageMaterialAt(fd, cert, key, receipt)
	if e != nil {
		t.Fatal(e)
	}
	// Even a replacement symlink selecting the same name is a later owner edit.
	if e = os.Symlink(initial, filepath.Join(root, "owner-selection")); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(filepath.Join(root, "owner-selection"), filepath.Join(root, "current")); e != nil {
		t.Fatal(e)
	}
	if changed, e := next.Publish(); changed || e == nil {
		t.Fatal("owner selection overwritten")
	}
	if e = next.Close(); e != nil {
		t.Fatal(e)
	}
	if current, e := os.Readlink(filepath.Join(root, "current")); e != nil || current != initial {
		t.Fatal("owner selection lost")
	}
}

func TestInvalidMaterialAndParentAreZeroTouch(t *testing.T) {
	root, fd, cert, key, receipt, _ := publicationFixture(t)
	receipt.LeafSHA256 = strings.Repeat("0", 64)
	if _, e := StageMaterialAt(fd, cert, key, receipt); e == nil {
		t.Fatal("mismatched receipt accepted")
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("invalid stage touched directory")
	}
	if _, e = ValidateMaterial(bytes.Repeat([]byte("x"), PEMMaxSize+1), key, receipt); e == nil {
		t.Fatal("oversize material accepted")
	}
}

func TestPublishedDespiteSyncFailureIsRetained(t *testing.T) {
	sentinel := errors.New("fsync failed")
	cleaned := false
	stage := &Stage{publishAction: func() (bool, error) { return true, sentinel }, cleanupAction: func(published bool) error {
		if !published {
			t.Fatal("publication uncertainty lost")
		}
		cleaned = true
		return nil
	}}
	if published, e := stage.Publish(); !published || !errors.Is(e, sentinel) {
		t.Fatal("published failure lost")
	}
	if e := stage.Close(); e != nil || !cleaned {
		t.Fatal("cleanup failed")
	}
}

func TestUnsafePublicationParentIsNotNormalized(t *testing.T) {
	for _, change := range []string{"owner", "group", "writable"} {
		t.Run(change, func(t *testing.T) {
			root, fd, cert, key, receipt, _ := publicationFixture(t)
			var e error
			switch change {
			case "owner":
				e = os.Chown(root, 12345, 0)
			case "group":
				e = os.Chown(root, 0, 12345)
			case "writable":
				e = os.Chmod(root, 0770)
			}
			if e != nil {
				t.Fatal(e)
			}
			var before, after unix.Stat_t
			if e = unix.Fstat(fd, &before); e != nil {
				t.Fatal(e)
			}
			if _, e = StageMaterialAt(fd, cert, key, receipt); e == nil {
				t.Fatal("unsafe parent accepted")
			}
			if e = unix.Fstat(fd, &after); e != nil {
				t.Fatal(e)
			}
			if !sameEvidenceStat(before, after) {
				t.Fatal("parent metadata normalized")
			}
			entries, e := os.ReadDir(root)
			if e != nil || len(entries) != 0 {
				t.Fatal("unsafe parent mutated")
			}
		})
	}
}
