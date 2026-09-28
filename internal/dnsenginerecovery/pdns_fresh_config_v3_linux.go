//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"os"
	"reflect"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/secureconfigwriter"
	"golang.org/x/sys/unix"
)

// ProbeInstalledPDNSFreshConfigsV3 classifies each fixed native config path
// against the frozen before and after images. Mixed complete files are
// expected after a crash; an unknown file is an owner edit, never an inverse.
func ProbeInstalledPDNSFreshConfigsV3(ctx context.Context, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32) ([]PDNSTargetConfigStateV4, error) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	return ProbePDNSFreshConfigsAtV3(ctx, root, policy, journal, pdnsGID)
}

func ProbePDNSFreshConfigsAtV3(ctx context.Context, root int, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32) ([]PDNSTargetConfigStateV4, error) {
	if ctx == nil || root < 0 || pdnsGID == 0 || pdnsGID > 1<<31-1 ||
		policy.PDNSMainPath != "/etc/powerdns/pdns.conf" ||
		policy.PDNSManagedPath != "/etc/powerdns/pdns.d/celikpanel.conf" ||
		policy.PDNSClusterPath != "/etc/powerdns/pdns.d/celikpanel-cluster.conf" {
		return nil, errors.New("v3 PowerDNS config probe requires installed fixed paths and service group")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, err
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 || journal.PDNSFreshPlan == nil ||
		len(journal.ConfigBefore) != 3 || len(journal.PDNSFreshPlan.ConfigAfter) != 3 {
		return nil, errors.New("v3 fresh PowerDNS config commitment is incomplete")
	}
	before, after := journal.ConfigBefore, journal.PDNSFreshPlan.ConfigAfter
	for i := range before {
		if before[i].Path != after[i].Path {
			return nil, errors.New("v3 config path changed")
		}
		if before[i].Path == policy.PDNSMainPath &&
			((before[i].Exists && before[i].GID != 0 && before[i].GID != pdnsGID) ||
				(after[i].Exists && after[i].GID != 0 && after[i].GID != pdnsGID)) {
			return nil, errors.New("v3 PowerDNS main config group changed")
		}
	}
	if _, err := bindroot.ValidateInheritedAnchor(root, "v3 PowerDNS config proof root"); err != nil {
		return nil, err
	}
	first, ids, err := probePDNSTargetConfigPassV4(ctx, root, before, after)
	if err != nil {
		return nil, err
	}
	second, again, err := probePDNSTargetConfigPassV4(ctx, root, before, after)
	if err != nil || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(ids, again) {
		return nil, errors.Join(errors.New("v3 PowerDNS config changed during exact observation"), err)
	}
	return second, ctx.Err()
}

// RestoreInstalledPDNSFreshConfigsV3 is called only after the independent
// recovery has durably selected an exact prestart inverse. Guard re-proves
// worker exclusion and inactive/empty PowerDNS cgroup before every effect.
func RestoreInstalledPDNSFreshConfigsV3(ctx context.Context, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32, guard func(context.Context) error) error {
	if ctx == nil || guard == nil || journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Candidate == nil ||
		(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack &&
			journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable) {
		return errors.New("v3 config inverse requires a sealed prestart rollback decision")
	}
	if _, err := ProbeInstalledPDNSFreshConfigsV3(ctx, policy, journal, pdnsGID); err != nil {
		return err
	}
	return RestorePDNSTargetConfigCheckpointV4(ctx, journal.ConfigBefore, journal.PDNSFreshPlan.ConfigAfter, PDNSTargetConfigRestoreOps{
		Guard: guard,
		Read: func(ctx context.Context) ([]PDNSTargetConfigStateV4, error) {
			return ProbeInstalledPDNSFreshConfigsV3(ctx, policy, journal, pdnsGID)
		},
		Write: func(ctx context.Context, current, desired dnsengineartifact.FileSnapshot) error {
			if err := guard(ctx); err != nil {
				return err
			}
			options := secureconfigwriter.Options{ParentValidator: inspectPDNSTargetConfigParentV4}
			if current.Exists {
				options.RequiredOwner = &secureconfigwriter.Owner{UID: current.UID, GID: current.GID}
			} else {
				options.PublishedOwner = &secureconfigwriter.Owner{UID: desired.UID, GID: desired.GID}
			}
			return secureconfigwriter.Write(desired.Path, desired.Data, os.FileMode(desired.Mode), &current, options)
		},
		Remove: func(ctx context.Context, current dnsengineartifact.FileSnapshot) error {
			if err := guard(ctx); err != nil {
				return err
			}
			return removeExactPDNSTargetConfigV4(ctx, current, nil)
		},
	})
}
