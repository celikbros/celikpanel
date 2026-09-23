package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDNSApplicationAdmissionIsReadOnlyOwnerScoped(t *testing.T) {
	calls := 0
	report := ""
	check := func(bin, root string) error {
		calls++
		if bin != "/release/bin" || root != "/var/lib/celikpanel-agent-private" {
			t.Fatal(bin, root)
		}
		return errors.New("unsupported DNS format")
	}
	args := []string{"verify-dns-application", "--bin", "/release/bin", "--state-root", "/var/lib/celikpanel-agent-private"}
	if got := dispatchDNSApplicationCompatibility(args, 1, check, func(s string) { report = s }); got != exitNotOwner || calls != 0 {
		t.Fatal(got, calls)
	}
	for _, invalid := range [][]string{nil, {"verify-dns-application"}, {"verify-dns-application", "--bin", "relative", "--state-root", "/state"}, {"verify-dns-application", "--bin", "/release/bin", "--state-root", "/state/../state"}} {
		if got := dispatchDNSApplicationCompatibility(invalid, 0, check, func(s string) { report = s }); got != exitUsage || calls != 0 {
			t.Fatal(got, calls)
		}
	}
	if got := dispatchDNSApplicationCompatibility(args, 0, check, func(s string) { report = s }); got != exitUnavailable || calls != 1 || !strings.Contains(report, "preserve the DNS files") {
		t.Fatal(got, calls, report)
	}
	if got := dispatchDNSApplicationCompatibility(args, 0, func(string, string) error { return nil }, func(string) { t.Fatal("unexpected error") }); got != exitOK {
		t.Fatal(got)
	}
}
