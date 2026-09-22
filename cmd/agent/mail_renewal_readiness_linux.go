//go:build linux

package main

import (
	"context"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"strings"
)

// Activity is checked separately from pinned disk bytes. Loaded overrides or a
// pending daemon reload cannot be mistaken for the reviewed native schedule.
func independentMailRenewalScheduleReady(ctx context.Context) bool {
	for _, unit := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		raw, err := serviceMutationCommand(ctx, "systemctl", "show", "--property=LoadState", "--property=FragmentPath", "--property=DropInPaths", "--property=NeedDaemonReload", "--property=ActiveState", "--property=UnitFileState", unit).Output()
		if err != nil || !independentMailRenewalUnitReady(unit, raw) {
			return false
		}
	}
	return true
}
func independentMailRenewalUnitReady(unit string, raw []byte) bool {
	if len(raw) == 0 || len(raw) > 16384 || (unit != mailrenewalkit.ServiceName && unit != mailrenewalkit.TimerName) {
		return false
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return false
		}
		if _, exists := fields[key]; exists {
			return false
		}
		switch key {
		case "LoadState", "FragmentPath", "DropInPaths", "NeedDaemonReload", "ActiveState", "UnitFileState":
		default:
			return false
		}
		fields[key] = value
	}
	if len(fields) != 6 || fields["LoadState"] != "loaded" || fields["FragmentPath"] != "/etc/systemd/system/"+unit || fields["DropInPaths"] != "" || fields["NeedDaemonReload"] != "no" {
		return false
	}
	if unit == mailrenewalkit.TimerName {
		return fields["ActiveState"] == "active" && fields["UnitFileState"] == "enabled"
	}
	return fields["UnitFileState"] == "static" && (fields["ActiveState"] == "inactive" || fields["ActiveState"] == "active" || fields["ActiveState"] == "activating")
}
