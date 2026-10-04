//go:build linux

package main

import (
	"context"
	"errors"
	"time"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/transport"
)

type mailEnrollmentPreviewProof interface {
	Revalidate() error
	Close()
}
type mailEnrollmentRPCPreview struct {
	proof      mailEnrollmentPreviewProof
	mode       recoveryruntime.MailRenewalHookMode
	generation string
	timer      mailrenewalkit.TimerState
}

func (a *Agent) MailEnrollmentPreviewV1(req *transport.MailEnrollmentSourceRequest, resp *transport.MailEnrollmentPreviewResponse) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return previewMailEnrollmentRPC(ctx, req, resp, buildCommit, inspectRunningAgentMailHelper, func(ctx context.Context) (mailEnrollmentRPCPreview, error) {
		p, err := recoveryruntime.InspectMailEnrollmentPreview(ctx, mailEnrollmentNativeHost{}.ObserveUnit)
		if err != nil {
			return mailEnrollmentRPCPreview{}, err
		}
		return mailEnrollmentRPCPreview{p, p.Mode, p.Generation, p.Timer}, nil
	})
}
func previewMailEnrollmentRPC(ctx context.Context, req *transport.MailEnrollmentSourceRequest, resp *transport.MailEnrollmentPreviewResponse, commit string, source func() (mailEnrollmentRPCInspection, error), native func(context.Context) (mailEnrollmentRPCPreview, error)) error {
	if resp == nil {
		return errors.New("mail enrollment preview response is required")
	}
	*resp = transport.MailEnrollmentPreviewResponse{MailEnrollmentSourceResponse: transport.MailEnrollmentSourceResponse{State: "unknown", Reason: "mail_enrollment_native_unverified"}}
	if ctx == nil || req == nil || native == nil || source == nil {
		return errors.New("mail enrollment preview request is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	preview, err := native(ctx)
	if preview.proof != nil {
		defer preview.proof.Close()
	}
	if errors.Is(err, mailrenewalkit.ErrScheduleBusy) {
		resp.State, resp.Reason = "waiting", "mail_enrollment_native_busy"
		return nil
	}
	if err != nil || preview.proof == nil {
		return nil
	}
	switch preview.mode {
	case recoveryruntime.MailRenewalHookAbsent, recoveryruntime.MailRenewalHookLegacy:
		if preview.generation != "" || preview.timer != (mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}) {
			return nil
		}
	case recoveryruntime.MailRenewalHookIndependent:
		if !recoveryruntime.ValidDigest(preview.generation) || (preview.timer.Enablement != "enabled" && preview.timer.Enablement != "disabled") || (preview.timer.Activity != "active" && preview.timer.Activity != "inactive") {
			return nil
		}
	default:
		return nil
	}
	var observed transport.MailEnrollmentSourceResponse
	if err := inspectMailEnrollmentSourceRPC(ctx, req, &observed, commit, source); err != nil {
		return err
	}
	if observed.State != "verified" {
		resp.Reason = observed.Reason
		return nil
	}
	if preview.proof.Revalidate() != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	*resp = transport.MailEnrollmentPreviewResponse{MailEnrollmentSourceResponse: observed, NativeMode: string(preview.mode), ExistingGeneration: preview.generation, TimerEnablement: preview.timer.Enablement, TimerActivity: preview.timer.Activity}
	resp.ObservedAt = time.Now().UTC()
	return nil
}
