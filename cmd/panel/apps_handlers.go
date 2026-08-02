package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

type appCatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	RequiresDB  bool   `json:"requires_db"`
	RequiresPHP bool   `json:"requires_php"`
}

var appCatalog = []appCatalogEntry{
	{
		ID:          "wordpress",
		Name:        "WordPress",
		Description: "The world's most popular CMS — blogs, sites, shops.",
		Icon:        "wordpress",
		RequiresDB:  true,
		RequiresPHP: true,
	},
}

var appInstallLocks [64]sync.Mutex

var errAppDatabaseExists = errors.New("WordPress database metadata already exists")

func appInstallMutex(domainID int) *sync.Mutex {
	if domainID < 0 {
		domainID = -domainID
	}
	return &appInstallLocks[uint(domainID)%uint(len(appInstallLocks))]
}

func (p *Panel) handleAppCatalog(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"apps": appCatalog})
}

type appInstallTarget struct {
	SiteID         int
	SubscriptionID int
	Domain         string
	DocumentRoot   string
	ProjectType    string
}

type appDatabaseLedger struct {
	OperationID string
	DatabaseID  int64
	UserID      int64
}

func (p *Panel) handleAppInstall(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		App string `json:"app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.App != "wordpress" {
		writeClientError(w, http.StatusBadRequest, "unknown application")
		return
	}

	installMu := appInstallMutex(domainID)
	installMu.Lock()
	defer installMu.Unlock()

	target, err := p.resolveAppInstallTarget(r.Context(), domainID)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "site not found")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	if target.ProjectType != "php" {
		writeClientError(w, http.StatusConflict, "WordPress requires a PHP site")
		return
	}
	if !p.requireEntitlement(w, r, target.SubscriptionID, "app_installer") {
		return
	}
	// A newly-created subscription may not have lazy local database metadata
	// yet. Discover installed engines before selecting the immutable server row.
	if err := p.ensureInstalledDBServers(r.Context(), target.SubscriptionID); err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.checkSubscriptionQuota(r.Context(), target.SubscriptionID, quotaDBs); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}

	base := sanitizeDBIdent(services.SiteUsername(target.Domain))
	databaseName := base + "_wp"
	databaseUser := base + "_wp"
	databasePassword, err := randomDBPassword()
	if err != nil {
		writeServerError(w, fmt.Errorf("generate application database password: %w", err))
		return
	}
	serverID, err := p.preflightAppDatabase(
		r.Context(), target.SubscriptionID, databaseName, databaseUser,
	)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusServiceUnavailable, "no MariaDB server is installed on this host")
		return
	}
	if errors.Is(err, errAppDatabaseExists) {
		writeClientError(w, http.StatusConflict, "WordPress is already installed or its database is already reserved")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	operationID, err := randomHexToken(16)
	if err != nil {
		writeServerError(w, fmt.Errorf("generate application operation ID: %w", err))
		return
	}
	cleanupToken, err := randomHexToken(32)
	if err != nil {
		writeServerError(w, fmt.Errorf("generate application cleanup token: %w", err))
		return
	}
	sealedPassword, err := p.secrets.Encrypt(databasePassword)
	if err != nil {
		writeServerError(w, err)
		return
	}
	sealedCleanupToken, err := p.secrets.Encrypt(cleanupToken)
	if err != nil {
		writeServerError(w, err)
		return
	}

	ledger, err := p.storeAppDatabase(
		r.Context(), serverID, target.SubscriptionID, domainID,
		target.SiteID, operationID, databaseName, databaseUser,
		sealedPassword, sealedCleanupToken,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.setAppInstallStatus(operationID, "database_creating", ""); err != nil {
		writeServerError(w, fmt.Errorf("persist application database creation state: %w", err))
		return
	}

	createRequest := transport.CreateDatabaseRequest{
		Type:         "mysql",
		Name:         databaseName,
		User:         databaseUser,
		Password:     databasePassword,
		OperationID:  operationID,
		CleanupToken: cleanupToken,
	}
	var createResponse transport.CreateDatabaseResponse
	createErr := p.callAgent("Agent.CreateDatabase", createRequest, &createResponse)
	if createErr != nil {
		if statusErr := p.persistAppInstallOutcome(operationID, "needs_review", createErr.Error(), createErr); statusErr != nil {
			writeServerError(w, statusErr)
			return
		}
		// The agent may have completed the operation before the response was
		// lost. Never issue a destructive cleanup after a transport failure.
		writeAgentError(w, createErr, createResponse.Error)
		return
	}
	if !createResponse.Success || createResponse.Error != "" {
		status := "failed"
		statusErr := createResponse.Error
		if createResponse.CleanupIncomplete {
			status = "needs_review"
		} else if cleanupErr := p.removeAppDatabaseLedger(&ledger); cleanupErr != nil {
			status = "needs_review"
			statusErr = errors.Join(errors.New(statusErr), cleanupErr).Error()
		}
		if persistErr := p.persistAppInstallOutcome(operationID, status, statusErr, nil); persistErr != nil {
			writeServerError(w, persistErr)
			return
		}
		writeAgentError(w, nil, createResponse.Error)
		return
	}
	if !createResponse.OwnedByOperation {
		err := errors.New("database creation did not return operation ownership proof")
		if statusErr := p.persistAppInstallOutcome(operationID, "needs_review", err.Error(), err); statusErr != nil {
			writeServerError(w, statusErr)
			return
		}
		writeServerError(w, err)
		return
	}
	if err := p.setAppInstallStatus(operationID, "database_ready", ""); err != nil {
		writeServerError(w, fmt.Errorf("persist application database state: %w", err))
		return
	}
	if err := p.setAppInstallStatus(operationID, "files_installing", ""); err != nil {
		writeServerError(w, fmt.Errorf("persist application file state: %w", err))
		return
	}

	var installResponse transport.InstallWordPressResponse
	installErr := p.callAgent("Agent.InstallWordPress", &transport.InstallWordPressRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		OperationID:         operationID,
		SiteID:              target.SiteID,
		SubscriptionID:      target.SubscriptionID,
		DomainID:            domainID,
		Domain:              target.Domain,
		DBName:              databaseName,
		DBUser:              databaseUser,
		DBPass:              databasePassword,
		DBHost:              "localhost",
		Username:            services.SiteUsername(target.Domain),
	}, &installResponse)
	if installErr != nil {
		if statusErr := p.persistAppInstallOutcome(operationID, "needs_review", installErr.Error(), installErr); statusErr != nil {
			writeServerError(w, statusErr)
			return
		}
		// A lost response may mean WordPress is already live. Keep its database.
		writeAgentError(w, installErr, installResponse.Error)
		return
	}
	if !installResponse.Installed || installResponse.Error != "" {
		if !installResponse.CompensationSafe {
			if statusErr := p.persistAppInstallOutcome(operationID, "needs_review", installResponse.Error, nil); statusErr != nil {
				writeServerError(w, statusErr)
				return
			}
			writeAgentError(w, nil, installResponse.Error)
			return
		}
		cleanupErr := p.compensateAppDatabase(
			&ledger, databaseName, databaseUser, operationID, cleanupToken,
		)
		if cleanupErr != nil {
			if statusErr := p.persistAppInstallOutcome(operationID, "needs_review", cleanupErr.Error(), cleanupErr); statusErr != nil {
				writeServerError(w, statusErr)
				return
			}
			writeAgentError(w, cleanupErr, installResponse.Error)
			return
		}
		if statusErr := p.persistAppInstallOutcome(operationID, "failed", installResponse.Error, nil); statusErr != nil {
			writeServerError(w, statusErr)
			return
		}
		writeAgentError(w, nil, installResponse.Error)
		return
	}
	if err := p.setAppInstallStatus(operationID, "applied", ""); err != nil {
		writeServerError(w, fmt.Errorf("persist completed application state: %w", err))
		return
	}

	p.audit(r, "app.install:"+request.App, "domain", domainID)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"detail":    installResponse.Detail,
		"setup_url": "https://" + target.Domain + "/wp-admin/install.php",
	})
}

func (p *Panel) resolveAppInstallTarget(ctx context.Context, domainID int) (appInstallTarget, error) {
	var target appInstallTarget
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.id, d.subscription_id, d.name, s.document_root,
		       COALESCE(s.project_type, 'php')
		FROM domains d
		JOIN sites s ON s.domain_id = d.id
		WHERE d.id = ?
		ORDER BY s.id
		LIMIT 1
	`, domainID).Scan(
		&target.SiteID,
		&target.SubscriptionID,
		&target.Domain,
		&target.DocumentRoot,
		&target.ProjectType,
	)
	if err != nil {
		return appInstallTarget{}, err
	}
	if target.SiteID <= 0 || target.SubscriptionID <= 0 || strings.TrimSpace(target.Domain) == "" {
		return appInstallTarget{}, fmt.Errorf("site has an invalid immutable identity")
	}
	if err := hostingpath.ValidateDocumentRoot(
		strings.TrimSpace(target.DocumentRoot), target.SubscriptionID, domainID,
	); err != nil {
		return appInstallTarget{}, fmt.Errorf("stored document root failed validation: %w", err)
	}
	target.ProjectType = strings.ToLower(strings.TrimSpace(target.ProjectType))
	return target, nil
}

func (p *Panel) preflightAppDatabase(
	ctx context.Context,
	subscriptionID int,
	databaseName string,
	databaseUser string,
) (int, error) {
	var serverID int
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT ds.id
		FROM database_servers ds
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE ds.subscription_id = ?
		  AND dst.name = 'mariadb'
		  AND lower(trim(COALESCE(ds.status, ''))) = 'active'
		  AND lower(trim(COALESCE(ds.host, ''))) = 'localhost'
		  AND ds.port = 3306
		ORDER BY ds.is_default DESC, ds.id
		LIMIT 1
	`, subscriptionID).Scan(&serverID)
	if err != nil {
		return 0, err
	}

	var databaseExists, userExists bool
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM databases_v2 WHERE server_id = ? AND name = ?),
			EXISTS(SELECT 1 FROM database_users WHERE server_id = ? AND username = ?)
	`, serverID, databaseName, serverID, databaseUser).Scan(&databaseExists, &userExists); err != nil {
		return 0, err
	}
	if databaseExists || userExists {
		return 0, errAppDatabaseExists
	}
	return serverID, nil
}

func (p *Panel) storeAppDatabase(
	ctx context.Context,
	serverID int,
	subscriptionID int,
	domainID int,
	siteID int,
	operationID string,
	databaseName string,
	databaseUser string,
	sealedPassword string,
	sealedCleanupToken string,
) (appDatabaseLedger, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return appDatabaseLedger{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO application_install_operations (
			operation_id, app_id, domain_id, subscription_id, site_id,
			database_server_id, database_name, database_user,
			cleanup_token_encrypted, status
		) VALUES (?, 'wordpress', ?, ?, ?, ?, ?, ?, ?, 'reserved')
	`, operationID, domainID, subscriptionID, siteID, serverID,
		databaseName, databaseUser, sealedCleanupToken); err != nil {
		return appDatabaseLedger{}, fmt.Errorf("reserve application install operation: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name)
		VALUES (?, ?, ?, ?)
	`, serverID, subscriptionID, domainID, databaseName)
	if err != nil {
		return appDatabaseLedger{}, fmt.Errorf("store application database: %w", err)
	}
	databaseID, err := result.LastInsertId()
	if err != nil {
		return appDatabaseLedger{}, err
	}
	result, err = tx.ExecContext(ctx, `
		INSERT INTO database_users (server_id, subscription_id, username, password)
		VALUES (?, ?, ?, ?)
	`, serverID, subscriptionID, databaseUser, sealedPassword)
	if err != nil {
		return appDatabaseLedger{}, fmt.Errorf("store application database user: %w", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return appDatabaseLedger{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO database_user_grants (database_id, user_id, privileges)
		VALUES (?, ?, 'ALL')
	`, databaseID, userID); err != nil {
		return appDatabaseLedger{}, fmt.Errorf("store application database grant: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return appDatabaseLedger{}, err
	}
	return appDatabaseLedger{
		OperationID: operationID,
		DatabaseID:  databaseID,
		UserID:      userID,
	}, nil
}

func (p *Panel) setAppInstallStatus(operationID, status, detail string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE application_install_operations
		SET status = ?, last_error = NULLIF(?, ''), updated_at = datetime('now')
		WHERE operation_id = ?
	`, status, detail, operationID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("application operation %s was not found", operationID)
	}
	return nil
}

// persistAppInstallOutcome makes a failed status write part of the reported
// operation failure. Callers must never hide a stale ledger behind the agent's
// original error, because recovery decisions depend on this durable state.
func (p *Panel) persistAppInstallOutcome(operationID, status, detail string, cause error) error {
	if err := p.setAppInstallStatus(operationID, status, detail); err != nil {
		persistErr := fmt.Errorf("persist application operation %s status %q: %w", operationID, status, err)
		if cause != nil {
			return errors.Join(cause, persistErr)
		}
		return persistErr
	}
	return nil
}

func (p *Panel) removeAppDatabaseLedger(ledger *appDatabaseLedger) error {
	if ledger == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := p.db.GetDB().BeginTx(cleanupCtx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(cleanupCtx,
		"DELETE FROM databases_v2 WHERE id = ?", ledger.DatabaseID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(cleanupCtx, `
		DELETE FROM database_users
		WHERE id = ?
		  AND NOT EXISTS (SELECT 1 FROM database_user_grants WHERE user_id = ?)
	`, ledger.UserID, ledger.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

// compensateAppDatabase removes a physical database only when the agent can
// verify the operation-specific token stored inside that exact database. The
// administrator ledger remains visible unless physical cleanup succeeds.
func (p *Panel) compensateAppDatabase(
	ledger *appDatabaseLedger,
	databaseName string,
	databaseUser string,
	operationID string,
	cleanupToken string,
) error {
	var response transport.DeleteDatabaseResponse
	rpcErr := p.callAgent("Agent.DeleteDatabase", transport.DeleteDatabaseRequest{
		Type:                  "mysql",
		Name:                  databaseName,
		User:                  databaseUser,
		RequireUserCleanup:    true,
		RequireOwnershipProof: true,
		OperationID:           operationID,
		CleanupToken:          cleanupToken,
	}, &response)
	if rpcErr != nil {
		return rpcErr
	}
	if !response.Success || response.Error != "" {
		return fmt.Errorf("agent refused database cleanup: %s", response.Error)
	}
	if ledger == nil {
		return nil
	}
	return p.removeAppDatabaseLedger(ledger)
}

// recoverInterruptedAppInstallOperations never guesses whether a privileged
// RPC completed. It makes every pre-terminal operation explicitly visible for
// administrator reconciliation after a restart.
func (p *Panel) recoverInterruptedAppInstallOperations(ctx context.Context) (int64, error) {
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE application_install_operations
		SET status = 'needs_review',
		    last_error = 'panel restarted before the application operation reached a terminal state',
		    updated_at = datetime('now')
		WHERE status IN ('reserved', 'database_creating', 'database_ready', 'files_installing')
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func sanitizeDBIdent(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(value) {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '_' {
			builder.WriteRune(character)
		}
	}
	result := builder.String()
	if len(result) > 48 {
		result = result[:48]
	}
	if result == "" {
		result = "app"
	}
	return result
}

func randomDBPassword() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	password := make([]byte, 24)
	maximum := big.NewInt(int64(len(charset)))
	for index := range password {
		value, err := rand.Int(rand.Reader, maximum)
		if err != nil {
			return "", err
		}
		password[index] = charset[value.Int64()]
	}
	return string(password), nil
}

func randomHexToken(byteCount int) (string, error) {
	if byteCount <= 0 {
		return "", fmt.Errorf("invalid random token size")
	}
	random := make([]byte, byteCount)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}
