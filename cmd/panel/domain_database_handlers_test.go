package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestDomainDatabaseAvailableTypesAreTenantScopedActiveAndStable(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open panel database: %v", err)
	}
	t.Cleanup(database.Close)
	pool := database.GetDB()

	mustInsertID := func(query string, args ...any) int {
		t.Helper()
		result, err := pool.Exec(query, args...)
		if err != nil {
			t.Fatalf("seed database: %v", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("read seeded ID: %v", err)
		}
		return int(id)
	}

	ownerID := mustInsertID(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('database-owner', 'x', 'database-owner@example.test', 'customer')
	`)
	foreignOwnerID := mustInsertID(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('foreign-database-owner', 'x', 'foreign-database-owner@example.test', 'customer')
	`)
	subscriptionID := mustInsertID(
		`INSERT INTO subscriptions (owner_id, name) VALUES (?, 'database-subscription')`,
		ownerID,
	)
	foreignSubscriptionID := mustInsertID(
		`INSERT INTO subscriptions (owner_id, name) VALUES (?, 'foreign-database-subscription')`,
		foreignOwnerID,
	)
	domainID := mustInsertID(
		`INSERT INTO domains (subscription_id, name) VALUES (?, 'databases.example.test')`,
		subscriptionID,
	)

	var mariaTypeID, postgresTypeID int
	if err := pool.QueryRow(`SELECT id FROM database_server_types WHERE name = 'mariadb'`).Scan(&mariaTypeID); err != nil {
		t.Fatalf("load MariaDB type: %v", err)
	}
	if err := pool.QueryRow(`SELECT id FROM database_server_types WHERE name = 'postgresql'`).Scan(&postgresTypeID); err != nil {
		t.Fatalf("load PostgreSQL type: %v", err)
	}

	mustInsertID(`
		INSERT INTO database_servers (subscription_id, type_id, name, host, port, status)
		VALUES (?, ?, 'tenant-mariadb-primary', '127.0.0.11', 3306, 'active')
	`, subscriptionID, mariaTypeID)
	mustInsertID(`
		INSERT INTO database_servers (subscription_id, type_id, name, host, port, status)
		VALUES (?, ?, 'tenant-mariadb-replica', '127.0.0.12', 3306, 'active')
	`, subscriptionID, mariaTypeID)
	tenantPostgresID := mustInsertID(`
		INSERT INTO database_servers (subscription_id, type_id, name, host, port, status)
		VALUES (?, ?, 'tenant-postgresql-inactive', '127.0.0.13', 5432, 'inactive')
	`, subscriptionID, postgresTypeID)
	mustInsertID(`
		INSERT INTO database_servers (subscription_id, type_id, name, host, port, status)
		VALUES (?, ?, 'foreign-postgresql', '127.0.0.14', 5432, 'active')
	`, foreignSubscriptionID, postgresTypeID)

	panel := &Panel{db: database}
	type databaseResponse struct {
		AvailableTypes []string       `json:"available_types"`
		Databases      []DatabaseInfo `json:"databases"`
	}
	getDatabases := func() databaseResponse {
		t.Helper()
		request := httptest.NewRequest(
			http.MethodGet,
			fmt.Sprintf("/api/v1/domains/%d/databases", domainID),
			nil,
		)
		request = request.WithContext(context.WithValue(
			request.Context(),
			callerKey,
			&Caller{ID: ownerID, Role: roleCustomer},
		))
		recorder := httptest.NewRecorder()
		panel.handleDomainSubroute(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
		}
		var response databaseResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		return response
	}

	response := getDatabases()
	if want := []string{"mysql"}; !reflect.DeepEqual(response.AvailableTypes, want) {
		t.Fatalf("available_types = %#v, want %#v", response.AvailableTypes, want)
	}

	if _, err := pool.Exec(`UPDATE database_servers SET status = 'active' WHERE id = ?`, tenantPostgresID); err != nil {
		t.Fatalf("activate tenant PostgreSQL: %v", err)
	}
	response = getDatabases()
	if want := []string{"mysql", "postgresql"}; !reflect.DeepEqual(response.AvailableTypes, want) {
		t.Fatalf("ordered available_types = %#v, want %#v", response.AvailableTypes, want)
	}

	if _, err := pool.Exec(`UPDATE database_servers SET status = 'inactive' WHERE subscription_id = ?`, subscriptionID); err != nil {
		t.Fatalf("deactivate tenant engines: %v", err)
	}
	response = getDatabases()
	if response.AvailableTypes == nil {
		t.Fatal("available_types encoded as null or omitted, want stable []")
	}
	if len(response.AvailableTypes) != 0 {
		t.Fatalf("empty tenant available_types = %#v; foreign engine leaked", response.AvailableTypes)
	}
}
