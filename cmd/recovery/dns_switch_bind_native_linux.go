//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/dnsunitidentity"
	"github.com/alicelik/celikpanel/internal/dnsunitrestore"
	"github.com/alicelik/celikpanel/internal/dnswire"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/pdnsvendor"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

var bindInverseUnitNames = []string{"named.service", "bind9.service", "pdns.service"}

func bindInverseSourceUnitIdentity(ctx context.Context) error {
	profile, err := hostplatform.Detect()
	if err != nil {
		return err
	}
	before, err := pdnsvendor.InspectInstalledUnit(ctx, profile)
	if err != nil {
		return err
	}
	identity, err := dnsenginerecovery.ProbePDNSVendorIdentity(ctx, profile, dnsenginerecovery.SystemdPDNSIdentityRunner)
	if err != nil {
		return err
	}
	after, err := pdnsvendor.InspectInstalledUnit(ctx, profile)
	if err != nil {
		return err
	}
	if before != after || identity.ExecStartPath == "" {
		return errors.New("PowerDNS vendor unit changed during source identity observation")
	}
	return nil
}
func bindInverseSourceProof(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) error {
	if err := bindInverseSourceUnitIdentity(ctx); err != nil {
		return fmt.Errorf("source PowerDNS vendor unit: %w", err)
	}
	if j.InversePlan == nil || j.InversePlan.SourcePDNS == nil {
		return errors.New("frozen PowerDNS source proof is absent")
	}
	gid, err := localServiceGroupID("/etc/group", "pdns")
	if err != nil {
		return err
	}
	p := j.InversePlan.SourcePDNS
	if err := dnsenginerecovery.ProbeInstalledPDNSAdoptionConfigs(ctx, policy, p.ConfigBefore, gid); err != nil {
		return fmt.Errorf("source PowerDNS config: %w", err)
	}
	if err := dnsenginerecovery.VerifyPDNSSourceDatabaseProof(ctx, policy.PDNSDatabasePath, p.Database); err != nil {
		return fmt.Errorf("source PowerDNS logical database: %w", err)
	}
	return ctx.Err()
}

func bindInversePlanLayoutMatches(layout bindroot.Layout, frozen string) bool {
	switch layout {
	case bindroot.APT:
		return frozen == "apt"
	case bindroot.Pacman:
		return frozen == "pacman"
	default:
		return false
	}
}

func bindInverseUnchangedConfig(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	return dnsenginerecovery.VerifyInstalledBINDUnchangedConfigV2(ctx, policy, j, layout, gid)
}
func bindInversePointer(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) (bool, error) {
	beforeCatalog, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return false, err
	}
	p, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return false, err
	}
	id, exists, err := p.Current()
	if err != nil {
		return false, err
	}
	target := exists && id == j.TargetGeneration
	previous := (j.HadPrevious && exists && id == j.PreviousGeneration) || (!j.HadPrevious && !exists)
	if !target && !previous {
		return false, errors.New("managed BIND pointer differs from frozen target and predecessor")
	}
	if exists {
		tree, err := p.LoadCurrent()
		if err != nil {
			return false, err
		}
		if tree.CurrentReceipt().Generation != id {
			return false, errors.New("managed BIND generation receipt differs from pointer")
		}
	}
	afterCatalog, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return false, err
	}
	if beforeCatalog != afterCatalog {
		return false, errors.New("installed BIND catalog changed during pointer observation")
	}
	again, againExists, err := p.Current()
	if err != nil || again != id || againExists != exists {
		return false, errors.Join(errors.New("managed BIND pointer changed during observation"), err)
	}
	return target, nil
}

func bindInverseUnits(ctx context.Context) ([]dnsenginerecovery.NativeUnitObservation, error) {
	return dnsenginerecovery.ProbeNativeUnits(ctx, bindInverseUnitNames, dnsenginerecovery.SystemdUnitRunner)
}

// bindSwitchNativeHost is the native half of recover-dns-bind-switch as
// injectable observers and effects. installedBINDSwitchNativeHost binds the
// fixed installed readers and writers; the assess and restore sequences below
// never choose them from journal content.
type bindSwitchNativeHost struct {
	sourceProof       func(context.Context, dnsengineartifact.SwitchJournalV1) error
	layout            func() (bindroot.Layout, uint32, error)
	unchangedConfig   func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) error
	pointer           func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) (bool, error)
	configs           func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) (bool, error)
	unitRunner        dnsenginerecovery.NativeUnitRunner
	runtimeRunner     dnsenginerecovery.BINDRuntimeRunner
	cgroup            func(context.Context, string) error
	sourceOnly        func(context.Context) error
	vendor            func(context.Context, dnsengineartifact.SwitchJournalV1, []dnsenginerecovery.NativeUnitObservation) error
	pdnsRuntime       func(context.Context) (uint64, error)
	answers           func(context.Context, dnsengineartifact.SwitchJournalV1, uint64) error
	activePredecessor func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) error
	maskParent        func() error
	runSystemd        func(context.Context, string, ...string) ([]byte, error)
	restoreConfigs    func(context.Context, dnsengineartifact.SwitchJournalV1, bindroot.Layout, uint32) error
	restorePointer    func(dnsengineartifact.SwitchJournalV1, bindroot.Layout) error
	restoreReceipt    func(dnsengineartifact.SwitchJournalV1) error
}

func installedBINDSwitchNativeHost(policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner) bindSwitchNativeHost {
	return bindSwitchNativeHost{
		sourceProof: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
			return bindInverseSourceProof(ctx, policy, j)
		},
		layout: installedBINDLayout,
		unchangedConfig: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
			return bindInverseUnchangedConfig(ctx, policy, j, layout, gid)
		},
		pointer: bindInversePointer,
		configs: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) (bool, error) {
			return bindInverseConfigs(ctx, policy, j, layout, gid)
		},
		unitRunner:    dnsenginerecovery.SystemdUnitRunner,
		runtimeRunner: dnsenginerecovery.SystemdBINDRuntimeRunner,
		cgroup: func(ctx context.Context, unit string) error {
			return dnsenginerecovery.ProbeEmptyUnitCgroup(ctx, unit, dnsenginerecovery.SystemdCgroupUnitRunner, dnsenginerecovery.NativeCgroupEvents)
		},
		sourceOnly:  installedBINDTargetSourceOnlyDNS,
		vendor:      verifyInstalledBINDSwitchVendorForJournal,
		pdnsRuntime: verifyInstalledPDNSRuntime,
		answers:     bindInverseAnswers,
		activePredecessor: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
			return bindInverseActivePredecessorProof(ctx, policy, j, layout, gid)
		},
		maskParent: bindInverseMaskParent,
		runSystemd: bindInverseSystemd,
		restoreConfigs: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
			return dnsenginerecovery.RestoreInstalledBINDSwitchConfigsV2(ctx, policy, j, layout, gid)
		},
		restorePointer: func(j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout) error {
			p, err := binddns.NewOSPublisher(string(layout))
			if err != nil {
				return err
			}
			return p.RestorePointer(j.TargetGeneration, j.PreviousGeneration, j.HadPrevious)
		},
		restoreReceipt: func(j dnsengineartifact.SwitchJournalV1) error {
			return dnsenginerecovery.RestoreExactBINDSwitchSourceReceipt(policy, owner, j)
		},
	}
}

func (h bindSwitchNativeHost) units(ctx context.Context) ([]dnsenginerecovery.NativeUnitObservation, error) {
	return dnsenginerecovery.ProbeNativeUnits(ctx, bindInverseUnitNames, h.unitRunner)
}

// stoppedTarget proves named.service stopped before and after each inverse
// effect. A journal admitted by BINDSwitchNeverStartedTargetJournal also
// accepts the two pre-start states (absent, the guard's persistent mask) with
// the source-only DNS proof; a loaded unit, and every other journal, keeps
// the unchanged loaded-unit proof.
func (h bindSwitchNativeHost) stoppedTarget(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
	if dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) {
		if _, err := dnsenginerecovery.ProbeStoppedNeverStartedBINDTarget(ctx, h.unitRunner, h.runtimeRunner, h.cgroup, h.sourceOnly); err != nil {
			return err
		}
	} else if err := dnsenginerecovery.ProbeStoppedUnitWithCgroup(ctx, "named.service", h.unitRunner, h.runtimeRunner, h.cgroup); err != nil {
		return err
	}
	units, err := h.units(ctx)
	if err != nil {
		return err
	}
	if units[1].ActiveState != "inactive" {
		return errors.New("BIND alias is not inactive")
	}
	return ctx.Err()
}

func (h bindSwitchNativeHost) restoreUnits(ctx context.Context, snapshots []dnsengineartifact.UnitSnapshot) error {
	return bindInverseRestoreUnitsWith(ctx, snapshots, h.maskParent, h.runSystemd)
}

// bindInverseGuardSealedTarget reports the one never-started target state the
// inverse retains: both BIND units still under the package guard's persistent
// mask (masked/masked, inactive) for a journal that froze them absent. The
// caller must also hold the vendor proof, which proves each mask link.
func bindInverseGuardSealedTarget(units []dnsenginerecovery.NativeUnitObservation, j dnsengineartifact.SwitchJournalV1) bool {
	if len(units) != 3 || !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) ||
		units[0].Name != "named.service" || units[1].Name != "bind9.service" {
		return false
	}
	for _, unit := range units[:2] {
		if unit.LoadState != "masked" || unit.UnitFileState != "masked" || unit.ActiveState != "inactive" {
			return false
		}
	}
	return true
}

func bindInverseTargetPreimage(units []dnsenginerecovery.NativeUnitObservation, j dnsengineartifact.SwitchJournalV1) bool {
	if len(units) != 3 {
		return false
	}
	// The package and its guard mask stay installed as rollback standby for a
	// target this operation created and never activated.
	if bindInverseGuardSealedTarget(units, j) {
		return true
	}
	for _, saved := range j.TargetUnitsBefore {
		found := false
		for _, unit := range units[:2] {
			if unit.Name == saved.Name && unit.ActiveState == saved.ActiveState && ((unit.LoadState == saved.LoadState && unit.UnitFileState == saved.UnitFileState) ||
				(saved.LoadState == "not-found" && saved.UnitFileState == "" &&
					unit.LoadState == "loaded" && unit.UnitFileState == "disabled" && unit.ActiveState == "inactive")) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func bindInverseAllowedNativeUnits(units []dnsenginerecovery.NativeUnitObservation, j dnsengineartifact.SwitchJournalV1) error {
	if len(units) != 3 || len(j.TargetUnitsBefore) != 2 || len(j.SourceUnitsBefore) != 1 {
		return errors.New("BIND inverse native unit evidence is incomplete")
	}
	source, savedSource := units[2], j.SourceUnitsBefore[0]
	if source.Name != "pdns.service" || savedSource.Name != "pdns.service" {
		return errors.New("BIND inverse source unit differs from frozen name")
	}
	sourceExact := source.LoadState == savedSource.LoadState && source.ActiveState == savedSource.ActiveState && source.UnitFileState == savedSource.UnitFileState
	sourceDisabling := savedSource.LoadState == "loaded" && source.LoadState == "loaded" &&
		source.UnitFileState == "disabled" && (source.ActiveState == "active" || source.ActiveState == "inactive")
	if !sourceExact && !sourceDisabling {
		return errors.New("source PowerDNS unit differs from frozen or operation-created state")
	}
	for _, saved := range j.TargetUnitsBefore {
		var current dnsenginerecovery.NativeUnitObservation
		found := false
		for _, u := range units[:2] {
			if u.Name == saved.Name {
				current = u
				found = true
			}
		}
		if !found || saved.ActiveState != "inactive" {
			return errors.New("target BIND unit lacks frozen inactive preimage")
		}
		exact := current.LoadState == saved.LoadState && current.ActiveState == saved.ActiveState && current.UnitFileState == saved.UnitFileState
		installed := current.ActiveState == "inactive" && current.LoadState == "loaded" && current.UnitFileState == "disabled"
		absent := current.ActiveState == "inactive" && current.LoadState == "not-found" && current.UnitFileState == ""
		sealed := current.ActiveState == "inactive" && current.LoadState == "masked" && current.UnitFileState == "masked"
		enabled := current.LoadState == "loaded" && current.UnitFileState == "enabled" &&
			(current.ActiveState == "inactive" || current.ActiveState == "active")
		if !exact && !installed && !absent && !sealed && !enabled {
			return fmt.Errorf("target BIND unit %s differs from allowed operation states", saved.Name)
		}
		if enabled && source.ActiveState != "inactive" {
			return errors.New("BIND activation preceded source PowerDNS stop")
		}
		if current.ActiveState == "active" && source.ActiveState != "inactive" {
			return errors.New("two native DNS authorities appear active")
		}
	}
	return nil
}
func bindInverseSourcePreimage(units []dnsenginerecovery.NativeUnitObservation, j dnsengineartifact.SwitchJournalV1) bool {
	if len(units) != 3 || len(j.SourceUnitsBefore) != 1 {
		return false
	}
	s, u := j.SourceUnitsBefore[0], units[2]
	return s.Name == u.Name && s.LoadState == u.LoadState && s.ActiveState == u.ActiveState && s.UnitFileState == u.UnitFileState
}
func bindInverseConfigs(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) (bool, error) {
	states, err := dnsenginerecovery.ProbeInstalledBINDSwitchConfigCheckpointV2(ctx, policy, j, layout, gid)
	if err != nil {
		return false, err
	}
	before := true
	for _, state := range states {
		if state != dnsenginerecovery.BINDConfigFileBefore {
			before = false
		}
	}
	return before, nil
}
func bindInverseAnswers(ctx context.Context, j dnsengineartifact.SwitchJournalV1, pid uint64) error {
	return bindInverseAuthorityAnswers(ctx, j, "pdns_server", pid)
}

func bindInverseAuthorityAnswers(ctx context.Context, j dnsengineartifact.SwitchJournalV1, process string, pid uint64) error {
	if err := dnsenginerecovery.ProbeAuthorityListeners(ctx, process, pid, "", dnsenginerecovery.SSListenerRunner); err != nil {
		return err
	}
	address, err := dnsenginerecovery.ProbeAuthorityIPv4Address(ctx, process, pid, dnsenginerecovery.SSListenerRunner)
	if err != nil {
		return err
	}
	endpoint := net.JoinHostPort(address, "53")
	for _, zone := range j.Zones {
		if zone.Delete {
			for _, network := range []string{"udp", "tcp"} {
				if err := dnswire.QueryDeletedZoneSOA(ctx, network, endpoint, zone.Domain); err != nil {
					return fmt.Errorf("deleted source zone %s over %s: %w", zone.Domain, network, err)
				}
			}
			continue
		}
		serial, err := dnsenginerecovery.FrozenSwitchSOASerial(zone)
		if err != nil {
			return err
		}
		tcp, err := dnswire.QueryAuthoritativeSOA(ctx, endpoint, zone.Domain)
		if err != nil || tcp != serial {
			return errors.Join(fmt.Errorf("source TCP SOA differs for %s", zone.Domain), err)
		}
		udp, err := dnswire.QueryAuthoritativeSOAUDP(ctx, endpoint, zone.Domain)
		if err != nil || udp != serial {
			return errors.Join(fmt.Errorf("source UDP SOA differs for %s", zone.Domain), err)
		}
	}
	return nil
}

// The publisher restores current before the rollback journal is rewritten. At
// that cut named can still serve the staged target despite a predecessor (or
// absent) current pointer. Only the exact operation-created native shape may
// enter the inverse; the immutable target and live answers are proved below.
func bindInversePredecessorActiveShape(j dnsengineartifact.SwitchJournalV1, units []dnsenginerecovery.NativeUnitObservation) error {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.Phase != dnsengineartifact.SwitchPhaseRollingBack ||
		j.InversePlan == nil || j.SourceEngine != transport.DNSEnginePowerDNS ||
		j.TargetEngine != transport.DNSEngineBIND || j.Topology != transport.DNSTopologyStandalone ||
		len(units) != 3 || units[0].Name != "named.service" || units[1].Name != "bind9.service" ||
		units[2].Name != "pdns.service" {
		return errors.New("active BIND predecessor checkpoint lacks supported frozen rollback")
	}
	for _, u := range units[:2] {
		if u.LoadState != "loaded" || u.ActiveState != "active" || u.UnitFileState != "enabled" {
			return errors.New("active BIND predecessor checkpoint lacks exact target unit state")
		}
	}
	if units[2].LoadState != "loaded" || units[2].ActiveState != "inactive" || units[2].UnitFileState != "disabled" {
		return errors.New("active BIND predecessor checkpoint lacks stopped source unit state")
	}
	return nil
}

func bindInverseExactAfterConfigs(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	states, err := dnsenginerecovery.ProbeInstalledBINDSwitchConfigCheckpointV2(ctx, policy, j, layout, gid)
	if err != nil {
		return err
	}
	if len(states) != len(j.ConfigBefore) || len(states) != len(j.InversePlan.ConfigAfter) {
		return errors.New("active BIND predecessor checkpoint lacks complete target config")
	}
	for i, state := range states {
		if state != dnsenginerecovery.BINDConfigFileAfter &&
			!(state == dnsenginerecovery.BINDConfigFileBefore && reflect.DeepEqual(j.ConfigBefore[i], j.InversePlan.ConfigAfter[i])) {
			return errors.New("active BIND predecessor checkpoint differs from exact target config")
		}
	}
	return nil
}

func bindInverseExpectedTarget(j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout) (binddns.Receipt, error) {
	zones := make([]binddns.ZoneSnapshot, len(j.Zones))
	for i, z := range j.Zones {
		zones[i] = binddns.ZoneSnapshot{
			DesiredGeneration: z.DesiredGeneration, Domain: z.Domain, Delete: z.Delete,
			Qualifier: z.ZoneQualifier, MutationRequestID: j.MutationRequestID,
			MutationOwnerID: j.MutationOwnerID, Records: z.Records,
		}
	}
	generation, err := binddns.RenderManifest(string(layout), binddns.Manifest{EngineEpoch: j.TargetEpoch, Zones: zones})
	if err != nil {
		return binddns.Receipt{}, err
	}
	if generation.ID != j.TargetGeneration {
		return binddns.Receipt{}, errors.New("frozen BIND target differs from journal generation")
	}
	return generation.ReceiptValue, nil
}

func bindInverseActivePredecessorProof(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	if err := bindInverseSourceProof(ctx, policy, j); err != nil {
		return err
	}
	if err := bindInverseUnchangedConfig(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	pointerTarget, err := bindInversePointer(ctx, j, layout, gid)
	if err != nil {
		return err
	}
	if pointerTarget {
		return errors.New("active predecessor proof found target pointer")
	}
	if err := bindInverseExactAfterConfigs(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	units, err := bindInverseUnits(ctx)
	if err != nil {
		return err
	}
	if err := bindInversePredecessorActiveShape(j, units); err != nil {
		return err
	}
	if err := verifyInstalledBINDSwitchVendorForUnits(ctx, units); err != nil {
		return err
	}
	if err := dnsenginerecovery.ProbeStoppedUnit(ctx, "pdns.service", dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdPDNSRuntimeRunner); err != nil {
		return err
	}
	beforeCatalog, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return err
	}
	publisher, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return err
	}
	expected, err := bindInverseExpectedTarget(j, layout)
	if err != nil {
		return err
	}
	tree, err := publisher.LoadGeneration(j.TargetGeneration)
	if err != nil || !reflect.DeepEqual(tree.CurrentReceipt(), expected) {
		return errors.Join(errors.New("immutable BIND target differs from frozen journal"), err)
	}
	afterCatalog, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil || beforeCatalog != afterCatalog {
		return errors.Join(errors.New("BIND catalog changed around immutable target proof"), err)
	}
	pid, err := verifyInstalledBINDRuntime(ctx)
	if err != nil {
		return err
	}
	if err := bindInverseAuthorityAnswers(ctx, j, "named", pid); err != nil {
		return err
	}
	if err := bindInverseSourceProof(ctx, policy, j); err != nil {
		return err
	}
	if err := bindInverseUnchangedConfig(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	if target, err := bindInversePointer(ctx, j, layout, gid); err != nil || target {
		return errors.Join(errors.New("BIND predecessor pointer changed during active proof"), err)
	}
	if err := bindInverseExactAfterConfigs(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	again, err := bindInverseUnits(ctx)
	if err != nil || !reflect.DeepEqual(units, again) {
		return errors.Join(errors.New("BIND units changed during active proof"), err)
	}
	if err := dnsenginerecovery.ProbeStoppedUnit(ctx, "pdns.service", dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdPDNSRuntimeRunner); err != nil {
		return err
	}
	if againPID, err := verifyInstalledBINDRuntime(ctx); err != nil || againPID != pid {
		return errors.Join(errors.New("BIND process changed during active proof"), err)
	}
	return bindInverseAuthorityAnswers(ctx, j, "named", pid)
}

func assessInstalledBINDSwitchNative(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.BINDSwitchNativeState, error) {
	return assessBINDSwitchNative(ctx, installedBINDSwitchNativeHost(policy, servicemutationledger.FileOwner{}), j)
}

func assessBINDSwitchNative(ctx context.Context, h bindSwitchNativeHost, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.BINDSwitchNativeState, error) {
	if err := h.sourceProof(ctx, j); err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	layout, gid, err := h.layout()
	if err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	if !bindInversePlanLayoutMatches(layout, j.InversePlan.HostLayout) {
		return dnsenginerecovery.BINDSwitchNativeUnknown, errors.New("installed BIND layout differs from frozen plan")
	}
	if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	target, err := h.pointer(ctx, j, layout, gid)
	if err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	configsBefore, err := h.configs(ctx, j, layout, gid)
	if err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	units, err := h.units(ctx)
	if err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	if err := bindInverseAllowedNativeUnits(units, j); err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	if err := h.vendor(ctx, j, units); err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, fmt.Errorf("BIND vendor unit: %w", err)
	}
	if !target && configsBefore && bindInverseTargetPreimage(units, j) && bindInverseSourcePreimage(units, j) {
		if err := h.stoppedTarget(ctx, j); err != nil {
			return dnsenginerecovery.BINDSwitchNativeUnknown, err
		}
		pid, err := h.pdnsRuntime(ctx)
		if err != nil {
			return dnsenginerecovery.BINDSwitchNativeUnknown, err
		}
		if err := h.answers(ctx, j, pid); err != nil {
			return dnsenginerecovery.BINDSwitchNativeUnknown, err
		}
		if err := h.sourceProof(ctx, j); err != nil {
			return dnsenginerecovery.BINDSwitchNativeUnknown, err
		}
		again, err := h.units(ctx)
		if err != nil || !reflect.DeepEqual(units, again) {
			return dnsenginerecovery.BINDSwitchNativeUnknown, errors.Join(errors.New("native units changed during source proof"), err)
		}
		if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
			return dnsenginerecovery.BINDSwitchNativeUnknown, err
		}
		return dnsenginerecovery.BINDSwitchNativeRestored, nil
	}
	if j.Phase == dnsengineartifact.SwitchPhaseRolledBack {
		return dnsenginerecovery.BINDSwitchNativeUnknown, errors.New("rolled-back checkpoint lacks native source restoration")
	}
	if !target {
		if units[0].ActiveState == "active" {
			if err := h.activePredecessor(ctx, j, layout, gid); err != nil {
				return dnsenginerecovery.BINDSwitchNativeUnknown, err
			}
		} else if err := h.stoppedTarget(ctx, j); err != nil {
			return dnsenginerecovery.BINDSwitchNativeUnknown, err
		}
	}
	if units[2].ActiveState != "inactive" && units[2].ActiveState != "active" {
		return dnsenginerecovery.BINDSwitchNativeUnknown, errors.New("source PowerDNS unit is in an unknown state")
	}
	if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, err
	}
	return dnsenginerecovery.BINDSwitchNativeNeedsRestore, nil
}

// installedBINDTargetSourceOnlyDNS is the installed source-only proof beside a
// never-started BIND target: no named process, and every public port-53
// listener belongs to the running source pdns_server MainPID, or no public
// listener exists while the source PowerDNS unit is stopped.
func installedBINDTargetSourceOnlyDNS(ctx context.Context) error {
	return proveBINDTargetSourceOnlyDNS(ctx, bindTargetSourceOnlyDNSOps{
		noNamedProcess: dnsenginerecovery.ProbeNoNamedProcess,
		sourceUnit: func(ctx context.Context) (dnsenginerecovery.NativeUnitObservation, error) {
			units, err := dnsenginerecovery.ProbeNativeUnits(ctx, []string{"pdns.service"}, dnsenginerecovery.SystemdUnitRunner)
			if err != nil {
				return dnsenginerecovery.NativeUnitObservation{}, err
			}
			return units[0], nil
		},
		sourcePID: verifyInstalledPDNSRuntime,
		listeners: dnsenginerecovery.SSListenerRunner,
	})
}

type bindTargetSourceOnlyDNSOps struct {
	noNamedProcess func(context.Context) error
	sourceUnit     func(context.Context) (dnsenginerecovery.NativeUnitObservation, error)
	sourcePID      func(context.Context) (uint64, error)
	listeners      dnsenginerecovery.BINDListenerRunner
}

// proveBINDTargetSourceOnlyDNS reuses the existing listener inventories: the
// authority proof that every public listener is the verified PowerDNS MainPID
// (with both TCP and UDP present), and the V4 no-public-listener proof for a
// stopped source. Loopback and link-local sockets are ignored by both, as by
// the fresh-install rule.
func proveBINDTargetSourceOnlyDNS(ctx context.Context, ops bindTargetSourceOnlyDNSOps) error {
	if ctx == nil || ops.noNamedProcess == nil || ops.sourceUnit == nil || ops.sourcePID == nil || ops.listeners == nil {
		return errors.New("never-started BIND target source-only proof is incomplete")
	}
	if err := ops.noNamedProcess(ctx); err != nil {
		return err
	}
	source, err := ops.sourceUnit(ctx)
	if err != nil {
		return err
	}
	switch source.ActiveState {
	case "active":
		pid, err := ops.sourcePID(ctx)
		if err != nil {
			return err
		}
		if err := dnsenginerecovery.ProbeAuthorityListeners(ctx, "pdns_server", pid, "", ops.listeners); err != nil {
			return fmt.Errorf("public port-53 listeners are not only the source PowerDNS: %w", err)
		}
	case "inactive":
		for range 2 {
			rows, err := ops.listeners(ctx)
			if err != nil {
				return err
			}
			if err := requireNoPublicDNSPort53ListenersV4(rows); err != nil {
				return err
			}
		}
	default:
		return errors.New("source PowerDNS unit is neither active nor inactive beside the never-started BIND target")
	}
	again, err := ops.sourceUnit(ctx)
	if err != nil || again != source {
		return errors.Join(errors.New("source PowerDNS unit changed during the source-only DNS proof"), err)
	}
	return ops.noNamedProcess(ctx)
}

func openBINDInverseMaskParent() (int, error) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	defer unix.Close(root)
	if _, err := bindroot.ValidateInheritedAnchor(root, "systemd mask root"); err != nil {
		return -1, err
	}
	current := root
	for _, component := range []string{"etc", "systemd", "system"} {
		next, _, openErr := bindroot.OpenInheritedAnchorAt(current, component, "systemd mask parent "+component)
		if current != root {
			unix.Close(current)
		}
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}
func bindInverseMaskParent() error {
	fd, err := openBINDInverseMaskParent()
	if err == nil {
		unix.Close(fd)
	}
	return err
}

func bindInversePersistentMask(unit string) error {
	parent, err := openBINDInverseMaskParent()
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	return bindInversePersistentMaskAt(parent, unit)
}
func bindInversePersistentMaskAt(parent int, unit string) error {
	if parent < 0 || (unit != "named.service" && unit != "bind9.service") {
		return errors.New("unsupported BIND mask identity")
	}
	fd, err := unix.Openat2(parent, unit, &unix.OpenHow{
		Flags:   uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return fmt.Errorf("open persistent BIND mask %s: %w", unit, err)
	}
	defer unix.Close(fd)
	observe := func() (unix.Stat_t, error) {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			return stat, err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFLNK || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
			return stat, errors.New("persistent BIND mask is not a root-owned single-link symlink")
		}
		return stat, nil
	}
	before, err := observe()
	if err != nil {
		return fmt.Errorf("verify %s mask identity: %w", unit, err)
	}
	target := make([]byte, len("/dev/null")+1)
	n, err := unix.Readlinkat(fd, "", target)
	if err != nil || n != len("/dev/null") || string(target[:n]) != "/dev/null" {
		return errors.Join(fmt.Errorf("BIND mask %s does not point exactly to /dev/null", unit), err)
	}
	after, err := observe()
	if err != nil || before.Dev != after.Dev || before.Ino != after.Ino ||
		before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid ||
		before.Nlink != after.Nlink || before.Ctim != after.Ctim {
		return errors.Join(fmt.Errorf("BIND mask %s changed during observation", unit), err)
	}
	return nil
}
func bindInverseVendorProofForUnits(
	units []dnsenginerecovery.NativeUnitObservation,
	maskProof func(string) error, fileProof, loadedProof func() error,
) error {
	if len(units) != 3 || maskProof == nil || fileProof == nil || loadedProof == nil ||
		units[0].Name != "named.service" || units[1].Name != "bind9.service" {
		return errors.New("BIND vendor proof lacks exact native units and readers")
	}
	masked := false
	for _, unit := range units[:2] {
		if unit.LoadState == "masked" {
			if unit.ActiveState != "inactive" || unit.UnitFileState != "masked" {
				return errors.New("BIND target has an unsupported mask state")
			}
			if err := maskProof(unit.Name); err != nil {
				return err
			}
			masked = true
		}
	}
	if masked {
		return fileProof()
	}
	return loadedProof()
}

// verifyInstalledBINDSwitchVendorForJournal adds the absent never-started
// target to the vendor proof: for a journal that froze both BIND units absent
// and both still read not-found, the typed target observation (twice, both
// absent) replaces the loaded identity that such a unit does not have. Every
// other state keeps verifyInstalledBINDSwitchVendorForUnits.
func verifyInstalledBINDSwitchVendorForJournal(ctx context.Context, j dnsengineartifact.SwitchJournalV1, units []dnsenginerecovery.NativeUnitObservation) error {
	if bindInverseAbsentTarget(units, j) {
		return verifyBINDTargetAbsent(ctx, dnsenginerecovery.SystemdBINDTargetRunner)
	}
	return verifyInstalledBINDSwitchVendorForUnits(ctx, units)
}

func bindInverseAbsentTarget(units []dnsenginerecovery.NativeUnitObservation, j dnsengineartifact.SwitchJournalV1) bool {
	if len(units) != 3 || !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) ||
		units[0].Name != "named.service" || units[1].Name != "bind9.service" {
		return false
	}
	for _, unit := range units[:2] {
		if unit.LoadState != "not-found" || unit.UnitFileState != "" || unit.ActiveState != "inactive" {
			return false
		}
	}
	return true
}

func verifyBINDTargetAbsent(ctx context.Context, runner dnsenginerecovery.BINDIdentityRunner) error {
	observations, err := dnsenginerecovery.ProbeBINDSwitchTargetObservations(ctx, runner)
	if err != nil {
		return err
	}
	for _, observation := range observations {
		if observation.State != dnsunitidentity.TargetAbsent {
			return errors.New("BIND target units are not both absent")
		}
	}
	return nil
}

func verifyInstalledBINDSwitchVendorForUnits(ctx context.Context, units []dnsenginerecovery.NativeUnitObservation) error {
	return bindInverseVendorProofForUnits(units, bindInversePersistentMask,
		func() error {
			profile, err := hostplatform.Detect()
			if err != nil {
				return err
			}
			before, err := bindroot.InspectInstalledVendor(ctx, profile)
			if err != nil {
				return err
			}
			after, err := bindroot.InspectInstalledVendor(ctx, profile)
			if err != nil {
				return err
			}
			if before != after {
				return errors.New("BIND vendor files changed around masked unit observation")
			}
			return nil
		},
		func() error { return verifyInstalledBINDVendorAndUnit(ctx) },
	)
}

func bindInverseSystemd(ctx context.Context, path string, args ...string) ([]byte, error) {
	if path != "/usr/bin/systemctl" {
		return nil, errors.New("unexpected systemctl path")
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, path, args...).CombinedOutput()
	if len(out) > 4096 {
		return nil, errors.New("systemctl output exceeds bound")
	}
	return out, err
}
func bindInverseRestoreUnitsWith(ctx context.Context, snapshots []dnsengineartifact.UnitSnapshot, maskParent func() error, run func(context.Context, string, ...string) ([]byte, error)) error {
	owned := map[string]bool{}
	for _, s := range snapshots {
		if s.LoadState != "masked" {
			owned[s.Name] = true
		}
	}
	return dnsunitrestore.Restore(ctx, snapshots, owned, dnsunitrestore.Ops{Systemctl: "/usr/bin/systemctl", VerifyMaskParent: maskParent, RunSystemd: run})
}
func restoreBINDTargetAfterUnchangedProof(prove func() error, restore func() error) error {
	if prove == nil || restore == nil {
		return errors.New("BIND target restore lacks unchanged-config proof or native operation")
	}
	if err := prove(); err != nil {
		return err
	}
	return restore()
}
func restoreInstalledBINDSwitchNative(ctx context.Context, policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner, j dnsengineartifact.SwitchJournalV1) error {
	return restoreBINDSwitchNative(ctx, installedBINDSwitchNativeHost(policy, owner), j)
}

func restoreBINDSwitchNative(ctx context.Context, h bindSwitchNativeHost, j dnsengineartifact.SwitchJournalV1) error {
	units, err := h.units(ctx)
	if err != nil {
		return err
	}
	if err := bindInverseAllowedNativeUnits(units, j); err != nil {
		return err
	}
	if err := h.sourceProof(ctx, j); err != nil {
		return err
	}
	if err := h.vendor(ctx, j, units); err != nil {
		return fmt.Errorf("BIND vendor unit: %w", err)
	}
	layout, gid, err := h.layout()
	if err != nil {
		return err
	}
	if !bindInversePlanLayoutMatches(layout, j.InversePlan.HostLayout) {
		return errors.New("BIND layout changed")
	}
	if _, err := h.pointer(ctx, j, layout, gid); err != nil {
		return err
	}
	if _, err := h.configs(ctx, j, layout, gid); err != nil {
		return err
	}
	if err := restoreBINDTargetAfterUnchangedProof(
		func() error {
			if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
				return err
			}
			current, err := h.units(ctx)
			if err != nil {
				return err
			}
			if err := bindInverseAllowedNativeUnits(current, j); err != nil {
				return err
			}
			if err := h.vendor(ctx, j, current); err != nil {
				return err
			}
			pointerTarget, err := h.pointer(ctx, j, layout, gid)
			if err != nil {
				return err
			}
			if !pointerTarget && current[0].ActiveState == "active" {
				return h.activePredecessor(ctx, j, layout, gid)
			}
			return nil
		},
		func() error {
			// A target this operation created and never activated keeps the
			// installed package and the guard's persistent mask as rollback
			// standby; dnsunitrestore would unmask and disable it because its
			// frozen preimage is absent. No systemctl call is made for it.
			current, err := h.units(ctx)
			if err != nil {
				return err
			}
			if bindInverseGuardSealedTarget(current, j) {
				return nil
			}
			return h.restoreUnits(ctx, j.TargetUnitsBefore)
		},
	); err != nil {
		return fmt.Errorf("restore target BIND unit preimage: %w", err)
	}
	if err := h.stoppedTarget(ctx, j); err != nil {
		return fmt.Errorf("prove stopped target BIND cgroup: %w", err)
	}
	units, err = h.units(ctx)
	if err != nil {
		return err
	}
	if err := bindInverseAllowedNativeUnits(units, j); err != nil {
		return err
	}
	if err := h.vendor(ctx, j, units); err != nil {
		return fmt.Errorf("BIND vendor unit after target restore: %w", err)
	}
	if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
		return err
	}
	if err := h.restoreConfigs(ctx, j, layout, gid); err != nil {
		return err
	}
	if err := h.stoppedTarget(ctx, j); err != nil {
		return err
	}
	if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
		return err
	}
	if err := h.restorePointer(j, layout); err != nil {
		return err
	}
	if target, err := h.pointer(ctx, j, layout, gid); err != nil || target {
		return errors.Join(errors.New("BIND predecessor pointer was not restored"), err)
	}
	if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
		return err
	}
	if err := h.restoreReceipt(j); err != nil {
		return err
	}
	units, err = h.units(ctx)
	if err != nil {
		return err
	}
	if err := bindInverseAllowedNativeUnits(units, j); err != nil {
		return err
	}
	if err := h.sourceProof(ctx, j); err != nil {
		return err
	}
	if err := h.unchangedConfig(ctx, j, layout, gid); err != nil {
		return err
	}
	// A source PowerDNS unit that still reads its exact frozen preimage (it
	// was never stopped: intent or target-staged) gets no systemctl call; the
	// assessment below re-proves its runtime, listeners and answers. A stopped
	// source is restored to its frozen enabled/active preimage.
	if !bindInverseSourcePreimage(units, j) {
		if err := h.restoreUnits(ctx, j.SourceUnitsBefore); err != nil {
			return fmt.Errorf("restore source PowerDNS unit preimage: %w", err)
		}
	}
	state, err := assessBINDSwitchNative(ctx, h, j)
	if err != nil || state != dnsenginerecovery.BINDSwitchNativeRestored {
		return errors.Join(errors.New("restored native PowerDNS source could not be proved"), err)
	}
	return nil
}
