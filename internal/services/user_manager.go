package services

import (
	"fmt"
	"os/exec"
	"strings"
)

type UserManager struct{}

func NewUserManager() *UserManager {
	return &UserManager{}
}

// CreateUser creates a Linux system user for a site
func (um *UserManager) CreateUser(username string, homeDir string, password string) error {
	// Create user with home directory
	cmd := exec.Command("useradd", "-m", "-d", homeDir, "-s", "/bin/bash", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create user: %s", string(output))
	}

	// Set password
	cmd = exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, password))
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set password: %s", string(output))
	}

	return nil
}

// DeleteUser removes a Linux system user
func (um *UserManager) DeleteUser(username string) error {
	cmd := exec.Command("userdel", "-r", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete user: %s", string(output))
	}
	return nil
}

// SetOwnership sets directory ownership to user
func (um *UserManager) SetOwnership(path string, username string) error {
	cmd := exec.Command("chown", "-R", fmt.Sprintf("%s:%s", username, username), path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set ownership: %s", string(output))
	}
	return nil
}

// UserExists checks if a user exists
func (um *UserManager) UserExists(username string) bool {
	cmd := exec.Command("id", username)
	err := cmd.Run()
	return err == nil
}
