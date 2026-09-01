package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"golang.org/x/term"
)

const minimumAdminPasswordBytes = 8

type adminCredentials struct {
	username string
	email    string
	password string
}

// runCreateAdmin creates (or updates) an administrator from the terminal.
// This is the bootstrap path until the Phase 2 first-run wizard exists:
// whoever can run the binary on the server can create the first admin.
//
// runCreateAdmin, terminalden bir yönetici oluşturur (ya da günceller).
// Faz 2 ilk açılış sihirbazı gelene kadar önyükleme yolu budur: sunucuda
// binary'yi çalıştırabilen, ilk yöneticiyi oluşturabilir.
func runCreateAdmin(database *db.SQLiteDB) error {
	reader := bufio.NewReader(os.Stdin)
	credentials, err := readInteractiveAdminCredentials(reader, os.Stdout)
	if err != nil {
		return err
	}
	return createOrUpdateAdmin(database, credentials, os.Stdout)
}

func readInteractiveAdminCredentials(reader *bufio.Reader, out io.Writer) (adminCredentials, error) {
	username, err := promptAdminUsername(reader, out)
	if err != nil {
		return adminCredentials{}, err
	}

	email, err := promptAdminEmail(reader, out)
	if err != nil {
		return adminCredentials{}, err
	}

	password, err := readPasswordTwice(reader, out)
	if err != nil {
		return adminCredentials{}, err
	}
	credentials := adminCredentials{username: username, email: email, password: password}
	if err := validateAdminCredentials(credentials); err != nil {
		return adminCredentials{}, err
	}
	return credentials, nil
}

func runCreateAdminFromCredentialsFile(database *db.SQLiteDB, file *os.File, out io.Writer) error {
	credentials, err := readAdminCredentialsFile(file)
	if err != nil {
		return err
	}
	return createOrUpdateAdmin(database, credentials, out)
}

func validateAdminCredentialsFile(file *os.File) error {
	_, err := readAdminCredentialsFile(file)
	return err
}

func createOrUpdateAdmin(database *db.SQLiteDB, credentials adminCredentials, out io.Writer) error {
	if err := validateAdminCredentials(credentials); err != nil {
		return err
	}

	hash, err := auth.HashPassword(credentials.password)
	if err != nil {
		return errors.New("failed to hash admin password")
	}

	ctx := context.Background()
	repo := repositories.NewPostgresUserRepository(database.GetDB())

	if existing, err := repo.GetByUsername(ctx, credentials.username); err == nil {
		existing.PasswordHash = hash
		existing.Email = credentials.email
		existing.Role = "admin"
		if err := repo.UpdateAndRevokeSessions(ctx, existing); err != nil {
			return fmt.Errorf("failed to update admin: %w", err)
		}
		revokePendingLogins(existing.ID)
		fmt.Fprintln(out, "Updated existing administrator / Mevcut yönetici güncellendi.")
		return nil
	}

	user := &core.User{
		Username:     credentials.username,
		PasswordHash: hash,
		Email:        credentials.email,
		Role:         "admin",
	}
	if err := repo.Create(ctx, user); err != nil {
		return fmt.Errorf("failed to create admin: %w", err)
	}
	fmt.Fprintln(out, "Created administrator / Yönetici oluşturuldu.")
	return nil
}

func parseAdminCredentialsJSON(content []byte) (adminCredentials, error) {
	if !utf8.Valid(content) {
		return adminCredentials{}, errors.New("admin credentials file is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return adminCredentials{}, errors.New("admin credentials file is invalid")
	}

	values := make(map[string]string, 3)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return adminCredentials{}, errors.New("admin credentials file is invalid")
		}
		key, ok := token.(string)
		if !ok || (key != "username" && key != "email" && key != "password") {
			return adminCredentials{}, errors.New("admin credentials file is invalid")
		}
		if _, duplicate := values[key]; duplicate {
			return adminCredentials{}, errors.New("admin credentials file is invalid")
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return adminCredentials{}, errors.New("admin credentials file is invalid")
		}
		values[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return adminCredentials{}, errors.New("admin credentials file is invalid")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return adminCredentials{}, errors.New("admin credentials file is invalid")
	}
	if len(values) != 3 {
		return adminCredentials{}, errors.New("admin credentials file is invalid")
	}

	credentials := adminCredentials{
		username: values["username"],
		email:    values["email"],
		password: values["password"],
	}
	if err := validateAdminCredentials(credentials); err != nil {
		return adminCredentials{}, err
	}
	return credentials, nil
}

func validateAdminCredentials(credentials adminCredentials) error {
	if strings.TrimSpace(credentials.username) != credentials.username ||
		strings.TrimSpace(credentials.email) != credentials.email ||
		credentials.username == "" || credentials.email == "" ||
		len(credentials.password) < minimumAdminPasswordBytes {
		return errors.New("admin credentials are invalid")
	}
	if err := auth.ValidateUsername(credentials.username); err != nil {
		return errors.New("admin credentials are invalid")
	}
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

// readPasswordTwice reads a password without echo on a terminal and confirms
// it. Non-interactive callers must use the explicit inherited credentials-file
// mode so a password cannot enter through an ambiguous stdin contract.
//
// readPasswordTwice, parolayı terminalde ekranda göstermeden okur ve doğrular.
// Etkileşimsiz çağıranlar, parolanın belirsiz bir stdin sözleşmesine girmemesi
// için açık devralınmış kimlik-dosyası modunu kullanmalıdır.
func readPasswordTwice(reader *bufio.Reader, out io.Writer) (string, error) {
	if err := requireInteractiveAdminTerminal(term.IsTerminal(int(syscall.Stdin))); err != nil {
		return "", err
	}

	visible, err := promptPasswordVisibility(reader, out)
	if err != nil {
		return "", err
	}

	var read passwordValueReader
	if visible {
		fmt.Fprintln(out, "Warning: password characters will be visible. / Uyarı: parola karakterleri görünecek.")
		read = func(prompt string) (string, error) {
			return promptLine(reader, out, prompt)
		}
	} else {
		read = func(prompt string) (string, error) {
			fmt.Fprint(out, prompt)
			value, readErr := term.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(out)
			if readErr != nil {
				return "", readErr
			}
			return string(value), nil
		}
	}

	return readAndConfirmPassword(read, out)
}

func requireInteractiveAdminTerminal(isTerminal bool) error {
	if !isTerminal {
		return errors.New("interactive create-admin requires a terminal")
	}
	return nil
}

type passwordValueReader func(prompt string) (string, error)

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
		if len(first) < minimumAdminPasswordBytes {
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
