package dnsenginerecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRollbackPDNSAdoptionFailStopAndCancellation(t *testing.T) {
	for _, fail := range []string{"", "prove", "restore", "verify"} {
		t.Run(fail, func(t *testing.T) {
			var calls []string
			failure := errors.New("injected failure")
			run := func(name string) func(context.Context) error {
				return func(context.Context) error {
					calls = append(calls, name)
					if name == fail {
						return failure
					}
					return nil
				}
			}
			err := RollbackPDNSAdoption(context.Background(), PDNSAdoptionRollbackOps{
				ProveConfigs: run("prove"), RestoreState: run("restore"), VerifyRestored: run("verify"),
			})
			want := []string{"prove", "restore", "verify"}
			switch fail {
			case "prove":
				want = want[:1]
			case "restore":
				want = want[:2]
			}
			if !reflect.DeepEqual(calls, want) || (fail == "" && err != nil) || (fail != "" && !errors.Is(err, failure)) {
				t.Fatalf("calls=%v err=%v", calls, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	var calls []string
	err := RollbackPDNSAdoption(ctx, PDNSAdoptionRollbackOps{
		ProveConfigs:   func(context.Context) error { calls = append(calls, "prove"); cancel(); return nil },
		RestoreState:   func(context.Context) error { calls = append(calls, "restore"); return nil },
		VerifyRestored: func(context.Context) error { calls = append(calls, "verify"); return nil },
	})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(calls, []string{"prove"}) {
		t.Fatalf("cancelled proof still restored state: %v, %v", calls, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	calls = nil
	err = RollbackPDNSAdoption(ctx, PDNSAdoptionRollbackOps{
		ProveConfigs:   func(context.Context) error { calls = append(calls, "prove"); return nil },
		RestoreState:   func(context.Context) error { calls = append(calls, "restore"); cancel(); return nil },
		VerifyRestored: func(context.Context) error { calls = append(calls, "verify"); return nil },
	})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(calls, []string{"prove", "restore"}) {
		t.Fatalf("cancelled restore still verified: %v, %v", calls, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	calls = nil
	err = RollbackPDNSAdoption(ctx, PDNSAdoptionRollbackOps{
		ProveConfigs:   func(context.Context) error { calls = append(calls, "prove"); return nil },
		RestoreState:   func(context.Context) error { calls = append(calls, "restore"); return nil },
		VerifyRestored: func(context.Context) error { calls = append(calls, "verify"); cancel(); return nil },
	})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(calls, []string{"prove", "restore", "verify"}) {
		t.Fatalf("cancelled final proof reported success: %v, %v", calls, err)
	}
}

func TestRollbackPDNSAdoptionRejectsMissingEffects(t *testing.T) {
	if err := RollbackPDNSAdoption(context.Background(), PDNSAdoptionRollbackOps{}); err == nil {
		t.Fatal("incomplete native inverse admitted")
	}
}
