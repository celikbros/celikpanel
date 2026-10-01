package main

import (
	"strings"
	"testing"
)

// upd4 F4: a reviewed updater failure line that is too long or carries a path
// keeps a bounded, Panel-built form (code, state, the preflight step and reason
// class) instead of disappearing. No agent text beyond closed tokens and a
// short plain reason crosses the API; paths, controls and URLs never do.
func TestPanelUpdateSummaryKeepsABoundedReviewedForm(t *testing.T) {
	const prefix = "reviewed updater failed: exit status 1: "
	diagnostic := " detail=2026/09/30 23:11:31 Starting CelikPanel Backend... 2026/09/30 23:11:31 WAL-aware service operation idle check failed: service operations are not idle: SQLite sidecar -shm changed after pinning"
	for _, test := range []struct{ name, raw, want string }{
		{"refused preflight",
			prefix + "!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=idle_probe class=concurrent_write: panel service operations are not idle; update refused; quiesce was safely aborted, rerun the exact trusted update" + diagnostic,
			"!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=idle_probe class=concurrent_write detail="},
		{"refused preflight with a slash in its reason",
			prefix + "!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=agent_idle class=check_failed: agent/package mutations are not idle; update refused detail=",
			"!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=agent_idle class=check_failed detail="},
		{"recovery runtime preflight",
			prefix + "!! CELIKPANEL_UPDATE_FAILURE code=recovery_runtime_preflight_failed state=unchanged reason=recovery runtime preflight step=panel_database_check: Recovery database check: service operations are not idle: verify panel database path: lstat /proc/self/fd/7/celikpanel.db: no such file detail=",
			"!! CELIKPANEL_UPDATE_FAILURE code=recovery_runtime_preflight_failed state=unchanged reason=recovery runtime preflight step=panel_database_check detail="},
		{"snapshot cause",
			prefix + "!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=transaction-consistent panel database snapshot failed detail=Create service operation snapshot failed: process 4242 still uses the celikpanel UID",
			"!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=transaction-consistent panel database snapshot failed detail="},
		{"generic reason with a path",
			prefix + "!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=panel database is missing: /var/lib/celikpanel/celikpanel.db detail=",
			"!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason= detail="},
		{"unknown code", prefix + "!! CELIKPANEL_UPDATE_FAILURE code=private_thing state=unchanged reason=x/y detail=", ""},
		{"forged step token", prefix + "!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason=update preflight step=../../etc class=x detail=/z", "!! CELIKPANEL_UPDATE_FAILURE code=update_preflight_refused state=unchanged reason= detail="},
		{"not an updater line", "dial unix /run/celikpanel/agent.sock: connection refused", ""},
	} {
		got := sanitizePanelUpdateSummary(test.raw)
		if got != test.want {
			t.Fatalf("%s:\n got %q\nwant %q", test.name, got, test.want)
		}
		if got != "" && (len(got) > 240 || strings.ContainsAny(got, "/\\\r\n\t") || strings.Contains(got, "://")) {
			t.Fatalf("%s: unsafe bounded form %q", test.name, got)
		}
	}
	// A short, plain summary is unchanged, as before.
	plain := prefix + "!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=unchanged reason=offline panel database migration failed detail="
	if got := sanitizePanelUpdateSummary(plain); got != plain {
		t.Fatalf("plain summary changed: %q", got)
	}
}
