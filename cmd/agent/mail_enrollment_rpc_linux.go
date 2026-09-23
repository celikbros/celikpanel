//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func validMailEnrollmentRequest(req *transport.MailEnrollmentRequest) bool {
	return req != nil && validMutationIdentity(req.RequestID) && validMutationIdentity(req.OwnerID) && recoveryruntime.ValidDigest(req.Generation)
}

// StartMailEnrollmentV1 is an explicit authenticated IPC mutation. It hands off
// to the independent worker; neither RPC return nor systemd acceptance is proof
// of enrollment. The Panel must persist its dispatch intent BEFORE calling it.
// This method is not advertised to setup until its native acceptance is complete.
func (a *Agent) StartMailEnrollmentV1(req *transport.MailEnrollmentStartRequest, resp *transport.MailEnrollmentStartResponse) error {
	if req == nil || resp == nil || !validMailEnrollmentRequest(&req.MailEnrollmentRequest) {
		return servicemutationledger.ErrMailEnrollment
	}
	if strings.TrimSpace(req.ExpectedBuildCommit) == "" || req.ExpectedBuildCommit == "unknown" {
		return errors.New("expected panel build commit is required before mail renewal enrollment")
	}
	if req.ExpectedBuildCommit != buildCommit {
		return errors.New("panel/agent build mismatch before mail renewal enrollment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return startMailEnrollmentRPC(ctx, req, resp, buildCommit, inspectRunningAgentMailHelper, observeAgentMailEnrollment, dispatchMailEnrollmentHelper)
}

type mailEnrollmentRPCSource interface {
	Revalidate() error
	AgentIdentity() (string, string)
	Close()
}
type mailEnrollmentRPCInspection struct {
	source                            mailEnrollmentRPCSource
	helper, generation, runningDigest string
}

func inspectRunningAgentMailHelper() (mailEnrollmentRPCInspection, error) {
	empty := mailEnrollmentRPCInspection{}
	if mailRenewalOnlyBuild || os.Geteuid() != 0 {
		return empty, servicemutationledger.ErrMailEnrollment
	}
	proof, err := recoveryruntime.InspectMailEnrollmentHelper("/opt/celikpanel/bin", mailrenewalkit.InstalledRoot)
	if err != nil {
		return empty, err
	}
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		proof.Close()
		return empty, err
	}
	raw, err := io.ReadAll(io.LimitReader(self, mailrenewalkit.MaxBinarySize+1))
	closeErr := self.Close()
	if err != nil || closeErr != nil || len(raw) == 0 || len(raw) > mailrenewalkit.MaxBinarySize {
		proof.Close()
		return empty, servicemutationledger.ErrMailEnrollment
	}
	return mailEnrollmentRPCInspection{proof, proof.Path, proof.Generation, recoveryruntime.Digest(raw)}, nil
}
func startMailEnrollmentRPC(ctx context.Context, req *transport.MailEnrollmentStartRequest, resp *transport.MailEnrollmentStartResponse, runningCommit string, inspect func() (mailEnrollmentRPCInspection, error), observe func(context.Context, *transport.MailEnrollmentRequest) (recoveryruntime.MailEnrollmentObservation, error), dispatch func(context.Context, string, []string) error) error {
	if ctx == nil || req == nil || resp == nil || !validMailEnrollmentRequest(&req.MailEnrollmentRequest) || inspect == nil || observe == nil || dispatch == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	*resp = transport.MailEnrollmentStartResponse{RequestID: req.RequestID, Handoff: "unknown"}
	// Production admission never uses development/empty identity as a release claim.
	if len(runningCommit) != 40 || req.ExpectedBuildCommit != runningCommit || strings.Trim(runningCommit, "0123456789abcdef") != "" {
		return servicemutationledger.ErrMailEnrollment
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	proof, err := inspect()
	if err != nil {
		return errors.New("mail enrollment source is unverified; preserve the accepted request and inspect the installed runtime")
	}
	if proof.source == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	defer proof.source.Close()
	commit, digest := proof.source.AgentIdentity()
	if commit != runningCommit || digest != proof.runningDigest || proof.generation != req.Generation || !recoveryruntime.ValidDigest(digest) {
		return servicemutationledger.ErrMailEnrollment
	}
	// Conflicting recorded owners/targets never reach dispatch. An unrecorded job
	// is admissible ONLY because this is an explicit start, never from polling.
	if _, err = observe(ctx, &req.MailEnrollmentRequest); err != nil {
		return errors.New("mail enrollment evidence is unverified; inspect the same request before continuing")
	}
	if _, err = mailEnrollmentWorkerUnitArgs(proof.helper, []string{req.RequestID, req.OwnerID, req.Generation}); err != nil {
		return err
	}
	if err = proof.source.Revalidate(); err != nil {
		return errors.New("mail enrollment source changed; review the accepted request before continuing")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = dispatch(ctx, proof.helper, []string{req.RequestID, req.OwnerID, req.Generation}); err != nil {
		resp.Reason = "mail_enrollment_handoff_unknown"
		return nil
	}
	resp.Handoff = "accepted"
	return nil
}

func observeAgentMailEnrollment(ctx context.Context, req *transport.MailEnrollmentRequest) (recoveryruntime.MailEnrollmentObservation, error) {
	if !validMailEnrollmentRequest(req) {
		return recoveryruntime.MailEnrollmentObservation{}, servicemutationledger.ErrMailEnrollment
	}
	return recoveryruntime.ObserveMailEnrollment(ctx, filepath.Join(hostingpath.ServiceMutationStateRoot(), serviceMutationLedgerFileName), servicemutationledger.FileOwner{UID: serviceMutationRequiredOwnerUID, GID: serviceMutationRequiredOwnerGID}, mailEnrollmentJournalRoot, req.RequestID, req.OwnerID, req.Generation)
}
func (a *Agent) MailEnrollmentStatusV1(req *transport.MailEnrollmentRequest, resp *transport.MailEnrollmentStatusResponse) error {
	return mailEnrollmentStatusRPC(req, resp, observeAgentMailEnrollment)
}
func mailEnrollmentStatusRPC(req *transport.MailEnrollmentRequest, resp *transport.MailEnrollmentStatusResponse, observe func(context.Context, *transport.MailEnrollmentRequest) (recoveryruntime.MailEnrollmentObservation, error)) error {
	if !validMailEnrollmentRequest(req) || resp == nil || observe == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	*resp = transport.MailEnrollmentStatusResponse{MailEnrollmentRequest: *req, State: "unknown"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := observe(ctx, req)
	resp.ObservedAt = time.Now().UTC()
	if err != nil {
		resp.Reason = "mail_enrollment_observation_unknown"
		return nil
	}
	if !result.Found {
		resp.State = "not_recorded"
		resp.Reason = "mail_enrollment_not_recorded"
		return nil
	}
	if result.Identity.RequestID != req.RequestID || result.Identity.OwnerID != req.OwnerID || result.Generation != req.Generation {
		resp.Reason = "mail_enrollment_observation_unknown"
		return nil
	}
	switch result.State {
	case servicemutationledger.MailEnrollmentForward, servicemutationledger.MailEnrollmentRollback, servicemutationledger.MailEnrollmentPublished, servicemutationledger.MailEnrollmentRestored:
		resp.State = result.State
	default:
		resp.Reason = "mail_enrollment_observation_unknown"
	}
	return nil
}
