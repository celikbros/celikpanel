//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// This fixture exercises the reviewed mail step against the real Agent, helper
// and systemd. Earlier setup steps and the license are fixture prerequisites;
// it does not claim a complete installation, browser or ACME issuance trial.
type nativeEnrollmentSetupAgent struct {
	*serviceOperationTestAgent
	native *transport.ReconnectingClient
	starts atomic.Int32
}

func (a *nativeEnrollmentSetupAgent) MailEnrollmentPreviewV1(req *transport.MailEnrollmentSourceRequest, out *transport.MailEnrollmentPreviewResponse) error {
	return a.native.Call("Agent.MailEnrollmentPreviewV1", req, out)
}
func (a *nativeEnrollmentSetupAgent) MailEnrollmentStatusV1(req *transport.MailEnrollmentRequest, out *transport.MailEnrollmentStatusResponse) error {
	return a.native.Call("Agent.MailEnrollmentStatusV1", req, out)
}
func (a *nativeEnrollmentSetupAgent) StartMailEnrollmentV1(req *transport.MailEnrollmentStartRequest, out *transport.MailEnrollmentStartResponse) error {
	a.starts.Add(1)
	return a.native.Call("Agent.StartMailEnrollmentV1", req, out)
}
func TestSetupMailEnrollmentNativeRPCDisposableVM(t *testing.T) {
	node := ""
	switch os.Getenv("CELIKPANEL_DISPOSABLE_MAIL_VM") {
	case "arch-20260923-setup-rpc":
		node = "arch"
	case "debian13-20260923-setup-rpc":
		node = "debian13"
	default:
		t.Skip("guarded disposable native fixture only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root fixture required")
	}
	const markerPath = "/etc/celikpanel-release-recovery-lab"
	var st unix.Stat_t
	if unix.Lstat(markerPath, &st) != nil || st.Mode != unix.S_IFREG|0444 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Size > 2048 {
		t.Fatal("protected marker required")
	}
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	var marker map[string]string
	if json.Unmarshal(raw, &marker) != nil || marker["schema"] != "celikpanel-release-recovery-lab/v1" || marker["node"] != node {
		t.Fatal("wrong fixture")
	}
	uuid, err := os.ReadFile("/sys/class/dmi/id/product_uuid")
	if err != nil || strings.ToLower(strings.TrimSpace(string(uuid))) != marker["vm_uuid"] {
		t.Fatal("wrong VM identity")
	}
	virt, err := exec.Command("systemd-detect-virt", "--vm").Output()
	if err != nil || (strings.TrimSpace(string(virt)) != "qemu" && strings.TrimSpace(string(virt)) != "kvm") {
		t.Fatal("QEMU required")
	}
	var intent struct {
		Commit  string `json:"commit"`
		Request string `json:"request"`
		Mode    string `json:"mode"`
	}
	intentPath := "/root/celikpanel-release-recovery-lab/mail-setup-rpc.intent"
	reviewOnly := os.Getenv("CP_MAIL_NATIVE_REVIEW_ONLY") == "1"
	if reviewOnly {
		intentPath = "/root/celikpanel-release-recovery-lab/mail-setup-rpc-review.intent"
	}
	if unix.Lstat(intentPath, &st) != nil || st.Mode != unix.S_IFREG|0600 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Size > 2048 {
		t.Fatal("protected intent required")
	}
	raw, err = os.ReadFile(intentPath)
	if err != nil || json.Unmarshal(raw, &intent) != nil || intent.Commit != buildCommit || !validServiceOperationID(intent.Request) || (intent.Mode != "legacy" && intent.Mode != "absent" && !(reviewOnly && intent.Mode == "independent")) {
		t.Fatal("exact fixture intent required", err)
	}
	client, err := transport.ConnectAgent()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	native := transport.NewReconnectingClient(client)
	var version transport.AgentVersionResponse
	if err = native.Call("Agent.Version", &transport.Empty{}, &version); err != nil || version.Commit != buildCommit {
		t.Fatal("native paired source required", err)
	}
	var preview transport.MailEnrollmentPreviewResponse
	if err = native.Call("Agent.MailEnrollmentPreviewV1", &transport.MailEnrollmentSourceRequest{ExpectedBuildCommit: buildCommit}, &preview); err != nil || preview.State != "verified" || preview.NativeMode != intent.Mode {
		t.Fatal("native preview", preview, err)
	}
	f := newServiceOperationTestFixture(t)
	f.panel.license = testPanelLicense(t, "active")
	caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
	f.agent.versionCapabilities = &caps
	for _, key := range []string{"CELIKPANEL_TLS_CERT", "CELIKPANEL_TLS_KEY"} {
		t.Setenv(key, "")
	}
	t.Setenv("CELIKPANEL_TLS_DIR", panelManagedTLSDirectory)
	t.Setenv("CELIKPANEL_SERVER_IP", "192.0.2.10")
	proxy := &nativeEnrollmentSetupAgent{serviceOperationTestAgent: f.agent, native: native}
	f.panel.agentClient = newPolicyDispatchTestPanel(t, proxy).agentClient
	state, err := f.panel.loadServerSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state, err = f.panel.saveServerSetupDraft(context.Background(), state.Revision, serverSetupDraft{Purpose: "web_mail", PanelDomain: "panel.example.test", MailHostname: "mail.example.test", DNSMode: "external"})
	if err != nil {
		t.Fatal(err)
	}
	plan := saveSetupPlanForTest(t, f, state)
	if reviewOnly {
		if intent.Mode != "independent" {
			t.Fatal("review-only requires independent before-state")
		}
		for _, step := range plan.Steps {
			if step.Kind == "mail_enrollment" {
				t.Fatal("existing independent schedule was re-enrolled")
			}
		}
		if proxy.starts.Load() != 0 {
			t.Fatal("review dispatched native work")
		}
		t.Logf("native_setup_review existing_generation=%s candidate_generation=%s enablement=%s activity=%s dispatches=0", preview.ExistingGeneration, preview.Generation, preview.TimerEnablement, preview.TimerActivity)
		return
	}
	serverSetupRunners.Store(f.panel, true)
	defer serverSetupRunners.Delete(f.panel)
	response := postSetupStartForTest(t, f, plan.ID, intent.Request)
	if response.Code != http.StatusAccepted {
		t.Fatal(response.Code, response.Body.String())
	}
	execution, err := f.panel.latestServerSetupExecution(context.Background())
	if err != nil || execution == nil {
		t.Fatal(err)
	}
	index := -1
	for i := range execution.Steps {
		if execution.Steps[i].Kind == "mail_enrollment" {
			index = i
			break
		}
		// Fixture prerequisite only. No preceding native setup action runs here.
		execution.Steps[i].Status = "succeeded"
	}
	if index < 0 || execution.Steps[index].Qualifier != preview.Generation {
		t.Fatal("missing exact reviewed native kit")
	}
	execution.Steps[index].Status = "running"
	execution.Phase = execution.Steps[index].ID
	if err = f.panel.persistServerSetupExecution(context.Background(), *execution); err != nil {
		t.Fatal(err)
	}
	done := false
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		done, err = f.panel.runServerSetupMailEnrollment(context.Background(), plan, execution, index)
		var wait *serverSetupMailEnrollmentWait
		if err != nil && !errors.As(err, &wait) {
			t.Fatal(err)
		}
		if done {
			break
		}
		// Load the durable attempt fence on each observation, as after reconnect.
		execution, err = f.panel.latestServerSetupExecution(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !done || proxy.starts.Load() != 1 || !execution.Steps[index].EnrollmentDispatchAttempted {
		t.Fatal("native enrollment did not complete once", done, proxy.starts.Load(), execution)
	}
	for i := 0; i < 2; i++ {
		done, err = f.panel.runServerSetupMailEnrollment(context.Background(), plan, execution, index)
		if err != nil || !done || proxy.starts.Load() != 1 {
			t.Fatal("terminal observation replayed mutation", err)
		}
	}
	var after transport.MailEnrollmentPreviewResponse
	if err = native.Call("Agent.MailEnrollmentPreviewV1", &transport.MailEnrollmentSourceRequest{ExpectedBuildCommit: buildCommit}, &after); err != nil || after.State != "verified" || after.NativeMode != "independent" || after.ExistingGeneration != preview.Generation || after.TimerEnablement != "enabled" || after.TimerActivity != "active" {
		t.Fatal("native result", after, err)
	}
	execution.Steps[index].Status = "succeeded"
	if err = f.panel.persistServerSetupExecution(context.Background(), *execution); err != nil {
		t.Fatal(err)
	}
	t.Logf("native_setup_enrollment plan=%s execution=%s request=%s owner=%s generation=%s dispatches=%d terminal=published", plan.ID, execution.ID, execution.Steps[index].RequestID, execution.Steps[index].OwnerID, preview.Generation, proxy.starts.Load())
}
