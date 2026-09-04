//go:build linux

package hostplatform

import "testing"

// R-048's exact evidence: at boot the agent read `starting` and the whole
// startup recovery was refused. `starting` and `initializing` are the boot
// transaction still running, and `stopping` is a machine going down - none of
// them is a verdict about the host. Every other state is settled.
// R-048'in tam kaniti: aciliste `starting` okundu ve butun kurtarma reddedildi.
func TestSystemdReadinessSeparatesTransitionsFromVerdicts(t *testing.T) {
	transitions := []string{"initializing", "starting", "stopping"}
	for _, state := range transitions {
		err := validateSystemdReadinessResult([]byte(state+"\n"), 1)
		if err == nil {
			t.Fatalf("state %q was accepted as ready", state)
		}
		if !StillStarting(err) || Unsupported(err) {
			t.Fatalf("state %q was not treated as a transition: %v", state, err)
		}
	}
	for _, state := range []string{"maintenance", "offline", "unknown", ""} {
		err := validateSystemdReadinessResult([]byte(state+"\n"), 1)
		if err == nil {
			t.Fatalf("state %q was accepted as ready", state)
		}
		if StillStarting(err) {
			t.Fatalf("settled state %q was offered as retryable: %v", state, err)
		}
	}
	for _, ready := range []struct {
		state  string
		status int
	}{{"running", 0}, {"degraded", 0}, {"degraded", 1}} {
		if err := validateSystemdReadinessResult(
			[]byte(ready.state+"\n"), ready.status,
		); err != nil {
			t.Fatalf("state %q status %d = %v, want ready", ready.state, ready.status, err)
		}
	}
}
