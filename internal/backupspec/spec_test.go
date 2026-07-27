package backupspec

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"testing"
)

type legacyCreateRequest struct {
	DomainName   string
	Type         string
	DatabaseName string
	DatabaseType string
	SourceDir    string
}

type legacyRestoreRequest struct {
	DomainName string
	BackupName string
	TargetDir  string
}

type legacyInfo struct {
	Name string
	Path string
}

func gobRoundTrip(t *testing.T, source, target any) {
	t.Helper()

	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(source); err != nil {
		t.Fatalf("encode %T: %v", source, err)
	}
	if err := gob.NewDecoder(&wire).Decode(target); err != nil {
		t.Fatalf("decode %T into %T: %v", source, target, err)
	}
}

func TestLegacyCreateRequestDecodesIntoV2(t *testing.T) {
	legacy := legacyCreateRequest{
		DomainName:   "example.test",
		Type:         TypeDatabase,
		DatabaseName: "tenant_database",
		DatabaseType: "postgresql",
		SourceDir:    "/legacy/source",
	}

	var current CreateRequest
	gobRoundTrip(t, legacy, &current)

	if current.DomainName != legacy.DomainName ||
		current.Type != legacy.Type ||
		current.DatabaseName != legacy.DatabaseName ||
		current.DatabaseType != legacy.DatabaseType ||
		current.SourceDir != legacy.SourceDir {
		t.Fatalf("legacy create request did not survive decode: %#v", current)
	}
}

func TestV2CreateRequestKeepsLegacyWireFields(t *testing.T) {
	current := CreateRequest{
		ProtocolVersion: ProtocolVersion,
		SubscriptionID:  4,
		DomainID:        13,
		DomainName:      "example.test",
		Type:            TypeDatabase,
		Database: DatabaseIdentity{
			ID:   7,
			Name: "tenant_database",
			Type: "postgresql",
		},
		DatabaseName: "tenant_database",
		DatabaseType: "postgresql",
		SourceDir:    "/legacy/source",
	}

	var legacy legacyCreateRequest
	gobRoundTrip(t, current, &legacy)

	if legacy.DomainName != current.DomainName ||
		legacy.Type != current.Type ||
		legacy.DatabaseName != current.DatabaseName ||
		legacy.DatabaseType != current.DatabaseType ||
		legacy.SourceDir != current.SourceDir {
		t.Fatalf("v2 create request lost legacy wire fields: %#v", legacy)
	}
}

func TestV2RestoreRequestKeepsLegacyWireFields(t *testing.T) {
	current := RestoreRequest{
		ProtocolVersion: ProtocolVersion,
		SubscriptionID:  4,
		DomainID:        13,
		DomainName:      "example.test",
		BackupName:      "files_20260727_120000.tar.gz",
		TargetDir:       "/legacy/target",
	}

	var legacy legacyRestoreRequest
	gobRoundTrip(t, current, &legacy)

	if legacy.DomainName != current.DomainName ||
		legacy.BackupName != current.BackupName ||
		legacy.TargetDir != current.TargetDir {
		t.Fatalf("v2 restore request lost legacy wire fields: %#v", legacy)
	}
}

func TestInfoKeepsLegacyGobPathButHidesItFromJSON(t *testing.T) {
	current := Info{Name: "backup.cpbak", Path: "/var/backups/celikpanel/backup.cpbak"}
	var legacy legacyInfo
	gobRoundTrip(t, current, &legacy)
	if legacy.Name != current.Name || legacy.Path != current.Path {
		t.Fatalf("v2 info lost legacy gob fields: %#v", legacy)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("/var/backups")) || bytes.Contains(encoded, []byte(`"path"`)) {
		t.Fatalf("private backup path leaked to JSON: %s", encoded)
	}
}
