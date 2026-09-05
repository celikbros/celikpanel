package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Domain Database Management Handlers
//
// These endpoints keep the v1 URL and payload shape the domain-detail page
// uses, but store metadata in the same tables as the v2 flow (databases_v2 +
// database_users + database_user_grants) so both screens see one truth.
// Engine-side work stays on the privileged agent RPC.
//
// Bu uç noktalar domain-detay sayfasının kullandığı v1 URL ve gövde biçimini
// korur; ama meta veriyi v2 akışıyla aynı tablolara yazar (databases_v2 +
// database_users + database_user_grants), böylece iki ekran tek gerçeği
// görür. Motor tarafı işler yetkili agent RPC'sinde kalır.

// DatabaseInfo represents a database associated with a domain
type DatabaseInfo struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"` // mysql or postgresql
	User      string `json:"user"`
	CreatedAt string `json:"created_at"`
	Size      string `json:"size,omitempty"`
}

// CreateDatabaseRequest represents a request to create a database
type CreateDatabaseRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // mysql or postgresql
	Password string `json:"password"`
}

// serverTypeNameFor maps the API's database type to the
// database_server_types.name the v2 tables use.
// serverTypeNameFor, API'nin veritabanı tipini v2 tablolarının kullandığı
// database_server_types.name değerine eşler.
func serverTypeNameFor(apiType string) string {
	if apiType == "mysql" {
		return "mariadb"
	}
	return apiType
}

// apiTypeNameFor is the reverse mapping, for responses and agent calls.
// apiTypeNameFor, yanıtlar ve agent çağrıları için ters eşlemedir.
func apiTypeNameFor(serverType string) string {
	if serverType == "mariadb" {
		return "mysql"
	}
	return serverType
}

// availableDatabaseTypes returns only the active engines registered for one
// subscription. The domain dispatcher has already authorized the domain; this
// query deliberately stays on that domain's tenant boundary and never asks the
// global host capability or agent surfaces.
func availableDatabaseTypes(ctx context.Context, pool *sql.DB, subscriptionID int) ([]string, error) {
	rows, err := pool.QueryContext(ctx, `
		SELECT DISTINCT dst.name
		FROM database_servers ds
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE ds.subscription_id = ?
		  AND ds.status = 'active'
		  AND dst.name IN ('mariadb', 'postgresql')
		ORDER BY dst.name
	`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := make([]string, 0, 2)
	for rows.Next() {
		var serverType string
		if err := rows.Scan(&serverType); err != nil {
			return nil, err
		}
		types = append(types, apiTypeNameFor(serverType))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return types, nil
}

// handleDomainDatabases handles GET/POST for domain databases
func (p *Panel) handleDomainDatabases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract domain ID
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/databases")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	if r.Method == "GET" {
		p.handleGetDomainDatabases(w, r, domainID)
	} else if r.Method == "POST" {
		p.handleCreateDatabase(w, r, domainID)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleGetDomainDatabases(w http.ResponseWriter, r *http.Request, domainID int) {
	ctx := r.Context()
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	availableTypes, err := availableDatabaseTypes(ctx, pool, domain.SubscriptionID)
	if err != nil {
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}

	// The first granted user is the one this endpoint auto-created with the
	// database, so it matches what create returned.
	// İlk yetkilendirilen kullanıcı bu ucun veritabanıyla birlikte otomatik
	// oluşturduğudur; create'in döndürdüğüyle eşleşir.
	rows, err := pool.QueryContext(ctx, `
		SELECT d.id, d.name, dst.name,
		       COALESCE((SELECT u.username
		                 FROM database_user_grants g
		                 JOIN database_users u ON u.id = g.user_id
		                 WHERE g.database_id = d.id
		                 ORDER BY g.id LIMIT 1), ''),
		       d.created_at
		FROM databases_v2 d
		JOIN database_servers ds ON ds.id = d.server_id
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE d.domain_id = ?
		ORDER BY d.created_at DESC, d.id DESC
	`, domainID)
	if err != nil {
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	databases := make([]DatabaseInfo, 0)
	for rows.Next() {
		var db DatabaseInfo
		var serverType string
		if err := rows.Scan(&db.ID, &db.Name, &serverType, &db.User, &db.CreatedAt); err != nil {
			http.Error(w, "Failed to load databases", http.StatusInternalServerError)
			return
		}
		db.Type = apiTypeNameFor(serverType)
		databases = append(databases, db)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Failed to load databases", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"domain_id":       domain.ID,
		"domain_name":     domain.Name,
		"databases":       databases,
		"available_types": availableTypes,
	}

	json.NewEncoder(w).Encode(response)
}

func (p *Panel) handleCreateDatabase(w http.ResponseWriter, r *http.Request, domainID int) {
	var req CreateDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.Name == "" || req.Type == "" || req.Password == "" {
		http.Error(w, "Name, type, and password are required", http.StatusBadRequest)
		return
	}

	if req.Type != "mysql" && req.Type != "postgresql" {
		http.Error(w, "Type must be 'mysql' or 'postgresql'", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	pool := p.db.GetDB()

	// Get domain info
	domainRepo := repositories.NewPostgresDomainRepository(pool)
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Generate database name (prefix with domain for organization)
	dbName := fmt.Sprintf("%s_%s", sanitizeName(domain.Name), req.Name)
	dbUser := dbName + "_user"

	// Databases live on the subscription's registered engine of the requested
	// type; auto-registration keeps this working without manual server setup.
	// Veritabanları aboneliğin istenen tipteki kayıtlı motorunda yaşar;
	// otomatik kayıt, elle sunucu eklemeye gerek bırakmaz.
	if err := p.ensureInstalledDBServers(ctx, domain.SubscriptionID); err != nil {
		writeServerError(w, err)
		return
	}

	var serverID int
	err = pool.QueryRowContext(ctx, `
		SELECT ds.id
		FROM database_servers ds
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE ds.subscription_id = ? AND dst.name = ?
		ORDER BY ds.is_default DESC, ds.id
		LIMIT 1
	`, domain.SubscriptionID, serverTypeNameFor(req.Type)).Scan(&serverID)
	if err == sql.ErrNoRows {
		writeClientError(w, http.StatusServiceUnavailable,
			fmt.Sprintf("no %s server is installed on this host", req.Type))
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Check if database already exists on that server
	var exists bool
	err = pool.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM databases_v2 WHERE server_id = ? AND name = ?)",
		serverID, dbName).Scan(&exists)
	if err != nil {
		http.Error(w, "Failed to check database existence", http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "Database already exists", http.StatusConflict)
		return
	}

	// Create database via agent RPC
	var agentResp transport.CreateDatabaseResponse

	sealedPassword, err := p.secrets.Encrypt(req.Password)
	if err != nil {
		writeServerError(w, err)
		return
	}

	agentReq := transport.CreateDatabaseRequest{
		Type:     req.Type,
		Name:     dbName,
		User:     dbUser,
		Password: req.Password,
	}

	err = p.callAgent("Agent.CreateDatabase", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		writeAgentError(w, err, agentResp.Error)
		return
	}

	// Record metadata in one transaction so the three v2 rows stay consistent.
	// Meta veriyi tek işlemde kaydet; üç v2 satırı tutarlı kalsın.
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name)
		VALUES (?, ?, ?, ?)
	`, serverID, domain.SubscriptionID, domainID, dbName)
	if err != nil {
		http.Error(w, "Failed to store database info", http.StatusInternalServerError)
		return
	}
	dbID, err := res.LastInsertId()
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Reuse the metadata row if this user already exists on the server
	// (UNIQUE(server_id, username)); the agent create is idempotent too.
	// Kullanıcı sunucuda zaten kayıtlıysa meta veri satırını yeniden kullan
	// (UNIQUE(server_id, username)); agent tarafı da idempotenttir.
	var userID int64
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM database_users WHERE server_id = ? AND username = ?",
		serverID, dbUser).Scan(&userID)
	if err == sql.ErrNoRows {
		res, err = tx.ExecContext(ctx, `
			INSERT INTO database_users (server_id, subscription_id, username, password)
			VALUES (?, ?, ?, ?)
		`, serverID, domain.SubscriptionID, dbUser, sealedPassword)
		if err == nil {
			userID, err = res.LastInsertId()
		}
	}
	if err != nil {
		http.Error(w, "Failed to store database user", http.StatusInternalServerError)
		return
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO database_user_grants (database_id, user_id, privileges)
		VALUES (?, ?, 'ALL')
	`, dbID, userID)
	if err != nil {
		http.Error(w, "Failed to store database grant", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"id":     dbID,
		"name":   dbName,
		"user":   dbUser,
		"type":   req.Type,
	})
}

// handleDeleteDatabase handles DELETE for a specific database
func (p *Panel) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID and database ID from URL
	// /api/v1/domains/:domain_id/databases/:db_id
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/databases/")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	dbID, err := getIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "Invalid database ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	pool := p.db.GetDB()

	// Scope by domain so a database can only be deleted through its own
	// domain (the dispatcher already verified the caller owns that domain).
	// Domain'e göre süz; bir veritabanı yalnızca kendi domain'i üzerinden
	// silinebilsin (yönlendirici, çağıranın o domain'in sahibi olduğunu
	// zaten doğruladı).
	var dbName, serverType string
	err = pool.QueryRowContext(ctx, `
		SELECT d.name, dst.name
		FROM databases_v2 d
		JOIN database_servers ds ON ds.id = d.server_id
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE d.id = ? AND d.domain_id = ?
	`, dbID, domainID).Scan(&dbName, &serverType)
	if err != nil {
		http.Error(w, "Database not found", http.StatusNotFound)
		return
	}

	// The auto-created user rides along with the database.
	// Otomatik oluşturulan kullanıcı veritabanıyla birlikte gider.
	var userID int
	var dbUser string
	err = pool.QueryRowContext(ctx, `
		SELECT u.id, u.username
		FROM database_user_grants g
		JOIN database_users u ON u.id = g.user_id
		WHERE g.database_id = ?
		ORDER BY g.id LIMIT 1
	`, dbID).Scan(&userID, &dbUser)
	if err != nil && err != sql.ErrNoRows {
		writeServerError(w, err)
		return
	}

	// Delete database via agent RPC
	var agentResp transport.DeleteDatabaseResponse

	agentReq := transport.DeleteDatabaseRequest{
		Type: apiTypeNameFor(serverType),
		Name: dbName,
		User: dbUser,
	}

	err = p.callAgent("Agent.DeleteDatabase", agentReq, &agentResp)
	if err != nil || !agentResp.Success {
		writeAgentError(w, err, agentResp.Error)
		return
	}

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()

	// Deleting the database cascades its grants.
	// Veritabanını silmek yetkilerini de beraberinde siler.
	_, err = tx.ExecContext(ctx, "DELETE FROM databases_v2 WHERE id = ?", dbID)
	if err != nil {
		http.Error(w, "Failed to remove database record", http.StatusInternalServerError)
		return
	}

	if userID != 0 {
		// Drop the user record only when nothing else references it.
		// Kullanıcı kaydını yalnızca başka hiçbir yetki ona başvurmuyorken sil.
		_, err = tx.ExecContext(ctx, `
			DELETE FROM database_users
			WHERE id = ? AND NOT EXISTS (SELECT 1 FROM database_user_grants WHERE user_id = ?)
		`, userID, userID)
		if err != nil {
			http.Error(w, "Failed to remove database user record", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// sanitizeName turns a domain into the leading fragment of a generated
// database name. Dropping and folding characters cannot fix the first one, so
// the finished fragment goes through the one place that decides that (R-051):
// a domain such as 1and1.com or 360.com is legal, and without this the name it
// produced began with a digit and the agent's validator refused it, so no
// database could be created for such a domain at all.
//
// sanitizeName bir domain'i üretilmiş veritabanı adının baştaki parçasına
// çevirir. Karakter atmak ilk karakteri düzeltemez; bu yüzden tamamlanmış
// parça, bu kararın verildiği tek yerden geçer (R-051). 1and1.com ya da
// 360.com meşru domain'lerdir ve bu olmadan ürettikleri ad rakamla başlıyor,
// agent'ın doğrulayıcısı reddediyordu.
func sanitizeName(name string) string {
	// Remove dots and hyphens, replace with underscores
	result := ""
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			result += string(char)
		} else if char == '.' || char == '-' {
			result += "_"
		}
	}
	return services.EnsureIdentifierLeader(result)
}
