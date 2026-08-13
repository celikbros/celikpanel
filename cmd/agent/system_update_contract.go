package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	systemUpdateOrigin          = "https://celikpanel.net"
	systemUpdateManifestFormat  = "celikpanel-release-manifest-v2"
	systemUpdateFloorFormat     = "celikpanel-release-sequence-floor-v1"
	systemUpdateKeyPath         = "/etc/celikpanel/release-signing-ed25519.pem"
	systemUpdateReleaseRoot     = "/var/lib/celikpanel-release-state"
	systemUpdateStateRoot       = "/var/lib/celikpanel-release-state/self-update"
	systemUpdateFloorPath       = "/var/lib/celikpanel-release-state/sequence.floor"
	systemUpdateAgentPath       = "/opt/celikpanel/bin/agent"
	systemUpdateInstallerPath   = "/usr/libexec/celikpanel/get.sh"
	systemUpdateMaxArchiveSize  = uint64(2_147_483_648)
	systemUpdateMaxManifestSize = 4096
	systemUpdateMaxErrorSize    = 1024
)

var (
	systemUpdateVersionRE = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
	systemUpdateCommitRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	systemUpdateDigestRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	systemUpdateRequestRE = regexp.MustCompile(`^[0-9a-f]{32}$`)
	systemUpdateTimeRE    = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
)

type systemUpdateManifest struct {
	Sequence      string
	Version       string
	Commit        string
	PublishedAt   string
	OS            string
	Arch          string
	Archive       string
	ArchiveSHA256 string
	ArchiveSize   string
}

func parseCanonicalPositiveDecimal(value string, maximum uint64) (uint64, error) {
	if value == "" || value[0] == '0' {
		return 0, errors.New("value is not a canonical positive decimal")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errors.New("value is not a canonical positive decimal")
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 || parsed > maximum || strconv.FormatUint(parsed, 10) != value {
		return 0, errors.New("value is outside the permitted range")
	}
	return parsed, nil
}

func validateSystemUpdateManifest(manifest systemUpdateManifest) error {
	if _, err := parseCanonicalPositiveDecimal(manifest.Sequence, math.MaxInt64); err != nil {
		return fmt.Errorf("invalid release sequence: %w", err)
	}
	if _, err := parseSystemUpdateSemver(manifest.Version); err != nil {
		return fmt.Errorf("invalid release version: %w", err)
	}
	if !systemUpdateCommitRE.MatchString(manifest.Commit) {
		return errors.New("invalid release commit")
	}
	if !systemUpdateTimeRE.MatchString(manifest.PublishedAt) {
		return errors.New("invalid publication time")
	}
	parsedTime, err := time.Parse("2006-01-02T15:04:05Z", manifest.PublishedAt)
	if err != nil || parsedTime.UTC().Format("2006-01-02T15:04:05Z") != manifest.PublishedAt {
		return errors.New("invalid publication time")
	}
	if manifest.OS != "linux" || (manifest.Arch != "amd64" && manifest.Arch != "arm64") {
		return errors.New("unsupported release platform")
	}
	wantArchive := "celikpanel-" + manifest.Version + "-" + manifest.OS + "-" + manifest.Arch + ".tar.gz"
	if manifest.Archive != wantArchive {
		return errors.New("release archive name is not canonical")
	}
	if !systemUpdateDigestRE.MatchString(manifest.ArchiveSHA256) {
		return errors.New("invalid release archive digest")
	}
	if _, err := parseCanonicalPositiveDecimal(manifest.ArchiveSize, systemUpdateMaxArchiveSize); err != nil {
		return fmt.Errorf("invalid release archive size: %w", err)
	}
	return nil
}

func canonicalSystemUpdateManifest(manifest systemUpdateManifest) ([]byte, error) {
	if err := validateSystemUpdateManifest(manifest); err != nil {
		return nil, err
	}
	return []byte(strings.Join([]string{
		"format=" + systemUpdateManifestFormat,
		"sequence=" + manifest.Sequence,
		"version=" + manifest.Version,
		"commit=" + manifest.Commit,
		"published_at=" + manifest.PublishedAt,
		"os=" + manifest.OS,
		"arch=" + manifest.Arch,
		"archive=" + manifest.Archive,
		"archive_sha256=" + manifest.ArchiveSHA256,
		"archive_size=" + manifest.ArchiveSize,
	}, "\n") + "\n"), nil
}

func parseCanonicalSystemUpdateManifest(raw []byte) (systemUpdateManifest, error) {
	if len(raw) == 0 || len(raw) > systemUpdateMaxManifestSize || raw[len(raw)-1] != '\n' {
		return systemUpdateManifest{}, errors.New("release manifest length or terminator is invalid")
	}
	for _, char := range raw {
		if char != '\n' && (char < 0x20 || char > 0x7e) {
			return systemUpdateManifest{}, errors.New("release manifest is not LF-terminated ASCII")
		}
	}
	lines := strings.Split(string(raw[:len(raw)-1]), "\n")
	if len(lines) != 10 {
		return systemUpdateManifest{}, errors.New("release manifest must contain exactly ten lines")
	}
	values := make([]string, len(lines))
	keys := []string{"format", "sequence", "version", "commit", "published_at", "os", "arch", "archive", "archive_sha256", "archive_size"}
	for index, key := range keys {
		prefix := key + "="
		if !strings.HasPrefix(lines[index], prefix) {
			return systemUpdateManifest{}, fmt.Errorf("release manifest line %d is not canonical", index+1)
		}
		values[index] = strings.TrimPrefix(lines[index], prefix)
	}
	if values[0] != systemUpdateManifestFormat {
		return systemUpdateManifest{}, errors.New("unsupported release manifest format")
	}
	manifest := systemUpdateManifest{
		Sequence: values[1], Version: values[2], Commit: values[3], PublishedAt: values[4],
		OS: values[5], Arch: values[6], Archive: values[7], ArchiveSHA256: values[8], ArchiveSize: values[9],
	}
	canonical, err := canonicalSystemUpdateManifest(manifest)
	if err != nil {
		return systemUpdateManifest{}, err
	}
	if !bytes.Equal(raw, canonical) {
		return systemUpdateManifest{}, errors.New("release manifest bytes are not canonical")
	}
	return manifest, nil
}

type systemUpdateFloor struct {
	Sequence string
	Version  string
}

func parseCanonicalSystemUpdateFloor(raw []byte) (systemUpdateFloor, error) {
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 4 || lines[3] != "" || lines[0] != "format="+systemUpdateFloorFormat ||
		!strings.HasPrefix(lines[1], "sequence=") || !strings.HasPrefix(lines[2], "version=") {
		return systemUpdateFloor{}, errors.New("release sequence floor is not canonical")
	}
	floor := systemUpdateFloor{Sequence: strings.TrimPrefix(lines[1], "sequence="), Version: strings.TrimPrefix(lines[2], "version=")}
	if _, err := parseCanonicalPositiveDecimal(floor.Sequence, math.MaxInt64); err != nil {
		return systemUpdateFloor{}, errors.New("release sequence floor has an invalid sequence")
	}
	if _, err := parseSystemUpdateSemver(floor.Version); err != nil {
		return systemUpdateFloor{}, errors.New("release sequence floor has an invalid version")
	}
	if !bytes.Equal(canonicalSystemUpdateFloor(floor), raw) {
		return systemUpdateFloor{}, errors.New("release sequence floor bytes are not canonical")
	}
	return floor, nil
}

func canonicalSystemUpdateFloor(floor systemUpdateFloor) []byte {
	return []byte("format=" + systemUpdateFloorFormat + "\nsequence=" + floor.Sequence + "\nversion=" + floor.Version + "\n")
}

func systemUpdateFloorAllows(floor *systemUpdateFloor, manifest systemUpdateManifest) error {
	if floor == nil {
		return errors.New("a pre-provisioned trusted release sequence floor is required")
	}
	floorSequence, err := parseCanonicalPositiveDecimal(floor.Sequence, math.MaxInt64)
	if err != nil {
		return errors.New("durable release sequence floor is invalid")
	}
	targetSequence, err := parseCanonicalPositiveDecimal(manifest.Sequence, math.MaxInt64)
	if err != nil {
		return errors.New("signed release sequence is invalid")
	}
	if targetSequence < floorSequence {
		return errors.New("signed release sequence is below the durable floor")
	}
	if targetSequence == floorSequence && manifest.Version != floor.Version {
		return errors.New("signed release sequence conflicts with the durable floor identity")
	}
	return nil
}

func systemUpdateRequestMatchesManifest(request *transport.SystemUpdateStartRequest, manifest systemUpdateManifest) bool {
	return request != nil && request.TargetVersion == manifest.Version && request.TargetCommit == manifest.Commit &&
		request.TargetSequence == manifest.Sequence && request.TargetOS == manifest.OS && request.TargetArch == manifest.Arch &&
		request.TargetArchiveSHA256 == manifest.ArchiveSHA256 && request.TargetArchiveSize == manifest.ArchiveSize
}

type systemUpdateSemver struct {
	core       [3]string
	prerelease []string
}

func parseSystemUpdateSemver(value string) (systemUpdateSemver, error) {
	matches := systemUpdateVersionRE.FindStringSubmatch(value)
	if matches == nil {
		return systemUpdateSemver{}, errors.New("version is not strict semantic versioning")
	}
	parsed := systemUpdateSemver{core: [3]string{matches[1], matches[2], matches[3]}}
	if matches[4] != "" {
		parsed.prerelease = strings.Split(matches[4], ".")
		for _, identifier := range parsed.prerelease {
			if identifier == "" || (decimalIdentifier(identifier) && len(identifier) > 1 && identifier[0] == '0') {
				return systemUpdateSemver{}, errors.New("version has a non-canonical prerelease identifier")
			}
		}
	}
	return parsed, nil
}

func decimalIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func compareDecimalIdentifiers(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func compareSystemUpdateSemver(left, right string) (int, error) {
	l, err := parseSystemUpdateSemver(left)
	if err != nil {
		return 0, err
	}
	r, err := parseSystemUpdateSemver(right)
	if err != nil {
		return 0, err
	}
	for index := range l.core {
		if compared := compareDecimalIdentifiers(l.core[index], r.core[index]); compared != 0 {
			return compared, nil
		}
	}
	if len(l.prerelease) == 0 && len(r.prerelease) == 0 {
		return 0, nil
	}
	if len(l.prerelease) == 0 {
		return 1, nil
	}
	if len(r.prerelease) == 0 {
		return -1, nil
	}
	limit := len(l.prerelease)
	if len(r.prerelease) < limit {
		limit = len(r.prerelease)
	}
	for index := 0; index < limit; index++ {
		ln, rn := decimalIdentifier(l.prerelease[index]), decimalIdentifier(r.prerelease[index])
		if ln && rn {
			if compared := compareDecimalIdentifiers(l.prerelease[index], r.prerelease[index]); compared != 0 {
				return compared, nil
			}
			continue
		}
		if ln != rn {
			if ln {
				return -1, nil
			}
			return 1, nil
		}
		if compared := strings.Compare(l.prerelease[index], r.prerelease[index]); compared != 0 {
			return compared, nil
		}
	}
	if len(l.prerelease) < len(r.prerelease) {
		return -1, nil
	}
	if len(l.prerelease) > len(r.prerelease) {
		return 1, nil
	}
	return 0, nil
}
