package main

import (
	"os"
	"path/filepath"
	"testing"
)

// stageLegacyDKIMStore points the legacy paths at a temporary directory and
// restores them afterwards, so a test can exercise the one-shot upgrade without
// touching a real /etc.
// stageLegacyDKIMStore, eski yolları geçici bir dizine yöneltir ve sonra geri
// alır; böylece bir test, gerçek bir /etc'ye dokunmadan tek seferlik yükseltmeyi
// sınayabilir.
func stageLegacyDKIMStore(t *testing.T) (legacyKeys, legacyTables, destination string) {
	t.Helper()
	root := t.TempDir()
	legacyKeys = filepath.Join(root, "etc", "dkim")
	legacyTables = filepath.Join(root, "etc", "dkim-tables")
	destination = filepath.Join(root, "var", "keys")

	previousKeys, previousTables := legacyDKIMKeyDir, legacyDKIMTablesDir
	legacyDKIMKeyDir, legacyDKIMTablesDir = legacyKeys, legacyTables
	t.Cleanup(func() { legacyDKIMKeyDir, legacyDKIMTablesDir = previousKeys, previousTables })

	return legacyKeys, legacyTables, destination
}

func writeDKIMKeyFixture(t *testing.T, dir, domain, contents string) string {
	t.Helper()
	domainDir := filepath.Join(dir, domain)
	if err := os.MkdirAll(domainDir, 0o750); err != nil {
		t.Fatalf("stage %s: %v", domain, err)
	}
	key := filepath.Join(domainDir, signingSelector+".private")
	if err := os.WriteFile(key, []byte(contents), 0o600); err != nil {
		t.Fatalf("stage key for %s: %v", domain, err)
	}
	return key
}

// The upgrade a running server performs exactly once: keys move, and they move
// intact. A key that arrives truncated or empty is worse than one that did not
// move — the DNS record still advertises its public half, so every signature
// fails verification instead of simply being absent.
// Çalışan bir sunucunun tam bir kez yaptığı yükseltme: anahtarlar taşınır ve
// bozulmadan taşınır. Kırpılmış ya da boş gelen bir anahtar, hiç taşınmamış
// olandan kötüdür — DNS kaydı hâlâ genel yarısını duyurur, dolayısıyla her imza
// yok sayılmak yerine doğrulamada başarısız olur.
func TestDKIMMigrationCarriesEveryKeyIntact(t *testing.T) {
	legacyKeys, legacyTables, destination := stageLegacyDKIMStore(t)
	writeDKIMKeyFixture(t, legacyKeys, "example.com", "KEY-example")
	writeDKIMKeyFixture(t, legacyKeys, "second.example", "KEY-second")
	if err := os.MkdirAll(legacyTables, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyTables, "keytable"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyDKIMStore(destination); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	for domain, want := range map[string]string{
		"example.com":    "KEY-example",
		"second.example": "KEY-second",
	} {
		got, err := os.ReadFile(filepath.Join(destination, domain, signingSelector+".private"))
		if err != nil {
			t.Fatalf("key for %s did not arrive: %v", domain, err)
		}
		if string(got) != want {
			t.Fatalf("key for %s arrived as %q, want %q", domain, got, want)
		}
	}
	if _, err := os.Stat(legacyKeys); !os.IsNotExist(err) {
		t.Fatalf("the emptied legacy key directory must be removed, stat gave %v", err)
	}
	// Tables are derived from the keys on every configure, so the stale copy
	// under /etc must go — it is the only remaining reason for opendkim to want
	// read access there.
	// Tablolar her yapılandırmada anahtarlardan türetilir; bu yüzden /etc
	// altındaki eskimiş kopya gitmelidir — opendkim'in orada okuma izni istemesi
	// için kalan tek sebep odur.
	if _, err := os.Stat(legacyTables); !os.IsNotExist(err) {
		t.Fatalf("the legacy table directory must be removed, stat gave %v", err)
	}
}

// Running it twice is not a special case: the agent calls it on every DKIM
// request, and a restart mid-upgrade replays it.
// İki kez çalıştırmak özel bir durum değildir: agent bunu her DKIM isteğinde
// çağırır ve yükseltmenin ortasındaki bir yeniden başlatma onu tekrarlar.
func TestDKIMMigrationIsIdempotent(t *testing.T) {
	legacyKeys, _, destination := stageLegacyDKIMStore(t)
	writeDKIMKeyFixture(t, legacyKeys, "example.com", "KEY-example")

	for attempt := 1; attempt <= 3; attempt++ {
		if err := migrateLegacyDKIMStore(destination); err != nil {
			t.Fatalf("attempt %d failed: %v", attempt, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(destination, "example.com", signingSelector+".private"))
	if err != nil || string(got) != "KEY-example" {
		t.Fatalf("the key must survive repeated migration, got %q err=%v", got, err)
	}
}

// A server with no DKIM keys is the common case, and it must not be treated as
// a fault: every fresh install runs this on its first DKIM request.
// DKIM anahtarı olmayan bir sunucu yaygın durumdur ve arıza sayılmamalıdır: her
// yeni kurulum bunu ilk DKIM isteğinde çalıştırır.
func TestDKIMMigrationAcceptsAServerWithNothingToMove(t *testing.T) {
	_, _, destination := stageLegacyDKIMStore(t)
	if err := migrateLegacyDKIMStore(destination); err != nil {
		t.Fatalf("a server with no legacy store must migrate cleanly: %v", err)
	}
}

// When a key already exists at the destination, the legacy copy is left alone
// rather than overwriting it — and, because something was left behind, the
// legacy directory is left standing too. Deleting it would destroy the copy an
// operator needs in order to decide which key is the real one.
// Hedefte zaten bir anahtar varsa, eski kopya üzerine yazılmaz, kendi hâline
// bırakılır — ve geride bir şey kaldığı için eski dizin de ayakta bırakılır.
// Onu silmek, operatörün hangi anahtarın gerçek olduğuna karar vermek için
// ihtiyaç duyduğu kopyayı yok ederdi.
func TestDKIMMigrationNeverOverwritesAKeyAlreadyAtTheDestination(t *testing.T) {
	legacyKeys, _, destination := stageLegacyDKIMStore(t)
	writeDKIMKeyFixture(t, legacyKeys, "example.com", "OLD-KEY")
	writeDKIMKeyFixture(t, destination, "example.com", "CURRENT-KEY")

	if err := migrateLegacyDKIMStore(destination); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(destination, "example.com", signingSelector+".private"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CURRENT-KEY" {
		t.Fatalf("the destination key was replaced with %q", got)
	}
	if _, err := os.Stat(filepath.Join(legacyKeys, "example.com")); err != nil {
		t.Fatalf("the unmoved legacy copy must be preserved for inspection: %v", err)
	}
}

// The destination must never be the legacy directory itself. Silently accepting
// that would report a successful migration while the keys stayed exactly where
// the agent token is readable from.
// Hedef asla eski dizinin kendisi olamaz. Bunu sessizce kabul etmek, anahtarlar
// tam da agent token'ının okunabildiği yerde kalırken başarılı bir taşıma
// bildirirdi.
func TestDKIMMigrationRefusesTheLegacyDirectoryAsItsOwnDestination(t *testing.T) {
	legacyKeys, _, _ := stageLegacyDKIMStore(t)
	for _, destination := range []string{"", legacyKeys} {
		if err := migrateLegacyDKIMStore(destination); err == nil {
			t.Fatalf("destination %q must be refused", destination)
		}
	}
}

// The migration deletes directories under /etc, so anything that is not a real
// server must never reach it. This is the guard that makes running the test
// suite on a machine that also runs CelikPanel safe: the store path is not the
// production one, so nothing under /etc is read, moved or removed.
// Taşıma /etc altındaki dizinleri siler; bu yüzden gerçek bir sunucu olmayan
// hiçbir şey ona ulaşmamalıdır. Aynı zamanda CelikPanel çalıştıran bir makinede
// test paketini koşmayı güvenli kılan nöbet budur: depo yolu üretim yolu
// değildir, dolayısıyla /etc altında hiçbir şey okunmaz, taşınmaz, silinmez.
func TestDKIMMigrationStandsDownWhenTheStoreIsNotTheProductionOne(t *testing.T) {
	legacyKeys, legacyTables, destination := stageLegacyDKIMStore(t)
	writeDKIMKeyFixture(t, legacyKeys, "example.com", "KEY-example")
	if err := os.MkdirAll(legacyTables, 0o755); err != nil {
		t.Fatal(err)
	}

	previousBase, previousDone := dkimBaseDir, dkimMigrationDone
	dkimBaseDir, dkimMigrationDone = destination, false
	t.Cleanup(func() { dkimBaseDir, dkimMigrationDone = previousBase, previousDone })

	if err := ensureDKIMStorageMigrated(); err != nil {
		t.Fatalf("a redirected store must be left alone, not refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyKeys, "example.com")); err != nil {
		t.Fatalf("the legacy store must be untouched: %v", err)
	}
	if _, err := os.Stat(legacyTables); err != nil {
		t.Fatalf("the legacy table directory must be untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "example.com")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be moved into a non-production path, stat gave %v", err)
	}
}

// dkimBaseDir must actually BE the production path in a shipped binary, or the
// guard above would stand the migration down on every real server and no
// upgrade would ever happen.
// Sevk edilen bir ikilide dkimBaseDir gerçekten üretim yolu OLMALIDIR; aksi
// hâlde yukarıdaki nöbet her gerçek sunucuda taşımayı durdurur ve hiçbir
// yükseltme gerçekleşmezdi.
func TestShippedDKIMStoreIsTheProductionPath(t *testing.T) {
	if os.Getenv("CELIKPANEL_DKIM_DIR") != "" {
		t.Skip("the store is redirected in this environment")
	}
	if dkimBaseDir != productionDKIMKeyDir {
		t.Fatalf("dkimBaseDir = %q, want %q", dkimBaseDir, productionDKIMKeyDir)
	}
}
