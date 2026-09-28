//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/alicelik/celikpanel/internal/bindconfigrestore"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/secureconfigwriter"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// RestoreInstalledBINDSwitchConfigsV2 restores only the frozen BIND config
// preimage of an already accepted standalone PowerDNS-to-BIND rollback.
// The caller must hold the installed release and host locks, exclude the
// accepted worker, prove the exact generation pointer and target BIND units
// stopped before this effect, and reprove native state after it. This function
// grants no authority to decide a rollback or start a DNS switch.
func RestoreInstalledBINDSwitchConfigsV2(
	ctx context.Context, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, serviceGID uint32,
) error {
	return restoreInstalledBINDConfigsV2(ctx, policy, journal, layout, serviceGID, nil)
}

// RestoreInstalledBINDAdoptionConfigsV2 is the no-stop variant. Guard must
// reprove the same running owner process, original zone files and unchanged
// config before every read/replacement. It must not stop or restart services.
func RestoreInstalledBINDAdoptionConfigsV2(
	ctx context.Context, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, serviceGID uint32,
	guard func(context.Context) error,
) error {
	if guard == nil {
		return errors.New("running BIND config inverse requires its native guard")
	}
	return restoreInstalledBINDConfigsV2(ctx, policy, journal, layout, serviceGID, guard)
}

func restoreInstalledBINDConfigsV2(
	ctx context.Context, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, serviceGID uint32,
	guard func(context.Context) error,
) error {
	if ctx == nil || serviceGID == 0 || serviceGID > uint32(1<<31-1) {
		return errors.New("BIND config inverse requires a context and exact service group")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if guard != nil {
		kind, err := PlanNativeInverse(journal)
		if err != nil || kind != NativeInverseBINDRunningAdoption ||
			journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 ||
			journal.InversePlan == nil || journal.InversePlan.SourceBIND == nil ||
			journal.StateBefore.Exists || layout != bindroot.APT ||
			(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
			return errors.New("running BIND config inverse lacks a supported retained v2 rollback")
		}
		if err := guard(ctx); err != nil {
			return err
		}
	} else {
		if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 || journal.InversePlan == nil ||
			journal.Mode != transport.DNSEngineSwitchModeSwitch ||
			journal.SourceEngine != transport.DNSEnginePowerDNS ||
			journal.TargetEngine != transport.DNSEngineBIND ||
			journal.Topology != transport.DNSTopologyStandalone ||
			journal.PairRole != "" || journal.LocalIP != "" || journal.PeerIP != "" ||
			!journal.StateBefore.Exists ||
			(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
				journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
			return errors.New("BIND config inverse lacks a supported retained v2 rollback")
		}
	}
	hostLayout := ""
	switch layout {
	case bindroot.APT:
		hostLayout = "apt"
	case bindroot.Pacman:
		hostLayout = "pacman"
	}
	if hostLayout == "" || journal.InversePlan.HostLayout != hostLayout {
		return errors.New("BIND config inverse host layout differs from frozen plan")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return fmt.Errorf("validate frozen BIND config inverse: %w", err)
	}
	before := journal.ConfigBefore
	after := journal.InversePlan.ConfigAfter
	return bindconfigrestore.Restore(ctx, before, after, bindconfigrestore.Operations{
		Read: func(ctx context.Context) ([]dnsengineartifact.FileSnapshot, error) {
			if guard != nil {
				if err := guard(ctx); err != nil {
					return nil, err
				}
			}
			states, err := ProbeInstalledBINDSwitchConfigCheckpointV2(ctx, policy, journal, layout, serviceGID)
			if err != nil {
				return nil, err
			}
			return frozenBINDSwitchConfigSnapshots(journal.Phase, states, before, after)
		},
		Write: func(ctx context.Context, current, desired dnsengineartifact.FileSnapshot) error {
			if guard != nil {
				if err := guard(ctx); err != nil {
					return err
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return secureconfigwriter.Write(desired.Path, desired.Data, os.FileMode(desired.Mode), &current,
				secureconfigwriter.Options{
					RequiredOwner: &secureconfigwriter.Owner{UID: current.UID, GID: current.GID},
					ParentValidator: func(parentFD int) (unix.Stat_t, error) {
						return bindroot.InspectConfigParentFD(parentFD, layout, serviceGID)
					},
				})
		},
	})
}

func frozenBINDSwitchConfigSnapshots(
	phase string, states []BINDConfigFileState,
	before, after []dnsengineartifact.FileSnapshot,
) ([]dnsengineartifact.FileSnapshot, error) {
	if len(states) != len(before) || len(states) != len(after) {
		return nil, errors.New("BIND config checkpoint has an incomplete file set")
	}
	current := make([]dnsengineartifact.FileSnapshot, len(states))
	for i, state := range states {
		if phase == dnsengineartifact.SwitchPhaseRolledBack && state != BINDConfigFileBefore {
			return nil, errors.New("rolled-back BIND checkpoint still needs native config restoration")
		}
		switch state {
		case BINDConfigFileBefore:
			current[i] = before[i]
		case BINDConfigFileAfter:
			current[i] = after[i]
		default:
			return nil, errors.New("BIND config checkpoint includes an unknown file")
		}
	}
	return current, nil
}
