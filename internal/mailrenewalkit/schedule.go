package mailrenewalkit

import (
	"errors"
	"strings"
)

var ErrScheduleObservation = errors.New("mail renewal schedule could not be verified; inspect the native service, timer and overrides before continuing")
var ErrScheduleBusy = errors.New("mail renewal service is running or changing state; wait for that invocation before preparing the same transition")

// UnitObservation is a bounded observation, not persisted ownership or mutation
// permission. Callers retain/revalidate native file evidence separately and must
// obtain these properties from the local native service manager, not a draft.
type UnitObservation struct {
	Unit             string
	LoadState        string
	FragmentPath     string
	DropInPaths      string
	NeedDaemonReload string
	ActiveState      string
	UnitFileState    string
}

// ScheduleProperties returns a new slice so a caller cannot change the shared
// query contract. Explicit properties avoid parsing locale-dependent status UI.
func ScheduleProperties() []string {
	return []string{"LoadState", "FragmentPath", "DropInPaths", "NeedDaemonReload", "ActiveState", "UnitFileState"}
}
func knownUnit(unit string) bool { return unit == ServiceName || unit == TimerName }
func ParseUnitObservation(unit string, raw []byte) (UnitObservation, error) {
	if !knownUnit(unit) || len(raw) == 0 || len(raw) > 16384 || strings.ContainsAny(string(raw), "\x00\r") {
		return UnitObservation{}, ErrScheduleObservation
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return UnitObservation{}, ErrScheduleObservation
		}
		if _, exists := fields[key]; exists {
			return UnitObservation{}, ErrScheduleObservation
		}
		switch key {
		case "LoadState", "FragmentPath", "DropInPaths", "NeedDaemonReload", "ActiveState", "UnitFileState":
		default:
			return UnitObservation{}, ErrScheduleObservation
		}
		fields[key] = value
	}
	if len(fields) != 6 {
		return UnitObservation{}, ErrScheduleObservation
	}
	return UnitObservation{unit, fields["LoadState"], fields["FragmentPath"], fields["DropInPaths"], fields["NeedDaemonReload"], fields["ActiveState"], fields["UnitFileState"]}, nil
}
func (s UnitObservation) reviewedLoaded() bool {
	return knownUnit(s.Unit) && s.LoadState == "loaded" && s.FragmentPath == "/etc/systemd/system/"+s.Unit && s.DropInPaths == "" && s.NeedDaemonReload == "no"
}
func (s UnitObservation) verifiedAbsent() bool {
	return knownUnit(s.Unit) && s.LoadState == "not-found" && s.FragmentPath == "" && s.DropInPaths == "" && s.NeedDaemonReload == "no" && s.ActiveState == "inactive" && s.UnitFileState == ""
}

// Ready preserves the existing public readiness policy. A valid disk kit alone
// is insufficient: disabled/overridden/stale/unobserved schedules are not ready.
// A running valid oneshot service can be ready without being safe to transition.
func (s UnitObservation) Ready() bool {
	if !s.reviewedLoaded() {
		return false
	}
	if s.Unit == TimerName {
		return s.ActiveState == "active" && s.UnitFileState == "enabled"
	}
	return s.UnitFileState == "static" && (s.ActiveState == "inactive" || s.ActiveState == "active" || s.ActiveState == "activating")
}

// TransitionTimer is read-only admission evidence for a separately authorized
// transition. It never enables or stops a timer. Existing owner preferences are
// retained; only two positively absent units admit the bootstrap observation.
// The oneshot service must be idle so transition orchestration can establish its
// native exclusion boundary without pretending a running invocation is absent.
func TransitionTimer(previous bool, service, timer UnitObservation) (TimerState, error) {
	if service.Unit != ServiceName || timer.Unit != TimerName {
		return TimerState{}, ErrScheduleObservation
	}
	if !previous {
		if service.verifiedAbsent() && timer.verifiedAbsent() {
			return TimerState{Enablement: "absent", Activity: "inactive"}, nil
		}
		return TimerState{}, ErrScheduleObservation
	}
	if !service.reviewedLoaded() || service.UnitFileState != "static" || !timer.reviewedLoaded() {
		return TimerState{}, ErrScheduleObservation
	}
	state := TimerState{Enablement: timer.UnitFileState, Activity: timer.ActiveState}
	if !validRetainedTimer(state) {
		return TimerState{}, ErrScheduleObservation
	}
	switch service.ActiveState {
	case "active", "activating", "deactivating":
		return TimerState{}, ErrScheduleBusy
	case "inactive":
	default:
		return TimerState{}, ErrScheduleObservation
	}
	return state, nil
}
