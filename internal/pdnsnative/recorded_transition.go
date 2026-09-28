package pdnsnative

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// RecordedTransition binds the exact poststart logical observation. The
// independent reader must also verify file identities and sidecars.
type RecordedTransition struct {
	Observed      CatalogTransition `json:"observed"`
	LogicalSHA256 string            `json:"logical_sha256"`
}

func LogicalSHA256(snapshot Snapshot) (string, error) {
	if len(snapshot.Schema) == 0 || snapshot.Tables == nil {
		return "", errors.New("PowerDNS logical snapshot is empty")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ObserveFreshPrimaryCatalogTransition(staged, live Snapshot, catalog string, sourceSerial uint32) (RecordedTransition, error) {
	observed, err := VerifyFreshPrimaryCatalogTransition(staged, live, catalog, sourceSerial)
	if err != nil {
		return RecordedTransition{}, err
	}
	digest, err := LogicalSHA256(live)
	if err != nil {
		return RecordedTransition{}, err
	}
	return RecordedTransition{Observed: observed, LogicalSHA256: digest}, nil
}

// A crash before this exact poststart observation is durably committed has no
// recognized inverse; recovery must preserve the live database.
func VerifyRecordedFreshPrimaryCatalogTransition(staged, live Snapshot, catalog string, sourceSerial uint32, recorded RecordedTransition) error {
	if recorded.LogicalSHA256 == "" {
		return errors.New("PowerDNS native observation was not durably recorded")
	}
	actual, err := ObserveFreshPrimaryCatalogTransition(staged, live, catalog, sourceSerial)
	if err != nil {
		return err
	}
	if actual != recorded {
		return errors.New("PowerDNS native catalog differs from recorded poststart observation")
	}
	return nil
}
