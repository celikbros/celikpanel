package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsEngineStateSchema = "celikpanel-dns-engine-state/v1"

	dnsSecondaryWriteDeniedError = "directional secondary DNS cannot publish panel-local zones"
)

type hostDNSEngineBackend struct{}

type bindHostLayout struct {
	GenerationRoot string
	MainConfig     string
	OptionsConfig  string
	AnchorConfig   string
	Unit           string
	Packages       []string
}

type dnsEngineStateReceipt struct {
	Schema               string              `json:"schema"`
	Mode                 string              `json:"mode"`
	Engine               transport.DNSEngine `json:"engine"`
	EngineEpoch          int64               `json:"engine_epoch"`
	Generation           string              `json:"generation,omitempty"`
	PairRole             string              `json:"pair_role,omitempty"`
	PrimaryCatalogSerial uint32              `json:"primary_catalog_serial,omitempty"`
	SourceRevision       int64               `json:"source_revision"`
	ManifestQualifier    string              `json:"manifest_qualifier"`
	MutationRequestID    string              `json:"mutation_request_id"`
	MutationOwnerID      string              `json:"mutation_owner_id"`
}

type dnsUnitState struct {
	Name          string
	LoadState     string
	ActiveState   string
	UnitFileState string
}

func (state dnsUnitState) active() bool { return state.ActiveState == "active" }

type dnsUnitIdentity struct {
	ID           string
	Names        []string
	FragmentPath string
}

type bindConfigMutation struct {
	paths     []string
	original  map[string][]byte
	desired   map[string][]byte
	snapshots map[string]dnsFileSnapshot
}

type trackedBINDValidator struct {
	checkZone string
	checkConf string
}

func runTrackedBINDValidation(
	ctx context.Context,
	path, name string,
	args ...string,
) ([]byte, error) {
	output, err := serviceMutationCommand(ctx, path, args...).CombinedOutputLimited(64 << 10)
	if err != nil {
		return output, fmt.Errorf("%s failed: %w: %s", name, err, firstLine(string(output)))
	}
	return output, nil
}

func (runner trackedBINDValidator) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path := ""
	switch name {
	case "named-checkzone":
		path = runner.checkZone
	case "named-checkconf":
		path = runner.checkConf
	default:
		return nil, fmt.Errorf("unsupported BIND validation command %q", name)
	}
	return runTrackedBINDValidation(ctx, path, name, args...)
}

func bindLayout(profile hostplatform.Profile) (bindHostLayout, error) {
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		return bindHostLayout{
			GenerationRoot: "/var/cache/bind/celikpanel",
			MainConfig:     "/etc/bind/named.conf",
			OptionsConfig:  "/etc/bind/named.conf.options",
			AnchorConfig:   "/etc/bind/named.conf.local",
			Unit:           "named.service",
			Packages:       []string{"bind9"},
		}, nil
	case hostplatform.PackageManagerPacman:
		return bindHostLayout{
			GenerationRoot: "/var/named/celikpanel",
			MainConfig:     "/etc/named.conf",
			OptionsConfig:  "/etc/named.conf",
			AnchorConfig:   "/etc/named.conf",
			Unit:           "named.service",
			Packages:       []string{"bind"},
		}, nil
	default:
		return bindHostLayout{}, errors.New("BIND switching is certified only for apt and pacman hosts")
	}
}

func dnsEngineStatePath() string {
	return filepath.Join(serviceMutationStateDirectory(), "dns-engine-state.json")
}

func encodeDNSEngineState(state dnsEngineStateReceipt) ([]byte, error) {
	if err := validateDNSEngineState(state); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode DNS engine state: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeDNSEngineState(data []byte) (dnsEngineStateReceipt, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return dnsEngineStateReceipt{}, errors.New("DNS engine state has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state dnsEngineStateReceipt
	if err := decoder.Decode(&state); err != nil {
		return dnsEngineStateReceipt{}, fmt.Errorf("decode DNS engine state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return dnsEngineStateReceipt{}, errors.New("DNS engine state contains trailing JSON")
	}
	canonical, err := encodeDNSEngineState(state)
	if err != nil {
		return dnsEngineStateReceipt{}, err
	}
	if !bytes.Equal(data, canonical) {
		return dnsEngineStateReceipt{}, errors.New("DNS engine state is not canonical JSON")
	}
	return state, nil
}

func validateDNSEngineState(state dnsEngineStateReceipt) error {
	if state.Schema != dnsEngineStateSchema || !transport.ValidDNSEngine(state.Engine) ||
		(state.Mode != transport.DNSEngineSwitchModeSwitch &&
			state.Mode != transport.DNSEngineSwitchModeAdopt) ||
		state.EngineEpoch < 1 || state.SourceRevision < 0 ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(state.ManifestQualifier) ||
		!validMutationIdentity(state.MutationRequestID) ||
		!validMutationIdentity(state.MutationOwnerID) {
		return errors.New("DNS engine state has an unsupported identity")
	}
	if state.Mode == transport.DNSEngineSwitchModeAdopt &&
		state.Engine != transport.DNSEnginePowerDNS {
		return errors.New("DNS engine adoption state must name PowerDNS")
	}
	if state.Mode == transport.DNSEngineSwitchModeAdopt &&
		(state.PairRole != "" || state.PrimaryCatalogSerial != 0) {
		return errors.New("legacy PowerDNS adoption state cannot claim directional primary identity")
	}
	switch state.PairRole {
	case transport.DNSPairRolePrimary:
		if state.PrimaryCatalogSerial == 0 {
			return errors.New("paired primary DNS engine state is missing its catalog serial")
		}
	case transport.DNSPairRoleSecondary:
		if state.PrimaryCatalogSerial != 0 {
			return errors.New("paired secondary DNS engine state contains a primary catalog serial")
		}
	case "":
		if state.PrimaryCatalogSerial != 0 {
			return errors.New("standalone DNS engine state contains a primary catalog serial")
		}
	default:
		return errors.New("DNS engine state has an unsupported pair role")
	}
	if state.Engine == transport.DNSEngineBIND {
		if !validDNSGeneration(state.Generation) {
			return errors.New("BIND engine state has an invalid generation")
		}
	} else if state.Generation != "" {
		return errors.New("PowerDNS engine state unexpectedly names a BIND generation")
	}
	return nil
}

func validDNSGeneration(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func readDNSEngineState() (dnsEngineStateReceipt, bool, error) {
	data, err := secureReadConfig(dnsEngineStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineStateReceipt{}, false, nil
	}
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	state, err := decodeDNSEngineState(data)
	return state, err == nil, err
}

func writeDNSEngineState(state dnsEngineStateReceipt) error {
	data, err := encodeDNSEngineState(state)
	if err != nil {
		return err
	}
	return secureWriteConfig(dnsEngineStatePath(), data, 0o600)
}

func newHostBINDPublisher(layout bindHostLayout) (*binddns.Publisher, trackedBINDValidator, error) {
	checkZone, err := firstTrustedExecutable(
		[]string{"/usr/sbin/named-checkzone", "/usr/bin/named-checkzone"},
		"named-checkzone",
	)
	if err != nil {
		return nil, trackedBINDValidator{}, err
	}
	checkConf, err := firstTrustedExecutable(
		[]string{"/usr/sbin/named-checkconf", "/usr/bin/named-checkconf"},
		"named-checkconf",
	)
	if err != nil {
		return nil, trackedBINDValidator{}, err
	}
	runner := trackedBINDValidator{checkZone: checkZone, checkConf: checkConf}
	publisher, err := binddns.NewPublisher(layout.GenerationRoot, binddns.OSFileSystem{}, runner)
	return publisher, runner, err
}

func (hostDNSEngineBackend) Readiness(
	ctx context.Context,
) (transport.DNSBackendReadinessResponse, error) {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return transport.DNSBackendReadinessResponse{}, err
	}
	layout, layoutErr := bindLayout(profile)
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.DNSBackendReadinessResponse{}, err
	}
	states := []transport.DNSBackendRuntimeState{
		{Engine: transport.DNSEngineBIND, Unit: "named.service"},
		{Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service"},
	}
	if layoutErr == nil {
		states[0].Installed = packagesInstalled(profile, layout.Packages)
	}
	pdnsPackages := []string(nil)
	if service := core.GetManagedServiceByID("pdns"); service != nil {
		pdnsPackages = append(
			pdnsPackages,
			service.Packages[string(profile.PackageManager)]...,
		)
	}
	states[1].Installed = packagesInstalled(profile, pdnsPackages)
	if err := verifyBINDUnitTopology(ctx, systemctl); err != nil && states[0].Installed {
		return transport.DNSBackendReadinessResponse{}, err
	}
	for index := range states {
		unit, captureErr := captureDNSUnitState(ctx, systemctl, states[index].Unit)
		if captureErr != nil {
			return transport.DNSBackendReadinessResponse{}, captureErr
		}
		states[index].Running = unit.active()
	}
	state, exists, stateErr := readDNSEngineState()
	if stateErr != nil {
		return transport.DNSBackendReadinessResponse{}, stateErr
	}
	if exists && state.Engine == transport.DNSEngineBIND && layoutErr == nil {
		publisher, _, publisherErr := newHostBINDPublisher(layout)
		if publisherErr == nil {
			tree, treeErr := publisher.LoadCurrent()
			if treeErr == nil {
				receipt := tree.CurrentReceipt()
				managedEvidence := receipt.EngineEpoch == state.EngineEpoch &&
					receipt.Generation == state.Generation
				states[0].Managed = exactActiveDNSBackendManaged(
					states[0], managedEvidence,
					func() error { return verifyOnlyBINDActive(ctx, systemctl) },
				)
				if states[0].Managed {
					states[0].PairReady, err = bindPrimaryPairReady(ctx, tree)
					if err != nil {
						log.Printf("BIND primary peer readiness proof failed: %v", err)
					}
				}
			}
		}
	} else if layoutErr == nil && states[0].Installed && !states[0].Running {
		ownership, ownershipExists, ownershipErr := readDNSEngineOwnership(
			transport.DNSEngineBIND,
		)
		if ownershipErr != nil {
			log.Printf("BIND standby ownership proof failed: %v", ownershipErr)
		} else if ownershipExists {
			publisher, _, publisherErr := newHostBINDPublisher(layout)
			if publisherErr == nil {
				tree, treeErr := publisher.LoadCurrent()
				if treeErr == nil {
					receipt := tree.CurrentReceipt()
					states[0].Managed = bindStandbyManagedForBackendReadiness(
						states[0], ownership, ownershipExists,
						receipt.EngineEpoch, receipt.Generation,
					)
				}
			}
		}
		if !states[0].Managed &&
			installOwnershipFallbackAllowed(ownershipExists, ownershipErr) {
			installReceipt, installExists, installErr := readDNSEngineInstallOwnership(
				transport.DNSEngineBIND,
			)
			if installErr != nil {
				log.Printf("BIND install ownership proof failed: %v", installErr)
			} else {
				states[0].Managed = installOwnedStandbyManagedForBackendReadiness(
					states[0], installReceipt, installExists,
					transport.DNSEngineBIND, profile.PackageManager, layout.Packages,
				)
			}
		}
	}
	if (!exists || state.Engine != transport.DNSEnginePowerDNS) &&
		states[1].Installed && !states[1].Running {
		ownership, ownershipExists, ownershipErr := readDNSEngineOwnership(
			transport.DNSEnginePowerDNS,
		)
		if ownershipErr != nil {
			log.Printf("PowerDNS standby ownership proof failed: %v", ownershipErr)
		} else {
			states[1].Managed = powerDNSStandbyManagedForBackendReadiness(
				states[1], ownership, ownershipExists,
				requireManagedPowerDNSArtifacts,
			)
		}
		if !states[1].Managed &&
			installOwnershipFallbackAllowed(ownershipExists, ownershipErr) {
			installReceipt, installExists, installErr := readDNSEngineInstallOwnership(
				transport.DNSEnginePowerDNS,
			)
			if installErr != nil {
				log.Printf("PowerDNS install ownership proof failed: %v", installErr)
			} else {
				states[1].Managed = installOwnedStandbyManagedForBackendReadiness(
					states[1], installReceipt, installExists,
					transport.DNSEnginePowerDNS, profile.PackageManager, pdnsPackages,
				)
			}
		}
	} else {
		states[1].Managed = powerDNSManagedForBackendReadiness(
			state,
			exists,
			states[1],
			requireManagedDNSClusterReady,
			func() error { return requireLegacyPowerDNSReadSafe(ctx, true) },
		)
		if states[1].Managed {
			states[1].PairReady, err = powerDNSPrimaryPairReady(ctx)
			if err != nil {
				log.Printf("PowerDNS primary peer readiness proof failed: %v", err)
			}
		}
	}
	port53Conflict, err := dnsPort53ConflictCheck(
		ctx, states[0].Running, states[1].Running,
	)
	if err != nil {
		return transport.DNSBackendReadinessResponse{}, err
	}
	return transport.DNSBackendReadinessResponse{
		Engines: states, Port53Conflict: port53Conflict,
	}, nil
}

func powerDNSManagedForBackendReadiness(
	state dnsEngineStateReceipt,
	stateExists bool,
	runtimeState transport.DNSBackendRuntimeState,
	managedConfigReady func() error,
	exactActiveRuntimeReady func() error,
) bool {
	if managedConfigReady == nil {
		return false
	}
	if stateExists && state.Engine != transport.DNSEnginePowerDNS {
		return false
	}
	if !runtimeState.Installed || !runtimeState.Running ||
		exactActiveRuntimeReady == nil {
		return false
	}
	if err := managedConfigReady(); err != nil {
		return false
	}
	return exactActiveDNSBackendManaged(
		runtimeState, true, exactActiveRuntimeReady,
	)
}

func exactActiveDNSBackendManaged(
	runtimeState transport.DNSBackendRuntimeState,
	managedEvidence bool,
	exactActiveRuntimeReady func() error,
) bool {
	return managedEvidence && runtimeState.Installed && runtimeState.Running &&
		exactActiveRuntimeReady != nil && exactActiveRuntimeReady() == nil
}

func powerDNSStandbyManagedForBackendReadiness(
	runtimeState transport.DNSBackendRuntimeState,
	ownership dnsEngineStateReceipt,
	ownershipExists bool,
	managedArtifactsReady func() error,
) bool {
	return runtimeState.Installed && !runtimeState.Running &&
		ownershipExists && ownership.Engine == transport.DNSEnginePowerDNS &&
		managedArtifactsReady != nil && managedArtifactsReady() == nil
}

func bindStandbyManagedForBackendReadiness(
	runtimeState transport.DNSBackendRuntimeState,
	ownership dnsEngineStateReceipt,
	ownershipExists bool,
	treeEpoch int64,
	treeGeneration string,
) bool {
	return runtimeState.Installed && !runtimeState.Running &&
		ownershipExists && ownership.Engine == transport.DNSEngineBIND &&
		ownership.EngineEpoch == treeEpoch &&
		ownership.Generation == treeGeneration
}

func installOwnedStandbyManagedForBackendReadiness(
	runtimeState transport.DNSBackendRuntimeState,
	receipt dnsEngineInstallOwnershipReceipt,
	receiptExists bool,
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
) bool {
	return runtimeState.Installed && !runtimeState.Running &&
		exactDNSEngineInstallOwnership(
			receipt, receiptExists, engine, manager, packages,
		)
}

func installOwnershipFallbackAllowed(
	engineOwnershipExists bool,
	engineOwnershipErr error,
) bool {
	return engineOwnershipErr == nil && !engineOwnershipExists
}

func packagesInstalled(profile hostplatform.Profile, packages []string) bool {
	if len(packages) == 0 {
		return false
	}
	for _, packageName := range packages {
		if !packageInstalledForProfile(profile, packageName) {
			return false
		}
	}
	return true
}

func managedServicePackagesInstalled(profile hostplatform.Profile, serviceID string) bool {
	service := core.GetManagedServiceByID(serviceID)
	if service == nil {
		return false
	}
	return packagesInstalled(profile, service.Packages[string(profile.PackageManager)])
}

func (hostDNSEngineBackend) Sync(
	ctx context.Context,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
) (string, error) {
	engine := transport.DNSEngine(commitment.Engine)
	if engine != transport.DNSEngineBIND && engine != transport.DNSEnginePowerDNS {
		return "", errors.New("DNS V3 publication engine is unsupported")
	}
	state, err := readExactDNSPanelWriteState(engine, commitment.EngineEpoch)
	if err != nil {
		return "", err
	}
	if err := reconcileExistingDNSEngineSwitchJournal(ctx); err != nil {
		return "", fmt.Errorf("reconcile prior DNS engine transaction: %w", err)
	}
	state, err = readExactDNSPanelWriteState(engine, commitment.EngineEpoch)
	if err != nil {
		return "", err
	}
	if engine == transport.DNSEnginePowerDNS {
		return syncPDNSV3Zone(ctx, state, commitment, binding)
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return "", err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return "", err
	}
	publisher, _, err := newHostBINDPublisher(layout)
	if err != nil {
		return "", err
	}
	current, err := publisher.LoadCurrent()
	if err != nil {
		return "", fmt.Errorf("load verified current BIND generation: %w", err)
	}
	receipt := current.CurrentReceipt()
	if receipt.EngineEpoch != state.EngineEpoch || receipt.Generation != state.Generation {
		return "", errors.New("BIND current pointer and engine state receipt disagree")
	}
	plan, err := binddns.ApplyDelta(current, binddns.ZoneSnapshot{
		DesiredGeneration: commitment.DesiredGeneration,
		Domain:            commitment.Domain,
		Delete:            commitment.Delete,
		Qualifier:         commitment.Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		Records:           commitment.Records,
	})
	if err != nil {
		return "", err
	}
	generation, err := publisher.StagePlan(ctx, plan)
	if err != nil {
		return "", err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return "", err
	}
	stateBefore, err := captureDNSFileSnapshot(dnsEngineStatePath(), 0o600, false)
	if err != nil {
		return "", err
	}
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if output, commandErr := runDNSSystemctl(applyCtx, systemctl, "reload", layout.Unit); commandErr != nil {
			return fmt.Errorf("reload BIND: %w: %s", commandErr, firstLine(string(output)))
		}
		if err := verifyOnlyBINDActive(applyCtx, systemctl); err != nil {
			return err
		}
		currentTree, err := publisher.LoadCurrent()
		if err != nil {
			return err
		}
		nextState := state
		nextState.Generation = generation.ID
		return applyVerifiedBINDV3GenerationAt(
			applyCtx, attempt, currentTree, receipt, generation.ReceiptValue,
			func(verifyCtx context.Context) error {
				return verifyDNSZoneManifestAuthority(verifyCtx, []transport.DNSEngineSwitchZoneSnapshot{{
					Domain: commitment.Domain, DesiredGeneration: commitment.DesiredGeneration,
					Delete: commitment.Delete, ZoneType: commitment.ZoneType,
					Records: commitment.Records, ZoneQualifier: commitment.Qualifier,
				}})
			},
			verifyRestoredBINDV3Generation,
			func() error {
				if err := writeDNSEngineState(nextState); err != nil {
					return fmt.Errorf("publish BIND engine generation state: %w", err)
				}
				return nil
			},
			func() error { return restoreDNSFileSnapshot(stateBefore) },
		)
	}
	recoverEmpty := func(recoveryCtx context.Context) error {
		return errors.Join(
			stopBINDUnitsFailClosed(recoveryCtx, systemctl),
			restoreDNSFileSnapshot(stateBefore),
		)
	}
	if err := publisher.Switch(ctx, generation.ID, apply, recoverEmpty); err != nil {
		return "", err
	}
	if err := markDNSZoneSyncV3Applied(
		ctx, commitment.Domain, commitment.Qualifier,
	); err != nil {
		return "", err
	}
	publishedTree, err := publisher.LoadCurrent()
	if err != nil {
		return "", dnsZoneV3RecoveryAmbiguous(
			fmt.Errorf("load published BIND generation: %w", err),
		)
	}
	if !reflect.DeepEqual(publishedTree.CurrentReceipt(), generation.ReceiptValue) {
		return "", dnsZoneV3RecoveryAmbiguous(
			errors.New("BIND published receipt changed before peer propagation"),
		)
	}
	if err := completeManagedBINDV3Propagation(
		ctx, publishedTree, commitment.Domain,
	); err != nil {
		var pending *dnsZoneV3RecoveryPendingError
		if errors.As(err, &pending) {
			return "", err
		}
		return "", dnsZoneV3RecoveryAmbiguous(err)
	}
	return generation.ID, nil
}

func (hostDNSEngineBackend) RecoverZone(
	ctx context.Context,
	domain, qualifier string,
	binding transport.ServiceMutationBinding,
) (bool, error) {
	state, exists, err := readDNSEngineState()
	if err != nil || !exists {
		return false, err
	}
	if err := requireDNSPanelWriteAuthority(state); err != nil {
		return false, err
	}
	if state.Engine == transport.DNSEnginePowerDNS {
		return recoverPDNSV3Zone(ctx, state, domain, qualifier, binding)
	}
	if state.Engine != transport.DNSEngineBIND {
		return false, errors.New("DNS V3 recovery engine is unsupported")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return false, err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return false, err
	}
	publisher, _, err := newHostBINDPublisher(layout)
	if err != nil {
		return false, err
	}
	tree, err := publisher.LoadCurrent()
	if err != nil {
		return false, err
	}
	receipt := tree.CurrentReceipt()
	if receipt.EngineEpoch != state.EngineEpoch {
		return false, errors.New("BIND current tree epoch differs from the active engine receipt")
	}
	zone, zoneData, found := tree.Zone(domain)
	if !found || zone.Qualifier != qualifier ||
		zone.MutationRequestID != binding.MutationRequestID ||
		zone.MutationOwnerID != binding.MutationOwnerID {
		return false, nil
	}
	expected, err := expectedDNSZoneAuthorityFromBINDTree(zone, zoneData)
	if err != nil {
		return false, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return false, err
	}
	if state.Generation != receipt.Generation {
		if output, commandErr := runDNSSystemctl(ctx, systemctl, "reload", layout.Unit); commandErr != nil {
			return false, fmt.Errorf("recover BIND publication reload: %w: %s", commandErr, firstLine(string(output)))
		}
		state.Generation = receipt.Generation
		if err := writeDNSEngineState(state); err != nil {
			return false, err
		}
	}
	if err := verifyOnlyBINDActive(ctx, systemctl); err != nil {
		return false, err
	}
	if err := verifyDNSZoneAuthorities(ctx, []expectedDNSZoneAuthority{expected}); err != nil {
		return false, err
	}
	if err := completeManagedBINDV3Propagation(ctx, tree, domain); err != nil {
		return false, err
	}
	return true, nil
}

func (hostDNSEngineBackend) Switch(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	if err := reconcileExistingDNSEngineSwitchJournal(ctx); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if manifest.TargetEngine == transport.DNSEnginePowerDNS {
		return switchToPDNS(ctx, manifest, binding)
	}
	if manifest.TargetEngine != transport.DNSEngineBIND {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("DNS engine switch target is unsupported")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if stateExists && state.Engine == manifest.TargetEngine &&
		state.EngineEpoch == manifest.TargetEpoch &&
		state.ManifestQualifier == manifest.Qualifier &&
		state.MutationRequestID == binding.MutationRequestID &&
		state.MutationOwnerID == binding.MutationOwnerID {
		if err := validateEngineStateCatalogContract(manifest, state); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(
			manifest, binding, state.PrimaryCatalogSerial,
		)
		if err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		expected, err := binddns.RenderTree(layout.GenerationRoot, plan)
		if err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		response, err := verifyCompletedBINDEngineSwitch(
			ctx, profile, layout, expected, state, manifest.Zones,
		)
		if err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyCompletedPrimaryCatalogTarget(
			ctx, profile, manifest, state,
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		return response, nil
	}
	if err := verifyDNSEngineSwitchSource(ctx, profile, manifest, state, stateExists); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	primaryCatalogSerial, err := primaryCatalogSerialFromSource(
		ctx, profile, manifest, state, stateExists,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(
		manifest, binding, primaryCatalogSerial,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		if err == nil {
			err = errors.New("a DNS engine switch recovery journal requires reconciliation")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyBINDUnitTopology(ctx, systemctl); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if manifest.SourceEngine == transport.DNSEnginePowerDNS {
		if err := verifyUnsignedPowerDNSForManifest(ctx, manifest); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
		if err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if _, err := probeDNSCatalogAXFR(ctx, manifest.PeerIP, catalogDomain); err != nil {
			return transport.SwitchDNSEngineV1Response{},
				errors.New("paired primary catalog is unavailable")
		}
	}
	targetBefore, err := captureDNSUnitStates(
		ctx, systemctl, []string{"bind9.service", "named.service"},
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	sourceUnits := []string{}
	if manifest.SourceEngine == transport.DNSEnginePowerDNS {
		sourceUnits = []string{"pdns.service"}
	}
	sourceBefore, err := captureDNSUnitStates(ctx, systemctl, sourceUnits)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := publishDNSEngineSourceOwnership(
		manifest, state, stateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	missing := make([]string, 0, len(layout.Packages))
	for _, packageName := range layout.Packages {
		if !packageInstalledForProfile(profile, packageName) {
			missing = append(missing, packageName)
		}
	}
	installMutation := func() error {
		_, installErr := installBINDPackagesWithGuard(ctx, systemctl, func() (string, error) {
			if len(missing) == 0 {
				return "", nil
			}
			return installPackagesWithCandidateContext(
				ctx, string(profile.PackageManager), missing, "",
			)
		})
		if installErr != nil {
			return fmt.Errorf("install BIND in no-start mode: %w", installErr)
		}
		return nil
	}
	if len(missing) != 0 {
		installReceipt, receiptErr := newDNSEngineInstallOwnership(
			transport.DNSEngineBIND, profile.PackageManager,
			layout.Packages, missing, manifest, binding,
		)
		if receiptErr != nil {
			return transport.SwitchDNSEngineV1Response{}, receiptErr
		}
		plainInstall := installMutation
		installMutation = func() error {
			return installOwnedDNSEnginePackages(installReceipt, plainInstall)
		}
	}
	if err := runDNSPort53PreMutationGuard(
		ctx, !stateExists && manifest.SourceEngine == "",
		installMutation,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	publisher, validator, err := newHostBINDPublisher(layout)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	generation, err := publisher.StagePlan(ctx, plan)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	transferPeer := ""
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		transferPeer = manifest.PeerIP
	}
	configs, err := prepareBINDConfigMutation(layout, transferPeer)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	stateBefore, err := captureDNSFileSnapshot(dnsEngineStatePath(), 0o600, true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	previousGeneration, hadPrevious, err := publisher.Current()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	actualState, actualStateExists, err := readDNSEngineState()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if actualStateExists != stateExists ||
		(actualStateExists && actualState != state) {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("DNS source state changed before the switch journal")
	}
	if err := verifyDNSEngineSwitchSource(
		ctx, profile, manifest, actualState, actualStateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	actualCatalogSerial, err := primaryCatalogSerialFromSource(
		ctx, profile, manifest, actualState, actualStateExists,
	)
	if err != nil || actualCatalogSerial != primaryCatalogSerial {
		if err == nil {
			err = errors.New("primary catalog source changed before the switch journal")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              manifest.Mode,
		MutationRequestID: binding.MutationRequestID, MutationOwnerID: binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier, SourceEngine: manifest.SourceEngine,
		TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch,
		TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		PrimaryCatalogSerial: primaryCatalogSerial,
		SnapshotBytes:        manifest.SnapshotBytes, Zones: manifest.Zones,
		TargetGeneration: generation.ID, PreviousGeneration: previousGeneration,
		HadPrevious: hadPrevious, StateBefore: stateBefore,
		ConfigBefore:      bindConfigMutationSnapshots(configs),
		TargetUnitsBefore: dnsUnitStateMapSnapshots(targetBefore),
		SourceUnitsBefore: dnsUnitStateMapSnapshots(sourceBefore),
	}
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollbackAndJournal := func(rollbackCtx context.Context) error {
		journal.Phase = dnsSwitchPhaseRollingBack
		journalErr := writeDNSEngineSwitchJournal(journal)
		rollbackErr := rollbackBINDActivation(
			rollbackCtx, systemctl, configs, stateBefore, targetBefore, sourceBefore,
		)
		if rollbackErr == nil {
			rollbackErr = verifyRestoredDNSSwitchSource(
				context.WithoutCancel(rollbackCtx), profile, systemctl, manifest, journal,
			)
		}
		if rollbackErr == nil {
			journal.Phase = dnsSwitchPhaseRolledBack
			journalErr = errors.Join(journalErr, writeDNSEngineSwitchJournal(journal), removeDNSEngineSwitchJournal())
		}
		return errors.Join(journalErr, rollbackErr)
	}
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if attempt > 1 {
			return rollbackAndJournal(applyCtx)
		}
		if err := configs.apply(); err != nil {
			return err
		}
		if _, err := runTrackedBINDValidation(
			applyCtx, validator.checkConf, "named-checkconf",
			"-z", layout.MainConfig,
		); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseTargetStaged
		if err := writeDNSEngineSwitchJournal(journal); err != nil {
			return err
		}
		if manifest.SourceEngine == transport.DNSEnginePowerDNS {
			if output, err := runDNSSystemctl(applyCtx, systemctl, "disable", "--now", "pdns.service"); err != nil {
				return fmt.Errorf("stop source PowerDNS: %w: %s", err, firstLine(string(output)))
			}
		}
		journal.Phase = dnsSwitchPhaseSourceStopped
		if err := writeDNSEngineSwitchJournal(journal); err != nil {
			return err
		}
		for _, unit := range []string{"bind9.service", "named.service"} {
			if output, err := runDNSSystemctl(applyCtx, systemctl, "unmask", unit); err != nil {
				return fmt.Errorf("unmask BIND target %s: %w: %s", unit, err, firstLine(string(output)))
			}
		}
		if err := enableServiceForMutationWithExecutable(applyCtx, systemctl, layout.Unit, true); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseTargetStarted
		if err := writeDNSEngineSwitchJournal(journal); err != nil {
			return err
		}
		if err := verifyOnlyBINDActive(applyCtx, systemctl); err != nil {
			return err
		}
		if err := verifyDNSZoneManifestAuthority(applyCtx, manifest.Zones); err != nil {
			return err
		}
		currentTree, err := publisher.LoadCurrent()
		if err != nil {
			return err
		}
		if err := verifyBINDPairingAuthority(
			applyCtx, currentTree.CurrentReceipt(),
		); err != nil {
			return err
		}
		nextState := dnsEngineStateReceipt{
			Schema: dnsEngineStateSchema,
			Mode:   manifest.Mode,
			Engine: transport.DNSEngineBIND, EngineEpoch: manifest.TargetEpoch,
			Generation: generation.ID, PairRole: pairRoleForEngineState(manifest),
			PrimaryCatalogSerial: primaryCatalogSerial,
			SourceRevision:       manifest.SourceRevision,
			ManifestQualifier:    manifest.Qualifier,
			MutationRequestID:    binding.MutationRequestID,
			MutationOwnerID:      binding.MutationOwnerID,
		}
		if err := verifyCompletedPrimaryCatalogTarget(
			applyCtx, profile, manifest, nextState,
		); err != nil {
			return err
		}
		if err := writeDNSEngineState(nextState); err != nil {
			actual, exists, readErr := readDNSEngineState()
			if readErr != nil || !exists || actual != nextState {
				return fmt.Errorf("publish active DNS engine state: %w", errors.Join(err, readErr))
			}
		}
		journal.Phase = dnsSwitchPhaseTargetVerified
		if err := writeDNSEngineSwitchJournal(journal); err != nil {
			return err
		}
		return nil
	}
	recoverEmpty := func(recoveryCtx context.Context) error {
		return rollbackAndJournal(recoveryCtx)
	}
	if err := publisher.Switch(ctx, generation.ID, apply, recoverEmpty); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	completed, exists, err := readDNSEngineState()
	if err != nil || !exists || completed.Generation != generation.ID ||
		completed.Engine != transport.DNSEngineBIND || completed.EngineEpoch != manifest.TargetEpoch {
		if err == nil {
			err = errors.New("active DNS engine receipt does not match the published BIND generation")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal.Phase = dnsSwitchPhaseCommitted
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
		Detail: "BIND is the verified active authoritative DNS engine",
	}, nil
}

func bindConfigMutationSnapshots(configs bindConfigMutation) []dnsFileSnapshot {
	snapshots := make([]dnsFileSnapshot, 0, len(configs.paths))
	for _, path := range configs.paths {
		snapshot := configs.snapshots[path]
		snapshot.Data = append([]byte(nil), snapshot.Data...)
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(left, right int) bool { return snapshots[left].Path < snapshots[right].Path })
	return snapshots
}

func dnsUnitStateMapSnapshots(states map[string]dnsUnitState) []dnsUnitSnapshot {
	units := make([]string, 0, len(states))
	for unit := range states {
		units = append(units, unit)
	}
	sort.Strings(units)
	snapshots := make([]dnsUnitSnapshot, 0, len(units))
	for _, unit := range units {
		state := states[unit]
		snapshots = append(snapshots, dnsUnitSnapshot{
			Name: state.Name, LoadState: state.LoadState,
			ActiveState: state.ActiveState, UnitFileState: state.UnitFileState,
		})
	}
	return snapshots
}

func bindSwitchTreePlanWithPrimaryCatalogSerial(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	primaryCatalogSerial uint32,
) (binddns.TreePlan, error) {
	if err := validatePrimaryCatalogSerialContract(
		manifest, primaryCatalogSerial,
	); err != nil {
		return binddns.TreePlan{}, err
	}
	zones := make([]binddns.ZoneSnapshot, len(manifest.Zones))
	for index, zone := range manifest.Zones {
		zones[index] = binddns.ZoneSnapshot{
			DesiredGeneration: zone.DesiredGeneration,
			Domain:            zone.Domain, Delete: zone.Delete,
			Qualifier:         zone.ZoneQualifier,
			MutationRequestID: binding.MutationRequestID,
			MutationOwnerID:   binding.MutationOwnerID,
			Records:           zone.Records,
		}
	}
	var pairing *binddns.Pairing
	if manifest.Topology == transport.DNSTopologyPaired {
		pairing = &binddns.Pairing{
			Role: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
			PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		}
	}
	return binddns.NewTreePlan(binddns.Manifest{
		EngineEpoch: manifest.TargetEpoch, Pairing: pairing,
		PrimaryCatalogSerial: primaryCatalogSerial, Zones: zones,
	})
}

func verifyCompletedBINDEngineSwitch(
	ctx context.Context,
	profile hostplatform.Profile,
	layout bindHostLayout,
	expected binddns.Generation,
	state dnsEngineStateReceipt,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) (transport.SwitchDNSEngineV1Response, error) {
	if state.Generation != expected.ID {
		return transport.SwitchDNSEngineV1Response{}, errors.New("completed BIND switch receipt differs from the resumed manifest")
	}
	publisher, _, err := newHostBINDPublisher(layout)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	tree, err := publisher.LoadCurrent()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	receipt := tree.CurrentReceipt()
	if receipt.Generation != expected.ID || receipt.EngineEpoch != state.EngineEpoch {
		return transport.SwitchDNSEngineV1Response{}, errors.New("completed BIND switch current tree differs from its receipt")
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyOnlyBINDActive(ctx, systemctl); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyDNSZoneManifestAuthority(ctx, zones); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyBINDPairingAuthority(ctx, receipt); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: state.EngineEpoch, AppliedZones: len(zones),
		Detail: "the exact BIND engine switch was already completed and verified",
	}, nil
}

func verifyDNSEngineSwitchSource(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
) error {
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	bindUnit, err := captureDNSUnitState(ctx, systemctl, "named.service")
	if err != nil {
		return err
	}
	pdnsUnit, err := captureDNSUnitState(ctx, systemctl, "pdns.service")
	if err != nil {
		return err
	}
	bindAliasUnit, err := captureDNSUnitState(ctx, systemctl, "bind9.service")
	if err != nil {
		return err
	}
	if manifest.Mode == transport.DNSEngineSwitchModeAdopt {
		if stateExists || manifest.SourceEngine != "" || manifest.SourceEpoch != 0 ||
			manifest.TargetEngine != transport.DNSEnginePowerDNS ||
			bindUnit.active() || bindAliasUnit.active() || !pdnsUnit.active() {
			return errors.New("PowerDNS adoption requires the sole unreceipted managed PowerDNS authority")
		}
		if err := requireManagedDNSClusterReady(); err != nil {
			return errors.New("PowerDNS adoption requires a managed PowerDNS authority")
		}
		return verifyOnlyPDNSActive(ctx, systemctl)
	}
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch {
		return errors.New("DNS engine operation mode is unsupported")
	}
	if isPDNSPairSecondaryReconfigureManifest(manifest) {
		if stateExists || bindUnit.active() || bindAliasUnit.active() ||
			!pdnsUnit.active() {
			return errors.New(
				"PowerDNS secondary reconfiguration requires the sole unreceipted running PowerDNS authority",
			)
		}
		if err := requireManagedDNSClusterReady(); err != nil {
			return errors.New(
				"PowerDNS secondary reconfiguration requires a managed PowerDNS authority",
			)
		}
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return err
		}
		if err := verifyStandaloneUnsignedPowerDNS(ctx); err != nil {
			return err
		}
		return verifyEmptyStandalonePDNSDatabase(ctx, pdnsDBPath())
	}
	if stateExists {
		if state.Engine != manifest.SourceEngine || state.EngineEpoch != manifest.SourceEpoch {
			return errors.New("DNS engine switch source does not match the active engine receipt")
		}
		switch manifest.SourceEngine {
		case transport.DNSEnginePowerDNS:
			if !pdnsUnit.active() || bindUnit.active() || bindAliasUnit.active() {
				return errors.New("PowerDNS receipt does not match the active port-53 authority")
			}
			return verifyOnlyPDNSActive(ctx, systemctl)
		case transport.DNSEngineBIND:
			if !bindUnit.active() || pdnsUnit.active() ||
				(bindAliasUnit.LoadState == "not-found" && bindAliasUnit.active()) {
				return errors.New("BIND receipt does not match the active port-53 authority")
			}
			layout, err := bindLayout(profile)
			if err != nil {
				return err
			}
			publisher, _, err := newHostBINDPublisher(layout)
			if err != nil {
				return err
			}
			tree, err := publisher.LoadCurrent()
			if err != nil {
				return err
			}
			receipt := tree.CurrentReceipt()
			if receipt.Generation != state.Generation || receipt.EngineEpoch != state.EngineEpoch {
				return errors.New("BIND source receipt differs from its immutable current generation")
			}
			if manifest.Topology == transport.DNSTopologyPaired {
				pairing := receipt.Pairing
				if pairing == nil || pairing.Role != manifest.PairRole ||
					pairing.LocalIP != manifest.LocalIP || pairing.LocalNS != manifest.LocalNS ||
					pairing.PeerIP != manifest.PeerIP || pairing.PeerNS != manifest.PeerNS {
					return errors.New("BIND source pair identity differs from the switch manifest")
				}
			} else if receipt.Pairing != nil {
				return errors.New("standalone switch found a paired BIND source")
			}
			return verifyOnlyBINDActive(ctx, systemctl)
		default:
			return errors.New("DNS engine switch source receipt names an unsupported engine")
		}
	}
	switch manifest.SourceEngine {
	case "":
		if manifest.SourceEpoch != 0 || bindUnit.active() ||
			bindAliasUnit.active() || pdnsUnit.active() {
			return errors.New("uninitialized DNS source requires no running authoritative DNS engine")
		}
		conflict, err := dnsPort53ConflictCheck(ctx, false, false)
		if err != nil {
			return err
		}
		if conflict {
			return errors.New("uninitialized DNS source has another public port-53 authority")
		}
	default:
		return errors.New("an unreceipted DNS source cannot be switched implicitly")
	}
	return nil
}

func prepareBINDConfigMutation(
	layout bindHostLayout,
	transferPeer string,
) (bindConfigMutation, error) {
	paths := []string{layout.OptionsConfig, layout.AnchorConfig}
	sort.Strings(paths)
	if len(paths) == 2 && paths[0] == paths[1] {
		paths = paths[:1]
	}
	mutation := bindConfigMutation{
		paths: paths, original: make(map[string][]byte, len(paths)),
		desired:   make(map[string][]byte, len(paths)),
		snapshots: make(map[string]dnsFileSnapshot, len(paths)),
	}
	for _, path := range paths {
		snapshot, err := captureDNSFileSnapshot(path, 0o644, false)
		if err != nil {
			return bindConfigMutation{}, fmt.Errorf("read BIND configuration %s: %w", path, err)
		}
		data := snapshot.Data
		mutation.snapshots[path] = snapshot
		mutation.original[path] = append([]byte(nil), data...)
		content := string(data)
		if path == layout.OptionsConfig {
			content, err = managedBINDOptions(content, transferPeer)
			if err != nil {
				return bindConfigMutation{}, fmt.Errorf("prepare BIND authoritative options: %w", err)
			}
		}
		if path == layout.AnchorConfig {
			content, err = managedBINDZoneInclude(
				content, filepath.ToSlash(filepath.Join(layout.GenerationRoot, "current", "zones.conf")),
			)
			if err != nil {
				return bindConfigMutation{}, fmt.Errorf("prepare BIND zone include: %w", err)
			}
		}
		mutation.desired[path] = []byte(content)
	}
	return mutation, nil
}

func (mutation bindConfigMutation) apply() error {
	written := make([]string, 0, len(mutation.paths))
	for _, path := range mutation.paths {
		if bytes.Equal(mutation.original[path], mutation.desired[path]) {
			continue
		}
		mode := os.FileMode(mutation.snapshots[path].Mode)
		if mode == 0 {
			return errors.New("BIND config mutation lost its exact file snapshot")
		}
		if err := secureWriteConfig(path, mutation.desired[path], mode); err != nil {
			for index := len(written) - 1; index >= 0; index-- {
				_ = restoreDNSFileSnapshot(mutation.snapshots[written[index]])
			}
			return fmt.Errorf("write managed BIND configuration %s: %w", path, err)
		}
		written = append(written, path)
	}
	return nil
}

func (mutation bindConfigMutation) restore() error {
	var restoreErr error
	for index := len(mutation.paths) - 1; index >= 0; index-- {
		path := mutation.paths[index]
		if err := restoreDNSFileSnapshot(mutation.snapshots[path]); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore BIND configuration %s: %w", path, err))
		}
	}
	return restoreErr
}

func captureDNSUnitState(ctx context.Context, systemctl, unit string) (dnsUnitState, error) {
	state, err := dnsSystemdStateGuard(systemctl).inspect(context.WithoutCancel(ctx), unit)
	if err != nil {
		return dnsUnitState{}, fmt.Errorf("inspect DNS unit %s: %w", unit, err)
	}
	snapshot := dnsUnitSnapshot{
		Name: state.name, LoadState: state.loadState,
		ActiveState: state.activeState, UnitFileState: state.unitFileState,
	}
	if err := validateDNSUnitSnapshot(snapshot); err != nil {
		return dnsUnitState{}, fmt.Errorf("inspect DNS unit %s: %w", unit, err)
	}
	return dnsUnitState{
		Name: snapshot.Name, LoadState: snapshot.LoadState,
		ActiveState: snapshot.ActiveState, UnitFileState: snapshot.UnitFileState,
	}, nil
}

func captureDNSUnitStates(ctx context.Context, systemctl string, units []string) (map[string]dnsUnitState, error) {
	states := make(map[string]dnsUnitState, len(units))
	for _, unit := range units {
		state, err := captureDNSUnitState(ctx, systemctl, unit)
		if err != nil {
			return nil, err
		}
		states[unit] = state
	}
	return states, nil
}

func runDNSSystemctl(ctx context.Context, systemctl string, args ...string) ([]byte, error) {
	return serviceMutationCommand(ctx, systemctl, args...).CombinedOutputLimited(64 << 10)
}

func stopBINDUnitsFailClosed(ctx context.Context, systemctl string) error {
	var stopErr error
	for _, unit := range []string{"bind9.service", "named.service"} {
		if output, err := runDNSSystemctl(ctx, systemctl, "stop", unit); err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("stop %s: %w: %s", unit, err, firstLine(string(output))))
		}
		state, err := dnsSystemdStateGuard(systemctl).inspect(ctx, unit)
		if err != nil {
			stopErr = errors.Join(stopErr, err)
		} else if state.active() {
			stopErr = errors.Join(stopErr, fmt.Errorf("%s remained active after fail-closed stop", unit))
		}
	}
	return stopErr
}

func restoreDNSUnitStates(
	ctx context.Context,
	systemctl string,
	states map[string]dnsUnitState,
	_ bool,
) error {
	units := make([]string, 0, len(states))
	for unit := range states {
		units = append(units, unit)
	}
	sort.Strings(units)
	snapshots := make([]dnsUnitSnapshot, 0, len(units))
	for _, unit := range units {
		state := states[unit]
		snapshots = append(snapshots, dnsUnitSnapshot{
			Name: state.Name, LoadState: state.LoadState,
			ActiveState: state.ActiveState, UnitFileState: state.UnitFileState,
		})
	}
	return restoreDNSUnitSnapshots(ctx, systemctl, snapshots)
}

func rollbackBINDActivation(
	ctx context.Context,
	systemctl string,
	configs bindConfigMutation,
	stateBefore dnsFileSnapshot,
	targetBefore, sourceBefore map[string]dnsUnitState,
) error {
	rollbackCtx := context.WithoutCancel(ctx)
	return errors.Join(
		restoreDNSUnitStates(rollbackCtx, systemctl, targetBefore, true),
		configs.restore(),
		restoreDNSFileSnapshot(stateBefore),
		restoreDNSUnitStates(rollbackCtx, systemctl, sourceBefore, false),
	)
}

func verifyOnlyBINDActive(ctx context.Context, systemctl string) error {
	if err := verifyBINDUnitTopology(ctx, systemctl); err != nil {
		return err
	}
	bindState, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "named.service")
	if err != nil {
		return err
	}
	bindAliasState, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "bind9.service")
	if err != nil {
		return err
	}
	pdnsState, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "pdns.service")
	if err != nil {
		return err
	}
	if !bindState.active() || pdnsState.active() ||
		(bindAliasState.loadState == "not-found" && bindAliasState.active()) {
		return errors.New("DNS activation did not leave exactly BIND as the active authority")
	}
	ss, err := firstTrustedExecutable([]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss")
	if err != nil {
		return err
	}
	output, err := serviceMutationCommand(
		context.WithoutCancel(ctx), ss, "-H", "-lntup", "sport = :53",
	).CombinedOutputLimited(64 << 10)
	if err != nil {
		return fmt.Errorf("inspect active DNS listeners: %w: %s", err, firstLine(string(output)))
	}
	if err := verifyBINDPublicListeners(string(output)); err != nil {
		return err
	}
	return nil
}

func syncPDNSV3Zone(
	ctx context.Context,
	state dnsEngineStateReceipt,
	commitment mutationpayload.DNSZoneSyncV3Commitment,
	binding transport.ServiceMutationBinding,
) (string, error) {
	if err := requireDNSPanelWriteAuthority(state); err != nil {
		return "", err
	}
	if err := requireManagedDNSClusterReady(); err != nil {
		return "", err
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return "", err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return "", err
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return "", err
	}
	if err := applyPDNSV3ZoneDatabase(ctx, pdnsDBPath(), commitment, binding); err != nil {
		return "", err
	}
	if err := markDNSZoneSyncV3Applied(
		ctx, commitment.Domain, commitment.Qualifier,
	); err != nil {
		return "", err
	}
	after, afterExists, err := readDNSEngineState()
	if err != nil || !afterExists || !reflect.DeepEqual(after, state) {
		if err == nil {
			err = errors.New("PowerDNS engine state changed during zone publication")
		}
		return "", dnsZoneV3RecoveryAmbiguous(err)
	}
	zone := transport.DNSEngineSwitchZoneSnapshot{
		Domain: commitment.Domain, DesiredGeneration: commitment.DesiredGeneration,
		Delete: commitment.Delete, ZoneType: commitment.ZoneType,
		Records: commitment.Records, ZoneQualifier: commitment.Qualifier,
	}
	propagation, err := prepareManagedPDNSV3Propagation(ctx, zone)
	if err != nil {
		var pending *dnsZoneV3RecoveryPendingError
		if errors.As(err, &pending) {
			return "", err
		}
		return "", dnsZoneV3RecoveryAmbiguous(err)
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return "", dnsZoneV3RecoveryAmbiguous(err)
	}
	if err := verifyDNSZoneManifestAuthority(
		ctx, []transport.DNSEngineSwitchZoneSnapshot{zone},
	); err != nil {
		return "", dnsZoneV3RecoveryAmbiguous(err)
	}
	if err := completePDNSV3Propagation(ctx, propagation); err != nil {
		return "", err
	}
	return commitment.Qualifier, nil
}

func recoverPDNSV3Zone(
	ctx context.Context,
	state dnsEngineStateReceipt,
	domain, qualifier string,
	binding transport.ServiceMutationBinding,
) (bool, error) {
	if err := requireDNSPanelWriteAuthority(state); err != nil {
		return false, err
	}
	if err := requireManagedDNSClusterReady(); err != nil {
		return false, err
	}
	snapshot, exact, err := readPDNSV3ZoneSnapshot(
		ctx, pdnsDBPath(), state.EngineEpoch, domain, qualifier, binding,
	)
	if err != nil || !exact {
		return false, err
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return false, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return false, err
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return false, err
	}
	propagation, err := prepareManagedPDNSV3Propagation(ctx, snapshot)
	if err != nil {
		return false, err
	}
	if err := verifyDNSZoneManifestAuthority(ctx, []transport.DNSEngineSwitchZoneSnapshot{snapshot}); err != nil {
		return false, err
	}
	if err := completePDNSV3Propagation(ctx, propagation); err != nil {
		return false, err
	}
	return true, nil
}

func readExactDNSPanelWriteState(
	engine transport.DNSEngine,
	epoch int64,
) (dnsEngineStateReceipt, error) {
	state, exists, err := readDNSEngineState()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("DNS engine state is not initialized")
		}
		return dnsEngineStateReceipt{}, err
	}
	if state.Engine != engine || state.EngineEpoch != epoch {
		switch engine {
		case transport.DNSEngineBIND:
			return dnsEngineStateReceipt{}, errors.New("BIND zone publication does not match the active engine epoch")
		case transport.DNSEnginePowerDNS:
			return dnsEngineStateReceipt{}, errors.New("PowerDNS zone publication does not match the active engine epoch")
		default:
			return dnsEngineStateReceipt{}, errors.New("DNS V3 publication engine is unsupported")
		}
	}
	if err := requireDNSPanelWriteAuthority(state); err != nil {
		return dnsEngineStateReceipt{}, err
	}
	return state, nil
}

func requireDNSPanelWriteAuthority(state dnsEngineStateReceipt) error {
	if state.PairRole == transport.DNSPairRoleSecondary {
		return errors.New(dnsSecondaryWriteDeniedError)
	}
	return nil
}

func verifyOnlyPDNSActive(ctx context.Context, systemctl string) error {
	bindState, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "named.service")
	if err != nil {
		return err
	}
	bindAliasState, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "bind9.service")
	if err != nil {
		return err
	}
	pdnsState, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "pdns.service")
	if err != nil {
		return err
	}
	if bindState.active() || bindAliasState.active() || !pdnsState.active() {
		return errors.New("DNS activation did not leave exactly PowerDNS as the active authority")
	}
	ss, err := firstTrustedExecutable([]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss")
	if err != nil {
		return err
	}
	output, err := serviceMutationCommand(
		context.WithoutCancel(ctx), ss, "-H", "-lntup", "sport = :53",
	).CombinedOutputLimited(64 << 10)
	if err != nil {
		return fmt.Errorf("inspect active DNS listeners: %w: %s", err, firstLine(string(output)))
	}
	return verifyPDNSPublicListeners(string(output))
}

func verifyPDNSPublicListeners(output string) error {
	foundTCP, foundUDP := false, false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		protocol := strings.ToLower(fields[0])
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		host, port, err := net.SplitHostPort(fields[4])
		if err != nil || port != "53" {
			continue
		}
		address := parseDNSListenerAddress(host)
		if address != nil && (address.IsLoopback() || address.IsLinkLocalUnicast()) {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "pdns_server") || strings.Contains(lower, "named") {
			return errors.New("a non-PowerDNS process is holding a public DNS listener")
		}
		if protocol == "tcp" {
			foundTCP = true
		} else {
			foundUDP = true
		}
	}
	if !foundTCP || !foundUDP {
		return errors.New("PowerDNS does not own both public TCP and UDP port 53 listeners")
	}
	return nil
}

func inspectDNSUnitIdentity(ctx context.Context, systemctl, unit string) (dnsUnitIdentity, error) {
	output, err := runDNSSystemctl(
		context.WithoutCancel(ctx), systemctl, "show", unit,
		"--property=Id,Names,FragmentPath", "--no-pager",
	)
	if err != nil {
		return dnsUnitIdentity{}, fmt.Errorf("inspect DNS unit identity %s: %w: %s", unit, err, firstLine(string(output)))
	}
	identity := dnsUnitIdentity{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "Id":
			identity.ID = value
			seen[key] = true
		case "Names":
			identity.Names = strings.Fields(value)
			sort.Strings(identity.Names)
			seen[key] = true
		case "FragmentPath":
			identity.FragmentPath = value
			seen[key] = true
		}
	}
	if !seen["Id"] || !seen["Names"] || !seen["FragmentPath"] {
		return dnsUnitIdentity{}, errors.New("systemctl returned incomplete DNS unit identity")
	}
	return identity, nil
}

func verifyBINDUnitTopology(ctx context.Context, systemctl string) error {
	guard := dnsSystemdStateGuard(systemctl)
	_, err := guard.inspect(ctx, "named.service")
	if err != nil {
		return err
	}
	bind9, err := guard.inspect(ctx, "bind9.service")
	if err != nil {
		return err
	}
	if bind9.loadState == "not-found" && bind9.unitFileState == "" {
		if bind9.active() {
			return errors.New("bind9.service is independently active without a loadable unit")
		}
		return nil
	}
	namedIdentity, err := inspectDNSUnitIdentity(ctx, systemctl, "named.service")
	if err != nil {
		return err
	}
	bindIdentity, err := inspectDNSUnitIdentity(ctx, systemctl, "bind9.service")
	if err != nil {
		return err
	}
	if namedIdentity.ID != bindIdentity.ID ||
		namedIdentity.FragmentPath == "" || namedIdentity.FragmentPath != bindIdentity.FragmentPath ||
		!reflect.DeepEqual(namedIdentity.Names, bindIdentity.Names) {
		return errors.New("bind9.service does not resolve to the exact named.service unit identity")
	}
	return nil
}

func verifyBINDPublicListeners(output string) error {
	foundTCP, foundUDP := false, false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		protocol := strings.ToLower(fields[0])
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		host, port, err := net.SplitHostPort(fields[4])
		if err != nil || port != "53" {
			continue
		}
		address := parseDNSListenerAddress(host)
		if address != nil && (address.IsLoopback() || address.IsLinkLocalUnicast()) {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "named") || strings.Contains(lower, "pdns") {
			return errors.New("a non-BIND process is holding a public DNS listener")
		}
		if protocol == "tcp" {
			foundTCP = true
		} else {
			foundUDP = true
		}
	}
	if !foundTCP || !foundUDP {
		return errors.New("BIND does not own both public TCP and UDP port 53 listeners")
	}
	return nil
}
