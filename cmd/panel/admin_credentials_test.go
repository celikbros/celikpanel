package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/auth"
	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestParseAdminCredentialsJSONStrict(t *testing.T) {
	const password = "never-print-this-password"
	credentials, err := parseAdminCredentialsJSON([]byte(
		`{"username":"first-admin","email":"admin@example.test","password":"` + password + `"}`,
	))
	if err != nil {
		t.Fatalf("parse valid credentials: %v", err)
	}
	if credentials.username != "first-admin" || credentials.email != "admin@example.test" || credentials.password != password {
		t.Fatal("valid credentials were not preserved")
	}

	invalid := map[string][]byte{
		"unknown field":    []byte(`{"username":"first-admin","email":"admin@example.test","password":"` + password + `","extra":"value"}`),
		"duplicate field":  []byte(`{"username":"first-admin","email":"admin@example.test","password":"` + password + `","password":"duplicate-secret"}`),
		"trailing object":  []byte(`{"username":"first-admin","email":"admin@example.test","password":"` + password + `"}{}`),
		"missing field":    []byte(`{"username":"first-admin","password":"` + password + `"}`),
		"non-string field": []byte(`{"username":"first-admin","email":7,"password":"` + password + `"}`),
		"invalid username": []byte(`{"username":"x","email":"admin@example.test","password":"` + password + `"}`),
		"padded username":  []byte(`{"username":" first-admin","email":"admin@example.test","password":"` + password + `"}`),
		"empty email":      []byte(`{"username":"first-admin","email":"","password":"` + password + `"}`),
		"short password":   []byte(`{"username":"first-admin","email":"admin@example.test","password":"short"}`),
		"malformed JSON":   []byte(`{"username":"first-admin"`),
		"invalid UTF-8": append(
			[]byte(`{"username":"first-admin","email":"admin@example.test","password":"`+password),
			[]byte{0xff, '"', '}'}...,
		),
	}
	for name, input := range invalid {
		t.Run(name, func(t *testing.T) {
			_, err := parseAdminCredentialsJSON(input)
			if err == nil {
				t.Fatal("invalid credentials were accepted")
			}
			for _, secret := range []string{password, "duplicate-secret"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("error exposed a password")
				}
			}
		})
	}
}

func TestCreateOrUpdateAdminUsesSharedValidatedPathWithoutPasswordOutput(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open panel database: %v", err)
	}
	t.Cleanup(database.Close)

	// A valid password may be identical to the username. Success output must
	// therefore contain no credential-derived field, not merely omit the
	// dedicated password field.
	const firstPassword = "first-admin"
	var output bytes.Buffer
	if err := createOrUpdateAdmin(database, adminCredentials{
		username: "first-admin",
		email:    "first@example.test",
		password: firstPassword,
	}, &output); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if strings.Contains(output.String(), firstPassword) {
		t.Fatal("create output exposed the password")
	}

	const secondPassword = "replacement-secret-password"
	output.Reset()
	if err := createOrUpdateAdmin(database, adminCredentials{
		username: "first-admin",
		email:    "updated@example.test",
		password: secondPassword,
	}, &output); err != nil {
		t.Fatalf("update admin: %v", err)
	}
	if strings.Contains(output.String(), secondPassword) {
		t.Fatal("update output exposed the password")
	}

	var count int
	var email, role, passwordHash string
	if err := database.GetDB().QueryRow(`
		SELECT COUNT(*), email, role, password_hash
		FROM users
		WHERE username = 'first-admin'
	`).Scan(&count, &email, &role, &passwordHash); err != nil {
		t.Fatalf("read stored admin: %v", err)
	}
	if count != 1 || email != "updated@example.test" || role != "admin" {
		t.Fatalf("stored admin count/email/role = %d/%q/%q", count, email, role)
	}
	if passwordHash == secondPassword {
		t.Fatal("stored password was not hashed")
	}
	verified, err := auth.VerifyPassword(secondPassword, passwordHash)
	if err != nil || !verified {
		t.Fatalf("stored password hash did not verify: verified=%v err=%v", verified, err)
	}
}

func TestValidateAdminCredentialsFileFlags(t *testing.T) {
	stdinFlag := inheritedAdminCredentialsFileFlag{set: true, value: "-"}
	for name, test := range map[string]struct {
		create     bool
		input      inheritedAdminCredentialsFileFlag
		validation inheritedAdminCredentialsFileFlag
	}{
		"interactive create": {create: true},
		"file create":        {create: true, input: stdinFlag},
		"validation only":    {validation: stdinFlag},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAdminCredentialsFileFlags(test.create, test.input, test.validation); err != nil {
				t.Fatalf("valid flags rejected: %v", err)
			}
		})
	}

	secretPath := "/tmp/never-print-secret-path"
	invalid := []struct {
		create     bool
		input      inheritedAdminCredentialsFileFlag
		validation inheritedAdminCredentialsFileFlag
	}{
		{input: stdinFlag},
		{create: true, input: inheritedAdminCredentialsFileFlag{set: true, value: ""}},
		{create: true, input: inheritedAdminCredentialsFileFlag{set: true, value: secretPath}},
		{validation: inheritedAdminCredentialsFileFlag{set: true, value: secretPath}},
	}
	for index, test := range invalid {
		err := validateAdminCredentialsFileFlags(test.create, test.input, test.validation)
		if err == nil {
			t.Fatalf("case %d: invalid flags accepted", index)
		}
		if strings.Contains(err.Error(), secretPath) {
			t.Fatalf("case %d: error exposed the rejected value", index)
		}
	}

	var duplicate inheritedAdminCredentialsFileFlag
	if err := duplicate.Set("-"); err != nil {
		t.Fatalf("first flag set failed: %v", err)
	}
	if err := duplicate.Set("-"); err == nil {
		t.Fatal("duplicate flag was accepted")
	}
}

func TestValidateAdminCredentialsFileArgumentSpellings(t *testing.T) {
	for name, arguments := range map[string][]string{
		"absent":           {"--create-admin"},
		"create input":     {"--create-admin", adminCredentialsFileArgument},
		"validation input": {validateAdminCredentialsFileArgument},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAdminCredentialsFileArgumentSpellings(arguments); err != nil {
				t.Fatalf("exact argument rejected: %v", err)
			}
		})
	}

	const secretPath = "/tmp/never-print-argument-value"
	for _, arguments := range [][]string{
		{"--admin-credentials-file", secretPath},
		{"-admin-credentials-file=-"},
		{"--admin-credentials-file=" + secretPath},
		{"--validate-admin-credentials-file", secretPath},
		{"-validate-admin-credentials-file=-"},
		{"--validate-admin-credentials-file=" + secretPath},
	} {
		err := validateAdminCredentialsFileArgumentSpellings(arguments)
		if err == nil {
			t.Fatal("non-exact argument was accepted")
		}
		if strings.Contains(err.Error(), secretPath) {
			t.Fatal("argument validation error exposed the rejected value")
		}
	}
}
