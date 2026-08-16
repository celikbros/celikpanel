package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const dnsEngineStateSchema = "celikpanel-dns-engine-state/v1"

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
	Schema            string              `json:"schema"`
	Engine            transport.DNSEngine `json:"engine"`
	EngineEpoch       int64               `json:"engine_epoch"`
	Generation        string              `json:"generation,omitempty"`
	SourceRevision    int64               `json:"source_revision"`
	ManifestQualifier string              `json:"manifest_qualifier"`
	MutationRequestID string              `json:"mutation_request_id"`
	MutationOwnerID   string              `json:"mutation_owner_id"`
}

type dnsUnitState struct {
	Exists  bool
	Enabled string
	Active  bool
}

type bindConfigMutation struct {
	paths    []string
	original map[string][]byte
	desired  map[string][]byte
}

type trackedBINDValidator struct {
	checkZone string
	checkConf string
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
	output, err := serviceMutationCommand(ctx, path, args...).CombinedOutputLimited(64 << 10)
	if err != nil {
		return output, fmt.Errorf("%s failed: %w: %s", name, err, firstLine(string(output)))
	}
	return output, nil
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
		state.EngineEpoch < 1 || state.SourceRevision < 0 ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(state.ManifestQualifier) ||
		!validMutationIdentity(state.MutationRequestID) ||
		!validMutationIdentity(state.MutationOwnerID) {
		return errors.New("DNS engine state has an unsupported identity")
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

func (hostDNSEngineBackend) Readiness(ctx context.Context) ([]transport.DNSBackendRuntimeState, error) {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return nil, err
	}
	layout, layoutErr := bindLayout(profile)
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return nil, err
	}
	states := []transport.DNSBackendRuntimeState{
		{Engine: transport.DNSEngineBIND, Unit: "named.service"},
		{Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service"},
	}
	if layoutErr == nil {
		states[0].Installed = packagesInstalled(profile, layout.Packages)
	}
	states[1].Installed = managedServicePackagesInstalled(profile, "pdns")
	for index := range states {
		unit, captureErr := captureDNSUnitState(ctx, systemctl, states[index].Unit)
		if captureErr != nil {
			return nil, captureErr
		}
		states[index].Running = unit.Active
	}
	state, exists, stateErr := readDNSEngineState()
	if stateErr != nil {
		return nil, stateErr
	}
	if exists && state.Engine == transport.DNSEngineBIND && layoutErr == nil {
		publisher, _, publisherErr := newHostBINDPublisher(layout)
		if publisherErr == nil {
			tree, treeErr := publisher.LoadCurrent()
			if treeErr == nil {
				receipt := tree.CurrentReceipt()
				states[0].Managed = receipt.EngineEpoch == state.EngineEpoch &&
					receipt.Generation == state.Generation
			}
		}
	}
	if exists && state.Engine == transport.DNSEnginePowerDNS && requireManagedDNSClusterReady() == nil {
		states[1].Managed = true
	}
	return states, nil
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
	if commitment.Engine != string(transport.DNSEngineBIND) {
		return "", errors.New("PowerDNS V3 publication is not initialized")
	}
	state, exists, err := readDNSEngineState()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("DNS engine state is not initialized")
		}
		return "", err
	}
	if state.Engine != transport.DNSEngineBIND || state.EngineEpoch != commitment.EngineEpoch {
		return "", errors.New("BIND zone publication does not match the active engine epoch")
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
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if output, commandErr := runDNSSystemctl(applyCtx, systemctl, "reload", layout.Unit); commandErr != nil {
			return fmt.Errorf("reload BIND: %w: %s", commandErr, firstLine(string(output)))
		}
		unit, inspectErr := captureDNSUnitState(applyCtx, systemctl, layout.Unit)
		if inspectErr != nil || !unit.Active {
			if inspectErr == nil {
				inspectErr = errors.New("BIND is not active after reload")
			}
			return inspectErr
		}
		if attempt == 1 {
			nextState := state
			nextState.Generation = generation.ID
			if err := writeDNSEngineState(nextState); err != nil {
				return fmt.Errorf("publish BIND engine generation state: %w", err)
			}
		}
		return nil
	}
	if err := publisher.Switch(ctx, generation.ID, apply); err != nil {
		return "", err
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
	if state.Engine != transport.DNSEngineBIND {
		return false, errors.New("PowerDNS V3 recovery is not initialized")
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
	exact := false
	for _, zone := range receipt.Zones {
		if zone.Domain != domain {
			continue
		}
		exact = zone.Qualifier == qualifier &&
			zone.MutationRequestID == binding.MutationRequestID &&
			zone.MutationOwnerID == binding.MutationOwnerID
		break
	}
	if !exact {
		return false, nil
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
	return true, nil
}

func (hostDNSEngineBackend) Switch(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	if manifest.TargetEngine != transport.DNSEngineBIND {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("switching to PowerDNS is not enabled by this agent build")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	plan, err := bindSwitchTreePlan(manifest, binding)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	expected, err := binddns.RenderTree(layout.GenerationRoot, plan)
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
		return verifyCompletedBINDEngineSwitch(ctx, profile, layout, expected, state, len(manifest.Zones))
	}
	if err := verifyDNSEngineSwitchSource(ctx, profile, manifest, state, stateExists); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	targetBefore, err := captureDNSUnitStates(ctx, systemctl, []string{"named.service", "bind9.service"})
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	sourceBefore, err := captureDNSUnitStates(ctx, systemctl, []string{"pdns.service"})
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	missing := make([]string, 0, len(layout.Packages))
	for _, packageName := range layout.Packages {
		if !packageInstalledForProfile(profile, packageName) {
			missing = append(missing, packageName)
		}
	}
	if _, err := installBINDPackagesWithGuard(ctx, systemctl, func() (string, error) {
		if len(missing) == 0 {
			return "", nil
		}
		return installPackagesWithCandidateContext(
			ctx, string(profile.PackageManager), missing, "",
		)
	}); err != nil {
		return transport.SwitchDNSEngineV1Response{}, fmt.Errorf("install BIND in no-start mode: %w", err)
	}
	publisher, validator, err := newHostBINDPublisher(layout)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	generation, err := publisher.StagePlan(ctx, plan)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	configs, err := prepareBINDConfigMutation(layout)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if attempt > 1 {
			return rollbackBINDActivation(
				applyCtx, systemctl, configs, targetBefore, sourceBefore,
			)
		}
		if err := configs.apply(); err != nil {
			return err
		}
		if _, err := validator.Run(applyCtx, "named-checkconf", "-z", layout.MainConfig); err != nil {
			return err
		}
		if manifest.SourceEngine == transport.DNSEnginePowerDNS {
			if output, err := runDNSSystemctl(applyCtx, systemctl, "disable", "--now", "pdns.service"); err != nil {
				return fmt.Errorf("stop source PowerDNS: %w: %s", err, firstLine(string(output)))
			}
		}
		for _, unit := range []string{"bind9.service", "named.service"} {
			if output, err := runDNSSystemctl(applyCtx, systemctl, "unmask", unit); err != nil {
				return fmt.Errorf("unmask BIND target %s: %w: %s", unit, err, firstLine(string(output)))
			}
		}
		if err := enableServiceForMutationWithExecutable(applyCtx, systemctl, layout.Unit, true); err != nil {
			return err
		}
		if err := verifyOnlyBINDActive(applyCtx, systemctl); err != nil {
			return err
		}
		nextState := dnsEngineStateReceipt{
			Schema: dnsEngineStateSchema,
			Engine: transport.DNSEngineBIND, EngineEpoch: manifest.TargetEpoch,
			Generation: generation.ID, SourceRevision: manifest.SourceRevision,
			ManifestQualifier: manifest.Qualifier,
			MutationRequestID: binding.MutationRequestID,
			MutationOwnerID:   binding.MutationOwnerID,
		}
		if err := writeDNSEngineState(nextState); err != nil {
			return fmt.Errorf("publish active DNS engine state: %w", err)
		}
		return nil
	}
	if err := publisher.Switch(ctx, generation.ID, apply); err != nil {
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
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
		Detail: "BIND is the verified active authoritative DNS engine",
	}, nil
}

func bindSwitchTreePlan(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (binddns.TreePlan, error) {
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
	return binddns.NewTreePlan(binddns.Manifest{EngineEpoch: manifest.TargetEpoch, Zones: zones})
}

func verifyCompletedBINDEngineSwitch(
	ctx context.Context,
	profile hostplatform.Profile,
	layout bindHostLayout,
	expected binddns.Generation,
	state dnsEngineStateReceipt,
	zoneCount int,
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
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: state.EngineEpoch, AppliedZones: zoneCount,
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
	if stateExists {
		if state.Engine != manifest.SourceEngine || state.EngineEpoch != manifest.SourceEpoch {
			return errors.New("DNS engine switch source does not match the active engine receipt")
		}
		if manifest.SourceEngine == transport.DNSEnginePowerDNS && (!pdnsUnit.Active || bindUnit.Active) {
			return errors.New("PowerDNS receipt does not match the active port-53 authority")
		}
		return nil
	}
	switch manifest.SourceEngine {
	case "":
		if manifest.SourceEpoch != 0 || bindUnit.Active || pdnsUnit.Active {
			return errors.New("uninitialized DNS source requires no running authoritative DNS engine")
		}
	case transport.DNSEnginePowerDNS:
		if manifest.SourceEpoch != 1 || !pdnsUnit.Active || bindUnit.Active ||
			requireManagedDNSClusterReady() != nil {
			return errors.New("legacy PowerDNS adoption requires the exact managed standalone source at epoch 1")
		}
	default:
		return errors.New("an unreceipted BIND source cannot be adopted implicitly")
	}
	return nil
}

func prepareBINDConfigMutation(layout bindHostLayout) (bindConfigMutation, error) {
	paths := []string{layout.OptionsConfig, layout.AnchorConfig}
	sort.Strings(paths)
	if len(paths) == 2 && paths[0] == paths[1] {
		paths = paths[:1]
	}
	mutation := bindConfigMutation{
		paths: paths, original: make(map[string][]byte, len(paths)),
		desired: make(map[string][]byte, len(paths)),
	}
	for _, path := range paths {
		data, err := secureReadConfig(path)
		if err != nil {
			return bindConfigMutation{}, fmt.Errorf("read BIND configuration %s: %w", path, err)
		}
		mutation.original[path] = append([]byte(nil), data...)
		content := string(data)
		if path == layout.OptionsConfig {
			content, err = managedBINDOptions(content)
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
		if err := secureWriteConfig(path, mutation.desired[path], 0o644); err != nil {
			for index := len(written) - 1; index >= 0; index-- {
				_ = secureWriteConfig(written[index], mutation.original[written[index]], 0o644)
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
		if err := secureWriteConfig(path, mutation.original[path], 0o644); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore BIND configuration %s: %w", path, err))
		}
	}
	return restoreErr
}

func captureDNSUnitState(ctx context.Context, systemctl, unit string) (dnsUnitState, error) {
	load, err := serviceMutationCommand(
		context.WithoutCancel(ctx), systemctl, "show", unit, "--property=LoadState", "--value", "--no-pager",
	).CombinedOutputLimited(4096)
	if err != nil {
		return dnsUnitState{}, fmt.Errorf("inspect DNS unit %s: %w: %s", unit, err, firstLine(string(load)))
	}
	loadState := strings.TrimSpace(string(load))
	if loadState == "not-found" || loadState == "" {
		return dnsUnitState{}, nil
	}
	if loadState != "loaded" && loadState != "masked" {
		return dnsUnitState{}, fmt.Errorf("DNS unit %s has unsupported LoadState %q", unit, loadState)
	}
	enabledOutput, _ := serviceMutationCommand(
		context.WithoutCancel(ctx), systemctl, "is-enabled", unit,
	).CombinedOutputLimited(4096)
	enabled := strings.TrimSpace(string(enabledOutput))
	activeErr := serviceMutationCommand(
		context.WithoutCancel(ctx), systemctl, "is-active", "--quiet", unit,
	).Run()
	return dnsUnitState{Exists: true, Enabled: enabled, Active: activeErr == nil}, nil
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

func restoreDNSUnitStates(
	ctx context.Context,
	systemctl string,
	states map[string]dnsUnitState,
	target bool,
) error {
	var restoreErr error
	units := make([]string, 0, len(states))
	for unit := range states {
		units = append(units, unit)
	}
	sort.Strings(units)
	for _, unit := range units {
		state := states[unit]
		if !state.Exists {
			if target {
				_, err := runDNSSystemctl(ctx, systemctl, "disable", "--now", unit)
				if err != nil {
					restoreErr = errors.Join(restoreErr, err)
				}
				_, err = runDNSSystemctl(ctx, systemctl, "mask", unit)
				if err != nil {
					restoreErr = errors.Join(restoreErr, err)
				}
			}
			continue
		}
		_, _ = runDNSSystemctl(ctx, systemctl, "unmask", unit)
		if state.Active {
			if _, err := runDNSSystemctl(ctx, systemctl, "start", unit); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		} else if _, err := runDNSSystemctl(ctx, systemctl, "stop", unit); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
		switch state.Enabled {
		case "enabled":
			if _, err := runDNSSystemctl(ctx, systemctl, "enable", unit); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		case "masked", "masked-runtime":
			if _, err := runDNSSystemctl(ctx, systemctl, "mask", unit); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		default:
			if _, err := runDNSSystemctl(ctx, systemctl, "disable", unit); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
	}
	return restoreErr
}

func rollbackBINDActivation(
	ctx context.Context,
	systemctl string,
	configs bindConfigMutation,
	targetBefore, sourceBefore map[string]dnsUnitState,
) error {
	rollbackCtx := context.WithoutCancel(ctx)
	return errors.Join(
		restoreDNSUnitStates(rollbackCtx, systemctl, targetBefore, true),
		configs.restore(),
		restoreDNSUnitStates(rollbackCtx, systemctl, sourceBefore, false),
	)
}

func verifyOnlyBINDActive(ctx context.Context, systemctl string) error {
	bindState, err := captureDNSUnitState(ctx, systemctl, "named.service")
	if err != nil {
		return err
	}
	pdnsState, err := captureDNSUnitState(ctx, systemctl, "pdns.service")
	if err != nil {
		return err
	}
	if !bindState.Active || pdnsState.Active {
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
		host = strings.Trim(host, "[]")
		address := net.ParseIP(host)
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
