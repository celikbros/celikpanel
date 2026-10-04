//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/secureconfigwriter"
	"golang.org/x/sys/unix"
)

// RestoreInstalledPDNSTargetConfigsV4 restores only the frozen PowerDNS config
// preimage of an accepted staged-target rollback. The caller holds both host
// locks and excludes the worker. Guard must additionally prove that PowerDNS
// remains inactive with no process, its candidate remains exact, and the BIND source
// still has recoverable native ownership before every observed/effect boundary.
func RestoreInstalledPDNSTargetConfigsV4(ctx context.Context, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32, guard func(context.Context) error) error {
	if ctx == nil || guard == nil || pdnsGID == 0 || pdnsGID > 1<<31-1 ||
		journal.Schema != dnsengineartifact.SwitchJournalSchemaV4 || journal.PDNSTargetPlan == nil ||
		journal.PDNSTargetPlan.Candidate == nil ||
		(journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable && journal.Phase != dnsengineartifact.SwitchPhaseRolledBack) {
		return errors.New("PowerDNS target config restore requires a staged V4 rollback, guard and service group")
	}
	if policy.PDNSMainPath != "/etc/powerdns/pdns.conf" ||
		policy.PDNSManagedPath != "/etc/powerdns/pdns.d/celikpanel.conf" ||
		policy.PDNSClusterPath != "/etc/powerdns/pdns.d/celikpanel-cluster.conf" {
		return errors.New("PowerDNS target config paths differ from installed policy")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return err
	}
	before, after := journal.ConfigBefore, journal.PDNSTargetPlan.ConfigAfter
	return RestorePDNSTargetConfigCheckpointV4(ctx, before, after, PDNSTargetConfigRestoreOps{
		Guard: guard,
		Read: func(ctx context.Context) ([]PDNSTargetConfigStateV4, error) {
			states, err := ProbeInstalledPDNSTargetConfigsV4(ctx, policy, journal, pdnsGID)
			if err != nil {
				return nil, err
			}
			if journal.Phase == dnsengineartifact.SwitchPhaseRolledBack {
				for _, state := range states {
					if state != PDNSTargetConfigBeforeV4 {
						return nil, errors.New("rolled-back PowerDNS checkpoint still needs config restoration")
					}
				}
			}
			return states, nil
		},
		Write: func(ctx context.Context, current, desired dnsengineartifact.FileSnapshot) error {
			if err := guard(ctx); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
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

func inspectPDNSTargetConfigParentV4(parentFD int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(parentFD, &stat); err != nil {
		return stat, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o7777 != 0o755 || stat.Nlink < 2 {
		return stat, errors.New("PowerDNS config parent differs from root-owned vendor directory")
	}
	if err := bindroot.RejectACL(parentFD, "PowerDNS config parent"); err != nil {
		return stat, err
	}
	return stat, nil
}

// removeExactPDNSTargetConfigV4 deletes only a frozen after-created file. Its
// secure open, second observation, parent reproof and post-unlink directory
// fsync permit idempotent replay after a process interruption. There is no
// wildcard or path discovery. beforeFinal is used only by fault tests.
func removeExactPDNSTargetConfigV4(ctx context.Context, expected dnsengineartifact.FileSnapshot, beforeFinal func()) error {
	if ctx == nil || ctx.Err() != nil || !expected.Exists ||
		(expected.Path != "/etc/powerdns/pdns.conf" && expected.Path != "/etc/powerdns/pdns.d/celikpanel.conf" && expected.Path != "/etc/powerdns/pdns.d/celikpanel-cluster.conf") {
		return errors.New("PowerDNS config removal requires an exact fixed-path after-image")
	}
	if err := dnsengineartifact.ValidateFileSnapshotIntegrity(expected); err != nil {
		return err
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	if _, err := bindroot.ValidateInheritedAnchor(rootFD, "PowerDNS config removal root"); err != nil {
		return err
	}
	return removeExactPDNSTargetConfigAtV4(ctx, rootFD, expected, beforeFinal)
}

func removeExactPDNSTargetConfigAtV4(ctx context.Context, rootFD int, expected dnsengineartifact.FileSnapshot, beforeFinal func()) error {
	if ctx == nil || rootFD < 0 || !expected.Exists {
		return errors.New("PowerDNS config removal lacks exact root and after-image")
	}
	opened, components, err := pdnsConfigParent(rootFD, expected.Path)
	if err != nil {
		return err
	}
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = unix.Close(opened[i])
		}
	}()
	parentFD := opened[len(opened)-1]
	if _, err := inspectPDNSTargetConfigParentV4(parentFD); err != nil {
		return err
	}
	first, err := probePDNSAdoptionConfigFile(ctx, rootFD, expected)
	if err != nil {
		return err
	}
	if beforeFinal != nil {
		beforeFinal()
	}
	second, err := probePDNSAdoptionConfigFile(ctx, rootFD, expected)
	if err != nil || first != second {
		return errors.Join(errors.New("PowerDNS config changed before exact removal"), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reprovePDNSConfigParent(rootFD, parentFD, components); err != nil {
		return err
	}
	leaf := filepath.Base(expected.Path)
	fd, err := unix.Openat2(parentFD, leaf, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return fmt.Errorf("reopen PowerDNS config before removal: %w", err)
	}
	var openedStat, namedStat unix.Stat_t
	statErr := unix.Fstat(fd, &openedStat)
	closeErr := unix.Close(fd)
	if err := errors.Join(statErr, closeErr); err != nil {
		return err
	}
	if configIdentity(openedStat) != second {
		return errors.New("PowerDNS config file identity changed before removal")
	}
	if err := unix.Fstatat(parentFD, leaf, &namedStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if configIdentity(namedStat) != second {
		return errors.New("PowerDNS config path changed before removal")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reprovePDNSConfigParent(rootFD, parentFD, components); err != nil {
		return err
	}
	if err := unix.Unlinkat(parentFD, leaf, 0); err != nil {
		return fmt.Errorf("remove exact PowerDNS config %s: %w", expected.Path, err)
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync PowerDNS config directory after removal: %w", err)
	}
	return nil
}
