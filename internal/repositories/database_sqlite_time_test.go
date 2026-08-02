package repositories

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestDatabaseRepositoriesScanSQLiteTextTimestamps(t *testing.T) {
	store, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	db := store.GetDB()
	ctx := context.Background()

	mustInsertID := func(query string, args ...any) int {
		t.Helper()
		result, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return int(id)
	}

	ownerID := mustInsertID(
		`INSERT INTO users (username, password_hash, email, role)
		 VALUES ('timestamp-owner', 'unused', 'timestamp@example.test', 'customer')`,
	)
	subscriptionID := mustInsertID(
		`INSERT INTO subscriptions (owner_id, name) VALUES (?, 'timestamp-subscription')`,
		ownerID,
	)
	var typeID int
	if err := db.QueryRowContext(ctx,
		`SELECT id FROM database_server_types WHERE name = 'mariadb'`,
	).Scan(&typeID); err != nil {
		t.Fatal(err)
	}
	serverID := mustInsertID(
		`INSERT INTO database_servers
		 (subscription_id, type_id, name, host, port)
		 VALUES (?, ?, 'timestamp-server', '127.0.0.1', 3306)`,
		subscriptionID, typeID,
	)
	databaseID := mustInsertID(
		`INSERT INTO databases_v2 (server_id, subscription_id, name)
		 VALUES (?, ?, 'timestamp_database')`,
		serverID, subscriptionID,
	)
	databaseUserID := mustInsertID(
		`INSERT INTO database_users
		 (server_id, subscription_id, username, password)
		 VALUES (?, ?, 'timestamp_user', 'sealed-test-value')`,
		serverID, subscriptionID,
	)
	grantID := mustInsertID(
		`INSERT INTO database_user_grants (database_id, user_id, privileges)
		 VALUES (?, ?, 'ALL')`,
		databaseID, databaseUserID,
	)

	assertTimestamp := func(label string, values ...time.Time) {
		t.Helper()
		for _, value := range values {
			if value.IsZero() {
				t.Fatalf("%s returned a zero timestamp", label)
			}
		}
	}

	databaseRepo := NewPostgresDatabaseV2Repository(db)
	database, err := databaseRepo.GetByID(ctx, databaseID)
	if err != nil {
		t.Fatal(err)
	}
	assertTimestamp("database GetByID", database.CreatedAt, database.UpdatedAt)
	databases, err := databaseRepo.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatal(err)
	}
	if len(databases) != 1 {
		t.Fatalf("database ListByServer count=%d, want 1", len(databases))
	}
	assertTimestamp("database ListByServer", databases[0].CreatedAt, databases[0].UpdatedAt)

	databaseUserRepo := NewPostgresDatabaseUserRepository(db)
	databaseUser, err := databaseUserRepo.GetByID(ctx, databaseUserID)
	if err != nil {
		t.Fatal(err)
	}
	assertTimestamp("database user GetByID", databaseUser.CreatedAt, databaseUser.UpdatedAt)
	byUsername, err := databaseUserRepo.GetByUsername(ctx, serverID, "timestamp_user")
	if err != nil {
		t.Fatal(err)
	}
	assertTimestamp("database user GetByUsername", byUsername.CreatedAt, byUsername.UpdatedAt)
	databaseUsers, err := databaseUserRepo.ListByServer(ctx, serverID)
	if err != nil {
		t.Fatal(err)
	}
	if len(databaseUsers) != 1 {
		t.Fatalf("database user ListByServer count=%d, want 1", len(databaseUsers))
	}
	assertTimestamp(
		"database user ListByServer",
		databaseUsers[0].CreatedAt,
		databaseUsers[0].UpdatedAt,
	)

	grantRepo := NewPostgresDatabaseGrantRepository(db)
	grant, err := grantRepo.GetByID(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	assertTimestamp("grant GetByID", grant.CreatedAt)
	grantsByDatabase, err := grantRepo.ListByDatabase(ctx, databaseID)
	if err != nil {
		t.Fatal(err)
	}
	grantsByUser, err := grantRepo.ListByUser(ctx, databaseUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grantsByDatabase) != 1 || len(grantsByUser) != 1 {
		t.Fatalf(
			"grant list counts by database/user=%d/%d, want 1/1",
			len(grantsByDatabase), len(grantsByUser),
		)
	}
	assertTimestamp("grant ListByDatabase", grantsByDatabase[0].CreatedAt)
	assertTimestamp("grant ListByUser", grantsByUser[0].CreatedAt)
}
