//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// The pacman BIND working directory is shipped by the bind package as
// /var/named, owned root:named, mode 0770: the daemon's group can write there
// because named keeps its journals and managed keys in its working directory.
// The managed generation root lives beneath it as /var/named/celikpanel, root
// owned and 0755 like its APT counterpart.
//
// A group-writable parent without the sticky bit would let the named group
// rename or unlink the root-owned managed directory itself, which is exactly
// what APT's dpkg-statoverride to 1775 on /var/cache/bind prevents. pacman has
// no statoverride, so the parent is hardened in place to 1770 when a switch
// prepares the root, and every later access requires that exact mode. pacman
// preserves the permissions of existing directories on upgrade, so the
// hardening survives the package's own updates.
//
// pacman BIND çalışma dizini bind paketiyle /var/named olarak, root:named
// sahipli ve 0770 kipinde gelir: named günlüklerini ve yönetilen anahtarlarını
// çalışma dizininde tuttuğu için daemon'un grubu oraya yazabilir. Yönetilen
// nesil kökü onun altında /var/named/celikpanel olarak, APT karşılığı gibi
// root sahipli ve 0755 yaşar.
//
// Sticky biti olmayan grup-yazılabilir bir üst dizin, named grubunun root
// sahipli yönetilen dizinin kendisini yeniden adlandırmasına ya da silmesine
// izin verirdi; APT'nin /var/cache/bind üzerindeki 1775 dpkg-statoverride'ı tam
// bunu engeller. pacman'da statoverride yoktur; bu yüzden bir geçiş kökü
// hazırlarken üst dizin yerinde 1770'e sertleştirilir ve sonraki her erişim o
// tam kipi ister. pacman yükseltmede var olan dizinlerin izinlerini korur;
// sertleştirme paketin kendi güncellemelerinden sağ çıkar.
const (
	pacmanBINDVendorParentPath      = "/var/named"
	pacmanBINDVendorParentMode      = uint32(0o1770)
	pacmanBINDStockVendorParentMode = uint32(0o0770)
	pacmanBINDServiceGroup          = "named"
	pacmanBINDExactPackageOwnerHead = "/var/named/ is owned by bind "
	pacmanBINDOwnerQueryTimeout     = 15 * time.Second
)

type pacmanBINDOwnerRunner func(context.Context, string, ...string) ([]byte, error)

func accessPacmanBindGenerationRootWithMode(
	ctx context.Context,
	allowParentHardening bool,
	createChild bool,
) error {
	if ctx == nil {
		return errors.New("pacman BIND generation root access requires a context")
	}
	namedGID, err := resolvePacmanBINDGroupGID(ctx)
	if err != nil {
		return err
	}
	rootFD, err := unix.Open(
		"/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return fmt.Errorf("open BIND filesystem root: %w", err)
	}
	defer unix.Close(rootFD)
	proveOwnership, cancel, err := hostPacmanBINDOwnershipProof(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if err := ensurePacmanBindGenerationRootAtWithMode(
		rootFD, namedGID, allowParentHardening, createChild, proveOwnership,
	); err != nil {
		return fmt.Errorf("prepare managed BIND generation root: %w", err)
	}
	return nil
}

func resolvePacmanBINDGroupGID(ctx context.Context) (uint32, error) {
	if ctx == nil {
		return 0, errors.New("BIND group proof requires a context")
	}
	getent, err := firstTrustedExecutable(
		[]string{"/usr/bin/getent", "/bin/getent"}, "getent",
	)
	if err != nil {
		return 0, err
	}
	return resolveServiceGroupGIDWithRunner(
		ctx, getent, pacmanBINDServiceGroup,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = bindSafeAPTCommandEnvironment()
			return command.CombinedOutputLimited(4 << 10)
		},
	)
}

// resolveServiceGroupGIDWithRunner is resolveBINDGroupGIDWithRunner for a
// named group: exactly one canonical getent record, no members, non-zero id.
// resolveServiceGroupGIDWithRunner, adı verilen bir grup için
// resolveBINDGroupGIDWithRunner'dır: tam bir kanonik getent kaydı, üye yok,
// sıfır olmayan kimlik.
func resolveServiceGroupGIDWithRunner(
	ctx context.Context,
	getent, group string,
	runner bindGroupLookupRunner,
) (uint32, error) {
	if ctx == nil || getent == "" || runner == nil || group == "" ||
		strings.ContainsAny(group, ":\n\x00 ") {
		return 0, errors.New("invalid service group proof")
	}
	output, err := runner(ctx, getent, "group", group)
	if err != nil {
		return 0, fmt.Errorf("resolve %s service group: %w", group, err)
	}
	line := string(output)
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		return 0, fmt.Errorf("getent returned a non-canonical %s group record", group)
	}
	fields := strings.Split(strings.TrimSuffix(line, "\n"), ":")
	if len(fields) != 4 || fields[0] != group || fields[1] != "x" ||
		fields[2] == "" || fields[3] != "" {
		return 0, fmt.Errorf("getent returned an unsafe %s group record", group)
	}
	value, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil || value == 0 || value > uint64(1<<31-1) ||
		strconv.FormatUint(value, 10) != fields[2] {
		return 0, fmt.Errorf("%s service group has an invalid numeric identity", group)
	}
	return uint32(value), nil
}

func hostPacmanBINDOwnershipProof(
	ctx context.Context,
) (func() error, context.CancelFunc, error) {
	pacman, err := firstTrustedExecutable(
		[]string{"/usr/bin/pacman", "/usr/sbin/pacman"}, "pacman",
	)
	if err != nil {
		return nil, nil, err
	}
	proofCtx, cancel := context.WithTimeout(ctx, pacmanBINDOwnerQueryTimeout)
	runner := func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
		command := serviceMutationCommand(commandCtx, name, args...)
		command.Env = bindSafeAPTCommandEnvironment()
		return command.CombinedOutputLimited(4 << 10)
	}
	return func() error {
		output, err := runner(proofCtx, pacman, "-Qo", "--", pacmanBINDVendorParentPath)
		return classifyExactPacmanBINDOwner(output, err)
	}, cancel, nil
}

// classifyExactPacmanBINDOwner accepts exactly one line of the form
// "/var/named/ is owned by bind <version>\n" and nothing else.
// classifyExactPacmanBINDOwner yalnız "/var/named/ is owned by bind
// <sürüm>\n" biçiminde tam bir satırı kabul eder, başkasını değil.
func classifyExactPacmanBINDOwner(output []byte, commandErr error) error {
	if commandErr != nil {
		return fmt.Errorf("verify /var/named package ownership: %w", commandErr)
	}
	line := string(output)
	if !strings.HasPrefix(line, pacmanBINDExactPackageOwnerHead) ||
		!strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		return errors.New("/var/named is not the exact bind package-owned directory")
	}
	version := strings.TrimSuffix(strings.TrimPrefix(line, pacmanBINDExactPackageOwnerHead), "\n")
	if version == "" || strings.ContainsAny(version, " \t") ||
		strings.Trim(version, "0123456789.:-+abcdefghijklmnopqrstuvwxyz_") != "" {
		return errors.New("/var/named package ownership version is not canonical")
	}
	return nil
}

func ensurePacmanBindGenerationRootAtWithMode(
	rootFD int,
	namedGID uint32,
	allowParentHardening bool,
	createChild bool,
	proveOwnership func() error,
) error {
	if namedGID == 0 || proveOwnership == nil {
		return errors.New("BIND service group must not be root")
	}
	if _, err := validateInheritedBINDAnchorFD(
		rootFD, "BIND filesystem root",
	); err != nil {
		return err
	}
	varFD, varIdentity, err := openInheritedBINDAnchorAt(
		rootFD, "var", "/var",
	)
	if err != nil {
		return err
	}
	defer unix.Close(varFD)
	namedFD, namedIdentity, err := securePacmanBINDVendorParentAt(
		varFD, namedGID, allowParentHardening, proveOwnership,
	)
	if err != nil {
		return err
	}
	defer unix.Close(namedFD)

	childFD, openErr := openBINDDirectoryAt(
		namedFD, "celikpanel", pacmanBINDGenerationRoot,
	)
	created := false
	if errors.Is(openErr, unix.ENOENT) && createChild {
		mkdirErr := unix.Mkdirat(namedFD, "celikpanel", bindManagedRootMode)
		if mkdirErr == nil {
			created = true
		} else if !errors.Is(mkdirErr, unix.EEXIST) {
			return fmt.Errorf("create %s: %w", pacmanBINDGenerationRoot, mkdirErr)
		}
		childFD, openErr = openBINDDirectoryAt(
			namedFD, "celikpanel", pacmanBINDGenerationRoot,
		)
	}
	if openErr != nil {
		return openErr
	}
	defer unix.Close(childFD)
	keepCreated := false
	defer func() {
		if created && !keepCreated {
			removeCreatedBINDDirectoryAt(namedFD, childFD)
		}
	}()
	if created {
		if err := unix.Fchown(childFD, 0, 0); err != nil {
			return fmt.Errorf("set managed BIND root ownership: %w", err)
		}
		if err := unix.Fchmod(childFD, bindManagedRootMode); err != nil {
			return fmt.Errorf("set managed BIND root permissions: %w", err)
		}
		if err := unix.Fsync(childFD); err != nil {
			return fmt.Errorf("sync managed BIND root: %w", err)
		}
		if err := unix.Fsync(namedFD); err != nil {
			return fmt.Errorf("sync /var/named after managed root creation: %w", err)
		}
	}
	childIdentity, err := validateExactBINDDirectoryFD(
		childFD, 0, 0, bindManagedRootMode, pacmanBINDGenerationRoot,
	)
	if err != nil {
		return err
	}
	if err := reverifyPacmanBindGenerationRootAt(
		rootFD, namedGID, varIdentity, namedIdentity, childIdentity, proveOwnership,
	); err != nil {
		return err
	}
	keepCreated = true
	return nil
}

func securePacmanBINDVendorParentAt(
	varFD int,
	namedGID uint32,
	allowStickyUpgrade bool,
	proveOwnership func() error,
) (int, bindDirectoryIdentity, error) {
	if proveOwnership == nil {
		return -1, bindDirectoryIdentity{},
			errors.New("pacman BIND parent ownership proof is required")
	}
	namedFD, err := openBINDDirectoryAt(varFD, "named", pacmanBINDVendorParentPath)
	if err != nil {
		return -1, bindDirectoryIdentity{}, err
	}
	identity, exactErr := validateExactBINDDirectoryFD(
		namedFD, 0, namedGID, pacmanBINDVendorParentMode, pacmanBINDVendorParentPath,
	)
	if exactErr == nil {
		if err := proveOwnership(); err != nil {
			unix.Close(namedFD)
			return -1, bindDirectoryIdentity{}, err
		}
		return namedFD, identity, nil
	}
	if !allowStickyUpgrade {
		unix.Close(namedFD)
		return -1, bindDirectoryIdentity{}, exactErr
	}
	identity, stockErr := validateExactBINDDirectoryFD(
		namedFD, 0, namedGID, pacmanBINDStockVendorParentMode, pacmanBINDVendorParentPath,
	)
	if stockErr != nil {
		unix.Close(namedFD)
		return -1, bindDirectoryIdentity{}, exactErr
	}
	if err := proveOwnership(); err != nil {
		unix.Close(namedFD)
		return -1, bindDirectoryIdentity{}, err
	}
	if err := unix.Fchmod(namedFD, pacmanBINDVendorParentMode); err != nil {
		unix.Close(namedFD)
		return -1, bindDirectoryIdentity{}, fmt.Errorf(
			"add sticky protection to /var/named: %w", err,
		)
	}
	if err := unix.Fsync(namedFD); err != nil {
		unix.Close(namedFD)
		return -1, bindDirectoryIdentity{}, fmt.Errorf("sync sticky /var/named: %w", err)
	}
	if err := unix.Fsync(varFD); err != nil {
		unix.Close(namedFD)
		return -1, bindDirectoryIdentity{}, fmt.Errorf(
			"sync /var after sticky BIND upgrade: %w", err,
		)
	}
	upgraded, err := validateExactBINDDirectoryFD(
		namedFD, 0, namedGID, pacmanBINDVendorParentMode, pacmanBINDVendorParentPath,
	)
	if err != nil || upgraded != identity {
		unix.Close(namedFD)
		if err == nil {
			err = errors.New("/var/named identity changed during sticky upgrade")
		}
		return -1, bindDirectoryIdentity{}, err
	}
	return namedFD, upgraded, nil
}

func reverifyPacmanBindGenerationRootAt(
	rootFD int,
	namedGID uint32,
	wantVar, wantNamed, wantChild bindDirectoryIdentity,
	proveOwnership func() error,
) error {
	if proveOwnership == nil {
		return errors.New("pacman BIND parent ownership proof is required")
	}
	if _, err := validateInheritedBINDAnchorFD(
		rootFD, "BIND filesystem root",
	); err != nil {
		return err
	}
	varFD, varIdentity, err := openInheritedBINDAnchorAt(rootFD, "var", "/var")
	if err != nil {
		return err
	}
	defer unix.Close(varFD)
	namedFD, namedIdentity, err := openExactBINDDirectoryAt(
		varFD, "named", 0, namedGID, pacmanBINDVendorParentMode, pacmanBINDVendorParentPath,
	)
	if err != nil {
		return err
	}
	defer unix.Close(namedFD)
	if err := proveOwnership(); err != nil {
		return err
	}
	childFD, childIdentity, err := openExactBINDDirectoryAt(
		namedFD, "celikpanel", 0, 0, bindManagedRootMode, pacmanBINDGenerationRoot,
	)
	if err != nil {
		return err
	}
	defer unix.Close(childFD)
	if varIdentity != wantVar || namedIdentity != wantNamed || childIdentity != wantChild {
		return errors.New("managed BIND directory chain changed during verification")
	}
	return nil
}
