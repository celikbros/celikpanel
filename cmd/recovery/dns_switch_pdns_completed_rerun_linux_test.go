//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Item 5: recover-dns-pdns-fresh-prestart and recover-dns-pdns-target-staged
// answer a re-run after an earlier owner run completed the same request with
// exit 0 and the completed text, exactly as the other three commands do. The
// fresh-prestart command also admits the Agent's release, so its reconciled
// re-run exits 0; for the V4 command that release keeps exit 3.
func TestFreshPrestartAndTargetStagedCompletedRerunExitZero(t *testing.T) {
	id := strings.Repeat("e", 32)
	completed := fmt.Errorf("%w: request %s; inspect native PowerDNS before treating current service as recovered",
		errPDNSInverseTerminalLedgerObserved, id)
	for _, tc := range []struct {
		name     string
		dispatch func(error, *bytes.Buffer, *bytes.Buffer) int
		lang     string
	}{
		{"fresh-prestart", func(err error, out, diagnostic *bytes.Buffer) int {
			return dispatchOwnerPDNSFreshPrestartV3([]string{ownerPDNSFreshPrestartV3Command, "--request-id", id}, 0,
				func(context.Context, string) error { return err }, out, diagnostic)
		}, "en"},
		{"target-staged-en", func(err error, out, diagnostic *bytes.Buffer) int {
			return dispatchOwnerPDNSTargetInverseV4([]string{ownerPDNSTargetInverseV4Command, "--request-id", id}, 0,
				func(context.Context, string) error { return err }, out, diagnostic)
		}, "en"},
		{"target-staged-tr", func(err error, out, diagnostic *bytes.Buffer) int {
			return dispatchOwnerPDNSTargetInverseV4([]string{ownerPDNSTargetInverseV4Command, "--request-id", id, "--lang", "tr"}, 0,
				func(context.Context, string) error { return err }, out, diagnostic)
		}, "tr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			if got := tc.dispatch(completed, &out, &diagnostic); got != exitOK || diagnostic.Len() != 0 {
				t.Fatalf("completed re-run exit=%d diagnostic=%q", got, diagnostic.String())
			}
			want := terminalDNSInverseVerdictText(tc.lang, id)
			if strings.TrimSpace(out.String()) != want {
				t.Fatalf("completed text = %q, want %q", out.String(), want)
			}
			health := "does not check current DNS health"
			if tc.lang == "tr" {
				health = "DNS'in şu anki sağlığını kontrol etmez"
			}
			if !strings.Contains(out.String(), health) {
				t.Fatalf("completed text does not disclaim current health: %q", out.String())
			}
			refusedErrors := []error{
				errors.New("exact v3 request journal is absent or owned by another operation"),
				errors.New("unknown native proof"),
			}
			if tc.name == "fresh-prestart" {
				// recover-dns-pdns-fresh-prestart admits the Agent's release, so
				// its reconciled re-run is complete (exit 0), as for the V1/V2
				// owner inverses.
				out.Reset()
				diagnostic.Reset()
				if got := tc.dispatch(releasedDNSInverseReconciledOutcome(id), &out, &diagnostic); got != exitOK ||
					diagnostic.Len() != 0 || strings.TrimSpace(out.String()) != releasedDNSInverseReconciledText(tc.lang, id) {
					t.Fatalf("reconciled release re-run exit=%d out=%q diagnostic=%q", got, out.String(), diagnostic.String())
				}
			} else {
				refusedErrors = append(refusedErrors, releasedDNSInverseReconciledOutcome(id))
			}
			for _, refused := range refusedErrors {
				out.Reset()
				diagnostic.Reset()
				if got := tc.dispatch(refused, &out, &diagnostic); got != exitUnavailable || out.Len() != 0 ||
					!strings.Contains(diagnostic.String(), "dns-switch-status --quiesced --request-id "+id) {
					t.Fatalf("%v: exit=%d out=%q diagnostic=%q", refused, got, out.String(), diagnostic.String())
				}
			}
			out.Reset()
			diagnostic.Reset()
			if got := tc.dispatch(nil, &out, &diagnostic); got != exitOK || out.Len() == 0 {
				t.Fatalf("successful run exit=%d out=%q", got, out.String())
			}
		})
	}
}

// A fresh-prestart run interrupted after its verdict and before journal
// retirement leaves a rolled-back journal beside its own terminal verdict:
// the re-run keeps that verdict (no second publication) and retires the
// journal. The Agent's deliberate release is kept the same way; any other
// terminal result is refused before retirement.
func TestFreshPrestartVerdictKeepsOnlyItsOwnTerminalResult(t *testing.T) {
	base := rolledBackTerminal(ownerGuidanceFreshPrestartEvidence())
	for _, tc := range []struct {
		name       string
		evidence   func() dnsenginerecovery.SwitchEvidence
		wantErr    bool
		wantPublis bool
	}{
		{"active job publishes", func() dnsenginerecovery.SwitchEvidence {
			e := base
			e.Observation.Status = dnsenginerecovery.EvidenceActive
			return e
		}, false, true},
		{"own terminal verdict kept", func() dnsenginerecovery.SwitchEvidence {
			e := base
			e.AcceptedJob = ownerRecoveryVerdictTestJob()
			return e
		}, false, false},
		{"agent release kept without publication", func() dnsenginerecovery.SwitchEvidence {
			e := ownerGuidanceReleased(base, dnsengineartifact.ReleasedNativeUnknownCode)
			e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
			return e
		}, false, false},
		{"agent rolled-back verdict refused", func() dnsenginerecovery.SwitchEvidence {
			e := ownerGuidanceReleased(base, dnsengineartifact.ReleasedNativeUnknownCode)
			e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
			e.AcceptedJob.ErrorCode = "dns_engine_switch_rolled_back_after_restart"
			e.Observation.ReleaseReason = e.AcceptedJob.ErrorCode
			return e
		}, true, false},
		{"terminal before checkpoint refused", func() dnsenginerecovery.SwitchEvidence {
			e := base
			e.AcceptedJob = ownerRecoveryVerdictTestJob()
			e.Journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
			return e
		}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			published := 0
			err := publishFreshPDNSPrestartVerdictV3(tc.evidence(), func() error { published++; return nil })
			if (err != nil) != tc.wantErr || (published == 1) != tc.wantPublis || published > 1 {
				t.Fatalf("err=%v published=%d", err, published)
			}
		})
	}
}
