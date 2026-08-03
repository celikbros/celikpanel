package services

import "testing"

func TestValidateSQLIdentifier(t *testing.T) {
	valid := []string{"example_com", "db1", "_private", "UserName", "a"}
	for _, name := range valid {
		if err := ValidateSQLIdentifier(name); err != nil {
			t.Errorf("ValidateSQLIdentifier(%q) = %v, want nil", name, err)
		}
	}

	// Each of these is a real injection or edge case that must be rejected.
	// Bunların her biri reddedilmesi gereken gerçek bir enjeksiyon ya da
	// sınır durumudur.
	invalid := []string{
		"",
		"1db",                     // starts with a digit
		"drop table",              // space
		`ex";DROP TABLE users;--`, // classic injection
		"user'--",                 // quote
		"user`--",                 // backtick
		"tab\tname",               // control char
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // too long
	}
	for _, name := range invalid {
		if err := ValidateSQLIdentifier(name); err == nil {
			t.Errorf("ValidateSQLIdentifier(%q) = nil, want error", name)
		}
	}
}

func TestQuoteIdentifiers(t *testing.T) {
	pg, err := QuotePGIdentifier("my_db")
	if err != nil || pg != `"my_db"` {
		t.Errorf("QuotePGIdentifier = %q, %v", pg, err)
	}
	my, err := QuoteMySQLIdentifier("my_db")
	if err != nil || my != "`my_db`" {
		t.Errorf("QuoteMySQLIdentifier = %q, %v", my, err)
	}
	if _, err := QuotePGIdentifier(`x"; DROP`); err == nil {
		t.Error("QuotePGIdentifier accepted an injection payload")
	}
}

func TestQuoteStringLiterals(t *testing.T) {
	// A password containing a quote must be neutralized, not passed through.
	// Tırnak içeren bir parola geçirilmemeli, etkisizleştirilmelidir.
	pg, err := QuotePGStringLiteral("pa'ss")
	if err != nil || pg != "'pa''ss'" {
		t.Errorf("QuotePGStringLiteral = %q, %v", pg, err)
	}

	my, err := QuoteMySQLStringLiteral(`pa'ss\word`)
	if err != nil || my != `'pa\'ss\\word'` {
		t.Errorf("QuoteMySQLStringLiteral = %q, %v", my, err)
	}

	// The canonical PostgreSQL injection: close the literal, run DROP.
	// Klasik PostgreSQL enjeksiyonu: literali kapat, DROP çalıştır.
	inj, err := QuotePGStringLiteral("x'; DROP DATABASE prod;--")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inj != "'x''; DROP DATABASE prod;--'" {
		t.Errorf("injection not neutralized: %q", inj)
	}

	if _, err := QuotePGStringLiteral("bad\x00null"); err == nil {
		t.Error("QuotePGStringLiteral accepted a NUL byte")
	}
	if _, err := QuoteMySQLStringLiteral(""); err == nil {
		t.Error("QuoteMySQLStringLiteral accepted an empty secret")
	}
}

func TestValidatePrivileges(t *testing.T) {
	got, err := ValidatePrivileges("select, insert ,update")
	if err != nil || got != "SELECT, INSERT, UPDATE" {
		t.Errorf("ValidatePrivileges = %q, %v", got, err)
	}

	if _, err := ValidatePrivileges("SELECT; DROP TABLE users"); err == nil {
		t.Error("ValidatePrivileges accepted an injection payload")
	}
	if _, err := ValidatePrivileges(""); err == nil {
		t.Error("ValidatePrivileges accepted an empty list")
	}
}
