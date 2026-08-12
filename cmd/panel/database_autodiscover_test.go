package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	autodiscoverUserID         = 9811
	autodiscoverSubscriptionID = 9812
)

type databaseAutodiscoverAgent struct {
	services []core.Service
	err      error
}

func (a *databaseAutodiscoverAgent) GetServices(
	_ *transport.Empty,
	reply *[]core.Service,
) error {
	*reply = append([]core.Service(nil), a.services...)
	return a.err
}

func newDatabaseAutodiscoverFixture(t *testing.T) (*Panel, *sql.DB) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role, status)
		VALUES (?, 'autodiscover-owner', 'x', 'autodiscover@example.test', 'admin', 'active')
	`, autodiscoverUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`
		INSERT INTO subscriptions (id, owner_id, name, status)
		VALUES (?, ?, 'Autodiscover subscription', 'active')
	`, autodiscoverSubscriptionID, autodiscoverUserID); err != nil {
		t.Fatal(err)
	}
	return &Panel{db: database}, database.GetDB()
}

func attachDatabaseAutodiscoverAgent(
	t *testing.T,
	panel *Panel,
	agent *databaseAutodiscoverAgent,
) {
	t.Helper()
	panel.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func autodiscoverServices() []core.Service {
	return []core.Service{
		{Name: "mariadb.service", Version: "11.4", Status: "running"},
		{Name: "postgresql@17-main.service", Version: "17.2", Status: "running"},
		{Name: "mariadb-helper.service", Version: "ignored-duplicate", Status: "running"},
	}
}

func TestEnsureInstalledDBServersPropagatesRPCFailure(t *testing.T) {
	panel, database := newDatabaseAutodiscoverFixture(t)
	attachDatabaseAutodiscoverAgent(t, panel, &databaseAutodiscoverAgent{
		err: errors.New("service inventory unavailable"),
	})

	err := panel.ensureInstalledDBServers(context.Background(), autodiscoverSubscriptionID)
	if err == nil || !strings.Contains(err.Error(), "service inventory unavailable") {
		t.Fatalf("expected actionable RPC failure, got %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM database_servers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("RPC failure created %d database server rows", count)
	}
}

func TestListDatabaseServersDoesNotHideAutodiscoveryFailure(t *testing.T) {
	panel, database := newDatabaseAutodiscoverFixture(t)
	attachDatabaseAutodiscoverAgent(t, panel, &databaseAutodiscoverAgent{
		err: errors.New("service inventory unavailable"),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v2/database-servers", nil)
	request = request.WithContext(context.WithValue(
		request.Context(),
		callerKey,
		&Caller{ID: autodiscoverUserID, Role: roleAdmin},
	))
	response := httptest.NewRecorder()

	panel.handleListDatabaseServers(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%q, want 500", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "internal server error") {
		t.Fatalf("body=%q, want a server error response", response.Body.String())
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM database_servers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed handler request created %d database server rows", count)
	}
}

func TestReconcileInstalledDBServersIsIdempotentAndSelectsOneDefault(t *testing.T) {
	_, database := newDatabaseAutodiscoverFixture(t)

	for attempt := 0; attempt < 2; attempt++ {
		if err := reconcileInstalledDBServers(
			context.Background(), database, autodiscoverSubscriptionID, autodiscoverServices(),
		); err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
	}

	rows, err := database.Query(`
		SELECT dst.name, ds.version, ds.is_default
		FROM database_servers ds
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE ds.subscription_id = ?
		ORDER BY dst.name`, autodiscoverSubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type engine struct {
		name      string
		version   string
		isDefault bool
	}
	var engines []engine
	for rows.Next() {
		var current engine
		if err := rows.Scan(&current.name, &current.version, &current.isDefault); err != nil {
			t.Fatal(err)
		}
		engines = append(engines, current)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(engines) != 2 {
		t.Fatalf("engines=%+v, want exactly two idempotent rows", engines)
	}
	if engines[0].name != "mariadb" || engines[0].version != "11.4" || !engines[0].isDefault {
		t.Fatalf("MariaDB row=%+v", engines[0])
	}
	if engines[1].name != "postgresql" || engines[1].version != "17.2" || engines[1].isDefault {
		t.Fatalf("PostgreSQL row=%+v", engines[1])
	}
}

func TestReconcileInstalledDBServersRollsBackOnExistingQueryFailure(t *testing.T) {
	_, database := newDatabaseAutodiscoverFixture(t)
	if _, err := database.Exec(`ALTER TABLE database_servers RENAME TO database_servers_unavailable`); err != nil {
		t.Fatal(err)
	}

	err := reconcileInstalledDBServers(
		context.Background(), database, autodiscoverSubscriptionID, autodiscoverServices(),
	)
	if err == nil || !strings.Contains(err.Error(), "list registered engines") {
		t.Fatalf("expected registered-engine query failure, got %v", err)
	}
}

func TestReconcileInstalledDBServersRollsBackOnScanFailure(t *testing.T) {
	_, database := newDatabaseAutodiscoverFixture(t)
	if _, err := database.Exec(`
		INSERT INTO database_servers
			(subscription_id, type_id, name, version, host, port, is_default, status)
		VALUES (?, 2, 'existing MariaDB', '11.4', 'localhost', 3306, 1, 'active');
		ALTER TABLE database_server_types RENAME TO database_server_types_real;
		CREATE VIEW database_server_types AS
			SELECT id, NULL AS name, display_name, default_port, icon,
			       supports_users, supports_databases, created_at
			FROM database_server_types_real;
	`, autodiscoverSubscriptionID); err != nil {
		t.Fatal(err)
	}

	err := reconcileInstalledDBServers(
		context.Background(), database, autodiscoverSubscriptionID, autodiscoverServices(),
	)
	if err == nil || !strings.Contains(err.Error(), "read registered engine") {
		t.Fatalf("expected scan failure, got %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM database_servers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("scan failure changed server rows: %d", count)
	}
}

func TestReconcileInstalledDBServersRollsBackOnInsertFailure(t *testing.T) {
	_, database := newDatabaseAutodiscoverFixture(t)
	if _, err := database.Exec(`
		CREATE TRIGGER reject_database_server_insert
		BEFORE INSERT ON database_servers
		BEGIN
			SELECT RAISE(FAIL, 'database server insert rejected');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	err := reconcileInstalledDBServers(
		context.Background(), database, autodiscoverSubscriptionID,
		[]core.Service{{Name: "mariadb.service", Version: "11.4"}},
	)
	if err == nil || !strings.Contains(err.Error(), "register mariadb engine") {
		t.Fatalf("expected insert failure, got %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM database_servers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed transaction left %d server rows", count)
	}
}

func TestReconcileInstalledDBServersRejectsIgnoredInsertConflict(t *testing.T) {
	_, database := newDatabaseAutodiscoverFixture(t)
	if _, err := database.Exec(`
		INSERT INTO database_servers
			(subscription_id, type_id, name, version, host, port, is_default, status)
		VALUES (?, 1, 'conflicting PostgreSQL', '17', 'localhost', 3306, 1, 'active')
	`, autodiscoverSubscriptionID); err != nil {
		t.Fatal(err)
	}

	err := reconcileInstalledDBServers(
		context.Background(), database, autodiscoverSubscriptionID,
		[]core.Service{{Name: "mariadb.service", Version: "11.4"}},
	)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing localhost:3306 metadata") {
		t.Fatalf("expected ignored-insert conflict, got %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM database_servers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("conflict changed server rows: %d", count)
	}
}

func TestReconcileInstalledDBServersRollsBackOnDefaultUpdateFailure(t *testing.T) {
	_, database := newDatabaseAutodiscoverFixture(t)
	if _, err := database.Exec(`
		INSERT INTO database_servers
			(subscription_id, type_id, name, version, host, port, is_default, status)
		VALUES (?, 2, 'existing MariaDB', '11.4', 'localhost', 3306, 0, 'active');
		CREATE TRIGGER reject_database_server_default
		BEFORE UPDATE OF is_default ON database_servers
		BEGIN
			SELECT RAISE(FAIL, 'default update rejected');
		END;
	`, autodiscoverSubscriptionID); err != nil {
		t.Fatal(err)
	}

	err := reconcileInstalledDBServers(
		context.Background(), database, autodiscoverSubscriptionID,
		[]core.Service{{Name: "mariadb.service", Version: "11.4"}},
	)
	if err == nil || !strings.Contains(err.Error(), "select default engine") {
		t.Fatalf("expected update failure, got %v", err)
	}
	var isDefault bool
	if err := database.QueryRow(`SELECT is_default FROM database_servers`).Scan(&isDefault); err != nil {
		t.Fatal(err)
	}
	if isDefault {
		t.Fatal("failed transaction changed the default flag")
	}
}
