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
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsEngineStateSchema = "celikpanel-dns-engine-state/v1"

	aptBINDGenerationRoot          = "/var/cache/bind/celikpanel"
	aptBINDCacheParentPath         = "/var/cache/bind"
	pacmanBINDGenerationRoot       = "/var/named/celikpanel"
	abandonedAPTBindGenerationRoot = "/etc/bind/celikpanel"
	bindDaemonReloadTimeout        = 15 * time.Second
	bindIdentityInspectionTimeout  = 15 * time.Second
	dnsRuntimeInspectionTimeout    = 15 * time.Second

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
	PairLocalIP          string              `json:"pair_local_ip,omitempty"`
	PairPeerIP           string              `json:"pair_peer_ip,omitempty"`
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
	ID            string
	Names         []string
	FragmentPath  string
	DropInPaths   []string
	SourcePath    string
	Transient     string
	ExecStartPath string
	ExecStartArgv string
}

type bindSecureFileIdentity struct {
	Device uint64
	Inode  uint64
	Size   int64
	Digest [32]byte
}

type bindVendorFilesIdentity struct {
	Unit        bindSecureFileIdentity
	Environment bindSecureFileIdentity
}

type bindConfigMutation struct {
	paths      []string
	original   map[string][]byte
	desired    map[string][]byte
	snapshots  map[string]dnsFileSnapshot
	layout     bindHostLayout
	ownerAware bool
	// adopted names the directives this mutation took over from the server's
	// own options block: what each said, and what CelikPanel makes it say. It
	// is empty on every path but a takeover.
	//
	// adopted, bu mutasyonun sunucunun kendi seçenek bloğundan devraldığı
	// direktifleri adlandırır: her biri ne diyordu ve CelikPanel ne dedirtiyor.
	// Devralma dışında her yolda boştur.
	adopted []bindForeignOptionDirective
}

// bindOptionsAuthority decides what the generation does when the server's own
// options block already defines a directive CelikPanel manages.
//
// bindOptionsExclusive is every path but one: refuse, because nobody consented
// to replacing a directive an administrator wrote. bindOptionsTakeover is the
// takeover the operator acknowledged on screen, having been shown each value
// found and each value CelikPanel will set (register R-042). The difference is
// consent, not safety: both prove ownership of every byte they write, and both
// snapshot the file so a rollback restores it exactly.
//
// bindOptionsAuthority, sunucunun kendi seçenek bloğu CelikPanel'in yönettiği
// bir direktifi zaten tanımlıyorsa neslin ne yapacağına karar verir.
//
// bindOptionsExclusive, biri dışında her yoldur: reddet; çünkü bir yöneticinin
// yazdığı direktifin değiştirilmesine kimse rıza göstermedi.
// bindOptionsTakeover, operatörün ekranda, bulunan her değer ile CelikPanel'in
// koyacağı her değer gösterildikten sonra onayladığı devralmadır (defter
// R-042). Fark güvenlik değil rızadır: ikisi de yazdığı her baytın sahipliğini
// kanıtlar ve ikisi de dosyayı anlık görüntüler, böylece bir geri alma onu
// birebir geri yükler.
type bindOptionsAuthority int

const (
	bindOptionsExclusive bindOptionsAuthority = iota
	bindOptionsTakeover
)

type bindConfigSnapshotReader func(
	path string,
	mode os.FileMode,
	allowAbsent bool,
) (dnsFileSnapshot, error)

type bindSwitchRollbackJournalOps struct {
	write    func(dnsEngineSwitchJournal) error
	rollback func() error
	verify   func() error
	remove   func() error
}

func runBINDRollbackWithJournal(
	journal *dnsEngineSwitchJournal,
	ops bindSwitchRollbackJournalOps,
) error {
	if journal == nil || ops.write == nil || ops.rollback == nil ||
		ops.verify == nil || ops.remove == nil {
		return errors.New("invalid BIND rollback journal operations")
	}
	journal.Phase = dnsSwitchPhaseRollingBack
	journalErr := ops.write(*journal)
	rollbackErr := ops.rollback()
	if rollbackErr == nil {
		rollbackErr = ops.verify()
	}
	if rollbackErr == nil {
		journal.Phase = dnsSwitchPhaseRolledBack
		finalWriteErr := ops.write(*journal)
		journalErr = errors.Join(journalErr, finalWriteErr)
		if finalWriteErr == nil {
			journalErr = errors.Join(journalErr, ops.remove())
		}
	}
	return errors.Join(journalErr, rollbackErr)
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
		if err := certifyAPTBINDCapabilities(profile); err != nil {
			return bindHostLayout{}, err
		}
		return bindHostLayout{
			GenerationRoot: aptBINDGenerationRoot,
			MainConfig:     "/etc/bind/named.conf",
			OptionsConfig:  "/etc/bind/named.conf.options",
			AnchorConfig:   "/etc/bind/named.conf.local",
			Unit:           "named.service",
			Packages:       []string{"bind9"},
		}, nil
	case hostplatform.PackageManagerPacman:
		if err := certifyPacmanBINDCapabilities(profile); err != nil {
			return bindHostLayout{}, err
		}
		return bindHostLayout{
			GenerationRoot: pacmanBINDGenerationRoot,
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

func certifyAPTBINDCapabilities(profile hostplatform.Profile) error {
	if profile.PackageManager != hostplatform.PackageManagerAPT ||
		profile.DistroFamily != hostplatform.DistroFamilyDebian ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return errors.New(
			"BIND switching requires a verified APT package ecosystem and systemd",
		)
	}
	return nil
}

func certifyPacmanBINDCapabilities(profile hostplatform.Profile) error {
	if profile.PackageManager != hostplatform.PackageManagerPacman ||
		profile.DistroFamily != hostplatform.DistroFamilyArch ||
		profile.ServiceManager != hostplatform.ServiceManagerSystemd {
		return errors.New(
			"BIND switching requires a verified pacman package ecosystem and systemd",
		)
	}
	return nil
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
		(state.PairRole != "" || state.PairLocalIP != "" ||
			state.PairPeerIP != "" || state.PrimaryCatalogSerial != 0) {
		return errors.New("legacy PowerDNS adoption state cannot claim directional primary identity")
	}
	if (state.PairLocalIP == "") != (state.PairPeerIP == "") {
		return errors.New("DNS engine state contains a partial pair address identity")
	}
	hasPairAddresses := state.PairLocalIP != ""
	if hasPairAddresses {
		localIP := net.ParseIP(state.PairLocalIP)
		peerIP := net.ParseIP(state.PairPeerIP)
		if localIP == nil || localIP.To4() == nil ||
			localIP.String() != state.PairLocalIP || !localIP.IsGlobalUnicast() ||
			peerIP == nil || peerIP.To4() == nil ||
			peerIP.String() != state.PairPeerIP || !peerIP.IsGlobalUnicast() ||
			localIP.Equal(peerIP) {
			return errors.New("DNS engine state pair addresses are not canonical and distinct")
		}
	}
	switch state.PairRole {
	case transport.DNSPairRolePrimary:
		if !hasPairAddresses || state.PrimaryCatalogSerial == 0 {
			return errors.New("paired primary DNS engine state is missing its catalog serial")
		}
	case transport.DNSPairRoleSecondary:
		if !hasPairAddresses || state.PrimaryCatalogSerial != 0 {
			return errors.New("paired secondary DNS engine state contains a primary catalog serial")
		}
	case "":
		if state.PrimaryCatalogSerial != 0 || hasPairAddresses {
			return errors.New("standalone DNS engine state contains directional pair identity")
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

func isLegacyDNSEngineState(state dnsEngineStateReceipt) bool {
	return state.PairRole == "" && state.PairLocalIP == "" &&
		state.PairPeerIP == "" && state.PrimaryCatalogSerial == 0
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

func readExactDNSEngineState() (dnsEngineStateReceipt, bool, error) {
	snapshot, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	if !snapshot.Exists {
		return dnsEngineStateReceipt{}, false, nil
	}
	state, err := decodeDNSEngineState(snapshot.Data)
	return state, err == nil, err
}

func writeDNSEngineState(state dnsEngineStateReceipt) error {
	data, err := encodeDNSEngineState(state)
	if err != nil {
		return err
	}
	before, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return err
	}
	return secureWriteConfigReplacingSnapshotWithOwner(
		before.Path,
		data,
		0o600,
		&before,
		serviceMutationRequiredOwnerUID,
		serviceMutationRequiredOwnerGID,
	)
}

type dnsEngineStateWriter func(dnsEngineStateReceipt) error
type dnsEngineStateReader func() (dnsEngineStateReceipt, bool, error)

func persistExactDNSEngineStateAt(
	state dnsEngineStateReceipt,
	write dnsEngineStateWriter,
	read dnsEngineStateReader,
) error {
	writeErr := write(state)
	actual, exists, readErr := read()
	if readErr != nil || !exists || actual != state {
		if readErr == nil {
			readErr = errors.New("DNS engine state did not persist exactly")
		}
		return errors.Join(writeErr, readErr)
	}
	// An atomic rename can be durable even when the final directory fsync or
	// return path reports an error. Exact readback is authoritative and avoids
	// rolling the live BIND pointer away from its matching durable receipt.
	return nil
}

func persistExactDNSEngineState(state dnsEngineStateReceipt) error {
	return persistExactDNSEngineStateAt(
		state, writeDNSEngineState, readExactDNSEngineState,
	)
}

func newHostBINDPublisher(
	ctx context.Context,
	layout bindHostLayout,
) (*binddns.Publisher, trackedBINDValidator, error) {
	if err := verifyHostBINDGenerationRoot(ctx, layout); err != nil {
		return nil, trackedBINDValidator{}, err
	}
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
		states[0].Installed, err = exactDNSReadinessPackagesInstalled(
			ctx, profile, layout.Packages,
		)
		if err != nil {
			return transport.DNSBackendReadinessResponse{}, err
		}
	}
	pdnsPackages := []string(nil)
	if service := core.GetManagedServiceByID("pdns"); service != nil {
		pdnsPackages = append(
			pdnsPackages,
			service.Packages[string(profile.PackageManager)]...,
		)
	}
	states[1].Installed, err = exactDNSReadinessPackagesInstalled(
		ctx, profile, pdnsPackages,
	)
	if err != nil {
		return transport.DNSBackendReadinessResponse{}, err
	}
	if err := verifyBINDReadinessUnitTopology(
		ctx, profile, systemctl, states[0].Installed,
	); err != nil {
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
		publisher, _, publisherErr := newHostBINDPublisher(ctx, layout)
		if publisherErr == nil {
			tree, treeErr := publisher.LoadCurrent()
			if treeErr == nil {
				receipt := tree.CurrentReceipt()
				managedEvidence := receipt.EngineEpoch == state.EngineEpoch &&
					receipt.Generation == state.Generation
				legacyOptions := isLegacyDNSEngineState(state) && receipt.Pairing != nil
				if configErr := verifyManagedBINDRuntimeConfigExact(
					ctx, layout, receipt, legacyOptions,
				); configErr != nil {
					managedEvidence = false
					log.Printf("BIND managed configuration proof failed: %v", configErr)
				}
				states[0].Managed = exactActiveDNSBackendManaged(
					states[0], managedEvidence,
					func() error { return verifyOnlyBINDActive(ctx, profile, systemctl) },
				)
				if states[0].Managed {
					states[0].PairReady, err = bindPrimaryPairReadyForState(
						ctx, layout.GenerationRoot, tree, state,
					)
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
			publisher, _, publisherErr := newHostBINDPublisher(ctx, layout)
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
			states[1].PairReady, err = powerDNSPrimaryPairReady(ctx, state)
			if err != nil {
				log.Printf("PowerDNS primary peer readiness proof failed: %v", err)
			}
		}
	}
	// What a takeover of this BIND would replace, reported as facts so the
	// preview can show the operator the difference before the operator agrees
	// to it (register R-042). Only for a BIND that is here and is not ours:
	// there is nothing to take over from a managed engine, and nothing to read
	// from a host that has no BIND. A probe failure is logged and reported as
	// nothing found, exactly like every other optional evidence here - it must
	// never make readiness itself unavailable.
	//
	// Bu BIND'in devralınmasının neyi değiştireceği, önizlemenin operatöre o
	// rıza göstermeden önce farkı gösterebilmesi için olgu olarak bildirilir
	// (defter R-042). Yalnız burada olan ve bizim olmayan bir BIND için:
	// yönetilen bir motordan devralınacak bir şey yoktur ve BIND'i olmayan bir
	// sunucudan okunacak bir şey yoktur. Bir yoklama hatası günlüğe yazılır ve
	// hiçbir şey bulunmamış gibi bildirilir; buradaki diğer her isteğe bağlı
	// kanıt gibi - hazırlığın kendisini asla erişilmez kılmamalıdır.
	if layoutErr == nil && states[0].Installed && !states[0].Managed {
		foreignOptions, foreignErr := reportForeignBINDOptions(layout)
		if foreignErr != nil {
			log.Printf("BIND foreign options probe failed: %v", foreignErr)
		} else {
			states[0].ForeignOptions = foreignOptions
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

type bindReadinessUnitProofOps struct {
	packageManager  hostplatform.PackageManager
	inspectStates   func() (bindInstallUnitState, bindInstallUnitState, error)
	verifySealed    func() error
	verifyPreEnable func() error
	verifyTopology  func() error
}

func verifyBINDReadinessUnitTopology(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	installed bool,
) error {
	if ctx == nil {
		return errors.New("BIND readiness unit proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	return verifyBINDReadinessUnitTopologyWithOps(installed, bindReadinessUnitProofOps{
		packageManager: profile.PackageManager,
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			return inspectBINDTargetStates(proofCtx, systemctl)
		},
		verifySealed: func() error {
			return verifyBINDSealedTargetNotServing(proofCtx, systemctl)
		},
		verifyPreEnable: func() error {
			return verifyBINDPreEnableIdentity(proofCtx, profile, systemctl)
		},
		verifyTopology: func() error {
			return verifyBINDUnitTopology(proofCtx, profile, systemctl)
		},
	})
}

func verifyBINDReadinessUnitTopologyWithOps(
	installed bool,
	ops bindReadinessUnitProofOps,
) error {
	if !installed {
		return nil
	}
	if ops.inspectStates == nil {
		return errors.New("invalid BIND readiness unit proof")
	}
	named, alias, err := ops.inspectStates()
	if err != nil {
		return err
	}
	if named.masked() || alias.masked() {
		sealed, err := classifyBINDTargetNotServingStates(named, alias)
		if err != nil {
			return err
		}
		if !sealed {
			return errors.New("BIND readiness mask state is not exactly sealed")
		}
		if ops.verifySealed == nil {
			return errors.New("BIND readiness sealed proof is unavailable")
		}
		return ops.verifySealed()
	}
	if named.activeState == "failed" || alias.activeState == "failed" {
		return errors.New("BIND readiness found a failed unit")
	}
	if exactStockDisabledBINDTarget(named, alias) {
		if ops.verifyPreEnable == nil {
			return errors.New("BIND readiness stock-unit proof is unavailable")
		}
		return ops.verifyPreEnable()
	}
	switch ops.packageManager {
	case hostplatform.PackageManagerAPT:
		if !exactLoadedUnmaskedBINDUnit(named) ||
			!exactLoadedUnmaskedBINDUnit(alias) {
			return errors.New("APT BIND readiness unit topology is mixed or incomplete")
		}
	case hostplatform.PackageManagerPacman:
		if !exactLoadedUnmaskedBINDUnit(named) ||
			!exactAbsentInactiveBINDUnit(alias) {
			return errors.New("pacman BIND readiness unit topology is mixed or incomplete")
		}
	default:
		return errors.New("BIND readiness unit topology is unsupported on this package manager")
	}
	if ops.verifyTopology == nil {
		return errors.New("BIND readiness runtime topology proof is unavailable")
	}
	return ops.verifyTopology()
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

func exactDNSReadinessPackagesInstalled(
	ctx context.Context,
	profile hostplatform.Profile,
	packages []string,
) (bool, error) {
	if len(packages) == 0 {
		return false, nil
	}
	for _, packageName := range packages {
		installed, err := exactDNSEnginePackageInstalled(
			ctx, profile, packageName,
		)
		if err != nil {
			return false, err
		}
		if !installed {
			return false, nil
		}
	}
	return true, nil
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
	publisher, _, err := newHostBINDPublisher(ctx, layout)
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
	legacyBINDState, err := bindStateTreePairContract(
		layout.GenerationRoot, state, current, false, false, false,
	)
	if err != nil {
		return "", err
	}
	if err := verifyManagedBINDRuntimeConfigExact(
		ctx, layout, receipt, legacyBINDState,
	); err != nil {
		return "", err
	}
	if legacyBINDState {
		evidence, primary, evidenceErr := bindPrimaryCatalogEvidence(current)
		if evidenceErr != nil || !primary {
			if evidenceErr == nil {
				evidenceErr = errors.New("legacy BIND primary evidence is unavailable")
			}
			return "", evidenceErr
		}
		if _, proofErr := verifyDNSLegacyPrimaryPairReadyAuthorityAt(
			ctx, evidence, probeDNSZoneSOA, probeDNSBoundCatalogAXFR,
		); proofErr != nil {
			return "", proofErr
		}
	}
	var legacyConfigMigration *bindConfigMutation
	if legacyBINDState {
		migration, err := prepareBINDConfigMutation(ctx, layout, "")
		if err != nil {
			return "", err
		}
		legacyConfigMigration = &migration
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
	nextState, err := bindStateForPublishedReceipt(state, generation.ReceiptValue)
	if err != nil {
		return "", err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return "", err
	}
	stateBefore, err := captureDNSEngineStateSnapshot(false)
	if err != nil {
		return "", err
	}
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if legacyConfigMigration != nil {
			if attempt == 1 {
				if err := legacyConfigMigration.apply(applyCtx); err != nil {
					return err
				}
			} else if err := legacyConfigMigration.restore(applyCtx); err != nil {
				return err
			}
		}
		if output, commandErr := runDNSSystemctl(applyCtx, systemctl, "reload", layout.Unit); commandErr != nil {
			return fmt.Errorf("reload BIND: %w: %s", commandErr, firstLine(string(output)))
		}
		if err := verifyOnlyBINDActive(applyCtx, profile, systemctl); err != nil {
			return err
		}
		currentTree, err := publisher.LoadCurrent()
		if err != nil {
			return err
		}
		return applyVerifiedBINDV3GenerationAt(
			applyCtx, attempt, currentTree, receipt, generation.ReceiptValue,
			func(verifyCtx context.Context) error {
				return verifyDNSZoneManifestAuthority(verifyCtx, []transport.DNSEngineSwitchZoneSnapshot{{
					Domain: commitment.Domain, DesiredGeneration: commitment.DesiredGeneration,
					Delete: commitment.Delete, ZoneType: commitment.ZoneType,
					Records: commitment.Records, ZoneQualifier: commitment.Qualifier,
				}})
			},
			func(verifyCtx context.Context, restored binddns.VerifiedTree, previous binddns.Receipt) error {
				return verifyRestoredBINDV3GenerationForState(
					verifyCtx, layout.GenerationRoot, restored, previous, state,
				)
			},
			func() error {
				if err := persistExactDNSEngineState(nextState); err != nil {
					return fmt.Errorf("publish BIND engine generation state: %w", err)
				}
				return nil
			},
			func() error { return restoreDNSEngineStateSnapshot(stateBefore) },
		)
	}
	recoverEmpty := func(recoveryCtx context.Context) error {
		var configErr error
		if legacyConfigMigration != nil {
			configErr = legacyConfigMigration.restore(recoveryCtx)
		}
		return errors.Join(
			stopBINDUnitsFailClosed(recoveryCtx, systemctl),
			restoreDNSEngineStateSnapshot(stateBefore),
			configErr,
		)
	}
	if legacyConfigMigration != nil {
		if err := verifyBINDConfigMutationPreimage(ctx, *legacyConfigMigration); err != nil {
			return "", err
		}
	} else if err := verifyManagedBINDRuntimeConfigExact(
		ctx, layout, receipt, legacyBINDState,
	); err != nil {
		return "", err
	}
	if err := publisher.Switch(ctx, generation.ID, apply, recoverEmpty); err != nil {
		return "", err
	}
	state = nextState
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
	if _, err := bindStateTreePairContract(
		layout.GenerationRoot, state, publishedTree, false, false, false,
	); err != nil {
		return "", dnsZoneV3RecoveryAmbiguous(err)
	}
	if err := completeManagedBINDV3PropagationForState(
		ctx, layout.GenerationRoot, publishedTree, commitment.Domain, state,
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
	publisher, _, err := newHostBINDPublisher(ctx, layout)
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
	legacyBINDState, err := bindStateTreePairContract(
		layout.GenerationRoot, state, tree, true, false, true,
	)
	if err != nil {
		return false, err
	}
	var recoveryConfigMigration *bindConfigMutation
	directionalTupleRepair := isLegacyDNSEngineState(state) &&
		!legacyBINDState && receipt.Pairing != nil &&
		receipt.Pairing.Role == binddns.PairRolePrimary
	if directionalTupleRepair {
		if currentErr := verifyManagedBINDRuntimeConfigExact(
			ctx, layout, receipt, false,
		); currentErr != nil {
			if legacyErr := verifyManagedBINDRuntimeConfigExact(
				ctx, layout, receipt, true,
			); legacyErr != nil {
				return false, errors.Join(currentErr, legacyErr)
			}
			migration, migrationErr := prepareBINDConfigMutation(ctx, layout, "")
			if migrationErr != nil {
				return false, migrationErr
			}
			recoveryConfigMigration = &migration
		}
	} else if legacyBINDState {
		if legacyErr := verifyManagedBINDRuntimeConfigExact(
			ctx, layout, receipt, true,
		); legacyErr != nil {
			if currentErr := verifyManagedBINDRuntimeConfigExact(
				ctx, layout, receipt, false,
			); currentErr != nil {
				return false, errors.Join(legacyErr, currentErr)
			}
			migration, migrationErr := prepareBINDLegacyConfigMutation(ctx, layout)
			if migrationErr != nil {
				return false, migrationErr
			}
			recoveryConfigMigration = &migration
		}
	} else if err := verifyManagedBINDRuntimeConfigExact(
		ctx, layout, receipt, false,
	); err != nil {
		return false, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return false, err
	}
	if legacyBINDState && recoveryConfigMigration != nil {
		if err := recoveryConfigMigration.apply(ctx); err != nil {
			return false, err
		}
		if output, commandErr := runDNSSystemctl(
			ctx, systemctl, "reload", layout.Unit,
		); commandErr != nil {
			return false, fmt.Errorf(
				"recover released BIND publication reload: %w: %s",
				commandErr, firstLine(string(output)),
			)
		}
		if err := verifyManagedBINDRuntimeConfigExact(
			ctx, layout, receipt, true,
		); err != nil {
			return false, err
		}
		if err := verifyOnlyBINDActive(ctx, profile, systemctl); err != nil {
			return false, err
		}
		expectedTree, err := expectedBINDTreeAuthorities(tree)
		if err != nil {
			return false, err
		}
		if err := verifyDNSZoneAuthorities(ctx, expectedTree); err != nil {
			return false, err
		}
		// The rollback hybrid is now repaired even when the restored old tree
		// does not carry the failed target request. Do not apply it again below.
		recoveryConfigMigration = nil
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
	nextState := state
	if legacyBINDState {
		nextState.Generation = receipt.Generation
	} else {
		nextState, err = bindStateForPublishedReceipt(state, receipt)
		if err != nil {
			return false, err
		}
	}
	if state != nextState || recoveryConfigMigration != nil {
		if recoveryConfigMigration != nil {
			if err := recoveryConfigMigration.apply(ctx); err != nil {
				return false, err
			}
		}
		if output, commandErr := runDNSSystemctl(ctx, systemctl, "reload", layout.Unit); commandErr != nil {
			return false, fmt.Errorf("recover BIND publication reload: %w: %s", commandErr, firstLine(string(output)))
		}
		if err := verifyManagedBINDRuntimeConfigExact(
			ctx, layout, receipt, legacyBINDState,
		); err != nil {
			return false, err
		}
	}
	if err := verifyOnlyBINDActive(ctx, profile, systemctl); err != nil {
		return false, err
	}
	if err := verifyDNSZoneAuthorities(ctx, []expectedDNSZoneAuthority{expected}); err != nil {
		return false, err
	}
	if state != nextState {
		if err := persistExactDNSEngineState(nextState); err != nil {
			return false, err
		}
		state = nextState
	}
	if err := completeManagedBINDV3PropagationForState(
		ctx, layout.GenerationRoot, tree, domain, state,
	); err != nil {
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
	// Reinstalling PowerDNS is not implemented and must not be attempted
	// through the BIND transaction below. Refusing here keeps the answer
	// legible instead of letting a PowerDNS manifest die somewhere inside a
	// BIND layout.
	//
	// PowerDNS'i yeniden kurmak uygulanmış değildir ve aşağıdaki BIND işlemi
	// üzerinden denenmemelidir. Burada reddetmek, bir PowerDNS bildirgesinin
	// bir BIND yerleşiminin içinde bir yerde ölmesine izin vermek yerine cevabı
	// okunur tutar.
	if manifest.Mode == transport.DNSEngineSwitchModeReinstall &&
		manifest.TargetEngine != transport.DNSEngineBIND {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("DNS engine reinstall is supported only for BIND")
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
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	// A takeover of a BIND that is answering right now is not this transaction
	// and must not borrow its proofs. It has its own file (register R-039):
	// it never stops or starts the unit, so proveBINDTargetNotServing and the
	// port-53 pre-mutation guard below are neither reached nor relaxed. Only
	// the panel's takeover manifest can select it, and only when the target is
	// actually serving; every other manifest stays here, where a running BIND
	// is refused exactly as it always was.
	//
	// Şu anda yanıt veren bir BIND'in devralınması bu işlem değildir ve onun
	// kanıtlarını ödünç almamalıdır. Kendi dosyası vardır (defter R-039):
	// birimi hiç durdurmaz ve başlatmaz, dolayısıyla aşağıdaki
	// proveBINDTargetNotServing ve 53 numaralı bağlantı noktası ön-mutasyon
	// koruması ne o yola girer ne gevşetilir. Onu yalnız panelin devralma
	// bildirgesi ve yalnız hedef gerçekten hizmet verirken seçebilir; diğer her
	// bildirge burada kalır, orada çalışan bir BIND her zamanki gibi
	// reddedilir.
	adopting, err := runningBINDAdoptionSelected(
		ctx, systemctl, manifest, stateExists,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if adopting {
		return adoptRunningBIND(ctx, profile, layout, systemctl, manifest, binding)
	}
	// The stopped half of the takeover runs this transaction unchanged. The one
	// thing it needs from it is permission to read the operator's own options
	// directives as part of what it is replacing, and that is decided here,
	// before any receipt of ours exists to confuse the question.
	//
	// Devralmanın durmuş yarısı bu işlemi değişmeden çalıştırır. Ondan
	// istediği tek şey, operatörün kendi seçenek direktiflerini değiştirdiğinin
	// parçası olarak okuma izniydir; buna, soruyu karıştıracak hiçbir makbuzumuz
	// daha yokken burada karar verilir.
	optionsAuthority, err := bindSwitchOptionsAuthority(manifest, stateExists)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
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
	targetInstallProof, err := proveBINDTargetNotServing(
		ctx, profile, systemctl,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if manifest.SourceEngine == transport.DNSEnginePowerDNS {
		if err := verifyUnsignedPowerDNSForManifest(ctx, manifest, state); err != nil {
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
	missing := make([]string, 0, len(layout.Packages))
	for _, packageName := range layout.Packages {
		installed, packageErr := exactDNSEnginePackageInstalled(
			ctx, profile, packageName,
		)
		if packageErr != nil {
			return transport.SwitchDNSEngineV1Response{}, packageErr
		}
		if !installed {
			missing = append(missing, packageName)
		}
	}
	// systemctl mask creates persistent links below /etc/systemd/system. Prove
	// that exact root-owned 0755 parent before publishing install ownership or
	// allowing any package/config mutation. This is deliberately read-only:
	// unexpected host metadata requires operator reconciliation, not chmod.
	if err := verifyBINDMaskParentMetadata(); err != nil {
		return transport.SwitchDNSEngineV1Response{}, fmt.Errorf(
			"preflight BIND mask parent: %w", err,
		)
	}
	if err := publishDNSEngineSourceOwnership(
		manifest, state, stateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	// Nothing to install: BIND's packages are already on this host. That is
	// still an event with provenance - this mutation takes them under
	// management - and finalization requires it to be recorded, exactly as an
	// installation is.
	//
	// Kurulacak bir şey yok: BIND'in paketleri bu sunucuda zaten var. Bu yine de
	// kökeni olan bir olaydır - bu mutasyon onları yönetimine alır - ve
	// sonlandırma bunun tıpkı bir kurulum gibi kaydedilmesini ister.
	if len(missing) == 0 {
		if err := assumeExistingDNSEnginePackageOwnership(
			transport.DNSEngineBIND, profile.PackageManager,
			layout.Packages, manifest, binding,
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
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
	// A reinstall stands where a first install stands: the ledger's engine is
	// not on this host, so nothing of ours is serving and no managed authority
	// exists to hand over. Both therefore take the stricter proof — prove the
	// public port is free before touching packages, and prove nothing managed
	// is serving before staging a generation — rather than the proof written
	// for a live source that is about to be replaced.
	//
	// Yeniden kurulum, ilk kurulumun durduğu yerde durur: defterin motoru bu
	// sunucuda yoktur, dolayısıyla bizim hiçbir şeyimiz hizmet vermiyordur ve
	// devredilecek yönetilen bir yetki de yoktur. İkisi de bu yüzden daha katı
	// kanıtı alır — paketlere dokunmadan önce genel bağlantı noktasının boş
	// olduğunu, bir nesli hazırlamadan önce yönetilen hiçbir şeyin hizmet
	// vermediğini kanıtla — değiştirilmek üzere olan canlı bir kaynak için
	// yazılmış kanıtı değil.
	noManagedAuthority := (!stateExists && manifest.SourceEngine == "") ||
		mutationpayload.ReinstallsActiveDNSEngine(manifest)
	if err := runVerifiedBINDTargetInstall(
		targetInstallProof,
		func() error {
			return runDNSPort53PreMutationGuard(
				ctx, noManagedAuthority, installMutation,
			)
		},
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	var publisher *binddns.Publisher
	var validator trackedBINDValidator
	var generation binddns.Generation
	if err := runBINDPostInstallContinuation(
		noManagedAuthority,
		bindPostInstallProofOps{
			verifyGeneric: func() error {
				return verifyBINDSealedTargetNotServing(ctx, systemctl)
			},
			verifyNoAuthority: func() error {
				return verifyBINDSealedTargetNotServingWithoutManagedAuthority(
					ctx, systemctl,
				)
			},
		},
		func() error {
			return runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error {
					if err := prepareHostBINDGenerationRoot(ctx, layout); err != nil {
						return err
					}
					var err error
					publisher, validator, err = newHostBINDPublisher(ctx, layout)
					if err != nil {
						return err
					}
					generation, err = publisher.StagePlan(ctx, plan)
					return err
				},
			)
		},
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	writeJournal := func(journal dnsEngineSwitchJournal) error {
		return writeDNSEngineSwitchJournalForFaultDriver(
			dnsEngineSwitchFaultDriverBIND, journal,
		)
	}
	if err := runDNSEngineSwitchPreIntentFaultHook(
		dnsEngineSwitchFaultDriverBIND, manifest, binding,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	transferPeer := ""
	if manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary {
		transferPeer = manifest.PeerIP
	}
	configs, err := prepareBINDConfigMutationWithAuthority(
		ctx, layout, transferPeer, optionsAuthority,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	stateBefore, err := captureDNSEngineStateSnapshot(true)
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
	if err := verifyBINDConfigMutationPreimage(ctx, configs); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := writeJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollbackAndJournal := func(rollbackCtx context.Context) error {
		return runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
			write: writeJournal,
			rollback: func() error {
				return rollbackBINDActivation(
					rollbackCtx, systemctl, configs, stateBefore, targetBefore, sourceBefore,
				)
			},
			verify: func() error {
				return verifyRestoredDNSSwitchSource(
					rollbackCtx, profile, systemctl, manifest, journal,
				)
			},
			remove: removeDNSEngineSwitchJournal,
		})
	}
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if attempt > 1 {
			return rollbackAndJournal(applyCtx)
		}
		if err := runBINDMutationWithMaskParentProof(
			verifyBINDMaskParentMetadata,
			func() error { return configs.apply(applyCtx) },
		); err != nil {
			return err
		}
		if _, err := runTrackedBINDValidation(
			applyCtx, validator.checkConf, "named-checkconf",
			"-z", layout.MainConfig,
		); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseTargetStaged
		if err := writeJournal(journal); err != nil {
			return err
		}
		if manifest.SourceEngine == transport.DNSEnginePowerDNS {
			var output []byte
			if err := runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error {
					var commandErr error
					output, commandErr = runDNSSystemctl(
						applyCtx, systemctl, "disable", "--now", "pdns.service",
					)
					return commandErr
				},
			); err != nil {
				return fmt.Errorf("stop source PowerDNS: %w: %s", err, firstLine(string(output)))
			}
		}
		journal.Phase = dnsSwitchPhaseSourceStopped
		if err := writeJournal(journal); err != nil {
			return err
		}
		if err := activateBINDTargetWithVerifiedIdentity(
			applyCtx, profile, systemctl, layout.Unit,
		); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseTargetStarted
		if err := writeJournal(journal); err != nil {
			return err
		}
		if err := verifyOnlyBINDActive(applyCtx, profile, systemctl); err != nil {
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
			Mode:   dnsEngineTenureModeForManifest(manifest),
			Engine: transport.DNSEngineBIND, EngineEpoch: manifest.TargetEpoch,
			Generation: generation.ID, PairRole: pairRoleForEngineState(manifest),
			PairLocalIP: manifest.LocalIP, PairPeerIP: manifest.PeerIP,
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
		if err := persistExactDNSEngineState(nextState); err != nil {
			return fmt.Errorf("publish active DNS engine state: %w", err)
		}
		journal.Phase = dnsSwitchPhaseTargetVerified
		if err := writeJournal(journal); err != nil {
			return err
		}
		return nil
	}
	recoverEmpty := func(recoveryCtx context.Context) error {
		return rollbackAndJournal(recoveryCtx)
	}
	if err := verifyBINDConfigMutationPreimage(ctx, configs); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := runBINDMutationWithMaskParentProof(
		verifyBINDMaskParentMetadata,
		func() error {
			return publisher.Switch(ctx, generation.ID, apply, recoverEmpty)
		},
	); err != nil {
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
	if err := writeJournal(journal); err != nil {
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
	publisher, _, err := newHostBINDPublisher(ctx, layout)
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
	legacy, err := bindStateTreePairContract(
		layout.GenerationRoot, state, tree, false, true, false,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyManagedBINDRuntimeConfigExact(ctx, layout, receipt, legacy); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyOnlyBINDActive(ctx, profile, systemctl); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyDNSZoneManifestAuthority(ctx, zones); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if legacy && receipt.Pairing != nil &&
		receipt.Pairing.Role == binddns.PairRolePrimary {
		ready, readyErr := bindPrimaryPairReadyForState(
			ctx, layout.GenerationRoot, tree, state,
		)
		if readyErr != nil || !ready {
			if readyErr == nil {
				readyErr = errors.New("legacy BIND primary pair is not ready")
			}
			return transport.SwitchDNSEngineV1Response{}, readyErr
		}
	} else if err := verifyBINDPairingAuthority(ctx, receipt); err != nil {
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
	_, err := proveDNSEngineSwitchSource(
		ctx, profile, manifest, state, stateExists,
	)
	return err
}

func proveDNSEngineSwitchSource(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
) (dnsEngineSwitchSourceProof, error) {
	proof := dnsEngineSwitchSourceProof{}
	err := verifyDNSEngineSwitchSourceRecordingProof(
		ctx, profile, manifest, state, stateExists, &proof,
	)
	return proof, err
}

func verifyDNSEngineSwitchSourceRecordingProof(
	ctx context.Context,
	profile hostplatform.Profile,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
	proof *dnsEngineSwitchSourceProof,
) error {
	if proof == nil {
		return errors.New("DNS engine source proof output is required")
	}
	*proof = dnsEngineSwitchSourceProof{}
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
	if manifest.Mode == transport.DNSEngineSwitchModeReinstall {
		return verifyDNSEngineReinstallSource(
			ctx, manifest, state, stateExists,
			bindUnit, bindAliasUnit, pdnsUnit,
		)
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
	sourceClass, err := classifyPDNSPairSecondarySource(
		manifest, stateExists, bindUnit.active(), bindAliasUnit.active(),
		pdnsUnit.active(),
	)
	if err != nil {
		return err
	}
	if sourceClass == pdnsPairSecondarySourceReconfigure {
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
		if err := verifyEmptyStandalonePDNSDatabase(ctx, pdnsDBPath()); err != nil {
			return err
		}
		proof.PDNSPairSecondaryReconfigure = true
		return nil
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
			if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
				return err
			}
			return verifyUnsignedPowerDNSForManifest(ctx, manifest, state)
		case transport.DNSEngineBIND:
			if !bindUnit.active() || pdnsUnit.active() ||
				(bindAliasUnit.LoadState == "not-found" && bindAliasUnit.active()) {
				return errors.New("BIND receipt does not match the active port-53 authority")
			}
			layout, err := bindLayout(profile)
			if err != nil {
				return err
			}
			publisher, _, err := newHostBINDPublisher(ctx, layout)
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
			if _, err := bindStateTreePairContract(
				layout.GenerationRoot, state, tree, false, true, false,
			); err != nil {
				return err
			}
			legacy := isLegacyDNSEngineState(state) && receipt.Pairing != nil
			if err := verifyManagedBINDRuntimeConfigExact(
				ctx, layout, receipt, legacy,
			); err != nil {
				return err
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
			return verifyOnlyBINDActive(ctx, profile, systemctl)
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

// dnsEngineTenureModeForManifest returns the mode a durable state receipt
// records for the tenure this manifest establishes. A reinstall re-establishes
// the tenure it repaired, so its receipt reads exactly like the one the host
// lost; every other mode records itself.
//
// dnsEngineTenureModeForManifest, bu bildirgenin kurduğu dönem için kalıcı
// durum makbuzunun kaydettiği kipi döndürür. Yeniden kurulum, onardığı dönemi
// yeniden kurar; bu yüzden makbuzu sunucunun kaybettiğiyle birebir aynı okunur.
// Diğer her kip kendini kaydeder.
func dnsEngineTenureModeForManifest(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) string {
	if manifest.Mode == transport.DNSEngineSwitchModeReinstall {
		return transport.DNSEngineSwitchModeSwitch
	}
	return manifest.Mode
}

// verifyDNSEngineReinstallSource proves the one host shape a reinstall may act
// on. Every clause is a way the operation could otherwise do harm.
//
// The receipt must exist and name this engine at this epoch: without it the
// panel would be installing an engine the host never agreed to run, which is a
// first install and has its own path. The ownership receipt must agree on
// engine and epoch: it is the proof that WE installed this engine, so putting
// it back is a repair rather than a takeover of somebody else's server. And
// nothing authoritative may be listening — no BIND, no PowerDNS, no other
// public port-53 process — because a reinstall neither stops a source nor
// arbitrates a conflict; if something is serving, this is not the absent-engine
// shape and the operator needs a different answer.
//
// It deliberately does not require the generations to match. A live BIND
// rewrites the state receipt's generation as zones are published while the
// ownership receipt keeps the generation of the tenure's first install, so on
// any host that ever synchronized a zone the two differ by construction.
//
// verifyDNSEngineReinstallSource, yeniden kurulumun üzerinde işlem
// yapabileceği tek sunucu biçimini kanıtlar. Her koşul, işlemin aksi hâlde
// zarar verebileceği bir yoldur.
//
// Makbuz var olmalı ve bu motoru bu çağda adlandırmalıdır: o olmadan panel,
// sunucunun çalıştırmayı hiç kabul etmediği bir motoru kuruyor olurdu; bu ilk
// kurulumdur ve kendi yolu vardır. Sahiplik makbuzu motor ve çağ üzerinde
// hemfikir olmalıdır: bu motoru BİZİM kurduğumuzun kanıtı odur; dolayısıyla onu
// geri koymak, başkasının sunucusunu ele geçirmek değil bir onarımdır. Ve yetki
// taşıyan hiçbir şey dinlemiyor olmamalıdır — ne BIND, ne PowerDNS, ne de başka
// bir genel 53 süreci — çünkü yeniden kurulum ne bir kaynağı durdurur ne de bir
// çakışmayı hakemler; bir şey hizmet veriyorsa bu, motoru olmayan biçim
// değildir ve operatörün başka bir cevaba ihtiyacı vardır.
//
// Nesillerin eşleşmesini bilerek istemez. Canlı bir BIND, bölgeler yayımlandıkça
// durum makbuzunun neslini yeniden yazar; sahiplik makbuzu ise dönemin ilk
// kurulumundaki nesli saklar. Bu yüzden bir kez bölge eşitlemiş her sunucuda
// ikisi yapı gereği farklıdır.
func verifyDNSEngineReinstallSource(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
	bindUnit, bindAliasUnit, pdnsUnit dnsUnitState,
) error {
	if !mutationpayload.ReinstallsActiveDNSEngine(manifest) ||
		manifest.TargetEngine != transport.DNSEngineBIND {
		return errors.New("DNS engine reinstall manifest is not the exact reinstall identity")
	}
	if !stateExists {
		return errors.New("DNS engine reinstall requires an active engine state receipt")
	}
	if err := validateDNSEngineState(state); err != nil {
		return fmt.Errorf("validate DNS engine reinstall source state: %w", err)
	}
	if state.Engine != manifest.TargetEngine ||
		state.EngineEpoch != manifest.TargetEpoch ||
		state.PairRole != "" || state.PairLocalIP != "" ||
		state.PairPeerIP != "" || state.PrimaryCatalogSerial != 0 {
		return errors.New("DNS engine reinstall does not match the active engine receipt")
	}
	ownership, ownershipExists, err := readDNSEngineOwnership(manifest.TargetEngine)
	if err != nil {
		return fmt.Errorf("read DNS engine reinstall ownership: %w", err)
	}
	if !ownershipExists || validateDNSEngineState(ownership) != nil ||
		ownership.Engine != state.Engine ||
		ownership.EngineEpoch != state.EngineEpoch {
		return errors.New(
			"DNS engine reinstall requires a panel ownership receipt at the active epoch",
		)
	}
	if bindUnit.active() || bindAliasUnit.active() || pdnsUnit.active() {
		return errors.New("DNS engine reinstall requires no running authoritative DNS engine")
	}
	conflict, err := dnsPort53ConflictCheck(ctx, false, false)
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("DNS engine reinstall found another public port-53 authority")
	}
	return nil
}

func prepareBINDConfigMutation(
	ctx context.Context,
	layout bindHostLayout,
	transferPeer string,
) (bindConfigMutation, error) {
	return prepareBINDConfigMutationWithAuthority(
		ctx, layout, transferPeer, bindOptionsExclusive,
	)
}

// prepareBINDAdoptionConfigMutation is the takeover's preparation: the same
// snapshot set, the same ownership policy, the same desired generation - and
// the server's own recursion, allow-recursion, allow-query-cache and
// allow-transfer read as part of what is being replaced rather than refused
// (register R-042). It is reachable only from a takeover the operator
// acknowledged.
//
// prepareBINDAdoptionConfigMutation, devralmanın hazırlığıdır: aynı anlık
// görüntü kümesi, aynı sahiplik politikası, aynı istenen nesil - ve sunucunun
// kendi recursion, allow-recursion, allow-query-cache ve allow-transfer
// direktifleri reddedilmek yerine değiştirilenin parçası olarak okunur (defter
// R-042). Yalnız operatörün onayladığı bir devralmadan ulaşılır.
func prepareBINDAdoptionConfigMutation(
	ctx context.Context,
	layout bindHostLayout,
	transferPeer string,
) (bindConfigMutation, error) {
	return prepareBINDConfigMutationWithAuthority(
		ctx, layout, transferPeer, bindOptionsTakeover,
	)
}

func prepareBINDConfigMutationWithAuthority(
	ctx context.Context,
	layout bindHostLayout,
	transferPeer string,
	authority bindOptionsAuthority,
) (bindConfigMutation, error) {
	policy, err := resolveBINDConfigOwnerPolicy(ctx, layout)
	if err != nil {
		return bindConfigMutation{}, err
	}
	captured, err := captureBINDConfigSnapshotSet(policy)
	if err != nil {
		return bindConfigMutation{}, err
	}
	capturedByPath := bindConfigSnapshotMap(captured)
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, transferPeer, authority,
		func(path string, mode os.FileMode, allowAbsent bool) (dnsFileSnapshot, error) {
			if allowAbsent {
				return dnsFileSnapshot{}, errors.New("BIND config snapshot cannot be absent")
			}
			snapshot, ok := capturedByPath[filepath.Clean(path)]
			if !ok || snapshot.Mode != uint32(mode.Perm()) {
				return dnsFileSnapshot{}, errors.New("BIND config snapshot set is incomplete")
			}
			snapshot.Data = append([]byte(nil), snapshot.Data...)
			return snapshot, nil
		},
	)
	if err != nil {
		return bindConfigMutation{}, err
	}
	if err := policy.validateSnapshots(mutation.originalSnapshots()); err != nil {
		return bindConfigMutation{}, err
	}
	mutation.ownerAware = true
	return mutation, nil
}

func prepareBINDConfigMutationWithSnapshotReader(
	layout bindHostLayout,
	transferPeer string,
	authority bindOptionsAuthority,
	readSnapshot bindConfigSnapshotReader,
) (bindConfigMutation, error) {
	if readSnapshot == nil {
		return bindConfigMutation{}, errors.New("BIND config snapshot reader is required")
	}
	paths := []string{layout.OptionsConfig, layout.AnchorConfig}
	sort.Strings(paths)
	if len(paths) == 2 && paths[0] == paths[1] {
		paths = paths[:1]
	}
	mutation := bindConfigMutation{
		paths: paths, original: make(map[string][]byte, len(paths)),
		desired:   make(map[string][]byte, len(paths)),
		snapshots: make(map[string]dnsFileSnapshot, len(paths)),
		layout:    layout,
	}
	for _, path := range paths {
		snapshot, err := readSnapshot(path, os.FileMode(bindVendorConfigMode(layout)), false)
		if err != nil {
			return bindConfigMutation{}, fmt.Errorf("read BIND configuration %s: %w", path, err)
		}
		data := snapshot.Data
		mutation.snapshots[path] = snapshot
		mutation.original[path] = append([]byte(nil), data...)
		content := string(data)
		if path == layout.OptionsConfig {
			// The takeover reads the server's own managed directives first, so
			// what it removes is captured with the value it had and the line it
			// was on, before the stock-package strip or the managed block can
			// change a single offset.
			//
			// Devralma önce sunucunun kendi yönetilen direktiflerini okur; böylece
			// kaldırdığı şey, stok paket temizliği ya da yönetilen blok tek bir
			// konumu değiştiremeden, sahip olduğu değer ve bulunduğu satırla
			// birlikte yakalanır.
			if authority == bindOptionsTakeover {
				var adopted []bindForeignOptionDirective
				content, adopted, err = adoptForeignBINDOptions(
					content, path, transferPeer,
				)
				if err != nil {
					return bindConfigMutation{}, fmt.Errorf(
						"prepare BIND authoritative options: %w", err,
					)
				}
				mutation.adopted = adopted
			}
			if bindLayoutIsPacman(layout) {
				content, err = stripStockPacmanBINDOptionDirectives(content)
				if err != nil {
					return bindConfigMutation{}, fmt.Errorf("prepare BIND authoritative options: %w", err)
				}
			}
			content, err = managedBINDOptions(content, transferPeer)
			if err != nil {
				return bindConfigMutation{}, bindManagedOptionsRefusal(
					path, string(data), transferPeer, err,
				)
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

func prepareBINDLegacyConfigMutation(
	ctx context.Context,
	layout bindHostLayout,
) (bindConfigMutation, error) {
	mutation, err := prepareBINDConfigMutation(ctx, layout, "")
	if err != nil {
		return bindConfigMutation{}, err
	}
	return prepareBINDLegacyConfigMutationFromPrepared(layout, mutation)
}

func prepareBINDLegacyConfigMutationWithSnapshotReader(
	layout bindHostLayout,
	readSnapshot bindConfigSnapshotReader,
) (bindConfigMutation, error) {
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsExclusive, readSnapshot,
	)
	if err != nil {
		return bindConfigMutation{}, err
	}
	return prepareBINDLegacyConfigMutationFromPrepared(layout, mutation)
}

func prepareBINDLegacyConfigMutationFromPrepared(
	layout bindHostLayout,
	mutation bindConfigMutation,
) (bindConfigMutation, error) {
	for _, path := range mutation.paths {
		if path != layout.OptionsConfig {
			continue
		}
		legacy, legacyErr := managedBINDLegacyOptions(
			string(mutation.original[path]),
		)
		if legacyErr != nil {
			return bindConfigMutation{}, fmt.Errorf(
				"prepare released BIND options: %w", legacyErr,
			)
		}
		mutation.desired[path] = []byte(legacy)
	}
	return mutation, nil
}

func verifyManagedBINDConfigExact(
	ctx context.Context,
	layout bindHostLayout,
	transferPeer string,
	requireLegacyOptions bool,
) error {
	mutation, err := prepareBINDConfigMutation(ctx, layout, transferPeer)
	if err != nil {
		return err
	}
	return verifyPreparedManagedBINDConfigExact(mutation, layout, requireLegacyOptions)
}

func verifyManagedBINDConfigExactWithSnapshotReader(
	layout bindHostLayout,
	transferPeer string,
	requireLegacyOptions bool,
	readSnapshot bindConfigSnapshotReader,
) error {
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, transferPeer, bindOptionsExclusive, readSnapshot,
	)
	if err != nil {
		return err
	}
	return verifyPreparedManagedBINDConfigExact(mutation, layout, requireLegacyOptions)
}

func verifyPreparedManagedBINDConfigExact(
	mutation bindConfigMutation,
	layout bindHostLayout,
	requireLegacyOptions bool,
) error {
	for _, path := range mutation.paths {
		if requireLegacyOptions && path == layout.OptionsConfig {
			if !exactLegacyManagedBINDOptions(string(mutation.original[path])) {
				return errors.New("legacy BIND options are not the exact released policy")
			}
			if path == layout.AnchorConfig {
				includePath := filepath.ToSlash(filepath.Join(
					layout.GenerationRoot, "current", "zones.conf",
				))
				anchor, anchorErr := managedBINDZoneInclude(
					string(mutation.original[path]), includePath,
				)
				if anchorErr != nil || anchor != string(mutation.original[path]) {
					return errors.New("legacy BIND options do not retain the exact managed zone include")
				}
			}
			continue
		}
		if bytes.Equal(mutation.original[path], mutation.desired[path]) {
			continue
		}
		return errors.New("managed BIND configuration differs from the active pair policy")
	}
	return nil
}

func verifyManagedBINDRuntimeConfigExact(
	ctx context.Context,
	layout bindHostLayout,
	receipt binddns.Receipt,
	allowLegacyOptions bool,
) error {
	transferPeer := ""
	if receipt.Pairing != nil && receipt.Pairing.Role == binddns.PairRoleSecondary {
		transferPeer = receipt.Pairing.PeerIP
	}
	return verifyManagedBINDConfigExact(
		ctx, layout, transferPeer, allowLegacyOptions,
	)
}

func verifyManagedBINDRuntimeConfigExactWithSnapshotReader(
	layout bindHostLayout,
	receipt binddns.Receipt,
	allowLegacyOptions bool,
	readSnapshot bindConfigSnapshotReader,
) error {
	transferPeer := ""
	if receipt.Pairing != nil && receipt.Pairing.Role == binddns.PairRoleSecondary {
		transferPeer = receipt.Pairing.PeerIP
	}
	return verifyManagedBINDConfigExactWithSnapshotReader(
		layout, transferPeer, allowLegacyOptions, readSnapshot,
	)
}

func (mutation bindConfigMutation) apply(ctx context.Context) error {
	if mutation.ownerAware {
		return mutation.applyOwnerAware(ctx)
	}
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

func (mutation bindConfigMutation) restore(ctx context.Context) error {
	if mutation.ownerAware {
		return mutation.restoreOwnerAware(ctx)
	}
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
	if ctx == nil {
		return dnsUnitState{}, errors.New("inspect DNS unit requires a context")
	}
	inspectCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	state, err := dnsSystemdStateGuard(systemctl).inspect(inspectCtx, unit)
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
	if ctx == nil {
		return errors.New("rollback BIND activation requires a bounded context")
	}
	return rollbackBINDActivationWithOps(ctx, bindRollbackActivationOps{
		restoreTarget: func(commandCtx context.Context) error {
			return restoreDNSUnitStates(
				commandCtx, systemctl, targetBefore, true,
			)
		},
		restoreConfigs: func() error {
			return runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error { return configs.restore(ctx) },
			)
		},
		restoreState: func() error {
			return restoreDNSEngineStateSnapshot(stateBefore)
		},
		restoreSource: func(commandCtx context.Context) error {
			return restoreDNSUnitStates(
				commandCtx, systemctl, sourceBefore, false,
			)
		},
	})
}

type bindRollbackActivationOps struct {
	restoreTarget  func(context.Context) error
	restoreConfigs func() error
	restoreState   func() error
	restoreSource  func(context.Context) error
}

func rollbackBINDActivationWithOps(
	ctx context.Context,
	ops bindRollbackActivationOps,
) error {
	if ctx == nil || ops.restoreTarget == nil ||
		ops.restoreConfigs == nil || ops.restoreState == nil ||
		ops.restoreSource == nil {
		return errors.New("invalid BIND activation rollback operations")
	}
	return errors.Join(
		ops.restoreTarget(ctx),
		ops.restoreConfigs(),
		ops.restoreState(),
		ops.restoreSource(ctx),
	)
}

func verifyOnlyBINDActive(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	if ctx == nil {
		return errors.New("verify active BIND requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	return verifyOnlyBINDActiveWithOps(profile, bindActiveProofOps{
		inspectTopology: func() (bindRuntimeTopologySnapshot, error) {
			return inspectVerifiedBINDRuntimeTopology(proofCtx, profile, systemctl)
		},
		inspectPowerDNS: func() (bindInstallUnitState, error) {
			return dnsSystemdStateGuard(systemctl).inspect(proofCtx, "pdns.service")
		},
		inspectListeners: func() (string, error) {
			ss, err := firstTrustedExecutable(
				[]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss",
			)
			if err != nil {
				return "", err
			}
			output, err := serviceMutationCommand(
				proofCtx, ss, "-H", "-lntup", "sport = :53",
			).CombinedOutputLimited(64 << 10)
			if err != nil {
				return "", fmt.Errorf(
					"inspect active DNS listeners: %w: %s",
					err, firstLine(string(output)),
				)
			}
			return string(output), nil
		},
	})
}

type bindActiveProofOps struct {
	inspectTopology  func() (bindRuntimeTopologySnapshot, error)
	inspectPowerDNS  func() (bindInstallUnitState, error)
	inspectListeners func() (string, error)
}

func verifyOnlyBINDActiveWithOps(
	profile hostplatform.Profile,
	ops bindActiveProofOps,
) error {
	if ops.inspectTopology == nil || ops.inspectPowerDNS == nil ||
		ops.inspectListeners == nil {
		return errors.New("invalid active BIND proof operations")
	}
	topologyBefore, err := ops.inspectTopology()
	if err != nil {
		return err
	}
	pdnsBefore, err := ops.inspectPowerDNS()
	if err != nil {
		return err
	}
	if err := verifyExactActiveBINDUnitStates(
		profile, topologyBefore.namedState, topologyBefore.aliasState, pdnsBefore,
	); err != nil {
		return err
	}
	listenersBefore, err := ops.inspectListeners()
	if err != nil {
		return err
	}
	canonicalBefore, err := canonicalBINDPublicListeners(
		listenersBefore, topologyBefore.namedProcesses.MainPID,
	)
	if err != nil {
		return err
	}
	listenersAfter, err := ops.inspectListeners()
	if err != nil {
		return err
	}
	canonicalAfter, err := canonicalBINDPublicListeners(
		listenersAfter, topologyBefore.namedProcesses.MainPID,
	)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonicalAfter, canonicalBefore) {
		return errors.New("BIND public listener snapshot changed during verification")
	}
	topologyAfter, err := ops.inspectTopology()
	if err != nil {
		return err
	}
	pdnsAfter, err := ops.inspectPowerDNS()
	if err != nil {
		return err
	}
	if err := verifyExactActiveBINDUnitStates(
		profile, topologyAfter.namedState, topologyAfter.aliasState, pdnsAfter,
	); err != nil {
		return err
	}
	if !reflect.DeepEqual(topologyAfter, topologyBefore) || pdnsAfter != pdnsBefore {
		return errors.New("DNS unit identity or state changed during active BIND verification")
	}
	return nil
}

func verifyExactActiveBINDUnitStates(
	profile hostplatform.Profile,
	named, alias, pdns bindInstallUnitState,
) error {
	exactNamed := named.loadState == "loaded" &&
		named.activeState == "active" &&
		named.unitFileState == "enabled"
	if !exactNamed {
		return errors.New("named.service is not exactly loaded, active, and enabled")
	}
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		if alias.loadState != "loaded" ||
			alias.activeState != "active" ||
			alias.unitFileState != "enabled" {
			return errors.New("bind9.service is not the exact active APT alias")
		}
	case hostplatform.PackageManagerPacman:
		if !exactAbsentInactiveBINDUnit(alias) {
			return errors.New("bind9.service unexpectedly exists on active pacman BIND")
		}
	default:
		return errors.New("active BIND proof is unsupported on this package manager")
	}
	// A masked PowerDNS counts as stopped, and the product is what masked it.
	//
	// A BIND-to-PowerDNS switch installs the PowerDNS packages under a
	// persistent mask (dns_engine_pdns_install.go) so the package manager
	// cannot start the target behind the transaction's back, and only the
	// activation step may unmask it. This proof of the active BIND source runs
	// after that install, so on every host where PowerDNS had to be installed
	// it met exactly the state the product had just created - loadState
	// "masked", neither "not-found" nor "loaded"+"disabled" - and refused its
	// own source (S-7 Boston negative, register R-028). This is the mirror
	// image of R-019's second cause on the BIND side, and the relaxation is
	// the same one: masked is stronger than disabled, and it is accepted only
	// together with the inactive requirement the other branches carry.
	//
	// Maskeli PowerDNS durdurulmuş sayılır; onu maskeleyen de bu üründür.
	//
	// BIND'dan PowerDNS'e geçiş, paket yöneticisi hedefi işlemin arkasından
	// başlatmasın diye PowerDNS paketlerini kalıcı bir maske altında kurar
	// (dns_engine_pdns_install.go) ve maskeyi yalnız etkinleştirme adımı
	// kaldırabilir. Etkin BIND kaynağının bu kanıtı o kurulumdan sonra koşar;
	// dolayısıyla PowerDNS'in kurulması gereken her sunucuda tam da ürünün az
	// önce yarattığı durumla - loadState "masked", ne "not-found" ne
	// "loaded"+"disabled" - karşılaştı ve kendi kaynağını reddetti (S-7 Boston
	// negatifi, defter R-028). Bu, R-019'un ikinci sebebinin BIND tarafındaki
	// ayna görüntüsüdür ve gevşetme aynıdır: maskeli olmak devre dışı olmaktan
	// güçlüdür ve yalnız diğer dalların taşıdığı "inactive" şartıyla birlikte
	// kabul edilir.
	exactPowerDNSInactive := exactAbsentInactiveBINDUnit(pdns) ||
		(pdns.loadState == "loaded" &&
			pdns.activeState == "inactive" &&
			pdns.unitFileState == "disabled") ||
		(pdns.masked() && pdns.activeState == "inactive")
	if !exactPowerDNSInactive {
		return errors.New("pdns.service is not exactly absent or loaded, inactive, and disabled")
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
	if state.PairRole == "" && state.PrimaryCatalogSerial == 0 {
		evidence, primary, evidenceErr := managedPDNSPrimaryCatalogEvidenceForState(
			ctx, state,
		)
		if evidenceErr != nil {
			return "", evidenceErr
		}
		if primary {
			if _, proofErr := verifyDNSLegacyPrimaryPairReadyAuthorityAt(
				ctx, evidence, probeDNSZoneSOA, probeDNSBoundCatalogAXFR,
			); proofErr != nil {
				return "", proofErr
			}
		}
	}
	if err := applyPDNSV3ZoneDatabaseForState(
		ctx, pdnsDBPath(), commitment, binding, state,
	); err != nil {
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
	propagation, err := prepareManagedPDNSV3Propagation(ctx, zone, state)
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
		ctx, pdnsDBPath(), state, domain, qualifier, binding,
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
	propagation, err := prepareManagedPDNSV3Propagation(ctx, snapshot, state)
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
	if ctx == nil {
		return errors.New("verify active PowerDNS requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return err
	}
	ss, err := firstTrustedExecutable(
		[]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss",
	)
	if err != nil {
		return err
	}
	return verifyOnlyPDNSActiveWithOps(profile, pdnsActiveProofOps{
		inspectTopology: func() (pdnsRuntimeTopologySnapshot, error) {
			return inspectVerifiedPDNSRuntimeTopology(
				proofCtx, profile, systemctl,
			)
		},
		inspectListeners: func() (string, error) {
			output, commandErr := serviceMutationCommand(
				proofCtx, ss, "-H", "-lntup", "sport = :53",
			).CombinedOutputLimited(64 << 10)
			if commandErr != nil {
				return "", fmt.Errorf(
					"inspect active DNS listeners: %w: %s",
					commandErr, firstLine(string(output)),
				)
			}
			return string(output), nil
		},
	})
}

type pdnsRuntimeTopologySnapshot struct {
	namedState, aliasState, pdnsState bindInstallUnitState
	namedProcesses, aliasProcesses    dnsUnitProcesses
	pdnsProcesses                     dnsUnitProcesses
	pdnsIdentity                      dnsUnitIdentity
	vendorUnit                        bindSecureFileIdentity
}

type pdnsRuntimeTopologyOps struct {
	inspectStates    func() (bindInstallUnitState, bindInstallUnitState, bindInstallUnitState, error)
	inspectIdentity  func() (dnsUnitIdentity, error)
	inspectVendor    func() (bindSecureFileIdentity, error)
	inspectProcesses func(string) (dnsUnitProcesses, error)
}

func inspectVerifiedPDNSRuntimeTopology(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) (pdnsRuntimeTopologySnapshot, error) {
	if ctx == nil {
		return pdnsRuntimeTopologySnapshot{},
			errors.New("PowerDNS topology proof requires a context")
	}
	guard := dnsSystemdStateGuard(systemctl)
	return inspectVerifiedPDNSRuntimeTopologyWithOps(profile, pdnsRuntimeTopologyOps{
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, bindInstallUnitState, error) {
			named, err := guard.inspect(ctx, "named.service")
			if err != nil {
				return bindInstallUnitState{}, bindInstallUnitState{}, bindInstallUnitState{}, err
			}
			alias, err := guard.inspect(ctx, "bind9.service")
			if err != nil {
				return bindInstallUnitState{}, bindInstallUnitState{}, bindInstallUnitState{}, err
			}
			pdns, err := guard.inspect(ctx, "pdns.service")
			return named, alias, pdns, err
		},
		inspectIdentity: func() (dnsUnitIdentity, error) {
			return inspectDNSUnitIdentity(ctx, systemctl, "pdns.service")
		},
		inspectVendor: func() (bindSecureFileIdentity, error) {
			return inspectHostPDNSVendorUnit(ctx, profile)
		},
		inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
			return inspectDNSUnitProcesses(ctx, systemctl, unit)
		},
	})
}

func inspectVerifiedPDNSRuntimeTopologyWithOps(
	profile hostplatform.Profile,
	ops pdnsRuntimeTopologyOps,
) (pdnsRuntimeTopologySnapshot, error) {
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return pdnsRuntimeTopologySnapshot{}, err
	}
	if ops.inspectStates == nil || ops.inspectIdentity == nil ||
		ops.inspectVendor == nil || ops.inspectProcesses == nil {
		return pdnsRuntimeTopologySnapshot{},
			errors.New("invalid PowerDNS topology proof operations")
	}
	capture := func() (pdnsRuntimeTopologySnapshot, error) {
		namedState, aliasState, pdnsState, err := ops.inspectStates()
		if err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		if err := verifyExactPDNSRuntimeUnitStates(
			namedState, aliasState, pdnsState,
		); err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		identity, err := ops.inspectIdentity()
		if err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		if err := validatePDNSVendorUnitIdentity(identity); err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		vendor, err := ops.inspectVendor()
		if err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		namedProcesses, err := ops.inspectProcesses("named.service")
		if err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		if err := verifyDNSUnitProcessesStopped(namedProcesses); err != nil {
			return pdnsRuntimeTopologySnapshot{}, fmt.Errorf("named.service is not stopped: %w", err)
		}
		aliasProcesses, err := ops.inspectProcesses("bind9.service")
		if err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		if err := verifyDNSUnitProcessesStopped(aliasProcesses); err != nil {
			return pdnsRuntimeTopologySnapshot{}, fmt.Errorf("bind9.service is not stopped: %w", err)
		}
		pdnsProcesses, err := ops.inspectProcesses("pdns.service")
		if err != nil {
			return pdnsRuntimeTopologySnapshot{}, err
		}
		if pdnsProcesses.MainPID == 0 || pdnsProcesses.ControlPID != 0 ||
			pdnsProcesses.SubState != "running" {
			return pdnsRuntimeTopologySnapshot{},
				errors.New("pdns.service lacks an exact running process identity")
		}
		return pdnsRuntimeTopologySnapshot{
			namedState: namedState, aliasState: aliasState, pdnsState: pdnsState,
			namedProcesses: namedProcesses, aliasProcesses: aliasProcesses,
			pdnsProcesses: pdnsProcesses, pdnsIdentity: identity,
			vendorUnit: vendor,
		}, nil
	}
	before, err := capture()
	if err != nil {
		return pdnsRuntimeTopologySnapshot{}, err
	}
	after, err := capture()
	if err != nil {
		return pdnsRuntimeTopologySnapshot{}, err
	}
	if !reflect.DeepEqual(after, before) {
		return pdnsRuntimeTopologySnapshot{},
			errors.New("PowerDNS runtime topology changed during exact verification")
	}
	return after, nil
}

func verifyExactPDNSRuntimeUnitStates(
	named, alias, pdns bindInstallUnitState,
) error {
	// A masked BIND unit counts as stopped, and this product is the thing that
	// masked it.
	//
	// bind_install_guard.go masks named.service and bind9.service so a package
	// manager cannot start BIND behind our back, and there is a whole proof
	// (verifyBINDPersistentMaskFiles) that those masks point at /dev/null. A
	// masked unit reports loadState "masked" and unitFileState "masked" or
	// "masked-runtime", so it matched neither branch below: not "not-found",
	// not "loaded"+"disabled". The proof therefore refused the exact state the
	// product creates for itself, and a PowerDNS-to-BIND switch on a host where
	// BIND had ever been installed could not establish its own source
	// (risk R-019).
	//
	// Masked is stronger than disabled, not weaker: a disabled unit can still
	// be started by name or pulled in by a dependency, while a masked one
	// cannot be started at all. It is accepted only together with the same
	// inactive requirement the other branches carry.
	//
	// Maskelenmiş bir BIND birimi durdurulmuş sayılır ve onu maskeleyen zaten
	// bu üründür.
	//
	// bind_install_guard.go, bir paket yöneticisi arkamızdan BIND'ı
	// başlatmasın diye named.service ve bind9.service birimlerini maskeler; o
	// maskelerin /dev/null'a baktığını kanıtlayan ayrı bir mekanizma da vardır
	// (verifyBINDPersistentMaskFiles). Maskeli bir birim loadState "masked" ve
	// unitFileState "masked" ya da "masked-runtime" bildirir; dolayısıyla
	// aşağıdaki iki dalın hiçbirine uymuyordu: ne "not-found" ne
	// "loaded"+"disabled". Kanıt böylece ürünün kendisi için yarattığı durumu
	// reddediyordu ve BIND'ın bir kez kurulmuş olduğu bir sunucuda
	// PowerDNS'ten BIND'a geçiş kendi kaynağını kuramıyordu (risk R-019).
	//
	// Maskeli olmak devre dışı olmaktan zayıf değil güçlüdür: devre dışı bir
	// birim adıyla başlatılabilir ya da bir bağımlılıkla çekilebilir, maskeli
	// bir birim hiç başlatılamaz. Yalnızca diğer dalların taşıdığı aynı
	// "inactive" şartıyla birlikte kabul edilir.
	exactStoppedBIND := func(state bindInstallUnitState) bool {
		return exactAbsentInactiveBINDUnit(state) ||
			(state.loadState == "loaded" && state.activeState == "inactive" &&
				state.unitFileState == "disabled") ||
			(state.masked() && state.activeState == "inactive")
	}
	if !exactStoppedBIND(named) || !exactStoppedBIND(alias) {
		return errors.New("BIND is not exactly absent or loaded, inactive, and disabled")
	}
	if pdns.loadState != "loaded" || pdns.activeState != "active" ||
		pdns.unitFileState != "enabled" {
		return errors.New("pdns.service is not exactly loaded, active, and enabled")
	}
	return nil
}

type pdnsActiveProofOps struct {
	inspectTopology  func() (pdnsRuntimeTopologySnapshot, error)
	inspectListeners func() (string, error)
}

func verifyOnlyPDNSActiveWithOps(
	profile hostplatform.Profile,
	ops pdnsActiveProofOps,
) error {
	if err := certifyAPTPDNSCapabilities(profile); err != nil {
		return err
	}
	if ops.inspectTopology == nil || ops.inspectListeners == nil {
		return errors.New("invalid active PowerDNS proof operations")
	}
	topologyBefore, err := ops.inspectTopology()
	if err != nil {
		return err
	}
	listenersBefore, err := ops.inspectListeners()
	if err != nil {
		return err
	}
	canonicalBefore, err := canonicalDNSAuthorityPublicListeners(
		listenersBefore, "pdns_server", topologyBefore.pdnsProcesses.MainPID,
	)
	if err != nil {
		return err
	}
	listenersAfter, err := ops.inspectListeners()
	if err != nil {
		return err
	}
	canonicalAfter, err := canonicalDNSAuthorityPublicListeners(
		listenersAfter, "pdns_server", topologyBefore.pdnsProcesses.MainPID,
	)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonicalAfter, canonicalBefore) {
		return errors.New("PowerDNS public listener snapshot changed during verification")
	}
	topologyAfter, err := ops.inspectTopology()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(topologyAfter, topologyBefore) {
		return errors.New("PowerDNS unit identity or state changed during active verification")
	}
	return nil
}

func verifyPDNSPublicListeners(output string, expectedMainPID uint64) error {
	_, err := canonicalDNSAuthorityPublicListeners(
		output, "pdns_server", expectedMainPID,
	)
	return err
}

func inspectDNSUnitIdentity(ctx context.Context, systemctl, unit string) (dnsUnitIdentity, error) {
	return inspectDNSUnitIdentityWithRunner(ctx, systemctl, unit, runDNSSystemctl)
}

func inspectDNSUnitIdentityWithRunner(
	ctx context.Context,
	systemctl string,
	unit string,
	runner func(context.Context, string, ...string) ([]byte, error),
) (dnsUnitIdentity, error) {
	if ctx == nil || systemctl == "" || unit == "" || runner == nil {
		return dnsUnitIdentity{}, errors.New("invalid DNS unit identity inspection")
	}
	inspectionCtx, cancel := context.WithTimeout(ctx, bindIdentityInspectionTimeout)
	defer cancel()
	output, err := runner(
		inspectionCtx, systemctl, "show", unit,
		"--property=Id,Names,FragmentPath,DropInPaths,SourcePath,Transient,ExecStart",
		"--no-pager",
	)
	if err != nil {
		return dnsUnitIdentity{}, fmt.Errorf("inspect DNS unit identity %s: %w: %s", unit, err, firstLine(string(output)))
	}
	identity, parseErr := parseDNSUnitIdentity(string(output))
	if parseErr != nil {
		return dnsUnitIdentity{}, fmt.Errorf("inspect DNS unit identity %s: %w", unit, parseErr)
	}
	return identity, nil
}

func parseDNSUnitIdentity(output string) (dnsUnitIdentity, error) {
	const expectedProperties = 7
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return dnsUnitIdentity{}, errors.New("systemctl returned a malformed DNS unit identity")
		}
		switch key {
		case "Id", "Names", "FragmentPath", "DropInPaths", "SourcePath", "Transient", "ExecStart":
		default:
			return dnsUnitIdentity{}, errors.New("systemctl returned an unexpected DNS unit identity property")
		}
		if _, duplicate := values[key]; duplicate {
			return dnsUnitIdentity{}, errors.New("systemctl returned an ambiguous DNS unit identity")
		}
		values[key] = value
	}
	if len(values) != expectedProperties {
		return dnsUnitIdentity{}, errors.New("systemctl returned incomplete DNS unit identity")
	}
	parseNames := func(property string, allowEmpty bool) ([]string, error) {
		raw := values[property]
		if raw == "" && allowEmpty {
			return nil, nil
		}
		fields := strings.Fields(raw)
		if len(fields) == 0 || strings.Join(fields, " ") != raw {
			return nil, fmt.Errorf("systemctl returned non-canonical %s", property)
		}
		sort.Strings(fields)
		for index := 1; index < len(fields); index++ {
			if fields[index] == fields[index-1] {
				return nil, fmt.Errorf("systemctl returned duplicate %s", property)
			}
		}
		return fields, nil
	}
	names, err := parseNames("Names", false)
	if err != nil {
		return dnsUnitIdentity{}, err
	}
	dropIns, err := parseNames("DropInPaths", true)
	if err != nil {
		return dnsUnitIdentity{}, err
	}
	execPath, execArgv, err := parseSystemdExecStart(values["ExecStart"])
	if err != nil {
		return dnsUnitIdentity{}, err
	}
	if values["Id"] == "" || values["FragmentPath"] == "" ||
		values["Transient"] == "" {
		return dnsUnitIdentity{}, errors.New("systemctl returned an empty DNS unit identity field")
	}
	return dnsUnitIdentity{
		ID: values["Id"], Names: names, FragmentPath: values["FragmentPath"],
		DropInPaths: dropIns, SourcePath: values["SourcePath"],
		Transient: values["Transient"], ExecStartPath: execPath,
		ExecStartArgv: execArgv,
	}, nil
}

func parseSystemdExecStart(value string) (string, string, error) {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") ||
		strings.Count(value, "{") != 1 || strings.Count(value, "}") != 1 ||
		!strings.HasPrefix(value, "{ ") || !strings.HasSuffix(value, " }") {
		return "", "", errors.New("systemctl returned a non-canonical ExecStart")
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(value, "{ "), " }")
	parts := strings.Split(inner, " ; ")
	fields := map[string]string{}
	for _, part := range parts {
		key, candidate, found := strings.Cut(part, "=")
		if !found || key == "" || candidate == "" {
			return "", "", errors.New("systemctl returned a malformed ExecStart")
		}
		switch key {
		case "path", "argv[]", "ignore_errors":
			if _, duplicate := fields[key]; duplicate {
				return "", "", errors.New("systemctl returned an ambiguous ExecStart")
			}
			fields[key] = candidate
		}
	}
	if len(fields) != 3 || fields["ignore_errors"] != "no" {
		return "", "", errors.New("systemctl returned an unsafe ExecStart")
	}
	executable := fields["path"]
	if !path.IsAbs(executable) || path.Clean(executable) != executable ||
		(fields["argv[]"] != executable &&
			!strings.HasPrefix(fields["argv[]"], executable+" ")) {
		return "", "", errors.New("systemctl returned a non-canonical ExecStart command")
	}
	return executable, fields["argv[]"], nil
}

type dnsUnitProcesses struct {
	MainPID    uint64
	ControlPID uint64
	SubState   string
}

func inspectDNSUnitProcesses(ctx context.Context, systemctl, unit string) (dnsUnitProcesses, error) {
	return inspectDNSUnitProcessesWithRunner(ctx, systemctl, unit, runDNSSystemctl)
}

func inspectDNSUnitProcessesWithRunner(
	ctx context.Context,
	systemctl string,
	unit string,
	runner func(context.Context, string, ...string) ([]byte, error),
) (dnsUnitProcesses, error) {
	if ctx == nil || runner == nil {
		return dnsUnitProcesses{}, errors.New("invalid DNS unit process inspection")
	}
	output, err := runner(
		ctx, systemctl, "show", unit,
		"--property=MainPID,ControlPID,SubState", "--no-pager",
	)
	if err != nil {
		return dnsUnitProcesses{}, fmt.Errorf("inspect DNS unit processes %s: %w: %s", unit, err, firstLine(string(output)))
	}
	processes, err := parseDNSUnitProcesses(string(output))
	if err != nil {
		return dnsUnitProcesses{}, fmt.Errorf("inspect DNS unit processes %s: %w", unit, err)
	}
	return processes, nil
}

func parseDNSUnitProcesses(output string) (dnsUnitProcesses, error) {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		key, candidate, found := strings.Cut(line, "=")
		if !found || (key != "MainPID" && key != "ControlPID" && key != "SubState") {
			return dnsUnitProcesses{},
				errors.New("systemctl returned an unexpected DNS unit process row")
		}
		if _, exists := values[key]; exists || candidate == "" {
			return dnsUnitProcesses{}, errors.New("systemctl returned an ambiguous DNS unit process")
		}
		values[key] = candidate
	}
	if len(values) != 3 {
		return dnsUnitProcesses{}, errors.New("systemctl returned incomplete DNS unit processes")
	}
	if values["SubState"] == "" {
		return dnsUnitProcesses{}, errors.New("systemctl returned an empty DNS unit substate")
	}
	parse := func(name string) (uint64, error) {
		value := values[name]
		pid, err := strconv.ParseUint(value, 10, 64)
		if err != nil || strconv.FormatUint(pid, 10) != value {
			return 0, fmt.Errorf("systemctl returned a non-canonical %s", name)
		}
		return pid, nil
	}
	mainPID, err := parse("MainPID")
	if err != nil {
		return dnsUnitProcesses{}, err
	}
	controlPID, err := parse("ControlPID")
	if err != nil {
		return dnsUnitProcesses{}, err
	}
	return dnsUnitProcesses{
		MainPID: mainPID, ControlPID: controlPID, SubState: values["SubState"],
	}, nil
}

func verifyDNSUnitProcessesStopped(processes dnsUnitProcesses) error {
	if processes.MainPID != 0 || processes.ControlPID != 0 || processes.SubState != "dead" {
		return errors.New("DNS unit is not exactly dead with zero main and control processes")
	}
	return nil
}

func validateAPTBINDVendorNamedIdentity(
	named dnsUnitIdentity,
	aliasEnabled bool,
) error {
	expectedNames := []string{"named.service"}
	if aliasEnabled {
		expectedNames = []string{"bind9.service", "named.service"}
	}
	if named.ID != "named.service" ||
		!reflect.DeepEqual(named.Names, expectedNames) ||
		named.FragmentPath != "/usr/lib/systemd/system/named.service" ||
		len(named.DropInPaths) != 0 || named.SourcePath != "" ||
		named.Transient != "no" || named.ExecStartPath != "/usr/sbin/named" ||
		named.ExecStartArgv != "/usr/sbin/named -f $OPTIONS" {
		return errors.New("named.service does not resolve to the exact APT vendor BIND identity")
	}
	return nil
}

func validateAPTBINDVendorAliasIdentity(named, alias dnsUnitIdentity) error {
	if err := validateAPTBINDVendorNamedIdentity(named, true); err != nil {
		return err
	}
	if err := validateAPTBINDVendorNamedIdentity(alias, true); err != nil ||
		!reflect.DeepEqual(named, alias) {
		return errors.New("BIND service aliases do not resolve to the exact vendor named.service identity")
	}
	return nil
}

func validatePacmanBINDVendorIdentity(named dnsUnitIdentity) error {
	if named.ID != "named.service" ||
		!reflect.DeepEqual(named.Names, []string{"named.service"}) ||
		named.FragmentPath != "/usr/lib/systemd/system/named.service" ||
		len(named.DropInPaths) != 0 || named.SourcePath != "" ||
		named.Transient != "no" || named.ExecStartPath != "/usr/bin/named" ||
		named.ExecStartArgv != "/usr/bin/named -f -u named" {
		return errors.New("named.service does not resolve to the exact vendor BIND identity")
	}
	return nil
}

func exactUnmaskedInactiveBINDUnit(state bindInstallUnitState) bool {
	return state.loadState == "loaded" && state.activeState == "inactive" &&
		!state.masked()
}

func exactAbsentInactiveBINDUnit(state bindInstallUnitState) bool {
	return state.loadState == "not-found" && state.activeState == "inactive" &&
		state.unitFileState == ""
}

func exactStockDisabledBINDTarget(
	named, alias bindInstallUnitState,
) bool {
	return named.loadState == "loaded" &&
		named.activeState == "inactive" &&
		named.unitFileState == "disabled" &&
		exactAbsentInactiveBINDUnit(alias)
}

func exactAbsentBINDTarget(
	named, alias bindInstallUnitState,
) bool {
	return exactAbsentInactiveBINDUnit(named) &&
		exactAbsentInactiveBINDUnit(alias)
}

func exactLoadedUnmaskedBINDUnit(state bindInstallUnitState) bool {
	return state.loadState == "loaded" &&
		(state.activeState == "active" || state.activeState == "inactive") &&
		!state.masked()
}

type bindUnitIdentityProofOps struct {
	inspectStates      func() (bindInstallUnitState, bindInstallUnitState, error)
	inspectIdentity    func(string) (dnsUnitIdentity, error)
	inspectVendorFiles func() (bindVendorFilesIdentity, error)
	inspectProcesses   func(string) (dnsUnitProcesses, error)
}

type bindUnitIdentityProofMode struct {
	aptAliasEnabled      bool
	requireInactive      bool
	requireStopped       bool
	requireNamedDisabled bool
	requireEnabled       bool
}

// bindPreStartIdentityOps keeps the narrow test seam readable while the same
// exact proof is reused by runtime readiness and post-start verification.
type bindPreStartIdentityOps = bindUnitIdentityProofOps

func bindUnitIdentityProofOperations(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) (bindUnitIdentityProofOps, error) {
	if ctx == nil || systemctl == "" {
		return bindUnitIdentityProofOps{},
			errors.New("invalid BIND unit identity proof")
	}
	return bindUnitIdentityProofOps{
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			return inspectBINDTargetStates(ctx, systemctl)
		},
		inspectIdentity: func(unit string) (dnsUnitIdentity, error) {
			return inspectDNSUnitIdentity(ctx, systemctl, unit)
		},
		inspectVendorFiles: func() (bindVendorFilesIdentity, error) {
			return inspectHostBINDVendorFiles(ctx, profile)
		},
		inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
			return inspectDNSUnitProcesses(ctx, systemctl, unit)
		},
	}, nil
}

func verifyBINDPreEnableIdentity(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	if ctx == nil {
		return errors.New("BIND pre-enable proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	ops, err := bindUnitIdentityProofOperations(proofCtx, profile, systemctl)
	if err != nil {
		return err
	}
	return verifyBINDUnitIdentityWithOps(profile, bindUnitIdentityProofMode{
		aptAliasEnabled:      false,
		requireInactive:      true,
		requireStopped:       true,
		requireNamedDisabled: true,
	}, ops)
}

func verifyBINDPreStartIdentity(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	if ctx == nil {
		return errors.New("BIND pre-start proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	ops, err := bindUnitIdentityProofOperations(proofCtx, profile, systemctl)
	if err != nil {
		return err
	}
	return verifyBINDUnitIdentityWithOps(profile, bindUnitIdentityProofMode{
		aptAliasEnabled: true,
		requireInactive: true,
		requireStopped:  true,
		requireEnabled:  true,
	}, ops)
}

func verifyBINDPreStartIdentityWithOps(
	profile hostplatform.Profile,
	ops bindPreStartIdentityOps,
) error {
	return verifyBINDUnitIdentityWithOps(profile, bindUnitIdentityProofMode{
		aptAliasEnabled: true,
		requireInactive: true,
		requireStopped:  true,
		requireEnabled:  true,
	}, ops)
}

type bindUnitProcessSnapshot struct {
	named dnsUnitProcesses
	alias dnsUnitProcesses
}

func verifyBINDUnitIdentityWithOps(
	profile hostplatform.Profile,
	mode bindUnitIdentityProofMode,
	ops bindUnitIdentityProofOps,
) error {
	if ops.inspectStates == nil || ops.inspectIdentity == nil ||
		ops.inspectVendorFiles == nil ||
		(mode.requireStopped && ops.inspectProcesses == nil) {
		return errors.New("invalid BIND unit identity operations")
	}
	validateStates := func(named, alias bindInstallUnitState) error {
		if !exactLoadedUnmaskedBINDUnit(named) ||
			(mode.requireInactive && !exactUnmaskedInactiveBINDUnit(named)) {
			return errors.New("named.service is not an exact unmasked BIND target")
		}
		if mode.requireNamedDisabled && named.unitFileState != "disabled" {
			return errors.New("named.service is not the exact disabled pre-enable target")
		}
		if mode.requireEnabled && named.unitFileState != "enabled" {
			return errors.New("named.service is not exactly enabled")
		}
		switch profile.PackageManager {
		case hostplatform.PackageManagerAPT:
			if mode.aptAliasEnabled {
				if !exactLoadedUnmaskedBINDUnit(alias) ||
					(mode.requireInactive && !exactUnmaskedInactiveBINDUnit(alias)) {
					return errors.New("bind9.service is not the exact enabled BIND alias")
				}
				if mode.requireEnabled && alias.unitFileState != "enabled" {
					return errors.New("bind9.service is not exactly enabled")
				}
			} else if !exactAbsentInactiveBINDUnit(alias) {
				return errors.New("bind9.service exists before its verified APT alias enable")
			}
		case hostplatform.PackageManagerPacman:
			if !exactAbsentInactiveBINDUnit(alias) {
				return errors.New("bind9.service unexpectedly exists on the pacman BIND target")
			}
		default:
			return errors.New("BIND identity proof is unsupported on this package manager")
		}
		return nil
	}
	inspectIdentities := func() (dnsUnitIdentity, dnsUnitIdentity, error) {
		named, err := ops.inspectIdentity("named.service")
		if err != nil {
			return dnsUnitIdentity{}, dnsUnitIdentity{}, err
		}
		switch profile.PackageManager {
		case hostplatform.PackageManagerAPT:
			if !mode.aptAliasEnabled {
				if err := validateAPTBINDVendorNamedIdentity(named, false); err != nil {
					return dnsUnitIdentity{}, dnsUnitIdentity{}, err
				}
				return named, dnsUnitIdentity{}, nil
			}
			alias, err := ops.inspectIdentity("bind9.service")
			if err != nil {
				return dnsUnitIdentity{}, dnsUnitIdentity{}, err
			}
			if err := validateAPTBINDVendorAliasIdentity(named, alias); err != nil {
				return dnsUnitIdentity{}, dnsUnitIdentity{}, err
			}
			return named, alias, nil
		case hostplatform.PackageManagerPacman:
			if err := validatePacmanBINDVendorIdentity(named); err != nil {
				return dnsUnitIdentity{}, dnsUnitIdentity{}, err
			}
			return named, dnsUnitIdentity{}, nil
		default:
			return dnsUnitIdentity{}, dnsUnitIdentity{},
				errors.New("BIND identity proof is unsupported on this package manager")
		}
	}
	inspectStoppedProcesses := func() (bindUnitProcessSnapshot, error) {
		if !mode.requireStopped {
			return bindUnitProcessSnapshot{}, nil
		}
		named, err := ops.inspectProcesses("named.service")
		if err != nil {
			return bindUnitProcessSnapshot{}, err
		}
		if err := verifyDNSUnitProcessesStopped(named); err != nil {
			return bindUnitProcessSnapshot{}, fmt.Errorf("named.service is not stopped: %w", err)
		}
		snapshot := bindUnitProcessSnapshot{named: named}
		if profile.PackageManager == hostplatform.PackageManagerAPT &&
			mode.aptAliasEnabled {
			alias, err := ops.inspectProcesses("bind9.service")
			if err != nil {
				return bindUnitProcessSnapshot{}, err
			}
			if err := verifyDNSUnitProcessesStopped(alias); err != nil {
				return bindUnitProcessSnapshot{}, fmt.Errorf("bind9.service is not stopped: %w", err)
			}
			snapshot.alias = alias
		}
		return snapshot, nil
	}

	namedStateBefore, aliasStateBefore, err := ops.inspectStates()
	if err != nil {
		return err
	}
	if err := validateStates(namedStateBefore, aliasStateBefore); err != nil {
		return err
	}
	namedIdentityBefore, aliasIdentityBefore, err := inspectIdentities()
	if err != nil {
		return err
	}
	vendorFilesBefore, err := ops.inspectVendorFiles()
	if err != nil {
		return fmt.Errorf("verify BIND vendor unit files: %w", err)
	}
	processesBefore, err := inspectStoppedProcesses()
	if err != nil {
		return err
	}

	namedStateAfter, aliasStateAfter, err := ops.inspectStates()
	if err != nil {
		return err
	}
	if err := validateStates(namedStateAfter, aliasStateAfter); err != nil {
		return err
	}
	if namedStateAfter != namedStateBefore || aliasStateAfter != aliasStateBefore {
		return errors.New("BIND unit state changed during exact identity verification")
	}
	namedIdentityAfter, aliasIdentityAfter, err := inspectIdentities()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(namedIdentityAfter, namedIdentityBefore) ||
		!reflect.DeepEqual(aliasIdentityAfter, aliasIdentityBefore) {
		return errors.New("BIND systemd identity changed during exact verification")
	}
	vendorFilesAfter, err := ops.inspectVendorFiles()
	if err != nil {
		return fmt.Errorf("reverify BIND vendor unit files: %w", err)
	}
	if vendorFilesAfter != vendorFilesBefore {
		return errors.New("BIND vendor unit files changed during exact verification")
	}
	processesAfter, err := inspectStoppedProcesses()
	if err != nil {
		return err
	}
	if processesAfter != processesBefore {
		return errors.New("BIND unit process state changed during exact verification")
	}
	return nil
}

type bindActivationOps struct {
	verifyMaskParent func() error
	unmask           func(context.Context, string, bool) error
	daemonReload     func(context.Context) error
	verifyPreEnable  func(context.Context) (bindBeforeEnableDisposition, error)
	enable           func(context.Context, string) error
	verifyPreStart   func(context.Context) error
	start            func(context.Context, string) error
	verifyStarted    func(context.Context) error
}

func activateBINDTargetWithVerifiedIdentity(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	unit string,
) error {
	return activateBINDTargetWithOps(ctx, unit, bindActivationOps{
		verifyMaskParent: verifyBINDMaskParentMetadata,
		unmask: func(commandCtx context.Context, target string, runtime bool) error {
			args := []string{"unmask", target}
			if runtime {
				args = []string{"unmask", "--runtime", target}
			}
			output, err := runServiceMutationCombinedOutput(commandCtx, systemctl, args...)
			if err != nil {
				return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, firstLine(string(output)))
			}
			return nil
		},
		daemonReload: func(commandCtx context.Context) error {
			reloadCtx, cancel := context.WithTimeout(commandCtx, bindDaemonReloadTimeout)
			defer cancel()
			output, err := runServiceMutationCombinedOutput(reloadCtx, systemctl, "daemon-reload")
			if err != nil {
				return fmt.Errorf("systemctl daemon-reload: %w: %s", err, firstLine(string(output)))
			}
			return nil
		},
		verifyPreEnable: func(
			verifyCtx context.Context,
		) (bindBeforeEnableDisposition, error) {
			return verifyBINDBeforeEnableIdentity(verifyCtx, profile, systemctl)
		},
		enable: func(enableCtx context.Context, target string) error {
			return enableServiceForMutationWithExecutable(enableCtx, systemctl, target, false)
		},
		verifyPreStart: func(verifyCtx context.Context) error {
			return verifyBINDPreStartIdentity(verifyCtx, profile, systemctl)
		},
		start: func(startCtx context.Context, target string) error {
			output, commandErr := runServiceMutationCombinedOutput(
				startCtx, systemctl, "start", target,
			)
			verifyErr := verifyServiceMutationUnitWithExecutable(
				startCtx, systemctl, target, true,
			)
			if verifyErr == nil {
				return nil
			}
			if commandErr != nil {
				return fmt.Errorf(
					"systemctl-start-failed:%v:%s; reconciliation: %v",
					commandErr, strings.TrimSpace(string(output)), verifyErr,
				)
			}
			return verifyErr
		},
		verifyStarted: func(verifyCtx context.Context) error {
			return verifyOnlyBINDActive(verifyCtx, profile, systemctl)
		},
	})
}

func activateBINDTargetWithOps(
	ctx context.Context,
	unit string,
	ops bindActivationOps,
) error {
	if ctx == nil || unit != "named.service" || ops.verifyMaskParent == nil ||
		ops.unmask == nil ||
		ops.daemonReload == nil || ops.verifyPreEnable == nil ||
		ops.enable == nil || ops.verifyPreStart == nil ||
		ops.start == nil || ops.verifyStarted == nil {
		return errors.New("invalid BIND activation operation")
	}
	verifyBeforeMutation := func(action string) error {
		if err := ops.verifyMaskParent(); err != nil {
			return fmt.Errorf("verify BIND mask parent before %s: %w", action, err)
		}
		return nil
	}
	for _, target := range []string{"named.service", "bind9.service"} {
		if err := verifyBeforeMutation("persistent unmask"); err != nil {
			return err
		}
		if err := ops.unmask(ctx, target, false); err != nil {
			return err
		}
		if err := verifyBeforeMutation("runtime unmask"); err != nil {
			return err
		}
		if err := ops.unmask(ctx, target, true); err != nil {
			return err
		}
	}
	if err := verifyBeforeMutation("daemon reload"); err != nil {
		return err
	}
	if err := ops.daemonReload(ctx); err != nil {
		return err
	}
	disposition, err := ops.verifyPreEnable(ctx)
	if err != nil {
		return fmt.Errorf("verify BIND vendor identity before enabling aliases: %w", err)
	}
	switch disposition {
	case bindBeforeEnableNeedsEnable:
		if err := verifyBeforeMutation("enable"); err != nil {
			return err
		}
		if err := ops.enable(ctx, unit); err != nil {
			return fmt.Errorf("enable BIND vendor unit without starting it: %w", err)
		}
	case bindBeforeEnableAlreadyEnabled:
		// Exact enabled alias identity was proved after unmask; do not rewrite
		// it before the second proof and separate start.
	default:
		return errors.New("BIND pre-enable proof returned an invalid disposition")
	}
	if err := verifyBeforeMutation("daemon reload"); err != nil {
		return err
	}
	if err := ops.daemonReload(ctx); err != nil {
		return err
	}
	if err := ops.verifyPreStart(ctx); err != nil {
		return fmt.Errorf("verify BIND vendor alias identity immediately before start: %w", err)
	}
	if err := verifyBeforeMutation("start"); err != nil {
		return err
	}
	if err := ops.start(ctx, unit); err != nil {
		return fmt.Errorf("start verified BIND vendor unit: %w", err)
	}
	if err := ops.verifyStarted(ctx); err != nil {
		return fmt.Errorf("verify started BIND vendor unit: %w", err)
	}
	return nil
}

type bindBeforeEnableDisposition uint8

const (
	bindBeforeEnableNeedsEnable bindBeforeEnableDisposition = iota + 1
	bindBeforeEnableAlreadyEnabled
)

func verifyBINDBeforeEnableIdentity(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) (bindBeforeEnableDisposition, error) {
	if ctx == nil {
		return 0, errors.New("BIND before-enable proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	named, alias, err := inspectBINDTargetStates(proofCtx, systemctl)
	if err != nil {
		return 0, err
	}
	return verifyBINDBeforeEnableIdentityWithOps(
		profile, named, alias,
		func() error {
			return verifyBINDPreEnableIdentity(proofCtx, profile, systemctl)
		},
		func() error {
			return verifyBINDPreStartIdentity(proofCtx, profile, systemctl)
		},
	)
}

func verifyBINDBeforeEnableIdentityWithOps(
	profile hostplatform.Profile,
	named, alias bindInstallUnitState,
	verifyDisabled, verifyEnabled func() error,
) (bindBeforeEnableDisposition, error) {
	if verifyDisabled == nil || verifyEnabled == nil {
		return 0, errors.New("invalid BIND before-enable proof operations")
	}
	if exactStockDisabledBINDTarget(named, alias) {
		if err := verifyDisabled(); err != nil {
			return 0, err
		}
		return bindBeforeEnableNeedsEnable, nil
	}
	enabledNamed := named.loadState == "loaded" &&
		named.activeState == "inactive" &&
		named.unitFileState == "enabled"
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		enabledAlias := alias.loadState == "loaded" &&
			alias.activeState == "inactive" &&
			alias.unitFileState == "enabled"
		if !enabledNamed || !enabledAlias {
			return 0, errors.New("APT BIND is neither exact stock-disabled nor exact enabled")
		}
	case hostplatform.PackageManagerPacman:
		if !enabledNamed || !exactAbsentInactiveBINDUnit(alias) {
			return 0, errors.New("pacman BIND is neither exact stock-disabled nor exact enabled")
		}
	default:
		return 0, errors.New("BIND before-enable proof is unsupported on this package manager")
	}
	if err := verifyEnabled(); err != nil {
		return 0, err
	}
	return bindBeforeEnableAlreadyEnabled, nil
}

func exactPersistentMaskedInactiveBINDUnit(state bindInstallUnitState) bool {
	return state.loadState == "masked" &&
		state.activeState == "inactive" &&
		state.unitFileState == "masked"
}

func classifyBINDTargetNotServingStates(
	named, alias bindInstallUnitState,
) (sealed bool, err error) {
	if named.activeState == "failed" || alias.activeState == "failed" {
		return false, errors.New("BIND target has a failed unit")
	}
	if named.masked() || alias.masked() {
		if exactPersistentMaskedInactiveBINDUnit(named) &&
			exactPersistentMaskedInactiveBINDUnit(alias) {
			return true, nil
		}
		return false, errors.New(
			"BIND target mask state is mixed, runtime-only, active, or otherwise unsealed",
		)
	}
	if named.active() || alias.active() {
		return false, errors.New("BIND target is already serving")
	}
	return false, nil
}

func inspectBINDTargetStates(
	ctx context.Context,
	systemctl string,
) (bindInstallUnitState, bindInstallUnitState, error) {
	guard := dnsSystemdStateGuard(systemctl)
	named, err := guard.inspect(ctx, "named.service")
	if err != nil {
		return bindInstallUnitState{}, bindInstallUnitState{}, err
	}
	alias, err := guard.inspect(ctx, "bind9.service")
	if err != nil {
		return bindInstallUnitState{}, bindInstallUnitState{}, err
	}
	return named, alias, nil
}

// verifyBINDTargetNotServing is a pre-mutation target check. An exact pair of
// persistent masks is accepted only through the stronger sealed proof below;
// it is never treated as proof of the vendor unit identity hidden by /dev/null.
func verifyBINDTargetNotServing(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	if ctx == nil {
		return errors.New("BIND target proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	return verifyBINDTargetNotServingWithOps(
		profile,
		bindTargetNotServingOps{
			inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
				return inspectBINDTargetStates(proofCtx, systemctl)
			},
			verifySealed: func() error {
				return verifyBINDSealedTargetNotServing(proofCtx, systemctl)
			},
			verifyAbsent: func() error {
				return verifyBINDAbsentTargetNotServing(proofCtx, systemctl)
			},
			verifyPreEnable: func() error {
				return verifyBINDPreEnableIdentity(proofCtx, profile, systemctl)
			},
			verifyPreStart: func() error {
				return verifyBINDPreStartIdentity(proofCtx, profile, systemctl)
			},
		},
	)
}

type bindTargetInstallProof struct {
	exact bool
}

func proveBINDTargetNotServing(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) (bindTargetInstallProof, error) {
	if err := verifyBINDTargetNotServing(ctx, profile, systemctl); err != nil {
		return bindTargetInstallProof{}, err
	}
	return bindTargetInstallProof{exact: true}, nil
}

func proveBINDTargetNotServingWithOps(
	profile hostplatform.Profile,
	ops bindTargetNotServingOps,
) (bindTargetInstallProof, error) {
	if err := verifyBINDTargetNotServingWithOps(profile, ops); err != nil {
		return bindTargetInstallProof{}, err
	}
	return bindTargetInstallProof{exact: true}, nil
}

func runVerifiedBINDTargetInstall(
	proof bindTargetInstallProof,
	install func() error,
) error {
	if !proof.exact || install == nil {
		return errors.New(
			"BIND package mutation requires an exact target-not-serving proof",
		)
	}
	return install()
}

type bindPostInstallProofOps struct {
	verifyGeneric     func() error
	verifyNoAuthority func() error
}

func runBINDPostInstallContinuation(
	requireNoManagedAuthority bool,
	ops bindPostInstallProofOps,
	continuation func() error,
) error {
	if ops.verifyGeneric == nil || ops.verifyNoAuthority == nil ||
		continuation == nil {
		return errors.New("invalid BIND post-install proof operation")
	}
	var err error
	if requireNoManagedAuthority {
		err = ops.verifyNoAuthority()
	} else {
		err = ops.verifyGeneric()
	}
	if err != nil {
		return err
	}
	return continuation()
}

type bindTargetNotServingOps struct {
	inspectStates   func() (bindInstallUnitState, bindInstallUnitState, error)
	verifySealed    func() error
	verifyAbsent    func() error
	verifyPreEnable func() error
	verifyPreStart  func() error
}

func verifyBINDTargetNotServingWithOps(
	profile hostplatform.Profile,
	ops bindTargetNotServingOps,
) error {
	if ops.inspectStates == nil {
		return errors.New("invalid BIND target proof operations")
	}
	namedBefore, aliasBefore, err := ops.inspectStates()
	if err != nil {
		return err
	}
	sealed, err := classifyBINDTargetNotServingStates(namedBefore, aliasBefore)
	if err != nil {
		return err
	}
	if sealed {
		if ops.verifySealed == nil {
			return errors.New("BIND sealed-target proof is unavailable")
		}
		return ops.verifySealed()
	}
	if exactAbsentBINDTarget(namedBefore, aliasBefore) {
		if ops.verifyAbsent == nil {
			return errors.New("BIND absent-target proof is unavailable")
		}
		return ops.verifyAbsent()
	}
	if exactStockDisabledBINDTarget(namedBefore, aliasBefore) {
		if ops.verifyPreEnable == nil {
			return errors.New("BIND stock-target proof is unavailable")
		}
		return ops.verifyPreEnable()
	}
	if !exactUnmaskedInactiveBINDUnit(namedBefore) {
		return errors.New("named.service is not an exact inactive BIND target")
	}
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		if !exactUnmaskedInactiveBINDUnit(aliasBefore) {
			return errors.New("APT BIND alias topology is mixed or incomplete")
		}
	case hostplatform.PackageManagerPacman:
		if !exactAbsentInactiveBINDUnit(aliasBefore) {
			return errors.New("pacman BIND unexpectedly has a bind9.service alias")
		}
	default:
		return errors.New("BIND target proof is unsupported on this package manager")
	}
	if ops.verifyPreStart == nil {
		return errors.New("BIND enabled-target proof is unavailable")
	}
	return ops.verifyPreStart()
}

type bindAbsentTargetProofOps struct {
	inspectStates  func() (bindInstallUnitState, bindInstallUnitState, error)
	port53Conflict func() (bool, error)
}

func verifyBINDAbsentTargetNotServing(
	ctx context.Context,
	systemctl string,
) error {
	if ctx == nil || systemctl == "" {
		return errors.New("invalid absent BIND target proof")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	return verifyBINDAbsentTargetNotServingWithOps(bindAbsentTargetProofOps{
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			return inspectBINDTargetStates(proofCtx, systemctl)
		},
		port53Conflict: func() (bool, error) {
			return dnsPort53ConflictCheck(proofCtx, false, true)
		},
	})
}

func verifyBINDAbsentTargetNotServingWithOps(
	ops bindAbsentTargetProofOps,
) error {
	if ops.inspectStates == nil || ops.port53Conflict == nil {
		return errors.New("invalid absent BIND target proof operations")
	}
	namedBefore, aliasBefore, err := ops.inspectStates()
	if err != nil {
		return err
	}
	if !exactAbsentBINDTarget(namedBefore, aliasBefore) {
		return errors.New("BIND target units are not exactly absent and inactive")
	}
	for sample := 0; sample < 2; sample++ {
		conflict, err := ops.port53Conflict()
		if err != nil {
			return err
		}
		if conflict {
			return errors.New("a public BIND or unknown port-53 listener exists while BIND units are absent")
		}
	}
	namedAfter, aliasAfter, err := ops.inspectStates()
	if err != nil {
		return err
	}
	if !exactAbsentBINDTarget(namedAfter, aliasAfter) ||
		namedAfter != namedBefore || aliasAfter != aliasBefore {
		return errors.New("absent BIND target state changed during verification")
	}
	return nil
}

type bindSealedTargetProofOps struct {
	inspectStates         func() (bindInstallUnitState, bindInstallUnitState, error)
	verifyPersistentMasks func() error
	inspectProcesses      func(string) (dnsUnitProcesses, error)
	port53Conflict        func() (bool, error)
}

// verifyBINDSealedTargetNotServing proves the exact transitional state left by
// the guarded package install. It deliberately proves only that BIND cannot be
// serving; the vendor alias identity is verified later, after both masks are
// removed and immediately before enable/start.
func verifyBINDSealedTargetNotServing(ctx context.Context, systemctl string) error {
	return verifyBINDSealedTargetNotServingWithAuthorityPolicy(
		ctx, systemctl, true,
	)
}

// verifyBINDSealedTargetNotServingWithoutManagedAuthority is the stricter
// source-none recovery proof. Unlike the generic standby proof above, it
// rejects both BIND and PowerDNS listeners. It is read-only and preserves the
// caller's deadline; its caller must hold the shared host mutation flock while
// binding this result to recovery evidence.
func verifyBINDSealedTargetNotServingWithoutManagedAuthority(
	ctx context.Context,
	systemctl string,
) error {
	return verifyBINDSealedTargetNotServingWithAuthorityPolicy(
		ctx, systemctl, false,
	)
}

func verifyBINDSealedTargetNotServingWithAuthorityPolicy(
	ctx context.Context,
	systemctl string,
	allowPowerDNS bool,
) error {
	if ctx == nil || systemctl == "" {
		return errors.New("invalid BIND sealed-target proof")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	return verifyBINDSealedTargetNotServingWithOps(
		bindSealedTargetProofOperations(
			proofCtx, systemctl, allowPowerDNS, dnsPort53ConflictCheck,
		),
	)
}

func bindSealedTargetProofOperations(
	ctx context.Context,
	systemctl string,
	allowPowerDNS bool,
	port53Check func(context.Context, bool, bool) (bool, error),
) bindSealedTargetProofOps {
	return bindSealedTargetProofOps{
		inspectStates: func() (bindInstallUnitState, bindInstallUnitState, error) {
			return inspectBINDTargetStates(ctx, systemctl)
		},
		verifyPersistentMasks: verifyBINDPersistentMaskFiles,
		inspectProcesses: func(unit string) (dnsUnitProcesses, error) {
			return inspectDNSUnitProcesses(ctx, systemctl, unit)
		},
		port53Conflict: func() (bool, error) {
			return port53Check(ctx, false, allowPowerDNS)
		},
	}
}

func verifyBINDSealedTargetNotServingWithOps(ops bindSealedTargetProofOps) error {
	if ops.inspectStates == nil || ops.verifyPersistentMasks == nil ||
		ops.inspectProcesses == nil || ops.port53Conflict == nil {
		return errors.New("invalid BIND sealed-target proof operations")
	}
	namedBefore, aliasBefore, err := ops.inspectStates()
	if err != nil {
		return err
	}
	sealed, err := classifyBINDTargetNotServingStates(namedBefore, aliasBefore)
	if err != nil || !sealed {
		if err == nil {
			err = errors.New("BIND target is not exactly persistently masked and inactive")
		}
		return err
	}
	if err := ops.verifyPersistentMasks(); err != nil {
		return err
	}
	for _, unit := range []string{"named.service", "bind9.service"} {
		processes, err := ops.inspectProcesses(unit)
		if err != nil {
			return err
		}
		if err := verifyDNSUnitProcessesStopped(processes); err != nil {
			return fmt.Errorf("%s is not stopped while masked: %w", unit, err)
		}
	}
	conflict, err := ops.port53Conflict()
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("a public BIND or unknown port-53 listener exists while BIND is masked")
	}
	namedAfter, aliasAfter, err := ops.inspectStates()
	if err != nil {
		return err
	}
	afterSealed, err := classifyBINDTargetNotServingStates(namedAfter, aliasAfter)
	if err != nil || !afterSealed ||
		namedAfter != namedBefore || aliasAfter != aliasBefore {
		if err == nil {
			err = errors.New("BIND sealed unit state changed during verification")
		}
		return err
	}
	if err := ops.verifyPersistentMasks(); err != nil {
		return err
	}
	for _, unit := range []string{"named.service", "bind9.service"} {
		processes, err := ops.inspectProcesses(unit)
		if err != nil {
			return err
		}
		if err := verifyDNSUnitProcessesStopped(processes); err != nil {
			return fmt.Errorf("%s changed process state during masked verification: %w", unit, err)
		}
	}
	conflict, err = ops.port53Conflict()
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("a public BIND or unknown port-53 listener appeared during masked verification")
	}
	return nil
}

func verifyBINDUnitTopology(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	if ctx == nil {
		return errors.New("BIND unit topology proof requires a context")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	ops, err := bindUnitIdentityProofOperations(proofCtx, profile, systemctl)
	if err != nil {
		return err
	}
	return verifyBINDUnitIdentityWithOps(profile, bindUnitIdentityProofMode{
		aptAliasEnabled: true,
		requireEnabled:  true,
	}, ops)
}

type bindRuntimeTopologySnapshot struct {
	namedState     bindInstallUnitState
	aliasState     bindInstallUnitState
	namedIdentity  dnsUnitIdentity
	aliasIdentity  dnsUnitIdentity
	vendorFiles    bindVendorFilesIdentity
	namedProcesses dnsUnitProcesses
	aliasProcesses dnsUnitProcesses
}

func inspectVerifiedBINDRuntimeTopology(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) (bindRuntimeTopologySnapshot, error) {
	if ctx == nil {
		return bindRuntimeTopologySnapshot{},
			errors.New("BIND runtime topology proof requires a context")
	}
	ops, err := bindUnitIdentityProofOperations(ctx, profile, systemctl)
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	capture := func() (bindRuntimeTopologySnapshot, error) {
		namedState, aliasState, err := ops.inspectStates()
		if err != nil {
			return bindRuntimeTopologySnapshot{}, err
		}
		if namedState.loadState != "loaded" ||
			namedState.unitFileState != "enabled" {
			return bindRuntimeTopologySnapshot{},
				errors.New("named.service does not have exact enabled runtime topology")
		}
		namedIdentity, err := ops.inspectIdentity("named.service")
		if err != nil {
			return bindRuntimeTopologySnapshot{}, err
		}
		aliasIdentity := dnsUnitIdentity{}
		switch profile.PackageManager {
		case hostplatform.PackageManagerAPT:
			if aliasState.loadState != "loaded" ||
				aliasState.unitFileState != "enabled" {
				return bindRuntimeTopologySnapshot{},
					errors.New("bind9.service does not have exact enabled alias topology")
			}
			aliasIdentity, err = ops.inspectIdentity("bind9.service")
			if err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
			if err := validateAPTBINDVendorAliasIdentity(
				namedIdentity, aliasIdentity,
			); err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
		case hostplatform.PackageManagerPacman:
			if !exactAbsentInactiveBINDUnit(aliasState) {
				return bindRuntimeTopologySnapshot{},
					errors.New("bind9.service unexpectedly exists in pacman runtime topology")
			}
			if err := validatePacmanBINDVendorIdentity(namedIdentity); err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
		default:
			return bindRuntimeTopologySnapshot{},
				errors.New("BIND runtime topology is unsupported on this package manager")
		}
		vendorFiles, err := ops.inspectVendorFiles()
		if err != nil {
			return bindRuntimeTopologySnapshot{},
				fmt.Errorf("verify BIND vendor unit files: %w", err)
		}
		namedProcesses, err := ops.inspectProcesses("named.service")
		if err != nil {
			return bindRuntimeTopologySnapshot{}, err
		}
		if namedProcesses.MainPID == 0 || namedProcesses.ControlPID != 0 ||
			namedProcesses.SubState != "running" {
			return bindRuntimeTopologySnapshot{},
				errors.New("named.service lacks an exact running process identity")
		}
		aliasProcesses := dnsUnitProcesses{}
		if profile.PackageManager == hostplatform.PackageManagerAPT {
			aliasProcesses, err = ops.inspectProcesses("bind9.service")
			if err != nil {
				return bindRuntimeTopologySnapshot{}, err
			}
			if aliasProcesses != namedProcesses {
				return bindRuntimeTopologySnapshot{},
					errors.New("bind9.service process identity differs from named.service")
			}
		}
		return bindRuntimeTopologySnapshot{
			namedState: namedState, aliasState: aliasState,
			namedIdentity: namedIdentity, aliasIdentity: aliasIdentity,
			vendorFiles:    vendorFiles,
			namedProcesses: namedProcesses, aliasProcesses: aliasProcesses,
		}, nil
	}
	before, err := capture()
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	after, err := capture()
	if err != nil {
		return bindRuntimeTopologySnapshot{}, err
	}
	if !reflect.DeepEqual(after, before) {
		return bindRuntimeTopologySnapshot{},
			errors.New("BIND runtime topology changed during exact verification")
	}
	return after, nil
}

func verifyBINDPublicListeners(output string, expectedMainPID uint64) error {
	_, err := canonicalBINDPublicListeners(output, expectedMainPID)
	return err
}

func canonicalBINDPublicListeners(
	output string,
	expectedMainPID uint64,
) ([]string, error) {
	return canonicalDNSAuthorityPublicListeners(
		output, "named", expectedMainPID,
	)
}

func canonicalDNSAuthorityPublicListeners(
	output string,
	expectedProcess string,
	expectedMainPID uint64,
) ([]string, error) {
	if expectedProcess == "" ||
		strings.ContainsAny(expectedProcess, "\x00\r\n\t ,()\"") ||
		expectedMainPID == 0 {
		return nil, errors.New("invalid DNS authority process identity")
	}
	foundTCP, foundUDP := false, false
	identities := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		row, err := parseCanonicalDNSPort53ListenerRow(line)
		if err != nil {
			return nil, err
		}
		if row.address.IsLoopback() || row.address.IsLinkLocalUnicast() {
			continue
		}
		if row.process != expectedProcess {
			return nil, errors.New("an unexpected process is holding a public DNS listener")
		}
		if row.pid != expectedMainPID {
			return nil, errors.New("a DNS authority listener PID differs from its systemd MainPID")
		}
		identities[fmt.Sprintf(
			"%s|%s|%d", row.protocol, row.address.String(), row.pid,
		)] = struct{}{}
		if row.protocol == "tcp" {
			foundTCP = true
		} else {
			foundUDP = true
		}
	}
	if !foundTCP || !foundUDP {
		return nil, errors.New("the DNS authority does not own both public TCP and UDP port 53 listeners")
	}
	result := make([]string, 0, len(identities))
	for identity := range identities {
		result = append(result, identity)
	}
	sort.Strings(result)
	return result, nil
}
