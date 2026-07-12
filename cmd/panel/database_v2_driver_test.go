package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

// Guards AUTOPSY A1: the v2 DB handlers once branched on TypeID==23/24, which
// matched NOTHING in the shipped seed (1=postgresql, 2=mariadb) — both branches
// were dead and the driver received an empty type. The fix maps the canonical
// engine NAME to the driver type; this test locks that mapping so the dead-code
// regression cannot return, and documents the accepted synonyms.
//
// AUTOPSY A1 muhafızı: v2 DB handler'ları bir zamanlar TypeID==23/24'e
// dallanıyordu; bu, dağıtılan tohumla (1=postgresql, 2=mariadb) HİÇ eşleşmiyordu
// — iki dal da ölüydü, sürücüye boş tip gidiyordu. Düzeltme kanonik motor ADINI
// sürücü tipine eşler; bu test o eşlemeyi kilitler ki ölü-kod regresyonu geri
// dönemesin ve kabul edilen eş anlamlıları belgeler.
func TestDBDriverTypeFor(t *testing.T) {
	cases := []struct {
		typeName string
		want     string
	}{
		{"postgresql", "postgresql"},
		{"PostgreSQL", "postgresql"}, // display_name casing must not matter
		{"postgres", "postgresql"},   // synonym
		{"mariadb", "mariadb"},
		{"MariaDB", "mariadb"},
		{"mysql", "mariadb"}, // MySQL is driven as MariaDB
		{"", ""},             // empty stays empty (honest, not a silent default)
	}
	for _, c := range cases {
		got := dbDriverTypeFor(&core.DatabaseServer{TypeName: c.typeName})
		if got != c.want {
			t.Errorf("dbDriverTypeFor(TypeName=%q) = %q, want %q", c.typeName, got, c.want)
		}
	}
}
