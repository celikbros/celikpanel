package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"golang.org/x/term"
)

// runCreateAdmin creates (or updates) an administrator from the terminal.
// This is the bootstrap path until the Phase 2 first-run wizard exists:
// whoever can run the binary on the server can create the first admin.
//
// runCreateAdmin, terminalden bir yönetici oluşturur (ya da günceller).
// Faz 2 ilk açılış sihirbazı gelene kadar önyükleme yolu budur: sunucuda
// binary'yi çalıştırabilen, ilk yöneticiyi oluşturabilir.
func runCreateAdmin(database *db.SQLiteDB) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Admin username / Yönetici kullanıcı adı: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}

	fmt.Print("Admin email / Yönetici e-posta: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email must not be empty")
	}

	password, err := readPasswordTwice(reader)
	if err != nil {
		return err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	ctx := context.Background()
	repo := repositories.NewPostgresUserRepository(database.GetDB())

	if existing, err := repo.GetByUsername(ctx, username); err == nil {
		existing.PasswordHash = hash
		existing.Email = email
		existing.Role = "admin"
		if err := repo.Update(ctx, existing); err != nil {
			return fmt.Errorf("failed to update admin: %w", err)
		}
		fmt.Printf("Updated existing admin %q / Mevcut yönetici %q güncellendi.\n", username, username)
		return nil
	}

	user := &core.User{
		Username:     username,
		PasswordHash: hash,
		Email:        email,
		Role:         "admin",
	}
	if err := repo.Create(ctx, user); err != nil {
		return fmt.Errorf("failed to create admin: %w", err)
	}
	fmt.Printf("Created admin %q / Yönetici %q oluşturuldu.\n", username, username)
	return nil
}

// readPasswordTwice reads a password without echo on a terminal and
// confirms it. When stdin is not a terminal (scripted setup, tests), it
// reads a single line so the command stays automatable.
//
// readPasswordTwice, bir terminalde parolayı ekranda göstermeden okur ve
// doğrular. stdin bir terminal değilse (scriptli kurulum, testler), komut
// otomatik kalabilsin diye tek satır okur.
func readPasswordTwice(reader *bufio.Reader) (string, error) {
	if !term.IsTerminal(int(syscall.Stdin)) {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if len(password) < 8 {
			return "", fmt.Errorf("password must be at least 8 characters")
		}
		return password, nil
	}

	fmt.Print("Admin password / Yönetici parolası: ")
	first, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}

	fmt.Print("Confirm password / Parolayı doğrula: ")
	second, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}

	if string(first) != string(second) {
		return "", fmt.Errorf("passwords do not match")
	}
	if len(first) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	return string(first), nil
}
