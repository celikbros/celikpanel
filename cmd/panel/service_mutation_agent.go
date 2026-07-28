package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	agentMutationRunning    = "running"
	agentMutationCancelling = "cancelling"
	agentMutationOrphaned   = "orphaned"
	agentMutationSucceeded  = "succeeded"
	agentMutationFailed     = "failed"

	panelMutationHeartbeatInterval = 5 * time.Second
	panelMutationFinishTimeout     = 15 * time.Second
	panelMutationRecoveryTimeout   = 90 * time.Second
)

type agentMutationBinding struct {
	MutationRequestID string `json:"mutation_request_id"`
	MutationOwnerID   string `json:"mutation_owner_id"`
}

type agentMutationJob struct {
	RequestID      string    `json:"request_id"`
	OwnerID        string    `json:"owner_id"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	PackageName    string    `json:"package_name,omitempty"`
	Status         string    `json:"status"`
	Phase          string    `json:"phase"`
	Attempt        int       `json:"attempt"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	DeadlineAt     time.Time `json:"deadline_at"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	WorkerPID      int       `json:"worker_pid,omitempty"`
	WorkerStarted  string    `json:"worker_started,omitempty"`
}

type agentMutationResponse struct {
	Job   *agentMutationJob `json:"job,omitempty"`
	Error string            `json:"error,omitempty"`
}

type panelMutationContextKey struct{}

func withPanelMutationBinding(ctx context.Context, binding agentMutationBinding) context.Context {
	return context.WithValue(ctx, panelMutationContextKey{}, binding)
}

func panelMutationBinding(ctx context.Context) (agentMutationBinding, error) {
	binding, ok := ctx.Value(panelMutationContextKey{}).(agentMutationBinding)
	if !ok || !validServiceOperationID(binding.MutationRequestID) ||
		!validServiceOperationID(binding.MutationOwnerID) {
		return agentMutationBinding{}, errors.New("durable service mutation binding is missing")
	}
	return binding, nil
}

func agentMutationActive(status string) bool {
	return status == agentMutationRunning ||
		status == agentMutationCancelling ||
		status == agentMutationOrphaned
}

func (p *Panel) beginAgentMutation(
	ctx context.Context,
	op serviceOperation,
	ownerID string,
	resume bool,
) (*agentMutationJob, error) {
	var response agentMutationResponse
	err := p.agentClient.CallContext(ctx, "Agent.BeginServiceMutation", &struct {
		RequestID   string `json:"request_id"`
		OwnerID     string `json:"owner_id"`
		Kind        string `json:"kind"`
		Target      string `json:"target"`
		PackageName string `json:"package_name,omitempty"`
		Resume      bool   `json:"resume,omitempty"`
	}{
		RequestID: op.RequestID, OwnerID: ownerID, Kind: op.Kind,
		Target: op.ServiceID, PackageName: op.PackageName, Resume: resume,
	}, &response)
	if err != nil {
		return nil, fmt.Errorf("begin agent service mutation: %w", err)
	}
	if response.Error != "" {
		return response.Job, errors.New(response.Error)
	}
	if response.Job == nil || response.Job.RequestID != op.RequestID ||
		response.Job.OwnerID != ownerID || response.Job.Status != agentMutationRunning {
		return response.Job, errors.New("agent did not grant the requested service mutation lease")
	}
	return response.Job, nil
}

func (p *Panel) heartbeatAgentMutation(
	ctx context.Context,
	binding agentMutationBinding,
	phase string,
) (*agentMutationJob, error) {
	var response agentMutationResponse
	err := p.agentClient.CallContext(ctx, "Agent.HeartbeatServiceMutation", &struct {
		RequestID string `json:"request_id"`
		OwnerID   string `json:"owner_id"`
		Phase     string `json:"phase,omitempty"`
	}{
		RequestID: binding.MutationRequestID,
		OwnerID:   binding.MutationOwnerID,
		Phase:     strings.TrimSpace(phase),
	}, &response)
	if err != nil {
		return nil, err
	}
	if response.Error != "" {
		return response.Job, errors.New(response.Error)
	}
	if response.Job == nil ||
		response.Job.RequestID != binding.MutationRequestID ||
		response.Job.OwnerID != binding.MutationOwnerID ||
		response.Job.Status != agentMutationRunning {
		return response.Job, errors.New("agent service mutation lease is no longer running")
	}
	return response.Job, nil
}

func (p *Panel) statusAgentMutation(
	ctx context.Context,
	requestID string,
) (*agentMutationJob, error) {
	var response agentMutationResponse
	if err := p.agentClient.CallContext(ctx, "Agent.ServiceMutationStatus", &struct {
		RequestID string `json:"request_id,omitempty"`
	}{RequestID: requestID}, &response); err != nil {
		return nil, err
	}
	if response.Error != "" {
		return response.Job, errors.New(response.Error)
	}
	return response.Job, nil
}

func (p *Panel) cancelAgentMutation(
	ctx context.Context,
	job *agentMutationJob,
	code, message string,
) error {
	if job == nil || !validServiceOperationID(job.RequestID) ||
		!validServiceOperationID(job.OwnerID) {
		return errors.New("agent mutation has no reusable durable owner identity")
	}
	var response agentMutationResponse
	if err := p.agentClient.CallContext(ctx, "Agent.CancelServiceMutation", &struct {
		RequestID      string `json:"request_id"`
		ExpectedOwner  string `json:"expected_owner"`
		Reason         string `json:"reason,omitempty"`
		FailureCode    string `json:"failure_code,omitempty"`
		FailureMessage string `json:"failure_message,omitempty"`
	}{
		RequestID: job.RequestID, ExpectedOwner: job.OwnerID,
		Reason: "panel_restart_reconcile", FailureCode: code, FailureMessage: message,
	}, &response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func (p *Panel) finishAgentMutation(
	binding agentMutationBinding,
	success bool,
	failure *serviceOperationFailure,
) (*agentMutationJob, error) {
	ctx, cancel := context.WithTimeout(context.Background(), panelMutationFinishTimeout)
	defer cancel()
	request := struct {
		RequestID   string `json:"request_id"`
		OwnerID     string `json:"owner_id"`
		Success     bool   `json:"success"`
		FailureCode string `json:"failure_code,omitempty"`
		Message     string `json:"message,omitempty"`
	}{
		RequestID: binding.MutationRequestID,
		OwnerID:   binding.MutationOwnerID,
		Success:   success,
	}
	if failure != nil {
		request.FailureCode = failure.Code
		request.Message = failure.Message
	}
	var response agentMutationResponse
	if err := p.agentClient.CallContext(
		ctx,
		"Agent.FinishServiceMutation",
		&request,
		&response,
	); err != nil {
		return nil, err
	}
	if response.Error != "" {
		return response.Job, errors.New(response.Error)
	}
	if response.Job == nil ||
		response.Job.RequestID != binding.MutationRequestID ||
		response.Job.OwnerID != binding.MutationOwnerID ||
		agentMutationActive(response.Job.Status) {
		return response.Job, errors.New("agent did not durably finish the service mutation")
	}
	return response.Job, nil
}

func (p *Panel) startAgentMutationHeartbeat(
	ctx context.Context,
	cancel context.CancelFunc,
	binding agentMutationBinding,
) func() error {
	errCh := make(chan error, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(panelMutationHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(context.Background(), panelMutationHeartbeatInterval)
				_, err := p.heartbeatAgentMutation(pingCtx, binding, "")
				pingCancel()
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	var stopErr error
	return func() error {
		once.Do(func() {
			close(stop)
			// Wait for an in-flight heartbeat before the caller commits a
			// terminal result. Otherwise a late heartbeat failure can race a
			// successful FinishServiceMutation call.
			// Çağıran terminal sonucu kaydetmeden önce devam eden heartbeat'i
			// bekle. Aksi halde geç gelen heartbeat hatası başarılı
			// FinishServiceMutation çağrısıyla yarışabilir.
			<-done
			select {
			case stopErr = <-errCh:
			default:
			}
		})
		return stopErr
	}
}

// withStandaloneAgentMutation gives a synchronous privileged RPC the same
// durable host lease used by asynchronous installs. If the panel dies, startup
// recovery sees the unmatched agent job and waits for its active step to stop
// before admitting another mutation.
// withStandaloneAgentMutation, eşzamanlı ayrıcalıklı RPC'ye asenkron
// kurulumlarla aynı kalıcı makine kirasını verir. Panel ölürse başlangıç
// kurtarması eşleşmeyen agent işini görür ve başka değişiklik kabul etmeden
// önce etkin adımın durmasını bekler.
func (p *Panel) withStandaloneAgentMutation(
	ctx context.Context,
	kind, target, packageName string,
	call func(context.Context, agentMutationBinding) error,
) error {
	if binding, err := panelMutationBinding(ctx); err == nil {
		return call(ctx, binding)
	}
	requestID, err := newServiceOperationID()
	if err != nil {
		return fmt.Errorf("create durable mutation request identity: %w", err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return fmt.Errorf("create durable mutation owner identity: %w", err)
	}
	op := serviceOperation{
		RequestID:   requestID,
		Kind:        strings.TrimSpace(kind),
		ServiceID:   strings.TrimSpace(target),
		PackageName: strings.TrimSpace(packageName),
	}
	beginCtx, beginCancel := context.WithTimeout(ctx, panelMutationFinishTimeout)
	job, err := p.beginAgentMutation(beginCtx, op, ownerID, false)
	beginCancel()
	if err != nil {
		return err
	}
	binding := agentMutationBinding{
		MutationRequestID: requestID,
		MutationOwnerID:   ownerID,
	}
	deadline := job.DeadlineAt
	if deadline.IsZero() {
		deadline = time.Now().Add(45 * time.Minute)
	}
	workerCtx, cancelWorker := context.WithDeadline(ctx, deadline)
	boundCtx := withPanelMutationBinding(workerCtx, binding)
	stopHeartbeat := p.startAgentMutationHeartbeat(workerCtx, cancelWorker, binding)

	callErr := call(boundCtx, binding)
	heartbeatErr := stopHeartbeat()
	cancelWorker()
	if callErr == nil && heartbeatErr != nil {
		callErr = fmt.Errorf("agent service mutation heartbeat: %w", heartbeatErr)
	}
	var failure *serviceOperationFailure
	if callErr != nil {
		failure = operationFailure(
			errCodeServiceOperationLeaseLost,
			"The privileged host operation did not complete.",
			callErr,
		)
	}
	terminal, finishErr := p.finishAgentMutation(binding, callErr == nil, failure)
	if finishErr != nil {
		return fmt.Errorf("finish durable agent mutation: %w", finishErr)
	}
	if callErr == nil && terminal.Status != agentMutationSucceeded {
		return fmt.Errorf("agent committed standalone mutation with status %s", terminal.Status)
	}
	return callErr
}

func (p *Panel) waitAgentMutationTerminal(
	ctx context.Context,
	requestID string,
) (*agentMutationJob, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := p.statusAgentMutation(ctx, requestID)
		if err != nil {
			return nil, err
		}
		if job == nil || !agentMutationActive(job.Status) {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-ticker.C:
		}
	}
}
