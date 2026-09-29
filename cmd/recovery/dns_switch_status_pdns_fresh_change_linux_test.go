//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// Batch 8 (2026-09-30, c09 and c10): the Agent released a fresh paired
// PowerDNS primary because the owner had edited the database (c09, after
// start) or the configuration (c10, before start), and its ledger message
// named that change. dns-switch-status --quiesced did not: c09 said "resolve
// the reported native error", c10 "could not prove that this first PowerDNS
// install ... never started". These tests pin that the status now compares,
// read-only, what the install wrote with what is there, and says what
// differs, where, who acts and how the install resumes.

const (
	freshChangeConfigMessage = "The first install of PowerDNS as the paired primary found that the PowerDNS configuration is not as this install wrote it (an administrator edit, PowerDNS behaviour CelikPanel has not measured, or a file it could not read). CelikPanel kept that change and neither continued nor undid the install."
	freshChangeSQLMessage    = "The first install of PowerDNS as the paired primary found that the PowerDNS database is not as this install wrote it (an administrator edit, PowerDNS behaviour CelikPanel has not measured, or a file it could not read). CelikPanel kept that change and neither continued nor undid the install."
	freshPrestartUnproven    = "The first install of PowerDNS as the paired primary was interrupted before PowerDNS started. CelikPanel undoes such an install by itself, but it could not prove that the server is exactly as the install left it, so it changed nothing more."
)

type freshChangeStatusCase struct {
	statusExitCase
	change  dnsenginerecovery.FreshPrimaryChangeV3
	err     error
	calls   int
	seen    dnsengineartifact.UnitSnapshot
	noQuiet bool // run without --quiesced
}

func freshChangeEvidence(phase, message string) dnsenginerecovery.SwitchEvidence {
	e := ownerGuidanceReleased(ownerGuidanceFreshPrestartEvidence(), dnsengineartifact.ReleasedNativeUnknownCode)
	e.Journal.Phase, e.Observation.Phase = phase, phase
	e.AcceptedJob.ErrorMessage = message
	e.Observation.NativeUnits = []string{"pdns.service"}
	e.Observation.TargetReceipt = dnsenginerecovery.TargetReceiptAbsent
	e.Observation.SourceReceipt = dnsenginerecovery.SourceReceiptMutualAbsence
	e.Observation.SourceOwnership = dnsenginerecovery.SourceOwnershipNotApplicable
	e.Observation.EvidenceSHA256 = "secured-evidence-fingerprint"
	return e
}

func newFreshChangeStatusCase(phase, message string, active bool) *freshChangeStatusCase {
	unit := dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}
	if active {
		unit = dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	}
	return &freshChangeStatusCase{statusExitCase: statusExitCase{
		evidence: freshChangeEvidence(phase, message),
		units:    []dnsenginerecovery.NativeUnitObservation{unit},
	}}
}

func (c *freshChangeStatusCase) run(t *testing.T) (int, string, string) {
	t.Helper()
	readers := c.statusExitCase.readers(t)
	{
		readers.freshPrimaryChange = func(_ context.Context, _ string, _ servicemutationledger.FileOwner,
			policy dnsengineartifact.JournalPolicy, e dnsenginerecovery.SwitchEvidence,
			pdns dnsengineartifact.UnitSnapshot) (dnsenginerecovery.FreshPrimaryChangeV3, error) {
			c.calls++
			c.seen = pdns
			if policy.PDNSDatabasePath != "/var/lib/powerdns/pdns.sqlite3" || e.Journal.MutationRequestID != ownerGuidanceRequest {
				t.Fatalf("comparison got policy %+v request %s", policy, e.Journal.MutationRequestID)
			}
			return c.change, c.err
		}
	}
	args := []string{"dns-switch-status", "--quiesced", "--request-id", ownerGuidanceRequest}
	if c.noQuiet {
		args = []string{"dns-switch-status"}
	}
	var out, diagnostic bytes.Buffer
	code := runDNSSwitchStatusWith(args, 0, &out, &diagnostic, readers)
	return code, out.String(), diagnostic.String()
}

func requireFreshChangeText(t *testing.T, text string, want, forbid []string) {
	t.Helper()
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Fatalf("status lacks %q:\n%s", fragment, text)
		}
	}
	for _, fragment := range forbid {
		if strings.Contains(text, fragment) {
			t.Fatalf("status contains %q:\n%s", fragment, text)
		}
	}
}

// Generic lines a classified owner change must not print: they told the
// owner to resolve an unnamed native error or to prove a never-started
// target, not what the Agent refused on.
var freshChangeGenericLines = []string{
	"resolve the reported native error",
	"could not prove that this first PowerDNS install",
	"No owner recovery command applies",
	"If PowerDNS never started for this fresh paired primary",
}

func TestDNSSwitchStatusNamesChangedFreshPrimaryConfiguration(t *testing.T) {
	c := newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Prestart: true, Compared: true, ConfigPaths: []string{"/etc/powerdns/pdns.conf"}}
	code, out, diagnostic := c.run(t)
	if code != exitOK || diagnostic != "" || c.calls != 1 {
		t.Fatalf("classified owner change: exit %d calls %d diagnostic %q", code, c.calls, diagnostic)
	}
	if c.seen != (dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}) {
		t.Fatalf("comparison got unit %+v", c.seen)
	}
	want := "DNS switch request " + ownerGuidanceRequest + ": released-undecided-with-journal (journal phase target-staged).\n" +
		"The Agent held the first install of PowerDNS as the paired primary (journal phase target-staged; the journal records no PowerDNS start) because the PowerDNS configuration was not as the install wrote it. " +
		"This check finds now: the PowerDNS configuration file /etc/powerdns/pdns.conf is not as the install wrote it. " +
		"CelikPanel kept that change and neither continued nor undid the install; the journal and the PowerDNS files are kept and block new DNS changes, and other server changes can continue. " +
		"Next step, for the server owner: either undo that change so it is again exactly as the install wrote it, rerun /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id " + ownerGuidanceRequest +
		" to confirm that no difference remains, and restart the Agent (systemctl restart celikpanel-agent), which then re-checks this install and undoes it by itself when it proves that PowerDNS never started; " +
		"or keep the change, keep the journal and contact support with request id " + ownerGuidanceRequest + ", because CelikPanel will not overwrite it. This status check changed nothing.\n" +
		"Frozen native inverse shape: pdns-switch."
	if !strings.HasPrefix(out, want) {
		t.Fatalf("status text:\n%s\nwant prefix:\n%s", out, want)
	}
	requireFreshChangeText(t, out, nil, freshChangeGenericLines)
}

func TestDNSSwitchStatusNamesChangedFreshPrimaryDatabaseAfterStart(t *testing.T) {
	c := newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStarted, freshChangeSQLMessage, true)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Compared: true, Database: true}
	code, out, diagnostic := c.run(t)
	if code != exitOK || diagnostic != "" {
		t.Fatalf("exit %d diagnostic %q", code, diagnostic)
	}
	requireFreshChangeText(t, out, []string{
		"(journal phase target-started; PowerDNS may have started, so CelikPanel only completes this install and never removes it) because the PowerDNS database was not as the install wrote it.",
		"This check finds now: the PowerDNS database /var/lib/powerdns/pdns.sqlite3 holds content that neither the install nor PowerDNS's own start-up wrote (it is not shown here).",
		"CelikPanel kept that change and neither continued nor undid the install",
		"which then re-checks PowerDNS and continues this same install forward;",
		"contact support with request id " + ownerGuidanceRequest + ", because CelikPanel will not overwrite it.",
		"This status check changed nothing.",
		"Native unit pdns.service: load=loaded active=active unit-file=enabled.",
	}, append([]string{"undoes it by itself", "recover-dns-pdns-fresh-prestart"}, freshChangeGenericLines...))
}

func TestDNSSwitchStatusNamesChangedFreshPrimaryRecords(t *testing.T) {
	c := newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStarted, freshChangeSQLMessage, true)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Compared: true, StateRecord: true, OwnershipRecord: true,
		ConfigPaths: []string{"/etc/powerdns/pdns.d/celikpanel.conf", "/etc/powerdns/pdns.d/celikpanel-cluster.conf"}}
	code, out, _ := c.run(t)
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	requireFreshChangeText(t, out, []string{
		"the PowerDNS configuration files /etc/powerdns/pdns.d/celikpanel.conf, /etc/powerdns/pdns.d/celikpanel-cluster.conf are not as the install wrote them; ",
		"CelikPanel's DNS state record " + installedDNSJournalPolicy(991).StatePath + " is not as the install wrote it; ",
		"CelikPanel's PowerDNS ownership record ",
		"dns-engine-ownership-pdns.json is not as the install wrote it.",
		"Do not edit CelikPanel's records by hand to make them match; if you did not change them, contact support.",
	}, freshChangeGenericLines)
	// Before start, a state record exists only if something else wrote it.
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshPrestartUnproven, false)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Prestart: true, Compared: true, StateRecord: true, Database: true}
	if code, out, _ = c.run(t); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	requireFreshChangeText(t, out, []string{
		"(journal phase target-staged; the journal records no PowerDNS start); the Agent's reason is this operation's message in CelikPanel. This check finds now: ",
		"exists, although the install writes it only after PowerDNS has started",
		"the PowerDNS database files (/var/lib/powerdns/pdns.sqlite3 or the copy the install staged) are not as the install wrote them",
	}, freshChangeGenericLines)
}

func TestDNSSwitchStatusRevertedFreshPrimaryChangeNamesTheNextStep(t *testing.T) {
	// Before start: the Agent restart and, as its own admission accepts
	// this release, the owner command.
	c := newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Prestart: true, Compared: true}
	code, out, diagnostic := c.run(t)
	if code != exitOK || diagnostic != "" {
		t.Fatalf("exit %d diagnostic %q", code, diagnostic)
	}
	requireFreshChangeText(t, out, []string{
		"because the PowerDNS configuration was not as the install wrote it. This check now finds the PowerDNS configuration, the database and CelikPanel's DNS records as the install wrote them, so that change is no longer present.",
		"Next step, for the server owner: restart the Agent (systemctl restart celikpanel-agent), which then re-checks this install and undoes it by itself when it proves that PowerDNS never started.",
		"Without the Agent, /usr/libexec/celikpanel/recovery recover-dns-pdns-fresh-prestart --request-id " + ownerGuidanceRequest + " restores the state before the install",
		"This status check changed nothing.",
	}, append([]string{"contact support"}, freshChangeGenericLines...))
	// After start: only the Agent restart, which goes forward.
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStarted, freshChangeSQLMessage, true)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Compared: true}
	if code, out, _ = c.run(t); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	requireFreshChangeText(t, out, []string{
		"because the PowerDNS database was not as the install wrote it.",
		"so that change is no longer present.",
		"restart the Agent (systemctl restart celikpanel-agent), which then re-checks PowerDNS and continues this same install forward.",
	}, append([]string{"recover-dns-pdns-fresh-prestart", "undoes it"}, freshChangeGenericLines...))
}

func TestDNSSwitchStatusUnknownFreshPrimaryComparisonExitsUnavailable(t *testing.T) {
	c := newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStarted, freshChangeSQLMessage, true)
	c.err = &dnsenginerecovery.FreshPrimaryUnknownV3{What: dnsenginerecovery.FreshPrimaryChangedDatabaseV3, Err: errors.New("database is locked")}
	code, out, diagnostic := c.run(t)
	if code != exitUnavailable {
		t.Fatalf("unknown comparison exit %d, want %d", code, exitUnavailable)
	}
	requireFreshChangeText(t, out, []string{
		"This check could not tell whether something is still not as the install wrote it: the PowerDNS database could not be compared with what the install wrote",
		"Nothing was concluded, and this status check changed nothing.",
		"rerun /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id " + ownerGuidanceRequest + " in a moment",
		"contact support with request id " + ownerGuidanceRequest,
	}, append([]string{"This check finds now", "no longer present"}, freshChangeGenericLines...))
	requireFreshChangeText(t, diagnostic, []string{"could not be compared", "database is locked"}, nil)
	// A file that could not be read is unknown the same way.
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.err = &dnsenginerecovery.FreshPrimaryUnknownV3{What: dnsenginerecovery.FreshPrimaryChangedConfigV3, Err: errors.New("PowerDNS config /etc/powerdns/pdns.conf could not be read")}
	if code, out, diagnostic = c.run(t); code != exitUnavailable || !strings.Contains(out, "it: the PowerDNS configuration could not be compared") ||
		!strings.Contains(diagnostic, "/etc/powerdns/pdns.conf could not be read") {
		t.Fatalf("unreadable config: exit %d\n%s\n%s", code, out, diagnostic)
	}
}

func TestDNSSwitchStatusFreshPrimaryComparisonKeepsOtherReasons(t *testing.T) {
	// Found as the install wrote it, but the Agent held the install for
	// another reason: that reason's lines stay, after one line saying so.
	c := newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshPrestartUnproven, false)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Prestart: true, Compared: true}
	code, out, _ := c.run(t)
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	requireFreshChangeText(t, out, []string{
		"This check found the PowerDNS configuration, the database and CelikPanel's DNS records as the install wrote them; the reason the Agent held this install follows.\n",
		"If PowerDNS never started for this fresh paired primary",
		"could not prove that this first PowerDNS install",
	}, []string{"no longer present"})
	// The Agent's route stops before the comparisons: nothing is added.
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.change = dnsenginerecovery.FreshPrimaryChangeV3{Prestart: true}
	if code, out, _ = c.run(t); code != exitOK || strings.Contains(out, "as the install wrote them") ||
		!strings.Contains(out, "could not prove that this first PowerDNS install") {
		t.Fatalf("uncompared route: exit %d\n%s", code, out)
	}
	// Unquiesced status does not compare; unit state unknown is not compared
	// (the unit step reports it with exit 3).
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.noQuiet = true
	if c.run(t); c.calls != 0 {
		t.Fatal("unquiesced status compared native files")
	}
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.unitErr = errors.New("systemctl timed out")
	if code, _, _ = c.run(t); code != exitUnavailable || c.calls != 0 {
		t.Fatalf("unknown units: exit %d calls %d", code, c.calls)
	}
	// Another release reason or a V1 journal is never compared.
	c = newFreshChangeStatusCase(dnsengineartifact.SwitchPhaseTargetStaged, freshChangeConfigMessage, false)
	c.evidence = ownerGuidanceReleased(c.evidence, dnsengineartifact.ReleasedHostWindowCode)
	if c.run(t); c.calls != 0 {
		t.Fatal("host-window release compared")
	}
}
