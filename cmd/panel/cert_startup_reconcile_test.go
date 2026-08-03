package main

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

func TestReferencedStagedCertificateLineagesIncludesRevokedRows(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()

	if _, err := database.Exec(`
		CREATE TABLE ssl_certificates (
			lineage_name TEXT,
			status TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}

	const (
		active  = "cp-site-4-111111111111111111111111"
		revoked = "cp-site-4-222222222222222222222222"
		expired = "cp-site-9-333333333333333333333333"
	)
	rows := []struct {
		lineage *string
		status  string
	}{
		{lineage: stringPointer(active), status: "active"},
		{lineage: stringPointer(revoked), status: "revoked"},
		{
			lineage: stringPointer("  " + strings.ToUpper(revoked) + "  "),
			status:  "revoked",
		},
		{lineage: stringPointer(expired), status: "expired"},
		{lineage: stringPointer("example.test"), status: "revoked"},
		{
			lineage: stringPointer("cp-site-0-444444444444444444444444"),
			status:  "revoked",
		},
		{
			lineage: stringPointer("cp-site-7-not-a-valid-random-suffix"),
			status:  "active",
		},
		{lineage: nil, status: "active"},
	}
	for _, row := range rows {
		if _, err := database.Exec(
			`INSERT INTO ssl_certificates (lineage_name, status) VALUES (?, ?)`,
			row.lineage,
			row.status,
		); err != nil {
			t.Fatal(err)
		}
	}

	got, err := referencedStagedCertificateLineages(
		context.Background(),
		database,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{active, revoked, expired}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("referenced lineages = %v, want %v", got, want)
	}
}

func stringPointer(value string) *string {
	return &value
}
