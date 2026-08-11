package manifestv2

import (
	"bytes"
	"context"
	"crypto/ed25519"
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
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	ErrRecipeNotFound  = errors.New("no matching component recipe")
	ErrRecipeAmbiguous = errors.New("multiple component recipes have equal specificity")
)

const (
	maxCatalogBytes   int64 = 64 << 20
	maxSignatureBytes       = 64 << 10
)

type Catalog struct {
	db          *sql.DB
	metadata    CatalogMetadata
	digest      string
	snapshotDir string
	closeOnce   sync.Once
	closeErr    error
}

// OpenVerifiedFiles verifies the detached signature before SQLite parses bytes.
// OpenVerifiedFiles, SQLite baytları ayrıştırmadan önce ayrık imzayı doğrular.
func OpenVerifiedFiles(
	ctx context.Context,
	databasePath string,
	signaturePath string,
	trustedKeys map[string]ed25519.PublicKey,
	policy OpenPolicy,
) (*Catalog, error) {
	if err := requireSecureCatalogFilesystem(); err != nil {
		return nil, err
	}
	file, err := os.Open(signaturePath)
	if err != nil {
		return nil, fmt.Errorf("open catalog signature: %w", err)
	}
	defer file.Close()
	signatureJSON, err := io.ReadAll(io.LimitReader(file, maxSignatureBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read catalog signature: %w", err)
	}
	if len(signatureJSON) > maxSignatureBytes {
		return nil, fmt.Errorf("catalog signature exceeds %d-byte size limit", maxSignatureBytes)
	}
	return OpenVerified(ctx, databasePath, signatureJSON, trustedKeys, policy)
}

// OpenVerified opens only a signed, structurally valid and agent-compatible catalog.
// OpenVerified, yalnız imzalı, yapısal olarak geçerli ve agent ile uyumlu kataloğu açar.
func OpenVerified(
	ctx context.Context,
	databasePath string,
	signatureJSON []byte,
	trustedKeys map[string]ed25519.PublicKey,
	policy OpenPolicy,
) (*Catalog, error) {
	if err := requireSecureCatalogFilesystem(); err != nil {
		return nil, err
	}
	if policy.AgentSchema < 1 {
		return nil, fmt.Errorf("open policy agent schema must be positive")
	}
	if policy.MinimumCatalogSequence < 0 {
		return nil, fmt.Errorf("open policy minimum catalog sequence cannot be negative")
	}
	if policy.MinimumCatalogSequence == 0 && policy.MinimumCatalogDigest != "" {
		return nil, fmt.Errorf("bootstrap open policy cannot pin a catalog digest")
	}
	if policy.MinimumCatalogSequence > 0 && !digestPattern.MatchString(policy.MinimumCatalogDigest) {
		return nil, fmt.Errorf("open policy must pin the digest at its minimum catalog sequence")
	}
	if len(signatureJSON) > maxSignatureBytes {
		return nil, fmt.Errorf("catalog signature exceeds %d-byte size limit", maxSignatureBytes)
	}
	snapshotDir, snapshotPath, err := snapshotCatalog(databasePath)
	if err != nil {
		return nil, err
	}
	keepSnapshot := false
	defer func() {
		if !keepSnapshot {
			_ = os.RemoveAll(snapshotDir)
		}
	}()

	var envelope SignatureEnvelope
	if err := strictJSON(signatureJSON, &envelope); err != nil {
		return nil, fmt.Errorf("decode catalog signature: %w", err)
	}
	if envelope.Algorithm != "ed25519-sha256" {
		return nil, fmt.Errorf("unsupported catalog signature algorithm %q", envelope.Algorithm)
	}
	publicKey, trusted := trustedKeys[envelope.KeyID]
	if !trusted || len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("catalog signing key %q is not trusted", envelope.KeyID)
	}
	if !digestPattern.MatchString(envelope.Digest) {
		return nil, fmt.Errorf("catalog signature has an invalid digest")
	}
	expectedDigest, _ := hex.DecodeString(envelope.Digest)
	actualHex, actualDigest, err := DigestCatalog(snapshotPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(expectedDigest, actualDigest) {
		return nil, fmt.Errorf("catalog digest mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("catalog signature is invalid")
	}
	if !ed25519.Verify(publicKey, actualDigest, signature) {
		return nil, fmt.Errorf("catalog signature verification failed")
	}

	dsn, err := sqliteCatalogURI(snapshotPath, url.Values{
		"mode":      {"ro"},
		"immutable": {"1"},
		"_pragma": {
			"query_only(1)",
			"foreign_keys(1)",
			"trusted_schema(0)",
			"busy_timeout(5000)",
		},
	})
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open verified catalog: %w", err)
	}
	db.SetMaxOpenConns(1)
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = db.Close()
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping verified catalog: %w", err)
	}
	if err := validateCatalogDatabase(ctx, db); err != nil {
		return nil, err
	}

	var meta CatalogMetadata
	if err := db.QueryRowContext(
		ctx,
		`SELECT schema_version, catalog_version, catalog_sequence,
                minimum_agent_schema, key_id, created_at
         FROM catalog_meta WHERE singleton = 1`,
	).Scan(
		&meta.SchemaVersion,
		&meta.CatalogVersion,
		&meta.CatalogSequence,
		&meta.MinimumAgentSchema,
		&meta.KeyID,
		&meta.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("read catalog metadata: %w", err)
	}
	if err := validateMetadata(meta); err != nil {
		return nil, err
	}
	if meta.KeyID != envelope.KeyID {
		return nil, fmt.Errorf("catalog metadata key does not match its signature")
	}
	if policy.AgentSchema < meta.MinimumAgentSchema {
		return nil, fmt.Errorf(
			"catalog requires agent schema %d, this agent supports %d",
			meta.MinimumAgentSchema,
			policy.AgentSchema,
		)
	}
	if meta.CatalogSequence < policy.MinimumCatalogSequence {
		return nil, fmt.Errorf(
			"catalog sequence %d is older than required sequence %d",
			meta.CatalogSequence,
			policy.MinimumCatalogSequence,
		)
	}
	if meta.CatalogSequence == policy.MinimumCatalogSequence && policy.MinimumCatalogSequence > 0 {
		pinnedDigest, _ := hex.DecodeString(policy.MinimumCatalogDigest)
		if !bytes.Equal(actualDigest, pinnedDigest) {
			return nil, fmt.Errorf("catalog sequence %d does not match its pinned digest", meta.CatalogSequence)
		}
	}

	catalog := &Catalog{
		db:          db,
		metadata:    meta,
		digest:      actualHex,
		snapshotDir: snapshotDir,
	}
	if err := catalog.validateAll(ctx); err != nil {
		return nil, err
	}
	closeOnError = false
	keepSnapshot = true
	return catalog, nil
}

// snapshotCatalog binds signature verification and SQLite parsing to one
// bounded private copy, even if the published source path changes concurrently.
// snapshotCatalog, yayımlanmış kaynak yolu eşzamanlı değişse bile imza
// doğrulamasını ve SQLite ayrıştırmasını tek, sınırlı özel kopyaya bağlar.
func snapshotCatalog(databasePath string) (snapshotDir string, snapshotPath string, err error) {
	source, err := os.Open(databasePath)
	if err != nil {
		return "", "", fmt.Errorf("open catalog for snapshot: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", "", fmt.Errorf("inspect catalog for snapshot: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("catalog source must be a regular file")
	}
	if info.Size() < 0 || info.Size() > maxCatalogBytes {
		return "", "", fmt.Errorf("catalog exceeds %d-byte size limit", maxCatalogBytes)
	}

	snapshotDir, err = os.MkdirTemp("", "celikpanel-manifest-v2-")
	if err != nil {
		return "", "", fmt.Errorf("create private catalog snapshot directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(snapshotDir)
		}
	}()
	if err := os.Chmod(snapshotDir, 0o700); err != nil {
		return "", "", fmt.Errorf("protect catalog snapshot directory: %w", err)
	}
	snapshotPath = filepath.Join(snapshotDir, "catalog.db")
	snapshot, err := os.OpenFile(snapshotPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("create private catalog snapshot: %w", err)
	}
	if err := snapshot.Chmod(0o600); err != nil {
		snapshot.Close()
		return "", "", fmt.Errorf("protect private catalog snapshot: %w", err)
	}
	copied, copyErr := io.Copy(snapshot, io.LimitReader(source, maxCatalogBytes+1))
	if copyErr == nil && copied > maxCatalogBytes {
		copyErr = fmt.Errorf("catalog exceeds %d-byte size limit", maxCatalogBytes)
	}
	if copyErr == nil {
		copyErr = snapshot.Sync()
	}
	closeErr := snapshot.Close()
	if copyErr != nil {
		return "", "", fmt.Errorf("copy catalog snapshot: %w", copyErr)
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("close catalog snapshot: %w", closeErr)
	}
	keep = true
	return snapshotDir, snapshotPath, nil
}

func validateCatalogDatabase(ctx context.Context, db *sql.DB) error {
	var trustedSchema int
	if err := db.QueryRowContext(ctx, "PRAGMA trusted_schema").Scan(&trustedSchema); err != nil {
		return fmt.Errorf("read catalog trusted_schema setting: %w", err)
	}
	if trustedSchema != 0 {
		return fmt.Errorf("verified catalog must use trusted_schema=OFF")
	}
	var queryOnly int
	if err := db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil {
		return fmt.Errorf("read catalog query_only setting: %w", err)
	}
	if queryOnly != 1 {
		return fmt.Errorf("verified catalog must be query-only")
	}
	var applicationID int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID); err != nil {
		return fmt.Errorf("read catalog application id: %w", err)
	}
	if applicationID != catalogApplicationID {
		return fmt.Errorf("file is not a CelikPanel component catalog")
	}
	var userVersion int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		return fmt.Errorf("read catalog schema version: %w", err)
	}
	if userVersion != SchemaVersion {
		return fmt.Errorf("catalog SQLite schema %d is unsupported", userVersion)
	}
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result); err != nil {
		return fmt.Errorf("check catalog integrity: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("catalog integrity check failed: %s", result)
	}
	if err := validateCatalogSchema(ctx, db); err != nil {
		return err
	}
	return validateCatalogDataInvariants(ctx, db)
}

func validateCatalogDataInvariants(ctx context.Context, db *sql.DB) error {
	var metadataRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_meta`).Scan(&metadataRows); err != nil {
		return fmt.Errorf("count catalog metadata rows: %w", err)
	}
	if metadataRows != 1 {
		return fmt.Errorf("catalog metadata must contain exactly one row")
	}
	checks := []struct {
		label string
		query string
	}{
		{
			label: "metadata domain",
			query: `SELECT COUNT(*) FROM catalog_meta
			         WHERE typeof(singleton) <> 'integer' OR singleton <> 1
			            OR typeof(schema_version) <> 'integer'
			            OR typeof(catalog_version) <> 'text'
			            OR typeof(catalog_sequence) <> 'integer' OR catalog_sequence < 1
			            OR typeof(minimum_agent_schema) <> 'integer' OR minimum_agent_schema < 1
			            OR typeof(key_id) <> 'text' OR typeof(created_at) <> 'text'`,
		},
		{
			label: "item domain",
			query: `SELECT COUNT(*) FROM catalog_items
			         WHERE typeof(id) <> 'text' OR typeof(kind) <> 'text'
			            OR kind NOT IN ('component', 'addon', 'application')
			            OR typeof(revision) <> 'integer' OR revision < 1
			            OR typeof(enabled) <> 'integer' OR enabled NOT IN (0, 1)
			            OR typeof(metadata_json) <> 'text'
			            OR NOT json_valid(metadata_json) OR json_type(metadata_json) <> 'object'`,
		},
		{
			label: "recipe domain",
			query: `SELECT COUNT(*) FROM catalog_recipes
			         WHERE typeof(id) <> 'text' OR typeof(item_id) <> 'text'
			            OR typeof(platform_key) <> 'text' OR typeof(operation) <> 'text'
			            OR typeof(revision) <> 'integer' OR revision < 1
			            OR typeof(support_state) <> 'text'
			            OR support_state NOT IN ('supported', 'unsupported', 'manual_only', 'unavailable', 'blocked')
			            OR typeof(unsupported_reason) <> 'text'
			            OR typeof(selector_json) <> 'text'
			            OR NOT json_valid(selector_json) OR json_type(selector_json) <> 'object'
			            OR typeof(recipe_json) <> 'text'
			            OR NOT json_valid(recipe_json) OR json_type(recipe_json) <> 'object'`,
		},
	}
	for _, check := range checks {
		var invalidRows int
		if err := db.QueryRowContext(ctx, check.query).Scan(&invalidRows); err != nil {
			return fmt.Errorf("check catalog %s: %w", check.label, err)
		}
		if invalidRows != 0 {
			return fmt.Errorf("catalog %s has %d invalid rows", check.label, invalidRows)
		}
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("check catalog foreign keys: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("catalog contains a foreign key violation")
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate catalog foreign key check: %w", err)
	}
	return nil
}

type catalogColumn struct {
	name       string
	columnType string
	notNull    int
	defaultSQL *string
	primaryKey int
}

type catalogIndex struct {
	unique    int
	origin    string
	partial   int
	columns   []string
	columnIDs []int
}

type catalogSchemaObject struct {
	table  string
	sql    string
	hasSQL bool
}

func validateCatalogSchema(ctx context.Context, db *sql.DB) error {
	expectedObjects := map[string]catalogSchemaObject{
		"index\x00idx_catalog_recipe_lookup": {
			table: "catalog_recipes", sql: catalogRecipeLookupSchema, hasSQL: true,
		},
		"index\x00sqlite_autoindex_catalog_items_1":   {table: "catalog_items"},
		"index\x00sqlite_autoindex_catalog_recipes_1": {table: "catalog_recipes"},
		"index\x00sqlite_autoindex_catalog_recipes_2": {table: "catalog_recipes"},
		"table\x00catalog_items": {
			table: "catalog_items", sql: catalogItemsSchema, hasSQL: true,
		},
		"table\x00catalog_meta": {
			table: "catalog_meta", sql: catalogMetaSchema, hasSQL: true,
		},
		"table\x00catalog_recipes": {
			table: "catalog_recipes", sql: catalogRecipesSchema, hasSQL: true,
		},
	}
	rows, err := db.QueryContext(
		ctx,
		`SELECT type, name, tbl_name, sql
		   FROM sqlite_master
		  WHERE type IN ('table', 'index', 'view', 'trigger')
		  ORDER BY type, name`,
	)
	if err != nil {
		return fmt.Errorf("read catalog schema objects: %w", err)
	}
	actualObjects := map[string]catalogSchemaObject{}
	for rows.Next() {
		var objectType, name, table string
		var objectSQL sql.NullString
		if err := rows.Scan(&objectType, &name, &table, &objectSQL); err != nil {
			rows.Close()
			return fmt.Errorf("scan catalog schema object: %w", err)
		}
		actualObjects[objectType+"\x00"+name] = catalogSchemaObject{
			table: table, sql: objectSQL.String, hasSQL: objectSQL.Valid,
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close catalog schema object rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate catalog schema objects: %w", err)
	}
	if len(actualObjects) != len(expectedObjects) {
		return fmt.Errorf("catalog has %d schema objects, expected %d", len(actualObjects), len(expectedObjects))
	}
	for key, want := range expectedObjects {
		got, ok := actualObjects[key]
		if !ok {
			return fmt.Errorf("catalog schema is missing object %q", key)
		}
		if got.table != want.table || got.hasSQL != want.hasSQL || got.sql != want.sql {
			return fmt.Errorf("catalog schema object %q does not exactly match", key)
		}
	}

	emptyDefault := "''"
	expectedColumns := map[string][]catalogColumn{
		"catalog_meta": {
			{name: "singleton", columnType: "INTEGER", primaryKey: 1},
			{name: "schema_version", columnType: "INTEGER", notNull: 1},
			{name: "catalog_version", columnType: "TEXT", notNull: 1},
			{name: "catalog_sequence", columnType: "INTEGER", notNull: 1},
			{name: "minimum_agent_schema", columnType: "INTEGER", notNull: 1},
			{name: "key_id", columnType: "TEXT", notNull: 1},
			{name: "created_at", columnType: "TEXT", notNull: 1},
		},
		"catalog_items": {
			{name: "id", columnType: "TEXT", primaryKey: 1},
			{name: "kind", columnType: "TEXT", notNull: 1},
			{name: "revision", columnType: "INTEGER", notNull: 1},
			{name: "enabled", columnType: "INTEGER", notNull: 1},
			{name: "metadata_json", columnType: "TEXT", notNull: 1},
		},
		"catalog_recipes": {
			{name: "id", columnType: "TEXT", primaryKey: 1},
			{name: "item_id", columnType: "TEXT", notNull: 1},
			{name: "platform_key", columnType: "TEXT", notNull: 1},
			{name: "operation", columnType: "TEXT", notNull: 1},
			{name: "revision", columnType: "INTEGER", notNull: 1},
			{name: "support_state", columnType: "TEXT", notNull: 1},
			{name: "unsupported_reason", columnType: "TEXT", notNull: 1, defaultSQL: &emptyDefault},
			{name: "selector_json", columnType: "TEXT", notNull: 1},
			{name: "recipe_json", columnType: "TEXT", notNull: 1},
		},
	}
	for table, columns := range expectedColumns {
		if err := validateTableColumns(ctx, db, table, columns); err != nil {
			return err
		}
	}
	for _, table := range []string{"catalog_meta", "catalog_items"} {
		if err := validateForeignKeys(ctx, db, table, nil); err != nil {
			return err
		}
	}
	if err := validateForeignKeys(ctx, db, "catalog_recipes", []string{
		"0\x000\x00catalog_items\x00item_id\x00id\x00NO ACTION\x00CASCADE\x00NONE",
	}); err != nil {
		return err
	}

	expectedIndexes := map[string]map[string]catalogIndex{
		"catalog_meta": {},
		"catalog_items": {
			"sqlite_autoindex_catalog_items_1": {
				unique: 1, origin: "pk", columns: []string{"id"}, columnIDs: []int{0},
			},
		},
		"catalog_recipes": {
			"idx_catalog_recipe_lookup": {
				origin: "c", columns: []string{"item_id", "operation"}, columnIDs: []int{1, 3},
			},
			"sqlite_autoindex_catalog_recipes_1": {
				unique: 1, origin: "pk", columns: []string{"id"}, columnIDs: []int{0},
			},
			"sqlite_autoindex_catalog_recipes_2": {
				unique: 1, origin: "u",
				columns: []string{"item_id", "platform_key", "operation"}, columnIDs: []int{1, 2, 3},
			},
		},
	}
	for table, indexes := range expectedIndexes {
		if err := validateIndexes(ctx, db, table, indexes); err != nil {
			return err
		}
	}
	return nil
}

func equalStringSets(label string, actual, expected map[string]struct{}) error {
	for value := range actual {
		if _, ok := expected[value]; !ok {
			return fmt.Errorf("%s contain unexpected entry %q", label, value)
		}
	}
	for value := range expected {
		if _, ok := actual[value]; !ok {
			return fmt.Errorf("%s are missing entry %q", label, value)
		}
	}
	return nil
}

func validateTableColumns(
	ctx context.Context,
	db *sql.DB,
	table string,
	expected []catalogColumn,
) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_xinfo("+table+")")
	if err != nil {
		return fmt.Errorf("read %s columns: %w", table, err)
	}
	var actual []catalogColumn
	for rows.Next() {
		var cid int
		var hidden int
		var column catalogColumn
		var defaultSQL sql.NullString
		if err := rows.Scan(
			&cid,
			&column.name,
			&column.columnType,
			&column.notNull,
			&defaultSQL,
			&column.primaryKey,
			&hidden,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan %s column: %w", table, err)
		}
		if cid != len(actual) {
			rows.Close()
			return fmt.Errorf("%s column identifiers are not contiguous", table)
		}
		if hidden != 0 {
			rows.Close()
			return fmt.Errorf("%s column %d is hidden or generated", table, cid)
		}
		if defaultSQL.Valid {
			value := defaultSQL.String
			column.defaultSQL = &value
		}
		actual = append(actual, column)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s column rows: %w", table, err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%s has %d columns, expected %d", table, len(actual), len(expected))
	}
	for index := range expected {
		got, want := actual[index], expected[index]
		defaultMatches := got.defaultSQL == nil && want.defaultSQL == nil
		if got.defaultSQL != nil && want.defaultSQL != nil {
			defaultMatches = *got.defaultSQL == *want.defaultSQL
		}
		if got.name != want.name || got.columnType != want.columnType ||
			got.notNull != want.notNull || got.primaryKey != want.primaryKey || !defaultMatches {
			return fmt.Errorf("%s column %d does not match the catalog schema", table, index)
		}
	}
	return nil
}

func validateForeignKeys(
	ctx context.Context,
	db *sql.DB,
	table string,
	expected []string,
) error {
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_list("+table+")")
	if err != nil {
		return fmt.Errorf("read %s foreign keys: %w", table, err)
	}
	var actual []string
	for rows.Next() {
		var id, sequence int
		var target, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(
			&id,
			&sequence,
			&target,
			&from,
			&to,
			&onUpdate,
			&onDelete,
			&match,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan %s foreign key: %w", table, err)
		}
		actual = append(actual, fmt.Sprintf(
			"%d\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
			id,
			sequence,
			target,
			from,
			to,
			onUpdate,
			onDelete,
			match,
		))
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s foreign key rows: %w", table, err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s foreign keys: %w", table, err)
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%s has %d foreign keys, expected %d", table, len(actual), len(expected))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("%s foreign key %d does not match the catalog schema", table, index)
		}
	}
	return nil
}

func validateIndexes(
	ctx context.Context,
	db *sql.DB,
	table string,
	expected map[string]catalogIndex,
) error {
	rows, err := db.QueryContext(ctx, "PRAGMA index_list("+table+")")
	if err != nil {
		return fmt.Errorf("read %s indexes: %w", table, err)
	}
	actual := map[string]catalogIndex{}
	for rows.Next() {
		var sequence int
		var name string
		var index catalogIndex
		if err := rows.Scan(
			&sequence,
			&name,
			&index.unique,
			&index.origin,
			&index.partial,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan %s index: %w", table, err)
		}
		actual[name] = index
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s index rows: %w", table, err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s indexes: %w", table, err)
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%s has %d indexes, expected %d", table, len(actual), len(expected))
	}
	for name, want := range expected {
		got, ok := actual[name]
		if !ok {
			return fmt.Errorf("%s is missing index %q", table, name)
		}
		if got.unique != want.unique || got.origin != want.origin || got.partial != want.partial {
			return fmt.Errorf("%s index %q properties do not match the catalog schema", table, name)
		}
		columnRows, err := db.QueryContext(ctx, "PRAGMA index_xinfo("+name+")")
		if err != nil {
			return fmt.Errorf("read %s index %q columns: %w", table, name, err)
		}
		rowCount := 0
		for columnRows.Next() {
			var sequence, cid, descending, key int
			var column, collation sql.NullString
			if err := columnRows.Scan(
				&sequence,
				&cid,
				&column,
				&descending,
				&collation,
				&key,
			); err != nil {
				columnRows.Close()
				return fmt.Errorf("scan %s index %q column: %w", table, name, err)
			}
			if sequence != rowCount {
				columnRows.Close()
				return fmt.Errorf("%s index %q column order is invalid", table, name)
			}
			switch {
			case sequence < len(want.columns):
				if cid != want.columnIDs[sequence] || !column.Valid ||
					column.String != want.columns[sequence] || descending != 0 ||
					!collation.Valid || collation.String != "BINARY" || key != 1 {
					columnRows.Close()
					return fmt.Errorf("%s index %q has unexpected key terms", table, name)
				}
			case sequence == len(want.columns):
				if cid != -1 || column.Valid || descending != 0 ||
					!collation.Valid || collation.String != "BINARY" || key != 0 {
					columnRows.Close()
					return fmt.Errorf("%s index %q has an unexpected auxiliary term", table, name)
				}
			default:
				columnRows.Close()
				return fmt.Errorf("%s index %q has too many terms", table, name)
			}
			rowCount++
		}
		if err := columnRows.Close(); err != nil {
			return fmt.Errorf("close %s index %q column rows: %w", table, name, err)
		}
		if err := columnRows.Err(); err != nil {
			return fmt.Errorf("iterate %s index %q columns: %w", table, name, err)
		}
		if rowCount != len(want.columns)+1 {
			return fmt.Errorf("%s index %q has %d terms, expected %d", table, name, rowCount, len(want.columns)+1)
		}
	}
	return nil
}

func (catalog *Catalog) validateAll(ctx context.Context) error {
	doc := CatalogDocument{Metadata: catalog.metadata}
	rows, err := catalog.db.QueryContext(
		ctx,
		`SELECT id, kind, revision, enabled, metadata_json
         FROM catalog_items ORDER BY id`,
	)
	if err != nil {
		return fmt.Errorf("read catalog items: %w", err)
	}
	for rows.Next() {
		var item CatalogItem
		var enabled int
		var metadata string
		if err := rows.Scan(&item.ID, &item.Kind, &item.Revision, &enabled, &metadata); err != nil {
			rows.Close()
			return fmt.Errorf("scan catalog item: %w", err)
		}
		item.Enabled = enabled == 1
		if enabled != 0 && enabled != 1 {
			rows.Close()
			return fmt.Errorf("catalog item %q has invalid enabled value %d", item.ID, enabled)
		}
		item.Metadata = json.RawMessage(metadata)
		doc.Items = append(doc.Items, item)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close catalog item rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate catalog items: %w", err)
	}

	recipeRows, err := catalog.db.QueryContext(
		ctx,
		`SELECT id, item_id, platform_key, operation, revision, support_state,
                unsupported_reason, selector_json, recipe_json
         FROM catalog_recipes ORDER BY id`,
	)
	if err != nil {
		return fmt.Errorf("read catalog recipes: %w", err)
	}
	for recipeRows.Next() {
		var recipe CatalogRecipe
		var selectorJSON, specJSON string
		if err := recipeRows.Scan(
			&recipe.ID,
			&recipe.ItemID,
			&recipe.PlatformKey,
			&recipe.Operation,
			&recipe.Revision,
			&recipe.Support,
			&recipe.UnsupportedReason,
			&selectorJSON,
			&specJSON,
		); err != nil {
			recipeRows.Close()
			return fmt.Errorf("scan catalog recipe: %w", err)
		}
		if err := strictJSON([]byte(selectorJSON), &recipe.Selector); err != nil {
			recipeRows.Close()
			return fmt.Errorf("decode recipe %q selector: %w", recipe.ID, err)
		}
		if err := strictJSON([]byte(specJSON), &recipe.Spec); err != nil {
			recipeRows.Close()
			return fmt.Errorf("decode recipe %q spec: %w", recipe.ID, err)
		}
		doc.Recipes = append(doc.Recipes, recipe)
	}
	if err := recipeRows.Close(); err != nil {
		return fmt.Errorf("close catalog recipe rows: %w", err)
	}
	if err := recipeRows.Err(); err != nil {
		return fmt.Errorf("iterate catalog recipes: %w", err)
	}
	return validateDocument(doc)
}

func (catalog *Catalog) Close() error {
	if catalog == nil {
		return nil
	}
	catalog.closeOnce.Do(func() {
		var databaseErr, cleanupErr error
		if catalog.db != nil {
			databaseErr = catalog.db.Close()
		}
		if catalog.snapshotDir != "" {
			cleanupErr = os.RemoveAll(catalog.snapshotDir)
			catalog.snapshotDir = ""
		}
		catalog.closeErr = errors.Join(databaseErr, cleanupErr)
	})
	return catalog.closeErr
}

func (catalog *Catalog) Metadata() CatalogMetadata {
	return catalog.metadata
}

func (catalog *Catalog) Digest() string {
	return catalog.digest
}

// Resolve returns one deterministic dry-run recipe; it never executes a step.
// Resolve, tek belirlenimci kuru-çalıştırma reçetesi döndürür; hiçbir adımı yürütmez.
func (catalog *Catalog) Resolve(
	ctx context.Context,
	itemID string,
	operation string,
	host HostProfile,
) (*ResolvedRecipe, error) {
	if !catalogIDPattern.MatchString(itemID) || !operationPattern.MatchString(operation) {
		return nil, ErrRecipeNotFound
	}
	var item CatalogItem
	var enabled int
	var metadata string
	if err := catalog.db.QueryRowContext(
		ctx,
		`SELECT id, kind, revision, enabled, metadata_json
         FROM catalog_items WHERE id = ?`,
		itemID,
	).Scan(&item.ID, &item.Kind, &item.Revision, &enabled, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecipeNotFound
		}
		return nil, fmt.Errorf("read catalog item: %w", err)
	}
	item.Enabled = enabled == 1
	item.Metadata = json.RawMessage(metadata)
	if !item.Enabled {
		return nil, ErrRecipeNotFound
	}

	rows, err := catalog.db.QueryContext(
		ctx,
		`SELECT id, item_id, platform_key, operation, revision, support_state,
                unsupported_reason, selector_json, recipe_json
         FROM catalog_recipes WHERE item_id = ? AND operation = ?`,
		itemID,
		operation,
	)
	if err != nil {
		return nil, fmt.Errorf("read matching recipes: %w", err)
	}
	defer rows.Close()

	bestScore := -1
	var best *CatalogRecipe
	tiedRecipeID := ""
	for rows.Next() {
		var recipe CatalogRecipe
		var selectorJSON, specJSON string
		if err := rows.Scan(
			&recipe.ID,
			&recipe.ItemID,
			&recipe.PlatformKey,
			&recipe.Operation,
			&recipe.Revision,
			&recipe.Support,
			&recipe.UnsupportedReason,
			&selectorJSON,
			&specJSON,
		); err != nil {
			return nil, fmt.Errorf("scan matching recipe: %w", err)
		}
		if err := strictJSON([]byte(selectorJSON), &recipe.Selector); err != nil {
			return nil, fmt.Errorf("decode recipe %q selector: %w", recipe.ID, err)
		}
		if err := strictJSON([]byte(specJSON), &recipe.Spec); err != nil {
			return nil, fmt.Errorf("decode recipe %q spec: %w", recipe.ID, err)
		}
		score, matches, err := selectorSpecificity(recipe.Selector, host)
		if err != nil {
			return nil, fmt.Errorf("match recipe %q: %w", recipe.ID, err)
		}
		if !matches {
			continue
		}
		if score > bestScore {
			copy := recipe
			best = &copy
			bestScore = score
			tiedRecipeID = ""
		} else if score == bestScore {
			tiedRecipeID = recipe.ID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matching recipes: %w", err)
	}
	if best == nil {
		return nil, ErrRecipeNotFound
	}
	if tiedRecipeID != "" {
		return nil, fmt.Errorf(
			"%w: %q and %q",
			ErrRecipeAmbiguous,
			best.ID,
			tiedRecipeID,
		)
	}
	return &ResolvedRecipe{
		CatalogVersion: catalog.metadata.CatalogVersion,
		Digest:         catalog.digest,
		Item:           item,
		Recipe:         *best,
		Specificity:    bestScore,
	}, nil
}

func selectorSpecificity(selector PlatformSelector, host HostProfile) (int, bool, error) {
	host.OSFamily = normalizeToken(host.OSFamily)
	host.DistroFamily = normalizeToken(host.DistroFamily)
	host.DistroID = normalizeToken(host.DistroID)
	host.Version = strings.TrimSpace(host.Version)
	host.Architecture = normalizeToken(host.Architecture)
	host.PackageManager = normalizeToken(host.PackageManager)
	host.ServiceManager = normalizeToken(host.ServiceManager)
	for index := range host.DistroLike {
		host.DistroLike[index] = normalizeToken(host.DistroLike[index])
	}
	if host.DistroFamily == "" {
		return 0, false, fmt.Errorf("host distro_family is required")
	}
	if _, ok := allowedDistroFamilies[host.DistroFamily]; !ok {
		return 0, false, fmt.Errorf("host uses unsupported distro_family %q", host.DistroFamily)
	}

	if selector.OSFamily != "" && normalizeToken(selector.OSFamily) != host.OSFamily {
		return 0, false, nil
	}
	if selector.DistroFamily != "" && normalizeToken(selector.DistroFamily) != host.DistroFamily {
		return 0, false, nil
	}
	if selector.PackageManager != "" &&
		(normalizeToken(selector.PackageManager) != host.PackageManager ||
			normalizeToken(selector.ServiceManager) != host.ServiceManager) {
		return 0, false, nil
	}
	if len(selector.Architectures) > 0 {
		matchesArchitecture := false
		for _, architecture := range selector.Architectures {
			if normalizeToken(architecture) == host.Architecture {
				matchesArchitecture = true
				break
			}
		}
		if !matchesArchitecture {
			return 0, false, nil
		}
	}
	if selector.Version != "" {
		if host.Version == "" {
			return 0, false, nil
		}
		matches, err := versionConstraintMatches(host.Version, selector.Version)
		if err != nil || !matches {
			return 0, false, err
		}
	}

	score := 0
	switch {
	case selector.DistroID != "":
		if normalizeToken(selector.DistroID) != host.DistroID {
			return 0, false, nil
		}
		score = 700
	case selector.DistroFamily != "":
		score = 500
	case selector.DistroLike != "":
		match := false
		for _, like := range host.DistroLike {
			if normalizeToken(selector.DistroLike) == like {
				match = true
				break
			}
		}
		if !match {
			return 0, false, nil
		}
		score = 400
	case selector.PackageManager != "":
		score = 200
	case selector.OSFamily != "":
		score = 100
	default:
		return 0, false, fmt.Errorf("selector has no explicit platform boundary")
	}
	if selector.Version != "" {
		score += 20
	}
	if len(selector.Architectures) > 0 {
		score += 10
	}
	return score, true, nil
}
