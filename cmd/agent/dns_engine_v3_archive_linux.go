//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

const freshPrimaryArchivePrefixV3 = "dns-engine-switch-v3-archive-"

func freshPrimaryArchivePathV3(j dnsEngineSwitchJournal, state dnsEngineStateReceipt) (string, error) {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV3 || j.Phase != dnsSwitchPhaseCommitted ||
		!dnsengineartifact.ExactSwitchTargetStateV1(state, j) ||
		dnsJournalPolicy().ValidateSwitchJournal(j) != nil {
		return "", errors.New("v3 archive requires exact committed journal and native state")
	}
	raw, err := dnsengineartifact.CanonicalStateDocumentV3(state)
	if err != nil {
		return "", err
	}
	return filepath.Join(serviceMutationStateDirectory(), freshPrimaryArchivePrefixV3+j.MutationRequestID+"-"+dnsengineartifact.DigestBytes(raw)+".json"), nil
}

func verifyFreshPrimaryArtifactsAbsentV3(j dnsEngineSwitchJournal) error {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV3 || j.PDNSFreshPlan == nil ||
		j.PDNSFreshPlan.Native == nil || dnsJournalPolicy().ValidateSwitchJournal(j) != nil {
		return errors.New("v3 retirement lacks committed native evidence")
	}
	if err := verifyPDNSTargetStageFilesystemV4(j.PDNSCandidatePath); err != nil {
		return err
	}
	for _, path := range []string{j.PDNSCandidatePath, j.PDNSBackupPath} {
		if err := verifyFreshPrimaryArtifactAbsentAtV3(path); err != nil {
			return err
		}
	}
	return nil
}

// Descriptor-pinned sibling enumeration proves both the named artifact and
// every SQLite sidecar or unknown database-prefixed file are absent.
func verifyFreshPrimaryArtifactAbsentAtV3(path string) error {
	parent, base := filepath.Dir(path), filepath.Base(path)
	open := func() (int, error) {
		return unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{
			Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
			Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
		})
	}
	fd, err := open()
	if err != nil {
		return fmt.Errorf("open v3 artifact parent: %w", err)
	}
	file := os.NewFile(uintptr(fd), parent)
	if file == nil {
		unix.Close(fd)
		return errors.New("v3 artifact parent descriptor unavailable")
	}
	defer file.Close()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return err
	}
	names, err := file.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == base || strings.HasPrefix(name, base+"-") || strings.HasPrefix(name, base+".") {
			return fmt.Errorf("v3 retirement artifact remains: %s", filepath.Join(parent, name))
		}
	}
	if err := unix.Fstat(fd, &after); err != nil || !sameSecureConfigStat(before, after) {
		return errors.New("v3 artifact directory changed during absence proof")
	}
	again, err := open()
	if err != nil {
		return err
	}
	defer unix.Close(again)
	if err := unix.Fstat(again, &named); err != nil || !sameSecureConfigStat(before, named) {
		return errors.New("v3 artifact directory was replaced")
	}
	return nil
}
func archiveCommittedFreshPrimaryV3(ctx context.Context, j dnsEngineSwitchJournal, state dnsEngineStateReceipt) error {
	path, err := freshPrimaryArchivePathV3(j, state)
	if err != nil {
		return err
	}
	raw, err := dnsJournalPolicy().EncodeSwitchJournal(j)
	if err != nil {
		return err
	}
	owner := servicemutationledger.FileOwner{UID: dnsJournalPolicy().StateUID, GID: dnsJournalPolicy().StateGID}
	if err := verifyFreshPrimaryArtifactsAbsentV3(j); err != nil {
		return err
	}
	if err := servicemutationledger.ArchiveFileExact(dnsEngineSwitchJournalPath(), path, raw, dnsengineartifact.SwitchJournalLimit, owner); err != nil {
		return err
	}
	return verifyFreshPrimaryArchivedV3(ctx, j, state)
}

func verifyFreshPrimaryArchivedV3(ctx context.Context, j dnsEngineSwitchJournal, state dnsEngineStateReceipt) error {
	path, err := freshPrimaryArchivePathV3(j, state)
	if err != nil {
		return err
	}
	owner := servicemutationledger.FileOwner{UID: dnsJournalPolicy().StateUID, GID: dnsJournalPolicy().StateGID}
	raw, exists, err := servicemutationledger.ReadFile(path, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil || !exists {
		return errors.Join(errors.New("v3 committed archive is missing or unsafe"), err)
	}
	encoded, err := dnsJournalPolicy().EncodeSwitchJournal(j)
	if err != nil || !bytes.Equal(raw, encoded) {
		return errors.Join(errors.New("v3 committed archive differs"), err)
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	if err := verifyFreshPDNSNativeVersionV3(ctx, profile); err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyRecordedFreshPrimaryNativeV3(ctx, dnsJournalPolicy(), j); err != nil {
		return err
	}
	return verifyFreshPrimaryArtifactsAbsentV3(j)
}

func exactArchivedFreshPrimaryProvenanceV3(ctx context.Context, target transport.DNSEngine, qualifier string, binding transport.ServiceMutationBinding) (bool, bool, error) {
	state, exists, err := readDNSEngineState()
	if err != nil || !exists || state.NativeCatalogV3 == "" {
		return false, false, err
	}
	if state.MutationRequestID != binding.MutationRequestID {
		return false, false, nil
	}
	if state.Engine != target || state.ManifestQualifier != qualifier || state.MutationOwnerID != binding.MutationOwnerID {
		return false, true, errors.New("v3 journal-free request differs from native state")
	}
	// The filename is derived from canonical state, then the exact canonical
	// journal is decoded and re-bound to the current host and native database.
	stateRaw, err := dnsengineartifact.CanonicalStateDocumentV3(state)
	if err != nil {
		return false, true, err
	}
	path := filepath.Join(serviceMutationStateDirectory(), freshPrimaryArchivePrefixV3+binding.MutationRequestID+"-"+dnsengineartifact.DigestBytes(stateRaw)+".json")
	owner := servicemutationledger.FileOwner{UID: dnsJournalPolicy().StateUID, GID: dnsJournalPolicy().StateGID}
	raw, archiveExists, err := servicemutationledger.ReadFile(path, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil || !archiveExists {
		return false, true, errors.Join(errors.New("v3 journal-free archive is missing or unsafe"), err)
	}
	j, err := dnsJournalPolicy().DecodeSwitchJournal(raw)
	if err != nil || j.Schema != dnsengineartifact.SwitchJournalSchemaV3 || j.Phase != dnsSwitchPhaseCommitted ||
		!exactSwitchJournalIdentity(j, target, qualifier, binding) || !dnsengineartifact.ExactSwitchTargetStateV1(state, j) {
		return false, true, errors.Join(errors.New("v3 journal-free archive is foreign"), err)
	}
	if err := verifyFreshPrimaryArchivedV3(ctx, j, state); err != nil {
		return false, true, err
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return false, true, err
	}
	_, install, ownership, err := exactCommittedDNSEngineProvenanceOnHost(j, profile)
	if err != nil || install || !ownership {
		return false, true, errors.Join(errors.New("v3 archived ownership handoff is incomplete"), err)
	}
	if err := verifyDNSSwitchJournalTarget(ctx, j); err != nil {
		return false, true, err
	}
	return true, true, nil
}
