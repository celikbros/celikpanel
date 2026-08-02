package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

const wordpressDatabaseOwnershipTable = "celikpanel_install_ownership"

var mysqlExecStatement = runMySQLStatement

func newMySQLStatementCommand(statement string) *exec.Cmd {
	command := exec.Command("mysql", "--batch", "--skip-column-names")
	command.Stdin = strings.NewReader(statement + "\n")
	return command
}

func runMySQLStatement(statement string) ([]byte, error) {
	return newMySQLStatementCommand(statement).CombinedOutput()
}

// Database Management RPC Methods

// CreateDatabaseRequest represents a request to create a database
type CreateDatabaseRequest = transport.CreateDatabaseRequest

// CreateDatabaseResponse represents the response from creating a database
type CreateDatabaseResponse = transport.CreateDatabaseResponse

// DeleteDatabaseRequest represents a request to delete a database
type DeleteDatabaseRequest = transport.DeleteDatabaseRequest

// DeleteDatabaseResponse represents the response from deleting a database
type DeleteDatabaseResponse = transport.DeleteDatabaseResponse

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

	hasOperation := req.OperationID != "" || req.CleanupToken != ""
	if hasOperation &&
		(!validWordPressOperationID(req.OperationID) || !validWordPressCleanupToken(req.CleanupToken)) {
		resp.Error = "invalid database operation ownership proof"
		return nil
	}

	databaseCreated := false
	userCreated := false
	if _, err := mysqlExecStatement(fmt.Sprintf("CREATE DATABASE %s;", dbIdent)); err != nil {
		if !hasOperation || !verifyMySQLOwnership(dbIdent, req.OperationID, req.CleanupToken) {
			resp.Error = "database already exists or could not be created exclusively"
			return nil
		}
		// A matching marker is written only after this operation exclusively
		// created both the database and user. A lost response can therefore
		// safely resume at the idempotent grant step.
		userCreated = true
	} else {
		databaseCreated = true
		if _, err := mysqlExecStatement(fmt.Sprintf(
			"CREATE USER %s@'localhost' IDENTIFIED BY %s;",
			userLiteral, pwLiteral,
		)); err != nil {
			resp.CleanupIncomplete = !cleanupCreatedMySQLResources(dbIdent, "", false, true)
			resp.Error = "database user already exists or could not be created exclusively"
			return nil
		}
		userCreated = true

		if hasOperation {
			operationLiteral, _ := services.QuoteMySQLStringLiteral(req.OperationID)
			hashLiteral, _ := services.QuoteMySQLStringLiteral(
				wordpressDatabaseOwnershipHash(req.OperationID, req.CleanupToken),
			)
			markerSQL := fmt.Sprintf(
				"CREATE TABLE %s.%s ("+
					"operation_id VARCHAR(32) NOT NULL PRIMARY KEY,"+
					"cleanup_token_hash CHAR(64) NOT NULL"+
					"); INSERT INTO %s.%s (operation_id, cleanup_token_hash) VALUES (%s, %s);",
				dbIdent, wordpressDatabaseOwnershipTable,
				dbIdent, wordpressDatabaseOwnershipTable,
				operationLiteral, hashLiteral,
			)
			if _, err := mysqlExecStatement(markerSQL); err != nil {
				resp.CleanupIncomplete = !cleanupCreatedMySQLResources(
					dbIdent, userLiteral, userCreated, databaseCreated,
				)
				resp.Error = "failed to persist database ownership proof"
				return nil
			}
		}
	}

	if _, err := mysqlExecStatement(fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON %s.* TO %s@'localhost'; FLUSH PRIVILEGES;",
		dbIdent, userLiteral,
	)); err != nil {
		// A resumed operation already had a durable marker. Preserve it for
		// retry instead of deleting resources after an ambiguous prior result.
		if databaseCreated {
			resp.CleanupIncomplete = !cleanupCreatedMySQLResources(
				dbIdent, userLiteral, userCreated, databaseCreated,
			)
		} else {
			resp.CleanupIncomplete = true
		}
		resp.Error = "failed to grant database privileges"
		return nil
	}

	resp.Success = true
	resp.OwnedByOperation = hasOperation
	return nil
}

func validWordPressCleanupToken(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func wordpressDatabaseOwnershipHash(operationID, cleanupToken string) string {
	digest := sha256.Sum256([]byte(operationID + "\x00" + cleanupToken))
	return hex.EncodeToString(digest[:])
}

func verifyMySQLOwnership(dbIdent, operationID, cleanupToken string) bool {
	operationLiteral, err := services.QuoteMySQLStringLiteral(operationID)
	if err != nil {
		return false
	}
	hashLiteral, err := services.QuoteMySQLStringLiteral(
		wordpressDatabaseOwnershipHash(operationID, cleanupToken),
	)
	if err != nil {
		return false
	}
	output, err := mysqlExecStatement(fmt.Sprintf(
		"SELECT COUNT(*) FROM %s.%s WHERE operation_id = %s AND cleanup_token_hash = %s;",
		dbIdent, wordpressDatabaseOwnershipTable, operationLiteral, hashLiteral,
	))
	return err == nil && strings.TrimSpace(string(output)) == "1"
}

func cleanupCreatedMySQLResources(
	dbIdent, userLiteral string,
	userCreated, databaseCreated bool,
) bool {
	complete := true
	if userCreated && userLiteral != "" {
		if _, err := mysqlExecStatement(fmt.Sprintf(
			"DROP USER IF EXISTS %s@'localhost';", userLiteral,
		)); err != nil {
			complete = false
		}
	}
	if databaseCreated {
		if _, err := mysqlExecStatement(fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s;", dbIdent,
		)); err != nil {
			complete = false
		}
	}
	return complete
}

// deleteMySQLDatabase deletes a MySQL database and user
func (a *Agent) deleteMySQLDatabase(req DeleteDatabaseRequest, resp *DeleteDatabaseResponse) error {
	dbIdent, err := services.QuoteMySQLIdentifier(req.Name)
	if err != nil {
		resp.Error = fmt.Sprintf("invalid database name: %v", err)
		return nil
	}

	if req.RequireOwnershipProof {
		if !validWordPressOperationID(req.OperationID) ||
			!validWordPressCleanupToken(req.CleanupToken) ||
			!verifyMySQLOwnership(dbIdent, req.OperationID, req.CleanupToken) {
			resp.Error = "database ownership proof did not match; destructive cleanup was refused"
			return nil
		}
	}

	// Normal database deletion remains tolerant for backward compatibility.
	// Transaction compensations can require proof that the user was dropped.
	if req.User != "" {
		userLiteral, quoteErr := services.QuoteMySQLStringLiteral(req.User)
		validateErr := services.ValidateSQLIdentifier(req.User)
		if quoteErr != nil || validateErr != nil {
			if req.RequireUserCleanup {
				resp.Success = false
				resp.Error = "invalid database user for required cleanup"
				return nil
			}
		} else {
			_, dropErr := mysqlExecStatement(fmt.Sprintf("DROP USER IF EXISTS %s@'localhost';", userLiteral))
			if dropErr != nil && req.RequireUserCleanup {
				resp.Success = false
				resp.Error = "failed to drop database user"
				return nil
			}
		}
	}

	if _, err := mysqlExecStatement(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbIdent)); err != nil {
		resp.Success = false
		resp.Error = "failed to drop database"
		return nil
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
