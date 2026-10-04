//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

const statusPointerPath = "/var/cache/bind/celikpanel/current"

func statusBINDJournal(phase string) dnsengineartifact.SwitchJournalV1 {
	return dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Phase: phase,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1,
		TargetGeneration: strings.Repeat("c", 64),
		Topology:         transport.DNSTopologyStandalone,
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
			{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{},
	}
}

func requireStatusText(t *testing.T, text string, want, forbid []string) {
	t.Helper()
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Errorf("status text lacks %q:\n%s", fragment, text)
		}
	}
	for _, fragment := range forbid {
		if strings.Contains(text, fragment) {
			t.Errorf("status text contains %q:\n%s", fragment, text)
		}
	}
}

func TestDNSSwitchStatusNamesTheMissingBINDPointer(t *testing.T) {
	other := strings.Repeat("d", 64)
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted} {
		j := statusBINDJournal(phase)
		request := j.MutationRequestID
		for _, test := range []struct {
			name   string
			status bindPointerStatus
			want   []string
			forbid []string
		}{
			{
				name: "missing and repairable, named running",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerRepairable, BootBlocked: true,
				}, namedState: "active"},
				want: []string{
					"generation pointer " + statusPointerPath + " is missing",
					"recorded BIND generation " + j.TargetGeneration + " as verified",
					statusPointerPath + "/zones.conf", "keeps answering while it runs",
					"cannot start after a reboot or a BIND restart until the pointer is back",
					"named.service is running now",
					"restarts the CelikPanel Agent (systemctl restart celikpanel-agent)",
					"restores the missing pointer to generation " + j.TargetGeneration + " only if its own full checks pass",
					"--quiesced --request-id " + request, "This status check changed nothing.",
				},
				forbid: []string{"contacts support"},
			},
			{
				name: "missing and repairable, include unconfirmed, named stopped",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerRepairable,
				}, namedState: "failed"},
				want:   []string{"was not established", "named.service is not running now, so BIND is not answering DNS"},
				forbid: []string{"cannot start after a reboot"},
			},
			{
				name: "missing, generation gone",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerGenerationUnverified, BootBlocked: true,
					Cause: errors.New("open generation: file does not exist"),
				}},
				want: []string{
					"is missing, and generation " + j.TargetGeneration, "will not restore it automatically",
					"/var/cache/bind/celikpanel/generations/" + j.TargetGeneration + " was deleted or edited",
					"contacts support with request id " + request, "avoids rebooting or restarting BIND",
				},
				forbid: []string{"only if its own full checks pass"},
			},
			{
				name: "missing, records changed",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerRecordsChanged, Cause: errors.New("receipt differs"),
				}},
				want: []string{"DNS engine records no longer name", "another operation or a restore changed the DNS engine records"},
			},
			{
				name: "missing, configuration changed",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerConfigChanged, Cause: errors.New("include absent"),
				}},
				want: []string{"no longer carries the managed include", "edited by hand"},
			},
			{
				name: "selects another generation",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerSelectsOther, Selected: other,
				}},
				want: []string{
					"selects generation " + other + ", not generation " + j.TargetGeneration,
					"no automatic repair applies and restarting the Agent will not change it",
					"after a restart or reboot it loads generation " + other,
					"who changed the pointer", "contacts support with request id " + request,
				},
				forbid: []string{"is missing", "restarts the CelikPanel Agent"},
			},
			{
				name: "unreadable",
				status: bindPointerStatus{finding: dnsenginerecovery.BINDTargetPointerFinding{
					Kind: dnsenginerecovery.BINDTargetPointerUnreadable, Cause: errors.New("not a root-owned symlink"),
				}},
				want: []string{"could not be read safely", "root-owned symbolic link", "contacts support"},
			},
		} {
			t.Run(phase+"/"+test.name, func(t *testing.T) {
				test.status.pointerPath = statusPointerPath
				requireStatusText(t, bindTargetPointerStatusText(j, test.status), test.want, test.forbid)
			})
		}
		t.Run(phase+"/pointer exact", func(t *testing.T) {
			text := bindTargetPointerStatusText(j, bindPointerStatus{pointerPath: statusPointerPath, finding: dnsenginerecovery.BINDTargetPointerFinding{Kind: dnsenginerecovery.BINDTargetPointerSelectsTarget}})
			if text != "" {
				t.Fatalf("exact pointer produced text %q", text)
			}
		})
	}
}

func TestDNSSwitchStatusNamesTheUnrecordedBINDTargetWithoutPointer(t *testing.T) {
	missing := bindPointerStatus{pointerPath: statusPointerPath, finding: dnsenginerecovery.BINDTargetPointerFinding{
		Kind: dnsenginerecovery.BINDTargetPointerGenerationUnverified, BootBlocked: true,
	}}
	first := statusBINDJournal(dnsengineartifact.SwitchPhaseTargetStarted)
	requireStatusText(t, bindTargetPointerStatusText(first, missing), []string{
		"stopped at phase target-started, before its BIND target was recorded as verified",
		"generation pointer " + statusPointerPath + " is missing", "earlier release",
		"first install with no previous DNS engine",
		"restarts the CelikPanel Agent (systemctl restart celikpanel-agent) without rebooting first",
		"rolls the first install back to no DNS engine", "no DNS service is left listening on port 53",
		"This status check changed nothing.",
	}, []string{"contacts support"})

	withSource := first
	withSource.SourceEngine, withSource.SourceEpoch = transport.DNSEnginePowerDNS, 1
	withSource.StateBefore.Exists = true
	requireStatusText(t, bindTargetPointerStatusText(withSource, missing), []string{
		"before its BIND target was recorded as verified", "does not restore the pointer",
		"does not roll back automatically", "systemctl status named pdns",
		"contacts support with request id " + first.MutationRequestID, "no owner recovery command applies",
	}, []string{"rolls the first install back"})

	present := missing
	present.finding = dnsenginerecovery.BINDTargetPointerFinding{Kind: dnsenginerecovery.BINDTargetPointerSelectsTarget}
	if text := bindTargetPointerStatusText(first, present); text != "" {
		t.Fatalf("unrecorded journal with its pointer produced text %q", text)
	}
}

func TestDNSSwitchStatusPointerAppliesOnlyToBINDTargetShapes(t *testing.T) {
	for _, test := range []struct {
		name    string
		phase   string
		receipt dnsenginerecovery.TargetReceiptStatus
		target  transport.DNSEngine
		want    bool
	}{
		{"verified", dnsengineartifact.SwitchPhaseTargetVerified, dnsenginerecovery.TargetReceiptAbsent, transport.DNSEngineBIND, true},
		{"committed", dnsengineartifact.SwitchPhaseCommitted, dnsenginerecovery.TargetReceiptDifferent, transport.DNSEngineBIND, true},
		{"unrecorded with target records", dnsengineartifact.SwitchPhaseTargetStarted, dnsenginerecovery.TargetReceiptExact, transport.DNSEngineBIND, true},
		{"unrecorded with source records", dnsengineartifact.SwitchPhaseTargetStarted, dnsenginerecovery.TargetReceiptAbsent, transport.DNSEngineBIND, false},
		{"rolling back", dnsengineartifact.SwitchPhaseRollingBack, dnsenginerecovery.TargetReceiptExact, transport.DNSEngineBIND, false},
		{"PowerDNS target", dnsengineartifact.SwitchPhaseTargetVerified, dnsenginerecovery.TargetReceiptExact, transport.DNSEnginePowerDNS, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			j := statusBINDJournal(test.phase)
			j.TargetEngine = test.target
			e := dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{TargetReceipt: test.receipt}}
			if got := bindPointerStatusApplies(e); got != test.want {
				t.Fatalf("applies=%v want %v", got, test.want)
			}
		})
	}
}

// The status checks read a real generation root and never write it.
func TestDNSSwitchStatusPointerChecksAreReadOnly(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the BIND generation publisher requires root-owned trees")
	}
	root := t.TempDir()
	generation, err := binddns.RenderManifest(root, binddns.Manifest{EngineEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "generations", generation.ID)
	if err := os.MkdirAll(filepath.Join(dir, "zones"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"receipt.json": generation.Receipt, "zones.conf": generation.Config} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(dir, "zones"), dir} {
		if err := os.Chmod(path, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	publisher, err := binddns.NewOSPublisher(root)
	if err != nil {
		t.Fatal(err)
	}
	j := statusBINDJournal(dnsengineartifact.SwitchPhaseTargetVerified)
	j.TargetGeneration = generation.ID
	e := dnsenginerecovery.SwitchEvidence{Journal: j, Observation: dnsenginerecovery.EvidenceObservation{
		TargetReceipt: dnsenginerecovery.TargetReceiptExact, SourceOwnership: dnsenginerecovery.SourceOwnershipNotApplicable,
	}}
	exact := func(binddns.Receipt) bool { return true }
	classify := func(e dnsenginerecovery.SwitchEvidence) dnsenginerecovery.BINDTargetPointerFinding {
		t.Helper()
		// The layout argument selects fixed host config paths that do not
		// exist here, so the include is never confirmed in this test.
		finding, err := dnsenginerecovery.ClassifyBINDTargetPointer(j.TargetGeneration, bindPointerStatusChecks(context.Background(), e, publisher, bindroot.APT, 0, exact))
		if err != nil {
			t.Fatal(err)
		}
		return finding
	}
	if finding := classify(e); finding.Kind != dnsenginerecovery.BINDTargetPointerConfigChanged || finding.BootBlocked {
		t.Fatalf("missing pointer with unconfirmed include: %+v", finding)
	}
	changed := e
	changed.Observation.TargetReceipt = dnsenginerecovery.TargetReceiptDifferent
	if finding := classify(changed); finding.Kind != dnsenginerecovery.BINDTargetPointerRecordsChanged {
		t.Fatalf("records changed: %+v", finding)
	}
	gone := e
	gone.Journal.TargetGeneration = strings.Repeat("e", 64)
	if finding, err := dnsenginerecovery.ClassifyBINDTargetPointer(gone.Journal.TargetGeneration, bindPointerStatusChecks(context.Background(), gone, publisher, bindroot.APT, 0, exact)); err != nil ||
		finding.Kind != dnsenginerecovery.BINDTargetPointerGenerationUnverified {
		t.Fatalf("missing generation: %+v %v", finding, err)
	}
	other := strings.Repeat("f", 64)
	if err := os.Symlink("generations/"+other, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if finding := classify(e); finding.Kind != dnsenginerecovery.BINDTargetPointerSelectsOther || finding.Selected != other {
		t.Fatalf("foreign pointer: %+v", finding)
	}
	if target, err := os.Readlink(filepath.Join(root, "current")); err != nil || target != "generations/"+other {
		t.Fatalf("status changed the pointer: %q %v", target, err)
	}
}
