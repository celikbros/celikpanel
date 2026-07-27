package repositories_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/repositories"
)

func newDomainRepositoryFixture(
	t *testing.T,
) (*repositories.PostgresDomainRepository, *paneldb.SQLiteDB, int) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open domain repository database: %v", err)
	}
	t.Cleanup(database.Close)
	raw := database.GetDB()
	userResult, err := raw.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('repository-owner', 'hash', 'repository-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	subscriptionResult, err := raw.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Repository test')`, userID)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return repositories.NewPostgresDomainRepository(raw), database, int(subscriptionID)
}

func TestDomainRepositoryCanonicalizesBeforeAtomicReservation(t *testing.T) {
	repository, database, subscriptionID := newDomainRepositoryFixture(t)
	domain := &core.Domain{
		SubscriptionID: subscriptionID,
		Name:           "  Mixed.Repository.Example. ",
		Status:         "active",
	}
	if err := repository.Create(context.Background(), domain); err != nil {
		t.Fatalf("create canonical domain: %v", err)
	}
	if domain.Name != "mixed.repository.example" {
		t.Fatalf("stored domain name = %q", domain.Name)
	}
	var stored string
	if err := database.GetDB().QueryRow(
		`SELECT name FROM domains WHERE id = ?`, domain.ID,
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != domain.Name {
		t.Fatalf("database name = %q, want %q", stored, domain.Name)
	}

	duplicate := &core.Domain{
		SubscriptionID: subscriptionID,
		Name:           "MIXED.REPOSITORY.EXAMPLE",
		Status:         "active",
	}
	if err := repository.Create(context.Background(), duplicate); !hostname.IsNamespaceConflict(err) {
		t.Fatalf("case-variant duplicate error = %v", err)
	}
}

func TestDomainRepositoryInsertsParentBeforeNamespaceTriggers(t *testing.T) {
	repository, database, subscriptionID := newDomainRepositoryFixture(t)
	parent := &core.Domain{
		SubscriptionID: subscriptionID,
		Name:           "parent-repository.example",
		Status:         "active",
	}
	if err := repository.Create(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	child := &core.Domain{
		SubscriptionID: subscriptionID,
		Name:           "blog.parent-repository.example",
		ParentDomainID: &parent.ID,
		Status:         "active",
	}
	if err := repository.Create(context.Background(), child); err != nil {
		t.Fatal(err)
	}

	loaded, err := repository.GetByID(context.Background(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ParentDomainID == nil || *loaded.ParentDomainID != parent.ID {
		t.Fatalf("loaded parent = %v, want %d", loaded.ParentDomainID, parent.ID)
	}
	var implicit int
	if err := database.GetDB().QueryRow(`
		SELECT COUNT(*) FROM hostname_reservations
		WHERE source_kind = 'implicit_www' AND source_id = ?`, child.ID,
	).Scan(&implicit); err != nil {
		t.Fatal(err)
	}
	if implicit != 0 {
		t.Fatalf("child implicit-www reservations = %d, want 0", implicit)
	}
}
