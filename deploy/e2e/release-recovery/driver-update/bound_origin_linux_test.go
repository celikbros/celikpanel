//go:build linux

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func boundOriginFixture(t *testing.T) (map[string]any, markerIdentity, manifest, []byte, []byte, []byte) {
	t.Helper()
	target := manifest{Sequence: "82", Version: boundWorkerTargetVersion, Commit: strings.Repeat("a", 40), PublishedAt: "2026-09-21T00:00:00Z",
		OS: "linux", Arch: "amd64", Archive: "celikpanel-v0.1.0-alpha.82-linux-amd64.tar.gz", ArchiveSHA256: strings.Repeat("b", 64), ArchiveSize: "123456"}
	fields := map[string]string{"format": "celikpanel-release-manifest-v2", "sequence": target.Sequence, "version": target.Version,
		"commit": target.Commit, "published_at": target.PublishedAt, "os": target.OS, "arch": target.Arch,
		"archive": target.Archive, "archive_sha256": target.ArchiveSHA256, "archive_size": target.ArchiveSize}
	raw := []byte(fmt.Sprintf("format=celikpanel-release-manifest-v2\nsequence=%s\nversion=%s\ncommit=%s\npublished_at=%s\nos=%s\narch=%s\narchive=%s\narchive_sha256=%s\narchive_size=%s\n", target.Sequence, target.Version, target.Commit, target.PublishedAt, target.OS, target.Arch, target.Archive, target.ArchiveSHA256, target.ArchiveSize))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	key := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	signature := ed25519.Sign(priv, raw)
	marker := markerIdentity{CellID: "release-recovery__abcd", Node: "debian13", Nonce: strings.Repeat("c", 64), VMUUID: "11111111-1111-4111-8111-111111111111"}
	files := map[string]any{}
	for _, name := range []string{"worker-origin-ca.pem", "worker-origin-tls.pem", "worker-origin-tls-key.pem"} {
		files[name] = map[string]any{"sha256": strings.Repeat("d", 64), "size": 1}
	}
	for name, bytes := range map[string][]byte{"worker-origin-manifest": raw, "worker-origin-signature": signature, "worker-origin-public.pem": key, "worker-origin-latest": []byte(target.Version + "\n"), "worker-origin-archive.sha256": []byte(target.ArchiveSHA256 + "  " + target.Archive + "\n")} {
		sum := sha256.Sum256(bytes)
		files[name] = map[string]any{"sha256": hex.EncodeToString(sum[:]), "size": len(bytes)}
	}
	size, _ := strconv.Atoi(target.ArchiveSize)
	files["worker-origin-archive.tar.gz"] = map[string]any{"sha256": target.ArchiveSHA256, "size": size}
	base := "/releases/" + target.Version + "/linux/amd64/"
	policy := map[string]any{"format": "celikpanel-release-sequence-policy-v1", "version": target.Version, "current": 82, "previous": 81, "previous_version": "v0.1.0-alpha.81", "previous_commit": strings.Repeat("d", 40)}
	policyRaw := "format=celikpanel-release-sequence-policy-v1\nversion=" + target.Version + "\ncurrent=82\nprevious=81\nprevious_version=v0.1.0-alpha.81\nprevious_commit=" + strings.Repeat("d", 40) + "\n"
	policySHA := sha256.Sum256([]byte(policyRaw))
	policy["sha256"] = hex.EncodeToString(policySHA[:])
	origin := map[string]any{"schema": "celikpanel/worker-fixture-origin/v1", "provenance": "unpublished-local-artifact-with-disposable-fixture-trust",
		"identity": map[string]string{"schema": labSchema, "nonce": marker.Nonce, "cell_id": marker.CellID, "node": marker.Node, "vm_uuid": marker.VMUUID},
		"target":   fields, "source_proof": map[string]any{"commit": target.Commit, "tree": strings.Repeat("e", 40), "verified_static_files": 5, "method": "real Git commit/tree and every packaged static source blob", "release_policy": policy},
		"routes": map[string]string{"/releases/latest.txt": "worker-origin-latest", base + "release-manifest-v2": "worker-origin-manifest", base + "release-manifest-v2.sig": "worker-origin-signature", base + target.Archive: "worker-origin-archive.tar.gz", base + target.Archive + ".sha256": "worker-origin-archive.sha256"}, "files": files}
	return origin, marker, target, raw, signature, key
}

func TestCurrentProducerManifestRequiresSignatureAndExactOrigin(t *testing.T) {
	origin, marker, target, raw, signature, key := boundOriginFixture(t)
	actual, err := verifiedManifest(raw, signature, key, "amd64")
	if err != nil || actual != target {
		t.Fatalf("fixture manifest: %+v %v", actual, err)
	}
	receipt, _ := json.Marshal(origin)
	if err := validateBoundWorkerOrigin(receipt, marker, target, raw, signature, key); err != nil {
		t.Fatal(err)
	}
	for _, modified := range []string{strings.Replace(string(raw), "sequence=82", "sequence=83", 1), strings.ReplaceAll(string(raw), "alpha.82", "alpha.83")} {
		if _, err := parseManifest([]byte(modified), "amd64"); err == nil {
			t.Fatal("unreviewed fixture release accepted")
		}
	}
	damaged := append([]byte(nil), signature...)
	damaged[0] ^= 1
	if _, err := verifiedManifest(raw, damaged, key, "amd64"); err == nil {
		t.Fatal("wrong signature accepted")
	}
	for _, component := range []string{"signature", "key", "manifest"} {
		m, s, k := raw, signature, key
		switch component {
		case "signature":
			s = damaged
		case "key":
			k = append(append([]byte(nil), key...), '\n')
		case "manifest":
			m = append(append([]byte(nil), raw...), '\n')
		}
		if err := validateBoundWorkerOrigin(receipt, marker, target, m, s, k); err == nil {
			t.Fatalf("changed %s accepted", component)
		}
	}
}

func TestCurrentProducerOriginRejectsCrossGuestAndProvenanceChanges(t *testing.T) {
	for _, scenario := range []string{"other-vm", "other-nonce", "other-key", "other-archive-size", "other-checksum-digest", "other-checksum-size", "missing-checksum", "other-commit", "unpublished-claim-missing", "route", "unknown-field", "extra-json"} {
		t.Run(scenario, func(t *testing.T) {
			origin, marker, target, raw, signature, key := boundOriginFixture(t)
			switch scenario {
			case "other-vm":
				marker.VMUUID = "22222222-2222-4222-8222-222222222222"
			case "other-nonce":
				marker.Nonce = strings.Repeat("e", 64)
			case "other-key":
				origin["files"].(map[string]any)["worker-origin-public.pem"].(map[string]any)["sha256"] = strings.Repeat("f", 64)
			case "other-archive-size":
				origin["files"].(map[string]any)["worker-origin-archive.tar.gz"].(map[string]any)["size"] = 42
			case "other-checksum-digest":
				origin["files"].(map[string]any)["worker-origin-archive.sha256"].(map[string]any)["sha256"] = strings.Repeat("f", 64)
			case "other-checksum-size":
				origin["files"].(map[string]any)["worker-origin-archive.sha256"].(map[string]any)["size"] = 42
			case "missing-checksum":
				delete(origin["files"].(map[string]any), "worker-origin-archive.sha256")
			case "other-commit":
				origin["source_proof"].(map[string]any)["commit"] = strings.Repeat("f", 40)
			case "unpublished-claim-missing":
				origin["provenance"] = "production-signed"
			case "route":
				origin["routes"].(map[string]string)["/key"] = "worker-origin-signing.pem"
			case "unknown-field":
				origin["unknown"] = true
			}
			receipt, _ := json.Marshal(origin)
			if scenario == "extra-json" {
				receipt = append(receipt, []byte(" {}")...)
			}
			if err := validateBoundWorkerOrigin(receipt, marker, target, raw, signature, key); err == nil {
				t.Fatal("changed origin admitted")
			}
		})
	}
}

func TestCurrentProducerRequestRequiresExactAvailableTransition(t *testing.T) {
	_, marker, target, _, _, _ := boundOriginFixture(t)
	current := transport.SystemUpdateCheckResponse{Supported: true, Available: true, CurrentVersion: "v0.1.0-alpha.81", CurrentCommit: strings.Repeat("d", 40),
		TargetVersion: target.Version, TargetCommit: target.Commit, TargetSequence: target.Sequence, TargetOS: target.OS, TargetArch: target.Arch,
		TargetArchiveSHA256: target.ArchiveSHA256, TargetArchiveSize: target.ArchiveSize, PublishedAt: target.PublishedAt}
	if _, err := requestFor(marker, target, current, strings.Repeat("f", 32)); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"old-baseline", "same-commit", "unavailable", "archive", "size", "latest"} {
		changed := current
		switch scenario {
		case "old-baseline":
			changed.CurrentVersion = "v0.1.0-alpha.75"
		case "same-commit":
			changed.CurrentCommit = target.Commit
		case "unavailable":
			changed.Available = false
		case "archive":
			changed.TargetArchiveSHA256 = strings.Repeat("e", 64)
		case "size":
			changed.TargetArchiveSize = "42"
		case "latest":
			changed.TargetVersion = "v0.1.0-alpha.83"
		}
		if _, err := requestFor(marker, target, changed, strings.Repeat("f", 32)); err == nil {
			t.Fatalf("%s admitted", scenario)
		}
	}
}

func TestCurrentProducerPolicyRefusesMislabeledArchiveAndWrongInstalledPredecessor(t *testing.T) {
	for _, field := range []string{"version", "current", "previous", "previous_version", "previous_commit", "sha256"} {
		t.Run(field, func(t *testing.T) {
			origin, marker, target, raw, sig, key := boundOriginFixture(t)
			policy := origin["source_proof"].(map[string]any)["release_policy"].(map[string]any)
			switch field {
			case "version", "previous_version":
				policy[field] = "v0.1.0-alpha.80"
			case "current", "previous":
				policy[field] = 80
			case "previous_commit":
				policy[field] = target.Commit
			case "sha256":
				policy[field] = strings.Repeat("f", 64)
			}
			receipt, _ := json.Marshal(origin)
			if validateBoundWorkerOrigin(receipt, marker, target, raw, sig, key) == nil {
				t.Fatal("mislabeled release policy admitted")
			}
		})
	}
	origin, _, _, _, _, _ := boundOriginFixture(t)
	receipt, _ := json.Marshal(origin)
	if err := validateBoundWorkerPredecessor(receipt, strings.Repeat("d", 40)); err != nil {
		t.Fatal(err)
	}
	for _, commit := range []string{strings.Repeat("a", 40), strings.Repeat("e", 40), "unknown"} {
		if validateBoundWorkerPredecessor(receipt, commit) == nil {
			t.Fatal("unrelated installed predecessor admitted")
		}
	}
}
