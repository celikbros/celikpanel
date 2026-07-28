package manifestv2

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

const catalogApplicationID = 0x43505632 // "CPV2"

type catalogBuildWorkspace struct {
	publishDirectory *os.File
	stagingDirectory *os.File
	stagingName      string
	database         *os.File
	databaseName     string
	databasePath     string
}

const catalogMetaSchema = `CREATE TABLE catalog_meta (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    schema_version INTEGER NOT NULL,
    catalog_version TEXT NOT NULL,
    catalog_sequence INTEGER NOT NULL CHECK (catalog_sequence > 0),
    minimum_agent_schema INTEGER NOT NULL,
    key_id TEXT NOT NULL,
    created_at TEXT NOT NULL
)`

const catalogItemsSchema = `CREATE TABLE catalog_items (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('component', 'addon', 'application')),
    revision INTEGER NOT NULL CHECK (revision > 0),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    metadata_json TEXT NOT NULL
        CHECK (json_valid(metadata_json) AND json_type(metadata_json) = 'object')
)`

const catalogRecipesSchema = `CREATE TABLE catalog_recipes (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    platform_key TEXT NOT NULL,
    operation TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    support_state TEXT NOT NULL
        CHECK (support_state IN ('supported', 'unsupported', 'manual_only', 'unavailable', 'blocked')),
    unsupported_reason TEXT NOT NULL DEFAULT '',
    selector_json TEXT NOT NULL
        CHECK (json_valid(selector_json) AND json_type(selector_json) = 'object'),
    recipe_json TEXT NOT NULL
        CHECK (json_valid(recipe_json) AND json_type(recipe_json) = 'object'),
    UNIQUE (item_id, platform_key, operation)
)`

const catalogRecipeLookupSchema = `CREATE INDEX idx_catalog_recipe_lookup
    ON catalog_recipes (item_id, operation)`

const catalogSchema = catalogMetaSchema + ";\n\n" +
	catalogItemsSchema + ";\n\n" +
	catalogRecipesSchema + ";\n\n" +
	catalogRecipeLookupSchema + ";"

// BuildCatalog creates a release in a private same-directory workspace and
// publishes it with one no-overwrite hard link.
// BuildCatalog, sürümü aynı dizindeki özel bir çalışma alanında oluşturur ve
// üzerine yazmayan tek bir sabit bağlantıyla yayımlar.
func BuildCatalog(ctx context.Context, databasePath string, doc CatalogDocument) (digest string, err error) {
	if err := requireSecureCatalogFilesystem(); err != nil {
		return "", err
	}
	if err := validateDocument(doc); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return "", fmt.Errorf("resolve catalog path: %w", err)
	}
	destinationName := filepath.Base(absolute)
	if err := validateCatalogBasename(destinationName); err != nil {
		return "", fmt.Errorf("validate catalog destination: %w", err)
	}
	parent := filepath.Dir(absolute)
	publishDirectory, err := openCatalogPublishDirectory(parent)
	if err != nil {
		return "", fmt.Errorf("open secure catalog publish directory: %w", err)
	}
	workspace, err := createCatalogBuildWorkspace(publishDirectory)
	if err != nil {
		_ = publishDirectory.Close()
		return "", fmt.Errorf("create private catalog build workspace: %w", err)
	}
	defer func() {
		cleanupErr := cleanupCatalogBuildWorkspace(workspace)
		closeErr := publishDirectory.Close()
		if cleanupErr == nil && closeErr == nil {
			return
		}
		deferredErr := fmt.Errorf(
			"clean private catalog build workspace: %w",
			errors.Join(cleanupErr, closeErr),
		)
		if err == nil {
			digest = ""
			err = deferredErr
			return
		}
		err = errors.Join(err, deferredErr)
	}()

	dsn, err := sqliteCatalogURI(workspace.databasePath, url.Values{
		"mode":    {"rw"},
		"_pragma": {"foreign_keys(1)", "trusted_schema(0)"},
	})
	if err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return "", fmt.Errorf("create catalog: %w", err)
	}
	dbOpen := true
	defer func() {
		if dbOpen {
			_ = db.Close()
		}
	}()
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = DELETE",
		"PRAGMA synchronous = FULL",
		fmt.Sprintf("PRAGMA application_id = %d", catalogApplicationID),
		fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion),
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return "", fmt.Errorf("initialize catalog: %w", err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin catalog build: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, catalogSchema); err != nil {
		return "", fmt.Errorf("create catalog schema: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO catalog_meta
            (singleton, schema_version, catalog_version, catalog_sequence,
             minimum_agent_schema, key_id, created_at)
         VALUES (1, ?, ?, ?, ?, ?, ?)`,
		doc.Metadata.SchemaVersion,
		doc.Metadata.CatalogVersion,
		doc.Metadata.CatalogSequence,
		doc.Metadata.MinimumAgentSchema,
		doc.Metadata.KeyID,
		doc.Metadata.CreatedAt,
	); err != nil {
		return "", fmt.Errorf("insert catalog metadata: %w", err)
	}

	items := append([]CatalogItem(nil), doc.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, item := range items {
		metadata := item.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage(`{}`)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO catalog_items (id, kind, revision, enabled, metadata_json)
             VALUES (?, ?, ?, ?, ?)`,
			item.ID,
			item.Kind,
			item.Revision,
			boolInt(item.Enabled),
			string(metadata),
		); err != nil {
			return "", fmt.Errorf("insert catalog item %q: %w", item.ID, err)
		}
	}

	recipes := append([]CatalogRecipe(nil), doc.Recipes...)
	sort.Slice(recipes, func(i, j int) bool { return recipes[i].ID < recipes[j].ID })
	for _, recipe := range recipes {
		selectorJSON, err := json.Marshal(recipe.Selector)
		if err != nil {
			return "", fmt.Errorf("encode recipe %q selector: %w", recipe.ID, err)
		}
		specJSON, err := json.Marshal(recipe.Spec)
		if err != nil {
			return "", fmt.Errorf("encode recipe %q: %w", recipe.ID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO catalog_recipes
                (id, item_id, platform_key, operation, revision, support_state,
                 unsupported_reason, selector_json, recipe_json)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			recipe.ID,
			recipe.ItemID,
			recipe.PlatformKey,
			recipe.Operation,
			recipe.Revision,
			recipe.Support,
			recipe.UnsupportedReason,
			string(selectorJSON),
			string(specJSON),
		); err != nil {
			return "", fmt.Errorf("insert catalog recipe %q: %w", recipe.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit catalog: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		return "", fmt.Errorf("compact catalog: %w", err)
	}
	if err := db.Close(); err != nil {
		return "", fmt.Errorf("close catalog: %w", err)
	}
	dbOpen = false
	if err := workspace.database.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect built catalog: %w", err)
	}
	if err := workspace.database.Sync(); err != nil {
		return "", fmt.Errorf("sync catalog: %w", err)
	}
	digest, _, err = digestCatalogFile(workspace.database)
	if err != nil {
		return "", err
	}
	if err := publishCatalog(
		workspace.database,
		destinationName,
		absolute,
		publishDirectory,
		syncCatalogDirectory,
	); err != nil {
		return "", err
	}
	return digest, nil
}

// syncCatalogDirectory makes the new entry durable before BuildCatalog succeeds.
// syncCatalogDirectory, BuildCatalog başarılı olmadan önce yeni girdiyi kalıcılaştırır.
func syncCatalogDirectory(directory *os.File) error {
	return directory.Sync()
}

func publishCatalog(
	source *os.File,
	destinationName string,
	destinationPath string,
	directory *os.File,
	syncDirectory func(*os.File) error,
) error {
	if err := validateCatalogBasename(destinationName); err != nil {
		return fmt.Errorf("validate catalog publish destination: %w", err)
	}
	if err := lockCatalogPublishDirectory(directory); err != nil {
		return fmt.Errorf("lock catalog publish directory: %w", err)
	}
	defer unlockCatalogPublishDirectory(directory)

	if err := linkCatalogFile(source, directory, destinationName); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("catalog output already exists: %s", destinationPath)
		}
		return fmt.Errorf("publish catalog without overwrite: %w", err)
	}
	if err := syncDirectory(directory); err == nil {
		return nil
	} else {
		publishErr := &CatalogPublishError{Path: destinationPath, Cause: err, DestinationMayRemain: true}
		publishErr.CleanupError = cleanupPublishedCatalog(
			source,
			destinationName,
			directory,
			syncDirectory,
		)
		publishErr.DestinationMayRemain = publishErr.CleanupError != nil
		return publishErr
	}
}

func cleanupPublishedCatalog(
	source *os.File,
	destinationName string,
	directory *os.File,
	syncDirectory func(*os.File) error,
) error {
	matches, err := catalogFileAtMatches(source, directory, destinationName)
	if err != nil {
		return fmt.Errorf("inspect linked catalog destination: %w", err)
	}
	if !matches {
		return fmt.Errorf("refuse to remove a destination that is not the built catalog")
	}
	if err := removeCatalogFileAt(directory, destinationName); err != nil {
		return fmt.Errorf("remove non-durable catalog destination: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync catalog cleanup: %w", err)
	}
	return nil
}

// validateCatalogBasename prevents an absolute or separated name from making
// openat/linkat ignore the pinned parent directory descriptor.
// validateCatalogBasename, mutlak ya da ayraçlı bir adın openat/linkat
// çağrılarında sabitlenmiş üst dizin tanıtıcısını yok saymasını önler.
func validateCatalogBasename(name string) error {
	if name == "" || name == "." || name == ".." ||
		filepath.IsAbs(name) ||
		filepath.Base(name) != name ||
		strings.ContainsRune(name, '/') ||
		strings.ContainsRune(name, rune(92)) ||
		strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("invalid catalog basename %q", name)
	}
	return nil
}

func sqliteCatalogURI(databasePath string, query url.Values) (string, error) {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return "", fmt.Errorf("resolve SQLite catalog path: %w", err)
	}
	slashPath := filepath.ToSlash(absolute)
	if filepath.VolumeName(absolute) != "" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	uri := url.URL{
		Scheme:   "file",
		Path:     slashPath,
		RawQuery: query.Encode(),
	}
	return uri.String(), nil
}

// DigestCatalog hashes the exact database bytes activated by the agent.
// DigestCatalog, agent'ın etkinleştireceği veritabanı baytlarını karmalar.
func DigestCatalog(databasePath string) (hexDigest string, rawDigest []byte, err error) {
	file, err := os.Open(databasePath)
	if err != nil {
		return "", nil, fmt.Errorf("open catalog for digest: %w", err)
	}
	defer file.Close()
	return digestCatalogFile(file)
}

func digestCatalogFile(file *os.File) (hexDigest string, rawDigest []byte, err error) {
	info, err := file.Stat()
	if err != nil {
		return "", nil, fmt.Errorf("inspect catalog for digest: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("catalog must be a regular file")
	}
	if info.Size() < 0 || info.Size() > maxCatalogBytes {
		return "", nil, fmt.Errorf("catalog exceeds %d-byte size limit", maxCatalogBytes)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", nil, fmt.Errorf("rewind catalog for digest: %w", err)
	}
	hasher := sha256.New()
	copied, err := io.Copy(hasher, io.LimitReader(file, maxCatalogBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("hash catalog: %w", err)
	}
	if copied != info.Size() || copied > maxCatalogBytes {
		return "", nil, fmt.Errorf("catalog changed while hashing")
	}
	raw := hasher.Sum(nil)
	return hex.EncodeToString(raw), raw, nil
}

// SignCatalog signs the SHA-256 digest, never an interpreted SQL or JSON value.
// SignCatalog, yorumlanmış SQL ya da JSON değerini değil SHA-256 karmasını imzalar.
func SignCatalog(databasePath, expectedDigest, keyID string, privateKey ed25519.PrivateKey) ([]byte, error) {
	if err := requireSecureCatalogFilesystem(); err != nil {
		return nil, err
	}
	if !digestPattern.MatchString(expectedDigest) {
		return nil, fmt.Errorf("invalid expected catalog digest")
	}
	if !catalogIDPattern.MatchString(keyID) {
		return nil, fmt.Errorf("invalid signing key id %q", keyID)
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key")
	}
	file, err := openCatalogSigningArtifact(databasePath)
	if err != nil {
		return nil, fmt.Errorf("open secure catalog signing artifact: %w", err)
	}
	defer file.Close()
	hexDigest, rawDigest, err := digestCatalogFile(file)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(hexDigest), []byte(expectedDigest)) != 1 {
		return nil, ErrCatalogDigestChanged
	}
	envelope := SignatureEnvelope{
		Algorithm: "ed25519-sha256",
		KeyID:     keyID,
		Digest:    hexDigest,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, rawDigest)),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode catalog signature: %w", err)
	}
	return append(data, '\n'), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
