package dnsenginerecovery

import (
	"context"
	"errors"
	"testing"
)

func TestVerifyStoppedUnitRequiresTwoExactNativeObservations(t *testing.T) {
	stopped := StoppedUnitObservation{
		Name: "named.service", LoadState: "loaded", ActiveState: "inactive",
		UnitFileState: "disabled", SubState: "dead",
	}
	reads := 0
	if err := VerifyStoppedUnit(context.Background(), "named.service",
		func(context.Context) (StoppedUnitObservation, error) {
			reads++
			return stopped, nil
		}); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatalf("native reads = %d, want two", reads)
	}
	for _, tc := range []struct {
		name   string
		change func(*StoppedUnitObservation)
	}{
		{"active", func(s *StoppedUnitObservation) { s.ActiveState = "active" }},
		{"wrong-unit", func(s *StoppedUnitObservation) { s.Name = "pdns.service" }},
		{"main-pid", func(s *StoppedUnitObservation) { s.MainPID = 17 }},
		{"control-pid", func(s *StoppedUnitObservation) { s.ControlPID = 19 }},
		{"transition", func(s *StoppedUnitObservation) { s.SubState = "stop-sigterm" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := stopped
			tc.change(&seen)
			if err := VerifyStoppedUnit(context.Background(), "named.service",
				func(context.Context) (StoppedUnitObservation, error) { return seen, nil },
			); err == nil {
				t.Fatal("unsafe native state was accepted")
			}
		})
	}
	reads = 0
	if err := VerifyStoppedUnit(context.Background(), "named.service",
		func(context.Context) (StoppedUnitObservation, error) {
			reads++
			seen := stopped
			if reads == 2 {
				seen.UnitFileState = "enabled"
			}
			return seen, nil
		},
	); err == nil {
		t.Fatal("changed native unit was accepted")
	}
	reads = 0
	if err := VerifyStoppedUnit(context.Background(), "named.service",
		func(context.Context) (StoppedUnitObservation, error) {
			reads++
			seen := stopped
			if reads == 2 {
				seen.ControlPID = 20
			}
			return seen, nil
		},
	); err == nil {
		t.Fatal("new control process was accepted")
	}
	if err := VerifyStoppedUnit(context.Background(), "ssh.service",
		func(context.Context) (StoppedUnitObservation, error) { return stopped, nil },
	); err == nil {
		t.Fatal("unrelated unit was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyStoppedUnit(ctx, "named.service",
		func(context.Context) (StoppedUnitObservation, error) {
			t.Fatal("cancelled proof ran the native observer")
			return stopped, nil
		},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled proof = %v", err)
	}
	ctxDuringRead, cancelDuringRead := context.WithCancel(context.Background())
	if err := VerifyStoppedUnit(ctxDuringRead, "named.service",
		func(context.Context) (StoppedUnitObservation, error) {
			cancelDuringRead()
			return stopped, nil
		},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled native observation was accepted: %v", err)
	}
}

func TestVerifyStoppedPDNSPersistentMaskRequiresExactStableNativeEvidence(t *testing.T) {
	sealed := StoppedUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked", SubState: "dead"}
	reads := 0
	if err := VerifyStoppedPDNSPersistentMask(context.Background(), func(context.Context) (StoppedUnitObservation, error) {
		reads++
		return sealed, nil
	}); err != nil || reads != 2 {
		t.Fatalf("sealed stop proof err=%v reads=%d", err, reads)
	}
	for _, change := range []func(*StoppedUnitObservation){
		func(s *StoppedUnitObservation) { s.LoadState = "loaded" },
		func(s *StoppedUnitObservation) { s.UnitFileState = "masked-runtime" },
		func(s *StoppedUnitObservation) { s.ActiveState = "active" },
		func(s *StoppedUnitObservation) { s.MainPID = 7 },
		func(s *StoppedUnitObservation) { s.ControlPID = 9 },
		func(s *StoppedUnitObservation) { s.SubState = "stop-sigterm" },
	} {
		seen := sealed
		change(&seen)
		if err := VerifyStoppedPDNSPersistentMask(context.Background(), func(context.Context) (StoppedUnitObservation, error) { return seen, nil }); err == nil {
			t.Fatalf("unsafe masked unit accepted: %+v", seen)
		}
	}
	reads = 0
	if err := VerifyStoppedPDNSPersistentMask(context.Background(), func(context.Context) (StoppedUnitObservation, error) {
		reads++
		seen := sealed
		if reads == 2 {
			seen.UnitFileState = "masked-runtime"
		}
		return seen, nil
	}); err == nil {
		t.Fatal("mask changed between observations")
	}
}
