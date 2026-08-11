package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileMailTLSFailsClosedWithoutDovecotBeforeCommands(t *testing.T) {
	previousLookup := lookupMailTLSCommand
	postconfPath := filepath.Join(t.TempDir(), "postconf")
	lookupMailTLSCommand = func(name string) (string, error) {
		if name == "postconf" {
			return postconfPath, nil
		}
		return "", fmt.Errorf("%s unavailable", name)
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })
	t.Setenv("CELIKPANEL_MAIL_DIR", "")

	commandCalled := false
	runner := func(string, ...string) ([]byte, error) {
		commandCalled = true
		return nil, nil
	}
	var response SecureMailTLSResponse
	if err := reconcileMailTLS(&SecureMailTLSRequest{}, &response, runner); err != nil {
		t.Fatal(err)
	}
	if response.Error != "dovecot is not installed" {
		t.Fatalf("missing-Dovecot error = %q", response.Error)
	}
	if commandCalled {
		t.Fatal("missing Dovecot was detected only after a host command")
	}
}

func TestMailTLSPreflightPinsAbsoluteCommandsAfterAllLookups(t *testing.T) {
	previousLookup := lookupMailTLSCommand
	binRoot := filepath.Join(t.TempDir(), "bin")
	var lookups []string
	lookupMailTLSCommand = func(name string) (string, error) {
		lookups = append(lookups, name)
		return filepath.Join(binRoot, name), nil
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })
	var calls []string
	runner := func(name string, args ...string) ([]byte, error) {
		if got := strings.Join(lookups, ","); got != "postconf,doveconf,postfix,dovecot,systemctl,postmap" {
			t.Fatalf("runner called before all lookups completed: %s", got)
		}
		calls = append(calls, name)
		if filepath.Base(name) == "postmap" {
			if len(args) != 2 || args[0] != "-F" || !strings.HasPrefix(args[1], "lmdb:") {
				t.Fatalf("unexpected probe command: %q %q", name, args)
			}
			probe := strings.TrimPrefix(args[1], "lmdb:")
			if err := os.WriteFile(probe+".lmdb", []byte("index"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return nil, nil
	}
	preflight, err := preflightMailTLSCommands(true, runner)
	if err != nil {
		t.Fatal(err)
	}
	if preflight.sniMapType != "lmdb" {
		t.Fatalf("map type = %q, want lmdb", preflight.sniMapType)
	}
	if _, err := preflight.run("postconf", "-h", "myhostname"); err != nil {
		t.Fatal(err)
	}
	if got, want := calls[len(calls)-1], filepath.Join(binRoot, "postconf"); got != want {
		t.Fatalf("pinned command = %q, want %q", got, want)
	}
	before := len(calls)
	if _, err := preflight.run("unapproved-command"); err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("unknown pinned command error = %v", err)
	}
	if len(calls) != before {
		t.Fatal("unknown command reached the underlying runner")
	}
}

func TestMailTLSPreflightRejectsNonCanonicalCommandPathBeforeRunner(t *testing.T) {
	previousLookup := lookupMailTLSCommand
	lookupMailTLSCommand = func(name string) (string, error) { return name, nil }
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })
	called := false
	_, err := preflightMailTLSCommands(false, func(string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "canonical absolute path") {
		t.Fatalf("non-canonical lookup error = %v", err)
	}
	if called {
		t.Fatal("runner called after a non-canonical lookup")
	}
}

func TestMailTLSPreflightRefusesEveryMissingToolBeforeRunner(t *testing.T) {
	tests := []struct {
		missing     string
		needsSNIMap bool
		wantMarker  string
	}{
		{missing: "postconf", wantMarker: "postfix is not installed"},
		{missing: "doveconf", wantMarker: "dovecot is not installed"},
		{missing: "postfix", wantMarker: "postfix"},
		{missing: "dovecot", wantMarker: "dovecot"},
		{missing: "systemctl", wantMarker: "systemctl"},
		{missing: "postmap", needsSNIMap: true, wantMarker: "postmap"},
	}
	for _, test := range tests {
		t.Run(test.missing, func(t *testing.T) {
			previousLookup := lookupMailTLSCommand
			binRoot := filepath.Join(t.TempDir(), "bin")
			lookupMailTLSCommand = func(name string) (string, error) {
				if name == test.missing {
					return "", fmt.Errorf("%s is unavailable", name)
				}
				return filepath.Join(binRoot, name), nil
			}
			t.Cleanup(func() { lookupMailTLSCommand = previousLookup })

			runnerCalled := false
			_, err := preflightMailTLSCommands(test.needsSNIMap, func(string, ...string) ([]byte, error) {
				runnerCalled = true
				return nil, nil
			})
			if err == nil || !strings.Contains(err.Error(), test.wantMarker) {
				t.Fatalf("missing %s error = %v", test.missing, err)
			}
			if runnerCalled {
				t.Fatalf("missing %s reached the command runner", test.missing)
			}
		})
	}
}

func TestMailTLSPreflightDoesNotRequirePostmapWithoutSNI(t *testing.T) {
	previousLookup := lookupMailTLSCommand
	binRoot := filepath.Join(t.TempDir(), "bin")
	postmapLookup := false
	lookupMailTLSCommand = func(name string) (string, error) {
		if name == "postmap" {
			postmapLookup = true
			return "", fmt.Errorf("postmap is unavailable")
		}
		return filepath.Join(binRoot, name), nil
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })

	runnerCalled := false
	if _, err := preflightMailTLSCommands(false, func(string, ...string) ([]byte, error) {
		runnerCalled = true
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if postmapLookup {
		t.Fatal("empty SNI preflight looked up postmap")
	}
	if runnerCalled {
		t.Fatal("empty SNI preflight ran a command")
	}
}

func TestReconcileMailTLSRejectsUnsupportedSNIMapBeforeSnapshot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_MAIL_DIR", "")
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
	previousLookup := lookupMailTLSCommand
	binRoot := filepath.Join(t.TempDir(), "bin")
	lookupMailTLSCommand = func(name string) (string, error) { return filepath.Join(binRoot, name), nil }
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })
	var commands []string
	runner := func(name string, _ ...string) ([]byte, error) {
		commands = append(commands, filepath.Base(name))
		return nil, nil
	}
	var response SecureMailTLSResponse
	err := reconcileMailTLS(&SecureMailTLSRequest{
		Myhostname: "mail.example.test",
		SNI:        []MailSNIEntry{validMailSNIEntry(snapshot)},
	}, &response, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "no usable indexed table type") {
		t.Fatalf("unsupported map response = %#v", response)
	}
	if got := strings.Join(commands, ","); got != "postmap,postmap,postmap" {
		t.Fatalf("commands before unsupported-map refusal = %q", got)
	}
}

func TestReconcileMailTLSMutationRequiresRequestAndDurableBinding(t *testing.T) {
	agent := &Agent{}
	if err := agent.SecureMailTLS(nil, nil); err == nil ||
		!strings.Contains(err.Error(), "response is required") {
		t.Fatalf("legacy nil response error = %v", err)
	}
	if err := agent.ReconcileMailTLSMutation(nil, nil); err == nil ||
		!strings.Contains(err.Error(), "response is required") {
		t.Fatalf("nil response error = %v", err)
	}
	var missingRequest SecureMailTLSResponse
	if err := agent.ReconcileMailTLSMutation(nil, &missingRequest); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missingRequest.Error, "request is required") {
		t.Fatalf("nil request error = %q", missingRequest.Error)
	}

	var missingBinding SecureMailTLSResponse
	if err := agent.ReconcileMailTLSMutation(
		&ReconcileMailTLSMutationRequest{Myhostname: "mail.example.test"},
		&missingBinding,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missingBinding.Error, "durable service mutation lease") {
		t.Fatalf("missing binding error = %q", missingBinding.Error)
	}

	var missingHostname SecureMailTLSResponse
	if err := agent.ReconcileMailTLSMutation(
		&ReconcileMailTLSMutationRequest{},
		&missingHostname,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missingHostname.Error, "requires a server hostname") {
		t.Fatalf("missing hostname error = %q", missingHostname.Error)
	}
}

const testCertificateVersion = "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type testManagedMailTLSSnapshot struct {
	root       string
	domain     string
	versionDir string
	certPath   string
	keyPath    string
}

func createManagedMailTLSSnapshot(t *testing.T, root, domain, version string) testManagedMailTLSSnapshot {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	if version == "" {
		version = testCertificateVersion
	}
	domainDir := filepath.Join(root, domain)
	versionDir := filepath.Join(domainDir, version)
	if err := os.MkdirAll(versionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{domainDir, versionDir} {
		if err := os.Chmod(path, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	certPath := filepath.Join(versionDir, "fullchain.pem")
	keyPath := filepath.Join(versionDir, "privkey.pem")
	if err := os.WriteFile(certPath, []byte("certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(certPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	return testManagedMailTLSSnapshot{
		root:       root,
		domain:     domain,
		versionDir: versionDir,
		certPath:   certPath,
		keyPath:    keyPath,
	}
}

func validMailSNIEntry(snapshot testManagedMailTLSSnapshot) MailSNIEntry {
	return MailSNIEntry{
		Names:    []string{"mail." + snapshot.domain, snapshot.domain},
		CertPath: snapshot.certPath,
		KeyPath:  snapshot.keyPath,
	}
}

func TestValidateMailSNIEntriesRejectsInvalidInput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
	otherVersion := createManagedMailTLSSnapshot(
		t,
		root,
		"example.test",
		"sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	otherDomain := createManagedMailTLSSnapshot(t, root, "other.test", "")
	outside := createManagedMailTLSSnapshot(t, t.TempDir(), "example.test", "")

	base := validMailSNIEntry(snapshot)
	tests := []struct {
		name       string
		mutate     func(*MailSNIEntry)
		wantDetail string
	}{
		{"no names", func(entry *MailSNIEntry) { entry.Names = nil }, "has no names"},
		{"blank name", func(entry *MailSNIEntry) { entry.Names = []string{"mail.example.test", "  "} }, "not a valid FQDN"},
		{"config injection name", func(entry *MailSNIEntry) { entry.Names = []string{"mail.example.test\nlocal_name attacker.test"} }, "not a valid FQDN"},
		{"unrelated name", func(entry *MailSNIEntry) { entry.Names = []string{"mail.attacker.test"} }, "does not belong"},
		{"mail name required", func(entry *MailSNIEntry) { entry.Names = []string{"example.test"} }, "does not include the managed mail hostname"},
		{"too many names", func(entry *MailSNIEntry) {
			entry.Names = []string{"mail.example.test", "example.test", "www.example.test"}
		}, "too many names"},
		{"empty certificate path", func(entry *MailSNIEntry) { entry.CertPath = "" }, "path is empty"},
		{"outside certificate path", func(entry *MailSNIEntry) { entry.CertPath = outside.certPath }, "outside the managed certificate root"},
		{"wrong certificate filename", func(entry *MailSNIEntry) { entry.CertPath = filepath.Join(snapshot.versionDir, "cert.pem") }, "must end in fullchain.pem"},
		{"non canonical certificate path", func(entry *MailSNIEntry) {
			entry.CertPath = snapshot.versionDir + string(filepath.Separator) + "nested" +
				string(filepath.Separator) + ".." + string(filepath.Separator) + "fullchain.pem"
		}, "canonical absolute path"},
		{"empty private key path", func(entry *MailSNIEntry) { entry.KeyPath = "" }, "path is empty"},
		{"different snapshot key", func(entry *MailSNIEntry) { entry.KeyPath = otherVersion.keyPath }, "same managed snapshot"},
		{"different domain key", func(entry *MailSNIEntry) { entry.KeyPath = otherDomain.keyPath }, "same managed snapshot"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := base
			entry.Names = append([]string(nil), base.Names...)
			test.mutate(&entry)
			_, err := validateMailSNIEntries([]MailSNIEntry{entry})
			if err == nil || !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("validateMailSNIEntries() error = %v, want detail %q", err, test.wantDetail)
			}
		})
	}
}

func TestValidateMailSNIEntriesNormalisesNames(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")

	got, err := validateMailSNIEntries([]MailSNIEntry{{
		Names:    []string{" Mail.Example.Test. ", " EXAMPLE.TEST. "},
		CertPath: snapshot.certPath,
		KeyPath:  snapshot.keyPath,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Names) != 2 ||
		got[0].Names[0] != "mail.example.test" ||
		got[0].Names[1] != "example.test" {
		t.Fatalf("unexpected validated entries: %#v", got)
	}
}

func TestValidateSecureMailTLSRequestCanonicalisesAndRejectsHostnameInjection(t *testing.T) {
	got, entries, err := validateSecureMailTLSRequest(&SecureMailTLSRequest{
		Myhostname: " Boston.CelikHost.COM. ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "boston.celikhost.com" || len(entries) != 0 {
		t.Fatalf("validated request = hostname %q entries %#v", got, entries)
	}

	for _, candidate := range []string{
		"localhost",
		"127.0.0.1",
		"boston.celikhost.com\nsmtpd_tls_security_level=none",
		"bad_name.celikhost.com",
	} {
		if _, _, err := validateSecureMailTLSRequest(&SecureMailTLSRequest{
			Myhostname: candidate,
		}); err == nil {
			t.Fatalf("validateSecureMailTLSRequest(%q) accepted an unsafe hostname", candidate)
		}
	}
}

func TestValidateMailSNIEntriesCapsSnapshotSize(t *testing.T) {
	entries := make([]MailSNIEntry, maxMailSNIEntries+1)
	if _, err := validateMailSNIEntries(entries); err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Fatalf("oversized snapshot error = %v", err)
	}
}

func TestValidateMailSNIEntriesRejectsDuplicateNamesAcrossEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	first := createManagedMailTLSSnapshot(t, root, "example.test", "")
	second := createManagedMailTLSSnapshot(
		t,
		root,
		"example.test",
		"sha256-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	if _, err := validateMailSNIEntries([]MailSNIEntry{
		validMailSNIEntry(first),
		validMailSNIEntry(second),
	}); err == nil || !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("duplicate SNI name error = %v", err)
	}
}

func TestSnapshotMailTLSFileRejectsNonRegularPath(t *testing.T) {
	if _, err := snapshotMailTLSFile(t.TempDir()); err == nil {
		t.Fatal("snapshotMailTLSFile() accepted a directory")
	}
}
