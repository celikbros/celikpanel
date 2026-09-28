package pdnsnative

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCaptureSQLiteSnapshotReadsCommittedWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `CREATE TABLE domains (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, last_check INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, `INSERT INTO domains (id,name,last_check) VALUES (1,'catalog.test',NULL)`); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	snapshot, err := CaptureSQLiteSnapshot(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Schema) != 2 || len(snapshot.Tables["domains"]) != 1 || len(snapshot.Tables["sqlite_sequence"]) != 1 || snapshot.Tables["domains"][0][1] != "catalog.test" {
		t.Fatalf("committed WAL row not captured: %+v", snapshot)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var recovered Snapshot
	if err := json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, recovered) {
		t.Fatal("SQLite observation changed across durable JSON journal round trip")
	}
}

func TestCaptureSQLiteSnapshotOrdersNativeWithoutRowidReceiptInWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	writer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE celikpanel_dns_zone_sync_receipts (
			domain TEXT NOT NULL PRIMARY KEY,
			request_id TEXT NOT NULL,
			qualifier TEXT NOT NULL,
			desired_generation INTEGER NOT NULL,
			action TEXT NOT NULL,
			zone_type TEXT NOT NULL,
			schema TEXT NOT NULL
		) STRICT, WITHOUT ROWID`,
		`INSERT INTO celikpanel_dns_zone_sync_receipts VALUES ('z.example','req-z','q-z',2,'upsert','NATIVE','v1')`,
		`INSERT INTO celikpanel_dns_zone_sync_receipts VALUES ('a.example','req-a','q-a',1,'upsert','NATIVE','v1')`,
	} {
		if _, err := writer.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	capture := func() Snapshot {
		tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		snapshot, err := CaptureSQLiteSnapshot(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	first, second := capture(), capture()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("WITHOUT ROWID snapshot was not stable")
	}
	rows := first.Tables["celikpanel_dns_zone_sync_receipts"]
	if len(rows) != 2 || rows[0][0] != "a.example" || rows[1][0] != "z.example" {
		t.Fatalf("receipt primary-key ordering failed: %#v", rows)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var durable Snapshot
	if err := json.Unmarshal(raw, &durable); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, durable) {
		t.Fatal("native receipt snapshot changed across journal JSON")
	}
}
