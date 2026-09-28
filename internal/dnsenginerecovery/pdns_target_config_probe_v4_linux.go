//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// PDNSTargetConfigStateV4 is an exact per-file checkpoint. Mixed complete files
// are allowed during owner-aware publication; an unknown file is never restored.
type PDNSTargetConfigStateV4 uint8

const (
	PDNSTargetConfigUnknownV4 PDNSTargetConfigStateV4 = iota
	PDNSTargetConfigBeforeV4
	PDNSTargetConfigAfterV4
)

// ProbeInstalledPDNSTargetConfigsV4 observes the fixed installed paths without
// changing them. The caller holds release and host locks and validates the
// accepted operation separately.
func ProbeInstalledPDNSTargetConfigsV4(ctx context.Context, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32) ([]PDNSTargetConfigStateV4, error) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	return ProbePDNSTargetConfigsAtV4(ctx, root, policy, journal, pdnsGID)
}

// ProbePDNSTargetConfigsAtV4 permits a disposable filesystem fixture. Every
// candidate is opened through the existing no-follow owner-aware observer.
func ProbePDNSTargetConfigsAtV4(ctx context.Context, root int, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32) ([]PDNSTargetConfigStateV4, error) {
	if ctx == nil || root < 0 || pdnsGID == 0 || pdnsGID > 1<<31-1 {
		return nil, errors.New("PowerDNS target config probe requires trusted root, context and group")
	}
	if policy.PDNSMainPath != "/etc/powerdns/pdns.conf" || policy.PDNSManagedPath != "/etc/powerdns/pdns.d/celikpanel.conf" || policy.PDNSClusterPath != "/etc/powerdns/pdns.d/celikpanel-cluster.conf" {
		return nil, errors.New("PowerDNS target config paths differ from installed policy")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, err
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV4 || journal.PDNSTargetPlan == nil || journal.PDNSTargetPlan.Candidate == nil {
		return nil, errors.New("PowerDNS target lacks staged V4 evidence")
	}
	before, after := journal.ConfigBefore, journal.PDNSTargetPlan.ConfigAfter
	if len(before) != 3 || len(after) != 3 {
		return nil, errors.New("PowerDNS target config path set is incomplete")
	}
	for i := range before {
		if before[i].Path != after[i].Path {
			return nil, errors.New("PowerDNS target config path sets differ")
		}
		if before[i].Path == policy.PDNSMainPath && ((before[i].Exists && before[i].GID != 0 && before[i].GID != pdnsGID) || (after[i].Exists && after[i].GID != 0 && after[i].GID != pdnsGID)) {
			return nil, errors.New("PowerDNS main config group differs from installed group")
		}
	}
	if _, err := bindroot.ValidateInheritedAnchor(root, "PowerDNS target config proof root"); err != nil {
		return nil, err
	}
	first, ids, err := probePDNSTargetConfigPassV4(ctx, root, before, after)
	if err != nil {
		return nil, err
	}
	second, again, err := probePDNSTargetConfigPassV4(ctx, root, before, after)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(ids, again) {
		return nil, errors.New("PowerDNS target config changed between secure observations")
	}
	return second, ctx.Err()
}

func probePDNSTargetConfigPassV4(ctx context.Context, root int, before, after []dnsengineartifact.FileSnapshot) ([]PDNSTargetConfigStateV4, []pdnsConfigReadIdentity, error) {
	states := make([]PDNSTargetConfigStateV4, len(before))
	identities := make([]pdnsConfigReadIdentity, len(before))
	for i := range before {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		identity, err := probePDNSAdoptionConfigFile(ctx, root, before[i])
		if err == nil {
			states[i], identities[i] = PDNSTargetConfigBeforeV4, identity
			continue
		}
		identity, err = probePDNSAdoptionConfigFile(ctx, root, after[i])
		if err != nil {
			return nil, nil, fmt.Errorf("PowerDNS target config %s differs from both frozen images: %w", before[i].Path, err)
		}
		states[i], identities[i] = PDNSTargetConfigAfterV4, identity
	}
	return states, identities, nil
}
