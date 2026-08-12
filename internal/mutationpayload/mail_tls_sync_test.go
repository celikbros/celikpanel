package mutationpayload

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

const mailTLSTestRoot = "/etc/ssl/celikpanel"

func mailTLSTestVersion(character string) string {
	return "sha256-" + strings.Repeat(character, 64)
}

func mailTLSTestEntry(domain, version string, names ...string) transport.MailSNIEntry {
	directory := mailTLSTestRoot + "/" + domain + "/" + version
	return transport.MailSNIEntry{
		Names:    names,
		CertPath: directory + "/fullchain.pem",
		KeyPath:  directory + "/privkey.pem",
	}
}

func TestCanonicalMailTLSSyncSortsFreezesAndBindsEverything(t *testing.T) {
	input := []transport.MailSNIEntry{
		mailTLSTestEntry("z.example", mailTLSTestVersion("b"), "z.example", "mail.z.example"),
		mailTLSTestEntry("a.example", mailTLSTestVersion("a"), "mail.a.example"),
	}
	commitment, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", input)
	if err != nil {
		t.Fatal(err)
	}
	if commitment.ManagedRoot != mailTLSTestRoot ||
		commitment.Myhostname != "mx.example.test" ||
		len(commitment.SNI) != 2 ||
		commitment.SNI[0].Names[0] != "mail.a.example" ||
		commitment.SNI[1].Names[0] != "mail.z.example" ||
		commitment.SNI[1].Names[1] != "z.example" ||
		!ValidMailTLSSyncQualifier(commitment.Qualifier) {
		t.Fatalf("unexpected canonical commitment: %+v", commitment)
	}

	input[0].Names[0] = "changed.example"
	input[0].CertPath = "/changed"
	if commitment.SNI[1].Names[1] != "z.example" ||
		!strings.HasSuffix(commitment.SNI[1].CertPath, "/fullchain.pem") {
		t.Fatal("canonical commitment aliases caller-owned input")
	}

	reordered, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", []transport.MailSNIEntry{
		mailTLSTestEntry("a.example", mailTLSTestVersion("a"), "mail.a.example"),
		mailTLSTestEntry("z.example", mailTLSTestVersion("b"), "mail.z.example", "z.example"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Qualifier != commitment.Qualifier {
		t.Fatalf("qualifier depends on caller order: %q != %q", reordered.Qualifier, commitment.Qualifier)
	}

	changedRoot, err := CanonicalMailTLSSync("/srv/certs", "mx.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyDefault, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	changedHost, err := CanonicalMailTLSSync(mailTLSTestRoot, "other.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if changedRoot.Qualifier == emptyDefault.Qualifier ||
		changedHost.Qualifier == emptyDefault.Qualifier {
		t.Fatal("qualifier does not bind managed root and hostname")
	}
}

func TestCanonicalMailTLSSyncRejectsNonCanonicalRootAndHost(t *testing.T) {
	for _, test := range []struct {
		name string
		root string
		host string
	}{
		{name: "empty root", host: "mx.example.test"},
		{name: "relative root", root: "etc/ssl/celikpanel", host: "mx.example.test"},
		{name: "root slash", root: "/", host: "mx.example.test"},
		{name: "root trailing slash", root: mailTLSTestRoot + "/", host: "mx.example.test"},
		{name: "root whitespace", root: " " + mailTLSTestRoot, host: "mx.example.test"},
		{name: "host uppercase", root: mailTLSTestRoot, host: "MX.example.test"},
		{name: "host whitespace", root: mailTLSTestRoot, host: " mx.example.test"},
		{name: "host trailing dot", root: mailTLSTestRoot, host: "mx.example.test."},
		{name: "host invalid", root: mailTLSTestRoot, host: "localhost"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CanonicalMailTLSSync(test.root, test.host, nil); err == nil {
				t.Fatal("non-canonical identity was accepted")
			}
		})
	}
}

func TestCanonicalMailTLSSyncRejectsInvalidSnapshotEntries(t *testing.T) {
	version := mailTLSTestVersion("a")
	valid := mailTLSTestEntry("example.test", version, "mail.example.test")
	clone := func() transport.MailSNIEntry {
		entry := valid
		entry.Names = append([]string(nil), valid.Names...)
		return entry
	}
	tests := []struct {
		name   string
		mutate func(*transport.MailSNIEntry)
	}{
		{name: "no names", mutate: func(entry *transport.MailSNIEntry) { entry.Names = nil }},
		{name: "too many names", mutate: func(entry *transport.MailSNIEntry) {
			entry.Names = []string{"mail.example.test", "example.test", "third.example.test"}
		}},
		{name: "uppercase name", mutate: func(entry *transport.MailSNIEntry) { entry.Names[0] = "Mail.example.test" }},
		{name: "unrelated name", mutate: func(entry *transport.MailSNIEntry) { entry.Names[0] = "mail.other.test" }},
		{name: "missing mail name", mutate: func(entry *transport.MailSNIEntry) { entry.Names[0] = "example.test" }},
		{name: "outside root", mutate: func(entry *transport.MailSNIEntry) {
			entry.CertPath = "/tmp/example.test/" + version + "/fullchain.pem"
		}},
		{name: "root prefix collision", mutate: func(entry *transport.MailSNIEntry) {
			entry.CertPath = mailTLSTestRoot + "-evil/example.test/" + version + "/fullchain.pem"
		}},
		{name: "mutable current", mutate: func(entry *transport.MailSNIEntry) {
			entry.CertPath = mailTLSTestRoot + "/example.test/current/fullchain.pem"
		}},
		{name: "uppercase version", mutate: func(entry *transport.MailSNIEntry) {
			entry.CertPath = mailTLSTestRoot + "/example.test/sha256-" + strings.Repeat("A", 64) + "/fullchain.pem"
		}},
		{name: "wrong certificate filename", mutate: func(entry *transport.MailSNIEntry) {
			entry.CertPath = mailTLSTestRoot + "/example.test/" + version + "/cert.pem"
		}},
		{name: "different key snapshot", mutate: func(entry *transport.MailSNIEntry) {
			entry.KeyPath = mailTLSTestRoot + "/example.test/" + mailTLSTestVersion("b") + "/privkey.pem"
		}},
		{name: "different key domain", mutate: func(entry *transport.MailSNIEntry) {
			entry.KeyPath = mailTLSTestRoot + "/other.test/" + version + "/privkey.pem"
		}},
		{name: "path whitespace", mutate: func(entry *transport.MailSNIEntry) { entry.KeyPath += " " }},
		{name: "path traversal", mutate: func(entry *transport.MailSNIEntry) {
			entry.KeyPath = mailTLSTestRoot + "/example.test/" + version + "/../" + version + "/privkey.pem"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := clone()
			test.mutate(&entry)
			if _, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", []transport.MailSNIEntry{entry}); err == nil {
				t.Fatal("invalid snapshot entry was accepted")
			}
		})
	}
}

func TestCanonicalMailTLSSyncRejectsDuplicateClaimsAndEntryOverflow(t *testing.T) {
	version := mailTLSTestVersion("a")
	duplicate := []transport.MailSNIEntry{
		mailTLSTestEntry("example.test", version, "mail.example.test"),
		mailTLSTestEntry("example.test", version, "mail.example.test"),
	}
	if _, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", duplicate); err == nil {
		t.Fatal("duplicate SNI claim was accepted")
	}
	tooMany := make([]transport.MailSNIEntry, mailTLSSyncMaxEntries+1)
	if _, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", tooMany); err == nil {
		t.Fatal("entry overflow was accepted")
	}
}

func TestValidMailTLSSyncQualifierIsStrict(t *testing.T) {
	commitment, err := CanonicalMailTLSSync(mailTLSTestRoot, "mx.example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"",
		commitment.Qualifier + "0",
		strings.ToUpper(commitment.Qualifier),
		strings.Replace(commitment.Qualifier, "sha256:", "sha512:", 1),
		strings.TrimPrefix(commitment.Qualifier, "mail-tls-sync/v1:"),
	} {
		if ValidMailTLSSyncQualifier(value) {
			t.Fatalf("invalid qualifier accepted: %q", value)
		}
	}
	if !ValidMailTLSSyncQualifier(commitment.Qualifier) {
		t.Fatal("canonical qualifier rejected")
	}
}
