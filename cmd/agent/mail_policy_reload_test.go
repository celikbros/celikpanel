package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The defect: a Postfix reload that failed after the policy was written to
// main.cf was logged, and the save was answered as a success. main.cf then
// held values Postfix was not running with, and nobody was told.
func TestSetMailPolicyReportsAFailedReloadAsWrittenNotReloaded(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = ownerExtended
	mailPolicyReload = func() error {
		fake.reloads++
		return errors.New("Job for postfix.service failed because the control process exited with error code.\nSee \"systemctl status postfix.service\" and \"journalctl -xeu postfix.service\" for details.")
	}

	loaded := readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, loaded)

	if resp.Code != transport.MailPolicyNotReloaded || resp.Error == "" {
		t.Fatalf("answer = %+v, want %q", resp, transport.MailPolicyNotReloaded)
	}
	// The value is in main.cf: this is a failure after a change, and the answer
	// carries what is written now with the version the next save needs.
	if want := [][]string{{"message_size_limit=52428800"}}; !reflect.DeepEqual(fake.writes, want) || fake.reloads != 1 {
		t.Fatalf("writes = %v reloads = %d", fake.writes, fake.reloads)
	}
	if resp.Policy.MessageSizeMB != 50 || resp.Policy.Version == "" || resp.Policy.Version == loaded.Version {
		t.Fatalf("policy in the answer = %+v, want what main.cf holds now", resp.Policy)
	}
	// One bounded line of what the reload said, nothing more.
	if !strings.HasPrefix(resp.Reason, "Job for postfix.service failed") || strings.Contains(resp.Reason, "\n") || len(resp.Reason) > 320 {
		t.Fatalf("reason = %q", resp.Reason)
	}
	if fake.values["smtpd_recipient_restrictions"] != ownerExtended {
		t.Fatal("a value the save did not name was rewritten")
	}

	// A save that changes nothing does not reload, so it cannot fail this way.
	fake.reloads = 0
	unchanged := setMailPolicyForTest(t, resp.Policy)
	if unchanged.Code != "" || fake.reloads != 0 {
		t.Fatalf("an unchanged save: %+v reloads = %d", unchanged, fake.reloads)
	}
}
