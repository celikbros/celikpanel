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
	"testing"
)

func TestMailEnrollmentRPCPreviewRetainsOwnerPreferencesAndRejectsMixedEvidence(t *testing.T) {
	for _, scenario := range []string{"absent", "legacy", "independent", "disabled", "native-unknown", "native-busy", "source-unknown", "changed-native", "bad-mode", "bad-generation", "bad-timer", "mixed-initial", "wrong-build", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			commit, generation, digest := strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64)
			req := transport.MailEnrollmentSourceRequest{ExpectedBuildCommit: commit}
			source := &enrollmentRPCProof{commit: commit, digest: digest}
			native := &enrollmentRPCProof{}
			preview := mailEnrollmentRPCPreview{native, recoveryruntime.MailRenewalHookAbsent, "", mailrenewalkit.TimerState{Enablement: "absent", Activity: "inactive"}}
			switch scenario {
			case "legacy":
				preview.mode = recoveryruntime.MailRenewalHookLegacy
			case "independent", "disabled":
				preview.mode = recoveryruntime.MailRenewalHookIndependent
				preview.generation = strings.Repeat("d", 64)
				preview.timer = mailrenewalkit.TimerState{Enablement: "enabled", Activity: "active"}
				if scenario == "disabled" {
					preview.timer = mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}
				}
			case "changed-native":
				native.changed = true
			case "bad-mode":
				preview.mode = "future"
			case "bad-generation":
				preview.mode = recoveryruntime.MailRenewalHookIndependent
				preview.generation = "../kit"
			case "bad-timer":
				preview.timer.Enablement = "unknown"
			case "mixed-initial":
				preview.generation = generation
			case "wrong-build":
				req.ExpectedBuildCommit = strings.Repeat("f", 40)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			calls := 0
			var out transport.MailEnrollmentPreviewResponse
			err := previewMailEnrollmentRPC(ctx, &req, &out, commit, func() (mailEnrollmentRPCInspection, error) {
				if scenario == "source-unknown" {
					return mailEnrollmentRPCInspection{}, errors.New("private source")
				}
				return mailEnrollmentRPCInspection{source, filepath.Join(mailrenewalkit.InstalledRoot, generation, mailrenewalkit.BinaryName), generation, digest}, nil
			}, func(context.Context) (mailEnrollmentRPCPreview, error) {
				calls++
				if scenario == "native-busy" {
					return preview, mailrenewalkit.ErrScheduleBusy
				}
				if scenario == "native-unknown" {
					return preview, errors.New("private native")
				}
				return preview, nil
			})
			good := scenario == "absent" || scenario == "legacy" || scenario == "independent" || scenario == "disabled"
			if good {
				if err != nil || out.State != "verified" || out.Generation != generation || out.ExistingGeneration != preview.generation || out.NativeMode != string(preview.mode) || out.TimerEnablement != preview.timer.Enablement || out.TimerActivity != preview.timer.Activity || out.ObservedAt.IsZero() {
					t.Fatalf("%+v %v", out, err)
				}
			} else if out.State != map[bool]string{true: "waiting", false: "unknown"}[scenario == "native-busy"] || out.Generation != "" || out.ExistingGeneration != "" || out.NativeMode != "" || out.TimerEnablement != "" || out.TimerActivity != "" || !out.ObservedAt.IsZero() {
				t.Fatalf("mixed evidence accepted: %+v", out)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("private detail exposed")
			}
			if calls > 0 && !native.closed {
				t.Fatal("native proof not closed")
			}
			if scenario == "cancelled" && calls != 0 {
				t.Fatal("cancelled preview queried native state")
			}
		})
	}
}
