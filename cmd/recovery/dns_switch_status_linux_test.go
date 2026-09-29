//go:build linux

package main

import (
	"bytes"
	"errors"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSSwitchStatusRequiresOwnerAndExactCommand(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if got := runDNSSwitchStatus([]string{"dns-switch-status", "extra"}, 0, &out, &diagnostic); got != exitUsage {
		t.Fatalf("unexpected argument was accepted: %d", got)
	}
	if got := runDNSSwitchStatus([]string{"dns-switch-status"}, 1000, &out, &diagnostic); got != exitNotOwner {
		t.Fatalf("unprivileged observation was accepted: %d", got)
	}
	if got := runDNSSwitchStatus([]string{"dns-switch-status", "--quiesced"}, 1000, &out, &diagnostic); got != exitNotOwner {
		t.Fatalf("unprivileged quiesced observation was accepted: %d", got)
	}
	if !strings.Contains(diagnostic.String(), "Owner authentication") || out.Len() != 0 {
		t.Fatalf("unexpected guidance or data disclosure: %q / %q", diagnostic.String(), out.String())
	}
}
func TestLocalCelikPanelGroupIsBoundedAndUnambiguous(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "group")
	write := func(data string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("root:x:0:\ncelikpanel:x:975:\n", 0o644)
	if got, err := localCelikPanelGroupID(path); err != nil || got != 975 {
		t.Fatalf("local identity: %d, %v", got, err)
	}
	for _, data := range []string{
		"root:x:0:\n",
		"celikpanel:x:0:\n",
		"celikpanel:x:not-a-number:\n",
		"celikpanel:x:975:\ncelikpanel:x:976:\n",
	} {
		write(data, 0o644)
		if _, err := localCelikPanelGroupID(path); err == nil {
			t.Fatalf("ambiguous or missing group accepted: %q", data)
		}
	}
	write("celikpanel:x:975:\n", 0o666)
	if _, err := localCelikPanelGroupID(path); err == nil {
		t.Fatal("writable group file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("celikpanel:x:975:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := localCelikPanelGroupID(path); err == nil {
		t.Fatal("symlink group file accepted")
	}
}
func TestDNSObservationLocksKeepReleaseThenHostAndReleaseOnFailure(t *testing.T) {
	root := t.TempDir()
	makeLock := func(name string) string {
		t.Helper()
		parent := filepath.Join(root, name)
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(parent, "transaction.lock")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	releasePath, hostPath := makeLock("release"), makeLock("host")
	locks, err := acquireDNSObservationLocks(releasePath, hostPath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostmutationlock.AcquireExisting(hostPath, hostmutationlock.Owner{}); !errors.Is(err, hostmutationlock.ErrBusy) {
		t.Fatalf("host lock was not retained: %v", err)
	}
	locks.Close()

	heldHost, err := hostmutationlock.AcquireExisting(hostPath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireDNSObservationLocks(releasePath, hostPath, hostmutationlock.Owner{}); !errors.Is(err, hostmutationlock.ErrBusy) {
		t.Fatalf("busy host was not refused: %v", err)
	}
	// The failed second acquisition must release the first lease.
	releaseAfterFailure, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatalf("release lease leaked: %v", err)
	}
	_ = releaseAfterFailure.Close()
	_ = heldHost.Close()

	heldRelease, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireDNSObservationLocks(releasePath, hostPath, hostmutationlock.Owner{}); !errors.Is(err, hostmutationlock.ErrBusy) {
		t.Fatalf("busy release was not refused: %v", err)
	}
	_ = heldRelease.Close()
}

func TestLocalBINDGroupRejectsNoncanonicalAndDuplicateRecords(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned group fixture requires root")
	}
	path := filepath.Join(t.TempDir(), "group")
	check := func(data string, wantOK bool) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		gid, err := localServiceGroupID(path, "bind")
		if wantOK && (err != nil || gid != 1234) {
			t.Fatalf("canonical BIND group rejected: %d %v", gid, err)
		}
		if !wantOK && err == nil {
			t.Fatalf("unsafe BIND group accepted: %q", data)
		}
	}
	check("bind:x:1234:\n", true)
	check("bind:x:1234:\nbind:x:1235:\n", false)
	check("bind:x:01234:\n", false)
	check("bind:x:1234:someone\n", false)
	check("bind:x:1234:\nbind:malformed\n", false)
}

func TestPDNSTargetV4OwnerRecoveryHintIsPhaseAndStatusBounded(t *testing.T) {
	e := dnsenginerecovery.SwitchEvidence{
		Journal: dnsengineartifact.SwitchJournalV1{
			Schema:            dnsengineartifact.SwitchJournalSchemaV4,
			Phase:             dnsengineartifact.SwitchPhaseTargetEnableIntent,
			MutationRequestID: strings.Repeat("a", 32),
			PDNSTargetPlan: &dnsengineartifact.PDNSTargetInversePlanV4{
				Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{},
			},
		},
		Observation: dnsenginerecovery.EvidenceObservation{
			RequestID:   strings.Repeat("a", 32),
			Phase:       dnsengineartifact.SwitchPhaseTargetEnableIntent,
			Status:      dnsenginerecovery.EvidenceActive,
			InverseKind: dnsenginerecovery.NativeInversePDNSSwitch,
		},
	}
	if !pdnsTargetV4OwnerRecoveryCandidate(e) {
		t.Fatal("exact interrupted pre-start request lacks owner guidance")
	}
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseCommitted} {
		e.Journal.Phase, e.Observation.Phase = phase, phase
		if pdnsTargetV4OwnerRecoveryCandidate(e) {
			t.Fatalf("post-start phase %s offered pre-start inverse", phase)
		}
	}
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable, dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	e.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	if pdnsTargetV4OwnerRecoveryCandidate(e) {
		t.Fatal("released-undecided job offered unsupported inverse")
	}
}

const ownerGuidanceRequest = "0123456789abcdef0123456789abcdef"

func ownerGuidanceBINDSwitchEvidence() dnsenginerecovery.SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV2,
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{
			Kind:                dnsengineartifact.BINDSwitchInversePlanKindV2,
			SourcePDNS:          &dnsengineartifact.PDNSSourceProofV2{Kind: dnsengineartifact.PDNSSourceProofKindV1},
			BINDUnchangedConfig: []dnsengineartifact.FileSnapshot{{Path: "/etc/bind/named.conf", Exists: true}, {Path: "/etc/bind/named.conf.default-zones", Exists: true}},
		},
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: ownerGuidanceRequest,
		SourceEngine:      transport.DNSEnginePowerDNS,
		TargetEngine:      transport.DNSEngineBIND,
		Topology:          transport.DNSTopologyStandalone,
		TargetGeneration:  strings.Repeat("c", 64),
		TargetEpoch:       2,
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "active"}},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "named.service", ActiveState: "inactive"},
			{Name: "bind9.service", ActiveState: "inactive"},
		},
	}
	return dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		EvidenceSHA256: "secured-evidence-fingerprint", Status: dnsenginerecovery.EvidenceActive,
		RequestID: j.MutationRequestID, Phase: j.Phase,
		SourceEngine: string(j.SourceEngine), TargetEngine: string(j.TargetEngine),
		TargetGeneration: j.TargetGeneration, TargetEpoch: j.TargetEpoch,
		InverseKind:     dnsenginerecovery.NativeInverseBINDSwitch,
		SourceOwnership: dnsenginerecovery.SourceOwnershipExact,
		SourceReceipt:   dnsenginerecovery.SourceReceiptDifferent,
		TargetReceipt:   dnsenginerecovery.TargetReceiptExact,
	}}
}

func ownerGuidancePDNSAdoptionEvidence() dnsenginerecovery.SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Schema:            dnsengineartifact.SwitchJournalSchemaV1,
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		Mode:              transport.DNSEngineSwitchModeAdopt,
		MutationRequestID: ownerGuidanceRequest,
		TargetEngine:      transport.DNSEnginePowerDNS,
		TargetEpoch:       1,
	}
	return dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		EvidenceSHA256: "secured-evidence-fingerprint", Status: dnsenginerecovery.EvidenceActive,
		RequestID: j.MutationRequestID, Phase: j.Phase, TargetEngine: string(j.TargetEngine), TargetEpoch: j.TargetEpoch,
		InverseKind:     dnsenginerecovery.NativeInversePDNSAdoption,
		SourceOwnership: dnsenginerecovery.SourceOwnershipNotApplicable,
		SourceReceipt:   dnsenginerecovery.SourceReceiptDifferent,
		TargetReceipt:   dnsenginerecovery.TargetReceiptExact,
	}}
}

func ownerGuidanceFreshPrestartEvidence() dnsenginerecovery.SwitchEvidence {
	j := dnsengineartifact.SwitchJournalV1{
		Schema:            dnsengineartifact.SwitchJournalSchemaV3,
		Phase:             dnsengineartifact.SwitchPhaseTargetStaged,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: ownerGuidanceRequest,
		TargetEngine:      transport.DNSEnginePowerDNS,
		Topology:          transport.DNSTopologyPaired,
		PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{
			Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{},
		},
	}
	// The fresh prestart command admits its exact V3 journal whatever the
	// ledger status, including a lease the Agent released after restart.
	return dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		Status: dnsenginerecovery.EvidenceReleasedUndecided, RequestID: j.MutationRequestID, Phase: j.Phase,
		TargetEngine: string(j.TargetEngine), InverseKind: dnsenginerecovery.NativeInversePDNSSwitch,
	}}
}

func requireOwnerGuidance(t *testing.T, e dnsenginerecovery.SwitchEvidence, command string) {
	t.Helper()
	if got := ownerDNSRecoveryCommand(e); got != command {
		t.Fatalf("owner command = %q, want %q", got, command)
	}
	text := ownerDNSRecoveryGuidance(e, false)
	want := "/usr/libexec/celikpanel/recovery " + command + " --request-id " + ownerGuidanceRequest + "."
	if !strings.Contains(text, want) || !strings.Contains(text, "does not start recovery") ||
		strings.Contains(text, "No owner recovery command applies") {
		t.Fatalf("guidance lacks exact owner command %q:\n%s", want, text)
	}
}

func requireNoOwnerGuidance(t *testing.T, e dnsenginerecovery.SwitchEvidence) {
	t.Helper()
	if got := ownerDNSRecoveryCommand(e); got != "" {
		t.Fatalf("unadmitted evidence named owner command %q", got)
	}
	text := ownerDNSRecoveryGuidance(e, true)
	if !strings.Contains(text, "No owner recovery command applies") ||
		!strings.Contains(text, "contact support with request id "+e.Observation.RequestID+".") ||
		strings.Contains(text, "/usr/libexec/celikpanel/recovery recover-") {
		t.Fatalf("unadmitted evidence lacks explicit no-command guidance:\n%s", text)
	}
}

func TestDNSSwitchStatusNamesBINDSwitchOwnerCommand(t *testing.T) {
	requireOwnerGuidance(t, ownerGuidanceBINDSwitchEvidence(), ownerBINDSwitchInverseCommand)
	e := ownerGuidanceBINDSwitchEvidence()
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseRolledBack, dnsengineartifact.SwitchPhaseRolledBack
	e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	e.Observation.SourceReceipt, e.Observation.TargetReceipt = dnsenginerecovery.SourceReceiptExact, dnsenginerecovery.TargetReceiptDifferent
	requireOwnerGuidance(t, e, ownerBINDSwitchInverseCommand)
	// After the Agent releases the lease, recover-dns-bind-switch does not
	// admit a rolling-back journal, so status must not advertise it.
	e = ownerGuidanceBINDSwitchEvidence()
	e.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	requireNoOwnerGuidance(t, e)
	e = ownerGuidanceBINDSwitchEvidence()
	e.Journal.Topology = transport.DNSTopologyPaired
	requireNoOwnerGuidance(t, e)
}

func TestDNSSwitchStatusNamesPDNSAdoptionOwnerCommand(t *testing.T) {
	requireOwnerGuidance(t, ownerGuidancePDNSAdoptionEvidence(), ownerPDNSAdoptionInverseCommand)
	e := ownerGuidancePDNSAdoptionEvidence()
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseRolledBack, dnsengineartifact.SwitchPhaseRolledBack
	e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	e.Observation.SourceReceipt, e.Observation.TargetReceipt = dnsenginerecovery.SourceReceiptMutualAbsence, dnsenginerecovery.TargetReceiptAbsent
	requireOwnerGuidance(t, e, ownerPDNSAdoptionInverseCommand)
	e = ownerGuidancePDNSAdoptionEvidence()
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetStarted
	requireNoOwnerGuidance(t, e)
	e = ownerGuidancePDNSAdoptionEvidence()
	e.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	requireNoOwnerGuidance(t, e)
}

func TestDNSSwitchStatusNamesFreshPrestartOwnerCommand(t *testing.T) {
	requireOwnerGuidance(t, ownerGuidanceFreshPrestartEvidence(), ownerPDNSFreshPrestartV3Command)
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseRollingBack,
		dnsengineartifact.SwitchPhaseRollingBackTargetEnable, dnsengineartifact.SwitchPhaseRolledBack} {
		e := ownerGuidanceFreshPrestartEvidence()
		e.Journal.Phase, e.Observation.Phase = phase, phase
		requireOwnerGuidance(t, e, ownerPDNSFreshPrestartV3Command)
	}
	e := ownerGuidanceFreshPrestartEvidence()
	e.Journal.PDNSFreshPlan.Candidate = nil
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseIntent
	requireOwnerGuidance(t, e, ownerPDNSFreshPrestartV3Command)
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseTargetStaged
	requireNoOwnerGuidance(t, e)
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted} {
		e := ownerGuidanceFreshPrestartEvidence()
		e.Journal.Phase, e.Observation.Phase = phase, phase
		requireNoOwnerGuidance(t, e)
	}
	e = ownerGuidanceFreshPrestartEvidence()
	e.Journal.PDNSFreshPlan.Native = &pdnsnative.RecordedTransition{}
	requireNoOwnerGuidance(t, e)
	e = ownerGuidanceFreshPrestartEvidence()
	e.Observation.RequestID = strings.Repeat("d", 32)
	if ownerDNSRecoveryCommand(e) != "" {
		t.Fatal("foreign observation named the fresh prestart command")
	}
}

func TestDNSSwitchStatusKeepsExistingOwnerCommandsAndExplicitNoCommand(t *testing.T) {
	v4 := dnsenginerecovery.SwitchEvidence{
		Journal: dnsengineartifact.SwitchJournalV1{
			Schema: dnsengineartifact.SwitchJournalSchemaV4, Phase: dnsengineartifact.SwitchPhaseTargetStaged,
			MutationRequestID: ownerGuidanceRequest,
			PDNSTargetPlan:    &dnsengineartifact.PDNSTargetInversePlanV4{Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}},
		},
		Observation: dnsenginerecovery.EvidenceObservation{
			RequestID: ownerGuidanceRequest, Phase: dnsengineartifact.SwitchPhaseTargetStaged,
			Status: dnsenginerecovery.EvidenceActive, InverseKind: dnsenginerecovery.NativeInversePDNSSwitch,
		},
	}
	if got := ownerDNSRecoveryGuidance(v4, true); !strings.Contains(got, "recover-dns-pdns-target-staged --request-id "+ownerGuidanceRequest+".") {
		t.Fatalf("quiesced V4 guidance lost its command:\n%s", got)
	}
	if got := ownerDNSRecoveryGuidance(v4, false); !strings.Contains(got, "--quiesced --request-id "+ownerGuidanceRequest) ||
		strings.Contains(got, "No owner recovery command applies") {
		t.Fatalf("unquiesced V4 guidance lacks its rerun step:\n%s", got)
	}
	unknown := dnsenginerecovery.SwitchEvidence{
		Journal: dnsengineartifact.SwitchJournalV1{
			Schema: dnsengineartifact.SwitchJournalSchemaV1, Phase: dnsengineartifact.SwitchPhaseIntent,
			Mode: transport.DNSEngineSwitchModeSwitch, MutationRequestID: ownerGuidanceRequest,
			SourceEngine: transport.DNSEngineBIND, TargetEngine: transport.DNSEnginePowerDNS,
		},
		Observation: dnsenginerecovery.EvidenceObservation{
			RequestID: ownerGuidanceRequest, Phase: dnsengineartifact.SwitchPhaseIntent,
			Status: dnsenginerecovery.EvidenceActive, InverseKind: dnsenginerecovery.NativeInversePDNSSwitch,
		},
	}
	requireNoOwnerGuidance(t, unknown)
}
