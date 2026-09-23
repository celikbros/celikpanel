//go:build linux

package dnsenginerecovery

import (
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
	return observation, true, nil
}
