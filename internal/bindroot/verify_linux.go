//go:build linux

package bindroot

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// VerifyInstalled applies the read-only proof to the fixed host filesystem.
func VerifyInstalled(ctx context.Context, layout Layout, serviceGID uint32) error {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open BIND filesystem root: %w", err)
	}
	defer unix.Close(rootFD)
	return VerifyAt(rootFD, layout, serviceGID, func() error { return ProveInstalledPackage(ctx, layout) })
}

type Layout string

const (
	APT    Layout = "/var/cache/bind/celikpanel"
	Pacman Layout = "/var/named/celikpanel"
)

// VerifyAt proves the installed root chain twice around the caller's package
// ownership proof. It does not create or harden directories, or inspect a BIND
// generation. rootFD must refer to the trusted filesystem root; production
// callers open "/" with O_DIRECTORY|O_NOFOLLOW.
func VerifyAt(rootFD int, layout Layout, serviceGID uint32, provePackage func() error) error {
	if serviceGID == 0 || provePackage == nil {
		return errors.New("BIND service group and package ownership proof are required")
	}
	before, err := inspectAt(rootFD, layout, serviceGID)
	if err != nil {
		return err
	}
	if err := provePackage(); err != nil {
		return fmt.Errorf("verify BIND package ownership: %w", err)
	}
	after, err := inspectAt(rootFD, layout, serviceGID)
	if err != nil {
		return err
	}
	if err := provePackage(); err != nil {
		return fmt.Errorf("reverify BIND package ownership: %w", err)
	}
	if before != after {
		return errors.New("managed BIND directory chain changed during verification")
	}
	return nil
}

type chain struct {
	root   Identity
	varDir Identity
	parent Identity
	child  Identity
	cache  Identity
}

func inspectAt(rootFD int, layout Layout, serviceGID uint32) (chain, error) {
	var result chain
	var err error
	result.root, err = ValidateInheritedAnchor(rootFD, "BIND filesystem root")
	if err != nil {
		return result, err
	}
	varFD, identity, err := OpenInheritedAnchorAt(rootFD, "var", "/var")
	if err != nil {
		return result, err
	}
	defer unix.Close(varFD)
	result.varDir = identity
	parentFD := varFD
	parentName := "named"
	parentPath := "/var/named"
	parentMode := uint32(0o1770)
	switch layout {
	case APT:
		cacheFD, cacheIdentity, err := OpenInheritedAnchorAt(varFD, "cache", "/var/cache")
		if err != nil {
			return result, err
		}
		defer unix.Close(cacheFD)
		result.cache = cacheIdentity
		parentFD, parentName, parentPath, parentMode = cacheFD, "bind", "/var/cache/bind", 0o1775
	case Pacman:
	default:
		return result, errors.New("unsupported managed BIND generation root")
	}
	parent, identity, err := OpenExactDirectoryAt(parentFD, parentName, 0, serviceGID, parentMode, parentPath)
	if err != nil {
		return result, err
	}
	defer unix.Close(parent)
	result.parent = identity
	child, identity, err := OpenExactDirectoryAt(parent, "celikpanel", 0, 0, 0o755, string(layout))
	if err != nil {
		return result, err
	}
	defer unix.Close(child)
	result.child = identity
	return result, nil
}
