package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// fakePostfix stands in for main.cf behind postconf: -h prints one value line,
// -e sets the named values.
type fakePostfix struct {
	values    map[string]string
	failRead  map[string]bool
	noLine    map[string]bool
	failWrite bool
	writes    [][]string
	reloads   int
}

func (f *fakePostfix) postconf(args ...string) ([]byte, error) {
	switch args[0] {
	case "-h":
		name := args[1]
		if f.failRead[name] {
			return nil, errors.New("exit status 1")
		}
		if f.noLine[name] {
			return nil, nil
		}
		return []byte(f.values[name] + "\n"), nil
	case "-e":
		if f.failWrite {
			return []byte("postconf: fatal: open /etc/postfix/main.cf.tmp: Permission denied"), errors.New("exit status 1")
		}
		f.writes = append(f.writes, append([]string(nil), args[1:]...))
		for _, assignment := range args[1:] {
			name, value, _ := strings.Cut(assignment, "=")
			f.values[name] = value
		}
		return nil, nil
	}
	return nil, errors.New("unexpected postconf call")
}

// stockPostfix is what `postconf -h` prints on a Debian or Ubuntu Postfix that
// nobody configured: the built-in defaults.
func stockPostfix() map[string]string {
	return map[string]string{
		"message_size_limit":              "10240000",
		"smtpd_recipient_restrictions":    "",
		"smtpd_client_message_rate_limit": "0",
		"anvil_rate_time_unit":            "60s",
	}
}

func installFakePostfix(t *testing.T, values map[string]string) *fakePostfix {
	t.Helper()
	fake := &fakePostfix{values: values, failRead: map[string]bool{}, noLine: map[string]bool{}}
	oldLook, oldPostconf, oldReload := mailPolicyLookPath, mailPolicyPostconf, mailPolicyReload
	t.Cleanup(func() { mailPolicyLookPath, mailPolicyPostconf, mailPolicyReload = oldLook, oldPostconf, oldReload })
	mailPolicyLookPath = func(string) (string, error) { return "/usr/sbin/postconf", nil }
	mailPolicyPostconf = fake.postconf
	mailPolicyReload = func() (string, error) { fake.reloads++; return mailServiceReloaded, nil }
	return fake
}

func readMailPolicyForTest(t *testing.T) MailPolicy {
	t.Helper()
	var resp MailPolicyResponse
	if err := (&Agent{}).GetMailPolicy(&transport.Empty{}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" || resp.Code != "" {
		t.Fatalf("read refused: %+v", resp)
	}
	return resp.Policy
}

func setMailPolicyForTest(t *testing.T, policy MailPolicy) MailPolicyResponse {
	t.Helper()
	var resp MailPolicyResponse
	if err := (&Agent{}).SetMailPolicy(&policy, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// The defect: a failed postconf read was answered as "0 MB, no DNSBL, no
// limit" with success, and the form saved its defaults over main.cf. A value
// that could not be read is an error and carries no policy at all.
func TestGetMailPolicyReportsAFailedReadAsAnErrorNotAsZeros(t *testing.T) {
	for _, name := range mailPolicyParameters {
		for _, kind := range []string{"postconf failed", "postconf printed nothing"} {
			t.Run(name+": "+kind, func(t *testing.T) {
				fake := installFakePostfix(t, stockPostfix())
				fake.values["smtpd_recipient_restrictions"] = ownerExtended
				if kind == "postconf failed" {
					fake.failRead[name] = true
				} else {
					fake.noLine[name] = true
				}
				var resp MailPolicyResponse
				if err := (&Agent{}).GetMailPolicy(&transport.Empty{}, &resp); err != nil {
					t.Fatal(err)
				}
				if resp.Code != transport.MailPolicyUnreadable || resp.Error == "" {
					t.Fatalf("code = %q error = %q, want %q", resp.Code, resp.Error, transport.MailPolicyUnreadable)
				}
				if !reflect.DeepEqual(resp.Policy, MailPolicy{}) {
					t.Fatalf("a failed read still answered a policy: %+v", resp.Policy)
				}
			})
		}
	}
	for name, value := range map[string]string{
		"message_size_limit":              "",
		"smtpd_client_message_rate_limit": "$default_rate",
	} {
		t.Run(name+" is not a number", func(t *testing.T) {
			fake := installFakePostfix(t, stockPostfix())
			fake.values[name] = value
			var resp MailPolicyResponse
			_ = (&Agent{}).GetMailPolicy(&transport.Empty{}, &resp)
			if resp.Code != transport.MailPolicyUnreadable || !reflect.DeepEqual(resp.Policy, MailPolicy{}) {
				t.Fatalf("answered %+v, want the unreadable refusal", resp)
			}
		})
	}
}

func TestGetMailPolicyAnswersTheServerValuesWithTheirVersion(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = ownerExtended
	fake.values["smtpd_client_message_rate_limit"] = "40"

	first := readMailPolicyForTest(t)
	want := MailPolicy{MessageSizeMB: 9, DNSBLZones: []string{zen}, OutboundRateLimit: 40, Version: first.Version}
	if !reflect.DeepEqual(first, want) || !strings.HasPrefix(first.Version, "mp1-") {
		t.Fatalf("policy = %+v, want %+v with a version", first, want)
	}
	if again := readMailPolicyForTest(t); again.Version != first.Version {
		t.Fatal("the version of unchanged values changed")
	}
	for _, name := range mailPolicyParameters {
		old := fake.values[name]
		fake.values[name] = old + "1"
		if name == "smtpd_recipient_restrictions" {
			fake.values[name] = old + ", reject_unverified_recipient"
		}
		if changed := readMailPolicyForTest(t); changed.Version == first.Version {
			t.Errorf("a change of %s on the server kept the same version", name)
		}
		fake.values[name] = old
	}
}

func TestGetMailPolicyMarksRestrictionsThePanelWillNotRewrite(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = "permit_mynetworks, $my_checks"
	policy := readMailPolicyForTest(t)
	if policy.DNSBLLocked != transport.MailPolicyLockVariable || len(policy.DNSBLZones) != 0 {
		t.Fatalf("policy = %+v, want DNSBL locked as %q", policy, transport.MailPolicyLockVariable)
	}
}

// A write is refused before it reads the form's values when it cannot say
// which settings it was built from, when those are no longer the server's, and
// when the server's cannot be read at all. main.cf is not touched in any case.
func TestSetMailPolicyRefusesWithoutACurrentVersion(t *testing.T) {
	defaults := MailPolicy{MessageSizeMB: 25, DNSBLZones: nil, OutboundRateLimit: 0}

	t.Run("no version: the form never loaded", func(t *testing.T) {
		fake := installFakePostfix(t, stockPostfix())
		fake.values["smtpd_recipient_restrictions"] = ownerExtended
		resp := setMailPolicyForTest(t, defaults)
		if resp.Code != transport.MailPolicyVersionRequired || len(fake.writes) != 0 || fake.reloads != 0 {
			t.Fatalf("code = %q writes = %v reloads = %d", resp.Code, fake.writes, fake.reloads)
		}
	})

	t.Run("stale version: the server changed after the page loaded", func(t *testing.T) {
		fake := installFakePostfix(t, stockPostfix())
		loaded := readMailPolicyForTest(t)
		fake.values["smtpd_recipient_restrictions"] = ownerExtended // the owner edits main.cf
		loaded.MessageSizeMB = 50
		resp := setMailPolicyForTest(t, loaded)
		if resp.Code != transport.MailPolicyChanged || len(fake.writes) != 0 || fake.reloads != 0 {
			t.Fatalf("code = %q writes = %v reloads = %d", resp.Code, fake.writes, fake.reloads)
		}
		if fake.values["smtpd_recipient_restrictions"] != ownerExtended {
			t.Fatal("the owner's restrictions were changed by a stale save")
		}
	})

	t.Run("the current values cannot be read", func(t *testing.T) {
		fake := installFakePostfix(t, stockPostfix())
		loaded := readMailPolicyForTest(t)
		fake.failRead["smtpd_recipient_restrictions"] = true
		loaded.MessageSizeMB = 50
		resp := setMailPolicyForTest(t, loaded)
		if resp.Code != transport.MailPolicyUnreadable || len(fake.writes) != 0 || fake.reloads != 0 {
			t.Fatalf("code = %q writes = %v reloads = %d", resp.Code, fake.writes, fake.reloads)
		}
	})
}

// Saving what was loaded writes nothing and reloads nothing; changing one
// value writes that value only. A 10240000-byte limit shows as 9 MB and is not
// rounded down to 9 MiB by a save that did not change it.
func TestSetMailPolicyWritesOnlyTheValuesThatChanged(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = ownerExtended
	fake.values["anvil_rate_time_unit"] = "1h"

	loaded := readMailPolicyForTest(t)
	if resp := setMailPolicyForTest(t, loaded); resp.Code != "" || resp.Error != "" {
		t.Fatalf("an unchanged save was refused: %+v", resp)
	}
	if len(fake.writes) != 0 || fake.reloads != 0 {
		t.Fatalf("an unchanged save wrote %v and reloaded %d times", fake.writes, fake.reloads)
	}

	loaded.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, loaded)
	if resp.Code != "" {
		t.Fatalf("refused: %+v", resp)
	}
	if want := [][]string{{"message_size_limit=52428800"}}; !reflect.DeepEqual(fake.writes, want) || fake.reloads != 1 {
		t.Fatalf("writes = %v reloads = %d, want %v and one reload", fake.writes, fake.reloads, want)
	}
	if fake.values["smtpd_recipient_restrictions"] != ownerExtended || fake.values["anvil_rate_time_unit"] != "1h" {
		t.Fatalf("values the save did not change were rewritten: %v", fake.values)
	}
	// The answer is the server's new state and the version the next save needs.
	if resp.Policy.MessageSizeMB != 50 || resp.Policy.Version == "" || resp.Policy.Version == loaded.Version {
		t.Fatalf("answer = %+v, want the saved policy with a new version", resp.Policy)
	}
	if next := setMailPolicyForTest(t, resp.Policy); next.Code != "" || len(fake.writes) != 1 {
		t.Fatalf("the returned version did not admit the next save: %+v writes=%v", next, fake.writes)
	}
}

// The second defect: every save dropped what the owner had added to
// smtpd_recipient_restrictions, because the read never returned it and the
// write rebuilt the list from three fixed entries.
func TestSetMailPolicyKeepsOwnerAddedRestrictions(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = ownerExtended

	loaded := readMailPolicyForTest(t)
	loaded.DNSBLZones = []string{zen, " BL.Spamcop.net "}
	loaded.OutboundRateLimit = 30
	if resp := setMailPolicyForTest(t, loaded); resp.Code != "" {
		t.Fatalf("refused: %+v", resp)
	}
	want := [][]string{{
		"smtpd_recipient_restrictions=permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
			"reject_unknown_sender_domain, reject_rbl_client zen.spamhaus.org, reject_rbl_client bl.spamcop.net, " +
			"check_policy_service unix:private/policyd-spf",
		"smtpd_client_message_rate_limit=30",
	}}
	if !reflect.DeepEqual(fake.writes, want) {
		t.Fatalf("writes = %q\n want %q", fake.writes, want)
	}

	// Turning DNSBL off removes the two managed entries and nothing else.
	loaded = readMailPolicyForTest(t)
	loaded.DNSBLZones = nil
	if resp := setMailPolicyForTest(t, loaded); resp.Code != "" {
		t.Fatalf("refused: %+v", resp)
	}
	if got, want := fake.values["smtpd_recipient_restrictions"],
		"permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, "+
			"reject_unknown_sender_domain, check_policy_service unix:private/policyd-spf"; got != want {
		t.Fatalf("restrictions = %q\n want %q", got, want)
	}
}

func TestSetMailPolicySetsTheRateWindowOnlyWithANewLimit(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	fake.values["anvil_rate_time_unit"] = "1h"
	loaded := readMailPolicyForTest(t)
	loaded.OutboundRateLimit = 30
	if resp := setMailPolicyForTest(t, loaded); resp.Code != "" {
		t.Fatalf("refused: %+v", resp)
	}
	if want := [][]string{{"smtpd_client_message_rate_limit=30", "anvil_rate_time_unit=60s"}}; !reflect.DeepEqual(fake.writes, want) {
		t.Fatalf("writes = %v, want %v", fake.writes, want)
	}
	loaded = readMailPolicyForTest(t)
	loaded.OutboundRateLimit = 0
	_ = setMailPolicyForTest(t, loaded)
	if want := []string{"smtpd_client_message_rate_limit=0"}; !reflect.DeepEqual(fake.writes[1], want) {
		t.Fatalf("turning the limit off wrote %v, want %v", fake.writes[1], want)
	}
}

// A list the Panel will not rewrite refuses the DNSBL change with its reason
// and leaves main.cf alone; message size and rate still save.
func TestSetMailPolicyRefusesToRewriteRestrictionsItCannotPlace(t *testing.T) {
	handWritten := "reject_unauth_destination, check_policy_service unix:private/policyd-spf"
	fake := installFakePostfix(t, stockPostfix())
	fake.values["smtpd_recipient_restrictions"] = handWritten

	loaded := readMailPolicyForTest(t)
	if loaded.DNSBLLocked != transport.MailPolicyLockNoBaseline {
		t.Fatalf("locked = %q, want %q", loaded.DNSBLLocked, transport.MailPolicyLockNoBaseline)
	}
	withZone := loaded
	withZone.DNSBLZones = []string{zen}
	withZone.MessageSizeMB = 50
	resp := setMailPolicyForTest(t, withZone)
	if resp.Code != transport.MailPolicyRestrictionsUnmanaged || resp.Reason != transport.MailPolicyLockNoBaseline {
		t.Fatalf("code/reason = %q/%q", resp.Code, resp.Reason)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("a refused save still wrote %v", fake.writes)
	}

	loaded.MessageSizeMB = 50
	if resp := setMailPolicyForTest(t, loaded); resp.Code != "" {
		t.Fatalf("message size alone was refused: %+v", resp)
	}
	if want := [][]string{{"message_size_limit=52428800"}}; !reflect.DeepEqual(fake.writes, want) {
		t.Fatalf("writes = %v, want %v", fake.writes, want)
	}
	if fake.values["smtpd_recipient_restrictions"] != handWritten {
		t.Fatal("the hand-written restrictions were changed")
	}
}

// A value outside what the Panel sets is refused, never replaced by a default
// (the old writer turned an out-of-range size into 25 MB and dropped a zone it
// could not parse, which silently turned DNSBL off).
func TestSetMailPolicyRefusesInvalidValuesInsteadOfSubstitutingDefaults(t *testing.T) {
	cases := []struct {
		name   string
		change func(*MailPolicy)
		reason string
	}{
		{"size zero", func(p *MailPolicy) { p.MessageSizeMB = 0 }, transport.MailPolicyInvalidSize},
		{"size too large", func(p *MailPolicy) { p.MessageSizeMB = 201 }, transport.MailPolicyInvalidSize},
		{"zones separated by a semicolon", func(p *MailPolicy) { p.DNSBLZones = []string{"zen.spamhaus.org; bl.spamcop.net"} }, transport.MailPolicyInvalidZone},
		{"a zone with a reply filter", func(p *MailPolicy) { p.DNSBLZones = []string{"zen.spamhaus.org=127.0.0.2"} }, transport.MailPolicyInvalidZone},
		{"negative rate", func(p *MailPolicy) { p.OutboundRateLimit = -1 }, transport.MailPolicyInvalidRate},
		{"rate too large", func(p *MailPolicy) { p.OutboundRateLimit = 10001 }, transport.MailPolicyInvalidRate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := installFakePostfix(t, stockPostfix())
			loaded := readMailPolicyForTest(t)
			tc.change(&loaded)
			resp := setMailPolicyForTest(t, loaded)
			if resp.Code != transport.MailPolicyInvalid || resp.Reason != tc.reason || len(fake.writes) != 0 {
				t.Fatalf("code/reason = %q/%q writes = %v, want %q/%q and no write",
					resp.Code, resp.Reason, fake.writes, transport.MailPolicyInvalid, tc.reason)
			}
		})
	}
}

func TestSetMailPolicyReportsAFailedWriteWithoutPostconfWords(t *testing.T) {
	fake := installFakePostfix(t, stockPostfix())
	loaded := readMailPolicyForTest(t)
	loaded.MessageSizeMB = 50
	fake.failWrite = true
	resp := setMailPolicyForTest(t, loaded)
	if resp.Code != transport.MailPolicyWriteFailed || fake.reloads != 0 {
		t.Fatalf("code = %q reloads = %d", resp.Code, fake.reloads)
	}
	if strings.Contains(resp.Error, "main.cf") || strings.Contains(resp.Error, "Permission denied") {
		t.Fatalf("postconf's own words reached the answer: %q", resp.Error)
	}
}

func TestMailPolicyWithoutPostfixKeepsItsFixedAnswer(t *testing.T) {
	installFakePostfix(t, stockPostfix())
	mailPolicyLookPath = func(string) (string, error) { return "", errors.New("not found") }
	var read, write MailPolicyResponse
	_ = (&Agent{}).GetMailPolicy(&transport.Empty{}, &read)
	_ = (&Agent{}).SetMailPolicy(&MailPolicy{Version: "mp1-x"}, &write)
	if read.Error != mailPolicyNotInstalled || write.Error != mailPolicyNotInstalled || read.Code != "" || write.Code != "" {
		t.Fatalf("read = %+v write = %+v", read, write)
	}
}
