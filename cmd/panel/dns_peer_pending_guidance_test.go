package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSPeerPendingGuidanceOnlyRecognizesReviewedCodes(t *testing.T) {
	for _, code := range []string{
		transport.DNSPeerPendingEnrollmentRequired,
		transport.DNSPeerPendingEnrollmentChanged,
		transport.DNSPeerPendingInspectionUnknown,
		transport.DNSPeerPendingNativeUnknown,
		transport.DNSPeerPendingJournalUnknown,
		transport.DNSPeerPendingOwnerEditUnknown,
	} {
		body, ok := dnsPeerPendingAPIError(errors.Join(
			errors.New("untrusted remote output"), &dnsZoneV3PropagationPendingError{Code: code, Exact: true},
		))
		if !ok || body.Code != errCodeDNSPublicationFailed || body.Reason != code ||
			!strings.Contains(body.Error, "same publication") ||
			strings.Contains(body.Error, "untrusted remote output") {
			t.Fatalf("reviewed code %q body=%+v ok=%v", code, body, ok)
		}
	}
	for _, pending := range []*dnsZoneV3PropagationPendingError{
		{}, {Code: "untrusted remote output", Exact: true},
		{Code: transport.DNSPeerPendingNativeUnknown},
	} {
		if body, ok := dnsPeerPendingAPIError(pending); ok || body.Reason != "" {
			t.Fatalf("unreviewed code leaked: body=%+v ok=%v", body, ok)
		}
	}
}

func TestDNSZoneV3PendingGuidanceRequiresExactPendingJob(t *testing.T) {
	code := transport.DNSPeerPendingOwnerEditUnknown
	for _, tc := range []struct {
		job  *agentMutationJob
		want string
	}{
		{nil, ""},
		{&agentMutationJob{Status: agentMutationPending, ErrorCode: "dns_zone_v3_propagation_pending"}, ""},
		{&agentMutationJob{Status: agentMutationSucceeded, ErrorCode: code}, ""},
		{&agentMutationJob{Status: agentMutationPending, ErrorCode: code}, code},
	} {
		if got := dnsZoneV3PendingCodeFromJob(tc.job); got != tc.want {
			t.Fatalf("job=%+v code=%q want=%q", tc.job, got, tc.want)
		}
	}
}

func TestDNSZoneV3PendingRPCCodeIsBounded(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := newDNSZoneV3TestAgent()
	attachDNSZoneV3TestAgent(t, panel, agent)
	request := transport.SyncDNSZoneV3Request{
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		DesiredGeneration: 3, Domain: "pending-code.example",
		ZoneType: "MASTER",
	}
	code := transport.DNSPeerPendingInspectionUnknown
	agent.syncResponseHook = func(request transport.SyncDNSZoneV3Request,
		response *transport.SyncDNSZoneV3Response) error {
		*response = transport.SyncDNSZoneV3Response{
			RecoveryPending: true, PendingCode: code,
			Engine: request.Engine, EngineEpoch: request.EngineEpoch,
			AppliedGeneration: request.DesiredGeneration,
		}
		return nil
	}
	var response transport.SyncDNSZoneV3Response
	err := panel.callSyncDNSZoneV3(context.Background(), &request, &response)
	var pending *dnsZoneV3PropagationPendingError
	if !errors.As(err, &pending) || pending.Code != code {
		t.Fatalf("exact pending code=%+v err=%v", response, err)
	}
	code = "private peer output"
	response = transport.SyncDNSZoneV3Response{}
	if err := panel.callSyncDNSZoneV3(context.Background(), &request, &response); err == nil ||
		!strings.Contains(err.Error(), "invalid pending") {
		t.Fatalf("unreviewed pending code accepted: response=%+v err=%v", response, err)
	}
	code = ""
	response = transport.SyncDNSZoneV3Response{}
	err = panel.callSyncDNSZoneV3(context.Background(), &request, &response)
	if !errors.As(err, &pending) || pending.Code != "" {
		t.Fatalf("legacy pending code misclassified: response=%+v err=%v", response, err)
	}
}

func TestDNSZoneV3DeferredErrorRetainsExactPendingCode(t *testing.T) {
	code := transport.DNSPeerPendingNativeUnknown
	err := &dnsAgentPublicationError{Err: errors.Join(
		errDNSZoneV3PropagationDeferred,
		&dnsZoneV3PropagationPendingError{Code: code, Exact: true},
		errors.New("private peer output"),
	)}
	body, ok := dnsPeerPendingAPIError(err)
	if !ok || body.Reason != code ||
		strings.Contains(body.Error, "private peer output") {
		t.Fatalf("deferred pending code lost or leaked: body=%+v ok=%v", body, ok)
	}
}

func TestDomainDeletionPendingReportsReviewedExactReason(t *testing.T) {
	panel := newDNSPanelForTest(t)
	for _, tc := range []struct {
		name       string
		stage      string
		cause      error
		wantReason string
	}{
		{"reviewed", "dns_cleanup", &dnsZoneV3PropagationPendingError{Code: transport.DNSPeerPendingNativeUnknown, Exact: true}, transport.DNSPeerPendingNativeUnknown},
		{"unreconciled", "dns_cleanup", &dnsZoneV3PropagationPendingError{Code: transport.DNSPeerPendingNativeUnknown}, ""},
		{"generic", "dns_cleanup", errors.New("private peer output"), ""},
		{"other stage", "site_cleanup", &dnsZoneV3PropagationPendingError{Code: transport.DNSPeerPendingNativeUnknown}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodDelete, "/api/v1/domains/3", nil)
			panel.writeDomainDeletionPending(w, r, 3, "gone.example", tc.stage, tc.cause)
			if w.Code != http.StatusAccepted {
				t.Fatalf("status=%d", w.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != domainDeletionPendingStatus || body["reason"] != tc.wantReason ||
				strings.Contains(body["message"], "private peer output") {
				t.Fatalf("pending body=%+v", body)
			}
		})
	}
}
