//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"

	"github.com/alicelik/celikpanel/internal/transport"
)

const boundWorkerTargetVersion = "v0.1.0-alpha.82"

type boundWorkerReleasePolicy struct {
	Format          string `json:"format"`
	Version         string `json:"version"`
	Current         int64  `json:"current"`
	Previous        int64  `json:"previous"`
	PreviousVersion string `json:"previous_version"`
	PreviousCommit  string `json:"previous_commit"`
	SHA256          string `json:"sha256"`
}

func (p boundWorkerReleasePolicy) validate(target manifest) error {
	raw := "format=" + p.Format + "\nversion=" + p.Version + "\ncurrent=" + strconv.FormatInt(p.Current, 10) + "\nprevious=" + strconv.FormatInt(p.Previous, 10) + "\nprevious_version=" + p.PreviousVersion + "\nprevious_commit=" + p.PreviousCommit + "\n"
	sha := sha256.Sum256([]byte(raw))
	if p.Format != "celikpanel-release-sequence-policy-v1" || p.Version != target.Version || p.Current != 82 || target.Sequence != "82" || p.Previous != 81 || p.PreviousVersion != "v0.1.0-alpha.81" || !hex40.MatchString(p.PreviousCommit) || p.PreviousCommit == target.Commit || p.SHA256 != hex.EncodeToString(sha[:]) {
		return errors.New("current-producer packaged release policy differs from the signed target transition")
	}
	return nil
}

// Both reads use the same protected origin bytes already admitted before RPC.
// The installed predecessor must be the actual policy predecessor, not merely
// an arbitrary binary with VERSION=Alpha81.
func validateBoundWorkerPredecessor(raw []byte, currentCommit string) error {
	var origin struct {
		SourceProof struct {
			ReleasePolicy boundWorkerReleasePolicy `json:"release_policy"`
		} `json:"source_proof"`
	}
	if json.Unmarshal(raw, &origin) != nil || !hex40.MatchString(currentCommit) || origin.SourceProof.ReleasePolicy.PreviousCommit != currentCommit {
		return errors.New("current-producer installed commit differs from the packaged policy predecessor")
	}
	return nil
}

const boundWorkerOriginPath = "/root/celikpanel-release-recovery-lab/worker-origin-intent.json"

// Alpha82 is an unpublished current-producer fixture, not an additional public
// release selection. Its installed test trust and exact artifacts must match
// the same nonce/VM-bound root-owned origin receipt before any Agent RPC.
func validateBoundWorkerOrigin(raw []byte, marker markerIdentity, target manifest, manifestRaw, signature, key []byte) error {
	var origin struct {
		Schema      string            `json:"schema"`
		Provenance  string            `json:"provenance"`
		Identity    map[string]string `json:"identity"`
		Target      map[string]string `json:"target"`
		SourceProof struct {
			Commit              string                   `json:"commit"`
			Tree                string                   `json:"tree"`
			VerifiedStaticFiles int                      `json:"verified_static_files"`
			Method              string                   `json:"method"`
			ReleasePolicy       boundWorkerReleasePolicy `json:"release_policy"`
		} `json:"source_proof"`
		Routes map[string]string `json:"routes"`
		Files  map[string]struct {
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"files"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&origin) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("current-producer origin receipt is invalid JSON")
	}
	expectedIdentity := map[string]string{"schema": labSchema, "nonce": marker.Nonce, "vm_uuid": marker.VMUUID, "cell_id": marker.CellID, "node": marker.Node}
	expectedTarget := map[string]string{"format": "celikpanel-release-manifest-v2", "sequence": target.Sequence,
		"version": target.Version, "commit": target.Commit, "published_at": target.PublishedAt, "os": target.OS,
		"arch": target.Arch, "archive": target.Archive, "archive_sha256": target.ArchiveSHA256, "archive_size": target.ArchiveSize}
	if target.Version != boundWorkerTargetVersion || target.Sequence != "82" || target.Arch != "amd64" ||
		origin.Schema != "celikpanel/worker-fixture-origin/v1" ||
		origin.Provenance != "unpublished-local-artifact-with-disposable-fixture-trust" ||
		!reflect.DeepEqual(origin.Identity, expectedIdentity) || !reflect.DeepEqual(origin.Target, expectedTarget) ||
		origin.SourceProof.Commit != target.Commit || !hex40.MatchString(origin.SourceProof.Tree) || origin.SourceProof.VerifiedStaticFiles <= 0 ||
		origin.SourceProof.Method != "real Git commit/tree and every packaged static source blob" {
		return errors.New("current-producer origin receipt differs from the exact guest and committed artifact")
	}
	if err := origin.SourceProof.ReleasePolicy.validate(target); err != nil {
		return err
	}
	base := "/releases/" + target.Version + "/linux/amd64/"
	expectedRoutes := map[string]string{"/releases/latest.txt": "worker-origin-latest", base + "release-manifest-v2": "worker-origin-manifest",
		base + "release-manifest-v2.sig": "worker-origin-signature", base + target.Archive: "worker-origin-archive.tar.gz",
		base + target.Archive + ".sha256": "worker-origin-archive.sha256"}
	if !reflect.DeepEqual(origin.Routes, expectedRoutes) || len(origin.Files) != 9 {
		return errors.New("current-producer origin route inventory differs")
	}
	for _, name := range []string{"worker-origin-latest", "worker-origin-manifest", "worker-origin-signature", "worker-origin-archive.tar.gz", "worker-origin-archive.sha256",
		"worker-origin-public.pem", "worker-origin-ca.pem", "worker-origin-tls.pem", "worker-origin-tls-key.pem"} {
		entry, ok := origin.Files[name]
		if !ok || !hex64.MatchString(entry.SHA256) || entry.Size <= 0 || entry.Size > 100*1024*1024 {
			return errors.New("current-producer origin file inventory differs")
		}
	}
	for name, contents := range map[string][]byte{"worker-origin-manifest": manifestRaw, "worker-origin-signature": signature, "worker-origin-public.pem": key,
		"worker-origin-latest":         []byte(target.Version + "\n"),
		"worker-origin-archive.sha256": []byte(target.ArchiveSHA256 + "  " + target.Archive + "\n")} {
		sum := sha256.Sum256(contents)
		entry := origin.Files[name]
		if entry.SHA256 != hex.EncodeToString(sum[:]) || entry.Size != int64(len(contents)) {
			return errors.New("current-producer manifest, signature or installed key differs from the sealed origin")
		}
	}
	archive := origin.Files["worker-origin-archive.tar.gz"]
	if archive.SHA256 != target.ArchiveSHA256 || !positiveDecimal(target.ArchiveSize, 2147483648) {
		return errors.New("current-producer archive identity differs")
	}
	size, err := strconv.ParseInt(target.ArchiveSize, 10, 64)
	if err != nil || size != archive.Size {
		return errors.New("current-producer archive size differs")
	}
	return nil
}

func validateBoundWorkerCurrent(target manifest, current transport.SystemUpdateCheckResponse) error {
	if target.Version != boundWorkerTargetVersion {
		return nil
	}
	if current.CurrentVersion != "v0.1.0-alpha.81" || current.CurrentCommit == target.Commit || !current.Available ||
		current.TargetVersion != target.Version || current.TargetCommit != target.Commit || current.TargetSequence != target.Sequence ||
		current.TargetOS != target.OS || current.TargetArch != target.Arch || current.TargetArchiveSHA256 != target.ArchiveSHA256 ||
		current.TargetArchiveSize != target.ArchiveSize || current.PublishedAt != target.PublishedAt {
		return errors.New("current-producer fixture requires the exact discovered Alpha81 to Alpha82 transition")
	}
	return nil
}
