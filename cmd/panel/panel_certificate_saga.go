package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	serviceOperationKindPanelCertificate = "panel_certificate_issue"
	panelCertificateSagaVersion          = 1
	panelCertificateSagaDataMaxBytes     = 32 << 10

	panelCertificatePhaseQueued              = "panel-certificate/v1/queued"
	panelCertificatePhasePreflightPlanning   = "panel-certificate/v1/preflight_planning"
	panelCertificatePhasePreflightSkipped    = "panel-certificate/v1/preflight_skipped_disabled"
	panelCertificatePhasePreflightChild      = "panel-certificate/v1/preflight_child"
	panelCertificatePhaseCertificatePlanning = "panel-certificate/v1/certificate_planning"
	panelCertificatePhaseCertificateChild    = "panel-certificate/v1/certificate_child"
	panelCertificatePhaseCompensatePlanning  = "panel-certificate/v1/compensation_pending"
	panelCertificatePhaseCompensateSkipped   = "panel-certificate/v1/compensation_skipped_disabled"
	panelCertificatePhaseCompensateChild     = "panel-certificate/v1/compensation_child"
	panelCertificatePhaseFinalPlanning       = "panel-certificate/v1/final_firewall_pending"
	panelCertificatePhaseFinalSkipped        = "panel-certificate/v1/final_firewall_skipped_disabled"
	panelCertificatePhaseFinalChild          = "panel-certificate/v1/final_firewall_child"
	panelCertificatePhaseFailedCompensated   = "panel-certificate/v1/failed_compensated"
)

var panelCertificateSagaRetryDelay = 2 * time.Second

const (
	panelCertificateChildPreflight    = "firewall_preflight"
	panelCertificateChildCertificate  = "certificate_issue"
	panelCertificateChildCompensation = "firewall_compensation"
	panelCertificateChildFinal        = "firewall_final"
)

var errPanelCertificateSagaIdentity = errors.New("panel certificate saga child identity mismatch")
var errPanelCertificateSagaTransient = errors.New("panel certificate saga child is not terminal")

type panelCertificateSagaFirewall struct {
	Enabled  bool  `json:"enabled"`
	Persist  bool  `json:"persist"`
	TCPPorts []int `json:"tcp_ports"`
	UDPPorts []int `json:"udp_ports"`
}

type panelCertificateSagaChild struct {
	Step      string                        `json:"step"`
	RequestID string                        `json:"request_id"`
	OwnerID   string                        `json:"owner_id"`
	Kind      string                        `json:"kind"`
	Target    string                        `json:"target"`
	Qualifier string                        `json:"qualifier"`
	Firewall  *panelCertificateSagaFirewall `json:"firewall,omitempty"`
}

type panelCertificateSagaData struct {
	Version              int                        `json:"version"`
	Domain               string                     `json:"domain"`
	Email                string                     `json:"email"`
	TLSDir               string                     `json:"tls_dir"`
	ExpectedBuildCommit  string                     `json:"expected_build_commit"`
	CertificateQualifier string                     `json:"certificate_qualifier"`
	CertificateCommitted bool                       `json:"certificate_committed"`
	ExpiresAt            string                     `json:"expires_at,omitempty"`
	Detail               string                     `json:"detail,omitempty"`
	FailureCode          string                     `json:"failure_code,omitempty"`
	FailureMessage       string                     `json:"failure_message,omitempty"`
	Child                *panelCertificateSagaChild `json:"child,omitempty"`
}

func newPanelCertificateSagaData(
	commitment mutationpayload.PanelCertificateIssueCommitment,
) panelCertificateSagaData {
	return panelCertificateSagaData{
		Version:              panelCertificateSagaVersion,
		Domain:               commitment.Domain,
		Email:                commitment.Email,
		TLSDir:               commitment.TLSDir,
		ExpectedBuildCommit:  commitment.ExpectedBuildCommit,
		CertificateQualifier: commitment.Qualifier,
	}
}

func canonicalPanelCertificateSagaData(
	op serviceOperation,
	data panelCertificateSagaData,
) (string, error) {
	if err := validatePanelCertificateSagaData(op, data); err != nil {
		return "", err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("encode panel certificate operation data: %w", err)
	}
	if len(raw) == 0 || len(raw) > panelCertificateSagaDataMaxBytes {
		return "", errors.New("panel certificate operation data exceeds its size limit")
	}
	return string(raw), nil
}

func decodePanelCertificateSagaData(op serviceOperation) (panelCertificateSagaData, error) {
	raw := []byte(op.OperationData)
	if len(raw) == 0 || len(raw) > panelCertificateSagaDataMaxBytes {
		return panelCertificateSagaData{}, errors.New("panel certificate operation data has invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var data panelCertificateSagaData
	if err := decoder.Decode(&data); err != nil {
		return panelCertificateSagaData{}, fmt.Errorf("decode panel certificate operation data: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return panelCertificateSagaData{}, fmt.Errorf("decode panel certificate operation data trailer: %w", err)
	}
	canonical, err := canonicalPanelCertificateSagaData(op, data)
	if err != nil {
		return panelCertificateSagaData{}, err
	}
	if !bytes.Equal(raw, []byte(canonical)) {
		return panelCertificateSagaData{}, errors.New("panel certificate operation data is not canonical JSON")
	}
	return data, nil
}

func validatePanelCertificateSagaData(op serviceOperation, data panelCertificateSagaData) error {
	if op.Kind != serviceOperationKindPanelCertificate ||
		!validServiceOperationID(op.RequestID) || data.Version != panelCertificateSagaVersion {
		return errors.New("invalid panel certificate operation identity")
	}
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		data.Domain, data.Email, data.TLSDir, data.ExpectedBuildCommit,
	)
	if err != nil || commitment.Domain != data.Domain || commitment.Email != data.Email ||
		commitment.TLSDir != data.TLSDir ||
		commitment.ExpectedBuildCommit != data.ExpectedBuildCommit ||
		commitment.Qualifier != data.CertificateQualifier ||
		op.ServiceID != data.Domain || op.PackageName != data.CertificateQualifier {
		return errors.New("panel certificate operation payload does not match its durable identity")
	}
	if (data.FailureCode == "") != (data.FailureMessage == "") ||
		len(data.FailureCode) > 128 || len(data.FailureMessage) > 512 || len(data.Detail) > 512 {
		return errors.New("invalid panel certificate operation result metadata")
	}
	if !data.CertificateCommitted && (data.ExpiresAt != "" || data.Detail != "") {
		return errors.New("pre-commit panel certificate state carries result metadata")
	}
	if data.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, data.ExpiresAt); err != nil {
			return errors.New("invalid panel certificate expiration metadata")
		}
	}
	wantStep := ""
	switch op.Phase {
	case panelCertificatePhaseQueued, panelCertificatePhasePreflightPlanning,
		panelCertificatePhasePreflightSkipped, panelCertificatePhaseCertificatePlanning:
		if data.CertificateCommitted || data.FailureCode != "" {
			return errors.New("invalid pre-issuance panel certificate state")
		}
	case panelCertificatePhasePreflightChild:
		wantStep = panelCertificateChildPreflight
		if data.CertificateCommitted || data.FailureCode != "" {
			return errors.New("invalid firewall preflight state")
		}
	case panelCertificatePhaseCertificateChild:
		wantStep = panelCertificateChildCertificate
		if data.CertificateCommitted || data.FailureCode != "" {
			return errors.New("invalid certificate child state")
		}
	case panelCertificatePhaseCompensatePlanning, panelCertificatePhaseCompensateSkipped:
		if data.CertificateCommitted || data.FailureCode == "" {
			return errors.New("invalid certificate compensation state")
		}
	case panelCertificatePhaseCompensateChild:
		wantStep = panelCertificateChildCompensation
		if data.CertificateCommitted || data.FailureCode == "" {
			return errors.New("invalid certificate compensation child state")
		}
	case panelCertificatePhaseFinalPlanning, panelCertificatePhaseFinalSkipped:
		if !data.CertificateCommitted || data.FailureCode != "" {
			return errors.New("invalid post-commit firewall state")
		}
	case panelCertificatePhaseFinalChild:
		wantStep = panelCertificateChildFinal
		if !data.CertificateCommitted || data.FailureCode != "" {
			return errors.New("invalid post-commit firewall child state")
		}
	default:
		return fmt.Errorf("invalid panel certificate operation phase %q", op.Phase)
	}
	if wantStep == "" {
		if data.Child != nil {
			return errors.New("panel certificate phase unexpectedly carries a child")
		}
		return nil
	}
	return validatePanelCertificateSagaChild(data, wantStep)
}

func validatePanelCertificateSagaChild(data panelCertificateSagaData, wantStep string) error {
	child := data.Child
	if child == nil || child.Step != wantStep ||
		!validServiceOperationID(child.RequestID) || !validServiceOperationID(child.OwnerID) {
		return errors.New("invalid panel certificate child identity")
	}
	if wantStep == panelCertificateChildCertificate {
		if child.Kind != "panel_certificate_issue" || child.Target != data.Domain ||
			child.Qualifier != data.CertificateQualifier || child.Firewall != nil {
			return errors.New("certificate child does not match the persisted payload")
		}
		return nil
	}
	if child.Kind != "firewall_sync" || child.Target != "nftables" || child.Firewall == nil {
		return errors.New("firewall child has an invalid durable identity")
	}
	canonical, err := mutationpayload.CanonicalFirewallApply(
		child.Firewall.Enabled, child.Firewall.Persist,
		child.Firewall.TCPPorts, child.Firewall.UDPPorts,
	)
	if err != nil || !canonical.Enabled || canonical.Persist ||
		canonical.Qualifier != child.Qualifier ||
		!equalIntSlice(canonical.TCPPorts, child.Firewall.TCPPorts) ||
		!equalIntSlice(canonical.UDPPorts, child.Firewall.UDPPorts) {
		return errors.New("firewall child does not match its canonical payload")
	}
	return nil
}

func equalIntSlice(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (p *Panel) transitionPanelCertificateSaga(
	ctx context.Context,
	op *serviceOperation,
	nextPhase string,
	data panelCertificateSagaData,
) error {
	if op == nil || op.Status != serviceOperationRunning {
		return errors.New("panel certificate operation is not running")
	}
	next := *op
	next.Phase = nextPhase
	encoded, err := canonicalPanelCertificateSagaData(next, data)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE service_operations
		SET phase=?, operation_data=?, updated_at=?
		WHERE id=? AND status=? AND phase=? AND operation_data=?`,
		nextPhase, encoded, now, op.ID, serviceOperationRunning, op.Phase,
		op.OperationData,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("panel certificate state transition lost its running row")
	}
	op.Phase = nextPhase
	op.OperationData = encoded
	return nil
}

func (p *Panel) startPanelCertificateSaga(op *serviceOperation) error {
	if op == nil || op.Status != serviceOperationQueued || op.Phase != panelCertificatePhaseQueued {
		return errors.New("invalid queued panel certificate operation")
	}
	data, err := decodePanelCertificateSagaData(*op)
	if err != nil {
		return err
	}
	next := *op
	next.Status = serviceOperationRunning
	next.Phase = panelCertificatePhasePreflightPlanning
	encoded, err := canonicalPanelCertificateSagaData(next, data)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := p.db.GetDB().ExecContext(context.Background(), `
		UPDATE service_operations
		SET status=?, phase=?, operation_data=?, updated_at=?
		WHERE id=? AND status=? AND phase=? AND operation_data=?`,
		serviceOperationRunning, next.Phase, encoded, now,
		op.ID, serviceOperationQueued, panelCertificatePhaseQueued,
		op.OperationData,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("queued panel certificate operation lost its row")
	}
	*op = next
	op.OperationData = encoded
	return nil
}

func panelCertificateChildIdentity(child *panelCertificateSagaChild) agentMutationIdentity {
	if child == nil {
		return agentMutationIdentity{}
	}
	return agentMutationIdentity{
		RequestID: child.RequestID, OwnerID: child.OwnerID,
		Kind: child.Kind, Target: child.Target, PackageName: child.Qualifier,
	}
}

type panelCertificateChildTerminalFailure struct {
	code    string
	message string
	cause   error
}

func (failure *panelCertificateChildTerminalFailure) Error() string {
	if failure == nil || failure.cause == nil {
		return "panel certificate child failed"
	}
	return failure.cause.Error()
}

func (p *Panel) runPanelCertificateChild(
	ctx context.Context,
	child *panelCertificateSagaChild,
	call func(context.Context, agentMutationBinding) error,
) error {
	identity := panelCertificateChildIdentity(child)
	if !validServiceOperationID(identity.RequestID) || !validServiceOperationID(identity.OwnerID) {
		return errPanelCertificateSagaIdentity
	}
	observe := func() (*agentMutationJob, error) {
		job, err := p.statusAgentMutation(ctx, identity.RequestID)
		if err != nil {
			return nil, err
		}
		if job != nil && !identity.matches(job) {
			return job, errPanelCertificateSagaIdentity
		}
		return job, nil
	}
	job, err := observe()
	if err != nil {
		return err
	}
	if job != nil && agentMutationActive(job.Status) {
		job, err = p.waitExpectedAgentMutationTerminal(ctx, identity)
		if err != nil {
			return fmt.Errorf("%w: %v", errPanelCertificateSagaTransient, err)
		}
	}
	if job != nil {
		switch job.Status {
		case agentMutationSucceeded:
			if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
				return err
			}
			return nil
		case agentMutationFailed:
			return &panelCertificateChildTerminalFailure{
				code:    nonEmptyMutationValue(job.ErrorCode, "panel_certificate_child_failed"),
				message: nonEmptyMutationValue(job.ErrorMessage, "The privileged certificate step failed."),
				cause:   errors.New("persisted privileged child failed"),
			}
		default:
			return errPanelCertificateSagaIdentity
		}
	}
	op := serviceOperation{
		RequestID: child.RequestID, Kind: child.Kind,
		ServiceID: child.Target, PackageName: child.Qualifier,
	}
	callErr := p.withStandaloneAgentMutationIdentity(ctx, op, child.OwnerID, call)
	if callErr == nil {
		return nil
	}
	job, statusErr := observe()
	if statusErr != nil {
		return errors.Join(callErr, statusErr)
	}
	if job == nil || agentMutationActive(job.Status) {
		return fmt.Errorf("%w: %v", errPanelCertificateSagaTransient, callErr)
	}
	if job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return err
		}
		return nil
	}
	if job.Status == agentMutationFailed {
		return &panelCertificateChildTerminalFailure{
			code:    nonEmptyMutationValue(job.ErrorCode, "panel_certificate_child_failed"),
			message: nonEmptyMutationValue(job.ErrorMessage, "The privileged certificate step failed."),
			cause:   callErr,
		}
	}
	return errPanelCertificateSagaIdentity
}

func newPanelCertificateSagaChild(step, kind, target, qualifier string) (*panelCertificateSagaChild, error) {
	requestID, err := newServiceOperationID()
	if err != nil {
		return nil, err
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return nil, err
	}
	return &panelCertificateSagaChild{
		Step: step, RequestID: requestID, OwnerID: ownerID,
		Kind: kind, Target: target, Qualifier: qualifier,
	}, nil
}

func (p *Panel) planPanelCertificateFirewall(
	ctx context.Context,
	op *serviceOperation,
	data *panelCertificateSagaData,
	step, childPhase, skippedPhase string,
	extraTCP ...int,
) error {
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()
	var status FirewallStatusResp
	if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &status); err != nil {
		return err
	}
	if status.Error != "" {
		return errors.New(status.Error)
	}
	if !status.Enabled {
		data.Child = nil
		return p.transitionPanelCertificateSaga(ctx, op, skippedPhase, *data)
	}
	tcp, udp, err := p.desiredFirewallPorts(extraTCP...)
	if err != nil {
		return err
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(true, false, tcp, udp)
	if err != nil {
		return err
	}
	child, err := newPanelCertificateSagaChild(step, "firewall_sync", "nftables", commitment.Qualifier)
	if err != nil {
		return err
	}
	child.Firewall = &panelCertificateSagaFirewall{
		Enabled: commitment.Enabled, Persist: commitment.Persist,
		TCPPorts: append([]int(nil), commitment.TCPPorts...),
		UDPPorts: append([]int(nil), commitment.UDPPorts...),
	}
	data.Child = child
	return p.transitionPanelCertificateSaga(ctx, op, childPhase, *data)
}

func (p *Panel) executePanelCertificateFirewall(
	ctx context.Context,
	data panelCertificateSagaData,
) error {
	child := data.Child
	if child == nil || child.Firewall == nil {
		return errPanelCertificateSagaIdentity
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(
		child.Firewall.Enabled, child.Firewall.Persist,
		child.Firewall.TCPPorts, child.Firewall.UDPPorts,
	)
	if err != nil || commitment.Qualifier != child.Qualifier {
		return errPanelCertificateSagaIdentity
	}
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()
	return p.runPanelCertificateChild(ctx, child, func(
		callCtx context.Context,
		binding agentMutationBinding,
	) error {
		var response FirewallStatusResp
		request := applyFirewallReq{
			ServiceMutationBinding: binding,
			Enabled:                commitment.Enabled, Persist: commitment.Persist,
			TCPPorts: append([]int(nil), commitment.TCPPorts...),
			UDPPorts: append([]int(nil), commitment.UDPPorts...),
		}
		if err := p.callAgentContext(
			callCtx, "Agent.ApplyFirewallV2", &request, &response,
		); err != nil {
			return err
		}
		if response.Error != "" {
			return &firewallAgentResponseError{message: response.Error}
		}
		return nil
	})
}

func (p *Panel) planPanelCertificateIssue(
	ctx context.Context,
	op *serviceOperation,
	data *panelCertificateSagaData,
) error {
	child, err := newPanelCertificateSagaChild(
		panelCertificateChildCertificate,
		"panel_certificate_issue", data.Domain, data.CertificateQualifier,
	)
	if err != nil {
		return err
	}
	data.Child = child
	return p.transitionPanelCertificateSaga(
		ctx, op, panelCertificatePhaseCertificateChild, *data,
	)
}

func (p *Panel) executePanelCertificateIssue(
	ctx context.Context,
	data *panelCertificateSagaData,
) error {
	child := data.Child
	if child == nil {
		return errPanelCertificateSagaIdentity
	}
	var response transport.IssuePanelCertificateV2Response
	return p.runPanelCertificateChild(ctx, child, func(
		callCtx context.Context,
		binding agentMutationBinding,
	) error {
		response = transport.IssuePanelCertificateV2Response{}
		err := p.callAgentContext(
			callCtx,
			"Agent.IssuePanelCertificateV2",
			&transport.IssuePanelCertificateV2Request{
				MutationRequestID: binding.MutationRequestID,
				MutationOwnerID:   binding.MutationOwnerID,
				Domain:            data.Domain, Email: data.Email, TLSDir: data.TLSDir,
				ExpectedBuildCommit: data.ExpectedBuildCommit,
			},
			&response,
		)
		if err != nil {
			return err
		}
		if response.Error != "" {
			return errors.New(response.Error)
		}
		if !response.Issued {
			return errors.New("agent did not confirm panel certificate publication")
		}
		if !response.ExpiresAt.IsZero() {
			data.ExpiresAt = response.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		data.Detail = strings.TrimSpace(response.Detail)
		return nil
	})
}

func panelCertificateSagaFailure(cause error) (string, string) {
	// Agent errors and ledger messages may contain host paths, command output,
	// or other privileged details. They stay in process logs only; the durable
	// private saga document and eventual public operation error use a closed,
	// operator-safe classification.
	_ = cause
	return "panel_certificate_issue_failed", "The panel certificate could not be issued and verified."
}

func (p *Panel) persistPanelCertificatePrecommitFailure(
	ctx context.Context,
	op *serviceOperation,
	data *panelCertificateSagaData,
	cause error,
) error {
	data.FailureCode, data.FailureMessage = panelCertificateSagaFailure(cause)
	data.Child = nil
	return p.transitionPanelCertificateSaga(
		ctx, op, panelCertificatePhaseCompensatePlanning, *data,
	)
}

func (p *Panel) finishPanelCertificateSagaSuccess(
	ctx context.Context,
	op serviceOperation,
	data panelCertificateSagaData,
) error {
	result := serviceOperationResult{
		"success": true, "issued": true, "domain": data.Domain, "restarting": true,
	}
	if data.ExpiresAt != "" {
		result["expires_at"] = data.ExpiresAt
	} else if current := currentPanelCert(); !current.ExpiresAt.IsZero() {
		result["expires_at"] = current.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if data.Detail != "" {
		result["detail"] = data.Detail
	}
	return p.finishServiceOperationSucceeded(ctx, op.ID, result)
}

func (p *Panel) finishPanelCertificateSagaFailure(
	ctx context.Context,
	op serviceOperation,
	data panelCertificateSagaData,
) error {
	failure := operationFailure(data.FailureCode, data.FailureMessage, nil)
	return p.finishServiceOperationFailed(
		ctx, op.ID, panelCertificatePhaseFailedCompensated,
		serviceOperationResult{"success": false, "issued": false, "domain": data.Domain},
		failure,
	)
}

func (p *Panel) stepPanelCertificateSaga(
	ctx context.Context,
	op *serviceOperation,
) (terminal bool, retry bool, err error) {
	data, err := decodePanelCertificateSagaData(*op)
	if err != nil {
		return false, false, err
	}
	switch op.Phase {
	case panelCertificatePhasePreflightPlanning:
		err = p.planPanelCertificateFirewall(
			ctx, op, &data, panelCertificateChildPreflight,
			panelCertificatePhasePreflightChild, panelCertificatePhasePreflightSkipped, 80,
		)
		if err != nil {
			if persistErr := p.persistPanelCertificatePrecommitFailure(ctx, op, &data, err); persistErr != nil {
				return false, true, errors.Join(err, persistErr)
			}
			return false, true, nil
		}
		return false, false, nil
	case panelCertificatePhasePreflightSkipped:
		return false, false, p.transitionPanelCertificateSaga(
			ctx, op, panelCertificatePhaseCertificatePlanning, data,
		)
	case panelCertificatePhasePreflightChild:
		err = p.executePanelCertificateFirewall(ctx, data)
		if err != nil {
			if errors.Is(err, errPanelCertificateSagaIdentity) || errors.Is(err, errPanelCertificateSagaTransient) {
				return false, true, err
			}
			if persistErr := p.persistPanelCertificatePrecommitFailure(ctx, op, &data, err); persistErr != nil {
				return false, true, errors.Join(err, persistErr)
			}
			return false, true, nil
		}
		data.Child = nil
		return false, false, p.transitionPanelCertificateSaga(
			ctx, op, panelCertificatePhaseCertificatePlanning, data,
		)
	case panelCertificatePhaseCertificatePlanning:
		return false, false, p.planPanelCertificateIssue(ctx, op, &data)
	case panelCertificatePhaseCertificateChild:
		err = p.executePanelCertificateIssue(ctx, &data)
		if err != nil {
			if errors.Is(err, errPanelCertificateSagaIdentity) || errors.Is(err, errPanelCertificateSagaTransient) {
				return false, true, err
			}
			if persistErr := p.persistPanelCertificatePrecommitFailure(ctx, op, &data, err); persistErr != nil {
				return false, true, errors.Join(err, persistErr)
			}
			return false, true, nil
		}
		data.CertificateCommitted = true
		data.Child = nil
		return false, false, p.transitionPanelCertificateSaga(
			ctx, op, panelCertificatePhaseFinalPlanning, data,
		)
	case panelCertificatePhaseCompensatePlanning:
		err = p.planPanelCertificateFirewall(
			ctx, op, &data, panelCertificateChildCompensation,
			panelCertificatePhaseCompensateChild, panelCertificatePhaseCompensateSkipped,
		)
		return false, err != nil, err
	case panelCertificatePhaseCompensateSkipped:
		return true, false, p.finishPanelCertificateSagaFailure(ctx, *op, data)
	case panelCertificatePhaseCompensateChild:
		err = p.executePanelCertificateFirewall(ctx, data)
		if err != nil {
			if errors.Is(err, errPanelCertificateSagaIdentity) || errors.Is(err, errPanelCertificateSagaTransient) {
				return false, true, err
			}
			data.Child = nil
			transitionErr := p.transitionPanelCertificateSaga(
				ctx, op, panelCertificatePhaseCompensatePlanning, data,
			)
			return false, true, errors.Join(err, transitionErr)
		}
		return true, false, p.finishPanelCertificateSagaFailure(ctx, *op, data)
	case panelCertificatePhaseFinalPlanning:
		err = p.planPanelCertificateFirewall(
			ctx, op, &data, panelCertificateChildFinal,
			panelCertificatePhaseFinalChild, panelCertificatePhaseFinalSkipped,
		)
		return false, err != nil, err
	case panelCertificatePhaseFinalSkipped:
		return true, false, p.finishPanelCertificateSagaSuccess(ctx, *op, data)
	case panelCertificatePhaseFinalChild:
		err = p.executePanelCertificateFirewall(ctx, data)
		if err != nil {
			if errors.Is(err, errPanelCertificateSagaIdentity) || errors.Is(err, errPanelCertificateSagaTransient) {
				return false, true, err
			}
			data.Child = nil
			transitionErr := p.transitionPanelCertificateSaga(
				ctx, op, panelCertificatePhaseFinalPlanning, data,
			)
			return false, true, errors.Join(err, transitionErr)
		}
		return true, false, p.finishPanelCertificateSagaSuccess(ctx, *op, data)
	default:
		return false, false, fmt.Errorf("unsupported panel certificate phase %q", op.Phase)
	}
}

func (p *Panel) drivePanelCertificateSaga(op serviceOperation, actor serviceOperationActor) {
	for {
		current, err := p.serviceOperationByID(context.Background(), op.ID)
		if err != nil {
			log.Printf("panel certificate operation %s reload failed: %v", op.ID, err)
			time.Sleep(panelCertificateSagaRetryDelay)
			continue
		}
		if current.Status == serviceOperationQueued {
			if err := p.startPanelCertificateSaga(&current); err != nil {
				log.Printf("panel certificate operation %s start failed: %v", op.ID, err)
				time.Sleep(panelCertificateSagaRetryDelay)
				continue
			}
		}
		if current.Status != serviceOperationRunning {
			return
		}
		terminal, retry, err := p.stepPanelCertificateSaga(context.Background(), &current)
		if err != nil {
			log.Printf("panel certificate operation %s phase %s: %v", op.ID, current.Phase, err)
		}
		if terminal && err == nil {
			finished, reloadErr := p.serviceOperationByID(context.Background(), op.ID)
			if reloadErr != nil {
				log.Printf("panel certificate operation %s terminal reload failed: %v", op.ID, reloadErr)
				return
			}
			action := "panel.certificate:" + current.ServiceID
			if finished.Status == serviceOperationFailed {
				action = "panel.certificate.failed:" + current.ServiceID
			}
			p.auditServiceOperation(context.Background(), actor, action)
			return
		}
		if retry || err != nil {
			// An identity mismatch is deliberately not terminalized. Retaining
			// the process mutation lock and using the bounded retry delay keeps
			// unrelated privileged work out while an operator repairs the
			// durable row/agent-ledger disagreement; this is not a busy loop.
			time.Sleep(panelCertificateSagaRetryDelay)
		}
	}
}

func (p *Panel) launchPanelCertificateSaga(
	op serviceOperation,
	actor serviceOperationActor,
	releaseMutation func(),
) {
	go func() {
		defer releaseMutation()
		p.drivePanelCertificateSaga(op, actor)
	}()
}

func recoverablePanelCertificateSagaChild(
	op serviceOperation,
	job *agentMutationJob,
) bool {
	data, err := decodePanelCertificateSagaData(op)
	if err != nil || data.Child == nil || job == nil || !agentMutationActive(job.Status) {
		return false
	}
	return panelCertificateChildIdentity(data.Child).matches(job)
}

func recoverablePanelCertificateActivation(
	op serviceOperation,
	job *agentMutationJob,
) bool {
	if _, err := decodePanelCertificateSagaData(op); err != nil {
		return false
	}
	return validIndependentPanelCertificateActivation(job)
}

func validIndependentPanelCertificateActivation(job *agentMutationJob) bool {
	if job == nil || !agentMutationActive(job.Status) ||
		!validServiceOperationID(job.RequestID) ||
		!validServiceOperationID(job.OwnerID) ||
		job.Kind != "panel-certificate-activation" ||
		job.PackageName != "" {
		return false
	}
	// Certificate activation is an independently authorized agent-owned
	// infrastructure lease. It can legitimately restart the panel in any
	// no-child-lease gap (including an old-domain renewal during a domain
	// change). Accepting its exact canonical identity only lets startup resume
	// the saga; it is never interpreted as success for the persisted child.
	canonicalTarget, err := hostname.CanonicalFQDN(job.Target)
	return err == nil && canonicalTarget == job.Target
}

func (p *Panel) resumePanelCertificateSagaLocked(op serviceOperation) error {
	if op.Kind != serviceOperationKindPanelCertificate ||
		(op.Status != serviceOperationQueued && op.Status != serviceOperationRunning) {
		return errors.New("invalid active panel certificate operation")
	}
	if _, err := decodePanelCertificateSagaData(op); err != nil {
		return err
	}
	p.launchPanelCertificateSaga(op, serviceOperationActor{}, p.serviceMutationMu.Unlock)
	return nil
}
