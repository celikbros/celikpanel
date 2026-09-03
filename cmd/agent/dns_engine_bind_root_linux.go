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

const (
	aptBINDCacheParentMode         = uint32(0o1775)
	aptBINDStockCacheParentMode    = uint32(0o0775)
	bindManagedRootMode            = uint32(0o0755)
	bindDirectoryModeMask          = uint32(0o7777)
	aptBINDStatOverrideTimeout     = 15 * time.Second
	aptBINDExactStatOverrideLine   = "root bind 1775 /var/cache/bind\n"
	aptBINDExactPackageOwnerLine   = "bind9: /var/cache/bind\n"
	aptBINDStatOverrideOutputLimit = 4 << 10
)

var errBINDAbandonedGenerationRoot = errors.New(
	"the unreleased APT BIND generation root is unsupported",
)

type bindDirectoryIdentity struct {
	Device uint64
	Inode  uint64
}

type aptBINDStatOverrideOps struct {
	owner func() ([]byte, error)
	list  func() ([]byte, error)
	add   func() ([]byte, error)
}

type aptBINDStatOverrideRunner func(
	context.Context,
	string,
	...string,
) ([]byte, error)

func prepareHostBINDGenerationRoot(
	ctx context.Context,
	layout bindHostLayout,
) error {
	return accessHostBINDGenerationRootWithMode(ctx, layout, true, true)
}

func hardenExistingHostBINDGenerationRoot(
	ctx context.Context,
	layout bindHostLayout,
) error {
	return accessHostBINDGenerationRootWithMode(ctx, layout, true, false)
}

func verifyHostBINDGenerationRoot(
	ctx context.Context,
	layout bindHostLayout,
) error {
	return accessHostBINDGenerationRootWithMode(ctx, layout, false, false)
}

func accessHostBINDGenerationRoot(
	ctx context.Context,
	layout bindHostLayout,
	create bool,
) error {
	return accessHostBINDGenerationRootWithMode(ctx, layout, create, create)
}

func accessHostBINDGenerationRootWithMode(
	ctx context.Context,
	layout bindHostLayout,
	allowParentHardening bool,
	createChild bool,
) error {
	switch layout.GenerationRoot {
	case aptBINDGenerationRoot:
	case pacmanBINDGenerationRoot:
		return accessPacmanBindGenerationRootWithMode(
			ctx, allowParentHardening, createChild,
		)
	case abandonedAPTBindGenerationRoot:
		return errBINDAbandonedGenerationRoot
	default:
		return nil
	}
	if ctx == nil {
		return errors.New("APT BIND generation root access requires a context")
	}
	bindGID, err := resolveBINDGroupGID(ctx)
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
	durability, cancelDurability, err := hostAPTBindStatOverrideProof(
		ctx, allowParentHardening,
	)
	if err != nil {
		return err
	}
	defer cancelDurability()
	if err := ensureAPTBindGenerationRootAtWithMode(
		rootFD, bindGID, allowParentHardening, createChild, durability, nil,
	); err != nil {
		return fmt.Errorf("prepare managed BIND generation root: %w", err)
	}
	return nil
}

func hostAPTBindStatOverrideProof(
	ctx context.Context,
	create bool,
) (func(uint32) error, context.CancelFunc, error) {
	executable, err := firstTrustedExecutable(
		[]string{"/usr/sbin/dpkg-statoverride", "/usr/bin/dpkg-statoverride"},
		"dpkg-statoverride",
	)
	if err != nil {
		return nil, nil, err
	}
	dpkgQuery, err := firstTrustedExecutable(
		[]string{"/usr/bin/dpkg-query", "/usr/sbin/dpkg-query"},
		"dpkg-query",
	)
	if err != nil {
		return nil, nil, err
	}
	proofCtx, cancel := context.WithTimeout(ctx, aptBINDStatOverrideTimeout)
	ops, err := aptBINDStatOverrideOperations(
		proofCtx, executable, dpkgQuery,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = aptBINDStatOverrideCommandEnvironment()
			return command.CombinedOutputLimited(aptBINDStatOverrideOutputLimit)
		},
	)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return func(mode uint32) error {
		if err := verifyOrCreateExactAPTBindStatOverride(
			create, mode, ops,
		); err != nil {
			return err
		}
		return nil
	}, cancel, nil
}

func aptBINDStatOverrideCommandEnvironment() []string {
	return bindSafeAPTCommandEnvironment()
}

func aptBINDStatOverrideOperations(
	ctx context.Context,
	executable, dpkgQuery string,
	runner aptBINDStatOverrideRunner,
) (aptBINDStatOverrideOps, error) {
	if ctx == nil || executable == "" || dpkgQuery == "" || runner == nil {
		return aptBINDStatOverrideOps{},
			errors.New("invalid APT BIND statoverride command operations")
	}
	return aptBINDStatOverrideOps{
		owner: func() ([]byte, error) {
			return runner(
				ctx, dpkgQuery, "-S", "--", aptBINDCacheParentPath,
			)
		},
		list: func() ([]byte, error) {
			return runner(
				ctx, executable, "--list", aptBINDCacheParentPath,
			)
		},
		add: func() ([]byte, error) {
			return runner(
				ctx, executable,
				"--no-force-statoverride-add",
				"--add", "root", "bind", "1775", aptBINDCacheParentPath,
			)
		},
	}, nil
}

type aptBINDStatOverrideListState uint8

const (
	aptBINDStatOverrideAbsent aptBINDStatOverrideListState = iota
	aptBINDStatOverrideExact
)

type commandExitCoder interface {
	ExitCode() int
}

func classifyExactAPTBindStatOverride(
	output []byte,
	commandErr error,
) (aptBINDStatOverrideListState, error) {
	if commandErr == nil && string(output) == aptBINDExactStatOverrideLine {
		return aptBINDStatOverrideExact, nil
	}
	var exitCoder commandExitCoder
	if len(output) == 0 && errors.As(commandErr, &exitCoder) &&
		exitCoder.ExitCode() == 1 {
		return aptBINDStatOverrideAbsent, nil
	}
	return aptBINDStatOverrideAbsent, errors.New(
		"dpkg-statoverride returned a conflicting, redirected, or non-canonical /var/cache/bind result",
	)
}

func verifyOrCreateExactAPTBindStatOverride(
	create bool,
	parentMode uint32,
	ops aptBINDStatOverrideOps,
) error {
	if ops.owner == nil || ops.list == nil || (create && ops.add == nil) ||
		(parentMode != aptBINDStockCacheParentMode &&
			parentMode != aptBINDCacheParentMode) {
		return errors.New("invalid APT BIND statoverride proof")
	}
	ownerOutput, ownerErr := ops.owner()
	if ownerErr != nil {
		return fmt.Errorf(
			"verify /var/cache/bind package ownership: %w", ownerErr,
		)
	}
	if string(ownerOutput) != aptBINDExactPackageOwnerLine {
		return errors.New(
			"/var/cache/bind is not the exact bind9 package-owned directory",
		)
	}
	output, err := ops.list()
	state, err := classifyExactAPTBindStatOverride(output, err)
	if err != nil {
		return err
	}
	if state == aptBINDStatOverrideExact {
		return nil
	}
	if !create {
		return errors.New(
			"/var/cache/bind lacks the exact durable dpkg-statoverride",
		)
	}
	addOutput, addErr := ops.add()
	unexpectedAddOutput := strings.TrimSpace(string(addOutput)) != ""
	readback, readbackErr := ops.list()
	readbackState, readbackParseErr := classifyExactAPTBindStatOverride(
		readback, readbackErr,
	)
	if readbackParseErr != nil || readbackState != aptBINDStatOverrideExact {
		if readbackParseErr == nil {
			readbackParseErr = errors.New(
				"dpkg-statoverride add did not publish the exact durable override",
			)
		}
		return errors.Join(addErr, readbackParseErr)
	}
	if unexpectedAddOutput {
		return errors.New(
			"dpkg-statoverride --add returned unexpected output",
		)
	}
	// A command can report failure after atomically committing its database
	// update. Exact readback is authoritative and makes the retry idempotent.
	return nil
}

type bindGroupLookupRunner func(context.Context, string, ...string) ([]byte, error)

func resolveBINDGroupGID(ctx context.Context) (uint32, error) {
	if ctx == nil {
		return 0, errors.New("BIND group proof requires a context")
	}
	getent, err := firstTrustedExecutable(
		[]string{"/usr/bin/getent", "/bin/getent"}, "getent",
	)
	if err != nil {
		return 0, err
	}
	return resolveBINDGroupGIDWithRunner(
		ctx, getent,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = aptBINDStatOverrideCommandEnvironment()
			return command.CombinedOutputLimited(4 << 10)
		},
	)
}

func resolveBINDGroupGIDWithRunner(
	ctx context.Context,
	getent string,
	runner bindGroupLookupRunner,
) (uint32, error) {
	if ctx == nil || getent == "" || runner == nil {
		return 0, errors.New("invalid BIND group proof")
	}
	output, err := runner(ctx, getent, "group", "bind")
	if err != nil {
		return 0, fmt.Errorf("resolve BIND service group: %w", err)
	}
	line := string(output)
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		return 0, errors.New("getent returned a non-canonical BIND group record")
	}
	fields := strings.Split(strings.TrimSuffix(line, "\n"), ":")
	if len(fields) != 4 || fields[0] != "bind" || fields[1] != "x" ||
		fields[2] == "" || fields[3] != "" {
		return 0, errors.New("getent returned an unsafe BIND group record")
	}
	value, err := strconv.ParseUint(fields[2], 10, 32)
	// Keep the identity portable through Go int/Fchown conversions and reject
	// MaxUint32, which is the chown(2) "leave unchanged" sentinel.
	if err != nil || value == 0 || value > uint64(1<<31-1) ||
		strconv.FormatUint(value, 10) != fields[2] {
		return 0, errors.New("BIND service group has an invalid numeric identity")
	}
	return uint32(value), nil
}

func ensureAPTBindGenerationRootAt(
	rootFD int,
	bindGID uint32,
	create bool,
	proveDurability func(uint32) error,
	afterChildReady func(),
) error {
	return ensureAPTBindGenerationRootAtWithMode(
		rootFD, bindGID, create, create, proveDurability, afterChildReady,
	)
}

func ensureAPTBindGenerationRootAtWithMode(
	rootFD int,
	bindGID uint32,
	allowParentHardening bool,
	createChild bool,
	proveDurability func(uint32) error,
	afterChildReady func(),
) error {
	if bindGID == 0 || proveDurability == nil {
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
	cacheFD, cacheIdentity, err := openInheritedBINDAnchorAt(
		varFD, "cache", "/var/cache",
	)
	if err != nil {
		return err
	}
	defer unix.Close(cacheFD)
	bindFD, bindIdentity, err := secureAPTBindCacheParentAt(
		cacheFD, bindGID, allowParentHardening, proveDurability,
	)
	if err != nil {
		return err
	}
	defer unix.Close(bindFD)

	childFD, openErr := openBINDDirectoryAt(
		bindFD, "celikpanel", "/var/cache/bind/celikpanel",
	)
	created := false
	if errors.Is(openErr, unix.ENOENT) && createChild {
		mkdirErr := unix.Mkdirat(bindFD, "celikpanel", bindManagedRootMode)
		if mkdirErr == nil {
			created = true
		} else if !errors.Is(mkdirErr, unix.EEXIST) {
			return fmt.Errorf("create /var/cache/bind/celikpanel: %w", mkdirErr)
		}
		childFD, openErr = openBINDDirectoryAt(
			bindFD, "celikpanel", "/var/cache/bind/celikpanel",
		)
	}
	if openErr != nil {
		return openErr
	}
	defer unix.Close(childFD)
	keepCreated := false
	defer func() {
		if created && !keepCreated {
			removeCreatedBINDDirectoryAt(bindFD, childFD)
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
		if err := unix.Fsync(bindFD); err != nil {
			return fmt.Errorf("sync /var/cache/bind after managed root creation: %w", err)
		}
	}
	childIdentity, err := validateExactBINDDirectoryFD(
		childFD, 0, 0, bindManagedRootMode, "/var/cache/bind/celikpanel",
	)
	if err != nil {
		return err
	}
	if afterChildReady != nil {
		afterChildReady()
	}
	if err := reverifyAPTBindGenerationRootAt(
		rootFD, bindGID, varIdentity, cacheIdentity, bindIdentity, childIdentity,
		proveDurability,
	); err != nil {
		return err
	}
	keepCreated = true
	return nil
}

func secureAPTBindCacheParentAt(
	cacheFD int,
	bindGID uint32,
	allowStickyUpgrade bool,
	proveDurability func(uint32) error,
) (int, bindDirectoryIdentity, error) {
	if proveDurability == nil {
		return -1, bindDirectoryIdentity{},
			errors.New("APT BIND parent durability proof is required")
	}
	bindFD, err := openBINDDirectoryAt(cacheFD, "bind", "/var/cache/bind")
	if err != nil {
		return -1, bindDirectoryIdentity{}, err
	}
	identity, exactErr := validateExactBINDDirectoryFD(
		bindFD, 0, bindGID, aptBINDCacheParentMode, "/var/cache/bind",
	)
	if exactErr == nil {
		if err := proveDurability(aptBINDCacheParentMode); err != nil {
			unix.Close(bindFD)
			return -1, bindDirectoryIdentity{}, err
		}
		return bindFD, identity, nil
	}
	if !allowStickyUpgrade {
		unix.Close(bindFD)
		return -1, bindDirectoryIdentity{}, exactErr
	}
	identity, stockErr := validateExactBINDDirectoryFD(
		bindFD, 0, bindGID, aptBINDStockCacheParentMode, "/var/cache/bind",
	)
	if stockErr != nil {
		unix.Close(bindFD)
		return -1, bindDirectoryIdentity{}, exactErr
	}
	if err := proveDurability(aptBINDStockCacheParentMode); err != nil {
		unix.Close(bindFD)
		return -1, bindDirectoryIdentity{}, err
	}
	if err := unix.Fchmod(bindFD, aptBINDCacheParentMode); err != nil {
		unix.Close(bindFD)
		return -1, bindDirectoryIdentity{}, fmt.Errorf(
			"add sticky protection to /var/cache/bind: %w", err,
		)
	}
	if err := unix.Fsync(bindFD); err != nil {
		unix.Close(bindFD)
		return -1, bindDirectoryIdentity{}, fmt.Errorf(
			"sync sticky /var/cache/bind: %w", err,
		)
	}
	if err := unix.Fsync(cacheFD); err != nil {
		unix.Close(bindFD)
		return -1, bindDirectoryIdentity{}, fmt.Errorf(
			"sync /var/cache after sticky BIND upgrade: %w", err,
		)
	}
	upgraded, err := validateExactBINDDirectoryFD(
		bindFD, 0, bindGID, aptBINDCacheParentMode, "/var/cache/bind",
	)
	if err != nil || upgraded != identity {
		unix.Close(bindFD)
		if err == nil {
			err = errors.New("/var/cache/bind identity changed during sticky upgrade")
		}
		return -1, bindDirectoryIdentity{}, err
	}
	return bindFD, upgraded, nil
}

func removeCreatedBINDDirectoryAt(parentFD, createdFD int) {
	var createdStat unix.Stat_t
	if unix.Fstat(createdFD, &createdStat) != nil {
		return
	}
	currentFD, err := openBINDDirectoryAt(
		parentFD, "celikpanel", "/var/cache/bind/celikpanel cleanup",
	)
	if err != nil {
		return
	}
	defer unix.Close(currentFD)
	var currentStat unix.Stat_t
	if unix.Fstat(currentFD, &currentStat) != nil ||
		currentStat.Dev != createdStat.Dev || currentStat.Ino != createdStat.Ino {
		return
	}
	_ = unix.Unlinkat(parentFD, "celikpanel", unix.AT_REMOVEDIR)
}

func reverifyAPTBindGenerationRootAt(
	rootFD int,
	bindGID uint32,
	wantVar, wantCache, wantBind, wantChild bindDirectoryIdentity,
	proveDurability func(uint32) error,
) error {
	if proveDurability == nil {
		return errors.New("APT BIND parent durability proof is required")
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
	cacheFD, cacheIdentity, err := openInheritedBINDAnchorAt(
		varFD, "cache", "/var/cache",
	)
	if err != nil {
		return err
	}
	defer unix.Close(cacheFD)
	bindFD, bindIdentity, err := openExactBINDDirectoryAt(
		cacheFD, "bind", 0, bindGID, aptBINDCacheParentMode, "/var/cache/bind",
	)
	if err != nil {
		return err
	}
	defer unix.Close(bindFD)
	if err := proveDurability(aptBINDCacheParentMode); err != nil {
		return err
	}
	childFD, childIdentity, err := openExactBINDDirectoryAt(
		bindFD, "celikpanel", 0, 0, bindManagedRootMode,
		"/var/cache/bind/celikpanel",
	)
	if err != nil {
		return err
	}
	defer unix.Close(childFD)
	if varIdentity != wantVar || cacheIdentity != wantCache ||
		bindIdentity != wantBind ||
		childIdentity != wantChild {
		return errors.New("managed BIND directory chain changed during verification")
	}
	return nil
}

// A pre-existing filesystem ancestor is not a directory this product created,
// and demanding an exact mode from one is asking the wrong question.
//
// What these walks actually need from `/`, `/etc`, `/var` and friends is that
// nobody but root can substitute an entry along the path: root ownership, no
// group or other write, and no setuid/setgid/sticky surprise. Owner write on
// `/` is not part of that: removing it is strictly less power, not more, and a
// root filesystem is equally safe from non-root mutation at 0555 as at 0755.
//
// The official Arch image ships `/` as 0555, so the exact-0755 expectation
// rejected a legitimate supported host before the DNS engine could reach even
// its intent journal — on the BIND path and, because every engine shares this
// mask-parent proof, on the PowerDNS path too (risk R-018).
//
// Directories this product creates and owns keep validateExactBINDDirectoryFD
// with an exact mode. That distinction is the whole point: assert exactly what
// we built, and assert only what matters about what we inherited.
//
// Önceden var olan bir dosya sistemi üst dizini, bu ürünün oluşturduğu bir
// dizin değildir; ondan tam bir kip istemek yanlış soruyu sormaktır.
//
// Bu yürüyüşlerin `/`, `/etc`, `/var` ve benzerlerinden gerçekten ihtiyacı
// olan şey, yol üzerindeki bir girdiyi root'tan başkasının değiştirememesidir:
// root sahipliği, grup ya da diğer yazma izni olmaması ve setuid/setgid/sticky
// sürprizi bulunmaması. `/` üzerindeki sahip yazma izni bunun parçası
// değildir: onu kaldırmak daha az yetkidir, daha çok değil; bir kök dosya
// sistemi root olmayan değişimden 0555'te de 0755'teki kadar korunaklıdır.
//
// Resmi Arch imajı `/` dizinini 0555 olarak sunar; bu yüzden tam-0755
// beklentisi, DNS motoru intent günlüğüne bile ulaşamadan meşru ve desteklenen
// bir sunucuyu reddediyordu — BIND yolunda ve, bu mask üst dizin kanıtını her
// motor paylaştığı için, PowerDNS yolunda da (risk R-018).
//
// Bu ürünün oluşturup sahiplendiği dizinler tam kiple
// validateExactBINDDirectoryFD kullanmayı sürdürür. Ayrım tam da budur:
// kurduğumuz şeyi birebir doğrula, devraldığımız şeyde yalnız önemli olanı.
func validateInheritedBINDAnchorFD(fd int, label string) (bindDirectoryIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return bindDirectoryIdentity{}, fmt.Errorf("stat %s: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return bindDirectoryIdentity{}, fmt.Errorf("%s is not a directory", label)
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return bindDirectoryIdentity{}, fmt.Errorf(
			"%s has uid:gid %d:%d, want 0:0", label, stat.Uid, stat.Gid,
		)
	}
	permissions := stat.Mode & 0o7777
	if permissions&0o022 != 0 {
		return bindDirectoryIdentity{}, fmt.Errorf(
			"%s has mode %04o and is group- or world-writable", label, permissions,
		)
	}
	// These ancestors are shared system directories: the unprivileged services
	// that live below them have to traverse them. A parent that is not
	// world-traversable is not a stricter variant of a normal system path, it
	// is an anomaly, and this policy deliberately keeps refusing it rather than
	// widening into "anything root owns".
	// Bu üst dizinler paylaşılan sistem dizinleridir: altlarında yaşayan
	// yetkisiz servislerin onları geçmesi gerekir. Herkesçe geçilemeyen bir üst
	// dizin, normal bir sistem yolunun daha katı bir çeşidi değil bir
	// anormalliktir; bu politika "root neye sahipse kabul" noktasına genişlemek
	// yerine onu reddetmeyi bilerek sürdürür.
	if permissions&0o001 == 0 {
		return bindDirectoryIdentity{}, fmt.Errorf(
			"%s has mode %04o and is not world-traversable", label, permissions,
		)
	}
	if special := stat.Mode & uint32(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX); special != 0 {
		return bindDirectoryIdentity{}, fmt.Errorf(
			"%s carries setuid, setgid or sticky bits", label,
		)
	}
	if err := rejectBINDDirectoryACL(fd, label); err != nil {
		return bindDirectoryIdentity{}, err
	}
	return bindDirectoryIdentity{
		Device: uint64(stat.Dev),
		Inode:  stat.Ino,
	}, nil
}

func openInheritedBINDAnchorAt(
	parentFD int,
	name string,
	label string,
) (int, bindDirectoryIdentity, error) {
	fd, err := openBINDDirectoryAt(parentFD, name, label)
	if err != nil {
		return -1, bindDirectoryIdentity{}, err
	}
	identity, err := validateInheritedBINDAnchorFD(fd, label)
	if err != nil {
		unix.Close(fd)
		return -1, bindDirectoryIdentity{}, err
	}
	return fd, identity, nil
}

func openExactBINDDirectoryAt(
	parentFD int,
	name string,
	uid, gid, mode uint32,
	label string,
) (int, bindDirectoryIdentity, error) {
	fd, err := openBINDDirectoryAt(parentFD, name, label)
	if err != nil {
		return -1, bindDirectoryIdentity{}, err
	}
	identity, err := validateExactBINDDirectoryFD(fd, uid, gid, mode, label)
	if err != nil {
		unix.Close(fd)
		return -1, bindDirectoryIdentity{}, err
	}
	return fd, identity, nil
}

func openBINDDirectoryAt(parentFD int, name, label string) (int, error) {
	if name == "" || name == "." || name == ".." {
		return -1, fmt.Errorf("%s has an invalid path component", label)
	}
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{
		Flags: uint64(
			unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("%s requires Linux openat2: %w", label, err)
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
		return -1, fmt.Errorf("%s refused a symbolic link or path escape: %w", label, err)
	}
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", label, err)
	}
	return fd, nil
}

func validateExactBINDDirectoryFD(
	fd int,
	uid, gid, mode uint32,
	label string,
) (bindDirectoryIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return bindDirectoryIdentity{}, fmt.Errorf("stat %s: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return bindDirectoryIdentity{}, fmt.Errorf("%s is not a directory", label)
	}
	if stat.Uid != uid || stat.Gid != gid {
		return bindDirectoryIdentity{}, fmt.Errorf(
			"%s has uid:gid %d:%d, want %d:%d",
			label, stat.Uid, stat.Gid, uid, gid,
		)
	}
	if stat.Mode&bindDirectoryModeMask != mode {
		return bindDirectoryIdentity{}, fmt.Errorf(
			"%s has mode %04o, want %04o",
			label, stat.Mode&bindDirectoryModeMask, mode,
		)
	}
	if err := rejectBINDDirectoryACL(fd, label); err != nil {
		return bindDirectoryIdentity{}, err
	}
	return bindDirectoryIdentity{
		Device: uint64(stat.Dev),
		Inode:  stat.Ino,
	}, nil
}

func rejectBINDDirectoryACL(fd int, label string) error {
	for _, name := range []string{
		"system.posix_acl_access",
		"system.posix_acl_default",
	} {
		size, err := unix.Fgetxattr(fd, name, nil)
		if err == nil && size > 0 {
			return fmt.Errorf("%s has an unsupported POSIX ACL", label)
		}
		if err != nil && !errors.Is(err, unix.ENODATA) &&
			!errors.Is(err, unix.ENOTSUP) {
			return fmt.Errorf("inspect %s ACL: %w", label, err)
		}
	}
	return nil
}
