package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const dnsEngineInstallOwnershipSchema = "celikpanel-dns-engine-install-ownership/v1"

type dnsEngineInstallOwnershipReceipt struct {
	Schema            string              `json:"schema"`
	Engine            transport.DNSEngine `json:"engine"`
	PackageManager    string              `json:"package_manager"`
	Packages          []string            `json:"packages"`
	MissingBefore     []string            `json:"missing_before"`
	ManifestQualifier string              `json:"manifest_qualifier"`
	MutationRequestID string              `json:"mutation_request_id"`
	MutationOwnerID   string              `json:"mutation_owner_id"`
}

func dnsEngineOwnershipPath(engine transport.DNSEngine) (string, error) {
	if !transport.ValidDNSEngine(engine) {
		return "", errors.New("DNS engine ownership receipt names an unsupported engine")
	}
	return filepath.Join(
		serviceMutationStateDirectory(),
		"dns-engine-ownership-"+string(engine)+".json",
	), nil
}

func readDNSEngineOwnership(
	engine transport.DNSEngine,
) (dnsEngineStateReceipt, bool, error) {
	path, err := dnsEngineOwnershipPath(engine)
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	data, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineStateReceipt{}, false, nil
	}
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	state, err := decodeDNSEngineState(data)
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	if state.Engine != engine {
		return dnsEngineStateReceipt{}, false,
			errors.New("DNS engine ownership receipt engine differs from its path")
	}
	return state, true, nil
}

func writeDNSEngineOwnership(state dnsEngineStateReceipt) error {
	path, err := dnsEngineOwnershipPath(state.Engine)
	if err != nil {
		return err
	}
	encoded, err := encodeDNSEngineState(state)
	if err != nil {
		return err
	}
	if err := secureWriteConfig(path, encoded, 0o600); err != nil {
		actual, exists, readErr := readDNSEngineOwnership(state.Engine)
		if readErr == nil && exists && actual == state {
			return nil
		}
		return errors.Join(err, readErr)
	}
	actual, exists, err := readDNSEngineOwnership(state.Engine)
	if err != nil || !exists || actual != state {
		if err == nil {
			err = errors.New("DNS engine ownership receipt readback mismatch")
		}
		return err
	}
	return nil
}

func publishDNSEngineSourceOwnership(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
) error {
	if manifest.SourceEngine == "" {
		if stateExists {
			return errors.New("uninitialized DNS source unexpectedly has an active state receipt")
		}
		return nil
	}
	if !stateExists || state.Engine != manifest.SourceEngine ||
		state.EngineEpoch != manifest.SourceEpoch {
		return errors.New("DNS source ownership differs from the active engine state")
	}
	return writeDNSEngineOwnership(state)
}

func sourceStateFromDNSSwitchJournal(
	journal dnsEngineSwitchJournal,
) (dnsEngineStateReceipt, bool, error) {
	if journal.SourceEngine == "" {
		if journal.StateBefore.Exists {
			return dnsEngineStateReceipt{}, false,
				errors.New("uninitialized DNS source journal unexpectedly snapshots active state")
		}
		return dnsEngineStateReceipt{}, false, nil
	}
	if !journal.StateBefore.Exists {
		return dnsEngineStateReceipt{}, false,
			errors.New("DNS switch journal is missing source engine state")
	}
	state, err := decodeDNSEngineState(journal.StateBefore.Data)
	if err != nil {
		return dnsEngineStateReceipt{}, false,
			fmt.Errorf("decode DNS switch source state: %w", err)
	}
	if state.Engine != journal.SourceEngine ||
		state.EngineEpoch != journal.SourceEpoch {
		return dnsEngineStateReceipt{}, false,
			errors.New("DNS switch journal source state identity differs from its manifest")
	}
	return state, true, nil
}

func verifyDNSSwitchSourceOwnership(journal dnsEngineSwitchJournal) error {
	expected, exists, err := sourceStateFromDNSSwitchJournal(journal)
	if err != nil || !exists {
		return err
	}
	actual, actualExists, err := readDNSEngineOwnership(journal.SourceEngine)
	if err != nil {
		return err
	}
	if !actualExists || !reflect.DeepEqual(actual, expected) {
		return errors.New("DNS source ownership receipt is absent or differs from the switch journal")
	}
	return nil
}

func dnsEngineInstallOwnershipPath(engine transport.DNSEngine) (string, error) {
	if !transport.ValidDNSEngine(engine) {
		return "", errors.New("DNS engine install ownership names an unsupported engine")
	}
	return filepath.Join(
		serviceMutationStateDirectory(),
		"dns-engine-install-ownership-"+string(engine)+".json",
	), nil
}

func validateDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
) error {
	if receipt.Schema != dnsEngineInstallOwnershipSchema ||
		!transport.ValidDNSEngine(receipt.Engine) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(receipt.ManifestQualifier) ||
		!validMutationIdentity(receipt.MutationRequestID) ||
		!validMutationIdentity(receipt.MutationOwnerID) {
		return errors.New("DNS engine install ownership identity is invalid")
	}
	switch hostplatform.PackageManager(receipt.PackageManager) {
	case hostplatform.PackageManagerAPT, hostplatform.PackageManagerPacman,
		hostplatform.PackageManagerDNF:
	default:
		return errors.New("DNS engine install ownership package manager is unsupported")
	}
	if len(receipt.Packages) == 0 || len(receipt.Packages) > 32 ||
		len(receipt.MissingBefore) == 0 ||
		len(receipt.MissingBefore) > len(receipt.Packages) {
		return errors.New("DNS engine install ownership package set is invalid")
	}
	validateSorted := func(values []string) error {
		previous := ""
		for _, value := range values {
			if !validPackageName(value) || (previous != "" && value <= previous) {
				return errors.New("DNS engine install ownership packages are not canonical")
			}
			previous = value
		}
		return nil
	}
	if err := validateSorted(receipt.Packages); err != nil {
		return err
	}
	if err := validateSorted(receipt.MissingBefore); err != nil {
		return err
	}
	packageSet := make(map[string]struct{}, len(receipt.Packages))
	for _, packageName := range receipt.Packages {
		packageSet[packageName] = struct{}{}
	}
	for _, packageName := range receipt.MissingBefore {
		if _, ok := packageSet[packageName]; !ok {
			return errors.New("DNS engine install ownership missing set is not a package subset")
		}
	}
	return nil
}

func encodeDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
) ([]byte, error) {
	if err := validateDNSEngineInstallOwnership(receipt); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode DNS engine install ownership: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeDNSEngineInstallOwnership(
	data []byte,
) (dnsEngineInstallOwnershipReceipt, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt dnsEngineInstallOwnershipReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return dnsEngineInstallOwnershipReceipt{},
			fmt.Errorf("decode DNS engine install ownership: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership contains trailing JSON")
	}
	canonical, err := encodeDNSEngineInstallOwnership(receipt)
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, err
	}
	if !bytes.Equal(data, canonical) {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership is not canonical JSON")
	}
	return receipt, nil
}

func readDNSEngineInstallOwnership(
	engine transport.DNSEngine,
) (dnsEngineInstallOwnershipReceipt, bool, error) {
	path, err := dnsEngineInstallOwnershipPath(engine)
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, false, err
	}
	data, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, false, err
	}
	receipt, err := decodeDNSEngineInstallOwnership(data)
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, false, err
	}
	if receipt.Engine != engine {
		return dnsEngineInstallOwnershipReceipt{}, false,
			errors.New("DNS engine install ownership engine differs from its path")
	}
	return receipt, true, nil
}

func writeDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
) error {
	path, err := dnsEngineInstallOwnershipPath(receipt.Engine)
	if err != nil {
		return err
	}
	encoded, err := encodeDNSEngineInstallOwnership(receipt)
	if err != nil {
		return err
	}
	if err := secureWriteConfig(path, encoded, 0o600); err != nil {
		actual, exists, readErr := readDNSEngineInstallOwnership(receipt.Engine)
		if readErr == nil && exists && reflect.DeepEqual(actual, receipt) {
			return nil
		}
		return errors.Join(err, readErr)
	}
	actual, exists, err := readDNSEngineInstallOwnership(receipt.Engine)
	if err != nil || !exists || !reflect.DeepEqual(actual, receipt) {
		if err == nil {
			err = errors.New("DNS engine install ownership readback mismatch")
		}
		return err
	}
	return nil
}

func removeDNSEngineInstallOwnership(engine transport.DNSEngine) error {
	path, err := dnsEngineInstallOwnershipPath(engine)
	if err != nil {
		return err
	}
	removeErr := secureRemoveConfig(path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		_, exists, readErr := readDNSEngineInstallOwnership(engine)
		if readErr != nil || exists {
			return errors.Join(removeErr, readErr)
		}
	}
	if err := syncAtomicParentDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	_, exists, err := readDNSEngineInstallOwnership(engine)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("DNS engine install ownership still exists after retirement")
	}
	return nil
}

func retireDNSEngineInstallOwnership(journal dnsEngineSwitchJournal) error {
	if journal.Phase != dnsSwitchPhaseCommitted ||
		!transport.ValidDNSEngine(journal.TargetEngine) {
		return errors.New("DNS engine install ownership can retire only for a committed target")
	}
	return removeDNSEngineInstallOwnership(journal.TargetEngine)
}

func publishCommittedDNSEngineTargetOwnership(
	journal dnsEngineSwitchJournal,
) error {
	if journal.Phase != dnsSwitchPhaseCommitted ||
		!transport.ValidDNSEngine(journal.TargetEngine) {
		return errors.New("DNS engine target ownership requires a committed journal")
	}
	state, exists, err := readDNSEngineState()
	if err != nil {
		return err
	}
	if !exists || !exactDNSEngineStateForJournal(state, journal) {
		return errors.New("DNS engine target state differs from its committed journal")
	}
	return writeDNSEngineOwnership(state)
}

func newDNSEngineInstallOwnership(
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages, missing []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (dnsEngineInstallOwnershipReceipt, error) {
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
		manifest.TargetEngine != engine {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership differs from its switch target")
	}
	receipt := dnsEngineInstallOwnershipReceipt{
		Schema: dnsEngineInstallOwnershipSchema,
		Engine: engine, PackageManager: string(manager),
		Packages:          append([]string(nil), packages...),
		MissingBefore:     append([]string(nil), missing...),
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
	}
	sort.Strings(receipt.Packages)
	sort.Strings(receipt.MissingBefore)
	return receipt, validateDNSEngineInstallOwnership(receipt)
}

func installOwnedDNSEnginePackages(
	receipt dnsEngineInstallOwnershipReceipt,
	install func() error,
) error {
	if install == nil {
		return errors.New("DNS engine package installer is required")
	}
	if err := writeDNSEngineInstallOwnership(receipt); err != nil {
		return err
	}
	return install()
}

type dnsEngineInstallOwnershipHandoffOps struct {
	read  func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error)
	write func(dnsEngineInstallOwnershipReceipt) error
}

func handoffExistingDNSEngineInstallOwnership(
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) error {
	return handoffExistingDNSEngineInstallOwnershipWithOps(
		engine, manager, packages, manifest, binding,
		dnsEngineInstallOwnershipHandoffOps{
			read: readDNSEngineInstallOwnership, write: writeDNSEngineInstallOwnership,
		},
	)
}

func handoffExistingDNSEngineInstallOwnershipWithOps(
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	ops dnsEngineInstallOwnershipHandoffOps,
) error {
	if ops.read == nil || ops.write == nil || manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
		manifest.TargetEngine != engine {
		return errors.New("invalid DNS engine install ownership handoff")
	}
	existing, exists, err := ops.read(engine)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !exactDNSEngineInstallOwnership(existing, true, engine, manager, packages) {
		return errors.New("existing DNS engine install ownership differs from the retry target")
	}
	rebound := existing
	rebound.ManifestQualifier = manifest.Qualifier
	rebound.MutationRequestID = binding.MutationRequestID
	rebound.MutationOwnerID = binding.MutationOwnerID
	if err := validateDNSEngineInstallOwnership(rebound); err != nil {
		return err
	}
	if err := ops.write(rebound); err != nil {
		return err
	}
	actual, actualExists, err := ops.read(engine)
	if err != nil {
		return err
	}
	if !actualExists || !reflect.DeepEqual(actual, rebound) {
		return errors.New("DNS engine install ownership handoff readback mismatch")
	}
	return nil
}

func exactDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
	exists bool,
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
) bool {
	if !exists || receipt.Engine != engine ||
		receipt.PackageManager != string(manager) {
		return false
	}
	want := append([]string(nil), packages...)
	sort.Strings(want)
	return reflect.DeepEqual(receipt.Packages, want)
}

func managedDNSEnginePackagesForProfile(
	profile hostplatform.Profile,
	engine transport.DNSEngine,
) ([]string, error) {
	var packages []string
	switch engine {
	case transport.DNSEngineBIND:
		layout, err := bindLayout(profile)
		if err != nil {
			return nil, err
		}
		packages = append(packages, layout.Packages...)
	case transport.DNSEnginePowerDNS:
		service := core.GetManagedServiceByID("pdns")
		if service == nil {
			return nil, errors.New("PowerDNS service definition is unavailable")
		}
		family := string(profile.PackageManager)
		if service.LifecycleInstallFamilies == nil {
			return nil, errors.New("PowerDNS lifecycle policy is unavailable")
		}
		if !service.LifecycleInstallFamilies[family] {
			return nil, errors.New("PowerDNS lifecycle is unavailable for this host")
		}
		packages = append(
			packages,
			service.Packages[family]...,
		)
	default:
		return nil, errors.New("DNS engine package provenance target is unsupported")
	}
	if len(packages) == 0 {
		return nil, errors.New("DNS engine packages are unavailable for this host")
	}
	sort.Strings(packages)
	for index, packageName := range packages {
		if !validPackageName(packageName) ||
			(index > 0 && packages[index-1] == packageName) {
			return nil, errors.New("DNS engine package set is not canonical")
		}
	}
	return packages, nil
}

func exactCommittedDNSEngineProvenanceOnHost(
	journal dnsEngineSwitchJournal,
	profile hostplatform.Profile,
) (dnsEngineStateReceipt, bool, bool, error) {
	if err := validateDNSEngineSwitchJournal(journal); err != nil ||
		journal.Phase != dnsSwitchPhaseCommitted {
		if err == nil {
			err = errors.New("DNS engine provenance requires a committed journal")
		}
		return dnsEngineStateReceipt{}, false, false, err
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine state receipt: %w", err)
	}
	if !stateExists || validateDNSEngineState(state) != nil ||
		!exactDNSEngineStateForJournal(state, journal) {
		return dnsEngineStateReceipt{}, false, false,
			errors.New("committed DNS engine state receipt is absent or differs from its journal")
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false, err
	}
	request := signedUpdateRollbackEvidenceRequest(journal, manifest)
	install, installExists, err := readDNSEngineInstallOwnership(journal.TargetEngine)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine install ownership: %w", err)
	}
	if installExists {
		packages, err := managedDNSEnginePackagesForProfile(profile, journal.TargetEngine)
		if err != nil {
			return dnsEngineStateReceipt{}, false, false, err
		}
		if validateDNSEngineInstallOwnership(install) != nil ||
			!exactDNSEngineInstallEvidence(install, request, manifest) ||
			!exactDNSEngineInstallOwnership(
				install, true, journal.TargetEngine,
				profile.PackageManager, packages,
			) {
			return dnsEngineStateReceipt{}, false, false,
				errors.New("committed DNS engine install ownership differs from its journal")
		}
	}
	ownership, ownershipExists, err := readDNSEngineOwnership(journal.TargetEngine)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine ownership: %w", err)
	}
	if ownershipExists &&
		(validateDNSEngineState(ownership) != nil || ownership != state) {
		return dnsEngineStateReceipt{}, false, false,
			errors.New("committed DNS engine ownership differs from its exact active state")
	}
	if journal.Mode != transport.DNSEngineSwitchModeAdopt {
		if !installExists {
			if !ownershipExists {
				return dnsEngineStateReceipt{}, false, false,
					errors.New("committed DNS engine switch has no exact install or active ownership provenance")
			}
		}
	}
	return state, installExists, ownershipExists, nil
}
