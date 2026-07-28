package main

import (
	"strings"
	"testing"
)

func TestValidatePanelCommandModesRejectsEveryOneShotPair(t *testing.T) {
	tests := []struct {
		name   string
		enable func(*panelCommandModes)
	}{
		{name: "create-admin", enable: func(m *panelCommandModes) { m.createAdmin = true }},
		{name: "count-users", enable: func(m *panelCommandModes) { m.countUsers = true }},
		{name: "check-service-operations-idle", enable: func(m *panelCommandModes) { m.checkIdle = true }},
		{name: "check-pre-ledger-service-operations-idle", enable: func(m *panelCommandModes) { m.checkPreLedgerIdle = true }},
		{name: "snapshot-create-or-restore", enable: func(m *panelCommandModes) { m.createOrRestore = true }},
		{name: "migrate-only", enable: func(m *panelCommandModes) { m.migrateOnly = true }},
	}
	for leftIndex, left := range tests {
		for rightIndex := leftIndex + 1; rightIndex < len(tests); rightIndex++ {
			right := tests[rightIndex]
			t.Run(left.name+"+"+right.name, func(t *testing.T) {
				modes := panelCommandModes{}
				left.enable(&modes)
				right.enable(&modes)
				err := validatePanelCommandModes(modes)
				if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatalf("error=%v want mutually exclusive", err)
				}
			})
		}
	}
}

func TestValidatePanelCommandModesAcceptsEachOneShotAlone(t *testing.T) {
	tests := []panelCommandModes{
		{createAdmin: true},
		{countUsers: true},
		{checkIdle: true},
		{checkPreLedgerIdle: true},
		{createOrRestore: true},
		{migrateOnly: true},
	}
	for index, modes := range tests {
		if err := validatePanelCommandModes(modes); err != nil {
			t.Fatalf("case %d: unexpected error: %v", index, err)
		}
	}
}

func TestValidatePanelCommandModesRejectsRuntimeFlagsWithOneShot(t *testing.T) {
	for _, test := range []panelCommandModes{
		{createAdmin: true, demo: true},
		{countUsers: true, insecureCookies: true},
		{migrateOnly: true, demo: true, insecureCookies: true},
	} {
		err := validatePanelCommandModes(test)
		if err == nil || !strings.Contains(err.Error(), "runtime flags") {
			t.Fatalf("error=%v want runtime flags", err)
		}
	}
}

func TestValidatePanelCommandModesAcceptsRuntimeMode(t *testing.T) {
	if err := validatePanelCommandModes(panelCommandModes{demo: true, insecureCookies: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
