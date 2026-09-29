//go:build linux

package main

import (
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Batch 8 (2026-09-30) retained the Agent's ledger messages verbatim for c09
// (database edited after start) and c10 (configuration edited before start).
// The changed-thing names now come from dnsenginerecovery, which
// dns-switch-status shares; the messages must stay byte-identical, and the
// status reader must recover the name each one gave.
func TestFreshPrimaryV3OwnerChangeLedgerMessagesPinnedForStatus(t *testing.T) {
	for _, tc := range []struct {
		request, changed, running string
		prestart                  bool
		want                      string
	}{
		{
			request: "c59b537aac13c590e71b4682a88ed7b1", changed: freshPrimaryV3ChangedDatabase, running: "running",
			want: "The first install of PowerDNS as the paired primary found that the PowerDNS database is not as this install wrote it (an administrator edit, PowerDNS behaviour CelikPanel has not measured, or a file it could not read). CelikPanel kept that change and neither continued nor undid the install. The PowerDNS service is running on this server now (systemd); that alone does not prove it answers correctly. The operation's journal and the PowerDNS database are kept and block new DNS changes; other server changes can continue. Next step: the server administrator reviews that change and runs /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id c59b537aac13c590e71b4682a88ed7b1. Restarting the Agent retries the same operation only when the change is back as the install wrote it; otherwise contact support with request id c59b537aac13c590e71b4682a88ed7b1.",
		},
		{
			request: "dbf9982f7d79645b1cc9ddfa99573f41", changed: freshPrimaryV3ChangedConfig, running: "not running", prestart: true,
			want: "The first install of PowerDNS as the paired primary found that the PowerDNS configuration is not as this install wrote it (an administrator edit, PowerDNS behaviour CelikPanel has not measured, or a file it could not read). CelikPanel kept that change and neither continued nor undid the install. The PowerDNS service is not running on this server now, so this server does not answer DNS. The operation's journal and the PowerDNS database are kept and block new DNS changes; other server changes can continue. Next step: the server administrator reviews that change and runs /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id dbf9982f7d79645b1cc9ddfa99573f41. Restarting the Agent retries the same operation only when the change is back as the install wrote it; otherwise contact support with request id dbf9982f7d79645b1cc9ddfa99573f41.",
		},
	} {
		err := classifyFreshPrimaryV3RecoveryError(tc.request, tc.prestart, tc.running,
			freshPrimaryV3Changed(tc.changed, errors.New("owner edit")))
		for _, got := range []string{
			releasedDNSSwitchUnknownMessage(err),
			releasedDNSSwitchInProcessUnknownMessage(&dnsSwitchNativeRecoveryUnknownError{err: err}),
		} {
			if got != tc.want {
				t.Fatalf("ledger message changed:\n%s\nwant:\n%s", got, tc.want)
			}
			if named, ok := dnsenginerecovery.RecordedFreshPrimaryOwnerChangeV3(got); !ok || named != tc.changed {
				t.Fatalf("status reads %q %v from the ledger message, want %q", named, ok, tc.changed)
			}
		}
	}
	// The other fresh-primary texts are not owner changes for the status.
	for _, err := range []error{
		classifyFreshPrimaryV3RecoveryError("c59b537aac13c590e71b4682a88ed7b1", true, "not running", errors.New("listener")),
		classifyFreshPrimaryV3RecoveryError("c59b537aac13c590e71b4682a88ed7b1", false, "running", errors.New("peer")),
	} {
		if named, ok := dnsenginerecovery.RecordedFreshPrimaryOwnerChangeV3(releasedDNSSwitchUnknownMessage(err)); ok {
			t.Fatalf("non-change guidance read as owner change %q", named)
		}
	}
}
