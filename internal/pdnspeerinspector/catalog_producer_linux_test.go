//go:build linux

package pdnspeerinspector

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
)

// The consumed catalog comes from a CelikPanel primary of either engine. The
// inspector accepts the BIND producer (TTL 60, SHA-224 labels) and the native
// PowerDNS producer (TTL 0, base32hex labels), each only when consistent.
func TestCatalogDatabaseAcceptsEitherKnownProducerConsistently(t *testing.T) {
	r, p, _ := fixture(t)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	const member = "s1-kill.test"
	sha, err := binddns.CatalogMemberLabel(member)
	if err != nil {
		t.Fatal(err)
	}
	const pdnsLabel = "lf5eijnqp9ob8kmq5mv0vhjaevtcfuus"
	type row struct {
		name, typ, content string
		ttl                int
	}
	base := func(versionTTL int) []row {
		return []row{
			{catalog, "SOA", "invalid invalid 2 60 30 3600 30", 60},
			{catalog, "NS", "invalid", 60},
			{"version." + catalog, "TXT", `"2"`, versionTTL},
		}
	}
	for _, tc := range []struct {
		name string
		rows []row
		ok   bool
	}{
		{"BIND producer", append(base(60), row{sha + ".zones." + catalog, "PTR", member, 60}), true},
		{"PowerDNS producer", append(base(0), row{pdnsLabel + ".zones." + catalog, "PTR", member, 0}), true},
		{"PowerDNS label with BIND TTL", append(base(60), row{pdnsLabel + ".zones." + catalog, "PTR", member, 60}), false},
		{"SHA-224 label with PowerDNS TTL", append(base(0), row{sha + ".zones." + catalog, "PTR", member, 0}), false},
		{"metadata TTLs disagree", append(base(60), row{pdnsLabel + ".zones." + catalog, "PTR", member, 0}), false},
		{"foreign TTL", append(base(30), row{sha + ".zones." + catalog, "PTR", member, 30}), false},
		{"non-base32hex label", append(base(0), row{"wf5eijnqp9ob8kmq5mv0vhjaevtcfuus.zones." + catalog, "PTR", member, 0}), false},
		{"duplicate label", append(base(0),
			row{pdnsLabel + ".zones." + catalog, "PTR", member, 0},
			row{pdnsLabel + ".zones." + catalog, "PTR", "other.test", 0}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pdns.sqlite3")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				"CREATE TABLE domains(id INTEGER PRIMARY KEY,name TEXT,type TEXT,master TEXT,account TEXT,catalog TEXT)",
				"CREATE TABLE records(domain_id INTEGER,name TEXT,type TEXT,content TEXT,ttl INTEGER)",
				"INSERT INTO domains VALUES(1,'" + catalog + "','CONSUMER','192.0.2.10','fixture-pdns-peer',NULL)",
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			for _, record := range tc.rows {
				if _, err := db.Exec("INSERT INTO records VALUES(1,?,?,?,?)", record.name, record.typ, record.content, record.ttl); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			info := st.Sys().(*syscall.Stat_t)
			serial, members, _, err := readCatalogDatabase(context.Background(), path, uint64(info.Dev), info.Ino, r, p)
			if !tc.ok {
				if err == nil {
					t.Fatalf("inconsistent catalog accepted: %d %v", serial, members)
				}
				return
			}
			if err != nil || serial != 2 || len(members) != 1 || members[0] != member {
				t.Fatalf("serial=%d members=%v err=%v", serial, members, err)
			}
		})
	}
}
