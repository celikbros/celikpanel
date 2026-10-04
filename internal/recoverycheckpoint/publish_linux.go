//go:build linux

package recoverycheckpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"golang.org/x/sys/unix"
)

const transactionRoot = "/var/lib/celikpanel-release-transaction"

type runtimeProof struct {
	root, digest string
	revalidate   func() error
	close        func() error
}
type worker struct {
	pid                     int
	start, invocation, boot string
}
type config struct {
	anchor, transaction, output string
	fd                          int
	resolve                     func() (runtimeProof, error)
	observe                     func(runtimeProof) (worker, error)
	now                         func() time.Time
	beforeCommit                func() // deterministic private test seam; production nil
}

// Publish reads only fixed native roots and inherited descriptor 9. Environment
// variables cannot select another transaction, runtime, process or destination.
// Callers must treat an error as unavailable observation, not an action failure.
func Publish(name string) error {
	if os.Geteuid() != 0 || os.Getegid() != 0 || !ValidName(name) {
		return ErrUnavailable
	}
	return publish(name, config{anchor: "/", transaction: transactionRoot, output: Root, fd: 9, now: time.Now,
		resolve: func() (runtimeProof, error) {
			r, err := recoveryruntime.Resolve()
			if err != nil {
				return runtimeProof{}, ErrUnavailable
			}
			return runtimeProof{r.Root, r.Digest, r.Revalidate, r.Close}, nil
		}, observe: observeWorker})
}
func statSame(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
func safeDir(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Uid == 0 && st.Gid == 0 && st.Mode&07022 == 0
}
func openDir(anchor, path string) (*os.File, error) {
	if !filepath.IsAbs(anchor) || filepath.Clean(anchor) != anchor || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnavailable
	}
	relative, err := filepath.Rel(anchor, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(anchor, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "checkpoint-directory")
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || !safeDir(st) {
		file.Close()
		return nil, ErrUnavailable
	}
	if relative == "." {
		return file, nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		next, err := unix.Openat(int(file.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		file.Close()
		if err != nil {
			return nil, ErrUnavailable
		}
		file = os.NewFile(uintptr(next), "checkpoint-directory")
		if unix.Fstat(next, &st) != nil || !safeDir(st) {
			file.Close()
			return nil, ErrUnavailable
		}
	}
	return file, nil
}
func readAt(directory *os.File, name string, limit int64) ([]byte, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return nil, ErrUnavailable
	}
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "checkpoint-input")
	defer file.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0600 || before.Uid != 0 || before.Gid != 0 || before.Nlink != 1 || before.Size < 0 || before.Size > limit {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit || unix.Fstat(fd, &after) != nil || !statSame(before, after) {
		return nil, ErrUnavailable
	}
	return raw, nil
}
func readTransaction(directory *os.File) (marker, error) {
	var observed []marker
	for _, phase := range []string{"active", "completion.pending", "scheduler-restore.pending"} {
		raw, err := readAt(directory, phase, 4096)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return marker{}, ErrUnavailable
		}
		value, err := parseMarker(raw, phase)
		if err != nil {
			return marker{}, err
		}
		observed = append(observed, value)
	}
	var quiesce unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), "quiesce.pending", &quiesce, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return marker{}, ErrUnavailable
	}
	if len(observed) == 2 && observed[0].Phase == "completion.pending" && observed[1].Phase == "scheduler-restore.pending" {
		a, b := observed[0], observed[1]
		b.Phase = a.Phase
		if a == b {
			observed = observed[:1]
		}
	}
	if len(observed) != 1 {
		return marker{}, ErrUnavailable
	}
	return observed[0], nil
}
func verifyLock(directory *os.File, fd int) error {
	var held, path unix.Stat_t
	if unix.Fstat(fd, &held) != nil || unix.Fstatat(int(directory.Fd()), "transaction.lock", &path, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!statSame(held, path) || held.Mode&unix.S_IFMT != unix.S_IFREG || held.Mode&07777 != 0600 || held.Uid != 0 || held.Gid != 0 || held.Nlink != 1 {
		return ErrUnavailable
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil || len(raw) > 16384 {
		return ErrUnavailable
	}
	exclusive := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 9 && fields[0] == "lock:" && fields[2] == "FLOCK" && fields[3] == "ADVISORY" && fields[4] == "WRITE" && fields[7] == "0" && fields[8] == "EOF" {
			exclusive = true
		}
	}
	if !exclusive {
		return ErrUnavailable
	}
	other, err := unix.Openat(int(directory.Fd()), "transaction.lock", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(other)
	err = unix.Flock(other, unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		unix.Flock(other, unix.LOCK_UN)
		return ErrUnavailable
	}
	if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		return ErrUnavailable
	}
	return nil
}
func procRead(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrUnavailable
	}
	return raw, nil
}
func unitProperties() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", Unit, "-p", "Id", "-p", "LoadState", "-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "ControlGroup", "-p", "InvocationID")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	raw, err := cmd.Output()
	if err != nil || len(raw) > 8192 {
		return nil, ErrUnavailable
	}
	return parseUnitProperties(raw)
}
func parseUnitProperties(raw []byte) (map[string]string, error) {
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, ErrUnavailable
		}
		if _, exists := values[key]; exists {
			return nil, ErrUnavailable
		}
		values[key] = value
	}
	if len(values) != 7 || values["Id"] != Unit || values["LoadState"] != "loaded" ||
		(values["ActiveState"] != "active" && values["ActiveState"] != "activating") || (values["SubState"] != "running" && values["SubState"] != "start") ||
		values["ControlGroup"] != "/system.slice/"+Unit || !hex32.MatchString(values["InvocationID"]) {
		return nil, ErrUnavailable
	}
	return values, nil
}
func observeWorker(runtime runtimeProof) (worker, error) {
	values, err := unitProperties()
	if err != nil {
		return worker{}, err
	}
	pid, err := strconv.Atoi(values["MainPID"])
	if err != nil || pid <= 1 {
		return worker{}, ErrUnavailable
	}
	currentGroup, err := procRead("/proc/self/cgroup", 4096)
	if err != nil || string(currentGroup) != "0::/system.slice/"+Unit+"\n" {
		return worker{}, ErrUnavailable
	}
	prefix := fmt.Sprintf("/proc/%d/", pid)
	group, err := procRead(prefix+"cgroup", 4096)
	if err != nil || !bytes.Equal(group, currentGroup) {
		return worker{}, ErrUnavailable
	}
	raw, err := procRead(prefix+"stat", 16384)
	if err != nil {
		return worker{}, err
	}
	start, err := processStart(raw)
	if err != nil {
		return worker{}, err
	}
	command, err := procRead(prefix+"cmdline", 8192)
	if err != nil {
		return worker{}, err
	}
	valid := false
	for _, entry := range []string{"deploy/recovery/runtime-entry.sh", "deploy/release-recovery-runner.sh"} {
		if string(command) == "/bin/bash\x00"+filepath.Join(runtime.root, entry)+"\x00" {
			valid = true
		}
	}
	if !valid {
		return worker{}, ErrUnavailable
	}
	boot, err := procRead("/proc/sys/kernel/random/boot_id", 128)
	if err != nil {
		return worker{}, err
	}
	bootID := strings.TrimSpace(string(boot))
	if !uuid.MatchString(bootID) {
		return worker{}, ErrUnavailable
	}
	after, err := unitProperties()
	if err != nil || len(after) != len(values) {
		return worker{}, ErrUnavailable
	}
	for key, value := range values {
		if after[key] != value {
			return worker{}, ErrUnavailable
		}
	}
	statAfter, err := procRead(prefix+"stat", 16384)
	if err != nil {
		return worker{}, err
	}
	startAfter, err := processStart(statAfter)
	if err != nil || startAfter != start {
		return worker{}, ErrUnavailable
	}
	return worker{pid, start, values["InvocationID"], bootID}, nil
}
func sameDir(a, b *os.File) bool {
	var x, y unix.Stat_t
	return unix.Fstat(int(a.Fd()), &x) == nil && unix.Fstat(int(b.Fd()), &y) == nil && x.Dev == y.Dev && x.Ino == y.Ino && x.Mode == y.Mode && x.Uid == y.Uid && x.Gid == y.Gid
}
func outputDir(cfg config) (*os.File, error) {
	parent, err := openDir(cfg.anchor, filepath.Dir(cfg.output))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := unix.Mkdirat(int(parent.Fd()), filepath.Base(cfg.output), 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, ErrUnavailable
	}
	output, err := openDir(cfg.anchor, cfg.output)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if unix.Fstat(int(output.Fd()), &st) != nil || st.Mode&07777 != 0700 {
		output.Close()
		return nil, ErrUnavailable
	}
	if parent.Sync() != nil {
		output.Close()
		return nil, ErrUnavailable
	}
	return output, nil
}
func publish(name string, cfg config) error {
	if !ValidName(name) {
		return ErrUnavailable
	}
	transaction, err := openDir(cfg.anchor, cfg.transaction)
	if err != nil {
		return err
	}
	defer transaction.Close()
	if err := verifyLock(transaction, cfg.fd); err != nil {
		return err
	}
	initial, err := readTransaction(transaction)
	if err != nil {
		return err
	}
	runtime, err := cfg.resolve()
	if err != nil {
		return ErrUnavailable
	}
	defer runtime.close()
	if !hex64.MatchString(runtime.digest) || runtime.revalidate() != nil {
		return ErrUnavailable
	}
	identity, err := cfg.observe(runtime)
	if err != nil {
		return ErrUnavailable
	}
	record := Record{Schema: Schema, Snapshot: initial.Snapshot, TokenHash: initial.TokenHash, Operation: initial.Operation, Phase: initial.Phase,
		Checkpoint: name, ObservedAt: cfg.now().UTC().Format(time.RFC3339Nano), Unit: Unit, InvocationID: identity.invocation, MainPID: identity.pid,
		MainStartTicks: identity.start, BootID: identity.boot, RuntimeManifest: runtime.digest}
	output, err := outputDir(cfg)
	if err != nil {
		return err
	}
	defer output.Close()
	lockfd, err := unix.Openat(int(output.Fd()), ".publish.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(lockfd)
	var lockStat unix.Stat_t
	if unix.Fstat(lockfd, &lockStat) != nil || lockStat.Mode&unix.S_IFMT != unix.S_IFREG || lockStat.Mode&07777 != 0600 || lockStat.Uid != 0 || lockStat.Gid != 0 || lockStat.Nlink != 1 || unix.Flock(lockfd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return ErrUnavailable
	}
	destination := initial.TokenHash + ".json"
	var prior *Record
	raw, err := readAt(output, destination, 4096)
	if err == nil {
		old, e := decode(raw)
		if e != nil {
			return e
		}
		prior = &old
	} else if !errors.Is(err, unix.ENOENT) {
		return ErrUnavailable
	}
	record, err = next(prior, record)
	if err != nil {
		return err
	}
	encoded, err := encode(record)
	if err != nil {
		return err
	}
	revalidate := func() error {
		fresh, err := openDir(cfg.anchor, cfg.transaction)
		if err != nil {
			return err
		}
		defer fresh.Close()
		if !sameDir(transaction, fresh) || verifyLock(fresh, cfg.fd) != nil {
			return ErrUnavailable
		}
		current, err := readTransaction(fresh)
		if err != nil || current != initial {
			return ErrUnavailable
		}
		freshOutput, err := openDir(cfg.anchor, cfg.output)
		if err != nil {
			return err
		}
		defer freshOutput.Close()
		if !sameDir(output, freshOutput) {
			return ErrUnavailable
		}
		actual, e := cfg.observe(runtime)
		if e != nil || actual != identity || runtime.revalidate() != nil {
			return ErrUnavailable
		}
		again, e := readAt(output, destination, 4096)
		if prior == nil {
			if !errors.Is(e, unix.ENOENT) {
				return ErrUnavailable
			}
		} else if e != nil || !bytes.Equal(raw, again) {
			return ErrUnavailable
		}
		return nil
	}
	if cfg.beforeCommit != nil {
		cfg.beforeCommit()
	}
	if revalidate() != nil {
		return ErrUnavailable
	}
	// The fd-relative exclusive stage cannot follow a replaced pathname.
	stage := ".checkpoint-" + record.TokenHash + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	fd, err := unix.Openat(int(output.Fd()), stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrUnavailable
	}
	staged := os.NewFile(uintptr(fd), "checkpoint-stage")
	defer unix.Unlinkat(int(output.Fd()), stage, 0)
	if _, err := staged.Write(encoded); err != nil {
		staged.Close()
		return ErrUnavailable
	}
	if staged.Sync() != nil {
		staged.Close()
		return ErrUnavailable
	}
	if staged.Close() != nil {
		return ErrUnavailable
	}
	if revalidate() != nil {
		return ErrUnavailable
	}
	if unix.Renameat(int(output.Fd()), stage, int(output.Fd()), destination) != nil || output.Sync() != nil {
		return ErrUnavailable
	}
	return nil
}
