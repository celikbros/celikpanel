//go:build linux

package dnsenginerecovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbePDNSDatabasePreimageRequiresExactBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	data := []byte("frozen SQLite database bytes")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if err := ProbePDNSDatabasePreimage(context.Background(), path, int64(len(data)), digest); err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSDatabasePreimage(context.Background(), path, int64(len(data))+1, digest); err == nil {
		t.Fatal("accepted a different frozen size")
	}
	if err := ProbePDNSDatabasePreimage(context.Background(), path, int64(len(data)), strings.Repeat("a", 64)); err == nil {
		t.Fatal("accepted a different frozen digest")
	}
	if err := os.WriteFile(path, []byte("changed SQLite database bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSDatabasePreimage(context.Background(), path, int64(len(data)), digest); err == nil {
		t.Fatal("accepted changed bytes")
	}
}

func TestProbePDNSDatabasePreimageRejectsSymlinksAndCancelledContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pdns.sqlite3")
	data := []byte("database")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	link := filepath.Join(dir, "link.sqlite3")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSDatabasePreimage(context.Background(), link, int64(len(data)), digest); err == nil {
		t.Fatal("accepted a symlinked database")
	}
	parentLink := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSDatabasePreimage(context.Background(), filepath.Join(parentLink, "pdns.sqlite3"), int64(len(data)), digest); err == nil {
		t.Fatal("accepted a symlinked parent")
	}
	hardlink := filepath.Join(dir, "hardlink.sqlite3")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSDatabasePreimage(context.Background(), path, int64(len(data)), digest); err == nil {
		t.Fatal("accepted a multiply linked database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ProbePDNSDatabasePreimage(ctx, path, int64(len(data)), digest); err == nil {
		t.Fatal("accepted a cancelled observation")
	}
}
func TestProbePDNSDatabasePreimageRejectsPathReplacementAfterRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pdns.sqlite3")
	data := []byte("same database bytes")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	replacement := filepath.Join(dir, "replacement.sqlite3")
	if err := os.WriteFile(replacement, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var renameErr error
	err := probePDNSDatabasePreimage(context.Background(), path, int64(len(data)), digest, func() {
		renameErr = os.Rename(replacement, path)
	})
	if renameErr != nil {
		t.Fatal(renameErr)
	}
	if err == nil {
		t.Fatal("accepted replacement with identical database bytes")
	}
}
func TestInspectPDNSDatabaseFilePresenceAndDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	exists, size, digest, err := InspectPDNSDatabaseFile(context.Background(), path, true)
	if err != nil || exists || size != 0 || digest != "" {
		t.Fatalf("absent database: exists=%t size=%d digest=%q err=%v", exists, size, digest, err)
	}
	if _, _, _, err := InspectPDNSDatabaseFile(context.Background(), path, false); err == nil {
		t.Fatal("accepted an absent required database")
	}
	data := []byte("exact native database")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	exists, size, digest, err = InspectPDNSDatabaseFile(context.Background(), path, false)
	if err != nil || !exists || size != int64(len(data)) || digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("present database: exists=%t size=%d digest=%q err=%v", exists, size, digest, err)
	}
}
