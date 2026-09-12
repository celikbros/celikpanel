package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func previousSetupBuildForTest() string {
	previous := strings.Repeat("a", 40)
	if previous == strings.TrimSpace(buildCommit) {
		return strings.Repeat("b", 40)
	}
	return previous
}

func TestServerSetupBuildChangeRequiresReviewBeforeNewChild(t *testing.T) {
	for _, kind := range []string{"service", "panel_certificate", "mail_certificate"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := setupOperationFixture(t)
			target := "nginx"
			if kind != "service" {
				target = "panel.example.test"
			}
			step := serverSetupExecutionStep{
				serverSetupPlanStep: serverSetupPlanStep{ID: "remaining", Kind: kind, Target: target},
				RequestID:           strings.Repeat("c", 32), OwnerID: strings.Repeat("d", 32),
			}
			plan := serverSetupPlan{
				BuildCommit: previousSetupBuildForTest(), ContactEmail: "owner@example.test",
				Draft: serverSetupDraft{PanelDomain: "panel.example.test"},
				Actor: serviceOperationActor{UserID: f.userID},
			}
			done, err := f.panel.runServerSetupStep(context.Background(), plan, &step)
			if done || !errors.Is(err, errServerSetupBuildChanged) {
				t.Fatalf("changed build should require review: done=%v err=%v", done, err)
			}
			failure := serverSetupFailureForStep(step, fmt.Errorf("resume: %w", err))
			if failure.Code != "server_setup_build_changed" {
				t.Fatalf("build change lost its actionable reason: %+v", failure)
			}
			var rows int
			if err := f.database.GetDB().QueryRow(`SELECT COUNT(*) FROM service_operations`).Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("changed build admitted a child: rows=%d err=%v", rows, err)
			}
			if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
				t.Fatal("changed build reached host mutation")
			}
		})
	}
}

func TestServerSetupBuildChangeKeepsCompletedChildReceipt(t *testing.T) {
	f, _ := setupOperationFixture(t)
	step := serverSetupExecutionStep{
		serverSetupPlanStep: serverSetupPlanStep{ID: "already-started", Kind: "service", Target: "nginx"},
		RequestID:           strings.Repeat("e", 32), OwnerID: strings.Repeat("f", 32),
	}
	op, err := f.panel.createServiceOperationRequest(context.Background(), serviceOperationKindInstall, "nginx", "", step.RequestID, serviceOperationActor{UserID: f.userID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.database.GetDB().Exec(`UPDATE service_operations SET status='succeeded',result_json='{"success":true,"installed":true}' WHERE id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	done, err := f.panel.runServerSetupStep(context.Background(), serverSetupPlan{BuildCommit: previousSetupBuildForTest()}, &step)
	if err != nil || !done || step.OperationID != op.ID {
		t.Fatalf("build change discarded exact completed receipt: done=%v err=%v step=%+v", done, err, step)
	}
	if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
		t.Fatal("completed child was installed again after build change")
	}
}
