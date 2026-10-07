package main

import (
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	zen     = "zen.spamhaus.org"
	spamcop = "bl.spamcop.net"

	// What the Panel itself wrote before 8 Oct 2026, with and without DNSBL.
	panelBaseline = "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination"
	panelWithZen  = panelBaseline + ", reject_rbl_client zen.spamhaus.org"

	// A list an owner extended by hand after the Panel wrote it.
	ownerExtended = "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
		"reject_unknown_sender_domain, reject_rbl_client zen.spamhaus.org, " +
		"check_policy_service unix:private/policyd-spf"
)

// The whole point of the rewrite: what the owner added stays, in its place, and
// a save that changes nothing writes nothing. Values are the shapes main.cf
// really holds: stock Debian/Ubuntu (empty), the Panel's own earlier output,
// owner-added checks, a different permit order, space-separated and multi-line
// lists.
func TestPlanRecipientRestrictionsPreservesWhatThePanelDoesNotManage(t *testing.T) {
	cases := []struct {
		name    string
		current string
		wanted  []string
		next    string
		changed bool
	}{
		{
			name:    "stock Debian or Ubuntu, DNSBL stays off: nothing is written",
			current: "", wanted: nil, next: "", changed: false,
		},
		{
			name:    "stock Debian or Ubuntu, DNSBL turned on: the baseline and the zone",
			current: "", wanted: []string{zen},
			next: panelWithZen, changed: true,
		},
		{
			name:    "the Panel's own earlier output, same zone: stable",
			current: panelWithZen, wanted: []string{zen},
			next: panelWithZen, changed: false,
		},
		{
			name:    "the Panel's own earlier output without DNSBL, still off: stable",
			current: panelBaseline, wanted: nil,
			next: panelBaseline, changed: false,
		},
		{
			name:    "the Panel's own earlier output, DNSBL turned off: only the zone goes",
			current: panelWithZen, wanted: nil,
			next: panelBaseline, changed: true,
		},
		{
			name:    "a second zone joins the first",
			current: panelWithZen, wanted: []string{zen, spamcop},
			next: panelWithZen + ", reject_rbl_client bl.spamcop.net", changed: true,
		},
		{
			name:    "a zone replaced by another takes its place",
			current: ownerExtended, wanted: []string{spamcop},
			next: "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
				"reject_unknown_sender_domain, reject_rbl_client bl.spamcop.net, " +
				"check_policy_service unix:private/policyd-spf",
			changed: true,
		},
		{
			name:    "owner-added restrictions around the zone, same zone: stable",
			current: ownerExtended, wanted: []string{zen},
			next: ownerExtended, changed: false,
		},
		{
			name:    "owner-added restrictions around the zone, DNSBL off: they stay in place",
			current: ownerExtended, wanted: nil,
			next: "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
				"reject_unknown_sender_domain, check_policy_service unix:private/policyd-spf",
			changed: true,
		},
		{
			name:    "owner-added restrictions around the zone, a second zone: next to the first",
			current: ownerExtended, wanted: []string{zen, spamcop},
			next: "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
				"reject_unknown_sender_domain, reject_rbl_client zen.spamhaus.org, " +
				"reject_rbl_client bl.spamcop.net, check_policy_service unix:private/policyd-spf",
			changed: true,
		},
		{
			name: "owner-added check_policy_service and no zone yet: the zone goes after everything the owner wrote",
			current: "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
				"check_policy_service unix:private/policyd-spf",
			wanted: []string{zen},
			next: "permit_mynetworks, permit_sasl_authenticated, reject_unauth_destination, " +
				"check_policy_service unix:private/policyd-spf, reject_rbl_client zen.spamhaus.org",
			changed: true,
		},
		{
			name:    "permit_sasl_authenticated first: the owner's order is kept",
			current: "permit_sasl_authenticated, permit_mynetworks, reject_unauth_destination",
			wanted:  []string{zen},
			next:    "permit_sasl_authenticated, permit_mynetworks, reject_unauth_destination, reject_rbl_client zen.spamhaus.org",
			changed: true,
		},
		{
			name:    "a space-separated list keeps its style",
			current: "permit_mynetworks permit_sasl_authenticated defer_unauth_destination",
			wanted:  []string{zen},
			next:    "permit_mynetworks permit_sasl_authenticated defer_unauth_destination reject_rbl_client zen.spamhaus.org",
			changed: true,
		},
		{
			name: "a multi-line value, same zone: not touched, not even reformatted",
			current: "permit_mynetworks,\n    permit_sasl_authenticated,\n    reject_unauth_destination,\n" +
				"    reject_rbl_client zen.spamhaus.org",
			wanted: []string{zen},
			next: "permit_mynetworks,\n    permit_sasl_authenticated,\n    reject_unauth_destination,\n" +
				"    reject_rbl_client zen.spamhaus.org",
			changed: false,
		},
		{
			name: "a multi-line value, DNSBL off",
			current: "permit_mynetworks,\n    permit_sasl_authenticated,\n    reject_unauth_destination,\n" +
				"    reject_rbl_client zen.spamhaus.org",
			wanted: nil, next: panelBaseline, changed: true,
		},
		{
			name:    "a zone written in capitals is the same zone",
			current: panelBaseline + ", reject_rbl_client Zen.Spamhaus.ORG",
			wanted:  []string{zen},
			next:    panelBaseline + ", reject_rbl_client Zen.Spamhaus.ORG", changed: false,
		},
		{
			name:    "the same zone listed twice is removed twice",
			current: panelWithZen + ", reject_rbl_client zen.spamhaus.org",
			wanted:  nil, next: panelBaseline, changed: true,
		},
		{
			name:    "an owner DNSBL entry with a reply filter is not the Panel's",
			current: panelBaseline + ", reject_rbl_client zen.spamhaus.org=127.0.0.[2..11]",
			wanted:  []string{spamcop},
			next: panelBaseline + ", reject_rbl_client zen.spamhaus.org=127.0.0.[2..11]" +
				", reject_rbl_client bl.spamcop.net",
			changed: true,
		},
		{
			name:    "an owner DNSBL trial behind warn_if_reject is not the Panel's",
			current: panelBaseline + ", warn_if_reject reject_rbl_client dnsbl.example.org",
			wanted:  nil,
			next:    panelBaseline + ", warn_if_reject reject_rbl_client dnsbl.example.org", changed: false,
		},
		{
			name: "a policy service written with braces stays one element",
			current: panelBaseline + ", check_policy_service { inet:127.0.0.1:10023, timeout=10s, default_action=DUNNO }" +
				", reject_rbl_client zen.spamhaus.org",
			wanted:  nil,
			next:    panelBaseline + ", check_policy_service { inet:127.0.0.1:10023, timeout=10s, default_action=DUNNO }",
			changed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, changed, lock := planRecipientRestrictions(tc.current, tc.wanted)
			if lock != "" {
				t.Fatalf("refused with %q; want a plan", lock)
			}
			if next != tc.next || changed != tc.changed {
				t.Fatalf("plan = %q changed=%v\n want %q changed=%v", next, changed, tc.next, tc.changed)
			}
			// Saving the result again changes nothing: the plan converges.
			again, changedAgain, lockAgain := planRecipientRestrictions(next, tc.wanted)
			if lockAgain != "" || changedAgain || again != next {
				t.Fatalf("a second save is not stable: %q changed=%v lock=%q", again, changedAgain, lockAgain)
			}
		})
	}
}

// Where the right result is not certain the Panel refuses the DNSBL change and
// names why, instead of rewriting the owner's list. The same value with no
// DNSBL change asked is left exactly as it is.
func TestPlanRecipientRestrictionsRefusesWhatItCannotPlaceWithCertainty(t *testing.T) {
	cases := []struct {
		name    string
		current string
		lock    string
	}{
		{"a reference to another setting", "permit_mynetworks, permit_sasl_authenticated, $my_recipient_checks", transport.MailPolicyLockVariable},
		{"an unclosed brace", panelBaseline + ", check_policy_service { inet:127.0.0.1:10023, timeout=10s", transport.MailPolicyLockMalformed},
		{"a closing brace with no opening", panelBaseline + " }", transport.MailPolicyLockMalformed},
		{"reject_rbl_client with no zone", panelBaseline + ", reject_rbl_client", transport.MailPolicyLockMalformed},
		{"a hand-written list without the two permits", "reject_unauth_destination, check_policy_service unix:private/policyd-spf", transport.MailPolicyLockNoBaseline},
		{"a hand-written list with only one permit", "permit_mynetworks, reject_unauth_destination", transport.MailPolicyLockNoBaseline},
		{"a permit that is only a warn_if_reject trial", "permit_mynetworks, warn_if_reject permit_sasl_authenticated, reject_unauth_destination", transport.MailPolicyLockNoBaseline},
		{"a list that ends with permit", panelBaseline + ", permit", transport.MailPolicyLockTerminal},
		{"a list that ends with reject", panelBaseline + ", reject", transport.MailPolicyLockTerminal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRecipientRestrictions(tc.current).lock; got != tc.lock {
				t.Fatalf("lock = %q, want %q", got, tc.lock)
			}
			next, changed, lock := planRecipientRestrictions(tc.current, []string{zen})
			if lock != tc.lock || changed || next != "" {
				t.Fatalf("a DNSBL change was planned: %q changed=%v lock=%q, want refusal %q", next, changed, lock, tc.lock)
			}
			// Nothing asked of the DNSBL entries: the value is not touched.
			next, changed, lock = planRecipientRestrictions(tc.current, parseRecipientRestrictions(tc.current).zones)
			if lock != "" || changed || next != tc.current {
				t.Fatalf("an unchanged save must leave the value alone: %q changed=%v lock=%q", next, changed, lock)
			}
		})
	}
}

// A list that ends with permit but already carries a managed zone has a known
// place for the next zone, so it is not refused.
func TestPlanRecipientRestrictionsPlacesNextToAnExistingZoneBeforeATerminalAction(t *testing.T) {
	current := panelWithZen + ", permit"
	next, changed, lock := planRecipientRestrictions(current, []string{zen, spamcop})
	want := panelWithZen + ", reject_rbl_client bl.spamcop.net, permit"
	if lock != "" || !changed || next != want {
		t.Fatalf("plan = %q changed=%v lock=%q, want %q", next, changed, lock, want)
	}
}

func TestParseRecipientRestrictionsReportsOnlyManagedZones(t *testing.T) {
	parsed := parseRecipientRestrictions(panelBaseline +
		", reject_rbl_client zen.spamhaus.org=127.0.0.2" +
		", warn_if_reject reject_rbl_client trial.example.org" +
		", reject_rbl_client bl.spamcop.net, reject_rhsbl_sender dbl.spamhaus.org" +
		", reject_rbl_client BL.Spamcop.net")
	if !reflect.DeepEqual(parsed.zones, []string{spamcop}) || parsed.lock != "" {
		t.Fatalf("zones = %v lock=%q, want only %s", parsed.zones, parsed.lock, spamcop)
	}
}

func TestValidDNSBLZone(t *testing.T) {
	for _, zone := range []string{"zen.spamhaus.org", "bl.spamcop.net", "b.barracudacentral.org", "dnsbl-1.uceprotect.net"} {
		if !validDNSBLZone(zone) {
			t.Errorf("%q refused", zone)
		}
	}
	for _, zone := range []string{"", "localhost", "zen.spamhaus.org=127.0.0.2", "a b.org", "zen.spamhaus.org;", "-x.org", "x-.org", "x..org", "$zone.org", "{x}.org"} {
		if validDNSBLZone(zone) {
			t.Errorf("%q accepted", zone)
		}
	}
}
