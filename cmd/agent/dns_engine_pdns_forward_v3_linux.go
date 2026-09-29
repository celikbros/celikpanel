//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/processidentity"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

type freshPrimaryForwardObservationV3 struct {
	live         pdnsnative.Snapshot
	state        *dnsEngineStateReceipt
	topology     pdnsRuntimeTopologySnapshot
	processStart string
}

// recoverFreshPrimaryForwardV3 is entered only by the accepted operation's
// locked, worker-excluding recovery caller. Every effect is preceded by a new
// native and authority proof and followed by a new proof. It never starts,
// stops, changes SQL, or removes a serving target.
func recoverFreshPrimaryForwardV3(ctx context.Context, id dnsengineartifact.SwitchIdentity, journal dnsEngineSwitchJournal) (dnsEngineSwitchRecoveryOutcome, error) {
	policy := dnsJournalPolicy()
	if ctx == nil || ctx.Err() != nil || id.Validate() != nil ||
		policy.ValidateSwitchJournal(journal) != nil || journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		journal.MutationRequestID != id.RequestID || journal.MutationOwnerID != id.OwnerID ||
		journal.TargetEngine != id.Target || journal.ManifestQualifier != id.Qualifier {
		return dnsenginerecovery.OutcomeAbsent, errors.New("v3 forward recovery lacks the exact accepted journal")
	}
	switch journal.Phase {
	case dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseTargetStarted,
		dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted:
	default:
		return dnsenginerecovery.OutcomeAbsent, freshPrimaryPrestartRefusalV3(journal, "v3 phase has no poststart forward authority")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return dnsenginerecovery.OutcomeAbsent, err
	}
	if err = verifyFreshPDNSNativeVersionV3(ctx, profile); err != nil {
		return dnsenginerecovery.OutcomeAbsent, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return dnsenginerecovery.OutcomeAbsent, err
	}
	return runFreshPrimaryForwardV3(ctx, journal, freshPrimaryForwardOpsV3{
		read: readDNSEngineSwitchJournal,
		observe: func(c context.Context, j dnsEngineSwitchJournal) (freshPrimaryForwardObservationV3, error) {
			return observeFreshPrimaryForwardV3(c, profile, systemctl, j)
		},
		plan: func(j dnsEngineSwitchJournal, o freshPrimaryForwardObservationV3) (dnsengineartifact.FreshPrimaryForwardPlanV3, error) {
			return dnsengineartifact.PlanObservedFreshPrimaryForwardV3(policy, j, id, o.live, o.state, true, j.PDNSFreshPlan.Native != nil)
		},
		checkpoint: func(a, b dnsEngineSwitchJournal) error {
			return dnsenginerecovery.ReplaceFreshPrimaryJournalV3(policy, servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}, a, b)
		},
		publish: publishExactFreshPrimaryStateV3,
		verify:  verifyDNSSwitchJournalTarget,
	})
}

type freshPrimaryForwardOpsV3 struct {
	read       func() (dnsEngineSwitchJournal, bool, error)
	observe    func(context.Context, dnsEngineSwitchJournal) (freshPrimaryForwardObservationV3, error)
	plan       func(dnsEngineSwitchJournal, freshPrimaryForwardObservationV3) (dnsengineartifact.FreshPrimaryForwardPlanV3, error)
	checkpoint func(dnsEngineSwitchJournal, dnsEngineSwitchJournal) error
	publish    func(dnsEngineSwitchJournal, dnsEngineStateReceipt) error
	verify     func(context.Context, dnsEngineSwitchJournal) error
}

func runFreshPrimaryForwardV3(ctx context.Context, journal dnsEngineSwitchJournal, ops freshPrimaryForwardOpsV3) (dnsEngineSwitchRecoveryOutcome, error) {
	if ctx == nil || ops.read == nil || ops.observe == nil || ops.plan == nil || ops.checkpoint == nil || ops.publish == nil || ops.verify == nil {
		return dnsenginerecovery.OutcomeAbsent, errors.New("v3 forward recovery operations are incomplete")
	}
	for step := 0; step < 6; step++ {
		if err := ctx.Err(); err != nil {
			return dnsenginerecovery.OutcomeAbsent, err
		}
		current, exists, err := ops.read()
		if err != nil || !exists || !reflect.DeepEqual(current, journal) {
			return dnsenginerecovery.OutcomeAbsent, errors.Join(errors.New("v3 journal changed during forward recovery"), err)
		}
		before, err := ops.observe(ctx, journal)
		if err != nil {
			return dnsenginerecovery.OutcomeAbsent, err
		}
		plan, err := ops.plan(journal, before)
		if err != nil {
			return dnsenginerecovery.OutcomeAbsent, err
		}
		if plan.Action == dnsengineartifact.FreshPrimaryRetainCommittedV3 {
			if err := ops.verify(ctx, journal); err != nil {
				return dnsenginerecovery.OutcomeAbsent, err
			}
			return dnsenginerecovery.OutcomeCommitted, nil
		}
		current, exists, err = ops.read()
		if err != nil || !exists || !reflect.DeepEqual(current, journal) {
			return dnsenginerecovery.OutcomeAbsent, errors.Join(errors.New("v3 journal changed before checkpoint"), err)
		}
		switch plan.Action {
		case dnsengineartifact.FreshPrimaryPublishStateV3:
			if plan.DesiredState == nil {
				return dnsenginerecovery.OutcomeAbsent, errors.New("v3 state proposal is absent")
			}
			if err := ops.publish(journal, *plan.DesiredState); err != nil {
				return dnsenginerecovery.OutcomeAbsent, err
			}
		case dnsengineartifact.FreshPrimaryCheckpointStartedV3, dnsengineartifact.FreshPrimaryCheckpointNativeV3, dnsengineartifact.FreshPrimaryCheckpointVerifiedV3, dnsengineartifact.FreshPrimaryCheckpointCommittedV3:
			if err := ops.checkpoint(journal, plan.NextJournal); err != nil {
				return dnsenginerecovery.OutcomeAbsent, err
			}
			journal = plan.NextJournal
		default:
			return dnsenginerecovery.OutcomeAbsent, errors.New("v3 forward proposal has an unsupported effect")
		}
		after, err := ops.observe(ctx, journal)
		if err != nil {
			return dnsenginerecovery.OutcomeAbsent, err
		}
		if !reflect.DeepEqual(before.topology, after.topology) || !reflect.DeepEqual(before.live, after.live) || before.processStart != after.processStart {
			return dnsenginerecovery.OutcomeAbsent, errors.New("v3 native daemon or SQL changed around checkpoint")
		}
	}
	return dnsenginerecovery.OutcomeAbsent, errors.New("v3 forward recovery exceeded its bounded checkpoints")
}

// An unchanged native file can match both frozen images. The secure probe
// reports "before" first; that is still the prepared target only when the
// complete frozen snapshots are identical. A changed file left at "before"
// remains pending, and an unknown file is never accepted.
func verifyFreshPrimaryPreparedConfigsV3(configs []dnsenginerecovery.PDNSTargetConfigStateV4, j dnsEngineSwitchJournal) error {
	if j.PDNSFreshPlan == nil || len(configs) != len(j.ConfigBefore) || len(configs) != len(j.PDNSFreshPlan.ConfigAfter) {
		return errors.New("v3 target config count changed")
	}
	for i, config := range configs {
		if config == dnsenginerecovery.PDNSTargetConfigAfterV4 {
			continue
		}
		if config == dnsenginerecovery.PDNSTargetConfigBeforeV4 &&
			reflect.DeepEqual(j.ConfigBefore[i], j.PDNSFreshPlan.ConfigAfter[i]) {
			continue
		}
		return errors.New("v3 native PowerDNS config differs from prepared target")
	}
	return nil
}

func observeFreshPrimaryForwardV3(ctx context.Context, profile hostplatform.Profile, systemctl string, j dnsEngineSwitchJournal) (freshPrimaryForwardObservationV3, error) {
	var empty freshPrimaryForwardObservationV3
	if err := verifyFreshPDNSNativeVersionV3(ctx, profile); err != nil {
		return empty, err
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return empty, err
	}
	first, err := inspectVerifiedPDNSRuntimeTopology(ctx, profile, systemctl)
	if err != nil {
		return empty, err
	}
	started, err := freshPrimaryProcessIdentityV3(first.pdnsProcesses.MainPID)
	if err != nil {
		return empty, err
	}
	gid, err := resolvePDNSGroupGID(ctx)
	if err != nil {
		return empty, err
	}
	configs, err := dnsenginerecovery.ProbeInstalledPDNSFreshConfigsV3(ctx, dnsJournalPolicy(), j, gid)
	if err != nil {
		return empty, freshPrimaryV3Changed(freshPrimaryV3ChangedConfig, err)
	}
	if err := verifyFreshPrimaryPreparedConfigsV3(configs, j); err != nil {
		return empty, freshPrimaryV3Changed(freshPrimaryV3ChangedConfig, err)
	}
	live, err := dnsenginerecovery.CaptureFreshPrimaryNativeV3(ctx, dnsJournalPolicy(), j)
	if err != nil {
		// A capture that could not complete (for example a busy database) is
		// an unknown observation, not a change; only a content mismatch below
		// is reported as a change.
		return empty, err
	}
	snapshot, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return empty, err
	}
	if snapshot.Path != j.StateBefore.Path {
		return empty, errors.New("v3 state path differs from frozen intent")
	}
	var state *dnsEngineStateReceipt
	if snapshot.Exists {
		decoded, err := decodeDNSEngineState(snapshot.Data)
		if err != nil {
			return empty, err
		}
		state = &decoded
	}
	manifest, err := switchJournalManifest(j)
	if err != nil {
		return empty, err
	}
	// The candidate snapshot and live SQL are checked by the pure planner. For
	// authority verification before the native checkpoint, derive only an
	// ephemeral state from the observed SQL; never write that proposal.
	observed := freshPrimaryNativeObservationViewV3(j)
	if observed.PDNSFreshPlan.Native == nil {
		if err := dnsJournalPolicy().ValidateSwitchJournal(observed); err != nil {
			return empty, err
		}
		domain, err := binddns.CatalogDomain(j.LocalIP)
		if err != nil {
			return empty, err
		}
		observed, err = dnsJournalPolicy().AttachPDNSFreshNativeObservationV3(observed, domain, live)
		if err != nil {
			// Only the measured daemon start-up transform is admitted.
			return empty, freshPrimaryV3Changed(freshPrimaryV3ChangedDatabase, err)
		}
	} else {
		domain, err := binddns.CatalogDomain(j.LocalIP)
		if err != nil {
			return empty, err
		}
		if err := pdnsnative.VerifyRecordedFreshPrimaryCatalogTransition(
			*j.PDNSFreshPlan.Staged, live, domain, j.PrimaryCatalogSerial, *j.PDNSFreshPlan.Native,
		); err != nil {
			return empty, freshPrimaryV3Changed(freshPrimaryV3ChangedDatabase, err)
		}
	}
	desired, err := dnsengineartifact.FreshPrimaryTargetStateV3(observed)
	if err != nil {
		return empty, err
	}
	if state != nil && *state != desired {
		return empty, freshPrimaryV3Changed(freshPrimaryV3ChangedState, errors.New("v3 owner-edited state differs from observed target"))
	}
	ownership, ownershipExists, err := readDNSEngineOwnership(transport.DNSEnginePowerDNS)
	if err != nil {
		return empty, err
	}
	if ownershipExists && ownership != desired {
		return empty, freshPrimaryV3Changed(freshPrimaryV3ChangedState, errors.New("v3 owner-edited ownership differs from observed target"))
	}
	if err := verifyManagedPDNSPairIdentity(manifest, desired); err != nil {
		return empty, err
	}
	if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
		return empty, err
	}
	if err := verifyPDNSPairingAuthority(ctx, manifest); err != nil {
		return empty, err
	}
	domain, err := binddns.CatalogDomain(j.LocalIP)
	if err != nil {
		return empty, err
	}
	catalog, err := probeDNSPDNSCatalogAXFR(ctx, j.LocalIP, domain)
	if err != nil {
		return empty, err
	}
	if catalog.Serial != desired.PrimaryCatalogSerial {
		return empty, errors.New("v3 local catalog serial differs from native SQL")
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return empty, err
	}
	last, err := inspectVerifiedPDNSRuntimeTopology(ctx, profile, systemctl)
	if err != nil || !reflect.DeepEqual(first, last) {
		return empty, errors.Join(errors.New("v3 daemon or cgroup changed during authority observation"), err)
	}
	again, err := freshPrimaryProcessIdentityV3(last.pdnsProcesses.MainPID)
	if err != nil || again != started {
		return empty, errors.Join(errors.New("v3 PowerDNS process executable or start token changed"), err)
	}
	return freshPrimaryForwardObservationV3{live: live, state: state, topology: last, processStart: again}, nil
}

func publishExactFreshPrimaryStateV3(j dnsEngineSwitchJournal, desired dnsEngineStateReceipt) error {
	if j.PDNSFreshPlan == nil || j.PDNSFreshPlan.Native == nil ||
		j.StateBefore.Exists || !dnsengineartifact.ExactSwitchTargetStateV1(desired, j) {
		return errors.New("v3 state publication lacks frozen native evidence")
	}
	before := j.StateBefore
	actual, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, before) {
		return errors.New("v3 state preimage changed before publication")
	}
	encoded, err := dnsengineartifact.CanonicalStateDocumentV3(desired)
	if err != nil {
		return err
	}
	err = secureWriteConfigReplacingSnapshotWithOwner(before.Path, encoded, 0o600, &before,
		serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID)
	readback, exists, readErr := readExactDNSEngineState()
	if readErr != nil || !exists || readback != desired {
		return errors.Join(errors.New("v3 state publication has no exact readback"), err, readErr)
	}
	return nil
}
func freshPrimaryProcessIdentityV3(pid uint64) (string, error) {
	if pid == 0 || uint64(int(pid)) != pid {
		return "", errors.New("v3 PowerDNS process identity is invalid")
	}
	started, err := processidentity.StartToken(int(pid))
	if err != nil {
		return "", err
	}
	installed, err := os.Lstat("/usr/sbin/pdns_server")
	if err != nil || !installed.Mode().IsRegular() {
		return "", errors.New("v3 installed PowerDNS executable is unsafe")
	}
	running, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || !os.SameFile(installed, running) {
		return "", errors.New("v3 running PowerDNS executable differs from measured package")
	}
	again, err := processidentity.StartToken(int(pid))
	if err != nil || again != started {
		return "", errors.New("v3 PowerDNS process changed while inspected")
	}
	return started, nil
}

func freshPrimaryNativeObservationViewV3(j dnsEngineSwitchJournal) dnsEngineSwitchJournal {
	view := j
	// The enable-intent may already have started PowerDNS. Only the
	// observation uses TargetStarted; the durable journal remains unchanged.
	if view.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent &&
		view.PDNSFreshPlan != nil && view.PDNSFreshPlan.Native == nil {
		view.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	}
	return view
}
