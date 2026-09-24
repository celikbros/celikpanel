package main

import (
	"context"
	"testing"
)

func TestPDNSRollbackRequiresStableDeadTargetBeforeDatabaseRestore(t *testing.T) {
	for _, tc := range []struct {
		name      string
		first     bindInstallUnitState
		second    bindInstallUnitState
		processes dnsUnitProcesses
		wantError bool
	}{
		{"stable-stopped", bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "enabled"}, bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "enabled"}, dnsUnitProcesses{SubState: "dead"}, false},
		{"still-active", bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "active", unitFileState: "enabled"}, bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "active", unitFileState: "enabled"}, dnsUnitProcesses{SubState: "running", MainPID: 42}, true},
		{"remaining-process", bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "enabled"}, bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "enabled"}, dnsUnitProcesses{SubState: "dead", MainPID: 42}, true},
		{"unit-changed", bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "enabled"}, bindInstallUnitState{name: "pdns.service", loadState: "loaded", activeState: "inactive", unitFileState: "disabled"}, dnsUnitProcesses{SubState: "dead"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			err := verifyPDNSStoppedBeforeDatabaseRestoreWithOps(context.Background(), pdnsRollbackStoppedProofOps{
				inspectUnit: func(context.Context) (bindInstallUnitState, error) {
					reads++
					if reads == 1 {
						return tc.first, nil
					}
					return tc.second, nil
				},
				inspectProcesses: func(context.Context) (dnsUnitProcesses, error) {
					return tc.processes, nil
				},
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("stopped proof error = %v; want error %t", err, tc.wantError)
			}
		})
	}
}
