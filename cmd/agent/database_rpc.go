package main

import (
	"fmt"
	"os/exec"
)

// Database Management RPC Methods

// CreateDatabaseRequest represents a request to create a database
type CreateDatabaseRequest struct {
	Type     string `json:"type"`     // mysql or postgresql
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
	// Create database
	cmd := exec.Command("mysql", "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", req.Name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create database: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Create user and grant privileges
	cmd = exec.Command("mysql", "-e", fmt.Sprintf(
		"CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
		req.User, req.Password, req.Name, req.User,
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
	// Drop database
	cmd := exec.Command("mysql", "-e", fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", req.Name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to drop database: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Drop user
	cmd = exec.Command("mysql", "-e", fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", req.User))
	output, err = cmd.CombinedOutput()
	if err != nil {
		// Don't fail if user doesn't exist
		resp.Success = true
		return nil
	}

	resp.Success = true
	return nil
}

// createPostgreSQLDatabase creates a PostgreSQL database and user
func (a *Agent) createPostgreSQLDatabase(req CreateDatabaseRequest, resp *CreateDatabaseResponse) error {
	// Create user
	cmd := exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s';", req.User, req.Password))
	output, err := cmd.CombinedOutput()
	if err != nil && !contains(string(output), "already exists") {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to create user: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Create database
	cmd = exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("CREATE DATABASE %s OWNER %s;", req.Name, req.User))
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
	// Drop database
	cmd := exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("DROP DATABASE IF EXISTS %s;", req.Name))
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to drop database: %v\nOutput: %s", err, string(output))
		return nil
	}

	// Drop user
	cmd = exec.Command("sudo", "-u", "postgres", "psql", "-c",
		fmt.Sprintf("DROP USER IF EXISTS %s;", req.User))
	output, err = cmd.CombinedOutput()
	if err != nil {
		// Don't fail if user doesn't exist
		resp.Success = true
		return nil
	}

	resp.Success = true
	return nil
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
