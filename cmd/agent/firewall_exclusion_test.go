package main

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/alicelik/celikpanel/internal/firewalllock"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

type contendedFirewallRunner struct{ fakeFirewallCommandRunner }

func (*contendedFirewallRunner) AcquireFirewallLock() (io.Closer, error) {
	return nil, firewalllock.ErrBusy
}
func (*contendedFirewallRunner) LookPath(string) (string, error) {
	panic("host probe before exclusion")
}

type untouchedFirewallStore struct{}

func (untouchedFirewallStore) Load() ([]byte, bool, error) { panic("read before exclusion") }
func (untouchedFirewallStore) Save([]byte) error           { panic("write before exclusion") }
func (untouchedFirewallStore) Remove() error               { panic("remove before exclusion") }

func TestFirewallContentionDoesNotProbePublishOrClaimReady(t *testing.T) {
	runner := &contendedFirewallRunner{}
	for _, action := range []string{"apply", "legacy", "status"} {
		t.Run(action, func(t *testing.T) {
			response := FirewallStatusResponse{PersistenceState: firewallPersistenceReady}
			var err error
			switch action {
			case "apply":
				err = applyStandaloneFirewallV2(context.Background(), mutationpayload.FirewallApplyCommitment{}, runner, untouchedFirewallStore{}, &response)
			case "legacy":
				err = applyFirewallWithRunnerAndStore(runner, untouchedFirewallStore{}, &ApplyFirewallRequest{Enabled: true}, &response)
			case "status":
				err = firewallStatusWithRunnerAndStore(runner, untouchedFirewallStore{}, &response)
			}
			if err != nil || response.PersistenceState != firewallPersistenceUnverified || response.Error == "" || response.PersistenceError != firewalllock.ErrBusy.Error() {
				t.Fatalf("false result under contention: %+v %v", response, err)
			}
		})
	}
}

func TestFirewallRecoveryContentionPreservesAmbiguousOutcome(t *testing.T) {
	outcome, err := recoverFirewallApplyWithRunner(context.Background(), nil, &contendedFirewallRunner{}, untouchedFirewallStore{})
	if outcome != firewallHostAmbiguous || !errors.Is(err, firewalllock.ErrBusy) {
		t.Fatalf("recovery incorrectly terminal: %v %v", outcome, err)
	}
	if _, _, clean := firewallApplyCleanFailureText(outcome, err, true); clean {
		t.Fatal("busy recovery accepted as clean terminal failure")
	}
}
