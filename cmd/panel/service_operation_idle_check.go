package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var errServiceOperationsNotIdle = errors.New("service operations are not idle")

const (
	durableServiceOperationSchemaVersion = 22
	serviceOperationDataSchemaVersion    = 31
)

const requiredPreOperationDataServiceOperationTableSQL = `CREATE TABLE service_operations (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	service_id TEXT NOT NULL,
	package_name TEXT,
	status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
	phase TEXT NOT NULL,
	result_json TEXT,
	error_code TEXT,
	error_message TEXT,
	requested_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	request_ip TEXT,
	user_agent TEXT,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	request_id TEXT
)`

const requiredServiceOperationTableSQL = `CREATE TABLE service_operations (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	service_id TEXT NOT NULL,
	package_name TEXT,
	status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
	phase TEXT NOT NULL,
	result_json TEXT,
	error_code TEXT,
	error_message TEXT,
	requested_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
	request_ip TEXT,
	user_agent TEXT,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	request_id TEXT,
	operation_data TEXT
)`

type serviceOperationColumnContract struct {
	position     int
	declaredType string
	notNull      bool
	primaryKey   int
	hasDefault   bool
	defaultSQL   string
}

var requiredServiceOperationColumns = map[string]serviceOperationColumnContract{
	"id":             {position: 0, declaredType: "TEXT", notNull: false, primaryKey: 1, hasDefault: false, defaultSQL: ""},
	"kind":           {position: 1, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"service_id":     {position: 2, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"package_name":   {position: 3, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"status":         {position: 4, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"phase":          {position: 5, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"result_json":    {position: 6, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"error_code":     {position: 7, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"error_message":  {position: 8, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"requested_by":   {position: 9, declaredType: "INTEGER", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"request_ip":     {position: 10, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"user_agent":     {position: 11, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"started_at":     {position: 12, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"finished_at":    {position: 13, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"created_at":     {position: 14, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"updated_at":     {position: 15, declaredType: "TEXT", notNull: true, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"request_id":     {position: 16, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
	"operation_data": {position: 17, declaredType: "TEXT", notNull: false, primaryKey: 0, hasDefault: false, defaultSQL: ""},
}

type serviceOperationIndexKeyContract struct {
	column     string
	expression bool
	descending bool
	collation  string
}

type serviceOperationIndexContract struct {
	unique    bool
	partial   bool
	keys      []serviceOperationIndexKeyContract
	schemaSQL string
}

var requiredServiceOperationIndexes = map[string]serviceOperationIndexContract{
	"idx_service_operations_one_active": {
		unique:  true,
		partial: true,
		keys: []serviceOperationIndexKeyContract{
			{expression: true, collation: "BINARY"},
		},
		schemaSQL: `CREATE UNIQUE INDEX idx_service_operations_one_active
			ON service_operations((1))
			WHERE status IN ('queued', 'running')`,
	},
	"idx_service_operations_recent": {
		unique:  false,
		partial: false,
		keys: []serviceOperationIndexKeyContract{
			{column: "started_at", descending: true, collation: "BINARY"},
		},
		schemaSQL: `CREATE INDEX idx_service_operations_recent
			ON service_operations(started_at DESC)`,
	},
	"idx_service_operations_request_id": {
		unique:  true,
		partial: true,
		keys: []serviceOperationIndexKeyContract{
			{column: "request_id", collation: "BINARY"},
		},
		schemaSQL: `CREATE UNIQUE INDEX idx_service_operations_request_id
			ON service_operations(request_id)
			WHERE request_id IS NOT NULL`,
	},
}

// checkServiceOperationsIdle opens the panel database in SQLite read-only mode.
// Release tooling uses it after stopping the panel; it never migrates, repairs,
// deletes or completes an operation row.
// checkServiceOperationsIdle panel veritabanını SQLite salt-okunur modunda
// açar. Sürüm araçları bunu paneli durdurduktan sonra kullanır; hiçbir işlem
// satırını migrate etmez, onarmaz, silmez veya tamamlamaz.
func checkServiceOperationsIdle(databasePath string) error {
	databasePath = filepath.Clean(databasePath)
	if !filepath.IsAbs(databasePath) {
		absolutePath, err := filepath.Abs(databasePath)
		if err != nil {
			return fmt.Errorf("%w: resolve panel database path: %v", errServiceOperationsNotIdle, err)
		}
		databasePath = absolutePath
	}
	pinnedDatabase, err := pinPanelDatabase(databasePath)
	if err != nil {
		return err
	}
	defer pinnedDatabase.close()

	uri := &url.URL{Scheme: "file", Path: pinnedDatabase.sqlitePath()}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Set("immutable", "1")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return fmt.Errorf("%w: open panel database read-only: %v", errServiceOperationsNotIdle, err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("%w: read panel database: %v", errServiceOperationsNotIdle, err)
	}
	if err := pinnedDatabase.verifyPath(); err != nil {
		return err
	}
	var quickCheck string
	if err := database.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&quickCheck); err != nil {
		return fmt.Errorf("%w: run panel database quick check: %v", errServiceOperationsNotIdle, err)
	}
	if quickCheck != "ok" {
		return fmt.Errorf("%w: panel database quick check returned %q", errServiceOperationsNotIdle, quickCheck)
	}

	migrationRows, err := database.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return fmt.Errorf("%w: inspect migration history: %v", errServiceOperationsNotIdle, err)
	}
	expectedVersion := 1
	for migrationRows.Next() {
		var version int
		if err := migrationRows.Scan(&version); err != nil {
			migrationRows.Close()
			return fmt.Errorf("%w: read migration history: %v", errServiceOperationsNotIdle, err)
		}
		if version != expectedVersion {
			migrationRows.Close()
			return fmt.Errorf("%w: migration history is not contiguous", errServiceOperationsNotIdle)
		}
		expectedVersion++
	}
	if err := migrationRows.Err(); err != nil {
		migrationRows.Close()
		return fmt.Errorf("%w: iterate migration history: %v", errServiceOperationsNotIdle, err)
	}
	if err := migrationRows.Close(); err != nil {
		return fmt.Errorf("%w: close migration history: %v", errServiceOperationsNotIdle, err)
	}
	schemaVersion := expectedVersion - 1
	requiredTableSQL := requiredServiceOperationTableSQL
	requiredColumnCount := len(requiredServiceOperationColumns)
	if schemaVersion < serviceOperationDataSchemaVersion {
		requiredTableSQL = requiredPreOperationDataServiceOperationTableSQL
		requiredColumnCount--
	}
	if schemaVersion < durableServiceOperationSchemaVersion {
		return fmt.Errorf("%w: expected schema version %d or newer", errServiceOperationsNotIdle, durableServiceOperationSchemaVersion)
	}

	var tableCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='service_operations'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("%w: inspect service operation schema: %v", errServiceOperationsNotIdle, err)
	}
	if tableCount != 1 {
		return fmt.Errorf("%w: service operation schema is unavailable", errServiceOperationsNotIdle)
	}
	if err := validateServiceOperationTableSQL(ctx, database, requiredTableSQL); err != nil {
		return fmt.Errorf("%w: inspect service operation table SQL: %v", errServiceOperationsNotIdle, err)
	}
	columns, err := readServiceOperationColumns(ctx, database)
	if err != nil {
		return fmt.Errorf("%w: inspect service operation columns: %v", errServiceOperationsNotIdle, err)
	}
	if len(columns) != requiredColumnCount {
		return fmt.Errorf("%w: service operation column count has an invalid contract", errServiceOperationsNotIdle)
	}
	for requiredColumn, requiredContract := range requiredServiceOperationColumns {
		if requiredColumn == "operation_data" && schemaVersion < serviceOperationDataSchemaVersion {
			continue
		}
		contract, ok := columns[requiredColumn]
		if !ok {
			return fmt.Errorf("%w: service operation column %s is unavailable", errServiceOperationsNotIdle, requiredColumn)
		}
		if contract != requiredContract {
			return fmt.Errorf("%w: service operation column %s has an invalid contract", errServiceOperationsNotIdle, requiredColumn)
		}
	}

	for requiredIndex, requiredContract := range requiredServiceOperationIndexes {
		if err := validateServiceOperationIndex(ctx, database, requiredIndex, requiredContract); err != nil {
			return fmt.Errorf("%w: service operation index %s: %v", errServiceOperationsNotIdle, requiredIndex, err)
		}
	}

	var id, status string
	err = database.QueryRowContext(
		ctx,
		`SELECT id, status FROM service_operations
		 WHERE status IN (?, ?)
		 ORDER BY created_at ASC LIMIT 1`,
		serviceOperationQueued,
		serviceOperationRunning,
	).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return pinnedDatabase.verifyPath()
	}
	if err != nil {
		return fmt.Errorf("%w: inspect active service operations: %v", errServiceOperationsNotIdle, err)
	}
	return fmt.Errorf("%w: operation %s is %s", errServiceOperationsNotIdle, id, status)
}

func readServiceOperationColumns(
	ctx context.Context,
	database *sql.DB,
) (map[string]serviceOperationColumnContract, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT cid, name, type, "notnull", dflt_value, pk
		 FROM pragma_table_info('service_operations') ORDER BY cid ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make(map[string]serviceOperationColumnContract)
	for rows.Next() {
		var position, notNullFlag, primaryKey int
		var name, declaredType string
		var defaultSQL sql.NullString
		if err := rows.Scan(
			&position, &name, &declaredType, &notNullFlag, &defaultSQL, &primaryKey,
		); err != nil {
			return nil, err
		}
		if name == "" || (notNullFlag != 0 && notNullFlag != 1) || primaryKey < 0 {
			return nil, fmt.Errorf("service operation column has an invalid contract")
		}
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("service operation column %s is duplicated", name)
		}
		columns[name] = serviceOperationColumnContract{
			position:     position,
			declaredType: strings.ToUpper(strings.TrimSpace(declaredType)),
			notNull:      notNullFlag == 1,
			primaryKey:   primaryKey,
			hasDefault:   defaultSQL.Valid,
			defaultSQL:   defaultSQL.String,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func validateServiceOperationTableSQL(
	ctx context.Context,
	database *sql.DB,
	requiredTableSQL string,
) error {
	var schemaSQL string
	if err := database.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_master
		 WHERE type='table' AND name='service_operations'`,
	).Scan(&schemaSQL); err != nil {
		return err
	}
	normalized := normalizeSQLiteSchemaSQL(schemaSQL)
	expected := normalizeSQLiteSchemaSQL(requiredTableSQL)
	if normalized != expected {
		return fmt.Errorf(
			"service operation table SQL does not match the required contract: actual=%q expected=%q",
			normalized,
			expected,
		)
	}
	return nil
}

func validateServiceOperationIndex(
	ctx context.Context,
	database *sql.DB,
	name string,
	required serviceOperationIndexContract,
) error {
	var uniqueFlag, partialFlag int
	err := database.QueryRowContext(
		ctx,
		`SELECT "unique", partial FROM pragma_index_list('service_operations')
		 WHERE name=?`,
		name,
	).Scan(&uniqueFlag, &partialFlag)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("required index is unavailable")
	}
	if err != nil {
		return err
	}
	if (uniqueFlag != 0 && uniqueFlag != 1) || (partialFlag != 0 && partialFlag != 1) {
		return fmt.Errorf("index flags are invalid")
	}
	if uniqueFlag == 1 != required.unique || partialFlag == 1 != required.partial {
		return fmt.Errorf("index flags do not match the required contract")
	}

	var schemaSQL string
	if err := database.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_master
		 WHERE type='index' AND name=? AND tbl_name='service_operations'`,
		name,
	).Scan(&schemaSQL); err != nil {
		return err
	}
	if normalizeSQLiteSchemaSQL(schemaSQL) != normalizeSQLiteSchemaSQL(required.schemaSQL) {
		return fmt.Errorf("index SQL does not match the required contract")
	}

	rows, err := database.QueryContext(
		ctx,
		`SELECT seqno, cid, name, "desc", coll
		 FROM pragma_index_xinfo(?) WHERE "key"=1 ORDER BY seqno ASC`,
		name,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	keyPosition := 0
	for rows.Next() {
		var sequence, columnID, descendingFlag int
		var column, collation sql.NullString
		if err := rows.Scan(&sequence, &columnID, &column, &descendingFlag, &collation); err != nil {
			return err
		}
		if sequence != keyPosition || (descendingFlag != 0 && descendingFlag != 1) || !collation.Valid {
			return fmt.Errorf("index key metadata is invalid")
		}
		actual := serviceOperationIndexKeyContract{
			descending: descendingFlag == 1,
			collation:  strings.ToUpper(strings.TrimSpace(collation.String)),
		}
		switch {
		case columnID == -2 && !column.Valid:
			actual.expression = true
		case columnID >= 0 && column.Valid && column.String != "":
			actual.column = column.String
		default:
			return fmt.Errorf("index key target is invalid")
		}
		if keyPosition >= len(required.keys) || actual != required.keys[keyPosition] {
			return fmt.Errorf("index key %d does not match the required contract", keyPosition)
		}
		keyPosition++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if keyPosition != len(required.keys) {
		return fmt.Errorf("index key count does not match the required contract")
	}
	return nil
}

func normalizeSQLiteSchemaSQL(value string) string {
	var normalized strings.Builder
	runes := []rune(value)
	var closingQuote rune
	var previous rune
	hasPrevious := false
	pendingSpace := false
	for i := 0; i < len(runes); i++ {
		current := runes[i]
		if closingQuote == 0 && unicode.IsSpace(current) {
			pendingSpace = normalized.Len() > 0
			continue
		}
		if pendingSpace && hasPrevious && sqliteSchemaTokenBoundaryRune(previous) && sqliteSchemaTokenBoundaryRune(current) {
			normalized.WriteRune(' ')
		}
		pendingSpace = false
		if closingQuote != 0 {
			normalized.WriteRune(current)
			previous = current
			hasPrevious = true
			if current == closingQuote {
				if i+1 < len(runes) && runes[i+1] == closingQuote {
					i++
					normalized.WriteRune(runes[i])
					previous = runes[i]
				} else {
					closingQuote = 0
				}
			}
			continue
		}
		switch current {
		case '\'', '"', '`':
			closingQuote = current
			normalized.WriteRune(current)
			previous = current
			hasPrevious = true
		case '[':
			closingQuote = ']'
			normalized.WriteRune(current)
			previous = current
			hasPrevious = true
		default:
			previous = unicode.ToLower(current)
			normalized.WriteRune(previous)
			hasPrevious = true
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(normalized.String()), ";"))
}

func sqliteSchemaTokenBoundaryRune(value rune) bool {
	return unicode.IsLetter(value) ||
		unicode.IsDigit(value) ||
		value == '_' ||
		value == '$' ||
		value == '\'' ||
		value == '"' ||
		value == '`' ||
		value == '[' ||
		value == ']'
}

type pinnedPanelDatabase struct {
	path           string
	baseName       string
	directory      *os.File
	file           *os.File
	info           os.FileInfo
	sidecars       map[string]*pinnedSQLiteSidecar
	descriptorOnly bool
}

type pinnedSQLiteSidecar struct {
	file *os.File
	info os.FileInfo
}

func pinPanelDatabase(databasePath string) (*pinnedPanelDatabase, error) {
	return pinPanelDatabaseWithWALPolicy(databasePath, false)
}

func pinWALAwarePanelDatabase(databasePath string) (*pinnedPanelDatabase, error) {
	if isLinuxProcSelfFDPath(databasePath) {
		return nil, fmt.Errorf(
			"%w: WAL-aware panel database proof requires a canonical database path",
			errServiceOperationsNotIdle,
		)
	}
	return pinPanelDatabaseWithWALPolicy(databasePath, true)
}

func pinPanelDatabaseWithWALPolicy(databasePath string, allowNonEmptyWAL bool) (*pinnedPanelDatabase, error) {
	if isLinuxProcSelfFDPath(databasePath) {
		return pinPanelDatabaseDescriptor(databasePath)
	}

	directoryPath := filepath.Dir(databasePath)
	directoryPathInfo, err := os.Lstat(directoryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: inspect panel database directory: %v", errServiceOperationsNotIdle, err)
	}
	if !directoryPathInfo.IsDir() {
		if !isLinuxProcSelfFDDirectory(directoryPath) || directoryPathInfo.Mode()&os.ModeSymlink == 0 {
			return nil, fmt.Errorf("%w: panel database directory must be a directory", errServiceOperationsNotIdle)
		}
		directoryPathInfo, err = os.Stat(directoryPath)
		if err != nil {
			return nil, fmt.Errorf("%w: inspect pinned panel database directory target: %v", errServiceOperationsNotIdle, err)
		}
		if !directoryPathInfo.IsDir() {
			return nil, fmt.Errorf("%w: pinned panel database directory target must be a directory", errServiceOperationsNotIdle)
		}
	}
	pinnedDirectory, err := os.Open(directoryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: pin panel database directory: %v", errServiceOperationsNotIdle, err)
	}
	pinnedDirectoryInfo, err := pinnedDirectory.Stat()
	if err != nil {
		pinnedDirectory.Close()
		return nil, fmt.Errorf("%w: inspect pinned panel database directory: %v", errServiceOperationsNotIdle, err)
	}
	if !pinnedDirectoryInfo.IsDir() || !os.SameFile(directoryPathInfo, pinnedDirectoryInfo) {
		pinnedDirectory.Close()
		return nil, fmt.Errorf("%w: panel database directory changed while it was pinned", errServiceOperationsNotIdle)
	}

	pinned := &pinnedPanelDatabase{
		path:      databasePath,
		baseName:  filepath.Base(databasePath),
		directory: pinnedDirectory,
		sidecars:  make(map[string]*pinnedSQLiteSidecar),
	}
	pathInfo, err := os.Lstat(pinned.databaseEntryPath())
	if err != nil {
		pinned.close()
		return nil, fmt.Errorf("%w: inspect panel database: %v", errServiceOperationsNotIdle, err)
	}
	if !pathInfo.Mode().IsRegular() {
		pinned.close()
		return nil, fmt.Errorf("%w: panel database must be a regular file", errServiceOperationsNotIdle)
	}
	pinned.file, err = os.Open(pinned.databaseEntryPath())
	if err != nil {
		pinned.close()
		return nil, fmt.Errorf("%w: pin panel database: %v", errServiceOperationsNotIdle, err)
	}
	pinned.info, err = pinned.file.Stat()
	if err != nil {
		pinned.close()
		return nil, fmt.Errorf("%w: inspect pinned panel database: %v", errServiceOperationsNotIdle, err)
	}
	if !pinned.info.Mode().IsRegular() || !os.SameFile(pathInfo, pinned.info) {
		pinned.close()
		return nil, fmt.Errorf("%w: panel database path changed while it was pinned", errServiceOperationsNotIdle)
	}
	if err := pinned.pinSidecars(allowNonEmptyWAL); err != nil {
		pinned.close()
		return nil, err
	}
	if err := pinned.verifyPath(); err != nil {
		pinned.close()
		return nil, err
	}
	return pinned, nil
}

// pinPanelDatabaseDescriptor accepts only an exact Linux /proc/self/fd/<n> path.
// The trusted snapshot/restore layer already proves that SQLite sidecars are absent;
// this layer keeps validating the same inherited descriptor instead of walking back
// through a replaceable pathname.
// pinPanelDatabaseDescriptor yalnızca tam bir Linux /proc/self/fd/<n> yolunu kabul eder.
// Güvenilir snapshot/restore katmanı SQLite yan dosyalarının bulunmadığını önceden
// kanıtlar; bu katman değiştirilebilir bir dosya yoluna geri dönmek yerine aynı miras
// alınmış descriptor'ı doğrulamayı sürdürür.
func pinPanelDatabaseDescriptor(databasePath string) (*pinnedPanelDatabase, error) {
	pathInfo, err := os.Stat(databasePath)
	if err != nil {
		return nil, fmt.Errorf("%w: inspect pinned panel database descriptor: %v", errServiceOperationsNotIdle, err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: pinned panel database descriptor must name a regular file", errServiceOperationsNotIdle)
	}

	file, err := os.Open(databasePath)
	if err != nil {
		return nil, fmt.Errorf("%w: duplicate pinned panel database descriptor: %v", errServiceOperationsNotIdle, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("%w: inspect duplicated panel database descriptor: %v", errServiceOperationsNotIdle, err)
	}
	if !info.Mode().IsRegular() || !samePinnedSQLiteFileMetadata(pathInfo, info) {
		file.Close()
		return nil, fmt.Errorf("%w: panel database descriptor changed while it was duplicated", errServiceOperationsNotIdle)
	}

	pinned := &pinnedPanelDatabase{
		path:           databasePath,
		file:           file,
		info:           info,
		sidecars:       make(map[string]*pinnedSQLiteSidecar),
		descriptorOnly: true,
	}
	if err := pinned.verifyPath(); err != nil {
		pinned.close()
		return nil, err
	}
	return pinned, nil
}

func isLinuxProcSelfFDPath(path string) bool {
	if runtime.GOOS != "linux" || filepath.Clean(path) != path || filepath.Dir(path) != "/proc/self/fd" {
		return false
	}
	fd, err := strconv.Atoi(filepath.Base(path))
	return err == nil && fd >= 0
}

func isLinuxProcSelfFDDirectory(path string) bool {
	return isLinuxProcSelfFDPath(path)
}

func (p *pinnedPanelDatabase) close() {
	for _, sidecar := range p.sidecars {
		if sidecar != nil && sidecar.file != nil {
			_ = sidecar.file.Close()
		}
	}
	if p.file != nil {
		_ = p.file.Close()
	}
	if p.directory != nil {
		_ = p.directory.Close()
	}
}

func (p *pinnedPanelDatabase) databaseEntryPath() string {
	if p.descriptorOnly {
		return p.path
	}
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("/proc/self/fd/%d/%s", p.directory.Fd(), p.baseName)
	}
	return p.path
}

func (p *pinnedPanelDatabase) siblingPath(suffix string) string {
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("/proc/self/fd/%d/%s%s", p.directory.Fd(), p.baseName, suffix)
	}
	return p.path + suffix
}

func (p *pinnedPanelDatabase) pinSidecars(allowNonEmptyWAL bool) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		pathInfo, err := os.Lstat(p.siblingPath(suffix))
		if errors.Is(err, os.ErrNotExist) {
			p.sidecars[suffix] = nil
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: inspect SQLite sidecar %s: %v", errServiceOperationsNotIdle, suffix, err)
		}
		if !pathInfo.Mode().IsRegular() {
			return fmt.Errorf("%w: SQLite sidecar %s must be a regular file", errServiceOperationsNotIdle, suffix)
		}

		file, err := os.Open(p.siblingPath(suffix))
		if err != nil {
			return fmt.Errorf("%w: pin SQLite sidecar %s: %v", errServiceOperationsNotIdle, suffix, err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return fmt.Errorf("%w: inspect pinned SQLite sidecar %s: %v", errServiceOperationsNotIdle, suffix, err)
		}
		if !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
			file.Close()
			return fmt.Errorf("%w: SQLite sidecar %s changed while it was pinned", errServiceOperationsNotIdle, suffix)
		}
		if suffix == "-wal" && info.Size() != 0 && !allowNonEmptyWAL {
			file.Close()
			return fmt.Errorf("%w: SQLite WAL contains uncheckpointed data", errServiceOperationsNotIdle)
		}
		if suffix == "-journal" {
			file.Close()
			return fmt.Errorf("%w: SQLite rollback journal is present", errServiceOperationsNotIdle)
		}
		p.sidecars[suffix] = &pinnedSQLiteSidecar{
			file: file,
			info: info,
		}
	}
	return nil
}

func (p *pinnedPanelDatabase) sqlitePath() string {
	if runtime.GOOS == "linux" {
		return fmt.Sprintf("/proc/self/fd/%d", p.file.Fd())
	}
	uriPath := filepath.ToSlash(p.path)
	if filepath.VolumeName(p.path) != "" && uriPath[0] != '/' {
		uriPath = "/" + uriPath
	}
	return uriPath
}

func (p *pinnedPanelDatabase) verifyPath() error {
	var (
		pathInfo os.FileInfo
		err      error
	)
	if p.descriptorOnly {
		pathInfo, err = os.Stat(p.databaseEntryPath())
	} else {
		pathInfo, err = os.Lstat(p.databaseEntryPath())
	}
	if err != nil {
		return fmt.Errorf("%w: verify panel database path: %v", errServiceOperationsNotIdle, err)
	}
	currentInfo, err := p.file.Stat()
	if err != nil {
		return fmt.Errorf("%w: inspect pinned panel database: %v", errServiceOperationsNotIdle, err)
	}
	if !pathInfo.Mode().IsRegular() || !currentInfo.Mode().IsRegular() ||
		!samePinnedSQLiteFileMetadata(p.info, pathInfo) ||
		!samePinnedSQLiteFileMetadata(p.info, currentInfo) {
		return fmt.Errorf("%w: panel database path changed after SQLite opened it", errServiceOperationsNotIdle)
	}
	if p.descriptorOnly {
		return nil
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		sidecar := p.sidecars[suffix]
		pathInfo, err := os.Lstat(p.siblingPath(suffix))
		if sidecar == nil {
			if err == nil {
				return fmt.Errorf("%w: SQLite sidecar %s appeared after pinning", errServiceOperationsNotIdle, suffix)
			}
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%w: inspect SQLite sidecar %s: %v", errServiceOperationsNotIdle, suffix, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: verify SQLite sidecar %s: %v", errServiceOperationsNotIdle, suffix, err)
		}
		currentInfo, err := sidecar.file.Stat()
		if err != nil {
			return fmt.Errorf("%w: inspect pinned SQLite sidecar %s: %v", errServiceOperationsNotIdle, suffix, err)
		}
		if !pathInfo.Mode().IsRegular() || !currentInfo.Mode().IsRegular() ||
			!samePinnedSQLiteFileMetadata(sidecar.info, pathInfo) ||
			!samePinnedSQLiteFileMetadata(sidecar.info, currentInfo) {
			return fmt.Errorf("%w: SQLite sidecar %s changed after pinning", errServiceOperationsNotIdle, suffix)
		}
	}
	return nil
}

const preLedgerServiceOperationSchemaVersion = 20

// checkPreLedgerServiceOperationsIdle accepts only the exact migration history through version 20 and rejects
// any partial durable service queue objects.
// checkPreLedgerServiceOperationsIdle, yalnızca 20. sürüme kadarki tam migration geçmişini kabul eder ve
// kalıcı servis kuyruğuna ait yarım kalmış bütün nesneleri reddeder.
func checkPreLedgerServiceOperationsIdle(databasePath string) error {
	databasePath = filepath.Clean(databasePath)
	if !filepath.IsAbs(databasePath) {
		absolutePath, err := filepath.Abs(databasePath)
		if err != nil {
			return fmt.Errorf("%w: resolve panel database path: %v", errServiceOperationsNotIdle, err)
		}
		databasePath = absolutePath
	}
	pinnedDatabase, err := pinPanelDatabase(databasePath)
	if err != nil {
		return err
	}
	defer pinnedDatabase.close()

	uri := &url.URL{Scheme: "file", Path: pinnedDatabase.sqlitePath()}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Set("immutable", "1")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return fmt.Errorf("%w: open panel database read-only: %v", errServiceOperationsNotIdle, err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("%w: read panel database: %v", errServiceOperationsNotIdle, err)
	}
	if err := pinnedDatabase.verifyPath(); err != nil {
		return err
	}
	var quickCheck string
	if err := database.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&quickCheck); err != nil {
		return fmt.Errorf("%w: run panel database quick check: %v", errServiceOperationsNotIdle, err)
	}
	if quickCheck != "ok" {
		return fmt.Errorf("%w: panel database quick check returned %q", errServiceOperationsNotIdle, quickCheck)
	}

	rows, err := database.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return fmt.Errorf("%w: inspect migration history: %v", errServiceOperationsNotIdle, err)
	}
	expectedVersion := 1
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("%w: read migration history: %v", errServiceOperationsNotIdle, err)
		}
		if version != expectedVersion {
			rows.Close()
			return fmt.Errorf("%w: migration history is not the exact pre-ledger sequence", errServiceOperationsNotIdle)
		}
		expectedVersion++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: iterate migration history: %v", errServiceOperationsNotIdle, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("%w: close migration history: %v", errServiceOperationsNotIdle, err)
	}
	if expectedVersion != preLedgerServiceOperationSchemaVersion+1 {
		return fmt.Errorf("%w: expected schema version %d", errServiceOperationsNotIdle, preLedgerServiceOperationSchemaVersion)
	}

	var serviceOperationObjects int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		WHERE (type = 'table' AND name = 'service_operations')
		   OR name LIKE 'idx_service_operations_%'`).Scan(&serviceOperationObjects); err != nil {
		return fmt.Errorf("%w: inspect pre-ledger schema objects: %v", errServiceOperationsNotIdle, err)
	}
	if serviceOperationObjects != 0 {
		return fmt.Errorf("%w: partial service operation schema is present", errServiceOperationsNotIdle)
	}
	return pinnedDatabase.verifyPath()
}
