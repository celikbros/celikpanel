package systemd

import "testing"

func TestUnitStateReached(t *testing.T) {
	tests := []struct {
		name       string
		state      unitState
		wantActive bool
		done       bool
		success    bool
	}{
		{name: "active start succeeds", state: unitState{Active: "active", Sub: "running"}, wantActive: true, done: true, success: true},
		{name: "activating keeps waiting", state: unitState{Active: "activating"}, wantActive: true},
		{name: "failed start fails", state: unitState{Active: "failed", Result: "exit-code"}, wantActive: true, done: true},
		{name: "condition skip fails", state: unitState{Active: "inactive", ConditionResult: "no"}, wantActive: true, done: true},
		{name: "inactive stop succeeds", state: unitState{Active: "inactive", Sub: "dead"}, done: true, success: true},
		{name: "active stop waits", state: unitState{Active: "active"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			done, success := unitStateReached(tt.state, tt.wantActive)
			if done != tt.done || success != tt.success {
				t.Fatalf("unitStateReached() = (%v, %v), want (%v, %v)", done, success, tt.done, tt.success)
			}
		})
	}
}

func TestUnitStateSummary(t *testing.T) {
	state := unitState{Active: "failed", Sub: "failed", Result: "exit-code", ConditionResult: "no"}
	want := "state=failed/failed, result=exit-code, condition=no"
	if got := state.summary(); got != want {
		t.Fatalf("summary() = %q, want %q", got, want)
	}
}
