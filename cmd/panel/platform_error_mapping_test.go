package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"
)

func TestWriteServerErrorClassifiesOnlyLocalPlatformSentinels(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "capability denied",
			err:         fmt.Errorf("method context: %w", errAgentRPCPlatformCapabilityDenied),
			wantStatus:  http.StatusConflict,
			wantCode:    errCodePlatformCapabilityUnavailable,
			wantMessage: "this operation is unavailable on the connected server platform",
		},
		{
			name:        "identity unavailable",
			err:         fmt.Errorf("identity lookup: %w", errAgentRPCPlatformIdentityUnavailable),
			wantStatus:  http.StatusBadGateway,
			wantCode:    errCodePlatformIdentityUnavailable,
			wantMessage: "the connected server platform identity could not be verified",
		},
		{
			name: "joined sentinel cannot hide unrelated failure",
			err: errors.Join(
				errAgentRPCPlatformCapabilityDenied,
				errors.New("remote secret and command output"),
			),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantMessage: "internal server error",
		},
		{
			name:        "transport",
			err:         errors.New("connection reset"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantMessage: "internal server error",
		},
		{
			name:        "remote RPC",
			err:         rpc.ServerError("remote detail"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantMessage: "internal server error",
		},
		{
			name:        "context canceled",
			err:         context.Canceled,
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantMessage: "internal server error",
		},
		{
			name:        "context deadline",
			err:         context.DeadlineExceeded,
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantMessage: "internal server error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeServerError(recorder, test.err)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode || body.Error != test.wantMessage {
				t.Fatalf("body=%+v", body)
			}
			if strings.Contains(recorder.Body.String(), "remote secret") {
				t.Fatalf("untrusted detail leaked in body: %s", recorder.Body.String())
			}
		})
	}
}

func TestWriteAgentErrorUsesPlatformClassifier(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "capability",
			err:        fmt.Errorf("local authorization: %w", errAgentRPCPlatformCapabilityDenied),
			wantStatus: http.StatusConflict,
			wantCode:   errCodePlatformCapabilityUnavailable,
		},
		{
			name:       "identity",
			err:        fmt.Errorf("local identity: %w", errAgentRPCPlatformIdentityUnavailable),
			wantStatus: http.StatusBadGateway,
			wantCode:   errCodePlatformIdentityUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeAgentError(recorder, test.err, "remote agent detail")
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.wantStatus || body.Code != test.wantCode ||
				strings.Contains(recorder.Body.String(), "remote agent detail") {
				t.Fatalf("status=%d body=%+v", recorder.Code, body)
			}
		})
	}
}

func TestPlatformOperationFailureCodesAreDurable(t *testing.T) {
	factories := []struct {
		name string
		make func(error) *serviceOperationFailure
	}{
		{name: "service", make: serviceInstallFailure},
		{name: "node", make: nodeInstallFailure},
		{name: "mail profile", make: mailProfileInstallFailure},
	}
	sentinels := []struct {
		name string
		err  error
		code string
	}{
		{name: "capability", err: errAgentRPCPlatformCapabilityDenied, code: errCodePlatformCapabilityUnavailable},
		{name: "identity", err: errAgentRPCPlatformIdentityUnavailable, code: errCodePlatformIdentityUnavailable},
	}

	for _, factory := range factories {
		for _, sentinel := range sentinels {
			t.Run(factory.name+"/"+sentinel.name, func(t *testing.T) {
				fixture := newServiceOperationTestFixture(t)
				operation, err := fixture.panel.createServiceOperation(
					context.Background(),
					serviceOperationKindInstall,
					"nginx",
					"",
					serviceOperationActor{UserID: fixture.userID},
				)
				if err != nil {
					t.Fatal(err)
				}
				if err := fixture.panel.markServiceOperationRunning(
					context.Background(), operation.ID, "preflight",
				); err != nil {
					t.Fatal(err)
				}
				cause := fmt.Errorf("sensitive runner detail: %w", sentinel.err)
				failure := factory.make(cause)
				if failure.Code != sentinel.code || !errors.Is(failure.Cause, sentinel.err) {
					t.Fatalf("failure=%+v", failure)
				}
				classification, _ := classifyAgentRPCPlatformError(sentinel.err)
				if failure.Message != classification.Message {
					t.Fatalf("message=%q want %q", failure.Message, classification.Message)
				}
				if err := fixture.panel.finishServiceOperationFailed(
					context.Background(),
					operation.ID,
					"preflight",
					serviceOperationResult{"success": false},
					failure,
				); err != nil {
					t.Fatal(err)
				}
				loaded, err := fixture.panel.serviceOperationByID(context.Background(), operation.ID)
				if err != nil {
					t.Fatal(err)
				}
				if loaded.Error == nil || loaded.Error.Code != sentinel.code ||
					loaded.Error.Message != classification.Message ||
					strings.Contains(loaded.Error.Message, "sensitive runner detail") {
					t.Fatalf("durable operation=%+v", loaded)
				}
			})
		}
	}
}

func TestPartialOperationFailureCodeOverridesPlatformCause(t *testing.T) {
	failure := firewallSyncFailure(
		fmt.Errorf("after verified mutation: %w", errAgentRPCPlatformCapabilityDenied),
	)
	if failure.Code != "firewall_sync_failed" {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestGenericOperationFailuresRetainExistingCodes(t *testing.T) {
	tests := []struct {
		name     string
		make     func(error) *serviceOperationFailure
		wantCode string
	}{
		{name: "service", make: serviceInstallFailure, wantCode: "service_install_failed"},
		{name: "node", make: nodeInstallFailure, wantCode: "node_runtime_install_failed"},
		{name: "mail profile", make: mailProfileInstallFailure, wantCode: "mail_profile_install_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := test.make(errors.New("raw transport detail"))
			if failure.Code != test.wantCode ||
				strings.Contains(failure.Message, "raw transport detail") {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
}

func TestUpdateEmailPasswordPreservesOnlyLocalPlatformSentinels(t *testing.T) {
	tests := []struct {
		name       string
		family     string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "capability",
			family:     "dnf",
			wantStatus: http.StatusConflict,
			wantCode:   errCodePlatformCapabilityUnavailable,
		},
		{
			name:       "identity",
			family:     "",
			wantStatus: http.StatusBadGateway,
			wantCode:   errCodePlatformIdentityUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := &mailMutationPanelAgent{passwordApplied: true}
			panel, domainID := newMailMutationPanel(t, agent)
			panel.pkgFamilyMu.Lock()
			panel.pkgFamilyVal = test.family
			panel.pkgFamilyMu.Unlock()
			if _, err := panel.db.GetDB().Exec(
				"INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb) "+
					"VALUES (6401, ?, 'user@example.com', 'sentinel-hash', 100)",
				domainID,
			); err != nil {
				t.Fatal(err)
			}

			response := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPut,
				"/mail/accounts/password",
				strings.NewReader(fmt.Sprintf(
					"{%cid%c:6401,%cnew_password%c:%creplacement-password%c}",
					34, 34, 34, 34, 34, 34,
				)),
			)
			panel.handleUpdateEmailPassword(response, request, domainID)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode ||
				strings.Contains(response.Body.String(), "replacement-password") ||
				strings.Contains(strings.ToLower(response.Body.String()), "user@example.com") {
				t.Fatalf("body=%+v", body)
			}
			if calls := agent.passwordCallState(); len(calls) != 0 {
				t.Fatalf("password RPC reached agent: %+v", calls)
			}
		})
	}
}
