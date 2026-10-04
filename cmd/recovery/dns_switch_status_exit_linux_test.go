//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Batch 7 (source dbd6a6b6): before recovery, `dns-switch-status --quiesced`
// printed "accepted-active ... No owner recovery command applies" for a V1
// first install in both engines, but exited 3 for BIND (c06, c07) and 0 for
// PowerDNS (c08-c10). Both engines now print their Agent-recovered guidance
// instead of that line. The BIND exit came from the unit identity step: the
// strict loaded-unit reader cannot read the install guard's masked
// named.service ("parse named.service identity: systemctl returned incomplete
// DNS unit identity"), so the BIND observation was not classified; PowerDNS
// has no unit identity step before its target receipt is exact. Since
// a751e46a a V1 BIND journal before activation is read with the typed
// never-started observation. These tests pin the rule for both engines: exit
// 0 means the status was read and classified, exit 3 that it was not.

// masked-unit output of the fixed BIND identity query (install guard mask).
func maskedBINDTargetRunner(_ context.Context, name string) ([]byte, error) {
	return []byte("Id=" + name + "\nNames=" + name + "\nLoadState=masked\nUnitFileState=masked\nFragmentPath=/etc/systemd/system/" + name + "\nDropInPaths=\nSourcePath=\nTransient=no\n"), nil
}

// The strict loaded-unit identity reader's result on a masked named.service,
// as batch 7 c06/c07 recorded it.
var errStrictIdentityOnMaskedUnit = errors.New("parse named.service identity: systemctl returned incomplete DNS unit identity")

type statusExitCase struct {
	evidence   dnsenginerecovery.SwitchEvidence
	units      []dnsenginerecovery.NativeUnitObservation
	unitErr    error
	strictUsed int
}

func (c *statusExitCase) readers(t *testing.T) dnsSwitchStatusReaders {
	t.Helper()
	return dnsSwitchStatusReaders{
		groupID:      func() (uint32, error) { return 991, nil },
		stateRoot:    t.TempDir,
		acquireLocks: func(hostmutationlock.Owner) (func(), error) { return func() {}, nil },
		readEvidence: func(string, servicemutationledger.FileOwner, dnsengineartifact.JournalPolicy, time.Time) (dnsenginerecovery.SwitchEvidence, bool, error) {
			return c.evidence, true, nil
		},
		probeUnits: func(_ context.Context, names []string) ([]dnsenginerecovery.NativeUnitObservation, error) {
			if c.unitErr != nil {
				return nil, c.unitErr
			}
			if len(names) != len(c.units) {
				t.Fatalf("probed units %q, fixture has %d", names, len(c.units))
			}
			return append([]dnsenginerecovery.NativeUnitObservation(nil), c.units...), nil
		},
		inspectWorker: func(dnsengineartifact.SwitchIdentity, *transport.ServiceMutationJob, time.Time) (dnsenginerecovery.WorkerExclusion, error) {
			return dnsenginerecovery.WorkerNotRecorded, nil
		},
		bindRoot: func(context.Context) error { return nil },
		bindNeverStarted: func(ctx context.Context, beforeDecision bool) (string, error) {
			return observeNeverStartedBINDTarget(ctx, maskedBINDTargetRunner,
				func(context.Context) error { t.Fatal("loaded identity proof used for a masked target"); return nil },
				func(string) error { return nil }, func() error { return nil }, beforeDecision)
		},
		bindVendorAndUnit: func(context.Context) error {
			c.strictUsed++
			return errStrictIdentityOnMaskedUnit
		},
	}
}

func (c *statusExitCase) run(t *testing.T) (int, string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := runDNSSwitchStatusWith(
		[]string{"dns-switch-status", "--quiesced", "--request-id", ownerGuidanceRequest},
		0, &out, &diagnostic, c.readers(t))
	return code, out.String(), diagnostic.String()
}

func maskedUnits(names ...string) []dnsenginerecovery.NativeUnitObservation {
	units := make([]dnsenginerecovery.NativeUnitObservation, 0, len(names))
	for _, name := range names {
		units = append(units, dnsenginerecovery.NativeUnitObservation{
			Name: name, LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked",
		})
	}
	return units
}

// beforeActivationObservation fills the secured observation of an accepted,
// active first install whose target was not reached: no receipts yet.
func beforeActivationObservation(e dnsenginerecovery.SwitchEvidence, kind dnsenginerecovery.NativeInverseKind, units []string) dnsenginerecovery.SwitchEvidence {
	e.Observation.Status = dnsenginerecovery.EvidenceActive
	e.Observation.InverseKind = kind
	e.Observation.NativeUnits = units
	e.Observation.TargetReceipt = dnsenginerecovery.TargetReceiptAbsent
	e.Observation.SourceReceipt = dnsenginerecovery.SourceReceiptMutualAbsence
	e.Observation.SourceOwnership = dnsenginerecovery.SourceOwnershipNotApplicable
	e.Observation.EvidenceSHA256 = "secured-evidence-fingerprint"
	return e
}

func freshBINDStatusCase(t *testing.T, phase string) *statusExitCase {
	t.Helper()
	e := v1BINDAgentRecoveredEvidence(t, transport.DNSEngineSwitchModeSwitch, "", 1, phase)
	e = beforeActivationObservation(e, dnsenginerecovery.NativeInverseBINDSwitch,
		[]string{"bind9.service", "named.service"})
	return &statusExitCase{evidence: e, units: maskedUnits("bind9.service", "named.service")}
}

func freshPDNSStatusCase(t *testing.T, phase string) *statusExitCase {
	t.Helper()
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Phase: phase,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: ownerGuidanceRequest, TargetEngine: transport.DNSEnginePowerDNS,
		TargetEpoch: 1, SourceRevision: 3, Topology: transport.DNSTopologyStandalone,
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"},
		},
	}
	e := dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		RequestID: j.MutationRequestID, Phase: j.Phase, TargetEngine: string(j.TargetEngine), TargetEpoch: j.TargetEpoch,
	}}
	e = withReconstructedManifest(t, e)
	e = beforeActivationObservation(e, dnsenginerecovery.NativeInversePDNSSwitch, []string{"pdns.service"})
	return &statusExitCase{evidence: e, units: maskedUnits("pdns.service")}
}

// Lines every engine prints for this journal class after its engine lines.
func beforeActivationTail(units ...string) string {
	var b strings.Builder
	b.WriteString("The current DNS state receipt is absent. This alone does not prove that an inverse is safe; preserve the journal and inspect native DNS before recovery.\n")
	b.WriteString("The journal and current state both have no source receipt. This is not proof that DNS was rolled back or is healthy; the server owner must inspect native DNS before resuming this operation.\n")
	b.WriteString("The switch has no previous DNS engine, so no frozen source ownership receipt is required. Native DNS still needs verification before the operation resumes.\n")
	for _, unit := range units {
		fmt.Fprintf(&b, "Native unit %s: load=masked active=inactive unit-file=masked.\n", unit)
	}
	b.WriteString("These systemd properties were observed at one instant; they do not prove DNS answers, zone content, owner edits or recovery authority.\n")
	b.WriteString("Release and host mutation locks were held; the same evidence bytes and native unit properties matched again after native checks, and the accepted worker was rechecked. Future worker liveness, unobserved DNS answers, owner edits and recovery authority remain unproved.\n")
	return b.String()
}

const (
	quiescedNoWorkerLine = "Quiesced worker observation: no-recorded-worker. This is only a point-in-time process check; it does not authorize a DNS inverse.\n"
	acceptedRecordedLine = "The accepted operation is recorded. The server owner should follow its CelikPanel status and check native DNS health if progress stops. This read-only observation does not prove worker liveness or authorize another switch; recovery must recheck the same operation under the host lock.\n"
)

// expectedPDNSBeforeActivation is c08's retained stdout (source dbd6a6b6)
// with its request id replaced and one line changed: batch 7 printed the
// generic "No owner recovery command applies ... contact support with request
// id ..." line, although the restarted Agent then rolled the install back by
// itself and the same request ran forward on retry (batch 7 c08, batch 6a
// c03). That line is now the Agent-recovered first-install guidance; every
// other line is batch 7's.
func expectedPDNSBeforeActivation(phase string) string {
	return "DNS switch request " + ownerGuidanceRequest + ": accepted-active (journal phase " + phase + ").\n" +
		pdnsV1FirstInstallAgentRecoveredGuidance(phase, false, ownerGuidanceRequest) +
		quiescedNoWorkerLine + acceptedRecordedLine +
		"Frozen native inverse shape: pdns-switch. This classification does not prove worker exclusion, owner authority or safe recovery execution.\n" +
		beforeActivationTail("pdns.service")
}

func expectedBINDBeforeActivation(phase string) string {
	return "DNS switch request " + ownerGuidanceRequest + ": accepted-active (journal phase " + phase + ").\n" +
		bindV1AgentRecoveredGuidance(phase, transport.DNSEngineSwitchModeSwitch, true, ownerGuidanceRequest) +
		quiescedNoWorkerLine + acceptedRecordedLine +
		"Managed BIND root directory and package ownership matched on two read-only walks. This does not prove the selected generation, DNS answers, owner edits or recovery authority.\n" +
		"BIND never started for this operation: named.service and bind9.service are under the package guard's persistent mask (root-owned links to /dev/null) and the installed BIND vendor files were stable across two reads. Process state and DNS answers are not proved by this observation.\n" +
		"Frozen native inverse shape: bind-switch. This classification does not prove worker exclusion, owner authority or safe recovery execution.\n" +
		beforeActivationTail("bind9.service", "named.service")
}

func TestDNSSwitchStatusSameJournalClassExitsEquallyForBothEngines(t *testing.T) {
	for _, phase := range []string{dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged} {
		t.Run(phase, func(t *testing.T) {
			bind := freshBINDStatusCase(t, phase)
			pdns := freshPDNSStatusCase(t, phase)
			if !dnsenginerecovery.BINDV1BeforeActivationJournal(bind.evidence.Journal) {
				t.Fatal("the BIND fixture is not a V1 journal before activation")
			}
			for _, e := range []dnsenginerecovery.SwitchEvidence{bind.evidence, pdns.evidence} {
				if ownerDNSRecoveryCommand(e) != "" {
					t.Fatalf("%s fixture admits an owner command", e.Journal.TargetEngine)
				}
			}
			bindCode, bindOut, bindDiag := bind.run(t)
			pdnsCode, pdnsOut, pdnsDiag := pdns.run(t)
			if bindCode != exitOK || pdnsCode != exitOK || bindCode != pdnsCode {
				t.Fatalf("same journal class exited BIND %d (diagnostic %q), PowerDNS %d (diagnostic %q)",
					bindCode, bindDiag, pdnsCode, pdnsDiag)
			}
			if bindDiag != "" || pdnsDiag != "" {
				t.Fatalf("a classified status wrote a diagnostic: BIND %q, PowerDNS %q", bindDiag, pdnsDiag)
			}
			if bind.strictUsed != 0 {
				t.Fatal("the strict loaded-unit identity reader, which cannot read the guard mask, was used")
			}
			if want := expectedPDNSBeforeActivation(phase); pdnsOut != want {
				t.Fatalf("PowerDNS text changed:\n got: %q\nwant: %q", pdnsOut, want)
			}
			if want := expectedBINDBeforeActivation(phase); bindOut != want {
				t.Fatalf("BIND text changed:\n got: %q\nwant: %q", bindOut, want)
			}
		})
	}
}

// The other side of the same rule: when a required native observation cannot
// be read, both engines exit 3 with a diagnostic, and the guidance printed
// before it is the text a classified read prints.
func TestDNSSwitchStatusUnreadableUnitsExitUnavailableForBothEngines(t *testing.T) {
	unitErr := errors.New("systemctl show timed out")
	for _, c := range []*statusExitCase{
		freshBINDStatusCase(t, dnsengineartifact.SwitchPhaseIntent),
		freshPDNSStatusCase(t, dnsengineartifact.SwitchPhaseIntent),
	} {
		engine := c.evidence.Journal.TargetEngine
		c.unitErr = unitErr
		code, out, diagnostic := c.run(t)
		if code != exitUnavailable {
			t.Fatalf("%s: unreadable native units exited %d, want %d", engine, code, exitUnavailable)
		}
		if !strings.Contains(diagnostic, "Native DNS unit state is unknown") || !strings.Contains(diagnostic, unitErr.Error()) {
			t.Fatalf("%s: diagnostic does not name the unknown unit state: %q", engine, diagnostic)
		}
		header := "DNS switch request " + ownerGuidanceRequest + ": accepted-active (journal phase intent).\n" +
			ownerDNSRecoveryGuidance(c.evidence, true)
		if !strings.HasPrefix(out, header) {
			t.Fatalf("%s: guidance before the unknown observation changed: %q", engine, out)
		}
	}
}

// Batch 7's BIND exit 3 was this unclassified identity, not a verdict about
// the operation: a BIND target that the typed observation cannot classify
// still exits 3, while the class read above exits 0.
func TestDNSSwitchStatusUnclassifiedBINDIdentityStaysUnavailable(t *testing.T) {
	c := freshBINDStatusCase(t, dnsengineartifact.SwitchPhaseIntent)
	readers := c.readers(t)
	readers.bindNeverStarted = func(context.Context, bool) (string, error) {
		return "", errors.New("BIND target unit class is unknown")
	}
	var out, diagnostic bytes.Buffer
	code := runDNSSwitchStatusWith(
		[]string{"dns-switch-status", "--quiesced", "--request-id", ownerGuidanceRequest},
		0, &out, &diagnostic, readers)
	if code != exitUnavailable || !strings.Contains(diagnostic.String(), "Native BIND vendor files or loaded systemd unit identity are unknown") {
		t.Fatalf("unclassified BIND identity exited %d: %q", code, diagnostic.String())
	}
}
