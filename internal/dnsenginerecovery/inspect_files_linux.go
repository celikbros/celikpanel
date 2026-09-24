//go:build linux

package dnsenginerecovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// InspectFiles reads installed-format switch evidence from an established
// private state root. It has no mutation authority and does not acquire locks;
// callers that need a cross-file quiesced view hold the release and host locks.
func InspectFiles(stateRoot string, owner servicemutationledger.FileOwner, policy dnsengineartifact.JournalPolicy, now time.Time) (EvidenceObservation, bool, error) {
	if !filepath.IsAbs(stateRoot) || filepath.Clean(stateRoot) != stateRoot ||
		!policy.RequireOwner || policy.StateUID != owner.UID || policy.StateGID != owner.GID ||
		filepath.Dir(policy.StatePath) != stateRoot {
		return EvidenceObservation{}, false, errors.New("DNS switch evidence root and established host policy disagree")
	}
	if err := policy.Validate(); err != nil {
		return EvidenceObservation{}, false, err
	}
	journalRaw, exists, err := servicemutationledger.ReadFile(filepath.Join(stateRoot, "dns-engine-switch-journal.json"), dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil {
		return EvidenceObservation{}, false, fmt.Errorf("read DNS switch journal: %w", err)
	}
	if !exists {
		return EvidenceObservation{}, false, nil
	}
	journal, err := policy.DecodeSwitchJournal(journalRaw)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("decode DNS switch journal: %w", err)
	}
	ledgerRaw, ledgerExists, err := servicemutationledger.ReadFile(filepath.Join(stateRoot, "service-mutations.json"), servicemutationledger.MaxSize, owner)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("read DNS switch ledger: %w", err)
	}
	if !ledgerExists {
		return EvidenceObservation{}, true, errors.New("DNS switch ledger is absent while a journal remains")
	}
	ledger, err := servicemutationledger.Decode(ledgerRaw)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("decode DNS switch ledger: %w", err)
	}
	observation, err := InspectEvidence(policy, journal, ledger, now)
	if err != nil {
		return EvidenceObservation{}, true, err
	}
	stateRaw, stateExists, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("read current DNS state receipt: %w", err)
	}
	var state dnsengineartifact.StateV1
	if !stateExists {
		observation.TargetReceipt = TargetReceiptAbsent
	} else {
		state, _, err = dnsengineartifact.DecodeStateDocument(stateRaw)
		if err != nil {
			return EvidenceObservation{}, true, fmt.Errorf("decode current DNS state receipt: %w", err)
		}
		observation.TargetReceipt = TargetReceiptDifferent
		if dnsengineartifact.ExactSwitchTargetStateV1(state, journal) {
			observation.TargetReceipt = TargetReceiptExact
		}
	}
	sourceMatches, err := dnsengineartifact.ProveFrozenSwitchSourceState(journal, state, stateExists)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("compare frozen DNS source receipt: %w", err)
	}
	observation.SourceReceipt = SourceReceiptDifferent
	if sourceMatches {
		if stateExists {
			observation.SourceReceipt = SourceReceiptExact
		} else {
			observation.SourceReceipt = SourceReceiptMutualAbsence
		}
	}
	if journal.SourceEngine == "" {
		observation.EvidenceSHA256 = switchEvidenceFingerprint(journalRaw, ledgerRaw, stateRaw, stateExists, nil, false)
		observation.SourceOwnership = SourceOwnershipNotApplicable
		return observation, true, nil
	}
	ownershipPath := filepath.Join(stateRoot, "dns-engine-ownership-"+string(journal.SourceEngine)+".json")
	ownershipRaw, ownershipExists, err := servicemutationledger.ReadFile(ownershipPath, 64<<10, owner)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("read DNS source ownership receipt: %w", err)
	}
	if !ownershipExists {
		observation.EvidenceSHA256 = switchEvidenceFingerprint(journalRaw, ledgerRaw, stateRaw, stateExists, nil, false)
		observation.SourceOwnership = SourceOwnershipAbsent
		return observation, true, nil
	}
	ownership, _, err := dnsengineartifact.DecodeOwnershipDocument(ownershipRaw)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("decode DNS source ownership receipt: %w", err)
	}
	if ownership.Engine != journal.SourceEngine {
		return EvidenceObservation{}, true, errors.New("DNS source ownership receipt engine differs from its path")
	}
	matches, err := dnsengineartifact.ProveFrozenSwitchSourceOwnership(journal, ownership, true)
	if err != nil {
		return EvidenceObservation{}, true, fmt.Errorf("compare frozen DNS source ownership receipt: %w", err)
	}
	observation.EvidenceSHA256 = switchEvidenceFingerprint(journalRaw, ledgerRaw, stateRaw, stateExists, ownershipRaw, true)
	observation.SourceOwnership = SourceOwnershipDifferent
	if matches {
		observation.SourceOwnership = SourceOwnershipExact
	}
	return observation, true, nil
}

// switchEvidenceFingerprint binds the exact installed evidence bytes observed
// across separate secured reads. Presence and lengths keep absent and empty
// documents distinct. This detects changes during observation; it is not a
// signed receipt, native-state proof or recovery authority.
func switchEvidenceFingerprint(journal, ledger, state []byte, stateExists bool, ownership []byte, ownershipExists bool) string {
	sum := sha256.New()
	for _, part := range []struct {
		name   string
		raw    []byte
		exists bool
	}{
		{"journal", journal, true},
		{"ledger", ledger, true},
		{"state", state, stateExists},
		{"ownership", ownership, ownershipExists},
	} {
		fmt.Fprintf(sum, "%s:%t:%d:", part.name, part.exists, len(part.raw))
		_, _ = sum.Write(part.raw)
	}
	return hex.EncodeToString(sum.Sum(nil))
}
