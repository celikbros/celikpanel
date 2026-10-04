//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/transport"
)

// scaledPeerProofBudget is the production budget divided by 50 (15 s wave ->
// 300 ms), so the timing tests keep the production proportions.
func scaledPeerProofBudget() dnsPeerProofBudget {
	b := dnsPeerProofSteps
	return dnsPeerProofBudget{
		prepare: b.prepare / 50, challengeWrite: b.challengeWrite / 50, exchange: b.exchange / 50,
		postInspection: b.postInspection / 50, consume: b.consume / 50, expiryReserve: b.expiryReserve / 50,
	}
}

// sleepUnder is a step's work: it takes d unless ctx ends first, like a
// command started with exec.CommandContext.
func sleepUnder(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func refusedDeletionWaveProbes() (dnsV3PrimaryPropagationPlan, dnsZoneSOAProbe, dnsCatalogAXFRProbe, dnsBoundCatalogAXFRProbe, dnsBoundZoneAXFRProbe) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := dnsV3PrimaryPropagationPlan{
		Evidence: evidence,
		Changed:  expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	localAXFR := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: evidence.Serial}, nil
	}
	soa := func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
		if domain == evidence.Domain {
			return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{evidence.Serial}}, nil
		}
		return dnsSOAProbeResult{LocalIP: evidence.LocalIP, RCode: dnsRCodeRefused}, nil
	}
	return plan, soa, localAXFR, exactTestPeerCatalogAXFR(evidence), absentTestPeerZoneAXFR(evidence)
}

func withScaledWave(t *testing.T) time.Duration {
	t.Helper()
	previous := dnsPairProofWaveLimit
	dnsPairProofWaveLimit = dnsPairProofLimit / 50
	t.Cleanup(func() { dnsPairProofWaveLimit = previous })
	return dnsPairProofWaveLimit
}

// pair 6 P6-1: the peer's answer is accepted at t=14.5 s of the 15 s wave and
// consume-once (three rechecks that run commands under their context) takes
// 3 s. Before the fix every recheck ran under the expired wave context and the
// positive proof was discarded as dns_peer_journal_unknown. Scaled by 1/50.
func TestNativePeerProofAcceptedLateInWaveCompletesUnderFreshContext(t *testing.T) {
	logs := captureAgentLog(t)
	wave := withScaledWave(t)
	budget := scaledPeerProofBudget()
	plan, soa, localAXFR, peerCatalog, peerZone := refusedDeletionWaveProbes()
	acceptAt := wave * 145 / 150 // 14.5 s of 15 s
	recheck := wave / 15         // 1 s each: consume takes 3 s
	var waveStart time.Time
	var nativeDeadline time.Time
	var hasNativeDeadline bool
	consumeRechecks := 0
	var consumedAt time.Time
	native := func(ctx context.Context, _ dnsPeerAXFRAuthority, _ dnsV3PrimaryPropagationPlan) (result error) {
		nativeDeadline, hasNativeDeadline = ctx.Deadline()
		run := newDNSPeerProofRun(ctx, budget, "gone.example.test", "test")
		defer func() { run.finish(result) }()
		if err := run.step(dnsPeerProofStepPrepare, func(ctx context.Context) error {
			if err := sleepUnder(ctx, wave*2/15); err != nil {
				return err
			}
			// The challenge lives 30 s (scaled).
			run.challengeMinted(time.Now().Add(30 * time.Second / 50))
			return nil
		}); err != nil {
			return err
		}
		for _, step := range []dnsPeerProofStep{dnsPeerProofStepChallengeWrite, dnsPeerProofStepExchange} {
			if err := run.step(step, func(ctx context.Context) error { return sleepUnder(ctx, wave*4/15) }); err != nil {
				return err
			}
		}
		if err := run.step(dnsPeerProofStepPostInspection, func(ctx context.Context) error {
			return sleepUnder(ctx, time.Until(waveStart.Add(acceptAt)))
		}); err != nil {
			return err
		}
		return run.consume(func() error {
			// Accepted: every recheck runs a "command" under the consume
			// context, as exec.CommandContext would.
			ctx := run.accepted()
			for i := 0; i < 3; i++ {
				if err := sleepUnder(ctx, recheck); err != nil {
					return errors.New("consume-once recheck could not run: " + err.Error())
				}
				consumeRechecks++
			}
			consumedAt = time.Now()
			return nil
		})
	}
	waveStart = time.Now()
	err := completeDNSV3PrimaryPropagationWithNativeAt(context.Background(), plan, soa, localAXFR,
		peerCatalog, peerZone, native)
	if err != nil {
		t.Fatalf("a positive proof accepted late in the wave was discarded: %v\n%s", err, logs)
	}
	if hasNativeDeadline && !nativeDeadline.After(waveStart.Add(wave)) {
		t.Fatalf("the native proof received the wave's context (deadline %s after the wave start)",
			nativeDeadline.Sub(waveStart))
	}
	if consumeRechecks != 3 || !consumedAt.After(waveStart.Add(wave)) {
		t.Fatalf("consume did not run past the wave's bound: rechecks=%d at %s (wave %s)",
			consumeRechecks, consumedAt.Sub(waveStart), wave)
	}
	if !strings.Contains(logs.String(), "outcome verified") {
		t.Fatalf("no verified outcome logged:\n%s", logs)
	}
}

// A deadline during the SSH exchange, before any answer, is the reviewed
// dns_peer_proof_timeout: consume never runs. The enrollment's transport
// bound, when tighter, is the step's own deadline, so the transport can never
// outlive the step and misreport its timeout as an inspection failure.
func TestNativePeerProofDeadlineDuringExchangeIsProofTimeout(t *testing.T) {
	logs := captureAgentLog(t)
	withScaledWave(t)
	budget := scaledPeerProofBudget()
	plan, soa, localAXFR, peerCatalog, peerZone := refusedDeletionWaveProbes()
	transportLimit := budget.exchange / 2
	consumed := false
	native := func(ctx context.Context, _ dnsPeerAXFRAuthority, _ dnsV3PrimaryPropagationPlan) (result error) {
		run := newDNSPeerProofRun(ctx, budget, "gone.example.test", "test")
		defer func() { run.finish(result) }()
		if err := run.step(dnsPeerProofStepPrepare, func(context.Context) error { return nil }); err != nil {
			return err
		}
		if err := run.stepWithin(dnsPeerProofStepExchange, transportLimit, func(ctx context.Context) error {
			// The transport's own bound, as dnspeertransport.Inspect does.
			bounded, cancel := context.WithTimeout(ctx, transportLimit)
			defer cancel()
			<-bounded.Done()
			return pendingDNSPeerCause(inspectionPendingCode(""),
				dnspeertransport.Unknown{Code: dnspeertransport.CodeUnavailable})
		}); err != nil {
			return err
		}
		return run.consume(func() error { consumed = true; return nil })
	}
	err := completeDNSV3PrimaryPropagationWithNativeAt(context.Background(), plan, soa, localAXFR,
		peerCatalog, peerZone, native)
	if pendingDNSPeerCode(err) != transport.DNSPeerPendingProofTimeout || consumed {
		t.Fatalf("exchange deadline: code=%q consumed=%t err=%v", pendingDNSPeerCode(err), consumed, err)
	}
	if dnsZoneV3PendingLedgerCode(pendingDNSPeerCode(err)) != transport.DNSPeerPendingProofTimeout {
		t.Fatal("the ledger does not keep dns_peer_proof_timeout")
	}
	text := logs.String()
	for _, want := range []string{
		"stopped at step exchange as dns_peer_proof_timeout: peer_inspection_unavailable",
		"outcome pending dns_peer_proof_timeout",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log lacks %q:\n%s", want, text)
		}
	}
	// Once a challenge exists, a step also ends before the challenge expires
	// less the reserve consume-once needs, even within its own bound.
	run := newDNSPeerProofRun(context.Background(), budget, "gone.example.test", "test")
	run.challengeMinted(time.Now().Add(budget.expiryReserve + budget.postInspection/4))
	started := time.Now()
	err = run.step(dnsPeerProofStepPostInspection, func(ctx context.Context) error {
		return sleepUnder(ctx, budget.postInspection)
	})
	if pendingDNSPeerCode(err) != transport.DNSPeerPendingProofTimeout ||
		time.Since(started) >= budget.postInspection {
		t.Fatalf("acceptance deadline not applied: %v after %s", err, time.Since(started))
	}
}

// A check that could not run because its step's time ran out observed
// nothing: it is never reported (or logged) as an owner edit.
func TestNativePeerChecksAfterTheirDeadlineAreNotOwnerEdits(t *testing.T) {
	logs := captureAgentLog(t)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	checks := &nativePeerCurrentChecks{
		attempt:    func() error { return nil },
		enrollment: func() error { return nil },
		local: func(ctx context.Context) error {
			return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckActiveEngine, "systemctl: %v", ctx.Err())
		},
	}
	if err := checks.at(expired)(); !isDNSPeerProofDeadline(err) || pendingDNSPeerCode(err) != "" {
		t.Fatalf("recheck after the deadline: %v", err)
	}
	plan, soa, _, peerCatalog, peerZone := refusedDeletionWaveProbes()
	authority := dnsPeerAXFRAuthority{sourceIP: plan.Evidence.LocalIP, peerIP: plan.Evidence.PeerIP,
		catalog: plan.Evidence.Domain, catalogSerial: plan.Evidence.Serial}
	failing := func(ctx context.Context, _, _ string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{}, ctx.Err()
	}
	err := verifyNativePeerAfterInspectionAt(expired, recordedProducerCatalogFor(plan), authority, plan.Changed.Domain,
		nativePeerAfterInspectionProbes{soa: soa, localCatalog: failing, peerCatalog: peerCatalog, peerZone: peerZone},
		func() error { return nil })
	if !isDNSPeerProofDeadline(err) || pendingDNSPeerCode(err) != "" {
		t.Fatalf("post-inspection probe after the deadline: %v", err)
	}
	if strings.Contains(logs.String(), "observed different evidence") {
		t.Fatalf("a deadline was logged as an owner edit:\n%s", logs)
	}
}

// consume-once keeps the semantics of its re-verification: a real difference
// stays dns_peer_owner_edit_unknown with its check token; a journal failure
// or the fresh consume bound running out is dns_peer_journal_unknown, never
// dns_peer_proof_timeout.
func TestNativePeerConsumeOutcomes(t *testing.T) {
	logs := captureAgentLog(t)
	budget := scaledPeerProofBudget()
	consumeOnce := func(verify func() error) error {
		for i := 0; i < 3; i++ {
			if verify() != nil {
				return errors.New("challenge journal state unknown")
			}
		}
		return nil
	}
	newChecks := func(local func(context.Context) error) *nativePeerCurrentChecks {
		return &nativePeerCurrentChecks{
			attempt: func() error { return nil }, enrollment: func() error { return nil }, local: local,
		}
	}

	calls := 0
	edited := newChecks(func(context.Context) error {
		calls++
		if calls == 2 {
			return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckProducerCatalog,
				"recorded catalog serial 7; observed serial 9 with a new member")
		}
		return nil
	})
	run := newDNSPeerProofRun(context.Background(), budget, "gone.example.test", "test")
	err := run.consume(func() error { return edited.journalOp(run.accepted(), "consume-once", consumeOnce) })
	want := transport.DNSPeerPendingOwnerEditUnknownWithDetail(transport.DNSPeerOwnerEditCheckProducerCatalog)
	if pendingDNSPeerCode(err) != want {
		t.Fatalf("real difference at consume: %q (%v)", pendingDNSPeerCode(err), err)
	}

	journal := newChecks(func(context.Context) error { return nil })
	run = newDNSPeerProofRun(context.Background(), budget, "gone.example.test", "test")
	err = run.consume(func() error {
		return journal.journalOp(run.accepted(), "consume-once", func(func() error) error {
			return errors.New("challenge journal mismatch")
		})
	})
	if pendingDNSPeerCode(err) != transport.DNSPeerPendingJournalUnknown {
		t.Fatalf("journal failure at consume: %q", pendingDNSPeerCode(err))
	}
	if !strings.Contains(logs.String(), "stopped at step consume as dns_peer_journal_unknown: consume-once: challenge journal mismatch") {
		t.Fatalf("consume failure cause not logged:\n%s", logs)
	}

	slow := newChecks(func(ctx context.Context) error { return sleepUnder(ctx, budget.consume) })
	run = newDNSPeerProofRun(context.Background(), budget, "gone.example.test", "test")
	err = run.consume(func() error { return slow.journalOp(run.accepted(), "consume-once", consumeOnce) })
	if code := pendingDNSPeerCode(err); code != transport.DNSPeerPendingJournalUnknown {
		t.Fatalf("fresh consume bound exhausted: %q (%v)", code, err)
	}

	// The consume context survives cancellation of the request context.
	parent, cancelParent := context.WithCancel(context.Background())
	run = newDNSPeerProofRun(parent, budget, "gone.example.test", "test")
	cancelParent()
	ok := newChecks(func(ctx context.Context) error { return sleepUnder(ctx, time.Millisecond) })
	if err := run.consume(func() error { return ok.journalOp(run.accepted(), "consume-once", consumeOnce) }); err != nil {
		t.Fatalf("consume ran under a cancelled parent: %v", err)
	}
}

// D-024: one line per attempt with each step's elapsed time and the outcome;
// the inspector's answer fields are logged as bounded tokens.
func TestNativePeerProofLogsStepTimesAndAnswerFields(t *testing.T) {
	logs := captureAgentLog(t)
	run := newDNSPeerProofRun(context.Background(), scaledPeerProofBudget(), "gone.example.test", "PowerDNS")
	for _, step := range []dnsPeerProofStep{dnsPeerProofStepPrepare, dnsPeerProofStepChallengeWrite,
		dnsPeerProofStepExchange, dnsPeerProofStepPostInspection} {
		if err := run.step(step, func(ctx context.Context) error { return sleepUnder(ctx, time.Millisecond) }); err != nil {
			t.Fatal(err)
		}
	}
	err := run.consume(func() error {
		run.logAnswer("transferred", "absent", "unloaded\nFORGED line", true, nil)
		return nil
	})
	run.finish(err)
	text := logs.String()
	for _, want := range []string{
		"DNS peer inspector answer for gone.example.test (PowerDNS secondary): catalog_state=transferred member_state=absent native_state=unloaded????????line; accepted",
		"DNS peer proof for gone.example.test (PowerDNS secondary) step times: prepare=",
		" challenge_write=", " exchange=", " post_inspection=", " consume=",
		"; outcome verified",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "FORGED") {
		t.Fatalf("peer-supplied field not bounded:\n%s", text)
	}
	if got := boundedDNSPeerAnswerField(strings.Repeat("a", 40)); got != strings.Repeat("a", 32)+"..." {
		t.Fatalf("long field: %q", got)
	}
}

// The production budget: the exchange bound is the transport's, the total
// is 60 s, and the wave's DNS probes keep dnsPairProofLimit.
func TestNativePeerProofProductionBudget(t *testing.T) {
	b := dnsPeerProofSteps
	if b.exchange != dnspeertransport.ExchangeLimit {
		t.Fatalf("exchange %s != transport %s", b.exchange, dnspeertransport.ExchangeLimit)
	}
	if total := b.prepare + b.challengeWrite + b.exchange + b.postInspection + b.consume; total != 60*time.Second {
		t.Fatalf("total %s", total)
	}
	if b.consume <= dnsPairProofLimit || dnsPairProofWaveLimit != dnsPairProofLimit {
		t.Fatalf("consume %s wave %s", b.consume, dnsPairProofWaveLimit)
	}
}
