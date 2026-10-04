//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// CaptureInstalledBINDUnchangedConfigV2 records evidence only; these native
// files are never owned or rewritten by the inverse.
func CaptureInstalledBINDUnchangedConfigV2(ctx context.Context, layout bindroot.Layout, gid uint32) ([]dnsengineartifact.FileSnapshot, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	return captureBINDUnchangedConfigAtV2(ctx, fd, layout, gid)
}

func captureBINDUnchangedConfigAtV2(ctx context.Context, fd int, layout bindroot.Layout, gid uint32) ([]dnsengineartifact.FileSnapshot, error) {
	if ctx == nil || fd < 0 || layout != bindroot.APT || gid == 0 || gid > 1<<31-1 {
		return nil, errors.New("BIND unchanged config requires a trusted Debian root and service group")
	}
	main, _, err := bindroot.ReadExactBINDConfigAt(fd, layout, gid, "/etc/bind/named.conf")
	if err != nil {
		return nil, err
	}
	leaf, err := bindconfig.DebianInverseMainLeaf(string(main))
	if err != nil {
		return nil, err
	}
	return readBINDUnchangedConfigAtV2(ctx, fd, layout, gid, leaf)
}

func readBINDUnchangedConfigAtV2(ctx context.Context, fd int, layout bindroot.Layout, gid uint32, leaf string) ([]dnsengineartifact.FileSnapshot, error) {
	if ctx == nil || fd < 0 || layout != bindroot.APT || gid == 0 || gid > 1<<31-1 ||
		(leaf != "/etc/bind/named.conf.default-zones" && leaf != "/etc/bind/named.conf.root-hints") {
		return nil, errors.New("BIND unchanged config lacks a fixed supported Debian leaf")
	}
	paths := []dnsengineartifact.FileSnapshot{{Path: "/etc/bind/named.conf"}, {Path: leaf}}
	j := dnsengineartifact.SwitchJournalV1{ConfigBefore: paths}
	first, ids, err := readBINDSwitchConfigPass(ctx, fd, j, layout, gid)
	if err != nil {
		return nil, err
	}
	second, nextIDs, err := readBINDSwitchConfigPass(ctx, fd, j, layout, gid)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(ids, nextIDs) {
		return nil, errors.New("BIND unchanged configuration moved during observation")
	}
	selected, err := bindconfig.DebianInverseMainLeaf(string(second[0].Data))
	if err != nil || selected != leaf {
		return nil, errors.Join(errors.New("BIND main differs from frozen Debian include layout"), err)
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(second[1].Data)); err != nil {
		return nil, err
	}
	return second, ctx.Err()
}

// VerifyInstalledBINDUnchangedConfigV2 rechecks exact bytes and metadata before
// native effects. Changed owner files cause refusal, never compensation.
func VerifyInstalledBINDUnchangedConfigV2(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return verifyBINDUnchangedConfigAtV2(ctx, fd, policy, j, layout, gid)
}

func verifyBINDUnchangedConfigAtV2(ctx context.Context, fd int, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil ||
		j.InversePlan.HostLayout != "apt" || len(j.InversePlan.BINDUnchangedConfig) != 2 {
		return errors.New("BIND inverse lacks unchanged native configuration evidence")
	}
	if err := policy.ValidateSwitchJournal(j); err != nil {
		return fmt.Errorf("validate BIND unchanged evidence: %w", err)
	}
	actual, err := readBINDUnchangedConfigAtV2(ctx, fd, layout, gid, j.InversePlan.BINDUnchangedConfig[1].Path)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, j.InversePlan.BINDUnchangedConfig) {
		return errors.New("BIND main or default-zone configuration changed; preserve owner configuration")
	}
	return nil
}
