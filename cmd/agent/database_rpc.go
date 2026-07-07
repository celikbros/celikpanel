package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alicelik/celikpanel/internal/services"
)

// Database Management RPC Methods

// CreateDatabaseRequest represents a request to create a database
type CreateDatabaseRequest struct {
	Type     string `json:"type"` // mysql or postgresql
	Name     string `json:"name"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// CreateDatabaseResponse represents the response from creating a database
type CreateDatabaseResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// DeleteDatabaseRequest represents a request to delete a database
type DeleteDatabaseRequest struct {
	Type string `json:"type"` // mysql or postgresql
	Name string `json:"name"`
	User string `json:"user"`
}

// DeleteDatabaseResponse represents the response from deleting a database
type DeleteDatabaseResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// CreateDatabase creates a new MySQL or PostgreSQL database
func (a *Agent) CreateDatabase(req CreateDatabaseRequest, resp *CreateDatabaseResponse) error {
	if req.Type == "mysql" {
		return a.createMySQLDatabase(req, resp)
	} else if req.Type == "postgresql" {
		return a.createPostgreSQLDatabase(req, resp)
	}

	resp.Success = false
	resp.Error = "invalid database type"
	return nil
}

// DeleteDatabase deletes a MySQL or PostgreSQL database
func (a *Agent) DeleteDatabase(req DeleteDatabaseRequest, resp *DeleteDatabaseResponse) error {
	if req.Type == "mysql" {
		return a.deleteMySQLDatabase(req, resp)
	} else if req.Type == "postgresql" {
		return a.deletePostgreSQLDatabase(req, resp)
	}

	resp.Success = false
	resp.Error = "invalid database type"
	return nil
}

// createMySQLDatabase creates a MySQL database and user
func (a *Agent) createMySQLDatabase(req CreateDatabaseRequest, resp *CreateDatabaseResponse) error {
	dbIdent, err := services.QuoteMySQLIdentifier(req.Name)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid database name: %v", err)
		return nil
	}
	if err := services.ValidateSQLIdentifier(req.User); err != nil {
		resp.Error = fmt.Sprintf("invalid username: %v", err)
		return nil
	}
	userLiteral, err := services.QuoteMySQLStringLiteral(req.User)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid username: %v", err)
		return nil
	}
	pwLiteral, err := services.QuoteMySQLStringLiteral(req.Password)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid password: %v", err)
		return nil
	}

	// Create database
	cmd := exec.Command("mysql", "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s;", dbIdent))
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create database: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Create user and grant privileges
	cmd = exec.Command("mysql", "-e", fmt.Sprintf(
		"CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s; GRANT ALL PRIVILEGES ON %s.* TO %s@'localhost'; FLUSH PRIVILEGES;",
		userLiteral, pwLiteral, dbIdent, userLiteral,
	))
	output, err = cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create user: %v\nOutput: %s", err, string(output))
		return nil
	}

	resp.Success = true
	return nil
}

// deleteMySQLDatabase deletes a MySQL database and user
func (a *Agent) deleteMySQLDatabase(req DeleteDatabaseRequest, resp *DeleteDatabaseResponse) error {
	dbIdent, err := services.QuoteMySQLIdentifier(req.Name)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid database name: %v", err)
		return nil
	}

	// Drop database
	cmd := exec.Command("mysql", "-e", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbIdent))
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to drop database: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Drop user
	if userLiteral, err := services.QuoteMySQLStringLiteral(req.User); err == nil {
		if err := services.ValidateSQLIdentifier(req.User); err == nil {
			cmd = exec.Command("mysql", "-e", fmt.Sprintf("DROP USER IF EXISTS %s@'localhost';", userLiteral))
			_, _ = cmd.CombinedOutput() // Don't fail if user doesn't exist
		}
	}

	resp.Success = true
	return nil
}

// createPostgreSQLDatabase creates a PostgreSQL database and user
func (a *Agent) createPostgreSQLDatabase(req CreateDatabaseRequest, resp *CreateDatabaseResponse) error {
	userIdent, err := services.QuotePGIdentifier(req.User)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid username: %v", err)
		return nil
	}
	dbIdent, err := services.QuotePGIdentifier(req.Name)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid database name: %v", err)
		return nil
	}
	pwLiteral, err := services.QuotePGStringLiteral(req.Password)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid password: %v", err)
		return nil
	}

	// Create user
	cmd := exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("CREATE USER %s WITH PASSWORD %s;", userIdent, pwLiteral))
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "already exists") {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create user: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Create database
	cmd = exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("CREATE DATABASE %s OWNER %s;", dbIdent, userIdent))
	output, err = cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create database: %v\nOutput: %s", err, string(output))
		return nil
	}

	resp.Success = true
	return nil
}

// deletePostgreSQLDatabase deletes a PostgreSQL database and user
func (a *Agent) deletePostgreSQLDatabase(req DeleteDatabaseRequest, resp *DeleteDatabaseResponse) error {
	dbIdent, err := services.QuotePGIdentifier(req.Name)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid database name: %v", err)
		return nil
	}

	// Drop database
	cmd := exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbIdent))
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to drop database: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Drop user
	if userIdent, err := services.QuotePGIdentifier(req.User); err == nil {
		cmd = exec.Command("sudo", "-u", "postgres", "psql", "-c",
			fmt.Sprintf("DROP USER IF EXISTS %s;", userIdent))
		_, _ = cmd.CombinedOutput() // Don't fail if user doesn't exist
	}

	resp.Success = true
	return nil
}
