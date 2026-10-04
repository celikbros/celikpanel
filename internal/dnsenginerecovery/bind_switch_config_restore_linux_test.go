//go:build linux

package dnsenginerecovery

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestInstalledBINDSwitchConfigRestoreRefusesUnsupportedEvidenceBeforeNativeAccess(t *testing.T) {
	policy, _, base, _ := bindStateRestoreFixture(t)
	cases := []struct {
		name   string
		edit   func(*dnsengineartifact.SwitchJournalV1)
		layout bindroot.Layout
	}{
		{"old-journal", func(j *dnsengineartifact.SwitchJournalV1) {
			j.Schema = dnsengineartifact.SwitchJournalSchemaV1
			j.InversePlan = nil
		}, bindroot.APT},
		{"foreign-source", func(j *dnsengineartifact.SwitchJournalV1) { j.SourceEngine = transport.DNSEngineBIND }, bindroot.APT},
		{"foreign-layout", func(*dnsengineartifact.SwitchJournalV1) {}, bindroot.Pacman},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := base
			tc.edit(&j)
			err := RestoreInstalledBINDSwitchConfigsV2(context.Background(), policy, j, tc.layout, 42)
			if err == nil || strings.Contains(err.Error(), "read native BIND config") {
				t.Fatalf("unsafe BIND config inverse reached native read: %v", err)
			}
		})
	}
}

func TestRolledBackBINDConfigCheckpointNeverRewritesAfterState(t *testing.T) {
	_, _, j, _ := bindStateRestoreFixture(t)
	before, after := j.ConfigBefore, j.InversePlan.ConfigAfter
	_, err := frozenBINDSwitchConfigSnapshots(dnsengineartifact.SwitchPhaseRolledBack,
		[]BINDConfigFileState{BINDConfigFileBefore, BINDConfigFileAfter}, before, after)
	if err == nil || !strings.Contains(err.Error(), "still needs native config restoration") {
		t.Fatalf("rolled-back checkpoint admitted mixed config: %v", err)
	}
	current, err := frozenBINDSwitchConfigSnapshots(dnsengineartifact.SwitchPhaseRolledBack,
		[]BINDConfigFileState{BINDConfigFileBefore, BINDConfigFileBefore}, before, after)
	if err != nil || !reflect.DeepEqual(current, before) {
		t.Fatalf("rolled-back exact before state refused: %v", err)
	}
}
