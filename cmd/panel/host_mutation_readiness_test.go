package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func decodeHostMutationReadiness(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) transport.HostMutationReadinessResponse {
	t.Helper()
	var response transport.HostMutationReadinessResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode readiness %q: %v", recorder.Body.String(), err)
	}
	return response
}

func readHostMutationReadinessForTest(
	t *testing.T,
	fixture serviceOperationTestFixture,
) transport.HostMutationReadinessResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := serviceOperationAdminRequest(
		t, http.MethodGet, hostMutationReadinessPath, "", fixture.userID,
	)
	fixture.panel.handleHostMutationReadiness(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("readiness status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if cache := recorder.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control=%q want no-store", cache)
	}
	return decodeHostMutationReadiness(t, recorder)
}

func TestHostMutationReadinessFailsClosedWithStableReasons(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		response := readHostMutationReadinessForTest(t, fixture)
		if !response.Ready || response.Code != "" || response.Reason != "" {
			t.Fatalf("readiness = %+v", response)
		}
	})

	t.Run("agent package manager busy", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		fixture.agent.mutationReadiness = transport.HostMutationReadinessResponse{
			Code:   transport.HostMutationBusy,
			Reason: transport.HostMutationReasonPackageManager,
		}
		response := readHostMutationReadinessForTest(t, fixture)
		if response.Ready || response.Code != transport.HostMutationBusy ||
			response.Reason != transport.HostMutationReasonPackageManager {
			t.Fatalf("readiness = %+v", response)
		}
	})

	t.Run("local lock busy", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		fixture.panel.serviceMutationMu.Lock()
		response := readHostMutationReadinessForTest(t, fixture)
		fixture.panel.serviceMutationMu.Unlock()
		if response.Ready || response.Code != transport.HostMutationBusy ||
			response.Reason != transport.HostMutationReasonPanelOperation {
			t.Fatalf("readiness = %+v", response)
		}
	})

	t.Run("durable panel operation active", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		if _, err := fixture.panel.createServiceOperation(
			context.Background(), serviceOperationKindInstall, "nginx", "", serviceOperationActor{},
		); err != nil {
			t.Fatal(err)
		}
		response := readHostMutationReadinessForTest(t, fixture)
		if response.Ready || response.Code != transport.HostMutationBusy ||
			response.Reason != transport.HostMutationReasonPanelOperation {
			t.Fatalf("readiness = %+v", response)
		}
	})

	t.Run("invalid agent response is unavailable", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		fixture.agent.mutationReadiness = transport.HostMutationReadinessResponse{
			Code: "UNKNOWN", Reason: "invented",
		}
		response := readHostMutationReadinessForTest(t, fixture)
		if response.Ready || response.Code != transport.HostMutationUnavailable ||
			response.Reason != transport.HostMutationReasonStateUnverified {
			t.Fatalf("readiness = %+v", response)
		}
	})

	t.Run("contradictory ready response is unavailable", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		fixture.agent.mutationReadiness = transport.HostMutationReadinessResponse{
			Ready:  true,
			Code:   transport.HostMutationBusy,
			Reason: transport.HostMutationReasonPackageManager,
		}
		response := readHostMutationReadinessForTest(t, fixture)
		if response.Ready || response.Code != transport.HostMutationUnavailable ||
			response.Reason != transport.HostMutationReasonStateUnverified {
			t.Fatalf("readiness = %+v", response)
		}
	})

	t.Run("agent failure is unavailable", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		fixture.agent.mutationReadinessErr = errors.New("simulated read failure")
		response := readHostMutationReadinessForTest(t, fixture)
		if response.Ready || response.Code != transport.HostMutationUnavailable ||
			response.Reason != transport.HostMutationReasonStateUnverified {
			t.Fatalf("readiness = %+v", response)
		}
	})
}

func TestHostMutationReadinessRequiresAdminGET(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)

	nonAdmin := httptest.NewRequest(http.MethodGet, hostMutationReadinessPath, nil)
	nonAdmin = nonAdmin.WithContext(context.WithValue(
		nonAdmin.Context(), callerKey, &Caller{ID: fixture.userID, Role: roleCustomer},
	))
	nonAdminRecorder := httptest.NewRecorder()
	fixture.panel.handleHostMutationReadiness(nonAdminRecorder, nonAdmin)
	if nonAdminRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d body=%s", nonAdminRecorder.Code, nonAdminRecorder.Body.String())
	}

	postRecorder := httptest.NewRecorder()
	post := serviceOperationAdminRequest(
		t, http.MethodPost, hostMutationReadinessPath, "", fixture.userID,
	)
	fixture.panel.handleHostMutationReadiness(postRecorder, post)
	if postRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	if allow := postRecorder.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("POST Allow=%q want GET", allow)
	}
}

type structuredBusyMutationAgent struct{}

func (*structuredBusyMutationAgent) BeginServiceMutation(
	_ *transport.ServiceMutationBeginRequest,
	response *transport.ServiceMutationResponse,
) error {
	response.ErrorCode = transport.HostMutationBusy
	response.Error = "untrusted remote detail"
	return nil
}

func TestStructuredHostMutationBusyBecomesSafeHTTP409(t *testing.T) {
	panel := newPolicyDispatchTestPanel(t, &structuredBusyMutationAgent{})
	_, err := panel.beginAgentMutation(
		context.Background(),
		serviceOperation{
			RequestID: strings.Repeat("a", 32), Kind: serviceOperationKindInstall,
			ServiceID: "nginx",
		},
		strings.Repeat("b", 32),
		false,
	)
	if !errors.Is(err, errHostMutationBusy) {
		t.Fatalf("begin error = %v, want host mutation busy", err)
	}

	recorder := httptest.NewRecorder()
	writeServerError(recorder, fmt.Errorf("admit host mutation: %w", err))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != transport.HostMutationBusy || strings.Contains(body.Error, "untrusted") {
		t.Fatalf("body = %+v", body)
	}

	failure := operationStartFailure(errHostMutationBusy)
	if failure.Code != transport.HostMutationBusy || failure.Message != body.Error {
		t.Fatalf("async failure = %+v", failure)
	}
}
