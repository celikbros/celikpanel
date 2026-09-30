package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/transport"
)

// dnsProducerCatalogRestampLimit bounds how many PowerDNS daemon re-stamps
// one attempt (one completion wave, one ledger attempt) admits. PowerDNS 4.9
// re-stamps the producer once per membership change, and an attempt holds
// the host mutation lock, so no membership change can happen inside it: one
// admission is the expected maximum. A daemon that keeps re-stamping an
// unchanged member set is not the measured behaviour; the fourth re-stamp in
// one attempt is refused as producer_catalog instead of being followed.
const dnsProducerCatalogRestampLimit = 3

type dnsProducerCatalogVerdict uint8

const (
	// The observed producer catalog equals the recorded evidence.
	dnsProducerCatalogUnchanged dnsProducerCatalogVerdict = iota
	// The observed catalog differs only as PowerDNS's own re-stamp does.
	dnsProducerCatalogDaemonRestamp
	// Anything else: identity, members, member serials, a lower or equal
	// serial, an unchanged or missing CATALOG-HASH, or a BIND producer.
	dnsProducerCatalogDiffers
)

// classifyProducerCatalogEvidence is the one rule comparing an attempt's
// recorded producer catalog evidence with a fresh read of the producer. It is
// used by every PowerDNS- and BIND-primary publication and deletion proof,
// the resumed V3 recovery (the same completion wave) and the native peer
// proof's local rechecks.
//
// A PowerDNS daemon re-stamp is admitted, at any point of an operation's
// life, only when the observed serial is strictly higher, the CATALOG-HASH
// row is present and changed, and the producer identity (local address, peer
// address, catalog name), the member set and the member serials are
// identical. A higher serial with an unchanged hash is not the daemon's
// re-stamp (the daemon rewrites both together; CelikPanel's own publication
// never happens inside a recorded attempt) and is refused. BIND has no daemon
// re-stamp: its evidence must be identical.
func classifyProducerCatalogEvidence(
	engine transport.DNSEngine,
	recorded, observed dnsPrimaryCatalogEvidence,
) dnsProducerCatalogVerdict {
	sameIdentity := recorded.LocalIP == observed.LocalIP &&
		recorded.PeerIP == observed.PeerIP && recorded.Domain == observed.Domain
	// Element-wise: an empty and a nil member list are the same zero-zone
	// catalog (see the zero-zone section of docs/DNS-ENGINE-ARTIFACT.md).
	sameMembers := slices.Equal(recorded.Members, observed.Members) &&
		slices.Equal(recorded.MemberSerials, observed.MemberSerials)
	if sameIdentity && sameMembers && recorded.Serial == observed.Serial &&
		recorded.CatalogHash == observed.CatalogHash {
		return dnsProducerCatalogUnchanged
	}
	if engine == transport.DNSEnginePowerDNS && sameIdentity && sameMembers &&
		observed.Serial > recorded.Serial && observed.CatalogHash != "" &&
		observed.CatalogHash != recorded.CatalogHash {
		return dnsProducerCatalogDaemonRestamp
	}
	return dnsProducerCatalogDiffers
}

// partialDNSProducerCatalogRestamp reports a difference that a daemon
// re-stamp caught between its serial and hash writes could produce: same
// identity and members, and exactly one of "higher serial" and "changed
// hash". A recheck may read the producer once more before deciding.
func partialDNSProducerCatalogRestamp(
	engine transport.DNSEngine,
	recorded, observed dnsPrimaryCatalogEvidence,
) bool {
	if engine != transport.DNSEnginePowerDNS ||
		recorded.LocalIP != observed.LocalIP || recorded.PeerIP != observed.PeerIP ||
		recorded.Domain != observed.Domain ||
		!slices.Equal(recorded.Members, observed.Members) ||
		!slices.Equal(recorded.MemberSerials, observed.MemberSerials) {
		return false
	}
	higher := observed.Serial > recorded.Serial
	hashChanged := observed.CatalogHash != recorded.CatalogHash
	return (higher && !hashChanged) || (!higher && observed.Serial == recorded.Serial && hashChanged)
}

// dnsRecordedProducerCatalog holds one attempt's recorded producer catalog
// evidence. The completion wave creates it from the plan it was given (read
// from the producer database at the start of the attempt: publication or
// resumed V3 recovery) and shares it with the native peer proof, so every
// comparison of the attempt judges against the same, re-stamped record.
//
// There is no separate durable copy: each attempt re-derives the record from
// the durable producer rows, which hold the daemon's own re-stamp. A pending
// V3 job blocks every other DNS mutation, so between attempts only the
// daemon or the server owner can change the producer.
type dnsRecordedProducerCatalog struct {
	mu       sync.Mutex
	engine   transport.DNSEngine
	zone     string
	evidence dnsPrimaryCatalogEvidence
	refresh  func(context.Context) (dnsPrimaryCatalogEvidence, error)
	// serials lists every serial this record has held, oldest first; the
	// record moves only through admitted re-stamps.
	serials  []uint32
	restamps int
}

func newDNSRecordedProducerCatalog(plan dnsV3PrimaryPropagationPlan) *dnsRecordedProducerCatalog {
	return &dnsRecordedProducerCatalog{
		engine:   plan.SourceState.Engine,
		zone:     plan.Changed.Domain,
		evidence: cloneDNSPrimaryCatalogEvidence(plan.Evidence),
		refresh:  plan.RefreshEvidence,
		serials:  []uint32{plan.Evidence.Serial},
	}
}

// recordedProducerCatalogFor returns the plan's shared record, or a fixed
// record of the plan's evidence when a caller runs a proof outside a wave.
func recordedProducerCatalogFor(plan dnsV3PrimaryPropagationPlan) *dnsRecordedProducerCatalog {
	if plan.catalog != nil {
		return plan.catalog
	}
	return newDNSRecordedProducerCatalog(plan)
}

func cloneDNSPrimaryCatalogEvidence(evidence dnsPrimaryCatalogEvidence) dnsPrimaryCatalogEvidence {
	evidence.Members = slices.Clone(evidence.Members)
	evidence.MemberSerials = slices.Clone(evidence.MemberSerials)
	return evidence
}

// Evidence returns a copy of the current recorded evidence.
func (r *dnsRecordedProducerCatalog) Evidence() dnsPrimaryCatalogEvidence {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneDNSPrimaryCatalogEvidence(r.evidence)
}

// RestampedSince reports whether the record held serial and has since moved
// past it through admitted re-stamps only.
func (r *dnsRecordedProducerCatalog) RestampedSince(serial uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.evidence.Serial > serial && slices.Contains(r.serials, serial)
}

// Classify judges observed against the record without changing it. A nil
// error means unchanged or an admissible re-stamp; otherwise the error is a
// producer_catalog owner-edit observation naming recorded and observed
// values.
func (r *dnsRecordedProducerCatalog) Classify(observed dnsPrimaryCatalogEvidence) (dnsProducerCatalogVerdict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.classifyLocked(observed)
}

func (r *dnsRecordedProducerCatalog) classifyLocked(observed dnsPrimaryCatalogEvidence) (dnsProducerCatalogVerdict, error) {
	verdict := classifyProducerCatalogEvidence(r.engine, r.evidence, observed)
	switch verdict {
	case dnsProducerCatalogUnchanged:
		return verdict, nil
	case dnsProducerCatalogDaemonRestamp:
		if r.restamps >= dnsProducerCatalogRestampLimit {
			return dnsProducerCatalogDiffers, dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckProducerCatalog,
				"the daemon re-stamped the catalog more than %d times in one attempt: %s",
				dnsProducerCatalogRestampLimit, describeDNSCatalogEvidenceDifference(r.evidence, observed))
		}
		return verdict, nil
	default:
		err := dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckProducerCatalog,
			"producer catalog differs from the recorded evidence other than by a daemon re-stamp: %s",
			describeDNSCatalogEvidenceDifference(r.evidence, observed))
		if partialDNSProducerCatalogRestamp(r.engine, r.evidence, observed) {
			err.(*dnsPeerOwnerEditError).retryable = true
		}
		return verdict, err
	}
}

// Admit classifies observed and, for an admitted daemon re-stamp, re-stamps
// the record to it before returning. The proof then continues against the
// re-stamped record.
func (r *dnsRecordedProducerCatalog) Admit(observed dnsPrimaryCatalogEvidence) (dnsProducerCatalogVerdict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	verdict, err := r.classifyLocked(observed)
	if err != nil || verdict != dnsProducerCatalogDaemonRestamp {
		return verdict, err
	}
	previous := r.evidence
	r.evidence = cloneDNSPrimaryCatalogEvidence(observed)
	r.serials = append(r.serials, observed.Serial)
	r.restamps++
	log.Printf("DNS peer proof admitted the PowerDNS daemon's re-stamp of catalog %s (operation zone %s): serial %d -> %d, CATALOG-HASH %s -> %s, identity, members and member serials unchanged (%d member(s)); re-stamp %d of at most %d in this attempt; the proof continues against the re-stamped evidence",
		observed.Domain, r.zone, previous.Serial, observed.Serial,
		shortDNSCatalogHash(previous.CatalogHash), shortDNSCatalogHash(observed.CatalogHash),
		len(observed.Members), r.restamps, dnsProducerCatalogRestampLimit)
	return verdict, nil
}

// RefreshAndAdmit rereads the producer (only a native V3 PowerDNS plan has a
// reader) and admits a daemon re-stamp. It reports whether the record moved.
// A difference is not an error here: the wave keeps waiting and names its
// own check if it never converges.
func (r *dnsRecordedProducerCatalog) RefreshAndAdmit(ctx context.Context) bool {
	if r.refresh == nil {
		return false
	}
	observed, err := r.refresh(ctx)
	if err != nil {
		return false
	}
	verdict, err := r.Admit(observed)
	return err == nil && verdict == dnsProducerCatalogDaemonRestamp
}

func shortDNSCatalogHash(hash string) string {
	if hash == "" {
		return "(none)"
	}
	if len(hash) > 12 {
		return hash[:12] + "..."
	}
	return hash
}

// describeDNSCatalogEvidenceDifference is a bounded, product-authored
// description of recorded versus observed producer evidence for the Agent
// log. It carries addresses, the catalog and member names, serials and a
// shortened catalog digest; no key material exists in this evidence.
func describeDNSCatalogEvidenceDifference(recorded, observed dnsPrimaryCatalogEvidence) string {
	var parts []string
	if recorded.LocalIP != observed.LocalIP || recorded.PeerIP != observed.PeerIP ||
		recorded.Domain != observed.Domain {
		parts = append(parts, fmt.Sprintf("identity recorded local=%s peer=%s catalog=%s observed local=%s peer=%s catalog=%s",
			recorded.LocalIP, recorded.PeerIP, recorded.Domain,
			observed.LocalIP, observed.PeerIP, observed.Domain))
	}
	parts = append(parts, fmt.Sprintf("serial recorded=%d observed=%d", recorded.Serial, observed.Serial))
	parts = append(parts, fmt.Sprintf("catalog-hash recorded=%s observed=%s",
		shortDNSCatalogHash(recorded.CatalogHash), shortDNSCatalogHash(observed.CatalogHash)))
	parts = append(parts, fmt.Sprintf("members recorded=%d observed=%d", len(recorded.Members), len(observed.Members)))
	count := max(len(recorded.Members), len(observed.Members))
	for index := 0; index < count; index++ {
		recordedMember, observedMember := "(none)", "(none)"
		var recordedSerial, observedSerial uint32
		if index < len(recorded.Members) {
			recordedMember = recorded.Members[index]
			if index < len(recorded.MemberSerials) {
				recordedSerial = recorded.MemberSerials[index]
			}
		}
		if index < len(observed.Members) {
			observedMember = observed.Members[index]
			if index < len(observed.MemberSerials) {
				observedSerial = observed.MemberSerials[index]
			}
		}
		if recordedMember != observedMember || recordedSerial != observedSerial {
			parts = append(parts, fmt.Sprintf("first differing member #%d recorded=%s/%d observed=%s/%d",
				index, recordedMember, recordedSerial, observedMember, observedSerial))
			break
		}
	}
	return strings.Join(parts, "; ")
}

// errDNSProducerCatalogRestampedBeforeChallenge: the native proof admitted a
// daemon re-stamp after the wave proved the catalog pair at the previous
// serial and before any challenge was minted or inspection opened. The wave
// re-proves the pair against the re-stamped record and may call the native
// proof again; it is never a pending reason.
var errDNSProducerCatalogRestampedBeforeChallenge = errors.New(
	"the producer catalog was re-stamped by the PowerDNS daemon before the peer challenge; the pair is re-proved first",
)

// nativePeerChallengePlan returns the plan a challenge may be minted from:
// its evidence is the recorded evidence, which must still carry the serial
// the wave proved at the peer (authority). When an admitted re-stamp moved it,
// the wave must re-prove the pair first.
func nativePeerChallengePlan(
	plan dnsV3PrimaryPropagationPlan,
	record *dnsRecordedProducerCatalog,
	authority dnsPeerAXFRAuthority,
) (dnsV3PrimaryPropagationPlan, error) {
	evidence := record.Evidence()
	if evidence.Serial != authority.catalogSerial {
		if record.RestampedSince(authority.catalogSerial) {
			return plan, errDNSProducerCatalogRestampedBeforeChallenge
		}
		return plan, pendingDNSPeerProofInternal("challenge binding for "+plan.Changed.Domain,
			fmt.Errorf("recorded catalog serial %d is not the proved pair serial %d",
				evidence.Serial, authority.catalogSerial))
	}
	plan.Evidence = evidence
	return plan, nil
}
