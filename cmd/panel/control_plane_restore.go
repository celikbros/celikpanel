package main

import (
	"archive/tar"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// controlPlaneRestoreResult is the summary the caller prints.
type controlPlaneRestoreResult struct {
	Restored         int
	SchemaVersion    int
	MigrationVersion int
	Source           string
}

// controlPlaneFreshHostMarkers are the files whose presence means this host
// already has a control plane of its own. Restore is a fresh-host operation and
// nothing else, so there is never a merge and never a second identity
// (docs/DISASTER-RECOVERY.md section 5).
var controlPlaneFreshHostMarkers = []string{
	"service-mutations.json",
	"dns-engine-state.json",
}

// restoreControlPlaneArchive places one archive onto a fresh host. Nothing is
// written to the target tree until the last ciphertext chunk has authenticated,
// every member digest has matched and every recorded account has resolved.
//
// restoreControlPlaneArchive bir arşivi temiz bir makineye yerleştirir. Son
// şifreli parça doğrulanmadan, her üye özeti eşleşmeden ve kayıtlı her hesap
// çözülmeden hedef ağaca hiçbir şey yazılmaz.
func restoreControlPlaneArchive(
	sourcePath string,
	keyText string,
	target controlPlaneRoots,
	report io.Writer,
) (controlPlaneRestoreResult, error) {
	key, err := parseControlPlaneKey(keyText)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	defer zeroControlPlaneKey(key)

	if !filepath.IsAbs(sourcePath) {
		return controlPlaneRestoreResult{}, errors.New("the archive path must be absolute")
	}
	sourcePath = filepath.Clean(sourcePath)
	if err := requireFreshControlPlaneHost(target); err != nil {
		return controlPlaneRestoreResult{}, err
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("read %s: %w", sourcePath, err)
	}
	defer file.Close()

	header, preamble, err := readControlPlaneArchivePreamble(file)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	aead, err := newControlPlaneArchiveAEAD(key, header)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}

	// The staging area sits beside the archive, which is already a root-only
	// path, so plaintext never lands in a shared temporary directory.
	stagingDirectory, err := os.MkdirTemp(
		filepath.Dir(sourcePath),
		".celikpanel-control-plane-restore-",
	)
	if err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("create the restore staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)
	if err := os.Chmod(stagingDirectory, 0o700); err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("secure the restore staging directory: %w", err)
	}

	stream := newControlPlaneStreamReader(file, aead, preamble, header.Chunk)
	manifest, staged, err := extractControlPlaneArchive(stream, stagingDirectory)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	if err := verifyControlPlaneManifest(manifest, staged); err != nil {
		return controlPlaneRestoreResult{}, err
	}
	placements, err := planControlPlanePlacement(manifest, staged, target)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	restored, err := placeControlPlaneMembers(placements, report)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}

	restoredDatabase := filepath.Join(target.DataDir, controlPlaneDatabaseBasename)
	if err := validateServiceOperationSnapshotSchema(
		restoredDatabase,
		manifest.SchemaVersion,
		false,
	); err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("verify the restored panel database: %w", err)
	}

	result := controlPlaneRestoreResult{
		Restored:         restored,
		SchemaVersion:    manifest.SchemaVersion,
		MigrationVersion: manifest.DatabaseMigrationVersion,
		Source:           sourcePath,
	}
	fmt.Fprintf(
		report,
		"restored=%d schema=%d migration=%d from=%s\n",
		result.Restored,
		result.SchemaVersion,
		result.MigrationVersion,
		result.Source,
	)
	return result, nil
}

func requireFreshControlPlaneHost(target controlPlaneRoots) error {
	databasePath := filepath.Join(target.DataDir, controlPlaneDatabaseBasename)
	if _, err := os.Lstat(databasePath); err == nil {
		return fmt.Errorf(
			"this host already has a panel database at %s; a control-plane archive is restored onto a fresh host only",
			databasePath,
		)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect %s: %w", databasePath, err)
	}
	for _, marker := range controlPlaneFreshHostMarkers {
		markerPath := filepath.Join(target.AgentStateDir, marker)
		if _, err := os.Lstat(markerPath); err == nil {
			return fmt.Errorf(
				"this host already has agent state at %s; a control-plane archive is restored onto a fresh host only",
				markerPath,
			)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", markerPath, err)
		}
	}
	return nil
}

// extractControlPlaneArchive decrypts the whole stream into the staging area.
// It reads the manifest from the first entry and refuses an archive whose
// durable schema contract is newer than this binary before it stores a single
// member, so an archive from a future release costs nothing but a temporary
// directory.
func extractControlPlaneArchive(
	stream io.Reader,
	stagingDirectory string,
) (controlPlaneManifest, map[string]string, error) {
	reader := tar.NewReader(stream)
	var manifest controlPlaneManifest
	manifestSeen := false
	manifestDigest := ""
	var manifestJSON []byte
	staged := map[string]string{}

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errControlPlaneArchiveAuthentication) {
				return controlPlaneManifest{}, nil, errControlPlaneArchiveAuthentication
			}
			return controlPlaneManifest{}, nil, fmt.Errorf("read the archive: %w", err)
		}
		switch header.Name {
		case controlPlaneManifestName:
			if manifestSeen {
				return controlPlaneManifest{}, nil, errors.New("the archive carries more than one manifest")
			}
			manifestJSON, err = io.ReadAll(io.LimitReader(reader, controlPlaneArchiveMaxManifestBytes+1))
			if err != nil {
				return controlPlaneManifest{}, nil, fmt.Errorf("read the archive manifest: %w", err)
			}
			if int64(len(manifestJSON)) > controlPlaneArchiveMaxManifestBytes {
				return controlPlaneManifest{}, nil, errors.New("the archive manifest is too large")
			}
			if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
				return controlPlaneManifest{}, nil, fmt.Errorf("decode the archive manifest: %w", err)
			}
			if manifest.SchemaVersion > durableServiceOperationSchemaVersion {
				return controlPlaneManifest{}, nil, fmt.Errorf(
					"the archive was written for schema version %d and this panel understands %d; install the matching release or a newer one before restoring",
					manifest.SchemaVersion,
					durableServiceOperationSchemaVersion,
				)
			}
			if err := requireRestorableControlPlaneMigrationVersion(
				manifest.DatabaseMigrationVersion,
			); err != nil {
				return controlPlaneManifest{}, nil, err
			}
			manifestSeen = true
		case controlPlaneManifestDigestName:
			raw, err := io.ReadAll(io.LimitReader(reader, 128))
			if err != nil {
				return controlPlaneManifest{}, nil, fmt.Errorf("read the archive manifest digest: %w", err)
			}
			manifestDigest = strings.TrimSpace(string(raw))
		default:
			if !manifestSeen {
				return controlPlaneManifest{}, nil, errors.New("the archive does not start with its manifest")
			}
			if err := validateControlPlaneMemberName(header.Name); err != nil {
				return controlPlaneManifest{}, nil, err
			}
			if header.Typeflag == tar.TypeDir {
				continue
			}
			if header.Typeflag != tar.TypeReg {
				return controlPlaneManifest{}, nil, fmt.Errorf(
					"the archive entry %s is not a regular file",
					header.Name,
				)
			}
			stagedPath, err := stageControlPlaneArchiveEntry(
				stagingDirectory,
				header.Name,
				reader,
			)
			if err != nil {
				return controlPlaneManifest{}, nil, err
			}
			staged[header.Name] = stagedPath
		}
	}
	if !manifestSeen {
		return controlPlaneManifest{}, nil, errors.New("the archive has no manifest")
	}
	digest := sha256.Sum256(manifestJSON)
	if manifestDigest != hex.EncodeToString(digest[:]) {
		return controlPlaneManifest{}, nil, errors.New("the archive manifest does not match its recorded digest")
	}
	return manifest, staged, nil
}

// controlPlaneArchiveMaxManifestBytes bounds a hostile manifest. A real one
// lists a few dozen paths.
const controlPlaneArchiveMaxManifestBytes int64 = 8 * 1024 * 1024

// requireRestorableControlPlaneMigrationVersion refuses a database that has
// been migrated further than this release can run, BEFORE a single member is
// staged or placed. Without it the database would land and only fail later, or
// worse, be opened by an older panel that cannot read its own schema. The
// ceiling is read from the migrations this binary actually ships, never from a
// number written down here.
// requireRestorableControlPlaneMigrationVersion, bu yayının çalıştırabileceğinden
// daha ileri taşınmış bir veritabanını, hiçbir üye yerleştirilmeden reddeder.
func requireRestorableControlPlaneMigrationVersion(recorded int) error {
	shipped, err := paneldb.HighestEmbeddedMigrationVersion()
	if err != nil {
		return fmt.Errorf("read the migrations this release ships: %w", err)
	}
	if recorded < 1 {
		return errors.New(
			"the archive manifest records no database migration version; it was not written by this product",
		)
	}
	if recorded > shipped {
		return fmt.Errorf(
			"the archived database is migrated to version %d and this panel ships migrations up to %d; install that release or a newer one before restoring",
			recorded,
			shipped,
		)
	}
	return nil
}

func stageControlPlaneArchiveEntry(
	stagingDirectory string,
	name string,
	content io.Reader,
) (string, error) {
	stagedPath := filepath.Join(stagingDirectory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(stagedPath), 0o700); err != nil {
		return "", fmt.Errorf("prepare the restore staging directory: %w", err)
	}
	file, err := os.OpenFile(
		stagedPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|controlPlaneOpenNoFollow,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("stage the archive entry %s: %w", name, err)
	}
	defer file.Close()
	if _, err := io.Copy(file, content); err != nil {
		if errors.Is(err, errControlPlaneArchiveAuthentication) {
			return "", errControlPlaneArchiveAuthentication
		}
		return "", fmt.Errorf("stage the archive entry %s: %w", name, err)
	}
	return stagedPath, nil
}

func verifyControlPlaneManifest(
	manifest controlPlaneManifest,
	staged map[string]string,
) error {
	if len(manifest.Members) == 0 {
		return errors.New("the archive manifest lists no members")
	}
	expected := map[string]struct{}{}
	for _, entry := range manifest.Members {
		name, err := controlPlaneMemberName(entry.Path)
		if err != nil {
			return err
		}
		if entry.Type == controlPlaneManifestEntryDirectory {
			continue
		}
		if entry.Type != controlPlaneManifestEntryFile {
			return fmt.Errorf("the archive manifest has an unknown member type %q", entry.Type)
		}
		expected[name] = struct{}{}
		stagedPath, ok := staged[name]
		if !ok {
			return fmt.Errorf("the archive manifest lists %s but the archive does not carry it", entry.Path)
		}
		digest, size, err := digestControlPlaneFile(stagedPath)
		if err != nil {
			return err
		}
		if size != entry.Size || digest != entry.SHA256 {
			return fmt.Errorf("the archived copy of %s does not match its recorded digest", entry.Path)
		}
	}
	for name := range staged {
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("the archive carries %s, which its manifest does not list", name)
		}
	}
	return nil
}

// controlPlanePlacement is one resolved write: where the bytes come from, where
// they go, and with which owner and mode.
type controlPlanePlacement struct {
	TargetPath string
	StagedPath string
	Directory  bool
	Mode       os.FileMode
	UID        int
	GID        int
}

// planControlPlanePlacement rebases every recorded path from the roots the
// archive was taken under onto the roots this host uses, and resolves every
// recorded account. It returns only when every member can be placed.
func planControlPlanePlacement(
	manifest controlPlaneManifest,
	staged map[string]string,
	target controlPlaneRoots,
) ([]controlPlanePlacement, error) {
	rebase, err := newControlPlaneRebase(manifest.Roots, target)
	if err != nil {
		return nil, err
	}
	placements := make([]controlPlanePlacement, 0, len(manifest.Members))
	for _, entry := range manifest.Members {
		targetPath, err := rebase(entry.Path)
		if err != nil {
			return nil, err
		}
		mode, err := parseControlPlaneMode(entry.Mode)
		if err != nil {
			return nil, err
		}
		uid, gid, err := controlPlaneResolveOwnership(entry.Owner, entry.Group)
		if err != nil {
			return nil, err
		}
		placement := controlPlanePlacement{
			TargetPath: targetPath,
			Directory:  entry.Type == controlPlaneManifestEntryDirectory,
			Mode:       mode,
			UID:        uid,
			GID:        gid,
		}
		if !placement.Directory {
			name, err := controlPlaneMemberName(entry.Path)
			if err != nil {
				return nil, err
			}
			placement.StagedPath = staged[name]
		}
		placements = append(placements, placement)
	}
	// Directories first, shallowest first, so every parent exists with its
	// recorded owner and mode before a file lands in it.
	sort.SliceStable(placements, func(left, right int) bool {
		if placements[left].Directory != placements[right].Directory {
			return placements[left].Directory
		}
		return placements[left].TargetPath < placements[right].TargetPath
	})
	return placements, nil
}

// newControlPlaneRebase maps a recorded absolute path onto this host by the
// root it belongs to. When the archive was taken with the same layout every
// mapping is the identity.
func newControlPlaneRebase(
	source controlPlaneRoots,
	target controlPlaneRoots,
) (func(string) (string, error), error) {
	pairs := [][2]string{
		{source.DataDir, target.DataDir},
		{source.ConfDir, target.ConfDir},
		{source.AgentStateDir, target.AgentStateDir},
		{source.DKIMDir, target.DKIMDir},
		{source.WireGuardDir, target.WireGuardDir},
		{source.TLSDir, target.TLSDir},
	}
	cleaned := make([][2]string, 0, len(pairs))
	for _, pair := range pairs {
		if strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" {
			return nil, errors.New("the archive manifest does not record every control-plane root")
		}
		cleaned = append(cleaned, [2]string{filepath.Clean(pair[0]), filepath.Clean(pair[1])})
	}
	// Longest recorded root first: the TLS directory is normally inside the
	// data directory, and the more specific root has to win.
	sort.SliceStable(cleaned, func(left, right int) bool {
		return len(cleaned[left][0]) > len(cleaned[right][0])
	})
	return func(recorded string) (string, error) {
		if _, err := controlPlaneMemberName(recorded); err != nil {
			return "", err
		}
		candidate := filepath.Clean(recorded)
		for _, pair := range cleaned {
			if candidate == pair[0] {
				return pair[1], nil
			}
			prefix := pair[0] + string(os.PathSeparator)
			if strings.HasPrefix(candidate, prefix) {
				placed := filepath.Join(pair[1], strings.TrimPrefix(candidate, prefix))
				if err := requireControlPlaneContainment(pair[1], placed); err != nil {
					return "", err
				}
				return placed, nil
			}
		}
		return "", fmt.Errorf(
			"the archive records %s, which lies outside every control-plane root it names",
			recorded,
		)
	}, nil
}

func requireControlPlaneContainment(root string, candidate string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return fmt.Errorf("place %s below %s: %w", candidate, root, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("the archive tried to place %s outside %s", candidate, root)
	}
	return nil
}

func placeControlPlaneMembers(
	placements []controlPlanePlacement,
	report io.Writer,
) (int, error) {
	restored := 0
	for _, placement := range placements {
		if placement.Directory {
			if err := placeControlPlaneDirectory(placement); err != nil {
				return 0, err
			}
			continue
		}
		if err := placeControlPlaneFile(placement); err != nil {
			return 0, err
		}
		restored++
		fmt.Fprintf(
			report,
			"placed path=%s mode=%s\n",
			placement.TargetPath,
			formatControlPlaneMode(placement.Mode),
		)
	}
	return restored, nil
}

func placeControlPlaneDirectory(placement controlPlanePlacement) error {
	if err := os.MkdirAll(placement.TargetPath, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", placement.TargetPath, err)
	}
	if err := controlPlaneApplyDirectoryMetadata(
		placement.TargetPath,
		placement.Mode,
		placement.UID,
		placement.GID,
	); err != nil {
		return err
	}
	return controlPlaneSyncDirectory(placement.TargetPath)
}

// placeControlPlaneFile writes into a temporary name in the final directory,
// gives it its recorded owner and mode while it is still private, flushes it,
// and only then renames it into place.
func placeControlPlaneFile(placement controlPlanePlacement) (returnErr error) {
	directory := filepath.Dir(placement.TargetPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", directory, err)
	}
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("name the restore staging file: %w", err)
	}
	stagePath := filepath.Join(
		directory,
		"."+filepath.Base(placement.TargetPath)+".restoring-"+hex.EncodeToString(randomBytes),
	)
	file, err := os.OpenFile(
		stagePath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|controlPlaneOpenNoFollow,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create the restore staging file for %s: %w", placement.TargetPath, err)
	}
	published := false
	defer func() {
		if !published {
			if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
				returnErr = errors.Join(returnErr, err)
			}
		}
	}()

	if err := func() error {
		defer file.Close()
		if err := controlPlaneApplyFileMetadata(
			file,
			placement.Mode,
			placement.UID,
			placement.GID,
		); err != nil {
			return err
		}
		source, err := os.Open(placement.StagedPath)
		if err != nil {
			return fmt.Errorf("read the staged copy of %s: %w", placement.TargetPath, err)
		}
		defer source.Close()
		if _, err := io.Copy(file, source); err != nil {
			return fmt.Errorf("write %s: %w", placement.TargetPath, err)
		}
		if err := file.Sync(); err != nil {
			return fmt.Errorf("flush %s: %w", placement.TargetPath, err)
		}
		return nil
	}(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, placement.TargetPath); err != nil {
		return fmt.Errorf("publish %s: %w", placement.TargetPath, err)
	}
	published = true
	if err := controlPlaneFinalizeFileMode(placement.TargetPath, placement.Mode); err != nil {
		return err
	}
	return controlPlaneSyncDirectory(directory)
}
