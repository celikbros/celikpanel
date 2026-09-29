package dnsenginerecovery

import (
	"errors"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

type inProcessDisk struct {
	journal   dnsengineartifact.SwitchJournalV1
	exists    bool
	readErrs  []error
	fail      string // "", "before" or "after"
	persisted []string
}

func (disk *inProcessDisk) read() (dnsengineartifact.SwitchJournalV1, bool, error) {
	if len(disk.readErrs) > 0 {
		err := disk.readErrs[0]
		disk.readErrs = disk.readErrs[1:]
		if err != nil {
			return dnsengineartifact.SwitchJournalV1{}, false, err
		}
	}
	return disk.journal, disk.exists, nil
}

func (disk *inProcessDisk) write(journal dnsengineartifact.SwitchJournalV1) error {
	switch disk.fail {
	case "before":
		return errors.New("injected failure before the checkpoint became durable")
	case "after":
		disk.journal, disk.exists = journal, true
		disk.persisted = append(disk.persisted, journal.Phase)
		return errors.New("injected failure reported after the checkpoint became durable")
	}
	disk.journal, disk.exists = journal, true
	disk.persisted = append(disk.persisted, journal.Phase)
	return nil
}

func TestDecideInProcessRollbackFollowsTheDurableJournal(t *testing.T) {
	_, fixture, _ := switchFixture(t)
	for _, test := range []struct {
		name         string
		diskPhase    string
		memoryPhase  string
		fail         string
		readErrs     []error
		absent       bool
		foreign      bool
		want         InProcessRollbackDecision
		wantWrites   []string
		wantReturned string
	}{
		{name: "verified on disk", diskPhase: dnsengineartifact.SwitchPhaseTargetVerified, memoryPhase: dnsengineartifact.SwitchPhaseTargetVerified, want: InProcessForward, wantReturned: dnsengineartifact.SwitchPhaseTargetVerified},
		{name: "committed on disk", diskPhase: dnsengineartifact.SwitchPhaseCommitted, memoryPhase: dnsengineartifact.SwitchPhaseCommitted, want: InProcessForward, wantReturned: dnsengineartifact.SwitchPhaseCommitted},
		{name: "rolling-back on disk", diskPhase: dnsengineartifact.SwitchPhaseRollingBack, memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, want: InProcessRollbackDurable, wantReturned: dnsengineartifact.SwitchPhaseRollingBack},
		{name: "rolled-back on disk", diskPhase: dnsengineartifact.SwitchPhaseRolledBack, memoryPhase: dnsengineartifact.SwitchPhaseRolledBack, want: InProcessRollbackDurable, wantReturned: dnsengineartifact.SwitchPhaseRolledBack},
		{name: "intent decides", diskPhase: dnsengineartifact.SwitchPhaseIntent, memoryPhase: dnsengineartifact.SwitchPhaseIntent, want: InProcessRollbackDurable, wantWrites: []string{dnsengineartifact.SwitchPhaseRollingBack}, wantReturned: dnsengineartifact.SwitchPhaseRollingBack},
		{name: "target-staged decides", diskPhase: dnsengineartifact.SwitchPhaseTargetStaged, memoryPhase: dnsengineartifact.SwitchPhaseTargetStaged, want: InProcessRollbackDurable, wantWrites: []string{dnsengineartifact.SwitchPhaseRollingBack}, wantReturned: dnsengineartifact.SwitchPhaseRollingBack},
		{name: "source-stopped decides", diskPhase: dnsengineartifact.SwitchPhaseSourceStopped, memoryPhase: dnsengineartifact.SwitchPhaseSourceStopped, want: InProcessRollbackDurable, wantWrites: []string{dnsengineartifact.SwitchPhaseRollingBack}, wantReturned: dnsengineartifact.SwitchPhaseRollingBack},
		{name: "memory ahead of target-started", diskPhase: dnsengineartifact.SwitchPhaseTargetStarted, memoryPhase: dnsengineartifact.SwitchPhaseTargetVerified, want: InProcessRollbackDurable, wantWrites: []string{dnsengineartifact.SwitchPhaseRollingBack}, wantReturned: dnsengineartifact.SwitchPhaseRollingBack},
		{name: "decision write fails before durable", diskPhase: dnsengineartifact.SwitchPhaseTargetStarted, memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, fail: "before", want: InProcessHandOff, wantReturned: dnsengineartifact.SwitchPhaseTargetStarted},
		{name: "decision write fails after durable", diskPhase: dnsengineartifact.SwitchPhaseTargetStarted, memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, fail: "after", want: InProcessRollbackDurable, wantWrites: []string{dnsengineartifact.SwitchPhaseRollingBack}, wantReturned: dnsengineartifact.SwitchPhaseRollingBack},
		{name: "decision write fails after durable but readback fails", diskPhase: dnsengineartifact.SwitchPhaseTargetStarted, memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, fail: "after", readErrs: []error{nil, errors.New("second read failed")}, want: InProcessHandOff, wantWrites: []string{dnsengineartifact.SwitchPhaseRollingBack}, wantReturned: dnsengineartifact.SwitchPhaseTargetStarted},
		{name: "unreadable journal", diskPhase: dnsengineartifact.SwitchPhaseTargetStarted, memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, readErrs: []error{errors.New("read failed")}, want: InProcessHandOff, wantReturned: dnsengineartifact.SwitchPhaseTargetStarted},
		{name: "absent journal", memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, absent: true, want: InProcessHandOff, wantReturned: dnsengineartifact.SwitchPhaseTargetStarted},
		{name: "foreign journal", diskPhase: dnsengineartifact.SwitchPhaseTargetStarted, memoryPhase: dnsengineartifact.SwitchPhaseTargetStarted, foreign: true, want: InProcessHandOff, wantReturned: dnsengineartifact.SwitchPhaseTargetStarted},
		{name: "enable-intent has no in-process decision", diskPhase: dnsengineartifact.SwitchPhaseTargetEnableIntent, memoryPhase: dnsengineartifact.SwitchPhaseTargetEnableIntent, want: InProcessHandOff, wantReturned: dnsengineartifact.SwitchPhaseTargetEnableIntent},
	} {
		t.Run(test.name, func(t *testing.T) {
			disk := &inProcessDisk{journal: fixture, exists: !test.absent, readErrs: test.readErrs}
			disk.journal.Phase = test.diskPhase
			if test.foreign {
				disk.journal.MutationOwnerID = "ffffffffffffffffffffffffffffffff"
			}
			expected := fixture
			expected.Phase = test.memoryPhase
			disk.fail = test.fail
			ops := InProcessJournalOps{Read: disk.read, Write: disk.write}
			before := disk.journal
			got, decision, err := DecideInProcessRollback(expected, ops)
			if decision != test.want {
				t.Fatalf("decision=%s want %s err=%v", decision, test.want, err)
			}
			if (err != nil) != (decision == InProcessHandOff) {
				t.Fatalf("decision=%s err=%v", decision, err)
			}
			if got.Phase != test.wantReturned {
				t.Fatalf("returned phase=%s want %s", got.Phase, test.wantReturned)
			}
			if !reflect.DeepEqual(disk.persisted, test.wantWrites) && !(len(disk.persisted) == 0 && len(test.wantWrites) == 0) {
				t.Fatalf("writes=%v want %v", disk.persisted, test.wantWrites)
			}
			if decision != InProcessRollbackDurable && test.fail != "after" && !reflect.DeepEqual(disk.journal, before) {
				t.Fatalf("a non-durable decision changed the journal on disk: %s -> %s", before.Phase, disk.journal.Phase)
			}
		})
	}
}

func TestDecideInProcessRollbackRequiresJournalAccess(t *testing.T) {
	_, fixture, _ := switchFixture(t)
	if _, decision, err := DecideInProcessRollback(fixture, InProcessJournalOps{}); decision != InProcessHandOff || err == nil {
		t.Fatalf("decision=%s err=%v", decision, err)
	}
}
