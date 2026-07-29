package main

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alicelik/celikpanel/internal/manifestv2"
	"github.com/alicelik/celikpanel/internal/systemsqlite"
)

const (
	systemSQLiteListTimeout     = 10 * time.Second
	systemSQLiteCheckTimeout    = 30 * time.Second
	systemSQLiteSnapshotTimeout = 2 * time.Minute
	systemSQLiteOptimizeTimeout = 30 * time.Second
)

func systemSQLiteDefinitions() []systemsqlite.Definition {
	roundcubeWriterUID, roundcubeWriterGID, roundcubeWriterIdentitySet :=
		systemSQLiteWebWriterIdentity()
	panelPath := os.Getenv("CELIKPANEL_PANEL_DB")
	if panelPath == "" {
		if dataDir := os.Getenv("CELIKPANEL_DATA_DIR"); dataDir != "" {
			panelPath = filepath.Join(dataDir, "celikpanel.db")
		} else {
			panelPath = "/var/lib/celikpanel/celikpanel.db"
		}
	}
	catalogPath := os.Getenv("CELIKPANEL_COMPONENT_CATALOG")
	if catalogPath == "" {
		catalogPath = manifestv2.DefaultCatalogPath
	}
	return []systemsqlite.Definition{
		{
			ID: systemsqlite.DatabasePanel, Name: "CelikPanel", Kind: "control-plane",
			Purpose: "Panel accounts, domains, settings, and operational metadata.",
			Path:    panelPath, PathHint: "panel-data / celikpanel.db", Mutable: true, Optimizable: false,
			SnapshotAllowed: false,
		},
		{
			ID: systemsqlite.DatabasePowerDNS, Name: "PowerDNS", Kind: "dns",
			Purpose: "Authoritative DNS zones served by PowerDNS.",
			Path:    pdnsDBPath(), PathHint: "powerdns-data / pdns.sqlite3", Mutable: true, Optimizable: true,
			SnapshotAllowed: true,
		},
		{
			ID: systemsqlite.DatabaseRoundcube, Name: "Roundcube", Kind: "webmail",
			Purpose:  "Roundcube webmail application state and user preferences.",
			Path:     filepath.Join(webmailBaseDir, "db", "roundcube.sqlite3"),
			PathHint: "webmail-data / roundcube.sqlite3", Mutable: true, Optimizable: true,
			SnapshotAllowed:   true,
			WriterUID:         roundcubeWriterUID,
			WriterGID:         roundcubeWriterGID,
			WriterIdentitySet: roundcubeWriterIdentitySet,
		},
		{
			ID: systemsqlite.DatabaseComponentCatalog, Name: "Component catalog", Kind: "catalog",
			Purpose: "Signed component and platform operation catalog.",
			Path:    catalogPath, PathHint: "component-catalog / components-v2.db",
			Mutable: false, Optimizable: false, SnapshotAllowed: true,
		},
	}
}

// systemSQLiteWebWriterIdentity resolves the non-root account that writes the Roundcube SQLite database.
// systemSQLiteWebWriterIdentity, Roundcube SQLite veritabanına yazan root olmayan hesabı çözümler.
func systemSQLiteWebWriterIdentity() (uint32, uint32, bool) {
	for _, name := range []string{"www-data", "nginx", "http"} {
		account, err := user.Lookup(name)
		if err != nil {
			continue
		}
		group, err := user.LookupGroup(name)
		if err != nil {
			continue
		}
		uid, uidErr := strconv.ParseUint(account.Uid, 10, 32)
		gid, gidErr := strconv.ParseUint(group.Gid, 10, 32)
		if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
			continue
		}
		return uint32(uid), uint32(gid), true
	}
	return 0, 0, false
}

func newSystemSQLiteManager() (*systemsqlite.Manager, error) {
	definitions := systemSQLiteDefinitions()
	snapshotRoot := systemSQLiteSnapshotDirectory()
	// Inventory remains visible when the platform cannot provide the Linux owner-UID boundary; mutable actions then fail closed.
	// Platform Linux sahip-UID sınırını sağlayamazsa envanter görünür kalır; değiştirilebilir işlemler güvenli biçimde reddedilir.
	mutableOperations, _ := systemsqlite.NewOwnerProcessMutableOperations(
		definitions,
		systemSQLiteOwnerWorkspaceRoot(),
	)
	return systemsqlite.NewManager(definitions, systemsqlite.Options{
		SnapshotRoot:      snapshotRoot,
		MutableOperations: mutableOperations,
	})
}

func systemSQLiteSnapshotDirectory() string {
	return filepath.Join(serviceMutationStateDirectory(), "system-sqlite-snapshots")
}

func systemSQLiteOwnerWorkspaceRoot() string {
	return "/tmp"
}

func (a *Agent) systemSQLiteManager() (*systemsqlite.Manager, error) {
	if a == nil || a.sqliteAdmin == nil {
		return nil, errors.New("system SQLite manager is not available")
	}
	return a.sqliteAdmin, nil
}

// ListSystemSQLiteDatabases returns only the fixed system database inventory.
// ListSystemSQLiteDatabases yalnızca sabit sistem veritabanı envanterini döndürür.
func (a *Agent) ListSystemSQLiteDatabases(
	req *systemsqlite.ListRequest,
	resp *systemsqlite.ListResponse,
) error {
	if req == nil {
		resp.Error = "system SQLite list request is required"
		return nil
	}
	if err := systemsqlite.ValidateProtocol(req.ProtocolVersion); err != nil {
		resp.Error = err.Error()
		return nil
	}
	manager, err := a.systemSQLiteManager()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), systemSQLiteListTimeout)
	defer cancel()
	databases, err := manager.List(ctx)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Databases = databases
	return nil
}

// CheckSystemSQLiteDatabase runs bounded consistency checks on one allowlisted database.
// CheckSystemSQLiteDatabase izin listesindeki tek veritabanında sınırlı tutarlılık denetimleri çalıştırır.
func (a *Agent) CheckSystemSQLiteDatabase(
	req *systemsqlite.DatabaseRequest,
	resp *systemsqlite.CheckResponse,
) error {
	if !validSystemSQLiteDatabaseRequest(req, &resp.Error) {
		return nil
	}
	manager, err := a.systemSQLiteManager()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), systemSQLiteCheckTimeout)
	defer cancel()
	result, err := manager.Check(ctx, req.DatabaseID)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Check = result
	return nil
}

// CreateSystemSQLiteSnapshot creates a private consistent snapshot and returns an opaque token.
// CreateSystemSQLiteSnapshot özel ve tutarlı bir anlık görüntü oluşturur, opak bir belirteç döndürür.
func (a *Agent) CreateSystemSQLiteSnapshot(
	req *systemsqlite.DatabaseRequest,
	resp *systemsqlite.SnapshotResponse,
) error {
	if !validSystemSQLiteDatabaseRequest(req, &resp.Error) {
		return nil
	}
	manager, err := a.systemSQLiteManager()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), systemSQLiteSnapshotTimeout)
	defer cancel()
	snapshot, err := manager.CreateSnapshot(ctx, req.DatabaseID)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Snapshot = snapshot
	return nil
}

// ReadSystemSQLiteSnapshotChunk reads a bounded range from the pinned snapshot descriptor.
// ReadSystemSQLiteSnapshotChunk sabitlenmiş anlık görüntü tanıtıcısından sınırlı bir aralık okur.
func (a *Agent) ReadSystemSQLiteSnapshotChunk(
	req *systemsqlite.ReadSnapshotChunkRequest,
	resp *systemsqlite.ReadSnapshotChunkResponse,
) error {
	if req == nil {
		resp.Error = "system SQLite snapshot read request is required"
		return nil
	}
	if err := systemsqlite.ValidateProtocol(req.ProtocolVersion); err != nil {
		resp.Error = err.Error()
		return nil
	}
	manager, err := a.systemSQLiteManager()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	result, err := manager.ReadSnapshotChunk(*req)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	*resp = result
	return nil
}

// ReleaseSystemSQLiteSnapshot closes and removes one opaque private snapshot.
// ReleaseSystemSQLiteSnapshot tek opak özel anlık görüntüyü kapatır ve kaldırır.
func (a *Agent) ReleaseSystemSQLiteSnapshot(
	req *systemsqlite.ReleaseSnapshotRequest,
	resp *systemsqlite.ReleaseSnapshotResponse,
) error {
	if req == nil {
		resp.Error = "system SQLite snapshot release request is required"
		return nil
	}
	if err := systemsqlite.ValidateProtocol(req.ProtocolVersion); err != nil {
		resp.Error = err.Error()
		return nil
	}
	manager, err := a.systemSQLiteManager()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	released, err := manager.ReleaseSnapshot(req.Token)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Released = released
	return nil
}

// OptimizeSystemSQLiteDatabase runs PRAGMA optimize without VACUUM on a mutable database.
// OptimizeSystemSQLiteDatabase değiştirilebilir veritabanında VACUUM olmadan PRAGMA optimize çalıştırır.
func (a *Agent) OptimizeSystemSQLiteDatabase(
	req *systemsqlite.DatabaseRequest,
	resp *systemsqlite.OptimizeResponse,
) error {
	if !validSystemSQLiteDatabaseRequest(req, &resp.Error) {
		return nil
	}
	manager, err := a.systemSQLiteManager()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), systemSQLiteOptimizeTimeout)
	defer cancel()
	result, err := manager.Optimize(ctx, req.DatabaseID)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Success = true
	resp.Result = result
	return nil
}

func validSystemSQLiteDatabaseRequest(req *systemsqlite.DatabaseRequest, errorMessage *string) bool {
	if req == nil {
		*errorMessage = "system SQLite database request is required"
		return false
	}
	if err := systemsqlite.ValidateProtocol(req.ProtocolVersion); err != nil {
		*errorMessage = err.Error()
		return false
	}
	return true
}
