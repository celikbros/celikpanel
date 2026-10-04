//go:build linux

package dnsenginerecovery

import (
	"context"
	"strings"
	"testing"
)

func pdnsIdentityFixture() []byte {
	return []byte("Id=pdns.service\nNames=pdns.service\n" +
		"FragmentPath=/usr/lib/systemd/system/pdns.service\nDropInPaths=\nSourcePath=\nTransient=no\n" +
		"ExecStart={ path=/usr/sbin/pdns_server ; argv[]=/usr/sbin/pdns_server --guardian=no --daemon=no --disable-syslog --log-timestamp=no --write-pid=no ; ignore_errors=no }\n")
}

func TestProbePDNSVendorIdentityRequiresExactStableUnit(t *testing.T) {
	calls := 0
	identity, err := ProbePDNSVendorIdentity(context.Background(), pdnsProfile(),
		func(_ context.Context, unit string) ([]byte, error) {
			calls++
			if unit != "pdns.service" {
				t.Fatalf("unexpected unit %s", unit)
			}
			return pdnsIdentityFixture(), nil
		})
	if err != nil || identity.ExecStartPath != "/usr/sbin/pdns_server" || calls != 2 {
		t.Fatalf("identity: %+v err=%v calls=%d", identity, err, calls)
	}
	for _, bad := range [][]byte{
		[]byte(strings.Replace(string(pdnsIdentityFixture()), "--guardian=no", "--guardian=yes", 1)),
		[]byte(strings.Replace(string(pdnsIdentityFixture()), "DropInPaths=\n", "DropInPaths=/etc/systemd/system/pdns.service.d/override.conf\n", 1)),
		[]byte(strings.Replace(string(pdnsIdentityFixture()), "FragmentPath=/usr/lib/systemd/system/pdns.service", "FragmentPath=/etc/systemd/system/pdns.service", 1)),
		[]byte(strings.Replace(string(pdnsIdentityFixture()), "Transient=no", "Transient=yes", 1)),
		[]byte(strings.Replace(string(pdnsIdentityFixture()), "Names=pdns.service", "Names=pdns.service other.service", 1)),
	} {
		if _, err := ProbePDNSVendorIdentity(context.Background(), pdnsProfile(),
			func(context.Context, string) ([]byte, error) { return bad, nil }); err == nil {
			t.Fatalf("foreign vendor identity accepted: %q", bad)
		}
	}
	calls = 0
	if _, err := ProbePDNSVendorIdentity(context.Background(), pdnsProfile(),
		func(context.Context, string) ([]byte, error) {
			calls++
			if calls == 2 {
				return []byte(strings.Replace(string(pdnsIdentityFixture()), "--guardian=no", "--guardian=yes", 1)), nil
			}
			return pdnsIdentityFixture(), nil
		}); err == nil {
		t.Fatal("changed vendor unit accepted")
	}
	if _, err := SystemdPDNSIdentityRunner(nil, "pdns.service"); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := SystemdPDNSIdentityRunner(context.Background(), "sshd.service"); err == nil {
		t.Fatal("arbitrary unit accepted")
	}
}
