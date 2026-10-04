package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Native peer proof step budget (2026-10-01, pair 6 P6-1).
//
// Until this change the completion wave and its one native peer proof shared
// one 15 s context (dnsPairProofLimit). Writing the challenge, the SSH
// exchange and the post-inspection probes used most of it, and consume-once
// then ran its current-evidence rechecks under an expired context: a
// positive, authenticated answer was discarded as dns_peer_journal_unknown.
//
// The wave's own DNS answer probes keep dnsPairProofLimit. The native proof
// now runs from the request context, not the wave's, and each step has its
// own bound:
//
//	step             measured (pair 5/6, batch 11)   bound
//	prepare          about 1-3 s                     8 s
//	challenge_write  3.5-4.1 s                       12 s
//	exchange         2.5-4 s                         10 s (dnspeertransport.ExchangeLimit)
//	post_inspection  about 2-3 s                     10 s
//	consume          3 rechecks + write, 3-4 s       20 s, fresh context
//
// Total wall bound 60 s. Once a challenge is minted, every step before
// acceptance also ends expiryReserve before the challenge expires, so that
// consume-once still reaches its own expiry check (the challenge lives 30 s).
// A step that runs out of time before the peer's answer was accepted is
// dns_peer_proof_timeout. After dnspeerproof.Verify accepted the answer,
// consume-once and the final recheck run under a fresh context derived from
// context.WithoutCancel of the request context: a positive answer is never
// discarded because a clock of an earlier step ran out.
type dnsPeerProofStep string

const (
	dnsPeerProofStepPrepare        dnsPeerProofStep = "prepare"
	dnsPeerProofStepChallengeWrite dnsPeerProofStep = "challenge_write"
	dnsPeerProofStepExchange       dnsPeerProofStep = "exchange"
	dnsPeerProofStepPostInspection dnsPeerProofStep = "post_inspection"
	dnsPeerProofStepConsume        dnsPeerProofStep = "consume"
)

type dnsPeerProofBudget struct {
	prepare        time.Duration
	challengeWrite time.Duration
	exchange       time.Duration
	postInspection time.Duration
	consume        time.Duration
	// expiryReserve: the latest acceptance lies this long before the
	// challenge expires.
	expiryReserve time.Duration
}

// dnsPeerProofSteps is the production budget. Tests scale it down.
var dnsPeerProofSteps = dnsPeerProofBudget{
	prepare:        8 * time.Second,
	challengeWrite: 12 * time.Second,
	exchange:       10 * time.Second, // = dnspeertransport.ExchangeLimit (linux test)
	postInspection: 10 * time.Second,
	consume:        20 * time.Second,
	expiryReserve:  5 * time.Second,
}

func (b dnsPeerProofBudget) limit(step dnsPeerProofStep) time.Duration {
	switch step {
	case dnsPeerProofStepPrepare:
		return b.prepare
	case dnsPeerProofStepChallengeWrite:
		return b.challengeWrite
	case dnsPeerProofStepExchange:
		return b.exchange
	case dnsPeerProofStepPostInspection:
		return b.postInspection
	case dnsPeerProofStepConsume:
		return b.consume
	default:
		return 0
	}
}

// dnsPeerProofRun is one attempt of a native peer proof: its step bounds,
// the per-step timing and the Agent log lines of its outcome.
type dnsPeerProofRun struct {
	base     context.Context
	budget   dnsPeerProofBudget
	zone     string
	peer     string
	started  time.Time
	acceptBy time.Time
	timings  []string
	stopLog  bool

	fresh       context.Context
	cancelFresh context.CancelFunc
}

func newDNSPeerProofRun(base context.Context, budget dnsPeerProofBudget, zone, peer string) *dnsPeerProofRun {
	return &dnsPeerProofRun{base: base, budget: budget, zone: zone, peer: peer, started: time.Now()}
}

// challengeMinted bounds every later step before acceptance by the minted
// challenge's expiry less the reserve consume-once needs.
func (r *dnsPeerProofRun) challengeMinted(expiresAt time.Time) {
	r.acceptBy = expiresAt.Add(-r.budget.expiryReserve)
}

func (r *dnsPeerProofRun) record(step dnsPeerProofStep, elapsed time.Duration) {
	r.timings = append(r.timings, fmt.Sprintf("%s=%s", step, elapsed.Round(time.Millisecond)))
}

// step runs one step before the peer's answer is accepted under the step's
// own bound (and the acceptance deadline once a challenge exists).
func (r *dnsPeerProofRun) step(step dnsPeerProofStep, fn func(context.Context) error) error {
	return r.stepWithin(step, 0, fn)
}

// stepWithin is step with a tighter bound when limit is positive and lower
// than the step's own (the exchange passes the enrollment's transport bound,
// so the transport never outlives the step and its deadline is the step's).
func (r *dnsPeerProofRun) stepWithin(step dnsPeerProofStep, limit time.Duration, fn func(context.Context) error) error {
	own := r.budget.limit(step)
	if limit <= 0 || limit > own {
		limit = own
	}
	ctx, cancel := context.WithTimeout(r.base, limit)
	defer cancel()
	if !r.acceptBy.IsZero() {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, r.acceptBy)
		defer stop()
	}
	started := time.Now()
	err := fn(ctx)
	r.record(step, time.Since(started))
	if err == nil || errors.Is(err, errDNSProducerCatalogRestampedBeforeChallenge) {
		return err
	}
	if r.base.Err() == nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// Before acceptance: nothing was consumed, nothing changed on either
		// server; the same publication is retried as a new attempt.
		r.stopped(step, transport.DNSPeerPendingProofTimeout, err)
		return pendingDNSPeerCause(transport.DNSPeerPendingProofTimeout, err)
	}
	r.stopped(step, pendingDNSPeerCode(err), err)
	return err
}

// accepted returns the consume context. It is created on the first call,
// which the proof makes only inside dnspeerproof.Verify's consume callback,
// that is after the peer's answer was accepted. Its parent is the request
// context without its cancellation; it has its own bound.
func (r *dnsPeerProofRun) accepted() context.Context {
	if r.fresh == nil {
		r.fresh, r.cancelFresh = context.WithTimeout(context.WithoutCancel(r.base), r.budget.consume)
	}
	return r.fresh
}

// consume runs the acceptance step: fn calls the proof's Verify, whose
// consume callback and the final recheck use r.accepted(). A failure that
// carries no reviewed code (a journal failure, or the fresh bound running
// out) is dns_peer_journal_unknown, never dns_peer_proof_timeout; a recheck
// that found a real difference keeps its own code (owner edit with its
// check token).
func (r *dnsPeerProofRun) consume(fn func() error) error {
	started := time.Now()
	err := fn()
	r.record(dnsPeerProofStepConsume, time.Since(started))
	if r.cancelFresh != nil {
		r.cancelFresh()
	}
	if err == nil {
		return nil
	}
	if pendingDNSPeerCode(err) == "" {
		err = pendingDNSPeerCause(transport.DNSPeerPendingJournalUnknown, err)
	}
	r.stopped(dnsPeerProofStepConsume, pendingDNSPeerCode(err), err)
	return err
}

// stopped logs, once per attempt, the step and the underlying error of a
// pending outcome (bounded, product-authored, no key material).
func (r *dnsPeerProofRun) stopped(step dnsPeerProofStep, code string, err error) {
	if r.stopLog {
		return
	}
	r.stopLog = true
	if code == "" {
		code = "unclassified"
	}
	cause := dnsPeerPendingCause(err)
	if cause == nil {
		cause = err
	}
	log.Printf("DNS peer proof for %s (%s secondary) stopped at step %s as %s: %s",
		r.zone, r.peer, step, code, boundedDNSPeerProofLogText(cause))
}

// finish logs the per-step elapsed times and the outcome of the attempt.
func (r *dnsPeerProofRun) finish(err error) {
	outcome := "verified"
	switch {
	case err == nil:
	case errors.Is(err, errDNSProducerCatalogRestampedBeforeChallenge):
		outcome = "no challenge (catalog re-stamped; the wave proves the pair again)"
	default:
		code := pendingDNSPeerCode(err)
		if code == "" {
			code = "unclassified"
		}
		outcome = "pending " + code
		if !r.stopLog {
			r.stopped("verifier", code, err)
		}
	}
	timings := strings.Join(r.timings, " ")
	if timings == "" {
		timings = "none"
	}
	log.Printf("DNS peer proof for %s (%s secondary) step times: %s; total %s; outcome %s",
		r.zone, r.peer, timings, time.Since(r.started).Round(time.Millisecond), outcome)
}

// logAnswer records the inspector's answer fields (bounded tokens) and
// whether Verify accepted it (accepted: Verify called consume-once).
func (r *dnsPeerProofRun) logAnswer(catalogState, memberState, nativeState string, accepted bool, verifyErr error) {
	verdict := "accepted"
	if !accepted {
		verdict = "not accepted: " + boundedDNSPeerProofLogText(verifyErr)
	}
	log.Printf("DNS peer inspector answer for %s (%s secondary): catalog_state=%s member_state=%s native_state=%s; %s",
		r.zone, r.peer, boundedDNSPeerAnswerField(catalogState), boundedDNSPeerAnswerField(memberState),
		boundedDNSPeerAnswerField(nativeState), verdict)
}

// boundedDNSPeerAnswerField keeps a peer-supplied state token printable and
// short: lowercase letters, digits and underscore, at most 32 bytes.
func boundedDNSPeerAnswerField(value string) string {
	if value == "" {
		return "(empty)"
	}
	var out strings.Builder
	for i, r := range value {
		if i >= 32 {
			out.WriteString("...")
			break
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte('?')
		}
	}
	return out.String()
}

// nativePeerCurrentChecks is the current-evidence recheck of one native
// proof, bound to the context of the step that runs it.
type nativePeerCurrentChecks struct {
	attempt    func() error
	enrollment func() error
	local      func(context.Context) error
	last       error
}

// at returns the recheck under ctx. A local check that failed after ctx
// ended observed nothing: it is a deadline, never an owner edit.
func (c *nativePeerCurrentChecks) at(ctx context.Context) func() error {
	return func() error {
		err := peerCurrentPendingCodeAt(c.attempt, c.enrollment, func() error {
			if c.local == nil {
				return dnsPeerProofInternal(errors.New("local evidence recheck is missing"))
			}
			err := c.local(ctx)
			if err != nil && ctx.Err() != nil {
				return dnsPeerProofDeadline(err)
			}
			return err
		})
		c.last = err
		return err
	}
}

// journalOp runs one private challenge journal operation whose rechecks run
// under ctx. A recheck that found a reviewed difference keeps its own code;
// a recheck that ran out of time stays a deadline for the step runner;
// anything else is dns_peer_journal_unknown with its cause logged.
func (c *nativePeerCurrentChecks) journalOp(ctx context.Context, what string, op func(func() error) error) error {
	c.last = nil
	err := op(c.at(ctx))
	if err == nil {
		return nil
	}
	if last := c.last; last != nil {
		if pendingDNSPeerCode(last) != "" || isDNSPeerProofDeadline(last) {
			return last
		}
		err = errors.Join(err, last)
	}
	return pendingDNSPeerCause(transport.DNSPeerPendingJournalUnknown, fmt.Errorf("%s: %w", what, err))
}
