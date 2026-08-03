package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

	username, err := promptAdminUsername(reader, os.Stdout)
	if err != nil {
		return err
	}

	email, err := promptAdminEmail(reader, os.Stdout)
	if err != nil {
		return err
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
		if err := repo.UpdateAndRevokeSessions(ctx, existing); err != nil {
			return fmt.Errorf("failed to update admin: %w", err)
		}
		revokePendingLogins(existing.ID)
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

func promptAdminUsername(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		username, err := promptLine(reader, out, "Admin username / Yönetici kullanıcı adı: ")
		if err != nil {
			return "", fmt.Errorf("failed to read admin username: %w", err)
		}
		username = strings.TrimSpace(username)
		if err := auth.ValidateUsername(username); err != nil {
			fmt.Fprintf(out, "Invalid username: %v. Please try again. / Geçersiz kullanıcı adı. Lütfen yeniden deneyin.\n", err)
			continue
		}
		return username, nil
	}
}

func promptAdminEmail(reader *bufio.Reader, out io.Writer) (string, error) {
	for {
		email, err := promptLine(reader, out, "Admin email / Yönetici e-posta: ")
		if err != nil {
			return "", fmt.Errorf("failed to read admin email: %w", err)
		}
		email = strings.TrimSpace(email)
		if email == "" {
			fmt.Fprintln(out, "Email must not be empty. Please try again. / E-posta boş olamaz. Lütfen yeniden deneyin.")
			continue
		}
		return email, nil
	}
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
		return readAutomatedPassword(reader)
	}

	visible, err := promptPasswordVisibility(reader, os.Stdout)
	if err != nil {
		return "", err
	}

	var read passwordValueReader
	if visible {
		fmt.Fprintln(os.Stdout, "Warning: password characters will be visible. / Uyarı: parola karakterleri görünecek.")
		read = func(prompt string) (string, error) {
			return promptLine(reader, os.Stdout, prompt)
		}
	} else {
		read = func(prompt string) (string, error) {
			fmt.Fprint(os.Stdout, prompt)
			value, readErr := term.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(os.Stdout)
			if readErr != nil {
				return "", readErr
			}
			return string(value), nil
		}
	}

	return readAndConfirmPassword(read, os.Stdout)
}

type passwordValueReader func(prompt string) (string, error)

func readAutomatedPassword(reader *bufio.Reader) (string, error) {
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

func promptPasswordVisibility(reader *bufio.Reader, out io.Writer) (bool, error) {
	for {
		answer, err := promptLine(
			reader,
			out,
			"Show password while typing? / Parola yazarken görünsün mü? [y/e = yes/evet, Enter = no/hayır]: ",
		)
		if err != nil {
			return false, fmt.Errorf("failed to read password visibility choice: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "n", "no", "h", "hayir", "hayır":
			return false, nil
		case "y", "yes", "e", "evet":
			return true, nil
		default:
			fmt.Fprintln(out, "Please enter y/e for visible or n/h for hidden. / Görünür için y/e, gizli için n/h girin.")
		}
	}
}

func promptLine(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readAndConfirmPassword(read passwordValueReader, out io.Writer) (string, error) {
	for {
		first, err := read("Admin password / Yönetici parolası: ")
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		if len(first) < 8 {
			fmt.Fprintln(out, "Password must be at least 8 characters. Please try again. / Parola en az 8 karakter olmalı. Lütfen yeniden deneyin.")
			continue
		}

		second, err := read("Confirm password / Parolayı doğrula: ")
		if err != nil {
			return "", fmt.Errorf("failed to read password confirmation: %w", err)
		}
		if first != second {
			fmt.Fprintln(out, "Passwords do not match. Please try again. / Parolalar eşleşmiyor. Lütfen yeniden deneyin.")
			continue
		}

		return first, nil
	}
}
