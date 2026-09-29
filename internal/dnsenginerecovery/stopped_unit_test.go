package dnsenginerecovery

import (
	"context"
	"errors"
	"strings"
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

func TestVerifyStoppedFreshSourceTargetAcceptsOnlyNeverServedStates(t *testing.T) {
	base := StoppedUnitObservation{Name: "pdns.service", ActiveState: "inactive", SubState: "dead"}
	with := func(load, file string) StoppedUnitObservation {
		seen := base
		seen.LoadState, seen.UnitFileState = load, file
		return seen
	}
	noListener := func(context.Context) error { return nil }
	for _, tc := range []struct {
		name string
		seen StoppedUnitObservation
	}{
		{"not-found-before-packages", with("not-found", "")},
		{"package-guard-persistent-mask", with("masked", "masked")},
		{"loaded-disabled-after-unmask", with("loaded", "disabled")},
		{"loaded-enabled-after-enable", with("loaded", "enabled")},
	} {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			reads, listens := 0, 0
			err := VerifyStoppedFreshSourceTarget(context.Background(), "pdns.service",
				func(context.Context) (StoppedUnitObservation, error) { reads++; return tc.seen, nil },
				func(context.Context) error { listens++; return nil })
			if err != nil || reads != 2 || listens != 2 {
				t.Fatalf("fresh stopped proof err=%v reads=%d listener proofs=%d", err, reads, listens)
			}
			named := tc.seen
			named.Name = "named.service"
			if err := VerifyStoppedFreshSourceTarget(context.Background(), "named.service",
				func(context.Context) (StoppedUnitObservation, error) { return named, nil }, noListener); err != nil {
				t.Fatalf("fresh BIND target stopped proof: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*StoppedUnitObservation)
	}{
		{"active", func(s *StoppedUnitObservation) { s.ActiveState = "active" }},
		{"activating", func(s *StoppedUnitObservation) { s.ActiveState = "activating" }},
		{"failed", func(s *StoppedUnitObservation) { s.ActiveState = "failed" }},
		{"main-pid", func(s *StoppedUnitObservation) { s.MainPID = 17 }},
		{"control-pid", func(s *StoppedUnitObservation) { s.ControlPID = 19 }},
		{"not-dead", func(s *StoppedUnitObservation) { s.SubState = "running" }},
		{"wrong-unit", func(s *StoppedUnitObservation) { s.Name = "named.service" }},
		{"runtime-mask", func(s *StoppedUnitObservation) { s.UnitFileState = "masked-runtime" }},
		{"unknown-load-state", func(s *StoppedUnitObservation) { s.LoadState = "bad-setting"; s.UnitFileState = "" }},
	} {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			seen := with("masked", "masked")
			tc.change(&seen)
			if err := VerifyStoppedFreshSourceTarget(context.Background(), "pdns.service",
				func(context.Context) (StoppedUnitObservation, error) { return seen, nil }, noListener); err == nil {
				t.Fatalf("unsafe fresh target accepted: %+v", seen)
			}
		})
	}
	t.Run("reject/not-found-with-unit-file", func(t *testing.T) {
		seen := with("not-found", "disabled")
		if err := VerifyStoppedFreshSourceTarget(context.Background(), "pdns.service",
			func(context.Context) (StoppedUnitObservation, error) { return seen, nil }, noListener); err == nil {
			t.Fatal("not-found unit with a unit-file state was accepted")
		}
	})
	t.Run("reject/public-listener", func(t *testing.T) {
		listens := 0
		if err := VerifyStoppedFreshSourceTarget(context.Background(), "pdns.service",
			func(context.Context) (StoppedUnitObservation, error) { return with("masked", "masked"), nil },
			func(context.Context) error {
				listens++
				if listens == 2 {
					return errors.New("public port-53 listener appeared")
				}
				return nil
			}); err == nil {
			t.Fatal("listener appearing on the second observation was accepted")
		}
	})
	t.Run("reject/listener-observer-missing", func(t *testing.T) {
		if err := VerifyStoppedFreshSourceTarget(context.Background(), "pdns.service",
			func(context.Context) (StoppedUnitObservation, error) { return with("masked", "masked"), nil }, nil); err == nil {
			t.Fatal("fresh proof without a listener observer was accepted")
		}
	})
	for _, tc := range []struct {
		name          string
		first, second StoppedUnitObservation
	}{
		{"masked-to-loaded", with("masked", "masked"), with("loaded", "disabled")},
		{"not-found-to-masked", with("not-found", ""), with("masked", "masked")},
		{"disabled-to-enabled", with("loaded", "disabled"), with("loaded", "enabled")},
	} {
		t.Run("reject/changed-between-reads/"+tc.name, func(t *testing.T) {
			reads := 0
			if err := VerifyStoppedFreshSourceTarget(context.Background(), "pdns.service",
				func(context.Context) (StoppedUnitObservation, error) {
					reads++
					if reads == 2 {
						return tc.second, nil
					}
					return tc.first, nil
				}, noListener); err == nil {
				t.Fatal("unit changed between stopped observations was accepted")
			}
		})
	}
	t.Run("source-present-still-requires-loaded", func(t *testing.T) {
		for _, seen := range []StoppedUnitObservation{with("masked", "masked"), with("not-found", "")} {
			if err := VerifyStoppedUnit(context.Background(), "pdns.service",
				func(context.Context) (StoppedUnitObservation, error) { return seen, nil }); err == nil ||
				err.Error() != "DNS target is not a loaded unit" {
				t.Fatalf("source-present proof accepted %+v: %v", seen, err)
			}
		}
		if err := VerifyStoppedUnit(context.Background(), "pdns.service",
			func(context.Context) (StoppedUnitObservation, error) { return with("loaded", "enabled"), nil }); err != nil {
			t.Fatalf("source-present loaded proof: %v", err)
		}
	})
	t.Run("v3-persistent-mask-unchanged", func(t *testing.T) {
		for _, seen := range []StoppedUnitObservation{with("not-found", ""), with("loaded", "disabled")} {
			if err := VerifyStoppedPDNSPersistentMask(context.Background(),
				func(context.Context) (StoppedUnitObservation, error) { return seen, nil }); err == nil {
				t.Fatalf("V3 persistent-mask proof accepted %+v", seen)
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := VerifyStoppedFreshSourceTarget(ctx, "pdns.service",
		func(context.Context) (StoppedUnitObservation, error) { return with("masked", "masked"), nil },
		func(context.Context) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled listener proof = %v", err)
	}
}

func TestVerifyStoppedNeverStartedTargetAcceptsOnlyPreStartStates(t *testing.T) {
	with := func(load, file string) StoppedUnitObservation {
		return StoppedUnitObservation{Name: "named.service", LoadState: load, ActiveState: "inactive", UnitFileState: file, SubState: "dead"}
	}
	constant := func(seen StoppedUnitObservation) func(context.Context) (StoppedUnitObservation, error) {
		return func(context.Context) (StoppedUnitObservation, error) { return seen, nil }
	}
	for _, tc := range []struct {
		name       string
		seen       StoppedUnitObservation
		extraReads int
	}{
		{"absent", with("not-found", ""), 2},
		{"guard-persistent-mask", with("masked", "masked"), 2},
		// A loaded target keeps exactly VerifyStoppedUnit's proof: the V2
		// journal cannot say whether it started, so no extra condition and
		// no relaxed one apply.
		{"loaded", with("loaded", "disabled"), 0},
	} {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			extra := 0
			got, err := VerifyStoppedNeverStartedTarget(context.Background(), "named.service", constant(tc.seen),
				func(context.Context) error { extra++; return nil })
			if err != nil || got != tc.seen || extra != tc.extraReads {
				t.Fatalf("got %+v err=%v source-only reads=%d, want %d", got, err, extra, tc.extraReads)
			}
		})
	}
	for _, tc := range []struct {
		name string
		seen StoppedUnitObservation
	}{
		{"runtime-mask", with("masked", "masked-runtime")},
		{"absent-with-file-state", with("not-found", "disabled")},
		{"error-load-state", with("error", "")},
		{"active", func() StoppedUnitObservation { s := with("masked", "masked"); s.ActiveState = "active"; return s }()},
		{"main-pid", func() StoppedUnitObservation { s := with("masked", "masked"); s.MainPID = 4242; return s }()},
		{"control-pid", func() StoppedUnitObservation { s := with("not-found", ""); s.ControlPID = 7; return s }()},
		{"transition", func() StoppedUnitObservation { s := with("masked", "masked"); s.SubState = "start-pre"; return s }()},
		{"wrong-unit", func() StoppedUnitObservation { s := with("masked", "masked"); s.Name = "bind9.service"; return s }()},
	} {
		t.Run("refuse/"+tc.name, func(t *testing.T) {
			if _, err := VerifyStoppedNeverStartedTarget(context.Background(), "named.service", constant(tc.seen),
				func(context.Context) error { return nil }); err == nil {
				t.Fatalf("unsafe never-started state accepted: %+v", tc.seen)
			}
		})
	}
	for name, sourceErr := range map[string]error{
		"named-process":                errors.New("a named process (PID 4242) exists beside the stopped DNS target"),
		"listener-not-owned-by-source": errors.New("an unexpected process is holding a public DNS listener"),
	} {
		for _, seen := range []StoppedUnitObservation{with("masked", "masked"), with("not-found", "")} {
			t.Run("refuse/"+name+"/"+seen.LoadState, func(t *testing.T) {
				if _, err := VerifyStoppedNeverStartedTarget(context.Background(), "named.service", constant(seen),
					func(context.Context) error { return sourceErr }); err == nil || !strings.Contains(err.Error(), sourceErr.Error()) {
					t.Fatalf("source-only failure not enforced: %v", err)
				}
			})
		}
	}
	t.Run("refuse/changed-between-reads", func(t *testing.T) {
		reads := 0
		if _, err := VerifyStoppedNeverStartedTarget(context.Background(), "named.service",
			func(context.Context) (StoppedUnitObservation, error) {
				reads++
				if reads == 2 {
					return with("loaded", "disabled"), nil
				}
				return with("masked", "masked"), nil
			}, func(context.Context) error { return nil }); err == nil {
			t.Fatal("unit lifted from the mask between reads was accepted")
		}
	})
	t.Run("refuse/source-only-late-failure", func(t *testing.T) {
		calls := 0
		if _, err := VerifyStoppedNeverStartedTarget(context.Background(), "named.service", constant(with("masked", "masked")),
			func(context.Context) error {
				calls++
				if calls == 2 {
					return errors.New("a named process appeared")
				}
				return nil
			}); err == nil {
			t.Fatal("second-read source-only failure was accepted")
		}
	})
	t.Run("refuse/invalid-admission", func(t *testing.T) {
		if _, err := VerifyStoppedNeverStartedTarget(context.Background(), "named.service", constant(with("masked", "masked")), nil); err == nil {
			t.Fatal("missing source-only observer accepted")
		}
		if _, err := VerifyStoppedNeverStartedTarget(context.Background(), "pdns.service",
			constant(StoppedUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked", SubState: "dead"}),
			func(context.Context) error { return nil }); err == nil {
			t.Fatal("never-started BIND class admitted pdns.service")
		}
	})
	// Every other class is unchanged: a started target (or any journal
	// outside the never-started shape) still requires LoadState=loaded.
	t.Run("default-class-still-requires-loaded", func(t *testing.T) {
		for _, seen := range []StoppedUnitObservation{with("masked", "masked"), with("not-found", "")} {
			if err := VerifyStoppedUnit(context.Background(), "named.service", constant(seen)); err == nil ||
				err.Error() != "DNS target is not a loaded unit" {
				t.Fatalf("loaded-unit proof accepted %+v: %v", seen, err)
			}
		}
	})
}
