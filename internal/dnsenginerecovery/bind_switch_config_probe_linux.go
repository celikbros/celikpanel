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

// ProbeInstalledBINDSwitchConfigCheckpointV2 observes the installed BIND
// config files through the fixed native paths. It is read-only and never
// authorizes a rollback; the caller still needs exact journal/ledger authority,
// worker exclusion, generation and native service proofs under both locks.
func ProbeInstalledBINDSwitchConfigCheckpointV2(
	ctx context.Context, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, serviceGID uint32,
) ([]BINDConfigFileState, error) {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open BIND config observation root: %w", err)
	}
	defer unix.Close(rootFD)
	return ProbeBINDSwitchConfigCheckpointAtV2(ctx, rootFD, policy, journal, layout, serviceGID)
}

// ProbeBINDSwitchConfigCheckpointAtV2 accepts a pre-opened root for a disposable
// fixture. No journal path is opened before the strict v2 codec and layout proof.
func ProbeBINDSwitchConfigCheckpointAtV2(
	ctx context.Context, rootFD int, policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, serviceGID uint32,
) ([]BINDConfigFileState, error) {
	if ctx == nil || rootFD < 0 || serviceGID == 0 || serviceGID > uint32(1<<31-1) {
		return nil, errors.New("BIND config checkpoint requires a trusted root, context and service group")
	}
	hostLayout := ""
	switch layout {
	case bindroot.APT:
		hostLayout = "apt"
	case bindroot.Pacman:
		hostLayout = "pacman"
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 || journal.InversePlan == nil ||
		hostLayout == "" || hostLayout != journal.InversePlan.HostLayout {
		return nil, errors.New("BIND config checkpoint lacks a matching v2 host layout")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, fmt.Errorf("validate frozen BIND inverse plan: %w", err)
	}
	if _, err := bindroot.ValidateInheritedAnchor(rootFD, "BIND config checkpoint root"); err != nil {
		return nil, err
	}
	first, firstIDs, err := readBINDSwitchConfigPass(ctx, rootFD, journal, layout, serviceGID)
	if err != nil {
		return nil, err
	}
	second, secondIDs, err := readBINDSwitchConfigPass(ctx, rootFD, journal, layout, serviceGID)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstIDs, secondIDs) {
		return nil, errors.New("BIND config bytes or file identity changed between secure observations")
	}
	states, err := ClassifyBINDSwitchConfigFilesV2(policy, journal, second)
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		if state == BINDConfigFileUnknown {
			return nil, errors.New("BIND config checkpoint contains a changed owner or unknown file")
		}
	}
	return states, ctx.Err()
}

func readBINDSwitchConfigPass(
	ctx context.Context, rootFD int, journal dnsengineartifact.SwitchJournalV1,
	layout bindroot.Layout, serviceGID uint32,
) ([]dnsengineartifact.FileSnapshot, []bindroot.FileIdentity, error) {
	mode := uint32(0o644)
	if layout == bindroot.Pacman {
		mode = 0o640
	}
	snapshots := make([]dnsengineartifact.FileSnapshot, 0, len(journal.ConfigBefore))
	identities := make([]bindroot.FileIdentity, 0, len(journal.ConfigBefore))
	for _, frozen := range journal.ConfigBefore {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		data, identity, err := bindroot.ReadExactBINDConfigAt(rootFD, layout, serviceGID, frozen.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read native BIND config %s: %w", frozen.Path, err)
		}
		snapshots = append(snapshots, dnsengineartifact.FileSnapshot{
			Path: frozen.Path, Exists: true, Mode: mode, OwnerKnown: true,
			UID: 0, GID: identity.GID, Data: data,
			SHA256: dnsengineartifact.DigestBytes(data),
		})
		identities = append(identities, identity)
	}
	return snapshots, identities, ctx.Err()
}
