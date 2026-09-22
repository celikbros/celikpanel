//go:build linux

package main

import (
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"strings"
	"testing"
)

func TestIndependentMailReadinessRequiresActualNativeSchedule(t *testing.T) {
	for _, unit := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		active, enabled := "inactive", "static"
		if unit == mailrenewalkit.TimerName {
			active, enabled = "active", "enabled"
		}
		raw := "LoadState=loaded\nFragmentPath=/etc/systemd/system/" + unit + "\nDropInPaths=\nNeedDaemonReload=no\nActiveState=" + active + "\nUnitFileState=" + enabled + "\n"
		if !independentMailRenewalUnitReady(unit, []byte(raw)) {
			t.Fatal("native state refused", unit)
		}
		for _, bad := range []string{strings.Replace(raw, "LoadState=loaded", "LoadState=not-found", 1), strings.Replace(raw, "NeedDaemonReload=no", "NeedDaemonReload=yes", 1), strings.Replace(raw, "DropInPaths=", "DropInPaths=/etc/systemd/system/owner.conf", 1), strings.Replace(raw, "/etc/systemd/system/", "/usr/lib/systemd/system/", 1), strings.Replace(raw, "ActiveState="+active, "ActiveState=failed", 1), strings.Replace(raw, "UnitFileState="+enabled, "UnitFileState=disabled", 1), raw + "LoadState=loaded\n", raw + "Unexpected=1\n", strings.Replace(raw, "DropInPaths=", "Unknown=", 1), ""} {
			if independentMailRenewalUnitReady(unit, []byte(bad)) {
				t.Fatalf("unsafe ready: %q", bad)
			}
		}
	}
}
