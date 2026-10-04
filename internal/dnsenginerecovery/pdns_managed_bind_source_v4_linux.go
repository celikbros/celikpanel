//go:build linux

package dnsenginerecovery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// CaptureManagedBINDSourceProofV4 freezes the selected Debian BIND generation
// and its native configuration before the source is stopped. It reads no
// PowerDNS state and never changes either DNS service. The caller holds the DNS
// mutation lock and separately proves the installed BIND vendor and runtime.
func CaptureManagedBINDSourceProofV4(ctx context.Context, generation string, epoch int64, serviceGID uint32) (dnsengineartifact.ManagedBINDSourceProofV4, error) {
	var empty dnsengineartifact.ManagedBINDSourceProofV4
	if ctx == nil || ctx.Err() != nil || !dnsengineartifact.ValidGeneration(generation) || epoch < 1 || serviceGID == 0 || serviceGID > 1<<31-1 {
		return empty, errors.New("managed BIND source capture requires exact generation, epoch and service group")
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return empty, err
	}
	defer unix.Close(rootFD)
	publisher, err := binddns.NewOSPublisher(string(bindroot.APT))
	if err != nil {
		return empty, err
	}
	return captureManagedBINDSourceAtV4(ctx, rootFD, func() (binddns.Receipt, error) {
		tree, err := publisher.LoadCurrent()
		if err != nil {
			return binddns.Receipt{}, err
		}
		return tree.CurrentReceipt(), nil
	}, generation, epoch, serviceGID)
}

// VerifyManagedBINDSourceProofV4 refuses any changed generation pointer,
// receipt, native file bytes or ownership. The caller must still prove that
// named has loaded this configuration and answers the frozen zones.
func VerifyManagedBINDSourceProofV4(ctx context.Context, proof dnsengineartifact.ManagedBINDSourceProofV4, serviceGID uint32) error {
	actual, err := CaptureManagedBINDSourceProofV4(ctx, proof.Generation, proof.EngineEpoch, serviceGID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, proof) {
		return errors.New("managed BIND source generation or native configuration differs from frozen proof")
	}
	return nil
}

func captureManagedBINDSourceAtV4(ctx context.Context, rootFD int, loadReceipt func() (binddns.Receipt, error), generation string, epoch int64, serviceGID uint32) (dnsengineartifact.ManagedBINDSourceProofV4, error) {
	var empty dnsengineartifact.ManagedBINDSourceProofV4
	if ctx == nil || rootFD < 0 || loadReceipt == nil || !dnsengineartifact.ValidGeneration(generation) || epoch < 1 || serviceGID == 0 || serviceGID > 1<<31-1 {
		return empty, errors.New("managed BIND source capture is incomplete")
	}
	main, _, err := bindroot.ReadExactBINDConfigAt(rootFD, bindroot.APT, serviceGID, "/etc/bind/named.conf")
	if err != nil {
		return empty, err
	}
	if err := bindconfig.VerifyMainIncludes(string(main)); err != nil {
		return empty, err
	}
	leaf, err := bindconfig.DebianInverseMainLeaf(string(main))
	if err != nil {
		return empty, err
	}
	paths := []dnsengineartifact.FileSnapshot{
		{Path: "/etc/bind/named.conf"},
		{Path: leaf},
		{Path: "/etc/bind/named.conf.local"},
		{Path: "/etc/bind/named.conf.options"},
	}
	journal := dnsengineartifact.SwitchJournalV1{ConfigBefore: paths}
	firstConfig, firstIDs, err := readBINDSwitchConfigPass(ctx, rootFD, journal, bindroot.APT, serviceGID)
	if err != nil {
		return empty, err
	}
	firstReceipt, err := loadReceipt()
	if err != nil {
		return empty, err
	}
	if firstReceipt.Generation != generation || firstReceipt.EngineEpoch != epoch || firstReceipt.Pairing != nil {
		return empty, errors.New("managed standalone BIND source differs from frozen generation or epoch")
	}
	secondReceipt, err := loadReceipt()
	if err != nil {
		return empty, err
	}
	secondConfig, secondIDs, err := readBINDSwitchConfigPass(ctx, rootFD, journal, bindroot.APT, serviceGID)
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(firstIDs, secondIDs) || !reflect.DeepEqual(firstConfig, secondConfig) ||
		!reflect.DeepEqual(firstReceipt, secondReceipt) {
		return empty, errors.New("managed BIND source changed during frozen observation")
	}
	if err := bindconfig.VerifyMainIncludes(string(secondConfig[0].Data)); err != nil {
		return empty, err
	}
	if selected, err := bindconfig.DebianInverseMainLeaf(string(secondConfig[0].Data)); err != nil || selected != leaf {
		return empty, errors.Join(errors.New("managed BIND source include changed during observation"), err)
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(secondConfig[1].Data)); err != nil {
		return empty, err
	}
	if err := bindconfig.VerifyDebianInverseNoIncludes(string(secondConfig[3].Data)); err != nil {
		return empty, err
	}
	if err := bindconfig.VerifyExactZoneInclude(string(secondConfig[2].Data), "/var/cache/bind/celikpanel/current/zones.conf"); err != nil {
		return empty, err
	}
	encoded, err := json.Marshal(firstReceipt)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return dnsengineartifact.ManagedBINDSourceProofV4{
		Kind: "managed-bind-source/v1", HostLayout: "apt", Generation: generation,
		EngineEpoch: epoch, ReceiptSHA256: dnsengineartifact.DigestBytes(encoded),
		ConfigBefore: secondConfig,
	}, nil
}
