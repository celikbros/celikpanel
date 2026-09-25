package dnsenginerecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBINDActivationRollbackDoesNotRewriteConfigAfterFailedTargetRestore(t *testing.T) {
	boom := errors.New("target unit could still be running")
	var calls []string
	ops := BINDActivationRollbackOps{
		RestoreTarget:            func(context.Context) error { calls = append(calls, "target"); return boom },
		VerifyTargetBeforeConfig: func(context.Context) error { calls = append(calls, "verify"); return nil },
		RestoreConfigs:           func() error { calls = append(calls, "configs"); return nil },
		RestoreState:             func() error { calls = append(calls, "state"); return nil },
		RestoreSource:            func(context.Context) error { calls = append(calls, "source"); return nil },
	}
	if err := RollbackBINDActivation(context.Background(), ops); !errors.Is(err, boom) {
		t.Fatalf("failed target restore was lost: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"target"}) {
		t.Fatalf("later mutation ran after failed target restore: %v", calls)
	}
}

func TestBINDActivationRollbackStopsAtEveryFailedPredecessor(t *testing.T) {
	names := []string{"target", "verify", "configs", "state", "source"}
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
			ops := BINDActivationRollbackOps{
				RestoreTarget:            func(context.Context) error { return run("target") },
				VerifyTargetBeforeConfig: func(context.Context) error { return run("verify") },
				RestoreConfigs:           func() error { return run("configs") },
				RestoreState:             func() error { return run("state") },
				RestoreSource:            func(context.Context) error { return run("source") },
			}
			if err := RollbackBINDActivation(context.Background(), ops); !errors.Is(err, boom) {
				t.Fatalf("rollback failure was lost: %v", err)
			}
			if !reflect.DeepEqual(calls, names[:failAt+1]) {
				t.Fatalf("later effect ran after %s failed: %v", names[failAt], calls)
			}
		})
	}
}
