package main

import (
	"context"
	"strings"
	"testing"
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
