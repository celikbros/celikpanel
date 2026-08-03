package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

const backupManifestName = "manifest.json"
const filesPayloadName = "payload/files.tar.gz"

type manifestPayload struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifestDatabase struct {
	Identity backupspec.DatabaseIdentity `json:"identity"`
	Payload  manifestPayload             `json:"payload"`
}

type backupManifest struct {
	Version        int                `json:"version"`
	Type           string             `json:"type"`
	Origin         string             `json:"origin"`
	JobKey         string             `json:"job_key,omitempty"`
	SubscriptionID int                `json:"subscription_id"`
	DomainID       int                `json:"domain_id"`
	CreatedAt      time.Time          `json:"created_at"`
	Files          manifestPayload    `json:"files,omitempty"`
	Databases      []manifestDatabase `json:"databases,omitempty"`
}

var backupNow = time.Now

func (m backupManifest) databaseIdentities() []backupspec.DatabaseIdentity {
	out := make([]backupspec.DatabaseIdentity, 0, len(m.Databases))
	for _, database := range m.Databases {
		out = append(out, database.Identity)
	}
	return out
}

func (m backupManifest) info(name string, size int64, legacy bool) backupspec.Info {
	return backupspec.Info{
		Name: name, Size: size, Type: m.Type, Origin: m.Origin,
		DatabaseID: manifestDatabaseID(m), Legacy: legacy, Restorable: true,
		CreatedAt: m.CreatedAt,
	}
}

func manifestDatabaseID(m backupManifest) int {
	if m.Type == backupspec.TypeDatabase && len(m.Databases) == 1 {
		return m.Databases[0].Identity.ID
	}
	return 0
}

func validBackupOrigin(origin string) bool {
	switch origin {
	case backupspec.OriginManual, backupspec.OriginScheduled, backupspec.OriginPreRestore:
		return true
	default:
		return false
	}
}

func databasePayloadName(id int) string {
	return path.Join("payload", "databases", strconv.Itoa(id)+".sql.gz")
}

func describePayload(filePath, archiveName string) (manifestPayload, error) {
	if !validPackageEntryName(archiveName) {
		return manifestPayload{}, errors.New("unsafe payload name")
	}
	file, info, err := secureOpenRegular(filePath)
	if err != nil {
		return manifestPayload{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return manifestPayload{}, err
	}
	if size != info.Size() {
		return manifestPayload{}, errors.New("payload changed while hashing")
	}
	return manifestPayload{
		Name:   archiveName,
		Size:   size,
		SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func validateManifestPayload(payload manifestPayload) error {
	if !validPackageEntryName(payload.Name) {
		return errors.New("unsafe payload name")
	}
	if payload.Size < 0 {
		return errors.New("negative payload size")
	}
	if len(payload.SHA256) != sha256.Size*2 {
		return errors.New("invalid payload hash")
	}
	if _, err := hex.DecodeString(payload.SHA256); err != nil {
		return errors.New("invalid payload hash")
	}
	return nil
}

func validPackageEntryName(name string) bool {
	if name == "" || strings.Contains(name, `\`) || path.IsAbs(name) {
		return false
	}
	clean := path.Clean(name)
	if clean != name || clean == "." {
		return false
	}
	return !strings.HasPrefix(clean, "../")
}

func validateManifest(m backupManifest, scope backupScope) error {
	if m.Version != backupspec.ProtocolVersion {
		return errors.New("manifest version mismatch")
	}
	if m.SubscriptionID != scope.SubscriptionID || m.DomainID != scope.DomainID {
		return errors.New("manifest scope mismatch")
	}
	if !validBackupOrigin(m.Origin) || m.CreatedAt.IsZero() {
		return errors.New("invalid manifest metadata")
	}
	if m.JobKey != "" && !backupspec.ValidJobKey(m.JobKey) {
		return errors.New("invalid backup job key")
	}
	if m.Files.Name != "" {
		if err := validateManifestPayload(m.Files); err != nil {
			return err
		}
		if m.Files.Name != filesPayloadName {
			return errors.New("unexpected files payload")
		}
	}
	seen := make(map[int]bool, len(m.Databases))
	for _, database := range m.Databases {
		identity, err := validateDatabaseIdentity(database.Identity)
		if err != nil {
			return err
		}
		if identity != database.Identity {
			return errors.New("database identity is not canonical")
		}
		if seen[identity.ID] {
			return errors.New("duplicate database ID")
		}
		seen[identity.ID] = true
		if err := validateManifestPayload(database.Payload); err != nil {
			return err
		}
		if database.Payload.Name != databasePayloadName(identity.ID) {
			return errors.New("unexpected database payload")
		}
	}
	switch m.Type {
	case backupspec.TypeFiles:
		if m.Files.Name == "" || len(m.Databases) != 0 {
			return errors.New("invalid files manifest")
		}
	case backupspec.TypeDatabase:
		if m.Files.Name != "" || len(m.Databases) != 1 {
			return errors.New("invalid database manifest")
		}
	case backupspec.TypeFull:
		if m.Files.Name == "" {
			return errors.New("full manifest missing files")
		}
	default:
		return errors.New("invalid manifest type")
	}
	return nil
}

func newBackupName(backupType string, databaseID int) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	stamp := backupNow().UTC().Format("20060102T150405.000000000Z")
	suffix := fmt.Sprintf("%s-%x.cpbak", stamp, random)
	switch backupType {
	case backupspec.TypeFiles:
		return "files-" + suffix, nil
	case backupspec.TypeDatabase:
		if databaseID < 1 {
			return "", errors.New("database ID required")
		}
		return fmt.Sprintf("database-%d-%s", databaseID, suffix), nil
	case backupspec.TypeFull:
		return "full-" + suffix, nil
	default:
		return "", errors.New("invalid backup type")
	}
}

var generatedBackupName = regexp.MustCompile(`^(files|full)-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{16}\.cpbak$|^database-[1-9][0-9]*-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{16}\.cpbak$`)
var legacyFilesName = regexp.MustCompile(`^(files|full)_[0-9]{8}_[0-9]{6}\.tar\.gz$`)
var legacyDatabaseName = regexp.MustCompile(`^db_[A-Za-z_][A-Za-z0-9_]*_[0-9]{8}_[0-9]{6}\.sql\.gz$`)
var legacyDomainName = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func validLegacyDomainName(name string) bool {
	return len(name) <= 253 && legacyDomainName.MatchString(name)
}

func validBackupName(name string) bool {
	if name == "" || name == "." || name == ".." || path.Base(name) != name {
		return false
	}
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return false
	}
	return generatedBackupName.MatchString(name) || legacyFilesName.MatchString(name) || legacyDatabaseName.MatchString(name)
}

func sortedPayloads(m backupManifest) []manifestPayload {
	result := make([]manifestPayload, 0, len(m.Databases)+1)
	if m.Files.Name != "" {
		result = append(result, m.Files)
	}
	for _, database := range m.Databases {
		result = append(result, database.Payload)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
