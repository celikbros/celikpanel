package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDANEAutomationFeatureStateIsSafelyDisabled(t *testing.T) {
	state := currentDANEAutomationState()
	if state.Enabled {
		t.Fatal("automatic DANE mutation is enabled without a durable rollover job")
	}
	if daneMutationSafetyPrerequisitesAvailable() {
		t.Fatal("DANE mutation safety prerequisites unexpectedly report available")
	}
	reason := strings.ToLower(state.Reason)
	if !strings.Contains(reason, "rollover") || !strings.Contains(reason, "ownership") {
		t.Fatalf("disabled DANE state does not explain both safety blockers: %+v", state)
	}
}

func TestRefreshTLSARecordsIsNoOpWhileGateDisabled(t *testing.T) {
	// A zero-value Panel would panic if refresh tried to reach the database,
	// agent, or DNS publisher. Success proves certificate/mail flows cannot
	// mutate any TLSA record while the release gate is disabled.
	if err := (&Panel{}).refreshTLSARecords(context.Background(), 42); err != nil {
		t.Fatalf("disabled DANE refresh should not block core TLS operations: %v", err)
	}
}

func TestDNSSECResultErrorPreservesAgentFailure(t *testing.T) {
	const want = "rectify zone: backend refused the update"
	got := dnssecResultError(dnssecAgentResponse{Error: want}, true)
	if got != want {
		t.Fatalf("dnssecResultError() = %q, want exact agent error %q", got, want)
	}
}

func TestDNSSECResultErrorRejectsSuccessWithoutDS(t *testing.T) {
	got := dnssecResultError(dnssecAgentResponse{Secured: true}, true)
	if !strings.Contains(got, "no DS") {
		t.Fatalf("dnssecResultError() = %q, want missing-DS failure", got)
	}
}

func TestDNSSECResultErrorAllowsUnsignedStatus(t *testing.T) {
	if got := dnssecResultError(dnssecAgentResponse{}, false); got != "" {
		t.Fatalf("unsigned status returned error %q", got)
	}
}

func TestDNSSECPostPreflightsV2BeforeSigningOrSnapshotMutation(t *testing.T) {
	tests := []struct {
		name         string
		panelCommit  string
		agentCommit  string
		capabilities []string
		rhel         bool
	}{
		{name: "legacy capability missing", capabilities: []string{}},
		{
			name: "paired build mismatch", panelCommit: "panel-release",
			agentCommit: "agent-release",
			capabilities: []string{
				transport.AgentCapabilityDNSZoneSyncV2,
				transport.AgentCapabilityDNSSECSecureV2,
			},
		},
		{
			name: "RHEL policy denial",
			capabilities: []string{
				transport.AgentCapabilityDNSZoneSyncV2,
				transport.AgentCapabilityDNSSECSecureV2,
			},
			rhel: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.panelCommit != "" {
				withPanelBuildCommit(t, test.panelCommit)
			}
			p := newDNSPanelForTest(t)
			setDNSIdentityForTest(t, p, "standalone")
			result, err := p.db.GetDB().Exec(`
				INSERT INTO users (username, password_hash, email, role)
				VALUES ('dnssec-owner', 'hash', 'dnssec-owner@example.test', 'customer');
				INSERT INTO subscriptions (owner_id, name)
				VALUES (last_insert_rowid(), 'DNSSEC preflight');
				INSERT INTO domains (subscription_id, name)
				VALUES (last_insert_rowid(), 'dnssec-preflight.example');
			`)
			if err != nil {
				t.Fatal(err)
			}
			domainID64, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			domainID := int(domainID64)
			seedStrictDNSZone(t, p, "dnssec-preflight.example")
			before, err := readDNSZoneSyncState(
				context.Background(), p.db.GetDB(), "dnssec-preflight.example",
			)
			if err != nil {
				t.Fatal(err)
			}
			var beforeSOA string
			if err := p.db.GetDB().QueryRow(`
				SELECT r.content
				FROM pdns_records r
				JOIN pdns_domains d ON d.id = r.domain_id
				WHERE d.name = 'dnssec-preflight.example' AND r.type = 'SOA'
			`).Scan(&beforeSOA); err != nil {
				t.Fatal(err)
			}
			capabilities := append([]string(nil), test.capabilities...)
			agent := &strictDNSRPCAgent{
				versionCommit:       test.agentCommit,
				versionCapabilities: &capabilities,
			}
			attachStrictDNSRPCAgent(t, p, agent)
			if test.rhel {
				p.pkgFamilyMu.Lock()
				p.pkgFamilyVal = "dnf"
				p.hostPlatformVal = rhelPolicyTestIdentity()
				p.hostPlatformKnown = true
				p.pkgFamilyMu.Unlock()
			}

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost, "/api/v1/domains/1/dnssec", nil,
			)
			p.handleDomainDNSSEC(recorder, request, domainID)
			if recorder.Code < http.StatusBadRequest {
				t.Fatalf(
					"DNSSEC preflight status=%d body=%s",
					recorder.Code, recorder.Body.String(),
				)
			}

			after, err := readDNSZoneSyncState(
				context.Background(), p.db.GetDB(), "dnssec-preflight.example",
			)
			if err != nil {
				t.Fatal(err)
			}
			var afterSOA string
			if err := p.db.GetDB().QueryRow(`
				SELECT r.content
				FROM pdns_records r
				JOIN pdns_domains d ON d.id = r.domain_id
				WHERE d.name = 'dnssec-preflight.example' AND r.type = 'SOA'
			`).Scan(&afterSOA); err != nil {
				t.Fatal(err)
			}
			if after.hasLease() ||
				after.DesiredGeneration != before.DesiredGeneration ||
				after.AppliedGeneration != before.AppliedGeneration ||
				after.Status != before.Status || afterSOA != beforeSOA {
				t.Fatalf(
					"DNSSEC preflight mutated snapshot: before=%+v/%q after=%+v/%q",
					before, beforeSOA, after, afterSOA,
				)
			}
			agent.mu.Lock()
			secureCalls := agent.secureDNSCalls
			syncCalls := len(agent.syncCalls)
			agent.mu.Unlock()
			agent.durableMutationRPCFixture.mu.Lock()
			jobs := len(agent.durableMutationRPCFixture.jobs)
			agent.durableMutationRPCFixture.mu.Unlock()
			if secureCalls != 0 || syncCalls != 0 || jobs != 0 {
				t.Fatalf(
					"preflight host/jobs calls secure=%d sync=%d jobs=%d",
					secureCalls, syncCalls, jobs,
				)
			}
		})
	}
}
