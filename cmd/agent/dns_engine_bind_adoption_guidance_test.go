package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestV2RunningBINDAdoptionRefusalNamesOwnerRecoveryOnlyAfterRollbackDecision(t *testing.T) {
	journal := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV2,
		MutationRequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{
			SourceBIND: &dnsengineartifact.BINDAdoptionSourceProofV1{},
		},
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	err := rollbackDNSSwitchJournal(context.Background(), journal)
	if err == nil || !strings.Contains(err.Error(), "recover-dns-bind-adoption --request-id "+journal.MutationRequestID) {
		t.Fatalf("durable rollback lacked exact owner action: %v", err)
	}
	// The Agent then releases its lease; the status command, which applies
	// the command's own admission to the released ledger, gives the answer.
	if !strings.Contains(err.Error(), "when recovery dns-switch-status --quiesced --request-id "+journal.MutationRequestID+" names it") ||
		strings.Contains(err.Error(), "must run") {
		t.Fatalf("running BIND adoption refusal lacks its status confirmation step: %v", err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseTargetStaged
	err = rollbackDNSSwitchJournal(context.Background(), journal)
	if err == nil || !strings.Contains(err.Error(), "no durable rollback decision") || strings.Contains(err.Error(), "recover-dns-bind-adoption") {
		t.Fatalf("predecision adoption advertised unsafe inverse: %v", err)
	}
}

const agentOwnerGuidanceRequest = "0123456789abcdef0123456789abcdef"

func requireAgentOwnerCommand(t *testing.T, err error, command string) {
	t.Helper()
	if err == nil ||
		!strings.Contains(err.Error(), "/usr/libexec/celikpanel/recovery "+command+" --request-id "+agentOwnerGuidanceRequest) ||
		strings.Contains(err.Error(), "no owner recovery command applies") {
		t.Fatalf("refusal lacks exact owner command %s: %v", command, err)
	}
}

func requireAgentNoOwnerCommand(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "no owner recovery command applies") ||
		!strings.Contains(err.Error(), "contact support with request id "+agentOwnerGuidanceRequest) ||
		strings.Contains(err.Error(), "/usr/libexec/celikpanel/recovery recover-") {
		t.Fatalf("unadmitted journal lacks explicit no-command guidance: %v", err)
	}
}

func TestV2BINDSwitchRefusalNamesOwnerRecoveryCommand(t *testing.T) {
	journal := dnsEngineSwitchJournal{
		Schema: dnsengineartifact.SwitchJournalSchemaV2,
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{
			Kind:                dnsengineartifact.BINDSwitchInversePlanKindV2,
			SourcePDNS:          &dnsengineartifact.PDNSSourceProofV2{Kind: dnsengineartifact.PDNSSourceProofKindV1},
			BINDUnchangedConfig: []dnsengineartifact.FileSnapshot{{Path: "/etc/bind/named.conf", Exists: true}, {Path: "/etc/bind/named.conf.default-zones", Exists: true}},
		},
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: agentOwnerGuidanceRequest,
		SourceEngine:      transport.DNSEnginePowerDNS,
		TargetEngine:      transport.DNSEngineBIND,
		Topology:          transport.DNSTopologyStandalone,
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "active"}},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "named.service", ActiveState: "inactive"},
			{Name: "bind9.service", ActiveState: "inactive"},
		},
	}
	err := rollbackDNSSwitchJournal(context.Background(), journal)
	requireAgentOwnerCommand(t, err, "recover-dns-bind-switch")
	if !strings.Contains(err.Error(), "dns-switch-status --quiesced --request-id "+agentOwnerGuidanceRequest) ||
		!strings.Contains(err.Error(), "independent inverse adapter") {
		t.Fatalf("BIND switch refusal lacks its status confirmation step: %v", err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	requireAgentOwnerCommand(t, rollbackDNSSwitchJournal(context.Background(), journal), "recover-dns-bind-switch")
	journal.Topology = transport.DNSTopologyPaired
	requireAgentNoOwnerCommand(t, rollbackDNSSwitchJournal(context.Background(), journal))
}

func TestV3FreshPrimaryRefusalNamesPrestartOwnerCommand(t *testing.T) {
	journal := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV3,
		Phase:             dnsengineartifact.SwitchPhaseTargetStaged,
		MutationRequestID: agentOwnerGuidanceRequest,
		TargetEngine:      transport.DNSEnginePowerDNS,
		PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{
			Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{},
		},
	}
	requireAgentOwnerCommand(t, rollbackDNSSwitchJournal(context.Background(), journal), "recover-dns-pdns-fresh-prestart")
	err := freshPrimaryPrestartRefusalV3(journal, "v3 phase has no poststart forward authority")
	requireAgentOwnerCommand(t, err, "recover-dns-pdns-fresh-prestart")
	if !strings.HasPrefix(err.Error(), "v3 phase has no poststart forward authority; if PowerDNS never started") {
		t.Fatalf("forward refusal lost its reason: %v", err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	requireAgentNoOwnerCommand(t, rollbackDNSSwitchJournal(context.Background(), journal))
	journal.Phase = dnsengineartifact.SwitchPhaseTargetStaged
	journal.PDNSFreshPlan.Candidate = nil
	requireAgentNoOwnerCommand(t, freshPrimaryPrestartRefusalV3(journal, "v3 phase has no poststart forward authority"))
}

func TestPDNSAdoptionRollbackFailureNamesOwnerRecoveryCommand(t *testing.T) {
	journal := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV1,
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeAdopt,
		MutationRequestID: agentOwnerGuidanceRequest,
		TargetEngine:      transport.DNSEnginePowerDNS,
	}
	cause := errors.New("native PowerDNS source differs")
	err := withPDNSAdoptionOwnerRecovery(dnsenginerecovery.NativeInversePDNSAdoption, journal, cause)
	requireAgentOwnerCommand(t, err, "recover-dns-pdns-adoption")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "dns-switch-status --quiesced --request-id "+agentOwnerGuidanceRequest) {
		t.Fatalf("adoption guidance lost its cause or status step: %v", err)
	}
	if got := withPDNSAdoptionOwnerRecovery(dnsenginerecovery.NativeInversePDNSSwitch, journal, cause); got != cause {
		t.Fatalf("non-adoption rollback error was rewritten: %v", got)
	}
	if got := withPDNSAdoptionOwnerRecovery(dnsenginerecovery.NativeInversePDNSAdoption, journal, nil); got != nil {
		t.Fatalf("successful adoption rollback gained an error: %v", got)
	}
	journal.StateBefore.Exists = true
	requireAgentNoOwnerCommand(t, withPDNSAdoptionOwnerRecovery(dnsenginerecovery.NativeInversePDNSAdoption, journal, cause))
}
