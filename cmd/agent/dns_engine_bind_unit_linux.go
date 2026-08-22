//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

const bindVendorFileMaxSize = 64 << 10

type bindVendorFileContract struct {
	unitPath         string
	environmentPath  string
	unitBytes        []byte
	environmentBytes []byte
}

const certifiedAPTBINDVendorUnit = "[Unit]\n" +
	"Description=BIND Domain Name Server\n" +
	"Documentation=man:named(8)\n" +
	"After=network.target\nWants=nss-lookup.target\nBefore=nss-lookup.target\n\n" +
	"[Service]\nType=notify\nEnvironmentFile=-/etc/default/named\n" +
	"ExecStart=/usr/sbin/named -f $OPTIONS\n" +
	"ExecReload=/usr/sbin/rndc reload\nExecStop=/usr/sbin/rndc stop\n" +
	"Restart=on-failure\n\n[Install]\nWantedBy=multi-user.target\nAlias=bind9.service\n"

const certifiedAPTBINDVendorEnvironment = "#\n# run resolvconf?\n" +
	"RESOLVCONF=no\n\n# startup options for the server\nOPTIONS=\"-u bind\"\n"

const certifiedPacmanBINDVendorUnit = "[Unit]\n" +
	"Description=Internet domain name server\nAfter=network.target\n\n" +
	"[Service]\nExecStart=/usr/bin/named -f -u named\n" +
	"ExecReload=/usr/bin/kill -HUP $MAINPID\n\n" +
	"[Install]\nWantedBy=multi-user.target\n"

func bindVendorContract(profile hostplatform.Profile) (bindVendorFileContract, error) {
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		if err := certifyAPTBINDProfile(profile); err != nil {
			return bindVendorFileContract{}, err
		}
		return bindVendorFileContract{
			unitPath:         "/usr/lib/systemd/system/named.service",
			environmentPath:  "/etc/default/named",
			unitBytes:        []byte(certifiedAPTBINDVendorUnit),
			environmentBytes: []byte(certifiedAPTBINDVendorEnvironment),
		}, nil
	case hostplatform.PackageManagerPacman:
		return bindVendorFileContract{
			unitPath:  "/usr/lib/systemd/system/named.service",
			unitBytes: []byte(certifiedPacmanBINDVendorUnit),
		}, nil
	default:
		return bindVendorFileContract{},
			errors.New("BIND vendor unit proof is unsupported on this package manager")
	}
}

func inspectHostBINDVendorFiles(
	ctx context.Context,
	profile hostplatform.Profile,
) (bindVendorFilesIdentity, error) {
	if ctx == nil {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor proof requires a context")
	}
	rootFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return bindVendorFilesIdentity{}, fmt.Errorf("open BIND vendor proof root: %w", err)
	}
	defer unix.Close(rootFD)
	if profile.PackageManager != hostplatform.PackageManagerAPT {
		return inspectBINDVendorFilesAt(rootFD, profile, nil)
	}
	dpkgQuery, err := firstTrustedExecutable(
		[]string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"}, "dpkg-query",
	)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	lookup := func(lookupCtx context.Context, filename string) ([]byte, error) {
		command := serviceMutationCommand(
			lookupCtx, dpkgQuery, "-S", "--", filename,
		)
		command.Env = aptBINDStatOverrideCommandEnvironment()
		return command.CombinedOutputLimited(4 << 10)
	}
	if err := verifyExactAPTBINDVendorPackageOwnership(ctx, lookup); err != nil {
		return bindVendorFilesIdentity{}, err
	}
	identity, err := inspectBINDVendorFilesAt(rootFD, profile, nil)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if err := verifyExactAPTBINDVendorPackageOwnership(ctx, lookup); err != nil {
		return bindVendorFilesIdentity{}, err
	}
	return identity, nil
}

type aptBINDVendorOwnerLookup func(context.Context, string) ([]byte, error)

func verifyExactAPTBINDVendorPackageOwnership(
	ctx context.Context,
	lookup aptBINDVendorOwnerLookup,
) error {
	if ctx == nil || lookup == nil {
		return errors.New("invalid APT BIND vendor package ownership proof")
	}
	for _, file := range []struct {
		path string
		want string
	}{
		{path: "/usr/lib/systemd/system/named.service", want: "bind9: /usr/lib/systemd/system/named.service\n"},
		{path: "/etc/default/named", want: "bind9: /etc/default/named\n"},
	} {
		output, err := lookup(ctx, file.path)
		if err != nil {
			return fmt.Errorf("verify BIND vendor package ownership for %s: %w", file.path, err)
		}
		if string(output) != file.want {
			return fmt.Errorf("%s is not owned by the exact bind9 package", file.path)
		}
	}
	return nil
}

// inspectBINDVendorFilesAt reads the package unit and its effective APT
// environment through no-follow descriptors, then repeats the complete read.
// The callback is a test-only TOCTOU seam executed between the two snapshots.
func inspectBINDVendorFilesAt(
	rootFD int,
	profile hostplatform.Profile,
	afterFirstSnapshot func(),
) (bindVendorFilesIdentity, error) {
	contract, err := bindVendorContract(profile)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	first, err := readBINDVendorFilesSnapshotAt(rootFD, contract)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if afterFirstSnapshot != nil {
		afterFirstSnapshot()
	}
	second, err := readBINDVendorFilesSnapshotAt(rootFD, contract)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if first != second {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor unit files changed during exact verification")
	}
	return second, nil
}

func readBINDVendorFilesSnapshotAt(
	rootFD int,
	contract bindVendorFileContract,
) (bindVendorFilesIdentity, error) {
	unit, unitIdentity, err := readExactRootOwnedBINDFileAt(
		rootFD, contract.unitPath, "BIND vendor unit",
	)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if !bytes.Equal(unit, contract.unitBytes) {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor unit bytes differ from the certified package unit")
	}
	identity := bindVendorFilesIdentity{Unit: unitIdentity}
	if contract.environmentPath == "" {
		return identity, nil
	}
	environment, environmentIdentity, err := readExactRootOwnedBINDFileAt(
		rootFD, contract.environmentPath, "BIND vendor environment",
	)
	if err != nil {
		return bindVendorFilesIdentity{}, err
	}
	if !bytes.Equal(environment, contract.environmentBytes) {
		return bindVendorFilesIdentity{},
			errors.New("BIND vendor environment bytes differ from the certified safe options")
	}
	identity.Environment = environmentIdentity
	return identity, nil
}

func readExactRootOwnedBINDFileAt(
	rootFD int,
	absolutePath string,
	label string,
) ([]byte, bindSecureFileIdentity, error) {
	if !path.IsAbs(absolutePath) || path.Clean(absolutePath) != absolutePath ||
		absolutePath == "/" {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("%s path is not canonical", label)
	}
	if _, err := validateExactBINDDirectoryFD(
		rootFD, 0, 0, bindManagedRootMode, "BIND vendor filesystem root",
	); err != nil {
		return nil, bindSecureFileIdentity{}, err
	}
	components := strings.Split(strings.TrimPrefix(absolutePath, "/"), "/")
	if len(components) < 2 {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("%s path is incomplete", label)
	}
	currentFD, err := unix.FcntlInt(uintptr(rootFD), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("duplicate BIND vendor root: %w", err)
	}
	defer unix.Close(currentFD)
	for _, component := range components[:len(components)-1] {
		nextFD, _, openErr := openExactBINDDirectoryAt(
			currentFD, component, 0, 0, bindManagedRootMode,
			path.Join("/", strings.Join(components[:len(components)-1], "/")),
		)
		if openErr != nil {
			return nil, bindSecureFileIdentity{}, openErr
		}
		unix.Close(currentFD)
		currentFD = nextFD
	}
	leaf := components[len(components)-1]
	fd, err := unix.Openat2(currentFD, leaf, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOSYS) {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("%s requires Linux openat2: %w", label, err)
	}
	if err != nil {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("open %s: %w", label, err)
	}
	file := os.NewFile(uintptr(fd), absolutePath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, bindSecureFileIdentity{}, fmt.Errorf("open %s handle", label)
	}
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("stat %s: %w", label, err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 ||
		before.Mode&bindDirectoryModeMask != 0o0644 || before.Nlink != 1 ||
		before.Size < 1 || before.Size > bindVendorFileMaxSize {
		return nil, bindSecureFileIdentity{},
			fmt.Errorf("%s is not an exact root:root 0644 single-link regular file", label)
	}
	if err := rejectBINDDirectoryACL(fd, label); err != nil {
		return nil, bindSecureFileIdentity{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, bindVendorFileMaxSize+1))
	if err != nil {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("read %s: %w", label, err)
	}
	if len(data) == 0 || len(data) > bindVendorFileMaxSize || int64(len(data)) != before.Size {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("%s size changed while reading", label)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("restat %s: %w", label, err)
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size ||
		before.Uid != after.Uid || before.Gid != after.Gid || before.Mode != after.Mode ||
		before.Nlink != after.Nlink || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, bindSecureFileIdentity{}, fmt.Errorf("%s changed while reading", label)
	}
	return data, bindSecureFileIdentity{
		Device: uint64(after.Dev), Inode: after.Ino, Size: after.Size,
		Digest: sha256.Sum256(data),
	}, nil
}
