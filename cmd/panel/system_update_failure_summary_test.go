package main

import "testing"

// New updater summaries must cross the existing panel boundary, including
// when the installed agent contributes its legacy prefix. Never loosen the
// summary path/control-character restrictions to display a transient cause.
func TestSystemUpdatePackageConflictSummaryLegacyCompatibility(t *testing.T) {
	for _, state := range []string{"unchanged", "recovery_required"} {
		raw := "reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=package_manager_busy state=" + state + " reason=the host package manager is active detail="
		if got := sanitizePanelUpdateSummary(raw); got != raw {
			t.Fatalf("package conflict disappeared at panel boundary: %q", got)
		}
	}
}
