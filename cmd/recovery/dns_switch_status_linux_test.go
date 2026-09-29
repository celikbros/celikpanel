//go:build linux

package main

import (
	"bytes"
	"errors"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	// The fresh prestart command's worker exclusion admits an active job (or
	// its own terminal verdict beside a rolled-back journal); it refuses a
	// lease the Agent released after restart.
	return dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		Status: dnsenginerecovery.EvidenceActive, RequestID: j.MutationRequestID, Phase: j.Phase,
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
	// The restarted Agent's deliberate release keeps the rolling-back journal
	// admitted by recover-dns-bind-switch, so status names the command.
	requireOwnerGuidance(t, ownerGuidanceReleased(ownerGuidanceBINDSwitchEvidence(), dnsengineartifact.ReleasedNativeUnknownCode), ownerBINDSwitchInverseCommand)
	// A release for any other reason is not admitted and names no command.
	requireNoOwnerGuidance(t, ownerGuidanceReleased(ownerGuidanceBINDSwitchEvidence(), dnsengineartifact.ReleasedHostWindowCode))
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
	requireOwnerGuidance(t, ownerGuidanceReleased(ownerGuidancePDNSAdoptionEvidence(), dnsengineartifact.ReleasedNativeUnknownCode), ownerPDNSAdoptionInverseCommand)
	requireNoOwnerGuidance(t, ownerGuidanceReleased(ownerGuidancePDNSAdoptionEvidence(), dnsengineartifact.ReleasedUnsupportedHostCode))
	e = ownerGuidancePDNSAdoptionEvidence()
	e.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	requireNoOwnerGuidance(t, e)
}

// ownerGuidanceReleased turns guidance evidence into the Agent's lease release
// with the given reason, bound to the journal's exact operation identity.
func ownerGuidanceReleased(e dnsenginerecovery.SwitchEvidence, code string) dnsenginerecovery.SwitchEvidence {
	e.Journal.MutationOwnerID = strings.Repeat("b", 32)
	e.Journal.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	e.Observation.Status = dnsenginerecovery.EvidenceReleasedUndecided
	e.Observation.ReleaseReason = code
	e.AcceptedJob = transport.ServiceMutationJob{
		RequestID: e.Journal.MutationRequestID, OwnerID: e.Journal.MutationOwnerID,
		Kind: "dns_engine_switch", Target: string(e.Journal.TargetEngine), PackageName: e.Journal.ManifestQualifier,
		Status: servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now, FinishedAt: now, DeadlineAt: now.Add(time.Hour),
		ErrorCode: code, ErrorMessage: "The interrupted DNS switch could not be verified after the Agent restarted.",
	}
	return e
}

// The secured reader, not a hand-built observation, decides the naming: each
// command's retained journal beside the Agent's deliberate release is named,
// and the same journal beside another release reason is not.
func TestDNSSwitchStatusNamesOwnerCommandForAgentReleaseOnDisk(t *testing.T) {
	for _, tc := range releasedOwnerInverseCases() {
		for _, code := range []string{dnsengineartifact.ReleasedNativeUnknownCode, dnsengineartifact.ReleasedHostWindowCode} {
			t.Run(tc.name+"/"+code, func(t *testing.T) {
				h := newReleasedInverseHost(t)
				j := tc.journal(t, h)
				h.stage(t, j, code)
				evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(h.root, h.owner, h.policy, time.Now().UTC())
				if err != nil || !present {
					t.Fatalf("released evidence unreadable: %v", err)
				}
				want := ""
				if code == dnsengineartifact.ReleasedNativeUnknownCode {
					want = tc.name
				}
				if got := ownerDNSRecoveryCommand(evidence); got != want {
					t.Fatalf("status named %q, want %q", got, want)
				}
			})
		}
	}
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
	// The command admits the Agent's deliberate release of this exact
	// pre-start request at every pre-start phase (the Agent may release
	// before any rollback decision), and no other release reason.
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseTargetEnableIntent,
		dnsengineartifact.SwitchPhaseRollingBack, dnsengineartifact.SwitchPhaseRollingBackTargetEnable} {
		e = ownerGuidanceReleased(ownerGuidanceFreshPrestartEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
		e.Journal.Phase, e.Observation.Phase = phase, phase
		requireOwnerGuidance(t, e, ownerPDNSFreshPrestartV3Command)
	}
	for _, code := range []string{dnsengineartifact.ReleasedHostWindowCode, dnsengineartifact.ReleasedUnsupportedHostCode} {
		requireNoOwnerGuidance(t, ownerGuidanceReleased(ownerGuidanceFreshPrestartEvidence(), code))
	}
	foreignRelease := ownerGuidanceReleased(ownerGuidanceFreshPrestartEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
	foreignRelease.AcceptedJob.OwnerID = strings.Repeat("e", 32)
	requireNoOwnerGuidance(t, foreignRelease)
	startedRelease := ownerGuidanceReleased(ownerGuidanceFreshPrestartEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
	startedRelease.Journal.Phase, startedRelease.Observation.Phase = dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetStarted
	requireNoOwnerGuidance(t, startedRelease)
	// Beside a rolled-back journal the command's own terminal verdict and the
	// Agent's release classified terminal are admitted; nothing else.
	e = ownerGuidanceFreshPrestartEvidence()
	e.Journal.Phase, e.Observation.Phase = dnsengineartifact.SwitchPhaseRolledBack, dnsengineartifact.SwitchPhaseRolledBack
	e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	e.AcceptedJob = ownerRecoveryVerdictTestJob()
	requireOwnerGuidance(t, e, ownerPDNSFreshPrestartV3Command)
	e = ownerGuidanceReleased(e, dnsengineartifact.ReleasedNativeUnknownCode)
	e.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	requireOwnerGuidance(t, e, ownerPDNSFreshPrestartV3Command)
	e.AcceptedJob.ErrorCode = "dns_engine_switch_rolled_back_after_restart"
	e.Observation.ReleaseReason = e.AcceptedJob.ErrorCode
	requireNoOwnerGuidance(t, e)
}

func ownerRecoveryVerdictTestJob() transport.ServiceMutationJob {
	return transport.ServiceMutationJob{
		Status: servicemutationledger.StatusFailed, Phase: "interrupted",
		ErrorCode:    "dns_engine_switch_rolled_back_by_owner_recovery",
		ErrorMessage: "The interrupted DNS engine switch was rolled back to the verified previous state.",
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

func TestDNSSwitchStatusReleasedTextPointsToAdmittedOwnerCommand(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence dnsenginerecovery.SwitchEvidence
	}{
		{ownerBINDSwitchInverseCommand, ownerGuidanceBINDSwitchEvidence()},
		{ownerPDNSAdoptionInverseCommand, ownerGuidancePDNSAdoptionEvidence()},
	} {
		text, known := releasedDNSSwitchGuidance(ownerGuidanceReleased(tc.evidence, dnsengineartifact.ReleasedNativeUnknownCode))
		if !known || !strings.Contains(text, "owner recovery command named above") ||
			strings.Contains(text, "restart the Agent") {
			t.Fatalf("%s: deliberate release text does not point to the named command: %q", tc.name, text)
		}
		// Item 3b (2026-09-30): the Agent retries a released V1 PowerDNS
		// adoption by itself at its next start, so that text names the Agent
		// first and the owner command as the alternative; the V2 BIND switch,
		// which the Agent never runs, names only the command.
		agentRetries := strings.Contains(text, "retries the same rollback by itself")
		if agentRetries != (tc.name == ownerPDNSAdoptionInverseCommand) {
			t.Fatalf("%s: Agent retry named=%v: %q", tc.name, agentRetries, text)
		}
		text, known = releasedDNSSwitchGuidance(ownerGuidanceReleased(tc.evidence, dnsengineartifact.ReleasedHostWindowCode))
		if !known || !strings.Contains(text, "recovery window") || strings.Contains(text, "named above") {
			t.Fatalf("%s: host-window release text changed: %q", tc.name, text)
		}
	}
	// A deliberate release of a V3 pre-start journal: the owner command
	// admits it and the Agent retries its own pre-start undo on restart.
	fresh := ownerGuidanceReleased(ownerGuidanceFreshPrestartEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
	if text, known := releasedDNSSwitchGuidance(fresh); !known || !strings.Contains(text, "named above") ||
		!strings.Contains(text, "If PowerDNS never started") ||
		!strings.Contains(text, "restarting the Agent also retries the same automatic undo") ||
		strings.Contains(text, "another restart will not finish it") {
		t.Fatalf("fresh prestart release text does not name the command and the Agent retry: %q", text)
	}
	// A V1 journal without an admitting command keeps the Agent-restart text.
	v1 := ownerGuidanceReleased(ownerGuidancePDNSAdoptionEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
	v1.Journal.Phase, v1.Observation.Phase = dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetStarted
	if text, known := releasedDNSSwitchGuidance(v1); !known || !strings.Contains(text, "restart the Agent to retry") {
		t.Fatalf("V1 release lost its Agent-restart text: %q", text)
	}
	unknown := ownerGuidanceReleased(ownerGuidanceBINDSwitchEvidence(), "other_release")
	if _, known := releasedDNSSwitchGuidance(unknown); known {
		t.Fatal("unknown release reason was reported as known")
	}
}
