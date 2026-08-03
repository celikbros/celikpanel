package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/hostname"
)

type namespaceFixture struct {
	database      *paneldb.SQLiteDB
	subscription1 int
	subscription2 int
}

func newNamespaceFixture(t *testing.T) namespaceFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open namespace database: %v", err)
	}
	t.Cleanup(database.Close)
	raw := database.GetDB()
	raw.SetMaxOpenConns(8)

	createSubscription := func(username string) int {
		t.Helper()
		userResult, err := raw.Exec(`
			INSERT INTO users (username, password_hash, email, role)
			VALUES (?, 'hash', ?, 'customer')`,
			username, username+"@example.test")
		if err != nil {
			t.Fatalf("insert user %s: %v", username, err)
		}
		userID, err := userResult.LastInsertId()
		if err != nil {
			t.Fatalf("read user id: %v", err)
		}
		subscriptionResult, err := raw.Exec(`
			INSERT INTO subscriptions (owner_id, name)
			VALUES (?, ?)`, userID, username+" subscription")
		if err != nil {
			t.Fatalf("insert subscription %s: %v", username, err)
		}
		subscriptionID, err := subscriptionResult.LastInsertId()
		if err != nil {
			t.Fatalf("read subscription id: %v", err)
		}
		return int(subscriptionID)
	}

	return namespaceFixture{
		database:      database,
		subscription1: createSubscription("namespace-owner-1"),
		subscription2: createSubscription("namespace-owner-2"),
	}
}

func insertNamespaceDomain(
	db *sql.DB,
	subscriptionID int,
	name string,
	parentID *int,
) (int, error) {
	var parent any
	if parentID != nil {
		parent = *parentID
	}
	result, err := db.Exec(`
		INSERT INTO domains (subscription_id, name, parent_domain_id, status)
		VALUES (?, ?, ?, 'active')`,
		subscriptionID, name, parent)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return int(id), err
}

func insertNamespaceAlias(db *sql.DB, domainID int, alias string) error {
	_, err := db.Exec(`
		INSERT INTO domain_aliases (domain_id, alias)
		VALUES (?, ?)`, domainID, alias)
	return err
}

func requireNamespaceConflict(t *testing.T, err error) {
	t.Helper()
	if !hostname.IsNamespaceConflict(err) {
		t.Fatalf("error = %v, want hostname namespace conflict", err)
	}
}

func TestHostnameNamespacePrimaryAliasConflictsInBothOrdersAcrossTenants(t *testing.T) {
	t.Run("primary then alias", func(t *testing.T) {
		fixture := newNamespaceFixture(t)
		raw := fixture.database.GetDB()
		ownerID, err := insertNamespaceDomain(raw, fixture.subscription1, "owner-one.example", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := insertNamespaceDomain(raw, fixture.subscription2, "claimed.example", nil); err != nil {
			t.Fatal(err)
		}
		requireNamespaceConflict(t, insertNamespaceAlias(raw, ownerID, "claimed.example"))
	})

	t.Run("alias then primary", func(t *testing.T) {
		fixture := newNamespaceFixture(t)
		raw := fixture.database.GetDB()
		ownerID, err := insertNamespaceDomain(raw, fixture.subscription1, "owner-two.example", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := insertNamespaceAlias(raw, ownerID, "reverse-claimed.example"); err != nil {
			t.Fatal(err)
		}
		_, err = insertNamespaceDomain(raw, fixture.subscription2, "reverse-claimed.example", nil)
		requireNamespaceConflict(t, err)
	})
}

func TestHostnameNamespaceConcurrentPrimaryAndAliasHaveOneWinner(t *testing.T) {
	fixture := newNamespaceFixture(t)
	raw := fixture.database.GetDB()
	ownerID, err := insertNamespaceDomain(raw, fixture.subscription1, "race-owner.example", nil)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		_, err := insertNamespaceDomain(raw, fixture.subscription2, "race.example", nil)
		results <- err
	}()
	go func() {
		defer workers.Done()
		<-start
		results <- insertNamespaceAlias(raw, ownerID, "race.example")
	}()
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case hostname.IsNamespaceConflict(result):
			conflicts++
		default:
			t.Fatalf("concurrent mutation returned unexpected error: %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent results: successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}

	var reservations int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE hostname = 'race.example'`).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if reservations != 1 {
		t.Fatalf("race.example reservations = %d, want 1", reservations)
	}
}

func TestHostnameNamespaceImplicitWWWAndSubdomainRules(t *testing.T) {
	fixture := newNamespaceFixture(t)
	raw := fixture.database.GetDB()
	rootID, err := insertNamespaceDomain(raw, fixture.subscription1, "parent.example", nil)
	if err != nil {
		t.Fatal(err)
	}

	requireNamespaceConflict(t, insertNamespaceAlias(raw, rootID, "www.parent.example"))
	requireNamespaceConflict(t, insertNamespaceAlias(raw, rootID, "mail.parent.example"))

	if _, err := insertNamespaceDomain(raw, fixture.subscription2, "www.reverse.example", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := insertNamespaceDomain(raw, fixture.subscription1, "reverse.example", nil); err == nil {
		t.Fatal("top-level domain unexpectedly stole an existing primary as its implicit www name")
	} else {
		requireNamespaceConflict(t, err)
	}

	if _, err := insertNamespaceDomain(raw, fixture.subscription2, "mail.reverse-mail.example", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := insertNamespaceDomain(raw, fixture.subscription1, "reverse-mail.example", nil); err == nil {
		t.Fatal("domain unexpectedly stole an existing primary as its implicit mail name")
	} else {
		requireNamespaceConflict(t, err)
	}

	childID, err := insertNamespaceDomain(raw, fixture.subscription1, "blog.parent.example", &rootID)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNamespaceAlias(raw, childID, "www.blog.parent.example"); err != nil {
		t.Fatalf("hosted subdomain incorrectly reserved an implicit www name: %v", err)
	}
	var implicit int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE source_kind = 'implicit_www' AND source_id = ?`, childID).Scan(&implicit); err != nil {
		t.Fatal(err)
	}
	if implicit != 0 {
		t.Fatalf("subdomain implicit reservations = %d, want 0", implicit)
	}
	var implicitMail int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE source_kind = 'implicit_mail' AND source_id = ?`, childID).Scan(&implicitMail); err != nil {
		t.Fatal(err)
	}
	if implicitMail != 1 {
		t.Fatalf("subdomain implicit mail reservations = %d, want 1", implicitMail)
	}
	requireNamespaceConflict(t, insertNamespaceAlias(raw, childID, "mail.blog.parent.example"))
}

func TestHostnameNamespaceRejectsNonCanonicalDirectWrites(t *testing.T) {
	fixture := newNamespaceFixture(t)
	_, err := insertNamespaceDomain(
		fixture.database.GetDB(),
		fixture.subscription1,
		"Mixed.Example",
		nil,
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "canonical lowercase") {
		t.Fatalf("mixed-case direct insert error = %v", err)
	}
}

func TestHostnameNamespaceReservationsFollowDeletes(t *testing.T) {
	fixture := newNamespaceFixture(t)
	raw := fixture.database.GetDB()
	domainID, err := insertNamespaceDomain(raw, fixture.subscription1, "delete.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNamespaceAlias(raw, domainID, "alias-delete.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		DELETE FROM domain_aliases
		WHERE domain_id = ? AND alias = 'alias-delete.example'`, domainID); err != nil {
		t.Fatal(err)
	}
	var aliasReservations int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE hostname = 'alias-delete.example'`).Scan(&aliasReservations); err != nil {
		t.Fatal(err)
	}
	if aliasReservations != 0 {
		t.Fatalf("deleted alias left %d reservations", aliasReservations)
	}

	if _, err := raw.Exec(`DELETE FROM domains WHERE id = ?`, domainID); err != nil {
		t.Fatal(err)
	}
	var domainReservations int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE domain_id = ?`, domainID).Scan(&domainReservations); err != nil {
		t.Fatal(err)
	}
	if domainReservations != 0 {
		t.Fatalf("deleted domain left %d reservations", domainReservations)
	}
}

func downgradeHostnameNamespaceMigration(t *testing.T, database *paneldb.SQLiteDB) {
	t.Helper()
	raw := database.GetDB()
	for _, trigger := range []string{
		"trg_domains_hostname_canonical_insert",
		"trg_domains_hostname_canonical_update",
		"trg_domain_aliases_hostname_canonical_insert",
		"trg_domain_aliases_hostname_canonical_update",
		"trg_domains_hostname_reserve_insert",
		"trg_domains_hostname_reserve_update",
		"trg_domain_aliases_hostname_reserve_insert",
		"trg_domain_aliases_hostname_reserve_update",
		"trg_domain_aliases_hostname_reserve_delete",
		"trg_hostname_reservations_reject_invalid",
		"trg_hostname_reservations_reject_conflict",
	} {
		if _, err := raw.Exec(`DROP TRIGGER IF EXISTS ` + trigger); err != nil {
			t.Fatalf("drop trigger %s: %v", trigger, err)
		}
	}
	for _, index := range []string{
		"idx_domains_name_canonical",
		"idx_domain_aliases_alias_canonical",
		"idx_hostname_reservations_source",
		"idx_hostname_reservations_domain",
	} {
		if _, err := raw.Exec(`DROP INDEX IF EXISTS ` + index); err != nil {
			t.Fatalf("drop index %s: %v", index, err)
		}
	}
	if _, err := raw.Exec(`DROP TABLE hostname_reservations`); err != nil {
		t.Fatalf("drop hostname reservations: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM schema_migrations WHERE version = 18`); err != nil {
		t.Fatalf("rewind hostname migration: %v", err)
	}
}

func TestHostnameNamespaceMigrationRejectsLegacyDuplicateWithoutDataLoss(t *testing.T) {
	fixture := newNamespaceFixture(t)
	raw := fixture.database.GetDB()
	downgradeHostnameNamespaceMigration(t, fixture.database)

	ownerID, err := insertNamespaceDomain(raw, fixture.subscription1, "legacy-owner.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insertNamespaceDomain(raw, fixture.subscription2, "legacy.example", nil); err != nil {
		t.Fatal(err)
	}
	if err := insertNamespaceAlias(raw, ownerID, "LEGACY.EXAMPLE"); err != nil {
		t.Fatalf("insert legacy cross-table duplicate: %v", err)
	}

	err = fixture.database.RunMigrations()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "hostname namespace conflict") {
		t.Fatalf("legacy duplicate migration error = %v", err)
	}

	var alias string
	if err := raw.QueryRow(`
		SELECT alias FROM domain_aliases
		WHERE domain_id = ?`, ownerID).Scan(&alias); err != nil {
		t.Fatal(err)
	}
	if alias != "LEGACY.EXAMPLE" {
		t.Fatalf("failed migration changed legacy alias to %q", alias)
	}
	var version18, reservationTable int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM schema_migrations WHERE version = 18`).Scan(&version18); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'hostname_reservations'`).Scan(&reservationTable); err != nil {
		t.Fatal(err)
	}
	if version18 != 0 || reservationTable != 0 {
		t.Fatalf("failed migration left version=%d table=%d", version18, reservationTable)
	}
}

func TestHostnameNamespaceMigrationRejectsLegacyImplicitMailCollisionWithoutDataLoss(t *testing.T) {
	fixture := newNamespaceFixture(t)
	raw := fixture.database.GetDB()
	downgradeHostnameNamespaceMigration(t, fixture.database)

	ownerID, err := insertNamespaceDomain(raw, fixture.subscription1, "legacy-mail-owner.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	collisionID, err := insertNamespaceDomain(
		raw,
		fixture.subscription2,
		"mail.legacy-mail-owner.example",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = fixture.database.RunMigrations()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "hostname namespace conflict") {
		t.Fatalf("legacy implicit-mail migration error = %v", err)
	}

	for id, want := range map[int]string{
		ownerID:     "legacy-mail-owner.example",
		collisionID: "mail.legacy-mail-owner.example",
	} {
		var got string
		if err := raw.QueryRow(`SELECT name FROM domains WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("failed migration changed legacy domain %d to %q, want %q", id, got, want)
		}
	}
	var version18, reservationTable int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM schema_migrations WHERE version = 18`).Scan(&version18); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'hostname_reservations'`).Scan(&reservationTable); err != nil {
		t.Fatal(err)
	}
	if version18 != 0 || reservationTable != 0 {
		t.Fatalf("failed migration left version=%d table=%d", version18, reservationTable)
	}
}

func TestHostnameNamespaceMigrationRejectsUnsafeLegacyNamesWithoutDataLoss(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		hostname string
	}{
		{name: "label longer than 63 octets", hostname: strings.Repeat("a", 64) + ".example"},
		{name: "raw IPv4 address", hostname: "192.0.2.1"},
		{name: "single-label name", hostname: "localhost"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newNamespaceFixture(t)
			raw := fixture.database.GetDB()
			downgradeHostnameNamespaceMigration(t, fixture.database)

			domainID, err := insertNamespaceDomain(
				raw,
				fixture.subscription1,
				testCase.hostname,
				nil,
			)
			if err != nil {
				t.Fatalf("insert unsafe legacy hostname: %v", err)
			}

			err = fixture.database.RunMigrations()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "hostname namespace invalid") {
				t.Fatalf("unsafe legacy migration error = %v", err)
			}

			var stored string
			if err := raw.QueryRow(
				`SELECT name FROM domains WHERE id = ?`,
				domainID,
			).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if stored != testCase.hostname {
				t.Fatalf("failed migration changed legacy hostname to %q", stored)
			}

			var version18, reservationTable int
			if err := raw.QueryRow(
				`SELECT COUNT(*) FROM schema_migrations WHERE version = 18`,
			).Scan(&version18); err != nil {
				t.Fatal(err)
			}
			if err := raw.QueryRow(`
				SELECT COUNT(*) FROM sqlite_master
				WHERE type = 'table' AND name = 'hostname_reservations'
			`).Scan(&reservationTable); err != nil {
				t.Fatal(err)
			}
			if version18 != 0 || reservationTable != 0 {
				t.Fatalf(
					"failed migration left version=%d table=%d",
					version18,
					reservationTable,
				)
			}
		})
	}
}

func TestHostnameNamespaceMigrationNormalizesSafeLegacyNames(t *testing.T) {
	fixture := newNamespaceFixture(t)
	raw := fixture.database.GetDB()
	downgradeHostnameNamespaceMigration(t, fixture.database)

	result, err := raw.Exec(`
		INSERT INTO domains (subscription_id, name, status)
		VALUES (?, '  Mixed.Legacy.Example.  ', 'active')`, fixture.subscription1)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.RunMigrations(); err != nil {
		t.Fatalf("migrate safe legacy hostname: %v", err)
	}

	var name string
	if err := raw.QueryRow(`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "mixed.legacy.example" {
		t.Fatalf("normalized legacy domain = %q", name)
	}
	var reservations int
	if err := raw.QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE domain_id = ?`, domainID).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if reservations != 3 {
		t.Fatalf(
			"normalized top-level reservations = %d, want primary + implicit www + implicit mail",
			reservations,
		)
	}
}

func TestHostnameNamespaceMigrationContextRemainsUsable(t *testing.T) {
	// A small smoke check that the migrated database accepts context-aware
	// writes; production repositories use ExecContext for every mutation.
	fixture := newNamespaceFixture(t)
	_, err := fixture.database.GetDB().ExecContext(
		context.Background(),
		`INSERT INTO domains (subscription_id, name) VALUES (?, 'context.example')`,
		fixture.subscription1,
	)
	if err != nil {
		t.Fatal(err)
	}
}
