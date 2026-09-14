//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/binddns"
	"golang.org/x/sys/unix"
)

func verifyExistingManagedBINDGenerationForPreflight(ctx context.Context, state dnsEngineStateReceipt) error {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return err
	}
	if layout.GenerationRoot != aptBINDGenerationRoot {
		return verifyExistingManagedBINDGenerationForSignedUpdate(ctx, state)
	}
	bindGID, err := resolveBINDGroupGID(ctx)
	if err != nil {
		return err
	}
	statoverride, err := firstTrustedExecutable([]string{"/usr/sbin/dpkg-statoverride", "/usr/bin/dpkg-statoverride"}, "dpkg-statoverride")
	if err != nil {
		return err
	}
	dpkgQuery, err := firstTrustedExecutable([]string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"}, "dpkg-query")
	if err != nil {
		return err
	}
	proofCtx, cancel := context.WithTimeout(ctx, aptBINDStatOverrideTimeout)
	defer cancel()
	ops, err := aptBINDStatOverrideOperations(proofCtx, statoverride, dpkgQuery,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = aptBINDStatOverrideCommandEnvironment()
			return command.CombinedOutputLimited(aptBINDStatOverrideOutputLimit)
		})
	if err != nil {
		return err
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	return verifyAPTBindRootMigrationCandidateAt(rootFD, bindGID, ops, func() error {
		// LoadCurrent only reads immutable files and the current pointer; it does
		// not stage, activate or execute commands. The migration candidate proof
		// surrounds the read instead of hardening its parent as a side effect.
		publisher, err := binddns.NewOSPublisher(layout.GenerationRoot)
		if err != nil {
			return err
		}
		tree, err := publisher.LoadCurrent()
		if err != nil {
			return err
		}
		return verifyExistingManagedBINDTreeForSignedUpdateOwnerAware(ctx, layout, state, tree)
	})
}

type bindUpdateRootCandidate struct {
	identities [4]bindDirectoryIdentity
	parentMode uint32
}

// This accepts precisely the existing root-hardening inputs, without creating
// the child, setting permissions, or adding a package statoverride. Both the
// directory identities and package proof are re-read around the current tree.
func verifyAPTBindRootMigrationCandidateAt(rootFD int, bindGID uint32, ops aptBINDStatOverrideOps, verifyTree func() error) error {
	if ops.owner == nil || ops.list == nil || verifyTree == nil {
		return errors.New("invalid read-only BIND root migration proof")
	}
	readProof := func() (bindUpdateRootCandidate, aptBINDStatOverrideListState, error) {
		candidate, err := inspectAPTBindRootMigrationCandidateAt(rootFD, bindGID)
		if err != nil {
			return candidate, 0, err
		}
		owner, err := ops.owner()
		if err != nil || string(owner) != aptBINDExactPackageOwnerLine {
			return candidate, 0, errors.New("BIND cache parent is not the exact bind9 package-owned directory")
		}
		output, commandErr := ops.list()
		state, err := classifyExactAPTBindStatOverride(output, commandErr)
		return candidate, state, err
	}
	before, overrideBefore, err := readProof()
	if err != nil {
		return err
	}
	if err := verifyTree(); err != nil {
		return err
	}
	after, overrideAfter, err := readProof()
	if err != nil {
		return err
	}
	if before != after || overrideBefore != overrideAfter {
		return errors.New("BIND root migration candidate changed during read-only verification")
	}
	return nil
}

func inspectAPTBindRootMigrationCandidateAt(rootFD int, bindGID uint32) (bindUpdateRootCandidate, error) {
	result := bindUpdateRootCandidate{}
	if bindGID == 0 {
		return result, errors.New("BIND service group must not be root")
	}
	if _, err := validateInheritedBINDAnchorFD(rootFD, "BIND filesystem root"); err != nil {
		return result, err
	}
	varFD, identity, err := openInheritedBINDAnchorAt(rootFD, "var", "/var")
	if err != nil {
		return result, err
	}
	defer unix.Close(varFD)
	result.identities[0] = identity
	cacheFD, identity, err := openInheritedBINDAnchorAt(varFD, "cache", "/var/cache")
	if err != nil {
		return result, err
	}
	defer unix.Close(cacheFD)
	result.identities[1] = identity
	bindFD, err := openBINDDirectoryAt(cacheFD, "bind", "/var/cache/bind")
	if err != nil {
		return result, err
	}
	defer unix.Close(bindFD)
	result.parentMode = aptBINDCacheParentMode
	identity, err = validateExactBINDDirectoryFD(bindFD, 0, bindGID, result.parentMode, "/var/cache/bind")
	if err != nil {
		result.parentMode = aptBINDStockCacheParentMode
		identity, err = validateExactBINDDirectoryFD(bindFD, 0, bindGID, result.parentMode, "/var/cache/bind")
	}
	if err != nil {
		return result, fmt.Errorf("BIND cache parent is not an exact supported migration candidate: %w", err)
	}
	result.identities[2] = identity
	childFD, identity, err := openExactBINDDirectoryAt(bindFD, "celikpanel", 0, 0, bindManagedRootMode, "/var/cache/bind/celikpanel")
	if err != nil {
		return result, err
	}
	defer unix.Close(childFD)
	result.identities[3] = identity
	return result, nil
}
