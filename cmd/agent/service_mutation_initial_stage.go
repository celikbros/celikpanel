package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	serviceMutationLedgerFileName      = "service-mutations.json"
	initialServiceMutationStagePrefix  = ".service-mutations-initial-"
	initialServiceMutationStageSuffix  = ".json"
	initialServiceMutationStagePattern = initialServiceMutationStagePrefix + "*" + initialServiceMutationStageSuffix
)

func canonicalInitialServiceMutationLedger() ([]byte, error) {
	return json.Marshal(&serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{},
	})
}

func isInitialServiceMutationStageName(name string) bool {
	if !strings.HasPrefix(name, initialServiceMutationStagePrefix) ||
		!strings.HasSuffix(name, initialServiceMutationStageSuffix) {
		return false
	}
	random := strings.TrimSuffix(
		strings.TrimPrefix(name, initialServiceMutationStagePrefix),
		initialServiceMutationStageSuffix,
	)
	if random == "" {
		return false
	}
	for _, char := range random {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// inspectInitialServiceMutationStateEntries proves that the private state
// directory contains exactly the expected final ledger, or a pre-ledger state
// with at most one recoverable initializer stage. A recoverable stage is a
// bounded byte-prefix of the canonical initial ledger with strict metadata;
// this covers crashes after create, chown, or a partial write without accepting
// arbitrary content.
// inspectInitialServiceMutationStateEntries, özel durum dizininin tam beklenen
// nihai ledger'ı veya en fazla bir kurtarılabilir initializer stage'i içerdiğini
// kanıtlar. Kurtarılabilir stage, katı metadata ile kanonik ilk ledger'ın sınırlı
// bir bayt önekidir; böylece keyfi içerik kabul edilmeden create, chown veya
// kısmi yazma sonrası çökmeler kapsanır.
func inspectInitialServiceMutationStateEntries(stateDir string, finalExists bool, expected []byte) (string, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return "", fmt.Errorf("inspect initial service mutation state entries: %w", err)
	}
	if finalExists {
		if len(entries) != 1 || entries[0].Name() != serviceMutationLedgerFileName {
			return "", errors.New("initialized service mutation state contains unexpected entries")
		}
		return "", nil
	}
	if len(entries) == 0 {
		return "", nil
	}
	if len(entries) != 1 || !isInitialServiceMutationStageName(entries[0].Name()) {
		return "", errors.New("pre-ledger service mutation state contains unexpected entries")
	}

	stagePath := filepath.Join(stateDir, entries[0].Name())
	raw, exists, err := readRecoverableInitialServiceMutationStage(stagePath, int64(len(expected)))
	if err != nil {
		return "", fmt.Errorf("validate abandoned initial service mutation stage: %w", err)
	}
	if !exists || !bytes.HasPrefix(expected, raw) {
		return "", errors.New("abandoned initial service mutation stage is not a canonical bounded prefix")
	}
	return stagePath, nil
}

// cleanupAbandonedInitialServiceMutationStage removes only the single strict
// initializer artifact proven by inspectInitialServiceMutationStateEntries.
// The caller must hold the common service-mutation flock.
// cleanupAbandonedInitialServiceMutationStage, yalnızca
// inspectInitialServiceMutationStateEntries tarafından kanıtlanan tek ve katı
// initializer stage'ini kaldırır. Çağıran ortak servis mutasyonu flock kilidini
// tutmalıdır.
func cleanupAbandonedInitialServiceMutationStage(stateDir string, expected []byte) error {
	stagePath, err := inspectInitialServiceMutationStateEntries(stateDir, false, expected)
	if err != nil {
		return err
	}
	if stagePath == "" {
		return nil
	}
	if err := os.Remove(stagePath); err != nil {
		return fmt.Errorf("remove abandoned initial service mutation stage: %w", err)
	}
	if err := syncServiceMutationDirectory(filepath.Join(stateDir, serviceMutationLedgerFileName)); err != nil {
		return fmt.Errorf("sync abandoned initial service mutation stage removal: %w", err)
	}
	return nil
}
