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
