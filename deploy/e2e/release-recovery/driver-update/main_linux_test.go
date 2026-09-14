//go:build linux

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func signedTarget(t *testing.T) ([]byte, []byte, []byte, manifest) {
	t.Helper()
	target := manifest{
		Sequence: "79", Version: "v0.1.0-alpha.79", Commit: strings.Repeat("a", 40),
		PublishedAt: "2026-09-14T00:00:00Z", OS: "linux", Arch: "amd64",
		Archive: "celikpanel-v0.1.0-alpha.79-linux-amd64.tar.gz", ArchiveSHA256: strings.Repeat("b", 64), ArchiveSize: "123456",
	}
	raw := []byte(fmt.Sprintf("format=celikpanel-release-manifest-v2\nsequence=%s\nversion=%s\ncommit=%s\npublished_at=%s\nos=%s\narch=%s\narchive=%s\narchive_sha256=%s\narchive_size=%s\n", target.Sequence, target.Version, target.Commit, target.PublishedAt, target.OS, target.Arch, target.Archive, target.ArchiveSHA256, target.ArchiveSize))
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: keyDER})
	return raw, ed25519.Sign(privateKey, raw), keyPEM, target
}

func TestPublishedManifestRequiresExactSignatureAndPlatform(t *testing.T) {
	raw, signature, key, target := signedTarget(t)
	actual, err := verifiedManifest(raw, signature, key, "amd64")
	if err != nil || actual != target {
		t.Fatalf("valid manifest: %+v %v", actual, err)
	}
	tampered := append([]byte{}, raw...)
	tampered[0] ^= 1
	for _, test := range []struct {
		name                string
		raw, signature, key []byte
		arch                string
	}{
		{"tampered", tampered, signature, key, "amd64"},
		{"short-signature", raw, signature[:63], key, "amd64"},
		{"other-platform", raw, signature, key, "arm64"},
		{"extra-key", raw, signature, append(append([]byte{}, key...), key...), "amd64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := verifiedManifest(test.raw, test.signature, test.key, test.arch); err == nil {
				t.Fatal("unverified target accepted")
			}
		})
	}
}

func TestCanonicalManifestRejectsUnsupportedVersionsAndIntegerAliases(t *testing.T) {
	raw, _, _, _ := signedTarget(t)
	for _, altered := range []string{
		strings.ReplaceAll(string(raw), "v0.1.0-alpha.79", "v0.1.0-alpha.81"),
		strings.Replace(string(raw), "sequence=79", "sequence=079", 1),
		strings.Replace(string(raw), "archive_size=123456", "archive_size=2147483649", 1),
		strings.ReplaceAll(string(raw), "\n", "\r\n"),
	} {
		if _, err := parseManifest([]byte(altered), "amd64"); err == nil {
			t.Fatal("unsupported/noncanonical manifest accepted")
		}
	}
}

func TestExactHistoricalTargetUsesCurrentBuildAndExplicitRequestID(t *testing.T) {
	_, _, _, target := signedTarget(t)
	marker := markerIdentity{CellID: "release-recovery__abcd", Node: "debian13", Nonce: strings.Repeat("c", 64)}
	current := transport.SystemUpdateCheckResponse{
		Supported: true, Available: true, CurrentVersion: "v0.1.0-alpha.75", CurrentCommit: strings.Repeat("d", 40),
		TargetVersion: "v0.1.0-alpha.80",
	}
	requestID := strings.Repeat("e", 32)
	request, err := requestFor(marker, target, current, requestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.RequestID != requestID || request.TargetVersion != "v0.1.0-alpha.79" ||
		request.ExpectedCurrentVersion != current.CurrentVersion || request.ExpectedCurrentCommit != current.CurrentCommit ||
		request.TargetArchiveSHA256 != target.ArchiveSHA256 {
		t.Fatalf("review lost exact identity: %+v", request)
	}
	current.Error = "cannot verify installed release floor"
	if _, err := requestFor(marker, target, current, requestID); err == nil {
		t.Fatal("unverified current state accepted")
	}
}

func TestStartTransportFailureDoesNotRetryOrLaunchAnotherPath(t *testing.T) {
	calls := 0
	call := func(_ context.Context, method string, _, _ any) error {
		calls++
		if method != "Agent.StartSystemUpdate" {
			t.Fatalf("alternate mutation path used: %s", method)
		}
		return errors.New("lost connection")
	}
	_, err := reviewedStart(context.Background(), call, transport.SystemUpdateStartRequest{RequestID: strings.Repeat("a", 32)})
	if err == nil || calls != 1 {
		t.Fatalf("uncertain Start retried or reported accepted: calls=%d err=%v", calls, err)
	}
}

func TestStatusRequiresFullTargetIdentityAndTreatsAbsenceAsUnconfirmed(t *testing.T) {
	_, _, _, target := signedTarget(t)
	requestID := strings.Repeat("a", 32)
	for _, scenario := range []string{"valid", "wrong-hash", "absent"} {
		call := func(_ context.Context, method string, request, output any) error {
			if method != "Agent.SystemUpdateStatus" || request.(*transport.SystemUpdateStatusRequest).RequestID != requestID {
				t.Fatal("wrong status call")
			}
			response := output.(*transport.SystemUpdateStatusResponse)
			*response = transport.SystemUpdateStatusResponse{Found: true, RequestID: requestID, Status: "failed", TargetVersion: target.Version, TargetCommit: target.Commit, TargetSequence: target.Sequence, TargetOS: target.OS, TargetArch: target.Arch, TargetArchiveSHA256: target.ArchiveSHA256, TargetArchiveSize: target.ArchiveSize}
			if scenario == "wrong-hash" {
				response.TargetArchiveSHA256 = strings.Repeat("f", 64)
			}
			if scenario == "absent" {
				response.Found = false
			}
			return nil
		}
		_, err := inspect(context.Background(), call, requestID, target)
		if (scenario == "valid") != (err == nil) {
			t.Fatalf("%s: %v", scenario, err)
		}
	}
}

func TestGuestIdentityCannotCrossNonceOrVM(t *testing.T) {
	nonce := strings.Repeat("a", 64)
	uuid := "d24e58ea-3de4-49aa-a151-2beb4c32a29e"
	raw := []byte(fmt.Sprintf("{\"schema\":%q,\"nonce\":%q,\"vm_uuid\":%q,\"cell_id\":\"release-recovery__abcd\",\"node\":\"debian13\"}", labSchema, nonce, uuid))
	if _, err := parseMarker(raw, nonce, uuid, "QEMU"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseMarker(raw, strings.Repeat("b", 64), uuid, "QEMU"); err == nil {
		t.Fatal("wrong nonce accepted")
	}
	if _, err := parseMarker(raw, nonce, "00000000-0000-0000-0000-000000000000", "QEMU"); err == nil {
		t.Fatal("wrong VM accepted")
	}
	if _, err := parseMarker(raw, nonce, uuid, "Dell"); err == nil {
		t.Fatal("non-QEMU host accepted")
	}
}
