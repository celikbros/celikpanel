package main

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPromptPasswordVisibilityDefaultsToHidden(t *testing.T) {
	var output bytes.Buffer
	visible, err := promptPasswordVisibility(bufio.NewReader(strings.NewReader("\n")), &output)
	if err != nil {
		t.Fatalf("promptPasswordVisibility returned an error: %v", err)
	}
	if visible {
		t.Fatal("empty answer should select hidden password input")
	}
}

func TestPromptAdminUsernameAndEmailRetryInvalidInput(t *testing.T) {
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader("x\nvalid-admin\n\nadmin@example.com\n"))

	username, err := promptAdminUsername(reader, &output)
	if err != nil {
		t.Fatalf("promptAdminUsername returned an error: %v", err)
	}
	if username != "valid-admin" {
		t.Fatalf("username = %q, want valid-admin", username)
	}

	email, err := promptAdminEmail(reader, &output)
	if err != nil {
		t.Fatalf("promptAdminEmail returned an error: %v", err)
	}
	if email != "admin@example.com" {
		t.Fatalf("email = %q, want admin@example.com", email)
	}
	if !strings.Contains(output.String(), "Invalid username") {
		t.Fatalf("invalid username did not produce guidance: %q", output.String())
	}
	if !strings.Contains(output.String(), "Email must not be empty") {
		t.Fatalf("empty email did not produce guidance: %q", output.String())
	}
}

func TestPromptPasswordVisibilityAcceptsTurkishAndRetriesInvalidChoice(t *testing.T) {
	var output bytes.Buffer
	visible, err := promptPasswordVisibility(bufio.NewReader(strings.NewReader("maybe\ne\n")), &output)
	if err != nil {
		t.Fatalf("promptPasswordVisibility returned an error: %v", err)
	}
	if !visible {
		t.Fatal("e should select visible password input")
	}
	if !strings.Contains(output.String(), "Please enter y/e") {
		t.Fatalf("invalid choice did not produce guidance: %q", output.String())
	}
}

func TestReadAndConfirmPasswordRetriesAfterMismatch(t *testing.T) {
	answers := []string{"first-password", "different-password", "second-password", "second-password"}
	read := func(string) (string, error) {
		answer := answers[0]
		answers = answers[1:]
		return answer, nil
	}

	var output bytes.Buffer
	password, err := readAndConfirmPassword(read, &output)
	if err != nil {
		t.Fatalf("readAndConfirmPassword returned an error: %v", err)
	}
	if password != "second-password" {
		t.Fatalf("password = %q, want second-password", password)
	}
	if !strings.Contains(output.String(), "Passwords do not match") {
		t.Fatalf("mismatch did not produce guidance: %q", output.String())
	}
}

func TestReadAndConfirmPasswordRetriesShortPassword(t *testing.T) {
	answers := []string{"short", "long-enough", "long-enough"}
	read := func(string) (string, error) {
		answer := answers[0]
		answers = answers[1:]
		return answer, nil
	}

	var output bytes.Buffer
	password, err := readAndConfirmPassword(read, &output)
	if err != nil {
		t.Fatalf("readAndConfirmPassword returned an error: %v", err)
	}
	if password != "long-enough" {
		t.Fatalf("password = %q, want long-enough", password)
	}
	if !strings.Contains(output.String(), "at least 8 characters") {
		t.Fatalf("short password did not produce guidance: %q", output.String())
	}
}

func TestReadAndConfirmPasswordStopsOnInputError(t *testing.T) {
	wantErr := errors.New("terminal closed")
	_, err := readAndConfirmPassword(func(string) (string, error) {
		return "", wantErr
	}, &bytes.Buffer{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestReadAutomatedPasswordRemainsSingleLine(t *testing.T) {
	password, err := readAutomatedPassword(bufio.NewReader(strings.NewReader("automated-password\n")))
	if err != nil {
		t.Fatalf("readAutomatedPassword returned an error: %v", err)
	}
	if password != "automated-password" {
		t.Fatalf("password = %q, want automated-password", password)
	}
}
