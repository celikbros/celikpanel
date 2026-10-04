//go:build linux

package dnsenginerecovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// A configuration observation is evidence of current bytes, not proof of what
// pdns_server has loaded. The installed policy fixes every path before a journal
// can influence an open. This observer never writes a file or authorizes inverse.
func ProbeInstalledPDNSAdoptionConfigs(ctx context.Context, policy dnsengineartifact.JournalPolicy, snapshots []dnsengineartifact.FileSnapshot, pdnsGID uint32) error {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open PowerDNS config proof root: %w", err)
	}
	defer unix.Close(rootFD)
	return ProbePDNSAdoptionConfigsAt(ctx, rootFD, policy, snapshots, pdnsGID)
}

// ProbePDNSAdoptionConfigsAt accepts an already opened trusted filesystem root
// so the same installed-path contract can be exercised in an isolated fixture.
func ProbePDNSAdoptionConfigsAt(ctx context.Context, rootFD int, policy dnsengineartifact.JournalPolicy, snapshots []dnsengineartifact.FileSnapshot, pdnsGID uint32) error {
	if ctx == nil || rootFD < 0 || pdnsGID == 0 || pdnsGID > uint32(1<<31-1) {
		return errors.New("PowerDNS adoption config observation requires a trusted root, context and service group")
	}
	if policy.PDNSMainPath != "/etc/powerdns/pdns.conf" || policy.PDNSManagedPath != "/etc/powerdns/pdns.d/celikpanel.conf" || policy.PDNSClusterPath != "/etc/powerdns/pdns.d/celikpanel-cluster.conf" {
		return errors.New("PowerDNS adoption config paths differ from the installed contract")
	}
	if err := policy.ValidatePDNSConfigSnapshotSet(snapshots); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if snapshot.Path == policy.PDNSMainPath && snapshot.GID != 0 && snapshot.GID != pdnsGID {
			return errors.New("frozen PowerDNS main config group differs from the local service group")
		}
	}
	if _, err := bindroot.ValidateInheritedAnchor(rootFD, "PowerDNS config proof root"); err != nil {
		return err
	}
	first, err := probePDNSAdoptionConfigPass(ctx, rootFD, snapshots)
	if err != nil {
		return err
	}
	second, err := probePDNSAdoptionConfigPass(ctx, rootFD, snapshots)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(first, second) {
		return errors.New("PowerDNS config file identity changed between secure observations")
	}
	return ctx.Err()
}

type pdnsConfigReadIdentity struct {
	Exists bool
	Device uint64
	Inode  uint64
	Mode   uint32
	UID    uint32
	GID    uint32
	Links  uint64
	Size   int64
	MTime  unix.Timespec
	CTime  unix.Timespec
}

func configIdentity(stat unix.Stat_t) pdnsConfigReadIdentity {
	return pdnsConfigReadIdentity{true, uint64(stat.Dev), stat.Ino, stat.Mode, stat.Uid, stat.Gid, uint64(stat.Nlink), stat.Size, stat.Mtim, stat.Ctim}
}

func probePDNSAdoptionConfigPass(ctx context.Context, rootFD int, snapshots []dnsengineartifact.FileSnapshot) ([]pdnsConfigReadIdentity, error) {
	identities := make([]pdnsConfigReadIdentity, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identity, err := probePDNSAdoptionConfigFile(ctx, rootFD, snapshot)
		if err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, nil
}

func pdnsConfigParent(rootFD int, path string) ([]int, []string, error) {
	components := []string{"etc", "powerdns"}
	if filepath.Dir(path) == "/etc/powerdns/pdns.d" {
		components = append(components, "pdns.d")
	}
	parent := rootFD
	opened := make([]int, 0, len(components))
	for _, component := range components {
		next, _, err := bindroot.OpenExactDirectoryAt(parent, component, 0, 0, 0o755, "PowerDNS config parent")
		if err != nil {
			for _, fd := range opened {
				unix.Close(fd)
			}
			return nil, nil, err
		}
		opened = append(opened, next)
		parent = next
	}
	return opened, components, nil
}

// pdnsConfigDifferenceError marks a completed observation that found a file
// other than the frozen image: missing, present where none was recorded, a
// symbolic link, or different owner, mode, size, type or bytes. Its text is
// the underlying message. Every other probe error means the file could not be
// read or stayed unstable, which is unknown, not a difference.
type pdnsConfigDifferenceError struct{ err error }

func (e *pdnsConfigDifferenceError) Error() string { return e.err.Error() }
func (e *pdnsConfigDifferenceError) Unwrap() error { return e.err }

func pdnsConfigDiffers(err error) bool {
	var difference *pdnsConfigDifferenceError
	return errors.As(err, &difference)
}

func probePDNSAdoptionConfigFile(ctx context.Context, rootFD int, snapshot dnsengineartifact.FileSnapshot) (pdnsConfigReadIdentity, error) {
	// The caller has already checked the fixed path set and journal integrity.
	opened, components, err := pdnsConfigParent(rootFD, snapshot.Path)
	if err != nil {
		return pdnsConfigReadIdentity{}, err
	}
	defer func() {
		for index := len(opened) - 1; index >= 0; index-- {
			unix.Close(opened[index])
		}
	}()
	parentFD := opened[len(opened)-1]
	leaf := filepath.Base(snapshot.Path)
	open := func() (int, error) {
		return unix.Openat2(parentFD, leaf, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	}
	fd, err := open()
	if errors.Is(err, unix.ENOENT) && !snapshot.Exists {
		if err := reprovePDNSConfigParent(rootFD, parentFD, components); err != nil {
			return pdnsConfigReadIdentity{}, err
		}
		var stat unix.Stat_t
		if err := unix.Fstatat(parentFD, leaf, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return pdnsConfigReadIdentity{}, errors.New("absent PowerDNS config changed during observation")
		}
		return pdnsConfigReadIdentity{}, nil
	}
	if err != nil {
		opened := fmt.Errorf("open PowerDNS config %s: %w", snapshot.Path, err)
		// A missing file the image records, or a symbolic link at the
		// path, is a different file, not an unreadable one.
		if (errors.Is(err, unix.ENOENT) && snapshot.Exists) || errors.Is(err, unix.ELOOP) {
			return pdnsConfigReadIdentity{}, &pdnsConfigDifferenceError{err: opened}
		}
		return pdnsConfigReadIdentity{}, opened
	}
	file := os.NewFile(uintptr(fd), snapshot.Path)
	if file == nil {
		unix.Close(fd)
		return pdnsConfigReadIdentity{}, errors.New("PowerDNS config descriptor is invalid")
	}
	defer file.Close()
	var before, after unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return pdnsConfigReadIdentity{}, err
	}
	if !snapshot.Exists || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Mode&0o7777 != snapshot.Mode || before.Uid != snapshot.UID || before.Gid != snapshot.GID || before.Size != int64(len(snapshot.Data)) {
		return pdnsConfigReadIdentity{}, &pdnsConfigDifferenceError{err: fmt.Errorf("PowerDNS config %s differs from frozen owner, mode, size or file type", snapshot.Path)}
	}
	if err := bindroot.RejectACL(fd, "PowerDNS config file"); err != nil {
		return pdnsConfigReadIdentity{}, err
	}
	data := make([]byte, len(snapshot.Data))
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return pdnsConfigReadIdentity{}, err
		}
		end := offset + 64<<10
		if end > len(data) {
			end = len(data)
		}
		if _, err := io.ReadFull(file, data[offset:end]); err != nil {
			return pdnsConfigReadIdentity{}, err
		}
		offset = end
	}
	if err := ctx.Err(); err != nil {
		return pdnsConfigReadIdentity{}, err
	}
	if !bytes.Equal(data, snapshot.Data) {
		return pdnsConfigReadIdentity{}, &pdnsConfigDifferenceError{err: fmt.Errorf("PowerDNS config %s bytes differ from frozen evidence", snapshot.Path)}
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return pdnsConfigReadIdentity{}, err
	}
	if configIdentity(before) != configIdentity(after) {
		return pdnsConfigReadIdentity{}, errors.New("PowerDNS config changed while read")
	}
	currentFD, err := open()
	if err != nil {
		return pdnsConfigReadIdentity{}, fmt.Errorf("reopen PowerDNS config %s: %w", snapshot.Path, err)
	}
	var current unix.Stat_t
	statErr := unix.Fstat(currentFD, &current)
	closeErr := unix.Close(currentFD)
	if statErr != nil || closeErr != nil {
		return pdnsConfigReadIdentity{}, errors.Join(statErr, closeErr)
	}
	if configIdentity(after) != configIdentity(current) {
		return pdnsConfigReadIdentity{}, errors.New("PowerDNS config path changed while read")
	}
	if err := reprovePDNSConfigParent(rootFD, parentFD, components); err != nil {
		return pdnsConfigReadIdentity{}, err
	}
	return configIdentity(after), nil
}

func reprovePDNSConfigParent(rootFD, heldFD int, components []string) error {
	current := rootFD
	opened := make([]int, 0, len(components))
	defer func() {
		for _, fd := range opened {
			unix.Close(fd)
		}
	}()
	for _, component := range components {
		next, _, err := bindroot.OpenExactDirectoryAt(current, component, 0, 0, 0o755, "PowerDNS config parent recheck")
		if err != nil {
			return err
		}
		opened = append(opened, next)
		current = next
	}
	var held, named unix.Stat_t
	if err := unix.Fstat(heldFD, &held); err != nil {
		return err
	}
	if err := unix.Fstat(current, &named); err != nil {
		return err
	}
	if configIdentity(held) != configIdentity(named) {
		return errors.New("PowerDNS config parent path changed during observation")
	}
	return nil
}
