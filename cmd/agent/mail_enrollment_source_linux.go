//go:build linux

package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/transport"
	"path/filepath"
	"strings"
	"time"
)

// MailEnrollmentSourceV1 pins the complete installed helper and the running
// Agent's release declaration. It neither prepares a journal nor acquires
// mutation authority, changes native state, or claims present renewal health.
func (a *Agent) MailEnrollmentSourceV1(req *transport.MailEnrollmentSourceRequest, resp *transport.MailEnrollmentSourceResponse) error {
	return inspectMailEnrollmentSourceRPC(context.Background(), req, resp, buildCommit, inspectRunningAgentMailHelper)
}
func inspectMailEnrollmentSourceRPC(ctx context.Context, req *transport.MailEnrollmentSourceRequest, resp *transport.MailEnrollmentSourceResponse, runningCommit string, inspect func() (mailEnrollmentRPCInspection, error)) error {
	if resp == nil {
		return errors.New("mail enrollment source response is required")
	}
	*resp = transport.MailEnrollmentSourceResponse{State: "unknown", Reason: "mail_enrollment_source_unverified"}
	if ctx == nil || req == nil || inspect == nil {
		return errors.New("mail enrollment source request is required")
	}
	if len(runningCommit) != 40 || strings.Trim(runningCommit, "0123456789abcdef") != "" || req.ExpectedBuildCommit != runningCommit {
		return errors.New("matching released panel and agent builds are required for mail enrollment source inspection")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	proof, err := inspect()
	if proof.source != nil {
		defer proof.source.Close()
	}
	if err != nil || proof.source == nil {
		return nil
	}
	commit, digest := proof.source.AgentIdentity()
	if commit != runningCommit || !recoveryruntime.ValidDigest(digest) || digest != proof.runningDigest || !recoveryruntime.ValidDigest(proof.generation) || proof.helper != filepath.Join(mailrenewalkit.InstalledRoot, proof.generation, mailrenewalkit.BinaryName) {
		return nil
	}
	if proof.source.Revalidate() != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	*resp = transport.MailEnrollmentSourceResponse{State: "verified", Generation: proof.generation, BuildCommit: runningCommit, ObservedAt: time.Now().UTC()}
	return nil
}
