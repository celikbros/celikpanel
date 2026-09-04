package main

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// controlPlaneArchiveResult is what the caller prints and what a test asserts
// on. It never carries key material or file content.
type controlPlaneArchiveResult struct {
	Path    string
	Members int
	SHA256  string
}

// controlPlaneDatabaseFingerprint is the pair of numbers that say whether the
// live database changed shape underneath the online copy.
type controlPlaneDatabaseFingerprint struct {
	SchemaVersion    int64
	MigrationVersion int
}

// createControlPlaneArchive reads the live control plane once and writes one
// sealed archive. It is the whole feature; the CLI mode is a thin root-only
// wrapper around this function, which is also what the tests drive.
//
// createControlPlaneArchive canlı kontrol düzlemini bir kez okur ve tek bir
// mühürlü arşiv yazar. CLI kipi bu işlevin ince, yalnız-root sarmalayıcısıdır.
func createControlPlaneArchive(
	destinationPath string,
	keyText string,
	roots controlPlaneRoots,
	report io.Writer,
) (controlPlaneArchiveResult, error) {
	key, err := parseControlPlaneKey(keyText)
	if err != nil {
		return controlPlaneArchiveResult{}, err
	}
	defer zeroControlPlaneKey(key)

	if !filepath.IsAbs(destinationPath) {
		return controlPlaneArchiveResult{}, errors.New("the archive path must be absolute")
	}
	destinationPath = filepath.Clean(destinationPath)
	destinationDirectory := filepath.Dir(destinationPath)
	if _, err := os.Stat(destinationPath); err == nil {
		return controlPlaneArchiveResult{}, fmt.Errorf("%s already exists", destinationPath)
	} else if !os.IsNotExist(err) {
		return controlPlaneArchiveResult{}, fmt.Errorf("inspect %s: %w", destinationPath, err)
	}

	collection, err := collectControlPlaneMembers(roots)
	if err != nil {
		return controlPlaneArchiveResult{}, err
	}
	for _, skipped := range collection.Skipped {
		fmt.Fprintf(
			report,
			"skipped component=%q path=%s reason=%s\n",
			skipped.Component,
			skipped.Path,
			skipped.Reason,
		)
	}

	// The staging area is private and lives beside the archive, not in a shared
	// temporary directory: the copied database is the whole panel in one file.
	stagingDirectory, err := os.MkdirTemp(destinationDirectory, ".celikpanel-control-plane-stage-")
	if err != nil {
		return controlPlaneArchiveResult{}, fmt.Errorf("create the archive staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)
	if err := os.Chmod(stagingDirectory, 0o700); err != nil {
		return controlPlaneArchiveResult{}, fmt.Errorf("secure the archive staging directory: %w", err)
	}

	manifest, sources, err := buildControlPlaneManifest(roots, collection, stagingDirectory)
	if err != nil {
		return controlPlaneArchiveResult{}, err
	}
	for index, entry := range manifest.Members {
		if entry.Type != controlPlaneManifestEntryFile {
			continue
		}
		fmt.Fprintf(
			report,
			"member component=%q path=%s owner=%s:%s mode=%s size=%d sha256=%s\n",
			sources[index].Component,
			entry.Path,
			entry.Owner,
			entry.Group,
			entry.Mode,
			entry.Size,
			entry.SHA256,
		)
	}

	digest, err := writeControlPlaneArchiveFile(destinationPath, key, manifest, sources)
	if err != nil {
		return controlPlaneArchiveResult{}, err
	}
	result := controlPlaneArchiveResult{
		Path:    destinationPath,
		Members: countControlPlaneFileMembers(manifest),
		SHA256:  digest,
	}
	fmt.Fprintf(
		report,
		"archive=%s members=%d sha256=%s\n",
		result.Path,
		result.Members,
		result.SHA256,
	)
	return result, nil
}

// controlPlaneArchiveSource pairs one manifest entry with the bytes that will
// be written for it. For the database it is the verified staged copy, not the
// live file.
type controlPlaneArchiveSource struct {
	Component string
	ReadPath  string
}

func buildControlPlaneManifest(
	roots controlPlaneRoots,
	collection controlPlaneCollection,
	stagingDirectory string,
) (controlPlaneManifest, []controlPlaneArchiveSource, error) {
	host, err := os.Hostname()
	if err != nil {
		return controlPlaneManifest{}, nil, fmt.Errorf("read this host name: %w", err)
	}
	manifest := controlPlaneManifest{
		// The recorded schema version is the durable service-operation contract
		// this binary was built against, not the migration count of the copied
		// database. A restore refuses an archive whose contract is newer than
		// its own and then lets the ordinary migration path move the database
		// forward.
		SchemaVersion: durableServiceOperationSchemaVersion,
		PanelVersion:  buildVersion,
		PanelCommit:   buildCommit,
		Host:          host,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Roots:         roots,
	}
	sources := []controlPlaneArchiveSource{}

	for _, directory := range collection.Directories {
		entry, err := controlPlaneDirectoryEntry(directory)
		if err != nil {
			return controlPlaneManifest{}, nil, err
		}
		manifest.Members = append(manifest.Members, entry)
		sources = append(sources, controlPlaneArchiveSource{Component: "directory"})
	}

	for _, member := range collection.Members {
		readPath := member.Path
		if member.Database {
			staged, err := stageControlPlaneDatabase(member.Path, stagingDirectory)
			if err != nil {
				return controlPlaneManifest{}, nil, err
			}
			readPath = staged
			// Read the migration high water mark from the STAGED copy, which is
			// the copy the archive actually carries, not from the live database
			// that may have moved on since.
			fingerprint, err := readControlPlaneDatabaseFingerprint(staged)
			if err != nil {
				return controlPlaneManifest{}, nil, err
			}
			manifest.DatabaseMigrationVersion = fingerprint.MigrationVersion
		}
		entry, err := controlPlaneFileEntry(member.Path, readPath)
		if err != nil {
			return controlPlaneManifest{}, nil, err
		}
		manifest.Members = append(manifest.Members, entry)
		sources = append(sources, controlPlaneArchiveSource{
			Component: member.Component,
			ReadPath:  readPath,
		})
	}
	return manifest, sources, nil
}

func controlPlaneDirectoryEntry(path string) (controlPlaneManifestEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return controlPlaneManifestEntry{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return controlPlaneManifestEntry{}, fmt.Errorf("%s must be a real directory", path)
	}
	owner, group, err := controlPlaneOwnership(path, info)
	if err != nil {
		return controlPlaneManifestEntry{}, err
	}
	if _, err := controlPlaneMemberName(path); err != nil {
		return controlPlaneManifestEntry{}, err
	}
	return controlPlaneManifestEntry{
		Path:  filepath.Clean(path),
		Type:  controlPlaneManifestEntryDirectory,
		Owner: owner,
		Group: group,
		Mode:  formatControlPlaneMode(info.Mode()),
	}, nil
}

func controlPlaneFileEntry(recordedPath string, readPath string) (controlPlaneManifestEntry, error) {
	// Owner and mode always come from the live path; content comes from the
	// path that is actually read, which for the database is the staged copy.
	liveInfo, err := os.Lstat(recordedPath)
	if err != nil {
		return controlPlaneManifestEntry{}, fmt.Errorf("inspect %s: %w", recordedPath, err)
	}
	if err := requireControlPlaneRegularFile(recordedPath, liveInfo); err != nil {
		return controlPlaneManifestEntry{}, err
	}
	owner, group, err := controlPlaneOwnership(recordedPath, liveInfo)
	if err != nil {
		return controlPlaneManifestEntry{}, err
	}
	digest, size, err := digestControlPlaneFile(readPath)
	if err != nil {
		return controlPlaneManifestEntry{}, err
	}
	if _, err := controlPlaneMemberName(recordedPath); err != nil {
		return controlPlaneManifestEntry{}, err
	}
	return controlPlaneManifestEntry{
		Path:   filepath.Clean(recordedPath),
		Type:   controlPlaneManifestEntryFile,
		Owner:  owner,
		Group:  group,
		Mode:   formatControlPlaneMode(liveInfo.Mode()),
		Size:   size,
		SHA256: digest,
	}, nil
}

func formatControlPlaneMode(mode os.FileMode) string {
	return "0" + strconv.FormatUint(uint64(mode.Perm()), 8)
}

func parseControlPlaneMode(text string) (os.FileMode, error) {
	value, err := strconv.ParseUint(text, 8, 32)
	if err != nil || value > 0o7777 {
		return 0, fmt.Errorf("the recorded mode %q is not an octal file mode", text)
	}
	return os.FileMode(value), nil
}

func digestControlPlaneFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", path, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// stageControlPlaneDatabase takes the transaction-consistent copy the update
// path already relies on, then proves the copy really is a panel database. The
// live database is read twice around the copy: a migration that lands in
// between is detected and the copy is retried once rather than shipped.
func stageControlPlaneDatabase(sourcePath string, stagingDirectory string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		stagePath := filepath.Join(
			stagingDirectory,
			fmt.Sprintf("celikpanel.db.stage-%d", attempt),
		)
		before, err := readControlPlaneDatabaseFingerprint(sourcePath)
		if err != nil {
			return "", err
		}
		if err := copySQLiteDatabaseOnline(sourcePath, stagePath); err != nil {
			return "", fmt.Errorf("copy the panel database: %w", err)
		}
		if err := os.Chmod(stagePath, 0o600); err != nil {
			return "", fmt.Errorf("secure the staged panel database: %w", err)
		}
		if err := validateServiceOperationSnapshotSchema(
			stagePath,
			durableServiceOperationSchemaVersion,
			false,
		); err != nil {
			return "", fmt.Errorf("verify the copied panel database: %w", err)
		}
		after, err := readControlPlaneDatabaseFingerprint(sourcePath)
		if err != nil {
			return "", err
		}
		if before == after {
			return stagePath, nil
		}
		lastErr = fmt.Errorf(
			"the panel database changed while it was being copied (schema %d/%d, migrations %d/%d)",
			before.SchemaVersion, after.SchemaVersion,
			before.MigrationVersion, after.MigrationVersion,
		)
		if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("discard the retried database copy: %w", err)
		}
	}
	return "", lastErr
}

func readControlPlaneDatabaseFingerprint(
	path string,
) (controlPlaneDatabaseFingerprint, error) {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(path, true))
	if err != nil {
		return controlPlaneDatabaseFingerprint{}, fmt.Errorf("read the panel database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fingerprint := controlPlaneDatabaseFingerprint{}
	if err := database.QueryRowContext(ctx, `PRAGMA schema_version`).
		Scan(&fingerprint.SchemaVersion); err != nil {
		return controlPlaneDatabaseFingerprint{}, fmt.Errorf("read the panel database schema version: %w", err)
	}
	if err := database.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&fingerprint.MigrationVersion); err != nil {
		return controlPlaneDatabaseFingerprint{}, fmt.Errorf("read the panel migration version: %w", err)
	}
	return fingerprint, nil
}

func countControlPlaneFileMembers(manifest controlPlaneManifest) int {
	count := 0
	for _, entry := range manifest.Members {
		if entry.Type == controlPlaneManifestEntryFile {
			count++
		}
	}
	return count
}

// writeControlPlaneArchiveFile seals the tar stream into a temporary name in the
// destination directory and publishes it with one rename, so an interrupted run
// can never leave a half-written archive under the final name.
func writeControlPlaneArchiveFile(
	destinationPath string,
	key []byte,
	manifest controlPlaneManifest,
	sources []controlPlaneArchiveSource,
) (returnDigest string, returnErr error) {
	destinationDirectory := filepath.Dir(destinationPath)
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("name the archive staging file: %w", err)
	}
	stagePath := filepath.Join(
		destinationDirectory,
		"."+filepath.Base(destinationPath)+".incomplete-"+hex.EncodeToString(randomBytes),
	)
	file, err := os.OpenFile(
		stagePath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|controlPlaneOpenNoFollow,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("create the archive staging file: %w", err)
	}
	published := false
	closed := false
	defer func() {
		if !published {
			if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
				returnErr = errors.Join(returnErr, err)
			}
		}
	}()
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, file.Close())
		}
	}()

	hasher := sha256.New()
	sink := io.MultiWriter(file, hasher)
	header, err := newControlPlaneArchiveHeader(manifest.CreatedAt)
	if err != nil {
		return "", err
	}
	preamble, err := encodeControlPlaneArchivePreamble(header)
	if err != nil {
		return "", err
	}
	if _, err := sink.Write(preamble); err != nil {
		return "", fmt.Errorf("write the archive header: %w", err)
	}
	aead, err := newControlPlaneArchiveAEAD(key, header)
	if err != nil {
		return "", err
	}
	stream := newControlPlaneStreamWriter(sink, aead, preamble, header.Chunk)
	if err := writeControlPlaneTar(stream, manifest, sources); err != nil {
		return "", err
	}
	if err := stream.Close(); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("flush the archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close the archive: %w", err)
	}
	closed = true
	if err := os.Rename(stagePath, destinationPath); err != nil {
		return "", fmt.Errorf("publish the archive: %w", err)
	}
	published = true
	if err := controlPlaneSyncDirectory(destinationDirectory); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeControlPlaneTar(
	destination io.Writer,
	manifest controlPlaneManifest,
	sources []controlPlaneArchiveSource,
) error {
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode the archive manifest: %w", err)
	}
	manifestDigest := sha256.Sum256(manifestJSON)

	writer := tar.NewWriter(destination)
	if err := writeControlPlaneTarBytes(writer, controlPlaneManifestName, manifestJSON); err != nil {
		return err
	}
	for index, entry := range manifest.Members {
		name, err := controlPlaneMemberName(entry.Path)
		if err != nil {
			return err
		}
		mode, err := parseControlPlaneMode(entry.Mode)
		if err != nil {
			return err
		}
		if entry.Type == controlPlaneManifestEntryDirectory {
			if err := writer.WriteHeader(&tar.Header{
				Format:   tar.FormatPAX,
				Typeflag: tar.TypeDir,
				Name:     name + "/",
				Mode:     int64(mode.Perm()),
				Uname:    entry.Owner,
				Gname:    entry.Group,
			}); err != nil {
				return fmt.Errorf("write the archive entry for %s: %w", entry.Path, err)
			}
			continue
		}
		if err := writer.WriteHeader(&tar.Header{
			Format:   tar.FormatPAX,
			Typeflag: tar.TypeReg,
			Name:     name,
			Mode:     int64(mode.Perm()),
			Size:     entry.Size,
			Uname:    entry.Owner,
			Gname:    entry.Group,
		}); err != nil {
			return fmt.Errorf("write the archive entry for %s: %w", entry.Path, err)
		}
		if err := copyControlPlaneMemberContent(writer, sources[index].ReadPath, entry); err != nil {
			return err
		}
	}
	if err := writeControlPlaneTarBytes(
		writer,
		controlPlaneManifestDigestName,
		[]byte(hex.EncodeToString(manifestDigest[:])+"\n"),
	); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close the archive stream: %w", err)
	}
	return nil
}

// copyControlPlaneMemberContent re-reads the member while hashing it. A file
// that changed between the manifest pass and this one is reported instead of
// being archived under a digest it no longer has.
func copyControlPlaneMemberContent(
	writer io.Writer,
	readPath string,
	entry controlPlaneManifestEntry,
) error {
	file, err := os.Open(readPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.Path, err)
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(writer, hasher), io.LimitReader(file, entry.Size))
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.Path, err)
	}
	if written != entry.Size || hex.EncodeToString(hasher.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("%s changed while the archive was being written", entry.Path)
	}
	return nil
}

func writeControlPlaneTarBytes(writer *tar.Writer, name string, content []byte) error {
	if err := writer.WriteHeader(&tar.Header{
		Format:   tar.FormatPAX,
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     0o600,
		Size:     int64(len(content)),
	}); err != nil {
		return fmt.Errorf("write the archive entry %s: %w", name, err)
	}
	if _, err := writer.Write(content); err != nil {
		return fmt.Errorf("write the archive entry %s: %w", name, err)
	}
	return nil
}
