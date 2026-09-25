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
}
