//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// Package adoption grants no ownership after rollback. Only the canonical
// receipt written by this exact Debian adoption can be removed; packages and
// owner service files are never touched.
type bindAdoptionInstallReceipt struct {
	Schema            string   `json:"schema"`
	Engine            string   `json:"engine"`
	PackageManager    string   `json:"package_manager"`
	Packages          []string `json:"packages"`
	MissingBefore     []string `json:"missing_before"`
	ManifestQualifier string   `json:"manifest_qualifier"`
	MutationRequestID string   `json:"mutation_request_id"`
	MutationOwnerID   string   `json:"mutation_owner_id"`
	AdoptedPresent    bool     `json:"adopted_present,omitempty"`
}

func bindAdoptionExpectedInstallReceipt(j dnsengineartifact.SwitchJournalV1) []byte {
	b, _ := json.Marshal(bindAdoptionInstallReceipt{
		Schema: "celikpanel-dns-engine-install-ownership/v1", Engine: "bind",
		PackageManager: "apt", Packages: []string{"bind9"}, MissingBefore: []string{},
		ManifestQualifier: j.ManifestQualifier, MutationRequestID: j.MutationRequestID,
		MutationOwnerID: j.MutationOwnerID, AdoptedPresent: true,
	})
	return append(b, '\n')
}

// BINDAdoptionInstallReceiptAbsent also rejects unrelated ownership evidence.
// Present is admissible only during rollback, never after its durable verdict.
func BINDAdoptionInstallReceiptAbsent(policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) (bool, error) {
	kind, err := PlanNativeInverse(j)
	if err != nil || kind != NativeInverseBINDRunningAdoption ||
		!policy.RequireOwner || filepath.Base(policy.StatePath) != "dns-engine-state.json" ||
		j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil ||
		j.InversePlan.HostLayout != "apt" || j.InversePlan.SourceBIND == nil ||
		j.StateBefore.Exists || (j.Phase != dnsengineartifact.SwitchPhaseRollingBack && j.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return false, errors.New("BIND adoption package receipt lacks exact rollback scope")
	}
	if err := policy.ValidateSwitchJournal(j); err != nil {
		return false, err
	}
	owner := servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}
	for _, name := range []string{"dns-engine-ownership-bind.json", "dns-engine-ownership-pdns.json", "dns-engine-install-ownership-pdns.json"} {
		_, present, err := servicemutationledger.ReadFile(filepath.Join(filepath.Dir(policy.StatePath), name), 64<<10, owner)
		if err != nil {
			return false, err
		}
		if present {
			return false, errors.New("owner BIND rollback has unrelated ownership evidence")
		}
	}
	raw, present, err := servicemutationledger.ReadFile(filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-install-ownership-bind.json"), 64<<10, owner)
	if err != nil {
		return false, err
	}
	if !present {
		return true, nil
	}
	if j.Phase != dnsengineartifact.SwitchPhaseRollingBack || !bytes.Equal(raw, bindAdoptionExpectedInstallReceipt(j)) {
		return false, errors.New("BIND adoption package receipt differs from the exact accepted operation")
	}
	return false, nil
}

// RemoveExactBINDAdoptionInstallReceipt requires the caller's locked native
// owner-source proof. Absence is idempotent, not evidence of source DNS health.
func RemoveExactBINDAdoptionInstallReceipt(policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner, j dnsengineartifact.SwitchJournalV1) error {
	if owner.UID != policy.StateUID || owner.GID != policy.StateGID {
		return errors.New("BIND adoption receipt owner mismatch")
	}
	absent, err := BINDAdoptionInstallReceiptAbsent(policy, j)
	if err != nil || absent {
		return err
	}
	return servicemutationledger.RemoveFileExact(filepath.Join(filepath.Dir(policy.StatePath), "dns-engine-install-ownership-bind.json"), bindAdoptionExpectedInstallReceipt(j), 64<<10, owner)
}
