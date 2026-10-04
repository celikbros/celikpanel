package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// Item 3c: the Agent's V3 refusal named recover-dns-pdns-fresh-prestart as the
// next step even where that command refuses (the same native proofs the Agent
// just failed, a started-and-failed unit, a degenerate journal). The command
// is now named only as what dns-switch-status offers once its own admission
// accepts the journal, and the enable phase no longer claims PowerDNS never
// started.
func TestFreshPrimaryV3GuidanceNamesCommandOnlyThroughItsAdmission(t *testing.T) {
	id := strings.Repeat("c", 32)
	status := "dns-switch-status --quiesced --request-id " + id
	staged := classifyFreshPrimaryV3RecoveryError(id, true, "not running", errors.New("listener"),
		dnsengineartifact.SwitchPhaseTargetStaged)
	enabling := classifyFreshPrimaryV3RecoveryError(id, true, "failed", errors.New("unit failed"),
		dnsengineartifact.SwitchPhaseTargetEnableIntent)
	for name, tc := range map[string]struct {
		err      error
		contains []string
		excludes []string
	}{
		"staged": {staged, []string{"interrupted before PowerDNS started", status,
			"Only when that check names it, /usr/libexec/celikpanel/recovery recover-dns-pdns-fresh-prestart --request-id " + id,
			"proves again that PowerDNS never started"}, nil},
		"enable-failed": {enabling, []string{"interrupted while PowerDNS was being enabled",
			"only when it can prove PowerDNS never started", "failed state", status, "Only when that check names it"},
			[]string{"interrupted before PowerDNS started", "is not running on this server now"}},
	} {
		t.Run(name, func(t *testing.T) {
			message := releasedDNSSwitchUnknownMessage(tc.err)
			for _, want := range tc.contains {
				if !strings.Contains(message, want) {
					t.Fatalf("message lacks %q:\n%s", want, message)
				}
			}
			for _, unwanted := range tc.excludes {
				if strings.Contains(message, unwanted) {
					t.Fatalf("message contains %q:\n%s", unwanted, message)
				}
			}
		})
	}

	// A V3 journal outside the command's own journal shape (two target
	// units) is never pointed at the command.
	degenerate := dnsEngineSwitchJournal{
		Schema: dnsengineartifact.SwitchJournalSchemaV3, Phase: dnsengineartifact.SwitchPhaseTargetStaged,
		MutationRequestID: id,
		PDNSFreshPlan:     &dnsengineartifact.PDNSFreshPrimaryPlanV3{Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}},
		TargetUnitsBefore: []dnsUnitSnapshot{{Name: "pdns.service"}, {Name: "pdns.service"}},
	}
	if text := freshPrimaryPrestartRefusalV3(degenerate, "x").Error(); strings.Contains(text, "recover-dns-pdns-fresh-prestart") ||
		!strings.Contains(text, "no owner recovery command applies") {
		t.Fatalf("degenerate V3 journal refusal = %q", text)
	}
	exact := degenerate
	exact.TargetUnitsBefore = exact.TargetUnitsBefore[:1]
	if text := freshPrimaryPrestartRefusalV3(exact, "x").Error(); !strings.Contains(text, "when it names /usr/libexec/celikpanel/recovery recover-dns-pdns-fresh-prestart --request-id "+id) {
		t.Fatalf("pre-start V3 refusal = %q", text)
	}
}
