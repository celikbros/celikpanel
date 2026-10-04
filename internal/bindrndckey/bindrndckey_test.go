package bindrndckey

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func validRecord() Record {
	return Record{
		Schema: RecordSchema, Path: PacmanKeyPath, Provenance: ProvenanceProductCreated,
		SHA256:            strings.Repeat("a", 64),
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("b", 64),
		MutationRequestID: strings.Repeat("1", 32), MutationOwnerID: strings.Repeat("2", 32),
	}
}

func TestRecordRoundTripAndRefusals(t *testing.T) {
	record := validRecord()
	encoded, err := Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded != record {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	owner := record
	owner.Provenance, owner.SHA256, owner.Basis = ProvenanceOwnerOrPackage, "", BasisControlsStatement
	if _, err := Encode(owner); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Record){
		"unknown-path":       func(r *Record) { r.Path = "/tmp/rndc.key" },
		"product-no-hash":    func(r *Record) { r.SHA256 = "" },
		"product-with-basis": func(r *Record) { r.Basis = BasisKeyPresent },
		"owner-with-hash":    func(r *Record) { r.Provenance = ProvenanceOwnerOrPackage; r.Basis = BasisKeyPresent },
		"owner-no-basis":     func(r *Record) { r.Provenance, r.SHA256 = ProvenanceOwnerOrPackage, "" },
		"bad-identity":       func(r *Record) { r.MutationRequestID = "x" },
		"bad-schema":         func(r *Record) { r.Schema = "v0" },
	} {
		bad := record
		mutate(&bad)
		if _, err := Encode(bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := Decode(append([]byte(" "), encoded...)); err == nil {
		t.Fatal("non-canonical record accepted")
	}
	if _, err := Decode([]byte(strings.Replace(string(encoded), `"schema"`, `"extra":1,"schema"`, 1))); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestClassifyControlFailure(t *testing.T) {
	exit := errors.New("exit status 1")
	for _, output := range []string{
		"rndc: neither /etc/rndc.conf nor /etc/rndc.key was found\n",
		"rndc: error: open: /etc/bind/rndc.key: permission denied\nrndc: could not load rndc configuration\n",
		"rndc: connection to remote host closed.\nThis may indicate that\n* the key is invalid.\n",
		"rndc: connect failed: 127.0.0.1#953: connection refused\n",
	} {
		err := ClassifyControlFailure([]byte(output), exit)
		unavailable, ok := ReasonOf(err)
		if !ok || unavailable.Reason() != transport.DNSPublicationFailureBINDRNDCUnavailable ||
			unavailable.Detail != strings.SplitN(output, "\n", 2)[0] {
			t.Fatalf("%q -> %v", output, err)
		}
	}
	for _, output := range []string{
		"rndc: 'zonestatus' failed: not found\nno matching zone 'a.test' in any view\n",
		"",
		"something else\n",
	} {
		if err := ClassifyControlFailure([]byte(output), exit); err != nil {
			t.Fatalf("%q classified: %v", output, err)
		}
	}
	if ClassifyControlFailure([]byte("rndc: neither /etc/rndc.conf nor /etc/rndc.key was found\n"), nil) != nil {
		t.Fatal("a successful command was classified")
	}
	if got := FirstLine([]byte("\n  rndc: xéy  \nsecond")); got != "rndc: x?y" {
		t.Fatalf("first line %q", got)
	}
}

func TestRemoveIfUnchangedKeepsChangedAndNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rndc.key")
	if err := os.WriteFile(path, []byte("key \"rndc-key\" { algorithm hmac-sha256; secret \"x\"; };\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	sum, err := HashKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := removeIfUnchangedAt(path, strings.Repeat("f", 64)); err != nil || outcome != RemovalKeptChanged {
		t.Fatalf("changed: %s %v", outcome, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("changed key was removed")
	}
	if outcome, err := removeIfUnchangedAt(path, sum); err != nil || outcome != RemovalRemoved {
		t.Fatalf("unchanged: %s %v", outcome, err)
	}
	if outcome, err := removeIfUnchangedAt(path, sum); err != nil || outcome != RemovalAlreadyAbsent {
		t.Fatalf("absent: %s %v", outcome, err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	targetSum, _ := HashKeyFile(target)
	if outcome, err := removeIfUnchangedAt(path, targetSum); err != nil || outcome != RemovalKeptChanged {
		t.Fatalf("symlink: %s %v", outcome, err)
	}
	if _, err := HashKeyFile(path); err == nil {
		t.Fatal("symlink hashed")
	}
	if _, err := RemoveIfUnchanged(path, targetSum); err == nil {
		t.Fatal("unknown path accepted")
	}
}
