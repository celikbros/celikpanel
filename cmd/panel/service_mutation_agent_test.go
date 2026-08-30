package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type beginAgentMutationIdentityTestAgent struct {
	durableMutationRPCFixture
	mutateResponse func(*ServiceOperationMutationJob)
}

func (a *beginAgentMutationIdentityTestAgent) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	if err := a.durableMutationRPCFixture.BeginServiceMutation(req, resp); err != nil {
		return err
	}
	if resp.Job != nil && a.mutateResponse != nil {
		a.mutateResponse(resp.Job)
	}
	return nil
}

func TestBeginAgentMutationRequiresExactDurableIdentityTuple(t *testing.T) {
	op := serviceOperation{
		RequestID:   strings.Repeat("a", 32),
		Kind:        serviceOperationKindInstall,
		ServiceID:   "postgresql",
		PackageName: "postgresql-16",
	}
	ownerID := strings.Repeat("b", 32)

	tests := []struct {
		name           string
		mutateResponse func(*ServiceOperationMutationJob)
		wantError      bool
	}{
		{name: "exact tuple is accepted"},
		{
			name: "mismatched kind is rejected",
			mutateResponse: func(job *ServiceOperationMutationJob) {
				job.Kind = serviceOperationKindRuntimeInstall
			},
			wantError: true,
		},
		{
			name: "mismatched target is rejected",
			mutateResponse: func(job *ServiceOperationMutationJob) {
				job.Target = "nginx"
			},
			wantError: true,
		},
		{
			name: "mismatched package is rejected",
			mutateResponse: func(job *ServiceOperationMutationJob) {
				job.PackageName = "postgresql-17"
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &beginAgentMutationIdentityTestAgent{mutateResponse: tt.mutateResponse}
			panel := newPolicyDispatchTestPanel(t, agent)

			job, err := panel.beginAgentMutation(context.Background(), op, ownerID, false)
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "did not grant the requested service mutation lease") {
					t.Fatalf("error = %v, want durable identity mismatch rejection", err)
				}
				if job == nil {
					t.Fatal("rejected response job = nil, want mismatched job for diagnosis")
				}
				return
			}

			if err != nil {
				t.Fatalf("begin agent mutation: %v", err)
			}
			if job == nil {
				t.Fatal("accepted response job = nil")
			}
			if job.RequestID != op.RequestID || job.OwnerID != ownerID ||
				job.Kind != op.Kind || job.Target != op.ServiceID ||
				job.PackageName != op.PackageName || job.Status != agentMutationRunning {
				t.Fatalf("accepted job = %+v, want exact operation identity and running status", job)
			}
		})
	}
}

type terminalAuthorityTestAgent struct {
	durableMutationRPCFixture

	finishTransportError bool
	omitFinishJob        bool
	mutateFinishJob      func(*ServiceOperationMutationJob)
	mutateStatusJob      func(*ServiceOperationMutationJob)
	heartbeatTerminal    bool
	heartbeatSeen        chan struct{}
	heartbeatSeenOnce    sync.Once
	statusCalls          atomic.Int32
	legacyDNSEngineV1    bool
}

func (a *terminalAuthorityTestAgent) PkgFamily(
	_ *transport.Empty,
	out *string,
) error {
	*out = "apt"
	return nil
}

func (a *terminalAuthorityTestAgent) markSucceededLocked(requestID string) *ServiceOperationMutationJob {
	job := a.jobs[requestID]
	if job == nil {
		return nil
	}
	job.Status = agentMutationSucceeded
	job.Phase = "completed"
	identity := agentMutationIdentity{
		RequestID: job.RequestID, OwnerID: job.OwnerID,
		Kind: job.Kind, Target: job.Target, PackageName: job.PackageName,
	}
	if phase, required, err := payloadBoundMutationPublishedPhase(identity); err == nil && required {
		job.Phase = phase
	}
	if a.legacyDNSEngineV1 && identity.Kind == dnsEngineSwitchKind {
		job.Phase = dnsEngineSwitchLegacyPublishedPhasePrefix +
			identity.RequestID + "/" + identity.PackageName
	}
	if a.active == requestID {
		a.active = ""
	}
	return job
}

func TestPayloadBoundMutationSuccessRequiresExactPublishedReceipt(t *testing.T) {
	requestID := strings.Repeat("a", 32)
	ownerID := strings.Repeat("b", 32)
	tests := []struct {
		name        string
		kind        string
		target      string
		packageName string
	}{
		{
			name: "VPN peer", kind: "vpn_peer_sync", target: "wireguard",
			packageName: "vpn-peer-sync/v1:sha256:" + strings.Repeat("1", 64),
		},
		{
			name: "firewall apply", kind: "firewall_apply", target: "nftables",
			packageName: "firewall-apply/v1:sha256:" + strings.Repeat("2", 64),
		},
		{
			name: "firewall sync", kind: "firewall_sync", target: "nftables",
			packageName: "firewall-apply/v1:sha256:" + strings.Repeat("3", 64),
		},
		{
			name: "mail TLS", kind: "mail_tls_sync", target: "mail-tls",
			packageName: "mail-tls-sync/v1:sha256:" + strings.Repeat("4", 64),
		},
		{
			name: "DNS cluster", kind: "dns_cluster_configure", target: "pdns",
			packageName: "dns-cluster-config/v1:sha256:" + strings.Repeat("5", 64),
		},
		{
			name: "DNS zone", kind: "dns_zone_sync", target: "example.test",
			packageName: "dns-zone-sync/v1:sha256:" + strings.Repeat("6", 64),
		},
		{
			name: "DNS zone V3", kind: "dns_zone_sync", target: "v3.example.test",
			packageName: "dns-zone-sync/v3:sha256:" + strings.Repeat("8", 64),
		},
		{
			name: "panel certificate", kind: "panel_certificate_issue", target: "panel.example.test",
			packageName: "panel-certificate-issue/v1:sha256:" + strings.Repeat("7", 64),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity := agentMutationIdentity{
				RequestID: requestID, OwnerID: ownerID,
				Kind: test.kind, Target: test.target, PackageName: test.packageName,
			}
			phase, required, err := payloadBoundMutationPublishedPhase(identity)
			if err != nil {
				t.Fatalf("published phase: %v", err)
			}
			if !required {
				t.Fatal("payload-bound mutation did not require a published receipt")
			}
			job := &agentMutationJob{
				RequestID: requestID, OwnerID: ownerID,
				Kind: test.kind, Target: test.target, PackageName: test.packageName,
				Status: agentMutationSucceeded, Phase: phase,
			}
			if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
				t.Fatalf("exact published receipt rejected: %v", err)
			}

			job.Phase = "completed"
			if err := validateAgentMutationSucceededReceipt(job, identity); !errors.Is(err, errAgentMutationPublishedReceiptMismatch) {
				t.Fatalf("completed success error = %v, want published receipt mismatch", err)
			}
		})
	}
}

func TestDNSEngineSwitchSuccessRequiresExactFinalizedV2Receipt(t *testing.T) {
	requestID := strings.Repeat("8", 32)
	ownerID := strings.Repeat("9", 32)
	qualifier := "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64)
	identity := agentMutationIdentity{
		RequestID: requestID, OwnerID: ownerID,
		Kind: dnsEngineSwitchKind, Target: string(transport.DNSEngineBIND),
		PackageName: qualifier,
	}
	finalizedPhase := dnsEngineSwitchFinalizedPhasePrefix + requestID + "/" + qualifier

	exact := &agentMutationJob{
		RequestID: requestID, OwnerID: ownerID,
		Kind: dnsEngineSwitchKind, Target: string(transport.DNSEngineBIND),
		PackageName: qualifier, Status: agentMutationSucceeded,
		Phase: finalizedPhase,
	}
	if err := validateAgentMutationSucceededReceipt(exact, identity); err != nil {
		t.Fatalf("exact v2 finalized receipt rejected: %v", err)
	}

	legacy := cloneAgentMutationJob(exact)
	legacy.Phase = dnsEngineSwitchLegacyPublishedPhasePrefix + requestID + "/" + qualifier
	legacyErr := validateAgentMutationSucceededReceipt(legacy, identity)
	if !errors.Is(legacyErr, errAgentMutationRecoveryRequired) ||
		!errors.Is(legacyErr, errAgentMutationPublishedReceiptMismatch) ||
		!mutationTerminalUncertain(legacyErr) {
		t.Fatalf("legacy v1 receipt error = %v, want recovery-required uncertainty", legacyErr)
	}

	malformed := cloneAgentMutationJob(exact)
	malformed.Phase = "completed"
	malformedErr := validateAgentMutationSucceededReceipt(malformed, identity)
	if !errors.Is(malformedErr, errAgentMutationRecoveryRequired) ||
		!errors.Is(malformedErr, errAgentMutationPublishedReceiptMismatch) ||
		!mutationTerminalUncertain(malformedErr) {
		t.Fatalf("malformed success receipt error = %v, want recovery-required uncertainty", malformedErr)
	}

	mismatched := cloneAgentMutationJob(exact)
	mismatched.OwnerID = strings.Repeat("7", 32)
	if err := validateAgentMutationSucceededReceipt(mismatched, identity); !errors.Is(err, errAgentMutationIdentityMismatch) {
		t.Fatalf("mismatched v2 identity error = %v", err)
	}
}

func TestStandaloneDNSEngineRPCErrorIsNotSuppressedByLegacyV1Receipt(t *testing.T) {
	agent := &terminalAuthorityTestAgent{legacyDNSEngineV1: true}
	panel := newPolicyDispatchTestPanel(t, agent)

	err := panel.withStandaloneAgentMutation(
		context.Background(),
		dnsEngineSwitchKind,
		string(transport.DNSEnginePowerDNS),
		"dns-engine-switch/v1:sha256:"+strings.Repeat("b", 64),
		func(context.Context, agentMutationBinding) error {
			agent.markActiveSucceeded()
			return errors.New("simulated DNS mutating RPC response loss")
		},
	)
	if !errors.Is(err, errAgentMutationRecoveryRequired) {
		t.Fatalf("legacy v1 RPC-loss result = %v, want recovery required", err)
	}
}

func (a *terminalAuthorityTestAgent) markActiveSucceeded() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.markSucceededLocked(a.active)
}

func (a *terminalAuthorityTestAgent) HeartbeatServiceMutation(
	req *ServiceOperationMutationHeartbeatRequest,
	resp *ServiceOperationMutationResponse,
) error {
	if !a.heartbeatTerminal {
		return a.durableMutationRPCFixture.HeartbeatServiceMutation(req, resp)
	}
	a.mu.Lock()
	job := a.markSucceededLocked(req.RequestID)
	resp.Job = cloneServiceOperationMutationJob(job)
	resp.Error = "service mutation lease is not running"
	a.mu.Unlock()
	if a.heartbeatSeen != nil {
		a.heartbeatSeenOnce.Do(func() { close(a.heartbeatSeen) })
	}
	return nil
}

func (a *terminalAuthorityTestAgent) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.mu.Lock()
	job := a.markSucceededLocked(req.RequestID)
	if !a.omitFinishJob {
		resp.Job = cloneServiceOperationMutationJob(job)
		if resp.Job != nil && a.mutateFinishJob != nil {
			a.mutateFinishJob(resp.Job)
		}
	}
	a.mu.Unlock()
	if a.finishTransportError {
		return errors.New("simulated finish response loss")
	}
	return nil
}

func (a *terminalAuthorityTestAgent) ServiceMutationStatus(
	req *ServiceOperationMutationStatusRequest,
	resp *ServiceOperationMutationResponse,
) error {
	a.statusCalls.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	resp.Job = cloneServiceOperationMutationJob(a.jobs[req.RequestID])
	if resp.Job != nil && a.mutateStatusJob != nil {
		a.mutateStatusJob(resp.Job)
	}
	return nil
}

func TestStandaloneMutationAcceptsExactFinishSuccessAfterCallError(t *testing.T) {
	agent := &terminalAuthorityTestAgent{}
	panel := newPolicyDispatchTestPanel(t, agent)

	err := panel.withStandaloneAgentMutation(
		context.Background(),
		"vpn_peer_sync",
		"wireguard",
		"vpn-peer-sync/v1:sha256:"+strings.Repeat("a", 64),
		func(context.Context, agentMutationBinding) error {
			agent.markActiveSucceeded()
			return errors.New("simulated mutating RPC response loss")
		},
	)
	if err != nil {
		t.Fatalf("exact durable success was not authoritative: %v", err)
	}
}

func TestStandaloneMutationReconcilesExactStatusAfterFinishResponseLoss(t *testing.T) {
	agent := &terminalAuthorityTestAgent{
		finishTransportError: true,
		omitFinishJob:        true,
	}
	panel := newPolicyDispatchTestPanel(t, agent)

	err := panel.withStandaloneAgentMutation(
		context.Background(),
		"vpn_peer_sync",
		"wireguard",
		"vpn-peer-sync/v1:sha256:"+strings.Repeat("b", 64),
		func(context.Context, agentMutationBinding) error {
			return errors.New("simulated mutating RPC response loss")
		},
	)
	if err != nil {
		t.Fatalf("status did not reconcile exact durable success: %v", err)
	}
	if calls := agent.statusCalls.Load(); calls == 0 {
		t.Fatal("terminal status was not queried after the lost Finish response")
	}
}

func TestStandaloneMutationRejectsMismatchedTerminalIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ServiceOperationMutationJob)
	}{
		{
			name: "request",
			mutate: func(job *ServiceOperationMutationJob) {
				job.RequestID = strings.Repeat("1", 32)
			},
		},
		{
			name: "owner",
			mutate: func(job *ServiceOperationMutationJob) {
				job.OwnerID = strings.Repeat("2", 32)
			},
		},
		{
			name: "kind",
			mutate: func(job *ServiceOperationMutationJob) {
				job.Kind = "dns_zone_sync"
			},
		},
		{
			name: "target",
			mutate: func(job *ServiceOperationMutationJob) {
				job.Target = "another-host-target"
			},
		},
		{
			name: "package",
			mutate: func(job *ServiceOperationMutationJob) {
				job.PackageName = "another-payload-commitment"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := &terminalAuthorityTestAgent{mutateFinishJob: test.mutate}
			panel := newPolicyDispatchTestPanel(t, agent)
			err := panel.withStandaloneAgentMutation(
				context.Background(),
				"vpn_peer_sync",
				"wireguard",
				"vpn-peer-sync/v1:sha256:"+strings.Repeat("c", 64),
				func(context.Context, agentMutationBinding) error {
					return errors.New("simulated mutating RPC response loss")
				},
			)
			if !errors.Is(err, errAgentMutationIdentityMismatch) {
				t.Fatalf("mismatched terminal identity error = %v", err)
			}
		})
	}
}

func TestStandaloneMutationRejectsMismatchedStatusIdentity(t *testing.T) {
	agent := &terminalAuthorityTestAgent{
		finishTransportError: true,
		omitFinishJob:        true,
		mutateStatusJob: func(job *ServiceOperationMutationJob) {
			job.PackageName = "spoofed-payload-commitment"
		},
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	err := panel.withStandaloneAgentMutation(
		context.Background(),
		"vpn_peer_sync",
		"wireguard",
		"vpn-peer-sync/v1:sha256:"+strings.Repeat("d", 64),
		func(context.Context, agentMutationBinding) error {
			return errors.New("simulated mutating RPC response loss")
		},
	)
	if !errors.Is(err, errAgentMutationIdentityMismatch) {
		t.Fatalf("mismatched status identity error = %v", err)
	}
}

func TestStandaloneHeartbeatStopsQuietlyOnExactTerminalSuccess(t *testing.T) {
	agent := &terminalAuthorityTestAgent{
		heartbeatTerminal: true,
		heartbeatSeen:     make(chan struct{}),
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	op := serviceOperation{
		RequestID:   strings.Repeat("e", 32),
		Kind:        "vpn_peer_sync",
		ServiceID:   "wireguard",
		PackageName: "vpn-peer-sync/v1:sha256:" + strings.Repeat("e", 64),
	}
	ownerID := strings.Repeat("f", 32)
	job, err := panel.beginAgentMutation(
		context.Background(),
		op,
		ownerID,
		false,
	)
	if err != nil {
		t.Fatalf("begin mutation: %v", err)
	}
	binding := agentMutationBinding{
		MutationRequestID: op.RequestID,
		MutationOwnerID:   ownerID,
	}
	identity := agentMutationIdentityForOperation(op, ownerID)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	stop := panel.startObservedAgentMutationHeartbeat(
		workerCtx,
		cancelWorker,
		binding,
		&identity,
		50*time.Millisecond,
	)
	select {
	case <-agent.heartbeatSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not observe terminal success")
	}
	terminal, heartbeatErr := stop()
	if heartbeatErr != nil {
		t.Fatalf("terminal success produced heartbeat error: %v", heartbeatErr)
	}
	if terminal == nil || terminal.Status != agentMutationSucceeded ||
		!identity.matches(terminal) {
		t.Fatalf("observed terminal = %+v, initial job = %+v", terminal, job)
	}
	select {
	case <-workerCtx.Done():
		t.Fatal("exact terminal success was treated as lease loss")
	default:
	}
}

func TestStandaloneAgentMutationFailureClassification(t *testing.T) {
	backendErr := errors.New("backend exposed /secret/path and token=raw-backend-token")
	heartbeatTransportErr := errors.New("heartbeat transport exposed /run/private.sock")

	tests := []struct {
		name         string
		callErr      error
		heartbeatErr error
		wantCode     string
		wantMessage  string
		wantCauses   []error
	}{
		{
			name:        "healthy lease backend failure",
			callErr:     backendErr,
			wantCode:    errCodeServiceOperationFailed,
			wantMessage: "The privileged host operation did not complete.",
			wantCauses:  []error{backendErr},
		},
		{
			name:         "verified terminal lease loss",
			callErr:      backendErr,
			heartbeatErr: errAgentMutationLeaseLost,
			wantCode:     errCodeServiceOperationLeaseLost,
			wantMessage:  "The privileged host operation lost its agent lease.",
			wantCauses:   []error{backendErr, errAgentMutationLeaseLost},
		},
		{
			name:         "verified owner identity loss",
			heartbeatErr: errAgentMutationIdentityMismatch,
			wantCode:     errCodeServiceOperationLeaseLost,
			wantMessage:  "The privileged host operation lost its agent lease.",
			wantCauses:   []error{errAgentMutationIdentityMismatch},
		},
		{
			name:         "verified terminal backend failure",
			heartbeatErr: errAgentMutationTerminalFailed,
			wantCode:     errCodeServiceOperationFailed,
			wantMessage:  "The privileged host operation did not complete.",
			wantCauses:   []error{errAgentMutationTerminalFailed},
		},
		{
			name:         "heartbeat transport uncertainty",
			callErr:      backendErr,
			heartbeatErr: heartbeatTransportErr,
			wantCode:     errCodeServiceOperationHeartbeatUncertain,
			wantMessage:  "The privileged host operation's agent lease could not be verified.",
			wantCauses:   []error{backendErr, heartbeatTransportErr},
		},
		{
			name: "no failure",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := standaloneAgentMutationFailure(test.callErr, test.heartbeatErr)
			if test.wantCode == "" {
				if failure != nil {
					t.Fatalf("failure = %+v, want nil", failure)
				}
				return
			}
			if failure == nil {
				t.Fatal("failure = nil")
			}
			if failure.Code != test.wantCode || failure.Message != test.wantMessage {
				t.Fatalf(
					"failure code/message = %q/%q, want %q/%q",
					failure.Code,
					failure.Message,
					test.wantCode,
					test.wantMessage,
				)
			}
			for _, wantCause := range test.wantCauses {
				if !errors.Is(failure.Cause, wantCause) {
					t.Fatalf("failure cause = %v, want wrapped %v", failure.Cause, wantCause)
				}
			}
			for _, secret := range []string{"/secret/path", "raw-backend-token", "/run/private.sock"} {
				if strings.Contains(failure.Code, secret) || strings.Contains(failure.Message, secret) {
					t.Fatalf("durable failure leaked %q: %+v", secret, failure)
				}
			}
		})
	}
}

type heartbeatFailureClassificationAgent struct {
	job           *ServiceOperationMutationJob
	responseError string
	transportErr  error
}

func (a *heartbeatFailureClassificationAgent) PkgFamily(
	_ *transport.Empty,
	out *string,
) error {
	*out = "apt"
	return nil
}

func (a *heartbeatFailureClassificationAgent) HeartbeatServiceMutation(
	_ *ServiceOperationMutationHeartbeatRequest,
	response *ServiceOperationMutationResponse,
) error {
	response.Job = cloneServiceOperationMutationJob(a.job)
	response.Error = a.responseError
	return a.transportErr
}

func TestHeartbeatAgentMutationOnlyProvesConcreteLeaseLoss(t *testing.T) {
	requestID := strings.Repeat("1", 32)
	ownerID := strings.Repeat("2", 32)
	binding := agentMutationBinding{
		MutationRequestID: requestID,
		MutationOwnerID:   ownerID,
	}
	running := &ServiceOperationMutationJob{
		RequestID: requestID,
		OwnerID:   ownerID,
		Status:    agentMutationRunning,
	}

	tests := []struct {
		name          string
		job           *ServiceOperationMutationJob
		responseError string
		transportErr  error
		wantErr       bool
		wantLeaseLost bool
		wantFailed    bool
	}{
		{
			name: "healthy exact lease",
			job:  running,
		},
		{
			name: "exact lease-expired terminal",
			job: &ServiceOperationMutationJob{
				RequestID: requestID,
				OwnerID:   ownerID,
				Status:    agentMutationFailed,
				ErrorCode: agentMutationLeaseExpiredErrorCode,
			},
			responseError: "service mutation lease is not owned by this panel",
			wantErr:       true,
			wantLeaseLost: true,
		},
		{
			name: "exact backend-failed terminal is not lease loss",
			job: &ServiceOperationMutationJob{
				RequestID: requestID,
				OwnerID:   ownerID,
				Status:    agentMutationFailed,
				ErrorCode: "bind_stage_failed",
			},
			responseError: "service mutation lease is not owned by this panel",
			wantErr:       true,
			wantFailed:    true,
		},
		{
			name: "different owner",
			job: &ServiceOperationMutationJob{
				RequestID: requestID,
				OwnerID:   strings.Repeat("3", 32),
				Status:    agentMutationRunning,
			},
			wantErr:       true,
			wantLeaseLost: true,
		},
		{
			name:          "running response error is uncertain",
			job:           running,
			responseError: "agent manager state could not be read",
			wantErr:       true,
		},
		{
			name:    "missing job is uncertain",
			wantErr: true,
		},
		{
			name:         "transport failure is uncertain",
			transportErr: errors.New("simulated transport loss"),
			wantErr:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := &heartbeatFailureClassificationAgent{
				job:           test.job,
				responseError: test.responseError,
				transportErr:  test.transportErr,
			}
			panel := newPolicyDispatchTestPanel(t, agent)
			_, err := panel.heartbeatAgentMutation(
				context.Background(),
				binding,
				"",
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("heartbeat error = %v, wantErr=%v", err, test.wantErr)
			}
			if got := errors.Is(err, errAgentMutationLeaseLost); got != test.wantLeaseLost {
				t.Fatalf(
					"heartbeat lease-lost classification = %v, want %v (error=%v)",
					got,
					test.wantLeaseLost,
					err,
				)
			}
			if got := errors.Is(err, errAgentMutationTerminalFailed); got != test.wantFailed {
				t.Fatalf(
					"heartbeat terminal-failed classification = %v, want %v (error=%v)",
					got,
					test.wantFailed,
					err,
				)
			}
		})
	}
}

type standaloneFailureFinishAgent struct {
	durableMutationRPCFixture

	captureMu sync.Mutex
	finishes  []ServiceOperationMutationFinishRequest
}

func (a *standaloneFailureFinishAgent) PkgFamily(
	_ *transport.Empty,
	out *string,
) error {
	*out = "apt"
	return nil
}

func (a *standaloneFailureFinishAgent) FinishServiceMutation(
	request *ServiceOperationMutationFinishRequest,
	response *ServiceOperationMutationResponse,
) error {
	a.captureMu.Lock()
	a.finishes = append(a.finishes, *request)
	a.captureMu.Unlock()
	return a.durableMutationRPCFixture.FinishServiceMutation(request, response)
}

func (a *standaloneFailureFinishAgent) finishRequests() []ServiceOperationMutationFinishRequest {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	return append([]ServiceOperationMutationFinishRequest(nil), a.finishes...)
}

func TestStandaloneBackendFailureFinishesWithSanitizedGeneralCode(t *testing.T) {
	agent := &standaloneFailureFinishAgent{}
	panel := newPolicyDispatchTestPanel(t, agent)
	backendErr := errors.New("backend exposed /etc/private.conf and token=backend-secret")

	err := panel.withStandaloneAgentMutation(
		context.Background(),
		"service_action",
		"nginx",
		"",
		func(context.Context, agentMutationBinding) error {
			return backendErr
		},
	)
	if !errors.Is(err, backendErr) {
		t.Fatalf("standalone failure = %v, want backend cause", err)
	}

	finishes := agent.finishRequests()
	if len(finishes) != 1 {
		t.Fatalf("FinishServiceMutation calls = %d, want 1", len(finishes))
	}
	finish := finishes[0]
	if finish.Success {
		t.Fatal("ordinary backend failure was finished as success")
	}
	if finish.FailureCode != errCodeServiceOperationFailed ||
		finish.Message != "The privileged host operation did not complete." {
		t.Fatalf(
			"finish code/message = %q/%q",
			finish.FailureCode,
			finish.Message,
		)
	}
	if finish.FailureCode == errCodeServiceOperationLeaseLost {
		t.Fatal("ordinary backend failure was mislabeled as lease loss")
	}
	for _, secret := range []string{"/etc/private.conf", "backend-secret"} {
		if strings.Contains(finish.FailureCode, secret) ||
			strings.Contains(finish.Message, secret) {
			t.Fatalf("FinishServiceMutation leaked %q: %+v", secret, finish)
		}
	}

	agent.mu.Lock()
	job := cloneServiceOperationMutationJob(agent.jobs[finish.RequestID])
	agent.mu.Unlock()
	if job == nil || job.Status != agentMutationFailed ||
		job.ErrorCode != errCodeServiceOperationFailed ||
		job.ErrorMessage != "The privileged host operation did not complete." {
		t.Fatalf("durable terminal job = %+v", job)
	}
}

func TestHeartbeatUncertainFailureIsAcceptedAsTerminalFailed(t *testing.T) {
	agent := &standaloneFailureFinishAgent{}
	panel := newPolicyDispatchTestPanel(t, agent)
	op := serviceOperation{
		RequestID: strings.Repeat("4", 32),
		Kind:      "service_action",
		ServiceID: "nginx",
	}
	ownerID := strings.Repeat("5", 32)
	if _, err := panel.beginAgentMutation(
		context.Background(),
		op,
		ownerID,
		false,
	); err != nil {
		t.Fatalf("begin mutation: %v", err)
	}

	heartbeatErr := errors.New("heartbeat transport exposed /run/private.sock")
	failure := standaloneAgentMutationFailure(nil, heartbeatErr)
	binding := agentMutationBinding{
		MutationRequestID: op.RequestID,
		MutationOwnerID:   ownerID,
	}
	terminal, err := panel.finishAgentMutationContext(
		context.Background(),
		binding,
		false,
		failure,
	)
	if err != nil {
		t.Fatalf("finish heartbeat-uncertain mutation: %v", err)
	}
	if terminal == nil || terminal.Status != agentMutationFailed ||
		terminal.ErrorCode != errCodeServiceOperationHeartbeatUncertain ||
		terminal.ErrorMessage != "The privileged host operation's agent lease could not be verified." {
		t.Fatalf("terminal heartbeat-uncertain job = %+v", terminal)
	}
	if strings.Contains(terminal.ErrorMessage, "/run/private.sock") {
		t.Fatalf("terminal message leaked heartbeat detail: %q", terminal.ErrorMessage)
	}
}

func TestStandaloneTerminalFailureCodesAreNotStartupResumeSignals(t *testing.T) {
	for _, code := range []string{
		errCodeServiceOperationFailed,
		errCodeServiceOperationHeartbeatUncertain,
	} {
		t.Run(code, func(t *testing.T) {
			if agentMutationCanResume(&agentMutationJob{
				Status:    agentMutationFailed,
				ErrorCode: code,
			}) {
				t.Fatalf("terminal code %q was treated as a restart-resume signal", code)
			}
		})
	}
}
