package services

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

type UserManager struct{}

func NewUserManager() *UserManager {
	return &UserManager{}
}

// CreateUser creates a Linux system user for a site
func (um *UserManager) CreateUser(username string, homeDir string, password string) error {
	// Create user with home directory. An EXISTING user is not an error: a
	// site creation that fails after this step (a bad vhost, a refused
	// certificate) leaves the user behind, and the operator's retry would then
	// die on "user already exists" — the first failure permanently poisoning
	// every later attempt at the same domain. Found on Boston (25 Jul): adding
	// biovision.health failed at nginx validation and left biovision_health
	// (uid 5001) on the machine.
	// Ev dizini olan kullanıcıyı oluştur. VAR OLAN kullanıcı hata değildir: bu
	// adımdan sonra düşen bir site oluşturma (bozuk vhost, reddedilen
	// sertifika) kullanıcıyı geride bırakır ve operatörün yeniden denemesi
	// "kullanıcı zaten var" ile ölürdü — ilk arıza, aynı alan adı için sonraki
	// her denemeyi kalıcı olarak zehirler. Boston'da bulundu (25 Tem):
	// biovision.health eklenirken nginx doğrulamasında düştü ve makinede
	// biovision_health (uid 5001) kaldı.
	if _, err := user.Lookup(username); err == nil {
		if err := os.MkdirAll(homeDir, 0o750); err != nil {
			return fmt.Errorf("home directory: %w", err)
		}
	} else {
		cmd := exec.Command("useradd", "-m", "-d", homeDir, "-s", "/bin/bash", username)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to create user: %s", string(output))
		}
	}

	// Set password
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, password))
	output, err := cmd.CombinedOutput()
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
