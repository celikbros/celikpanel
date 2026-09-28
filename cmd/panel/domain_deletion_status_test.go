package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

func seedExactDeletionStatus(t *testing.T, p *Panel) (int, dnsZoneEngineLease) {
	t.Helper()
	const domain = "pending-deletion.example.test"
	id, _ := seedDomainDeletionLedger(t, p, domain, "dnsonly")
	switchDNSEngineIdentityForTest(t, p, transport.DNSEngineBIND)
	engineState, err := readDNSEngineDBState(t.Context(), p.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.markDomainDeletionPending(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	lease := dnsZoneEngineLease{
		ZoneName: domain, Engine: transport.DNSEngineBIND, EngineEpoch: engineState.EngineEpoch,
		RequestID: strings.Repeat("a", 32), OwnerID: strings.Repeat("b", 32),
		DesiredGeneration: 2, DesiredAction: "delete", DesiredZoneType: "MASTER",
		Qualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		ExpiresAt: "2030-01-01T00:00:00Z",
	}
	db := p.db.GetDB()
	for _, query := range []string{
		`INSERT INTO dns_zone_deletion_markers(zone_name, zone_type) VALUES ('pending-deletion.example.test','MASTER')`,
		`UPDATE dns_zone_sync_state SET source_domain_id=NULL,desired_generation=2,applied_generation=1,desired_action='delete',desired_zone_type='MASTER',status='pending' WHERE zone_name='pending-deletion.example.test'`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(`INSERT INTO dns_zone_engine_leases(zone_name,engine,engine_epoch,request_id,owner_id,desired_generation,desired_action,desired_zone_type,qualifier,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		lease.ZoneName, lease.Engine, lease.EngineEpoch, lease.RequestID, lease.OwnerID,
		lease.DesiredGeneration, lease.DesiredAction, lease.DesiredZoneType, lease.Qualifier, lease.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return id, lease
}

func readDeletionStatusForTest(t *testing.T, p *Panel, id int) (int, domainDeletionStatusResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/domains/%d/deletion-status", id), nil)
	p.handleDomainDeletionStatus(w, r, id)
	var body domainDeletionStatusResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, body
}

func TestDomainDeletionStatusRequiresExactPendingJobAndStableMarker(t *testing.T) {
	p := newDNSPanelForTest(t)
	id, lease := seedExactDeletionStatus(t, p)
	agent := newDNSZoneV3TestAgent()
	agent.durableMutationRPCFixture.jobs = map[string]*ServiceOperationMutationJob{}
	phase, err := dnsZoneSyncV3PendingPhase(lease.identity())
	if err != nil {
		t.Fatal(err)
	}
	job := &ServiceOperationMutationJob{
		RequestID: lease.RequestID, OwnerID: lease.OwnerID,
		Kind: "dns_zone_sync", Target: lease.ZoneName, PackageName: lease.Qualifier,
		Status: agentMutationPending, Phase: phase,
		ErrorCode: transport.DNSPeerPendingNativeUnknown,
	}
	agent.durableMutationRPCFixture.jobs[lease.RequestID] = job
	attachDNSZoneV3TestAgent(t, p, agent)

	status, body := readDeletionStatusForTest(t, p, id)
	if status != http.StatusOK || body.Status != domainDeletionPendingStatus ||
		body.Stage != "dns_cleanup" || body.Reason != transport.DNSPeerPendingNativeUnknown ||
		strings.Contains(body.Message, job.RequestID) {
		t.Fatalf("exact pending status=%d body=%+v", status, body)
	}
	job.OwnerID = strings.Repeat("d", 32)
	_, body = readDeletionStatusForTest(t, p, id)
	if body.Reason != "" || body.Status != "unknown" {
		t.Fatalf("foreign job disclosed reason: %+v", body)
	}
	job.OwnerID = lease.OwnerID
	job.ErrorCode = "private peer output"
	_, body = readDeletionStatusForTest(t, p, id)
	if body.Reason != "" || body.Status != domainDeletionPendingStatus || strings.Contains(body.Message, "private peer output") {
		t.Fatalf("raw job error disclosed: %+v", body)
	}
	job.ErrorCode = transport.DNSPeerPendingNativeUnknown
	agent.statusHook = func(_ string, _ *ServiceOperationMutationJob) {
		if _, err := p.db.GetDB().Exec(`UPDATE dns_zone_engine_leases SET expires_at='2031-01-01T00:00:00Z' WHERE zone_name=?`, lease.ZoneName); err != nil {
			t.Error(err)
		}
	}
	_, body = readDeletionStatusForTest(t, p, id)
	if body.Reason != "" || body.Status != "unknown" {
		t.Fatalf("stale marker disclosed reason: %+v", body)
	}
}

func TestDomainDeletionStatusMarkerAndTenantAuthorization(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	seedAdditionalUserSession(t, &fixture)
	path := fmt.Sprintf("/api/v1/domains/%d/deletion-status", authzMatrixCustomerDomainID)
	caller := teamMemberAuthzTestCaller()
	request := teamMemberAuthzTestRequest(http.MethodGet, path, caller)
	w := httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(w, request)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ungranted status=%d", w.Code)
	}

	grantTeamMemberDomainCapability(t, &fixture, authzMatrixCustomerDomainID,
		core.TeamCapabilityDNS, core.TeamPermissionView)
	request = teamMemberAuthzTestRequest(http.MethodGet, path, caller)
	w = httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(w, request)
	if w.Code != http.StatusNoContent {
		t.Fatalf("no deletion marker status=%d body=%s", w.Code, w.Body.String())
	}

	foreign := teamMemberAuthzTestRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/domains/%d/deletion-status", authzMatrixOutsiderDomainID), caller)
	w = httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(w, foreign)
	if w.Code != http.StatusNotFound {
		t.Fatalf("foreign domain status=%d body=%s", w.Code, w.Body.String())
	}

	method := teamMemberAuthzTestRequest(http.MethodPost, path, caller)
	w = httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(w, method)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mutation method status=%d", w.Code)
	}

	if _, err := fixture.panel.db.GetDB().Exec(`UPDATE domains SET status='pending' WHERE id=?`, authzMatrixCustomerDomainID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`INSERT INTO domain_deletion_operations(domain_id,previous_status) VALUES(?, 'active')`, authzMatrixCustomerDomainID); err != nil {
		t.Fatal(err)
	}
	request = teamMemberAuthzTestRequest(http.MethodGet, path, caller)
	w = httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(w, request)
	var body domainDeletionStatusResponse
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil ||
		body.Status != "unknown" || body.Reason != "" || body.Stage != "unknown" {
		t.Fatalf("generic pending status=%d body=%s", w.Code, w.Body.String())
	}
}
