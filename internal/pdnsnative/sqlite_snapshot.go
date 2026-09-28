package pdnsnative

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var sqliteTableName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// CaptureSQLiteSnapshot reads all application tables and schema in one
// caller-owned read transaction, including committed WAL content. The caller
// remains responsible for securing the SQLite path, sidecars and file identity.
func CaptureSQLiteSnapshot(ctx context.Context, tx *sql.Tx) (Snapshot, error) {
	if tx == nil {
		return Snapshot{}, errors.New("PowerDNS SQLite transaction is absent")
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		if err == nil {
			err = errors.New("PowerDNS SQLite integrity check failed")
		}
		return Snapshot{}, err
	}
	snapshot := Snapshot{Tables: make(map[string][][]any)}
	rows, err := tx.QueryContext(ctx, `SELECT type, name, tbl_name, sql FROM sqlite_master WHERE type = 'table' OR (type = 'index' AND sql IS NOT NULL) ORDER BY type, name`)
	if err != nil {
		return Snapshot{}, err
	}
	for rows.Next() {
		var item [4]string
		if err := rows.Scan(&item[0], &item[1], &item[2], &item[3]); err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		snapshot.Schema = append(snapshot.Schema, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Snapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return Snapshot{}, err
	}
	for _, item := range snapshot.Schema {
		if item[0] != "table" {
			continue
		}
		if !sqliteTableName.MatchString(item[1]) {
			return Snapshot{}, errors.New("PowerDNS SQLite table name is unsupported")
		}
		orderBy, err := sqliteSnapshotOrder(ctx, tx, item[1])
		if err != nil {
			return Snapshot{}, err
		}
		tableRows, err := tx.QueryContext(ctx, `SELECT * FROM "`+item[1]+`" ORDER BY `+orderBy)
		if err != nil {
			return Snapshot{}, err
		}
		columns, err := tableRows.Columns()
		if err != nil {
			tableRows.Close()
			return Snapshot{}, err
		}
		all := make([][]any, 0)
		for tableRows.Next() {
			values := make([]any, len(columns))
			addresses := make([]any, len(columns))
			for i := range values {
				addresses[i] = &values[i]
			}
			if err := tableRows.Scan(addresses...); err != nil {
				tableRows.Close()
				return Snapshot{}, err
			}
			for i, value := range values {
				switch typed := value.(type) {
				case int64:
					// Journal JSON decodes numbers as float64. Refuse values that
					// cannot survive that exact durable round trip.
					if typed < -(1<<53) || typed > 1<<53 {
						tableRows.Close()
						return Snapshot{}, errors.New("PowerDNS SQLite integer exceeds exact journal range")
					}
					values[i] = float64(typed)
				case []byte:
					// Native catalog fixtures contain only integer and text.
					// BLOB data has no measured V3 transformation contract.
					tableRows.Close()
					return Snapshot{}, errors.New("PowerDNS SQLite BLOB is unsupported by the native catalog proof")
				}
			}
			all = append(all, values)
		}
		if err := tableRows.Err(); err != nil {
			tableRows.Close()
			return Snapshot{}, err
		}
		if err := tableRows.Close(); err != nil {
			return Snapshot{}, err
		}
		snapshot.Tables[item[1]] = all
	}
	return snapshot, nil
}

// SQLite WITHOUT ROWID tables do not have the synthetic rowid used by ordinary
// PowerDNS tables. Their declared primary key is unique and supplies a stable
// total row order for the same native pre/post observation.
func sqliteSnapshotOrder(ctx context.Context, tx *sql.Tx, table string) (string, error) {
	if !sqliteTableName.MatchString(table) {
		return "", errors.New("PowerDNS SQLite table name is unsupported")
	}
	list, err := tx.QueryContext(ctx, "PRAGMA table_list")
	if err != nil {
		return "", err
	}
	found, withoutRowid := false, false
	for list.Next() {
		var schema, name, kind string
		var columnCount, wr, strict int
		if err := list.Scan(&schema, &name, &kind, &columnCount, &wr, &strict); err != nil {
			list.Close()
			return "", err
		}
		if schema == "main" && name == table {
			if found || kind != "table" || (wr != 0 && wr != 1) {
				list.Close()
				return "", errors.New("PowerDNS SQLite table classification is ambiguous")
			}
			found, withoutRowid = true, wr == 1
		}
	}
	if err := list.Err(); err != nil {
		list.Close()
		return "", err
	}
	if err := list.Close(); err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("PowerDNS SQLite table disappeared during snapshot")
	}
	if !withoutRowid {
		return "rowid", nil
	}
	info, err := tx.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
	if err != nil {
		return "", err
	}
	type keyColumn struct {
		rank int
		name string
	}
	var keys []keyColumn
	for info.Next() {
		var cid, notNull, rank int
		var name, declaredType string
		var defaultValue sql.NullString
		if err := info.Scan(&cid, &name, &declaredType, &notNull, &defaultValue, &rank); err != nil {
			info.Close()
			return "", err
		}
		if rank > 0 {
			if name == "" || strings.ContainsRune(name, 0) {
				info.Close()
				return "", errors.New("PowerDNS SQLite primary key column is unsupported")
			}
			keys = append(keys, keyColumn{rank: rank, name: name})
		}
	}
	if err := info.Err(); err != nil {
		info.Close()
		return "", err
	}
	if err := info.Close(); err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", errors.New("PowerDNS SQLite WITHOUT ROWID table has no primary key")
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].rank < keys[j].rank })
	ordered := make([]string, len(keys))
	for i, key := range keys {
		if key.rank != i+1 {
			return "", fmt.Errorf("PowerDNS SQLite primary key rank %d is invalid", key.rank)
		}
		ordered[i] = `"` + strings.ReplaceAll(key.name, `"`, `""`) + `"`
	}
	return strings.Join(ordered, ", "), nil
}
