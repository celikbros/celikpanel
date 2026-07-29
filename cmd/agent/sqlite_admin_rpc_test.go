package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/systemsqlite"
)

func createAgentSQLiteTestDatabase(t *testing.T, path string) {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=rwc", filepath.ToSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		PRAGMA user_version=9;
		CREATE TABLE settings (id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO settings(value) VALUES ('ready');
	`); err != nil {
		t.Fatal(err)
	}
}

func newAgentSQLiteTestManager(
	t *testing.T,
	definitions []systemsqlite.Definition,
) *systemsqlite.Manager {
	t.Helper()
	manager, err := systemsqlite.NewManager(definitions, systemsqlite.Options{
		SnapshotRoot: filepath.Join(t.TempDir(), "snapshots"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return manager
}

func agentSQLiteTestDefinition(id, path string, mutable bool) systemsqlite.Definition {
	return systemsqlite.Definition{
		ID: id, Name: "Test database", Purpose: "RPC test database.", Kind: "test",
		Path: path, PathHint: "managed-data / test.sqlite3",
		Mutable: mutable, Optimizable: mutable, SnapshotAllowed: true,
	}
}

func TestSystemSQLiteSnapshotDirectoryUsesPrivateAgentState(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "agent-private")
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	t.Setenv("CELIKPANEL_SYSTEM_SQLITE_SNAPSHOT_DIR", filepath.Join(t.TempDir(), "untrusted"))
	if got, want := systemSQLiteSnapshotDirectory(), filepath.Join(stateDir, "system-sqlite-snapshots"); got != want {
		t.Fatalf("systemSQLiteSnapshotDirectory() = %q, want %q", got, want)
	}
	if got := systemSQLiteOwnerWorkspaceRoot(); got != "/tmp" {
		t.Fatalf("systemSQLiteOwnerWorkspaceRoot() = %q, want /tmp", got)
	}
}

func TestSystemSQLiteDefinitionsHonorPanelDataDirectoryAndOverrides(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "panel-data")
	panelOverride := filepath.Join(t.TempDir(), "panel-override.sqlite3")
	pdnsPath := filepath.Join(t.TempDir(), "pdns.sqlite3")
	catalogPath := filepath.Join(t.TempDir(), "components-v2.db")
	webmailDir := filepath.Join(t.TempDir(), "webmail")

	t.Setenv("CELIKPANEL_PANEL_DB", "")
	t.Setenv("CELIKPANEL_DATA_DIR", dataDir)
	t.Setenv("CELIKPANEL_PDNS_DB", pdnsPath)
	t.Setenv("CELIKPANEL_COMPONENT_CATALOG", catalogPath)
	previousWebmailBaseDir := webmailBaseDir
	webmailBaseDir = webmailDir
	t.Cleanup(func() { webmailBaseDir = previousWebmailBaseDir })

	definitions := systemSQLiteDefinitions()
	if len(definitions) != 4 {
		t.Fatalf("len(definitions) = %d", len(definitions))
	}
	byID := make(map[string]systemsqlite.Definition, len(definitions))
	for _, definition := range definitions {
		byID[definition.ID] = definition
		if filepath.IsAbs(definition.PathHint) || strings.Contains(definition.PathHint, filepath.Dir(definition.Path)) {
			t.Fatalf("unsafe path hint for %s: %q", definition.ID, definition.PathHint)
		}
	}
	if got := byID[systemsqlite.DatabasePanel].Path; got != filepath.Join(dataDir, "celikpanel.db") {
		t.Fatalf("panel path = %q", got)
	}
	panel := byID[systemsqlite.DatabasePanel]
	if !panel.Mutable || panel.Optimizable || panel.SnapshotAllowed {
		t.Fatalf("panel maintenance policy = %+v", panel)
	}
	if got := byID[systemsqlite.DatabasePowerDNS].Path; got != pdnsPath {
		t.Fatalf("PowerDNS path = %q", got)
	}
	roundcube := byID[systemsqlite.DatabaseRoundcube]
	if got := roundcube.Path; got != filepath.Join(webmailDir, "db", "roundcube.sqlite3") {
		t.Fatalf("Roundcube path = %q", got)
	}
	if roundcube.WriterIdentitySet != (roundcube.WriterUID != 0 && roundcube.WriterGID != 0) {
		t.Fatalf("Roundcube writer identity is incomplete: %+v", roundcube)
	}
	catalog := byID[systemsqlite.DatabaseComponentCatalog]
	if catalog.Path != catalogPath || catalog.Mutable || catalog.Optimizable {
		t.Fatalf("component catalog definition = %+v", catalog)
	}

	t.Setenv("CELIKPANEL_PANEL_DB", panelOverride)
	definitions = systemSQLiteDefinitions()
	if definitions[0].ID != systemsqlite.DatabasePanel || definitions[0].Path != panelOverride {
		t.Fatalf("explicit panel override was not preferred: %+v", definitions[0])
	}
}

func TestSystemSQLiteRPCValidatesEveryRequestAndProtocol(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "panel.sqlite3")
	createAgentSQLiteTestDatabase(t, databasePath)
	agent := &Agent{sqliteAdmin: newAgentSQLiteTestManager(t, []systemsqlite.Definition{
		agentSQLiteTestDefinition(systemsqlite.DatabasePanel, databasePath, true),
	})}

	var list systemsqlite.ListResponse
	if err := agent.ListSystemSQLiteDatabases(nil, &list); err != nil || list.Error == "" {
		t.Fatalf("nil list request = %+v, %v", list, err)
	}
	list = systemsqlite.ListResponse{}
	if err := agent.ListSystemSQLiteDatabases(&systemsqlite.ListRequest{ProtocolVersion: 999}, &list); err != nil || list.Error == "" {
		t.Fatalf("list protocol response = %+v, %v", list, err)
	}

	var check systemsqlite.CheckResponse
	if err := agent.CheckSystemSQLiteDatabase(nil, &check); err != nil || check.Error == "" {
		t.Fatalf("nil check request = %+v, %v", check, err)
	}
	check = systemsqlite.CheckResponse{}
	if err := agent.CheckSystemSQLiteDatabase(&systemsqlite.DatabaseRequest{ProtocolVersion: 999}, &check); err != nil || check.Error == "" {
		t.Fatalf("check protocol response = %+v, %v", check, err)
	}

	var snapshot systemsqlite.SnapshotResponse
	if err := agent.CreateSystemSQLiteSnapshot(nil, &snapshot); err != nil || snapshot.Error == "" {
		t.Fatalf("nil snapshot request = %+v, %v", snapshot, err)
	}
	var chunk systemsqlite.ReadSnapshotChunkResponse
	if err := agent.ReadSystemSQLiteSnapshotChunk(nil, &chunk); err != nil || chunk.Error == "" {
		t.Fatalf("nil read request = %+v, %v", chunk, err)
	}
	chunk = systemsqlite.ReadSnapshotChunkResponse{}
	if err := agent.ReadSystemSQLiteSnapshotChunk(&systemsqlite.ReadSnapshotChunkRequest{ProtocolVersion: 999}, &chunk); err != nil || chunk.Error == "" {
		t.Fatalf("read protocol response = %+v, %v", chunk, err)
	}

	var release systemsqlite.ReleaseSnapshotResponse
	if err := agent.ReleaseSystemSQLiteSnapshot(nil, &release); err != nil || release.Error == "" {
		t.Fatalf("nil release request = %+v, %v", release, err)
	}
	release = systemsqlite.ReleaseSnapshotResponse{}
	if err := agent.ReleaseSystemSQLiteSnapshot(&systemsqlite.ReleaseSnapshotRequest{ProtocolVersion: 999}, &release); err != nil || release.Error == "" {
		t.Fatalf("release protocol response = %+v, %v", release, err)
	}

	var optimize systemsqlite.OptimizeResponse
	if err := agent.OptimizeSystemSQLiteDatabase(nil, &optimize); err != nil || optimize.Error == "" {
		t.Fatalf("nil optimize request = %+v, %v", optimize, err)
	}
	optimize = systemsqlite.OptimizeResponse{}
	if err := agent.OptimizeSystemSQLiteDatabase(&systemsqlite.DatabaseRequest{ProtocolVersion: 999}, &optimize); err != nil || optimize.Error == "" {
		t.Fatalf("optimize protocol response = %+v, %v", optimize, err)
	}

	unavailable := &Agent{}
	list = systemsqlite.ListResponse{}
	if err := unavailable.ListSystemSQLiteDatabases(&systemsqlite.ListRequest{ProtocolVersion: systemsqlite.ProtocolVersion}, &list); err != nil || list.Error == "" {
		t.Fatalf("unavailable manager response = %+v, %v", list, err)
	}
}

func TestSystemSQLiteRPCRoundTripUsesOpaqueSnapshotToken(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "panel.sqlite3")
	createAgentSQLiteTestDatabase(t, databasePath)
	agent := &Agent{sqliteAdmin: newAgentSQLiteTestManager(t, []systemsqlite.Definition{
		agentSQLiteTestDefinition(systemsqlite.DatabaseComponentCatalog, databasePath, false),
	})}
	request := &systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      systemsqlite.DatabaseComponentCatalog,
	}

	var list systemsqlite.ListResponse
	if err := agent.ListSystemSQLiteDatabases(&systemsqlite.ListRequest{ProtocolVersion: systemsqlite.ProtocolVersion}, &list); err != nil || !list.Success || len(list.Databases) != 1 {
		t.Fatalf("ListSystemSQLiteDatabases() = %+v, %v", list, err)
	}
	var check systemsqlite.CheckResponse
	if err := agent.CheckSystemSQLiteDatabase(request, &check); err != nil || !check.Success || !check.Check.IntegrityOK {
		t.Fatalf("CheckSystemSQLiteDatabase() = %+v, %v", check, err)
	}
	var snapshot systemsqlite.SnapshotResponse
	if err := agent.CreateSystemSQLiteSnapshot(request, &snapshot); err != nil || !snapshot.Success || len(snapshot.Snapshot.Token) != 64 {
		t.Fatalf("CreateSystemSQLiteSnapshot() = %+v, %v", snapshot, err)
	}
	if strings.Contains(snapshot.Snapshot.Token, databasePath) {
		t.Fatalf("snapshot token leaked its path: %q", snapshot.Snapshot.Token)
	}

	var downloaded []byte
	var offset int64
	for {
		var chunk systemsqlite.ReadSnapshotChunkResponse
		err := agent.ReadSystemSQLiteSnapshotChunk(&systemsqlite.ReadSnapshotChunkRequest{
			ProtocolVersion: systemsqlite.ProtocolVersion,
			Token:           snapshot.Snapshot.Token, Offset: offset, MaxBytes: 97,
		}, &chunk)
		if err != nil || !chunk.Success {
			t.Fatalf("ReadSystemSQLiteSnapshotChunk() = %+v, %v", chunk, err)
		}
		if chunk.DatabaseID != systemsqlite.DatabaseComponentCatalog {
			t.Fatalf("chunk database ID = %q", chunk.DatabaseID)
		}
		downloaded = append(downloaded, chunk.Data...)
		offset = chunk.NextOffset
		if chunk.EOF {
			break
		}
	}
	if int64(len(downloaded)) != snapshot.Snapshot.SizeBytes {
		t.Fatalf("downloaded bytes = %d, want %d", len(downloaded), snapshot.Snapshot.SizeBytes)
	}
	var release systemsqlite.ReleaseSnapshotResponse
	if err := agent.ReleaseSystemSQLiteSnapshot(&systemsqlite.ReleaseSnapshotRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		Token:           snapshot.Snapshot.Token,
	}, &release); err != nil || !release.Success || !release.Released {
		t.Fatalf("ReleaseSystemSQLiteSnapshot() = %+v, %v", release, err)
	}
}

func TestSystemSQLiteRPCNeverLeaksManagedPaths(t *testing.T) {
	root := t.TempDir()
	secretPath := filepath.Join(root, "secret-control.sqlite3")
	if err := os.WriteFile(secretPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	definition := agentSQLiteTestDefinition(systemsqlite.DatabaseComponentCatalog, secretPath, false)
	definition.SnapshotAllowed = false
	agent := &Agent{sqliteAdmin: newAgentSQLiteTestManager(t, []systemsqlite.Definition{definition})}
	request := &systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      systemsqlite.DatabaseComponentCatalog,
	}

	var check systemsqlite.CheckResponse
	if err := agent.CheckSystemSQLiteDatabase(request, &check); err != nil || check.Error == "" {
		t.Fatalf("malformed check response = %+v, %v", check, err)
	}
	var snapshot systemsqlite.SnapshotResponse
	if err := agent.CreateSystemSQLiteSnapshot(request, &snapshot); err != nil || snapshot.Error == "" {
		t.Fatalf("malformed snapshot response = %+v, %v", snapshot, err)
	}
	var list systemsqlite.ListResponse
	if err := agent.ListSystemSQLiteDatabases(&systemsqlite.ListRequest{ProtocolVersion: systemsqlite.ProtocolVersion}, &list); err != nil || !list.Success {
		t.Fatalf("malformed list response = %+v, %v", list, err)
	}
	encoded, err := json.Marshal(struct {
		Check    systemsqlite.CheckResponse
		Snapshot systemsqlite.SnapshotResponse
		List     systemsqlite.ListResponse
	}{check, snapshot, list})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), secretPath) || strings.Contains(string(encoded), "not a database") {
		t.Fatalf("RPC response leaked managed details: %s", encoded)
	}
}

func TestSystemSQLiteRPCRejectsUnknownAndImmutableOptimize(t *testing.T) {
	catalogPath := filepath.Join(t.TempDir(), "components-v2.db")
	createAgentSQLiteTestDatabase(t, catalogPath)
	definition := agentSQLiteTestDefinition(systemsqlite.DatabaseComponentCatalog, catalogPath, false)
	definition.Optimizable = false
	agent := &Agent{sqliteAdmin: newAgentSQLiteTestManager(t, []systemsqlite.Definition{definition})}

	var check systemsqlite.CheckResponse
	if err := agent.CheckSystemSQLiteDatabase(&systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      "../../unmanaged.sqlite3",
	}, &check); err != nil || check.Error == "" {
		t.Fatalf("unknown database response = %+v, %v", check, err)
	}
	var optimize systemsqlite.OptimizeResponse
	if err := agent.OptimizeSystemSQLiteDatabase(&systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      systemsqlite.DatabaseComponentCatalog,
	}, &optimize); err != nil || optimize.Error == "" || strings.Contains(optimize.Error, catalogPath) {
		t.Fatalf("immutable optimize response = %+v, %v", optimize, err)
	}

	result, err := agent.sqliteAdmin.Check(context.Background(), systemsqlite.DatabaseComponentCatalog)
	if err != nil || !result.IntegrityOK {
		t.Fatalf("immutable catalog check = %+v, %v", result, err)
	}
}
