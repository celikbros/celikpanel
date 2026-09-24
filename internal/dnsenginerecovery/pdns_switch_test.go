package dnsenginerecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPDNSSwitchRollbackNeverWritesAfterFailedStop(t *testing.T) {
	boom := errors.New("stop refused")
	var calls []string
	ops := PDNSSwitchRollbackOps{
		StopTarget: func(context.Context) error {
			calls = append(calls, "stop")
			return boom
		},
		RestoreDatabase: func() error { calls = append(calls, "database"); return nil },
		RestoreConfigs:  func() error { calls = append(calls, "configs"); return nil },
		RestoreState:    func() error { calls = append(calls, "state"); return nil },
		RestoreTarget:   func(context.Context) error { calls = append(calls, "target"); return nil },
		RestoreSource:   func(context.Context) error { calls = append(calls, "source"); return nil },
	}
	if err := RollbackPDNSSwitch(context.Background(), ops); !errors.Is(err, boom) {
		t.Fatalf("failed stop was lost: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"stop"}) {
		t.Fatalf("database or config changed after failed stop: %v", calls)
	}
}

func TestPDNSSwitchRollbackStopsAtEveryFailedPredecessor(t *testing.T) {
	names := []string{"stop", "database", "configs", "state", "target", "source"}
	for failAt := range names {
		t.Run(names[failAt], func(t *testing.T) {
			boom := errors.New("injected failure")
			var calls []string
			run := func(name string) error {
				calls = append(calls, name)
				if name == names[failAt] {
					return boom
				}
				return nil
			}
			ops := PDNSSwitchRollbackOps{
				StopTarget:      func(context.Context) error { return run("stop") },
				RestoreDatabase: func() error { return run("database") },
				RestoreConfigs:  func() error { return run("configs") },
				RestoreState:    func() error { return run("state") },
				RestoreTarget:   func(context.Context) error { return run("target") },
				RestoreSource:   func(context.Context) error { return run("source") },
			}
			if err := RollbackPDNSSwitch(context.Background(), ops); !errors.Is(err, boom) {
				t.Fatalf("rollback failure was lost: %v", err)
			}
			if !reflect.DeepEqual(calls, names[:failAt+1]) {
				t.Fatalf("later effect ran after %s failed: %v", names[failAt], calls)
			}
		})
	}
}

func TestPDNSSwitchRollbackCancellationWithholdsDatabaseRestore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var touched bool
	ops := PDNSSwitchRollbackOps{
		StopTarget:      func(context.Context) error { cancel(); return nil },
		RestoreDatabase: func() error { touched = true; return nil },
		RestoreConfigs:  func() error { return nil },
		RestoreState:    func() error { return nil },
		RestoreTarget:   func(context.Context) error { return nil },
		RestoreSource:   func(context.Context) error { return nil },
	}
	if err := RollbackPDNSSwitch(ctx, ops); !errors.Is(err, context.Canceled) || touched {
		t.Fatalf("database restore ran after interruption: touched=%t err=%v", touched, err)
	}
}
