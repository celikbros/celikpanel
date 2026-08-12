package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func mailPasswordTestHash(salt string, checksum byte) string {
	return "{SHA512-CRYPT}$6$" + salt + "$" + strings.Repeat(string(checksum), 86)
}

func TestValidateMailboxPasswordRejectsLineProtocolCharacters(t *testing.T) {
	tests := []string{
		"short",
		strings.Repeat("x", transport.MaxMailboxPasswordBytes+1),
		"long-enough\rrest",
		"long-enough\nrest",
		"long-enough\x00rest",
	}
	for _, password := range tests {
		err := transport.ValidateMailboxPassword(password)
		if err == nil {
			t.Fatal("invalid password unexpectedly accepted")
		}
		if strings.Contains(err.Error(), password) {
			t.Fatal("validation error disclosed the password")
		}
	}
	if err := transport.ValidateMailboxPassword("12345678"); err != nil {
		t.Fatalf("minimum-length password rejected: %v", err)
	}
	if err := transport.ValidateMailboxPassword("strong-password"); err != nil {
		t.Fatalf("normal password rejected: %v", err)
	}
}

func TestUpdateMailPasswordRejectsUnsafeRequestBeforeHashing(t *testing.T) {
	oldBuildCommit := buildCommit
	oldHashGenerator := mailHashGenerator
	buildCommit = "mail-password-test"
	hashCalled := false
	mailHashGenerator = func(context.Context, string) (string, error) {
		hashCalled = true
		return mailPasswordTestHash("1234567890abcdef", 'A'), nil
	}
	t.Cleanup(func() {
		buildCommit = oldBuildCommit
		mailHashGenerator = oldHashGenerator
	})

	tests := []transport.UpdateMailPasswordRequest{
		{
			ExpectedBuildCommit: "wrong-build",
			Email:               "user@example.com",
			NewPassword:         "strong-password",
		},
		{
			ExpectedBuildCommit: "mail-password-test",
			Email:               "user@@example.com",
			NewPassword:         "strong-password",
		},
		{
			ExpectedBuildCommit: "mail-password-test",
			Email:               "user@example.com",
			NewPassword:         "strong-password\ninjected",
		},
	}
	for _, request := range tests {
		response := transport.MailMutationResponse{}
		err := (&Agent{}).UpdateMailPassword(&request, &response)
		if err == nil {
			t.Fatal("unsafe password request unexpectedly succeeded")
		}
		if response.Applied {
			t.Fatal("unsafe password request reported applied")
		}
		if strings.Contains(err.Error(), request.NewPassword) {
			t.Fatal("unsafe request error disclosed the password")
		}
	}
	if hashCalled {
		t.Fatal("hash generator called for an unsafe request")
	}
}

func TestValidateGeneratedDovecotHashFailsClosed(t *testing.T) {
	valid := []string{
		mailPasswordTestHash("1234567890abcdef", 'A'),
		"{SHA512-CRYPT}$6$rounds=10000$salt$" + strings.Repeat("z", 86),
	}
	for _, hash := range valid {
		if err := validateGeneratedDovecotHash(hash); err != nil {
			t.Fatalf("valid hash rejected: %v", err)
		}
	}

	invalid := []string{
		"",
		"{CRYPT}$6$salt$" + strings.Repeat("A", 86),
		"{SHA512-CRYPT}$5$salt$" + strings.Repeat("A", 86),
		"{SHA512-CRYPT}$6$$" + strings.Repeat("A", 86),
		"{SHA512-CRYPT}$6$1234567890abcdefg$" + strings.Repeat("A", 86),
		"{SHA512-CRYPT}$6$salt$short",
		"{SHA512-CRYPT}$6$salt$" + strings.Repeat("A", 85) + ":",
		"{SHA512-CRYPT}$6$rounds=999$salt$" + strings.Repeat("A", 86),
		"{SHA512-CRYPT}$6$rounds=wat$salt$" + strings.Repeat("A", 86),
		"{SHA512-CRYPT}$6$salt$" + strings.Repeat("A", 86) + "\n",
	}
	for _, hash := range invalid {
		if err := validateGeneratedDovecotHash(hash); err == nil {
			t.Fatalf("invalid generated hash accepted: %q", hash)
		}
	}
}

func TestReplaceDovecotPasswordHashChangesOnlyExactHashField(t *testing.T) {
	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	newHash := mailPasswordTestHash("fedcba0987654321", 'C')
	content := []byte(
		"# retained comment\n" +
			"user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M:keep=yes\n" +
			"other@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=50M\n",
	)

	got, err := replaceDovecotPasswordHash(content, "user@example.com", newHash)
	if err != nil {
		t.Fatalf("replace password hash: %v", err)
	}
	want := strings.Replace(string(content), oldHash, newHash, 1)
	if string(got) != want {
		t.Fatalf("dovecot users changed outside hash field\ngot:  %q\nwant: %q", got, want)
	}
}

func TestReplaceDovecotPasswordHashRejectsAmbiguousRows(t *testing.T) {
	oldHash := "{CRYPT}$6$1234567890abcdef$" + strings.Repeat("B", 86)
	newHash := mailPasswordTestHash("fedcba0987654321", 'C')
	validRow := "user@example.com:" + oldHash + "::::::userdb_quota_rule=*:storage=250M\n"
	tests := map[string]string{
		"missing":             "other@example.com:" + oldHash + "\n",
		"duplicate":           validRow + validRow,
		"noncanonical key":    "User@Example.COM:" + oldHash + "\n",
		"missing delimiter":   "user@example.com " + oldHash + "\n",
		"empty hash":          "user@example.com:::::::userdb_quota_rule=*:storage=250M\n",
		"unsupported scheme":  "user@example.com:{PLAIN}not-accepted::::::\n",
		"carriage return row": strings.TrimSuffix(validRow, "\n") + "\r\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := replaceDovecotPasswordHash([]byte(content), "user@example.com", newHash); err == nil {
				t.Fatal("ambiguous or malformed row unexpectedly accepted")
			}
		})
	}
}
