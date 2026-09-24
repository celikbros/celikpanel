//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func bindIdentityFixture(names, executable, argv string) []byte {
	return []byte("Id=named.service\nNames=" + names +
		"\nFragmentPath=/usr/lib/systemd/system/named.service\n" +
		"DropInPaths=\nSourcePath=\nTransient=no\n" +
		"ExecStart={ path=" + executable + " ; argv[]=" + argv +
		" ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\n")
}

func TestProbeBINDVendorIdentityChecksAliasAndDrift(t *testing.T) {
	profile := hostplatform.Profile{DistroFamily: hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT, ServiceManager: hostplatform.ServiceManagerSystemd}
	withAlias := bindIdentityFixture("named.service bind9.service", "/usr/sbin/named", "/usr/sbin/named -f $OPTIONS")
	withoutAlias := bindIdentityFixture("named.service", "/usr/sbin/named", "/usr/sbin/named -f $OPTIONS")
	calls := 0
	got, err := ProbeBINDVendorIdentity(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		calls++
		if name != "named.service" && name != "bind9.service" {
			t.Fatalf("unexpected unit %s", name)
		}
		return withAlias, nil
	})
	if err != nil || got.ID != "named.service" || calls != 4 {
		t.Fatalf("alias observation: %+v %v calls=%d", got, err, calls)
	}
	if _, err := ProbeBINDVendorIdentity(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		if name == "bind9.service" {
			return withoutAlias, nil
		}
		return withAlias, nil
	}); err == nil {
		t.Fatal("divergent alias identity accepted")
	}
	namedCalls := 0
	if _, err := ProbeBINDVendorIdentity(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		if name != "named.service" && name != "bind9.service" {
			t.Fatalf("unexpected unit %s", name)
		}
		if name == "named.service" {
			namedCalls++
			if namedCalls == 2 {
				return withoutAlias, nil
			}
		}
		return withAlias, nil
	}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("identity drift accepted or misclassified: %v", err)
	}
}

func TestProbeBINDVendorIdentityChecksPacmanAndUnknown(t *testing.T) {
	profile := hostplatform.Profile{DistroFamily: hostplatform.DistroFamilyArch,
		PackageManager: hostplatform.PackageManagerPacman, ServiceManager: hostplatform.ServiceManagerSystemd}
	pacman := bindIdentityFixture("named.service", "/usr/bin/named", "/usr/bin/named -f -u named")
	if _, err := ProbeBINDVendorIdentity(context.Background(), profile, func(_ context.Context, name string) ([]byte, error) {
		if name != "named.service" {
			t.Fatalf("unexpected pacman alias %s", name)
		}
		return pacman, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeBINDVendorIdentity(context.Background(), profile, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("systemd unavailable")
	}); err == nil {
		t.Fatal("failed systemd query accepted")
	}
	if _, err := ProbeBINDVendorIdentity(context.Background(), profile, func(context.Context, string) ([]byte, error) {
		return bindIdentityFixture("named.service", "/evil/named", "/evil/named -f -u named"), nil
	}); err == nil {
		t.Fatal("foreign executable accepted")
	}
	if _, err := SystemdBINDIdentityRunner(context.Background(), "sshd.service"); err == nil {
		t.Fatal("arbitrary systemd unit accepted")
	}
	if _, err := SystemdBINDIdentityRunner(nil, "named.service"); err == nil {
		t.Fatal("nil context accepted")
	}
}
