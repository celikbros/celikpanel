package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// policyWithRealReload runs a save through the real reload path
// (mail_service_verify.go) against a fake mail host.
func policyWithRealReload(t *testing.T) (*fakePostfix, *fakeMailHost) {
	t.Helper()
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = ownerExtended
	host := installFakeMailHost(t)
	mailPolicyReload = func() (string, error) {
		fake.reloads++
		return applyPostfixVerified(host.run, mailServiceReload)
	}
	return fake, host
}

// The defect, first form (9 Oct 2026): a Postfix reload that failed after the
// policy was written to main.cf was logged, and the save was answered as a
// success.
//
// The defect, second form (measured on Ubuntu 24.04, 2026-10-08): the reload
// was `systemctl reload-or-restart postfix`, postfix.service is a wrapper unit
// there, systemctl exited 0 although `postfix@-.service` failed to reload, and
// the save was answered `200 success` with main.cf holding rate 46 and Postfix
// running with 45. The wrapper's exit status is not consulted any more: what
// Postfix's own check says about the owner's unfinished line is the answer.
func TestSetMailPolicyReportsAWrapperThatExitsZeroWhilePostfixDidNotReload(t *testing.T) {
	fake, host := policyWithRealReload(t)
	host.checkOutput = "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign\n"

	loaded := readMailPolicyForTest(t)
	loaded.OutboundRateLimit = 46
	resp := setMailPolicyForTest(t, loaded)

	if resp.Code != transport.MailPolicyNotReloaded || resp.Error == "" || resp.Applied != "" {
		t.Fatalf("answer = %+v, want %q", resp, transport.MailPolicyNotReloaded)
	}
	if resp.Stage != transport.MailPolicyStageCheck {
		t.Fatalf("stage = %q, want Postfix's own check", resp.Stage)
	}
	// The value is in main.cf: this is a failure after a change, and the answer
	// carries what is written now with the version the next save needs.
	if want := [][]string{{"smtpd_client_message_rate_limit=46"}}; !reflect.DeepEqual(fake.writes, want) || fake.reloads != 1 {
		t.Fatalf("writes = %v reloads = %d", fake.writes, fake.reloads)
	}
	if resp.Policy.OutboundRateLimit != 46 || resp.Policy.Version == "" || resp.Policy.Version == loaded.Version {
		t.Fatalf("policy in the answer = %+v, want what main.cf holds now", resp.Policy)
	}
	// One bounded line of what Postfix said, nothing more.
	if !strings.HasPrefix(resp.Reason, "postfix: fatal: bad numerical configuration: default_process_limit") || strings.Contains(resp.Reason, "\n") || len(resp.Reason) > 320 {
		t.Fatalf("reason = %q", resp.Reason)
	}
	for _, call := range host.calls {
		if strings.HasPrefix(call, "systemctl") || call == "postfix reload" {
			t.Fatalf("after a refused check nothing may be reloaded: %v", host.calls)
		}
	}
	if fake.values["smtpd_recipient_restrictions"] != ownerExtended {
		t.Fatal("a value the save did not name was rewritten")
	}

	// Saving again without a change, while the owner's line is still wrong,
	// writes nothing and is not answered "unchanged": Postfix is asked again,
	// refuses again, and the answer says so and that this request wrote
	// nothing.
	fake.reloads, fake.writes = 0, nil
	again := setMailPolicyForTest(t, resp.Policy)
	if again.Code != transport.MailPolicyNotReloaded || again.Stage != transport.MailPolicyStageCheck || !again.Unwritten || again.Applied != "" {
		t.Fatalf("an unchanged save while the check still refuses: %+v", again)
	}
	if len(fake.writes) != 0 || fake.reloads != 1 || again.Policy.Version != resp.Policy.Version {
		t.Fatalf("writes = %v reloads = %d policy = %+v", fake.writes, fake.reloads, again.Policy)
	}
}

// The gap this closes (set1, 10 Oct 2026): after "not reloaded" the owner
// corrects main.cf and presses Save without changing a value. That save
// answered 200 "unchanged" and reloaded nothing, so Postfix went on running the
// settings from before the first save. It now performs the verified reload:
// nothing is written, Postfix's own check and reload run, and the answer says
// the values were already there and Postfix was reloaded.
func TestSetMailPolicyUnchangedSaveReloadsAfterTheOwnerCorrectedMainCf(t *testing.T) {
	fake, host := policyWithRealReload(t)
	host.checkOutput = "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign\n"

	loaded := readMailPolicyForTest(t)
	loaded.OutboundRateLimit = 46
	first := setMailPolicyForTest(t, loaded)
	if first.Code != transport.MailPolicyNotReloaded || first.Unwritten {
		t.Fatalf("first save = %+v", first)
	}

	// The owner corrects the line; the policy values are the ones the first
	// save wrote, so the page's version is still current.
	host.checkOutput = ""
	host.calls, fake.writes, fake.reloads = nil, nil, 0
	second := setMailPolicyForTest(t, first.Policy)
	if second.Code != "" || second.Error != "" || second.Unwritten || second.Applied != transport.MailPolicyAppliedUnchangedReloaded {
		t.Fatalf("the unchanged save after the correction = %+v", second)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("an unchanged save wrote %v", fake.writes)
	}
	if got := strings.Join(host.calls, "; "); !strings.Contains(got, "postfix check; postfix status") || !strings.Contains(got, "postfix reload; postfix status") {
		t.Fatalf("the unchanged save did not run the verified reload: %v", host.calls)
	}
	if second.Policy.OutboundRateLimit != 46 || second.Policy.Version != first.Policy.Version {
		t.Fatalf("policy = %+v, want the values main.cf already held", second.Policy)
	}
}

// An unchanged save leaves a stopped Postfix stopped and says only that
// nothing was to be saved; an outcome that cannot be established is unknown
// and carries no proof of a change.
func TestSetMailPolicyUnchangedSaveWithAStoppedOrUnreachablePostfix(t *testing.T) {
	fake, host := policyWithRealReload(t)
	host.masterPID = 0
	resp := setMailPolicyForTest(t, readMailPolicyForTest(t))
	if resp.Code != "" || resp.Applied != transport.MailPolicyAppliedUnchanged || len(fake.writes) != 0 {
		t.Fatalf("answer = %+v writes = %v", resp, fake.writes)
	}
	for _, call := range host.calls {
		if strings.HasPrefix(call, "systemctl") || call == "postfix reload" {
			t.Fatalf("a stopped Postfix was started or reloaded: %v", host.calls)
		}
	}

	fake, host = policyWithRealReload(t)
	host.statusCannotRun = true
	resp = setMailPolicyForTest(t, readMailPolicyForTest(t))
	if resp.Code != transport.MailPolicyReloadUnknown || !resp.Unwritten || resp.Applied != "" || len(fake.writes) != 0 {
		t.Fatalf("answer = %+v writes = %v", resp, fake.writes)
	}
}

func TestSetMailPolicyReportsAFailedReloadAsWrittenNotReloaded(t *testing.T) {
	_, host := policyWithRealReload(t)
	host.reloadOutput = "postfix/postfix-script: fatal: the Postfix mail system is not running\n"

	loaded := readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, loaded)
	if resp.Code != transport.MailPolicyNotReloaded || resp.Stage != transport.MailPolicyStageReload || resp.Policy.MessageSizeMB != 50 {
		t.Fatalf("answer = %+v", resp)
	}

	// The reload command succeeded and the master was gone afterwards.
	_, host = policyWithRealReload(t)
	host.reloadKills = true
	loaded = readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	resp = setMailPolicyForTest(t, loaded)
	if resp.Code != transport.MailPolicyNotReloaded || resp.Stage != transport.MailPolicyStageVerify {
		t.Fatalf("answer = %+v", resp)
	}
}

// Success is said only for a verified reload, and says what it was.
func TestSetMailPolicySuccessIsAVerifiedReload(t *testing.T) {
	_, host := policyWithRealReload(t)
	loaded := readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, loaded)
	if resp.Code != "" || resp.Error != "" || resp.Applied != transport.MailPolicyAppliedReloaded || resp.Policy.MessageSizeMB != 50 {
		t.Fatalf("answer = %+v", resp)
	}
	if got := strings.Join(host.calls, "; "); !strings.Contains(got, "postfix check; postfix status") || !strings.Contains(got, "postfix reload; postfix status") {
		t.Fatalf("commands = %v", host.calls)
	}
}

// A Postfix the owner stopped is not started by a policy save. The values are
// in main.cf and the answer says they wait for the next start.
func TestSetMailPolicyLeavesAStoppedPostfixStopped(t *testing.T) {
	_, host := policyWithRealReload(t)
	host.masterPID = 0
	loaded := readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, loaded)
	if resp.Code != "" || resp.Applied != transport.MailPolicyAppliedNotRunning || resp.Policy.MessageSizeMB != 50 {
		t.Fatalf("answer = %+v", resp)
	}
	for _, call := range host.calls {
		if strings.HasPrefix(call, "systemctl") || call == "postfix reload" {
			t.Fatalf("a stopped Postfix was started or reloaded: %v", host.calls)
		}
	}
}

// What could not be established is said as unknown: not the verified failure,
// and never "saved".
func TestSetMailPolicyReportsAnUnknownReloadOutcomeAsUnknown(t *testing.T) {
	_, host := policyWithRealReload(t)
	host.statusCannotRun = true
	loaded := readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, loaded)
	if resp.Code != transport.MailPolicyReloadUnknown || resp.Error == "" || resp.Applied != "" {
		t.Fatalf("answer = %+v, want %q", resp, transport.MailPolicyReloadUnknown)
	}
	if resp.Policy.MessageSizeMB != 50 || resp.Policy.Version == "" {
		t.Fatalf("policy in the answer = %+v, want what main.cf holds now", resp.Policy)
	}
}

// The stages the Agent reports are the ones the Panel has sentences for.
func TestMailPolicyStagesMatchTheTransportContract(t *testing.T) {
	for agent, contract := range map[string]string{
		mailServiceStageCheck:  transport.MailPolicyStageCheck,
		mailServiceStageReload: transport.MailPolicyStageReload,
		mailServiceStageVerify: transport.MailPolicyStageVerify,
	} {
		if agent != contract {
			t.Errorf("stage %q differs from the contract's %q", agent, contract)
		}
	}
	if mailServiceReloaded != transport.MailPolicyAppliedReloaded || mailServiceNotRunning != transport.MailPolicyAppliedNotRunning {
		t.Error("the applied states differ from the contract")
	}
}
