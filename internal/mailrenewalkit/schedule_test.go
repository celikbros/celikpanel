package mailrenewalkit

import (
	"errors"
	"strings"
	"testing"
)

func loadedScheduleUnit(unit string) UnitObservation {
	active, state := "inactive", "static"
	if unit == TimerName {
		active, state = "active", "enabled"
	}
	return UnitObservation{unit, "loaded", "/etc/systemd/system/" + unit, "", "no", active, state}
}
func unitOutput(u UnitObservation) []byte {
	return []byte("LoadState=" + u.LoadState + "\nFragmentPath=" + u.FragmentPath + "\nDropInPaths=" + u.DropInPaths + "\nNeedDaemonReload=" + u.NeedDaemonReload + "\nActiveState=" + u.ActiveState + "\nUnitFileState=" + u.UnitFileState + "\n")
}
func TestScheduleObservationIsExactAndReadOnly(t *testing.T) {
	for _, unit := range []string{ServiceName, TimerName} {
		good := loadedScheduleUnit(unit)
		raw := unitOutput(good)
		got, err := ParseUnitObservation(unit, raw)
		if err != nil || got != good || !got.Ready() {
			t.Fatal(got, err)
		}
		for _, bad := range [][]byte{nil, []byte("LoadState=loaded\n"), append(append([]byte{}, raw...), []byte("LoadState=loaded\n")...), append(append([]byte{}, raw...), []byte("Owner=1\n")...), append(append([]byte{}, raw...), 0), []byte(strings.ReplaceAll(string(raw), "\n", "\r\n")), []byte(strings.Repeat("x", 16385)), []byte(strings.Replace(string(raw), "DropInPaths=", "Unknown=", 1))} {
			if _, err := ParseUnitObservation(unit, bad); !errors.Is(err, ErrScheduleObservation) {
				t.Fatalf("unsafe observation parsed %q %v", bad, err)
			}
		}
		for _, change := range []func(*UnitObservation){func(u *UnitObservation) { u.LoadState = "not-found" }, func(u *UnitObservation) { u.FragmentPath = "/run/systemd/system/" + unit }, func(u *UnitObservation) { u.DropInPaths = "/etc/systemd/system/owner.conf" }, func(u *UnitObservation) { u.NeedDaemonReload = "yes" }, func(u *UnitObservation) { u.ActiveState = "failed" }, func(u *UnitObservation) { u.UnitFileState = "masked" }} {
			altered := good
			change(&altered)
			parsed, err := ParseUnitObservation(unit, unitOutput(altered))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Ready() {
				t.Fatal("unready observation reported ready", parsed)
			}
		}
	}
	if _, err := ParseUnitObservation("owner.service", unitOutput(loadedScheduleUnit(ServiceName))); err == nil {
		t.Fatal("unrelated unit admitted")
	}
	props := ScheduleProperties()
	props[0] = "Owner"
	if ScheduleProperties()[0] != "LoadState" {
		t.Fatal("query contract mutated")
	}
}
func TestTransitionPreservesSchedulePreferencesAndRequiresIdleService(t *testing.T) {
	service, timer := loadedScheduleUnit(ServiceName), loadedScheduleUnit(TimerName)
	for _, enabled := range []string{"enabled", "disabled"} {
		for _, active := range []string{"active", "inactive"} {
			observed := timer
			observed.UnitFileState = enabled
			observed.ActiveState = active
			got, err := TransitionTimer(true, service, observed)
			want := TimerState{Enablement: enabled, Activity: active}
			if err != nil || got != want {
				t.Fatal(got, err)
			}
		}
	}
	for _, state := range []string{"active", "activating", "deactivating"} {
		busy := service
		busy.ActiveState = state
		if _, err := TransitionTimer(true, busy, timer); !errors.Is(err, ErrScheduleBusy) {
			t.Fatal("busy not distinguished", state, err)
		}
	}
	busy, masked := service, timer
	busy.ActiveState = "active"
	masked.UnitFileState = "masked"
	if _, err := TransitionTimer(true, busy, masked); !errors.Is(err, ErrScheduleObservation) {
		t.Fatal("busy invocation hid an unverified timer", err)
	}
	if !func() bool { s := service; s.ActiveState = "activating"; return s.Ready() }() {
		t.Fatal("readiness incorrectly requires idle transition state")
	}
	for _, side := range []string{"service", "timer"} {
		for _, change := range []func(*UnitObservation){func(u *UnitObservation) { u.LoadState = "error" }, func(u *UnitObservation) { u.DropInPaths = "owner.conf" }, func(u *UnitObservation) { u.NeedDaemonReload = "yes" }, func(u *UnitObservation) { u.ActiveState = "failed" }, func(u *UnitObservation) { u.UnitFileState = "enabled-runtime" }, func(u *UnitObservation) { u.FragmentPath = "" }} {
			s, timerCopy := service, timer
			if side == "service" {
				change(&s)
			} else {
				change(&timerCopy)
			}
			if _, err := TransitionTimer(true, s, timerCopy); !errors.Is(err, ErrScheduleObservation) {
				t.Fatal("unverified schedule admitted", side, err)
			}
		}
	}
	if _, err := TransitionTimer(true, timer, service); err == nil {
		t.Fatal("unit roles reversed")
	}
}
func TestBootstrapRequiresPositiveNativeAbsence(t *testing.T) {
	service := UnitObservation{Unit: ServiceName, LoadState: "not-found", NeedDaemonReload: "no", ActiveState: "inactive"}
	timer := service
	timer.Unit = TimerName
	got, err := TransitionTimer(false, service, timer)
	if err != nil || got != (TimerState{Enablement: "absent", Activity: "inactive"}) {
		t.Fatal(got, err)
	}
	for _, alter := range []func(*UnitObservation){func(u *UnitObservation) { u.LoadState = "" }, func(u *UnitObservation) { u.LoadState = "loaded" }, func(u *UnitObservation) { u.UnitFileState = "masked" }, func(u *UnitObservation) { u.NeedDaemonReload = "yes" }, func(u *UnitObservation) { u.ActiveState = "failed" }, func(u *UnitObservation) { u.FragmentPath = "/etc/systemd/system/owner.timer" }, func(u *UnitObservation) { u.DropInPaths = "owner.conf" }} {
		changed := timer
		alter(&changed)
		if _, err = TransitionTimer(false, service, changed); !errors.Is(err, ErrScheduleObservation) {
			t.Fatal("unknown treated as absent", err)
		}
	}
	if _, err = TransitionTimer(false, loadedScheduleUnit(ServiceName), loadedScheduleUnit(TimerName)); err == nil {
		t.Fatal("existing schedule adopted as bootstrap")
	}
}

func TestBootstrapLoadingDoesNotGrantScheduleReadiness(t *testing.T) {
	s := UnitObservation{Unit: ServiceName, LoadState: "not-found", NeedDaemonReload: "no", ActiveState: "inactive"}
	tm := s
	tm.Unit = TimerName
	ls, lt := loadedScheduleUnit(ServiceName), loadedScheduleUnit(TimerName)
	lt.UnitFileState, lt.ActiveState = "disabled", "inactive"
	for _, service := range []UnitObservation{s, ls} {
		for _, timer := range []UnitObservation{tm, lt} {
			for _, published := range []bool{false, true} {
				for _, pending := range []string{"no", "yes"} {
					a, b := service, timer
					a.NeedDaemonReload, b.NeedDaemonReload = pending, pending
					if err := VerifyBootstrapLoaded(a, b, published, true); err != nil {
						t.Fatal("verified intermediate cache refused", err)
					}
				}
				err := VerifyBootstrapLoaded(service, timer, published, false)
				want := published && service == ls && timer == lt || !published && service == s && timer == tm
				if (err == nil) != want {
					t.Fatal("terminal cache not exact", service, timer, published, err)
				}
			}
		}
	}
	if lt.Ready() {
		t.Fatal("idle bootstrap timer is not scheduled renewal")
	}
	for _, side := range []string{"service", "timer"} {
		for _, alter := range []func(*UnitObservation){
			func(u *UnitObservation) { u.Unit = "owner.service" },
			func(u *UnitObservation) { u.LoadState = "error" },
			func(u *UnitObservation) { u.FragmentPath = "/run/systemd/system/" + u.Unit },
			func(u *UnitObservation) { u.DropInPaths = "owner.conf" },
			func(u *UnitObservation) { u.NeedDaemonReload = "unknown" },
			func(u *UnitObservation) { u.ActiveState = "activating" },
			func(u *UnitObservation) { u.ActiveState = "active" },
			func(u *UnitObservation) { u.ActiveState = "failed" },
			func(u *UnitObservation) { u.UnitFileState = "enabled" },
			func(u *UnitObservation) { u.UnitFileState = "masked" },
		} {
			a, b := ls, lt
			if side == "service" {
				alter(&a)
			} else {
				alter(&b)
			}
			if err := VerifyBootstrapLoaded(a, b, true, true); err == nil {
				t.Fatal("owner or unknown native state adopted", side, a, b)
			}
		}
	}
}

func TestNativeShowMissingStatusRequiresCompleteAbsence(t *testing.T) {
	absent := UnitObservation{Unit: ServiceName, LoadState: "not-found", NeedDaemonReload: "no", ActiveState: "inactive"}
	for _, status := range []int{-1, 0, 1, 3, 4, 5, 124, 255} {
		got, err := ParseUnitObservationResult(ServiceName, unitOutput(absent), status)
		if (err == nil) != (status == 0 || status == 4) {
			t.Fatal("native status hidden", status, err)
		}
		if err == nil && got != absent {
			t.Fatal(got)
		}
	}
	for _, raw := range [][]byte{nil, []byte("LoadState=not-found\n"), unitOutput(loadedScheduleUnit(ServiceName))} {
		if _, err := ParseUnitObservationResult(ServiceName, raw, 4); err == nil {
			t.Fatal("failed native lookup treated as positive absence")
		}
	}
}
