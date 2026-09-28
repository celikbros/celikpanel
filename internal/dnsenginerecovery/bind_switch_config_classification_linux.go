//go:build linux

package dnsenginerecovery

import (
	"errors"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// BINDConfigFileState describes one exact v2 file at a read-only checkpoint.
// An unknown result includes owner edits, mixed bytes within a single file,
// missing data, and metadata drift; callers must not turn it into restoration.
type BINDConfigFileState uint8

const (
	BINDConfigFileUnknown BINDConfigFileState = iota
	BINDConfigFileBefore
	BINDConfigFileAfter
)

// ClassifyBINDSwitchConfigFilesV2 compares complete native file observations
// with the frozen before/after evidence. Different files may be at different
// exact phases; an individual file may not be partially written or rewritten
// by an owner. The caller must still prove descriptor/path identity, generation
// pointer, source receipt and native units under the installed locks.
func ClassifyBINDSwitchConfigFilesV2(
	policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1,
	current []dnsengineartifact.FileSnapshot,
) ([]BINDConfigFileState, error) {
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 || journal.InversePlan == nil {
		return nil, errors.New("BIND config checkpoint requires a v2 inverse plan")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, err
	}
	if len(current) != len(journal.ConfigBefore) {
		return nil, errors.New("BIND config checkpoint does not contain the complete path set")
	}
	result := make([]BINDConfigFileState, len(current))
	for i, observed := range current {
		before, after := journal.ConfigBefore[i], journal.InversePlan.ConfigAfter[i]
		if observed.Path != before.Path || !observed.Exists ||
			!observed.OwnerKnown ||
			dnsengineartifact.ValidateFileSnapshotIntegrity(observed) != nil {
			result[i] = BINDConfigFileUnknown
			continue
		}
		// A no-op managed write is the original preimage for inverse purposes.
		switch {
		case reflect.DeepEqual(observed, before):
			result[i] = BINDConfigFileBefore
		case reflect.DeepEqual(observed, after):
			result[i] = BINDConfigFileAfter
		default:
			result[i] = BINDConfigFileUnknown
		}
	}
	return result, nil
}
