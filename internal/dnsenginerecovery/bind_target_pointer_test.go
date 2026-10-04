package dnsenginerecovery

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestClassifyBINDTargetPointerIsOrderedAndReadOnly(t *testing.T) {
	target := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	cause := errors.New("observed difference")
	for _, test := range []struct {
		name        string
		pointer     string
		pointerErr  error
		anchor      bool
		anchorErr   error
		recordsErr  error
		treeErr     error
		configErr   error
		want        BINDTargetPointerKind
		wantSteps   []string
		wantBoot    bool
		wantMissing bool
	}{
		{name: "selects target", pointer: target, want: BINDTargetPointerSelectsTarget, wantSteps: []string{"pointer"}},
		{name: "selects other", pointer: other, want: BINDTargetPointerSelectsOther, wantSteps: []string{"pointer"}},
		{name: "unreadable", pointerErr: cause, want: BINDTargetPointerUnreadable, wantSteps: []string{"pointer"}},
		{name: "records changed", anchor: true, recordsErr: cause, want: BINDTargetPointerRecordsChanged, wantSteps: []string{"pointer", "anchor", "records"}, wantBoot: true, wantMissing: true},
		{name: "generation unverified", anchor: true, treeErr: cause, want: BINDTargetPointerGenerationUnverified, wantSteps: []string{"pointer", "anchor", "records", "tree"}, wantBoot: true, wantMissing: true},
		{name: "config changed, anchor unreadable", anchorErr: cause, anchor: true, configErr: cause, want: BINDTargetPointerConfigChanged, wantSteps: []string{"pointer", "anchor", "records", "tree", "config"}, wantMissing: true},
		{name: "repairable", anchor: true, want: BINDTargetPointerRepairable, wantSteps: []string{"pointer", "anchor", "records", "tree", "config"}, wantBoot: true, wantMissing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var steps []string
			finding, err := ClassifyBINDTargetPointer(target, BINDTargetPointerChecks{
				Current: func() (string, bool, error) {
					steps = append(steps, "pointer")
					return test.pointer, test.pointer != "", test.pointerErr
				},
				AnchorIncludesPointer: func() (bool, error) {
					steps = append(steps, "anchor")
					return test.anchor, test.anchorErr
				},
				VerifyRecords: func() error { steps = append(steps, "records"); return test.recordsErr },
				LoadTarget: func() (binddns.Receipt, error) {
					steps = append(steps, "tree")
					return binddns.Receipt{Generation: target}, test.treeErr
				},
				VerifyConfig: func(binddns.Receipt) error { steps = append(steps, "config"); return test.configErr },
			})
			if err != nil || finding.Kind != test.want || finding.BootBlocked != test.wantBoot ||
				finding.PointerMissing() != test.wantMissing || !reflect.DeepEqual(steps, test.wantSteps) {
				t.Fatalf("finding=%+v err=%v steps=%v", finding, err, steps)
			}
			if test.want == BINDTargetPointerSelectsOther && finding.Selected != other {
				t.Fatalf("selected=%q", finding.Selected)
			}
		})
	}
	if _, err := ClassifyBINDTargetPointer(target, BINDTargetPointerChecks{}); err == nil {
		t.Fatal("incomplete checks were accepted")
	}
}

func TestBINDTargetJournalPredicates(t *testing.T) {
	base := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV1, Mode: transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1,
		TargetGeneration: strings.Repeat("c", 64),
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
			{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		},
	}
	phases := map[string][2]bool{ // verified, unrecorded
		dnsengineartifact.SwitchPhaseIntent:         {false, true},
		dnsengineartifact.SwitchPhaseTargetStaged:   {false, true},
		dnsengineartifact.SwitchPhaseSourceStopped:  {false, true},
		dnsengineartifact.SwitchPhaseTargetStarted:  {false, true},
		dnsengineartifact.SwitchPhaseTargetVerified: {true, false},
		dnsengineartifact.SwitchPhaseCommitted:      {true, false},
		dnsengineartifact.SwitchPhaseRollingBack:    {false, false},
		dnsengineartifact.SwitchPhaseRolledBack:     {false, false},
	}
	for phase, want := range phases {
		j := base
		j.Phase = phase
		if VerifiedBINDTargetPointerJournal(j) != want[0] || UnrecordedBINDTargetJournal(j) != want[1] {
			t.Errorf("phase %s: verified=%v unrecorded=%v", phase, VerifiedBINDTargetPointerJournal(j), UnrecordedBINDTargetJournal(j))
		}
	}
	j := base
	j.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	if !FirstInstallBINDSwitchJournal(j) {
		t.Fatal("first install shape was not recognised")
	}
	for name, mutate := range map[string]func(*dnsengineartifact.SwitchJournalV1){
		"source engine": func(j *dnsengineartifact.SwitchJournalV1) { j.SourceEngine = transport.DNSEnginePowerDNS },
		"source epoch":  func(j *dnsengineartifact.SwitchJournalV1) { j.SourceEpoch = 1 },
		"prior receipt": func(j *dnsengineartifact.SwitchJournalV1) { j.StateBefore.Exists = true },
		"prior pointer": func(j *dnsengineartifact.SwitchJournalV1) { j.HadPrevious = true },
		"reinstall":     func(j *dnsengineartifact.SwitchJournalV1) { j.Mode = transport.DNSEngineSwitchModeReinstall },
		"V2":            func(j *dnsengineartifact.SwitchJournalV1) { j.Schema = dnsengineartifact.SwitchJournalSchemaV2 },
		"running BIND":  func(j *dnsengineartifact.SwitchJournalV1) { j.TargetUnitsBefore[1].ActiveState = "active" },
		"source units": func(j *dnsengineartifact.SwitchJournalV1) {
			j.SourceUnitsBefore = []dnsengineartifact.UnitSnapshot{{Name: "pdns.service"}}
		},
		"no target units": func(j *dnsengineartifact.SwitchJournalV1) { j.TargetUnitsBefore = nil },
	} {
		candidate := j
		candidate.TargetUnitsBefore = append([]dnsengineartifact.UnitSnapshot(nil), j.TargetUnitsBefore...)
		mutate(&candidate)
		if FirstInstallBINDSwitchJournal(candidate) {
			t.Errorf("%s was accepted as a first install", name)
		}
	}
}
