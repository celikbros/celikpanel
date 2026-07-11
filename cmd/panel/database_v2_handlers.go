package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
)

// Helper function to extract ID from path
func getIDFromPath(path string) (int, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid path")
	}
	// Get last part as ID
	idStr := parts[len(parts)-1]
	return strconv.Atoi(idStr)
}

// Helper function to extract server_id from path like /api/v2/database-servers/123/databases
func getServerIDFromPath(path string) (int, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Path: api/v2/database-servers/{id}/databases
	if len(parts) >= 4 && parts[2] == "database-servers" {
		return strconv.Atoi(parts[3])
	}
	return 0, fmt.Errorf("server ID not found in path")
}

// Helper function to extract database_id from path like /api/v2/databases/123/grants
func getDatabaseIDFromPath(path string) (int, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Path: api/v2/databases/{id}/grants
	if len(parts) >= 3 && parts[2] == "databases" {
		return strconv.Atoi(parts[3])
	}
	return 0, fmt.Errorf("database ID not found in path")
}

// Database Server Management Endpoints

// handleListDatabaseServers lists all database servers for a subscription
func (p *Panel) handleListDatabaseServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subscriptionID, err := p.callerSubscriptionID(r)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	// Auto-register installed engines so the list is never empty when
	// MariaDB/PostgreSQL are running.
	// Kurulu motorları otomatik kaydet; böylece MariaDB/PostgreSQL çalışırken
	// liste asla boş kalmaz.
	p.ensureInstalledDBServers(ctx, subscriptionID)

	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	servers, err := serverRepo.ListBySubscription(ctx, subscriptionID)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Build response with type information
	type ServerResponse struct {
		ID        int    `json:"id"`
		TypeID    int    `json:"type_id"`
		TypeName  string `json:"type_name"`
		TypeIcon  string `json:"type_icon"`
		Name      string `json:"name"`
		Version   string `json:"version"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		IsDefault bool   `json:"is_default"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	}

	response := make([]ServerResponse, 0)
	for _, server := range servers {
		response = append(response, ServerResponse{
			ID:        server.ID,
			TypeID:    server.TypeID,
			TypeName:  server.TypeName,
			TypeIcon:  server.TypeIcon,
			Name:      server.Name,
			Version:   server.Version,
			Host:      server.Host,
			Port:      server.Port,
			IsDefault: server.IsDefault,
			Status:    server.Status,
			CreatedAt: server.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateDatabaseServer creates a new database server
func (p *Panel) handleCreateDatabaseV2Server(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subscriptionID, err := p.callerSubscriptionID(r)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	var req struct {
		TypeID       int    `json:"type_id"`
		Name         string `json:"name"`
		Version      string `json:"version"`
		Host         string `json:"host"`
		Port         int    `json:"port"`
		IsDefault    bool   `json:"is_default"`
		RootPassword string `json:"root_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate
	if req.Name == "" || req.Port == 0 {
		http.Error(w, "name and port are required", http.StatusBadRequest)
		return
	}

	// Create server
	server := &core.DatabaseServer{
		SubscriptionID:        subscriptionID,
		TypeID:                req.TypeID,
		Name:                  req.Name,
		Version:               req.Version,
		Host:                  req.Host,
		Port:                  req.Port,
		IsDefault:             req.IsDefault,
		RootPasswordEncrypted: req.RootPassword, // TODO: Encrypt
		Status:                "active",
	}

	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	if err := serverRepo.Create(ctx, server); err != nil {
		writeServerError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         server.ID,
		"name":       server.Name,
		"host":       server.Host,
		"port":       server.Port,
		"created_at": server.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// handleDeleteDatabaseServer deletes a database server
func (p *Panel) handleDeleteDatabaseV2Server(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Extract ID from path
	serverID, err := getIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), serverID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	if err := serverRepo.Delete(ctx, serverID); err != nil {
		writeServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// Database Management Endpoints (per server)

// handleListDatabases lists all databases for a server
func (p *Panel) handleListDatabasesV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Extract ID from path
	serverID, err := getServerIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), serverID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	dbRepo := repositories.NewPostgresDatabaseV2Repository(p.db.GetDB())
	databases, err := dbRepo.ListByServer(ctx, serverID)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Get grants for each database
	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())

	type DatabaseResponse struct {
		ID        int      `json:"id"`
		Name      string   `json:"name"`
		Users     []string `json:"users"` // Usernames with access
		CreatedAt string   `json:"created_at"`
	}

	response := make([]DatabaseResponse, 0)
	for _, db := range databases {
		// Get grants
		grants, _ := grantRepo.ListByDatabase(ctx, db.ID)
		users := make([]string, 0)
		for _, grant := range grants {
			user, _ := userRepo.GetByID(ctx, grant.UserID)
			if user != nil {
				users = append(users, user.Username)
			}
		}

		response = append(response, DatabaseResponse{
			ID:        db.ID,
			Name:      db.Name,
			Users:     users,
			CreatedAt: db.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateDatabase creates a new database
func (p *Panel) handleCreateDatabaseV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subscriptionID, err := p.callerSubscriptionID(r)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}
	// Extract ID from path
	serverID, err := getServerIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), serverID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	// Quota: one more database must fit in the subscription.
	// Kota: aboneliğe bir veritabanı daha sığmalı.
	if err := p.checkSubscriptionQuota(r.Context(), subscriptionID, quotaDBs); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}

	var req struct {
		DatabaseName string `json:"database_name"`
		DomainID     *int   `json:"domain_id,omitempty"`    // Optional: Related site/domain
		UserID       int    `json:"user_id,omitempty"`      // Existing user
		NewUsername  string `json:"new_username,omitempty"` // Or create new user
		NewPassword  string `json:"new_password,omitempty"`
		Privileges   string `json:"privileges,omitempty"` // Default: ALL
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate
	if req.DatabaseName == "" {
		http.Error(w, "database_name is required", http.StatusBadRequest)
		return
	}

	// Get server info
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	server, err := serverRepo.GetByID(ctx, serverID)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	dbType := dbDriverTypeFor(server)

	// Create database driver
	driver, err := services.NewDatabaseDriver(services.DriverConfig{
		Host:         server.Host,
		Port:         server.Port,
		RootPassword: server.RootPasswordEncrypted, // TODO: Decrypt
		Type:         dbType,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Add subscription prefix
	dbName := fmt.Sprintf("%d_%s", subscriptionID, req.DatabaseName)

	// Create database on server
	if err := driver.CreateDatabase(dbName); err != nil {
		writeServerError(w, err)
		return
	}

	// Store in PostgreSQL
	dbRepo := repositories.NewPostgresDatabaseV2Repository(p.db.GetDB())
	database := &core.DatabaseV2{
		ServerID:       serverID,
		SubscriptionID: subscriptionID,
		DomainID:       req.DomainID, // Optional: Related site
		Name:           dbName,
	}

	if err := dbRepo.Create(ctx, database); err != nil {
		writeServerError(w, err)
		return
	}

	// Handle user (existing or new)
	var userID int
	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())

	if req.UserID != 0 {
		// Use existing user
		userID = req.UserID
	} else if req.NewUsername != "" {
		// Create new user
		username := fmt.Sprintf("%d_%s", subscriptionID, req.NewUsername)
		password := req.NewPassword
		if password == "" {
			password, _ = services.GeneratePassword(16)
		}

		// Create user on server
		if err := driver.CreateUser(username, password); err != nil {
			writeServerError(w, err)
			return
		}

		// Store user
		user := &core.DatabaseUser{
			ServerID:       serverID,
			SubscriptionID: subscriptionID,
			Username:       username,
			Password:       password,
		}

		if err := userRepo.Create(ctx, user); err != nil {
			writeServerError(w, err)
			return
		}

		userID = user.ID
	} else {
		http.Error(w, "either user_id or new_username is required", http.StatusBadRequest)
		return
	}

	// Grant privileges
	privileges := req.Privileges
	if privileges == "" {
		privileges = "ALL"
	}

	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	grant := &core.DatabaseGrant{
		DatabaseID: database.ID,
		UserID:     userID,
		Privileges: privileges,
	}

	if err := grantRepo.Grant(ctx, grant); err != nil {
		writeServerError(w, err)
		return
	}

	// Get user for response
	user, _ := userRepo.GetByID(ctx, userID)

	// Grant on actual database server
	if user != nil {
		if err := driver.GrantPrivileges(dbName, user.Username, privileges); err != nil {
			writeServerError(w, err)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         database.ID,
		"name":       database.Name,
		"user":       user.Username,
		"password":   user.Password,
		"created_at": database.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// handleDeleteDatabase deletes a database
func (p *Panel) handleDeleteDatabaseV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Extract ID from path
	databaseID, err := getIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid database ID", http.StatusBadRequest)
		return
	}

	// Get database info
	dbRepo := repositories.NewPostgresDatabaseV2Repository(p.db.GetDB())
	database, err := dbRepo.GetByID(ctx, databaseID)
	if err != nil {
		http.Error(w, "database not found", http.StatusNotFound)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), database.ServerID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	// Get server info
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	server, err := serverRepo.GetByID(ctx, database.ServerID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	dbType := dbDriverTypeFor(server)

	// Create driver
	driver, err := services.NewDatabaseDriver(services.DriverConfig{
		Host:         server.Host,
		Port:         server.Port,
		RootPassword: server.RootPasswordEncrypted,
		Type:         dbType,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Delete from server
	if err := driver.DeleteDatabase(database.Name); err != nil {
		writeServerError(w, err)
		return
	}

	// Delete from PostgreSQL (cascade deletes grants)
	if err := dbRepo.Delete(ctx, databaseID); err != nil {
		writeServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// User Management Endpoints (per server)

// handleListDatabaseUsers lists all users for a server
func (p *Panel) handleListDatabaseUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Extract ID from path
	serverID, err := getServerIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), serverID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())
	users, err := userRepo.ListByServer(ctx, serverID)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Get databases for each user
	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	dbRepo := repositories.NewPostgresDatabaseV2Repository(p.db.GetDB())

	type UserResponse struct {
		ID        int      `json:"id"`
		Username  string   `json:"username"`
		Databases []string `json:"databases"` // Database names
		CreatedAt string   `json:"created_at"`
	}

	response := make([]UserResponse, 0)
	for _, user := range users {
		// Get grants
		grants, _ := grantRepo.ListByUser(ctx, user.ID)
		databases := make([]string, 0)
		for _, grant := range grants {
			db, _ := dbRepo.GetByID(ctx, grant.DatabaseID)
			if db != nil {
				databases = append(databases, db.Name)
			}
		}

		response = append(response, UserResponse{
			ID:        user.ID,
			Username:  user.Username,
			Databases: databases,
			CreatedAt: user.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateDatabaseUser creates a new database user
func (p *Panel) handleCreateDatabaseV2User(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subscriptionID, err := p.callerSubscriptionID(r)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}
	// Extract ID from path
	serverID, err := getServerIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid server ID", http.StatusBadRequest)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), serverID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}

	// Get server info
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	server, err := serverRepo.GetByID(ctx, serverID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	dbType := dbDriverTypeFor(server)

	// Create driver
	driver, err := services.NewDatabaseDriver(services.DriverConfig{
		Host:         server.Host,
		Port:         server.Port,
		RootPassword: server.RootPasswordEncrypted,
		Type:         dbType,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Add subscription prefix
	username := fmt.Sprintf("%d_%s", subscriptionID, req.Username)
	password := req.Password
	if password == "" {
		password, _ = services.GeneratePassword(16)
	}

	// Create user on server
	if err := driver.CreateUser(username, password); err != nil {
		writeServerError(w, err)
		return
	}

	// Store user
	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())
	user := &core.DatabaseUser{
		ServerID:       serverID,
		SubscriptionID: subscriptionID,
		Username:       username,
		Password:       password,
	}

	if err := userRepo.Create(ctx, user); err != nil {
		writeServerError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         user.ID,
		"username":   user.Username,
		"password":   user.Password,
		"created_at": user.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// handleDeleteDatabaseUser deletes a database user
func (p *Panel) handleDeleteDatabaseV2User(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Extract ID from path
	userID, err := getIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	// Get user info
	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())
	user, err := userRepo.GetByID(ctx, userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err := p.canAccessDBServer(ctx, currentCaller(r), user.ServerID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return
	}

	// Check if user is used in any database
	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	grants, _ := grantRepo.ListByUser(ctx, userID)
	if len(grants) > 0 {
		http.Error(w, "user is still used in databases, revoke access first", http.StatusBadRequest)
		return
	}

	// Get server info
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	server, err := serverRepo.GetByID(ctx, user.ServerID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	dbType := dbDriverTypeFor(server)

	// Create driver
	driver, err := services.NewDatabaseDriver(services.DriverConfig{
		Host:         server.Host,
		Port:         server.Port,
		RootPassword: server.RootPasswordEncrypted,
		Type:         dbType,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Delete from server
	if err := driver.DeleteUser(user.Username); err != nil {
		writeServerError(w, err)
		return
	}

	// Delete from PostgreSQL
	if err := userRepo.Delete(ctx, userID); err != nil {
		writeServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// Grant Management Endpoints

// handleListDatabaseGrants lists all grants for a database
func (p *Panel) handleListDatabaseGrants(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	// Extract ID from path
	databaseID, err := getDatabaseIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid database ID", http.StatusBadRequest)
		return
	}

	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	grants, err := grantRepo.ListByDatabase(ctx, databaseID)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Get user info for each grant
	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())

	type GrantResponse struct {
		ID         int    `json:"id"`
		UserID     int    `json:"user_id"`
		Username   string `json:"username"`
		Privileges string `json:"privileges"`
		CreatedAt  string `json:"created_at"`
	}

	response := make([]GrantResponse, 0)
	for _, grant := range grants {
		user, _ := userRepo.GetByID(ctx, grant.UserID)
		username := ""
		if user != nil {
			username = user.Username
		}

		response = append(response, GrantResponse{
			ID:         grant.ID,
			UserID:     grant.UserID,
			Username:   username,
			Privileges: grant.Privileges,
			CreatedAt:  grant.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGrantDatabaseAccess grants a user access to a database
func (p *Panel) handleGrantDatabaseAccess(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	// Extract ID from path
	databaseID, err := getDatabaseIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid database ID", http.StatusBadRequest)
		return
	}

	var req struct {
		UserID     int    `json:"user_id"`
		Privileges string `json:"privileges,omitempty"` // Default: ALL
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.UserID == 0 {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	privileges := req.Privileges
	if privileges == "" {
		privileges = "ALL"
	}

	// Get database and user info
	dbRepo := repositories.NewPostgresDatabaseV2Repository(p.db.GetDB())
	database, err := dbRepo.GetByID(ctx, databaseID)
	if err != nil {
		http.Error(w, "database not found", http.StatusNotFound)
		return
	}

	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())
	user, err := userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Verify user and database are on same server
	if user.ServerID != database.ServerID {
		http.Error(w, "user and database must be on the same server", http.StatusBadRequest)
		return
	}

	// Get server info
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	server, err := serverRepo.GetByID(ctx, database.ServerID)
	if err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}

	dbType := dbDriverTypeFor(server)

	// Create driver
	driver, err := services.NewDatabaseDriver(services.DriverConfig{
		Host:         server.Host,
		Port:         server.Port,
		RootPassword: server.RootPasswordEncrypted,
		Type:         dbType,
	})
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Grant on server
	if err := driver.GrantPrivileges(database.Name, user.Username, privileges); err != nil {
		writeServerError(w, err)
		return
	}

	// Store grant
	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	grant := &core.DatabaseGrant{
		DatabaseID: databaseID,
		UserID:     req.UserID,
		Privileges: privileges,
	}

	if err := grantRepo.Grant(ctx, grant); err != nil {
		writeServerError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         grant.ID,
		"user_id":    grant.UserID,
		"username":   user.Username,
		"privileges": grant.Privileges,
		"created_at": grant.CreatedAt.Format("2006-01-02T15:04:05Z"),
	})
}

// handleRevokeDatabaseAccess revokes a user's access to a database
func (p *Panel) handleRevokeDatabaseAccess(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	// Extract ID from path
	grantID, err := getIDFromPath(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid grant ID", http.StatusBadRequest)
		return
	}

	// Get grant info
	grantRepo := repositories.NewPostgresDatabaseGrantRepository(p.db.GetDB())
	grant, err := grantRepo.GetByID(ctx, grantID)
	if err != nil {
		http.Error(w, "grant not found", http.StatusNotFound)
		return
	}

	// Get database and user info
	dbRepo := repositories.NewPostgresDatabaseV2Repository(p.db.GetDB())
	database, _ := dbRepo.GetByID(ctx, grant.DatabaseID)

	userRepo := repositories.NewPostgresDatabaseUserRepository(p.db.GetDB())
	user, _ := userRepo.GetByID(ctx, grant.UserID)

	if database != nil && user != nil {
		// Get server info
		serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
		server, _ := serverRepo.GetByID(ctx, database.ServerID)

		if server != nil {
			// Determine database type
			var dbType string
			if server.TypeID == 23 {
				dbType = "postgresql"
			} else if server.TypeID == 24 {
				dbType = "mariadb"
			}

			// Create driver
			driver, _ := services.NewDatabaseDriver(services.DriverConfig{
				Host:         server.Host,
				Port:         server.Port,
				RootPassword: server.RootPasswordEncrypted,
				Type:         dbType,
			})

			// Revoke on server
			if driver != nil {
				driver.RevokePrivileges(database.Name, user.Username)
			}
		}
	}

	// Delete grant
	if err := grantRepo.Delete(ctx, grantID); err != nil {
		writeServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

// dbDriverTypeFor maps the catalog's canonical engine name to the driver
// type. The removed TypeID==23/24 constants came from a pre-migration
// schema and matched NOTHING in the shipped seed (1=postgresql, 2=mariadb):
// both branches were dead and the driver received an empty type.
// dbDriverTypeFor, kataloğun kanonik motor adını sürücü tipine eşler.
// Kaldırılan TypeID==23/24 sabitleri migration-öncesi bir şemadan kalmaydı
// ve dağıtılan tohumla (1=postgresql, 2=mariadb) HİÇ eşleşmiyordu: iki dal
// da ölüydü, sürücüye boş tip gidiyordu.
func dbDriverTypeFor(server *core.DatabaseServer) string {
	switch strings.ToLower(server.TypeName) {
	case "postgresql", "postgres":
		return "postgresql"
	case "mariadb", "mysql":
		return "mariadb"
	}
	return strings.ToLower(server.TypeName)
}
