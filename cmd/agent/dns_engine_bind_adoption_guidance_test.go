package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestV2RunningBINDAdoptionRefusalNamesOwnerRecoveryOnlyAfterRollbackDecision(t *testing.T) {
	journal := dnsEngineSwitchJournal{
		Schema:            dnsengineartifact.SwitchJournalSchemaV2,
		MutationRequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{
			SourceBIND: &dnsengineartifact.BINDAdoptionSourceProofV1{},
		},
	}
	journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	err := rollbackDNSSwitchJournal(context.Background(), journal)
	if err == nil || !strings.Contains(err.Error(), "recover-dns-bind-adoption --request-id "+journal.MutationRequestID) {
		t.Fatalf("durable rollback lacked exact owner action: %v", err)
	}
	journal.Phase = dnsengineartifact.SwitchPhaseTargetStaged
	err = rollbackDNSSwitchJournal(context.Background(), journal)
	if err == nil || !strings.Contains(err.Error(), "no durable rollback decision") || strings.Contains(err.Error(), "recover-dns-bind-adoption") {
		t.Fatalf("predecision adoption advertised unsafe inverse: %v", err)
	}
}
