//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTestDKIMBaseDir(t *testing.T) string {
	t.Helper()
	previous := dkimBaseDir
	dkimBaseDir = filepath.Join(t.TempDir(), "dkim")
	t.Cleanup(func() {
		dkimBaseDir = previous
	})
	return dkimBaseDir
}

func TestEnsureDKIMKeyIsIdempotentAndPrivate(t *testing.T) {
	base := withTestDKIMBaseDir(t)
	agent := &Agent{}
	req := &DKIMEnsureRequest{Domain: "example.com", Selector: signingSelector}

	var first DKIMEnsureResponse
	if err := agent.EnsureDKIMKey(req, &first); err != nil {
		t.Fatal(err)
	}
	if first.Error != "" || !first.Created || first.PublicKeyB64 == "" {
		t.Fatalf("unexpected first response: %+v", first)
	}
	path := filepath.Join(base, "example.com", signingSelector+".private")
	firstBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("private key mode = %#o, want 0600", got)
	}

	var second DKIMEnsureResponse
	if err := agent.EnsureDKIMKey(req, &second); err != nil {
		t.Fatal(err)
	}
	if second.Error != "" || second.Created {
		t.Fatalf("unexpected second response: %+v", second)
	}
	if second.PublicKeyB64 != first.PublicKeyB64 {
		t.Fatal("idempotent call returned a different public key")
	}
	secondBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("idempotent call replaced the private key")
	}
}

func TestEnsureDKIMKeyRefusesSymlinkWithoutTouchingTarget(t *testing.T) {
	base := withTestDKIMBaseDir(t)
	domainDir := filepath.Join(base, "example.com")
	if err := os.MkdirAll(domainDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside-key")
	want := []byte("do-not-overwrite")
	if err := os.WriteFile(target, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(domainDir, signingSelector+".private")); err != nil {
		t.Fatal(err)
	}

	var resp DKIMEnsureResponse
	if err := (&Agent{}).EnsureDKIMKey(
		&DKIMEnsureRequest{Domain: "example.com", Selector: signingSelector},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" {
		t.Fatal("symlink key was accepted")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("symlink target changed: got %q want %q", got, want)
	}
}

func TestEnsureDKIMKeyDoesNotReplaceMalformedExistingKey(t *testing.T) {
	base := withTestDKIMBaseDir(t)
	path := filepath.Join(base, "example.com", signingSelector+".private")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("malformed-but-preserved")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	var resp DKIMEnsureResponse
	if err := (&Agent{}).EnsureDKIMKey(
		&DKIMEnsureRequest{Domain: "example.com", Selector: signingSelector},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "cannot read existing DKIM key") {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("malformed key was replaced: got %q want %q", got, want)
	}
}

func TestGetDKIMStatusReportsMalformedKey(t *testing.T) {
	base := withTestDKIMBaseDir(t)
	path := filepath.Join(base, "example.com", signingSelector+".private")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}

	var resp DKIMStatusResponse
	if err := (&Agent{}).GetDKIMStatus(
		&DKIMStatusRequest{Domain: "example.com", Selector: signingSelector},
		&resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" || resp.HasKey {
		t.Fatalf("malformed key was reported as a normal state: %+v", resp)
	}
}

func TestDKIMRPCRejectsNilRequests(t *testing.T) {
	var status DKIMStatusResponse
	if err := (&Agent{}).GetDKIMStatus(nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Error == "" {
		t.Fatal("nil status request was accepted")
	}
	var ensure DKIMEnsureResponse
	if err := (&Agent{}).EnsureDKIMKey(nil, &ensure); err != nil {
		t.Fatal(err)
	}
	if ensure.Error == "" {
		t.Fatal("nil ensure request was accepted")
	}
}

func TestSecureSetMailDirectoryMetadataRefusesSymlink(t *testing.T) {
	realDir := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	if err := secureSetMailDirectoryMetadata(link, 0o750, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("directory symlink was accepted")
	}
}
