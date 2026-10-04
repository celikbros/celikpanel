//go:build linux

package mailhoststore

import (
	"bytes"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func storeFixture(t *testing.T) (string, string, int, PairVerifier) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned native evidence")
	}
	root := t.TempDir()
	version := ".panel-cert-" + strings.Repeat("3", 32)
	dir := filepath.Join(root, version)
	if err := os.Mkdir(dir, 0750); err != nil {
		t.Fatal(err)
	}
	leaf := []byte("historical leaf DER fixture")
	receipt, err := os.ReadFile("../mailhostartifact/testdata/alpha81-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{mailhostartifact.ReceiptName: receipt, "mail.domain": []byte("mail.example.test\n"), "fullchain.pem": []byte("fixture cert"), "privkey.pem": []byte("fixture key")} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(version, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	verify := func(cert, key []byte, domain string) ([]byte, time.Time, error) {
		if !bytes.Equal(cert, []byte("fixture cert")) || !bytes.Equal(key, []byte("fixture key")) || domain != "mail.example.test" {
			t.Fatal("wrong verification input")
		}
		return leaf, time.Unix(2000000000, 0), nil
	}
	return root, version, fd, verify
}
func TestHistoricalStoreReadIsReadOnly(t *testing.T) {
	root, version, fd, verify := storeFixture(t)
	receiptPath := filepath.Join(root, version, mailhostartifact.ReceiptName)
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	got, r, leaf, _, found, err := ReadCurrentAt(fd, verify)
	if err != nil || !found || got != version || r.LeafSHA256 != mailhostartifact.LeafSHA256(leaf) {
		t.Fatalf("read: %s %+v %v %v", got, r, found, err)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reader changed evidence")
	}
}
func TestStoreCorruptionNeverBecomesAbsence(t *testing.T) {
	cases := map[string]func(string, string) error{
		"directory-owner":    func(r, v string) error { return os.Chown(filepath.Join(r, v), 12345, 0) },
		"directory-writable": func(r, v string) error { return os.Chmod(filepath.Join(r, v), 0770) },
		"missing-receipt":    func(r, v string) error { return os.Remove(filepath.Join(r, v, mailhostartifact.ReceiptName)) },
		"public-key":         func(r, v string) error { return os.Chmod(filepath.Join(r, v, "privkey.pem"), 0644) },
		"receipt-group":      func(r, v string) error { return os.Chown(filepath.Join(r, v, mailhostartifact.ReceiptName), 0, 12345) },
		"owner":              func(r, v string) error { return os.Chown(filepath.Join(r, v, "privkey.pem"), 12345, 0) },
		"hardlink": func(r, v string) error {
			return os.Link(filepath.Join(r, v, "privkey.pem"), filepath.Join(r, "extra-link"))
		},
		"key-symlink": func(r, v string) error {
			p := filepath.Join(r, v, "privkey.pem")
			if e := os.Rename(p, filepath.Join(r, "moved-key")); e != nil {
				return e
			}
			return os.Symlink("../moved-key", p)
		},
		"fifo": func(r, v string) error {
			p := filepath.Join(r, v, "privkey.pem")
			if e := os.Remove(p); e != nil {
				return e
			}
			return unix.Mkfifo(p, 0600)
		},
		"domain": func(r, v string) error {
			return os.WriteFile(filepath.Join(r, v, "mail.domain"), []byte("other.example.test\n"), 0600)
		},
		"outside": func(r, v string) error {
			p := filepath.Join(r, "current")
			if e := os.Remove(p); e != nil {
				return e
			}
			return os.Symlink("../"+v, p)
		},
		"version-symlink": func(r, v string) error {
			p := filepath.Join(r, v)
			if e := os.Rename(p, p+"-moved"); e != nil {
				return e
			}
			return os.Symlink(v+"-moved", p)
		},
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			r, v, fd, verify := storeFixture(t)
			if e := alter(r, v); e != nil {
				t.Fatal(e)
			}
			if _, _, _, _, found, err := ReadCurrentAt(fd, verify); err == nil || found {
				t.Fatalf("corruption accepted/absent: %v %v", found, err)
			}
		})
	}
}
func TestAbsentCurrentIsDistinctFromPresentCorruption(t *testing.T) {
	r, _, fd, _ := storeFixture(t)
	if e := os.Remove(filepath.Join(r, "current")); e != nil {
		t.Fatal(e)
	}
	if _, _, _, _, found, err := ReadCurrentAt(fd, nil); err != nil || found {
		t.Fatalf("absence: %v %v", found, err)
	}
}
func TestCurrentReplacedDuringVerificationIsUnknown(t *testing.T) {
	root, v, fd, verify := storeFixture(t)
	race := func(c, k []byte, d string) ([]byte, time.Time, error) {
		next := filepath.Join(root, "replacement")
		if e := os.Symlink(v, next); e != nil {
			t.Fatal(e)
		}
		if e := os.Rename(next, filepath.Join(root, "current")); e != nil {
			t.Fatal(e)
		}
		return verify(c, k, d)
	}
	if _, _, _, _, found, err := ReadCurrentAt(fd, race); err == nil || found {
		t.Fatalf("changed owner selection accepted: %v %v", found, err)
	}
	// Owner's new link is retained, not undone by this read failure.
	if got, e := os.Readlink(filepath.Join(root, "current")); e != nil || got != v {
		t.Fatal("owner selection altered")
	}
}

func TestReceiptCannotAuthorizeDifferentLeaf(t *testing.T) {
	_, _, fd, _ := storeFixture(t)
	verify := func([]byte, []byte, string) ([]byte, time.Time, error) {
		return []byte("different leaf"), time.Now(), nil
	}
	if _, _, _, _, found, err := ReadCurrentAt(fd, verify); err == nil || found {
		t.Fatalf("receipt mismatch accepted: %v %v", found, err)
	}
	if _, _, _, _, found, err := ReadCurrentAt(fd, nil); err == nil || found {
		t.Fatalf("missing verifier accepted: %v %v", found, err)
	}
}
