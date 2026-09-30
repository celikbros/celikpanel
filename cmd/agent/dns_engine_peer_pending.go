package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/transport"
)

// dnsPeerPendingError carries only a reviewed code. Probe, SSH and peer output
// must never become a durable owner-facing message.
type dnsPeerPendingError struct{ code string }

func (e *dnsPeerPendingError) Error() string { return e.code }

func pendingBINDPeer(code string) error {
	if !transport.ValidDNSPeerPendingCode(code) {
		code = transport.DNSPeerPendingNativeUnknown
	}
	return &dnsPeerPendingError{code: code}
}

func pendingDNSPeerCode(err error) string {
	var peer *dnsPeerPendingError
	if errors.As(err, &peer) && transport.ValidDNSPeerPendingCode(peer.code) {
		return peer.code
	}
	return ""
}

// peerCurrentPendingCodeAt keeps proved enrollment drift separate from an
// changed operation or local DNS evidence. The callbacks run in that order so
// no later observation can replace the first failed boundary. Their raw errors
// must never become a durable or owner-facing peer reason.
//
// A missing boundary, or a local check that could not run because the proof's
// own plan is unusable (dnsPeerProofInternalError), is an internal
// precondition failure of the Agent: it is reported as
// dns_peer_proof_internal, never as an owner change (D-024). An observed
// difference keeps dns_peer_owner_edit_unknown with the reviewed check token
// that found it (a changed ledger attempt is operation_attempt).
func peerCurrentPendingCodeAt(attempt, enrollment, local func() error) error {
	if attempt == nil || enrollment == nil || local == nil {
		return pendingDNSPeerProofInternal("current-evidence recheck",
			errors.New("a recheck boundary is missing"))
	}
	if err := attempt(); err != nil {
		return pendingDNSPeerOwnerEdit(dnsPeerOwnerEditAt(
			transport.DNSPeerOwnerEditCheckOperationAttempt, err))
	}
	if enrollment() != nil {
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	if err := local(); err != nil {
		var internal *dnsPeerProofInternalError
		if errors.As(err, &internal) {
			return pendingDNSPeerProofInternal("local evidence recheck", internal.err)
		}
		// An already reviewed pending code (for example a nested recheck)
		// passes through unchanged.
		if code := pendingDNSPeerCode(err); code != "" {
			return err
		}
		return pendingDNSPeerOwnerEdit(err)
	}
	return nil
}

// dnsPeerOwnerEditError is an observation that differed from the recorded
// evidence during a native peer proof. check is one reviewed token
// (transport.ValidDNSPeerOwnerEditCheck); err is product-authored text with
// the recorded and observed values for the Agent log only. retryable marks a
// difference a concurrent write by the local DNS daemon can produce for an
// instant (a database file time, a half-written re-stamp); a recheck may
// read once more before deciding.
type dnsPeerOwnerEditError struct {
	check     string
	err       error
	retryable bool
}

func (e *dnsPeerOwnerEditError) Error() string {
	return "native DNS peer proof check " + e.check + " differed: " + e.err.Error()
}

func (e *dnsPeerOwnerEditError) Unwrap() error { return e.err }

func dnsPeerOwnerEditAt(check string, err error) error {
	if err == nil {
		err = errors.New("no further detail")
	}
	var existing *dnsPeerOwnerEditError
	if errors.As(err, &existing) {
		return existing
	}
	return &dnsPeerOwnerEditError{check: check, err: err}
}

func dnsPeerOwnerEditf(check, format string, args ...any) error {
	return &dnsPeerOwnerEditError{check: check, err: fmt.Errorf(format, args...)}
}

func retryableDNSPeerOwnerEdit(err error) bool {
	var edit *dnsPeerOwnerEditError
	return errors.As(err, &edit) && edit.retryable
}

// pendingDNSPeerOwnerEdit logs which check differed with its recorded and
// observed values (bounded, product-authored, no key material) and returns
// dns_peer_owner_edit_unknown carrying that check as its reviewed detail. An
// untyped observation keeps the plain code and is logged as unclassified.
func pendingDNSPeerOwnerEdit(err error) error {
	check := ""
	var edit *dnsPeerOwnerEditError
	if errors.As(err, &edit) && transport.ValidDNSPeerOwnerEditCheck(edit.check) {
		check = edit.check
	}
	logged := check
	if logged == "" {
		logged = "unclassified"
	}
	log.Printf("DNS peer proof observed different evidence (check=%s); the deletion stays pending as %s: %s",
		logged, transport.DNSPeerPendingOwnerEditUnknown, boundedDNSPeerProofLogText(err))
	return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknownWithDetail(check))
}

// dnsPeerProofInternalError marks a failure of the proof's own preconditions
// (its plan, source receipt or probe selection), as opposed to evidence that
// was observed and differed. It is never shown to the owner as text.
type dnsPeerProofInternalError struct{ err error }

func (e *dnsPeerProofInternalError) Error() string {
	return "native DNS peer proof precondition failed: " + e.err.Error()
}

func (e *dnsPeerProofInternalError) Unwrap() error { return e.err }

func dnsPeerProofInternal(err error) error {
	if err == nil {
		err = errors.New("unspecified precondition")
	}
	return &dnsPeerProofInternalError{err: err}
}

// dnsPeerProofLogLimit bounds the underlying error text written to the Agent
// log. The text is product-authored (no peer output, keys or credentials).
const dnsPeerProofLogLimit = 512

func boundedDNSPeerProofLogText(err error) string {
	if err == nil {
		return "no error text"
	}
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, err.Error())
	if len(text) > dnsPeerProofLogLimit {
		cut := dnsPeerProofLogLimit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "..."
	}
	return text
}

// pendingDNSPeerProofInternal records why the Agent could not run its own
// proof of the secondary (the owner reads it with
// journalctl -u celikpanel-agent | grep peer) and returns the reviewed code.
// Nothing about the owner's configuration is claimed.
func pendingDNSPeerProofInternal(stage string, err error) error {
	log.Printf("DNS peer proof could not run its own check (%s); no owner change was observed: %s",
		stage, boundedDNSPeerProofLogText(err))
	return pendingBINDPeer(transport.DNSPeerPendingProofInternal)
}

// nativePeerProofCatalogProbes selects the catalog AXFR probes of the native
// peer proof by the plan's frozen source engine. Both native verifiers call it
// before any challenge is minted and reuse the result after the inspection,
// so the post-inspection re-verification cannot fail on selection. A plan
// without a known source engine is an internal precondition failure.
func nativePeerProofCatalogProbes(plan dnsV3PrimaryPropagationPlan) (dnsCatalogAXFRProbe, dnsBoundCatalogAXFRProbe, error) {
	local, peer, err := catalogAXFRProbesForSourceEngine(plan.SourceState.Engine)
	if err != nil {
		return nil, nil, pendingDNSPeerProofInternal("catalog probe selection for "+plan.Changed.Domain,
			fmt.Errorf("plan source engine receipt %q: %w", plan.SourceState.Engine, err))
	}
	return local, peer, nil
}

// inspectionPendingCode classifies an incomplete authenticated inspection by
// the reviewed reason token its forced command reported, if any. A refused
// local catalog transfer has its own typed reason (the secondary's owner can
// act on it); any other reviewed reason rides as the detail of
// dns_peer_inspection_unknown; no reason keeps the plain code.
func inspectionPendingCode(reason string) string {
	if reason == transport.DNSPeerInspectorReasonCatalogTransferRefused {
		return transport.DNSPeerPendingCatalogTransferRefused
	}
	return transport.DNSPeerPendingInspectionUnknownWithDetail(reason)
}

func dnsZoneV3PendingLedgerCode(code string) string {
	if transport.ValidDNSPeerPendingCode(code) {
		return code
	}
	return "dns_zone_v3_propagation_pending"
}
