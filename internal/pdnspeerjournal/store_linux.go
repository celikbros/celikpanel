//go:build linux

package pdnspeerjournal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"golang.org/x/sys/unix"
)

var processMu sync.Mutex

// VerifyCurrent must check both held host/publication locks, the current
// accepted ledger request/owner, and unchanged native/enrollment evidence.
// The caller holds those locks across this method. A nil verifier is refused.
// Cross-process exclusion comes from the caller's external locks, not this
// package's process-local mutex.
type VerifyCurrent func() error

// Read only observes the durable sidecar and distinguishes absence from unsafe
// or unreadable evidence. It grants no permission to issue/consume a proof.
func Read() (RecordV1, error) { return store{path: Path, now: time.Now}.read() }

// Publish durably records an outstanding challenge before any network request.
// Every call must mint a fresh nonce and the next proof-attempt number for the
// identical operation/catalog/enrollment context. An outstanding attempt may be
// superseded after a process restart; late responses then fail exact matching.
// Never reuse Read().Request after restart or rollback, even if outstanding.
func Publish(request pdnspeerproof.RequestV1, enrollmentSHA256 string, ledgerAttempt uint64, verify VerifyCurrent) error {
	return store{path: Path, now: time.Now}.publish(request, enrollmentSHA256, ledgerAttempt, verify, nil)
}

// ConsumeOnce durably marks exactly the published challenge consumed. It is
// suitable as the final callback of pdnspeerproof.Verify after peer/channel
// authentication and proof validation. A later terminal ledger result is a
// separate caller obligation; a crash before it requires a fresh challenge.
func ConsumeOnce(request pdnspeerproof.RequestV1, enrollmentSHA256, requestSHA256 string, ledgerAttempt uint64, verify VerifyCurrent) error {
	return store{path: Path, now: time.Now}.consume(request, enrollmentSHA256, requestSHA256, ledgerAttempt, verify, nil)
}

// Retire removes an exact challenge only after the caller proves the prior
// operation is terminal in the current ledger under the external locks. A
// missing journal is not a successful proof; callers inspect terminal state
// separately. This permits a later independent deletion to publish attempt 1.
func Retire(request pdnspeerproof.RequestV1, enrollmentSHA256, requestSHA256 string, ledgerAttempt uint64, verifyTerminal VerifyCurrent) error {
	return store{path: Path, now: time.Now}.retire(request, enrollmentSHA256, requestSHA256, ledgerAttempt, verifyTerminal)
}

type store struct {
	path      string
	trustRoot string // test-only bounded root; production defaults to /
	now       func() time.Time
}

func (s store) read() (RecordV1, error) {
	if os.Geteuid() != 0 {
		return RecordV1{}, StateError{Unknown}
	}
	dir, name, err := s.openDirectory()
	if err != nil {
		return RecordV1{}, StateError{Unknown}
	}
	defer unix.Close(dir)
	if countOrphanStages(dir) != nil {
		return RecordV1{}, StateError{Unknown}
	}
	return readAt(dir, name)
}

func (s store) publish(request pdnspeerproof.RequestV1, enrollment string, ledgerAttempt uint64, verify VerifyCurrent, checkpoint func(string)) error {
	processMu.Lock()
	defer processMu.Unlock()
	if os.Geteuid() != 0 || verify == nil || !hex32(enrollment) || ledgerAttempt == 0 {
		return StateError{Unknown}
	}
	digest, err := pdnspeerproof.RequestSHA256(request)
	if err != nil {
		return StateError{Unknown}
	}
	now := s.now().Unix()
	if request.IssuedAtUnix > now || now > request.ExpiresAtUnix {
		return StateError{Unknown}
	}
	if err = verify(); err != nil {
		return StateError{Unknown}
	}
	dir, name, err := s.openDirectory()
	if err != nil {
		return StateError{Unknown}
	}
	defer unix.Close(dir)
	if countOrphanStages(dir) != nil {
		return StateError{Unknown}
	}
	previous, err := readAt(dir, name)
	if err != nil && !IsCode(err, Missing) {
		return err
	}
	if err == nil {
		next := RecordV1{Schema: SchemaV1, State: StateOutstanding, LedgerAttempt: ledgerAttempt, EnrollmentSHA256: enrollment, RequestSHA256: digest, Request: request}
		if !sameContext(previous, next) {
			return StateError{Mismatch}
		}
		if ledgerAttempt < previous.LedgerAttempt {
			return StateError{Replay}
		}
		if request.Attempt != previous.Request.Attempt+1 || request.Nonce == previous.Request.Nonce {
			return StateError{Replay}
		}
	} else if request.Attempt != 1 {
		return StateError{Missing}
	}
	newRecord := RecordV1{Schema: SchemaV1, State: StateOutstanding, LedgerAttempt: ledgerAttempt, EnrollmentSHA256: enrollment, RequestSHA256: digest, Request: request}
	var before *RecordV1
	if err == nil {
		before = &previous
	}
	return writeAt(dir, name, newRecord, before, s, verify, checkpoint)
}

func (s store) consume(request pdnspeerproof.RequestV1, enrollment, digest string, ledgerAttempt uint64, verify VerifyCurrent, checkpoint func(string)) error {
	processMu.Lock()
	defer processMu.Unlock()
	if os.Geteuid() != 0 || verify == nil || !hex32(enrollment) || !hex32(digest) || ledgerAttempt == 0 {
		return StateError{Unknown}
	}
	calculated, err := pdnspeerproof.RequestSHA256(request)
	if err != nil || calculated != digest {
		return StateError{Mismatch}
	}
	if err = verify(); err != nil {
		return StateError{Unknown}
	}
	dir, name, err := s.openDirectory()
	if err != nil {
		return StateError{Unknown}
	}
	defer unix.Close(dir)
	if countOrphanStages(dir) != nil {
		return StateError{Unknown}
	}
	current, err := readAt(dir, name)
	if err != nil {
		return err
	}
	if current.State == StateConsumed && current.RequestSHA256 == digest {
		return StateError{Replay}
	}
	if current.State != StateOutstanding || current.RequestSHA256 != digest || current.EnrollmentSHA256 != enrollment || current.LedgerAttempt != ledgerAttempt || current.Request != request {
		return StateError{Mismatch}
	}
	if s.now().Unix() > request.ExpiresAtUnix {
		return StateError{Replay}
	}
	before := current
	current.State = StateConsumed
	return writeAt(dir, name, current, &before, s, verify, checkpoint)
}

func (s store) retire(request pdnspeerproof.RequestV1, enrollment, digest string, ledgerAttempt uint64, verifyTerminal VerifyCurrent) error {
	return s.retireAt(request, enrollment, digest, ledgerAttempt, verifyTerminal, nil)
}

// retireAt is split only to inject a deterministic owner edit at the narrow
// pre-exchange boundary in tests. Production passes no checkpoint.
func (s store) retireAt(request pdnspeerproof.RequestV1, enrollment, digest string, ledgerAttempt uint64, verifyTerminal VerifyCurrent, checkpoint func(string)) error {
	processMu.Lock()
	defer processMu.Unlock()
	if os.Geteuid() != 0 || verifyTerminal == nil || ledgerAttempt == 0 || !hex32(enrollment) || !hex32(digest) {
		return StateError{Unknown}
	}
	calculated, err := pdnspeerproof.RequestSHA256(request)
	if err != nil || calculated != digest {
		return StateError{Mismatch}
	}
	if verifyTerminal() != nil {
		return StateError{Unknown}
	}
	dir, name, err := s.openDirectory()
	if err != nil {
		return StateError{Unknown}
	}
	defer unix.Close(dir)
	if countOrphanStages(dir) != nil {
		return StateError{Unknown}
	}
	current, err := readAt(dir, name)
	if err != nil {
		return err
	}
	if current.Request != request || current.RequestSHA256 != digest ||
		current.EnrollmentSHA256 != enrollment || current.LedgerAttempt != ledgerAttempt {
		return StateError{Mismatch}
	}
	if verifyTerminal() != nil || s.verifyDirectory(dir) != nil {
		return StateError{Unknown}
	}
	again, err := readAt(dir, name)
	if err != nil || again != current {
		return StateError{Unknown}
	}
	var oldStat unix.Stat_t
	if unix.Fstatat(dir, name, &oldStat, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!safeFile(oldStat) {
		return StateError{Unknown}
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return StateError{Unknown}
	}
	retired := ".pdns-peer-retired-" + hex.EncodeToString(nonce[:])
	// A durable empty tombstone is exchanged with the target. The displaced
	// journal stays under the private name for identity/content inspection.
	fd, err := unix.Openat(dir, retired, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return StateError{Unknown}
	}
	tombstone := os.NewFile(uintptr(fd), retired)
	defer tombstone.Close()
	retain := false
	defer func() {
		if !retain {
			_ = unix.Unlinkat(dir, retired, 0)
		}
	}()
	if unix.Fchown(fd, 0, 0) != nil || tombstone.Chmod(0600) != nil || tombstone.Sync() != nil {
		return StateError{Unknown}
	}
	var tombstoneStat, namedTombstone unix.Stat_t
	if unix.Fstat(fd, &tombstoneStat) != nil || !safeFile(tombstoneStat) || tombstoneStat.Size != 0 ||
		unix.Fstatat(dir, retired, &namedTombstone, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameFile(tombstoneStat, namedTombstone) ||
		verifyTerminal() != nil || s.verifyDirectory(dir) != nil {
		return StateError{Unknown}
	}
	beforeExchange, err := readAt(dir, name)
	if err != nil || beforeExchange != current {
		return StateError{Unknown}
	}
	if checkpoint != nil {
		checkpoint("before_exchange")
	}
	if unix.Renameat2(dir, name, dir, retired, unix.RENAME_EXCHANGE) != nil {
		return StateError{Unknown}
	}
	retain = true
	var displaced, canonicalTombstone unix.Stat_t
	moved, err := readAt(dir, retired)
	if err != nil || moved != current ||
		unix.Fstatat(dir, retired, &displaced, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameFile(oldStat, displaced) || oldStat.Mtim != displaced.Mtim ||
		unix.Fstatat(dir, name, &canonicalTombstone, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameFile(tombstoneStat, canonicalTombstone) ||
		verifyTerminal() != nil || s.verifyDirectory(dir) != nil {
		_ = unix.Fsync(dir)
		return StateError{Unknown}
	}
	if unix.Fsync(dir) != nil {
		return StateError{Unknown}
	}
	if checkpoint != nil {
		checkpoint("exchanged_durable")
	}
	// Make the canonical name absent while the old evidence is still retained.
	if unix.Unlinkat(dir, name, 0) != nil || unix.Fsync(dir) != nil {
		return StateError{Unknown}
	}
	if checkpoint != nil {
		checkpoint("canonical_absent_durable")
	}
	if unix.Unlinkat(dir, retired, 0) != nil || unix.Fsync(dir) != nil {
		return StateError{Unknown}
	}
	retain = false
	return nil
}

func (s store) openDirectory() (int, string, error) {
	if !filepath.IsAbs(s.path) || filepath.Clean(s.path) != s.path || s.path == "/" {
		return -1, "", StateError{Unknown}
	}
	root := s.trustRoot
	if root == "" {
		root = "/"
	}
	parent := filepath.Dir(s.path)
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return -1, "", StateError{Unknown}
	}
	relative, err := filepath.Rel(root, parent)
	if err != nil || relative == ".." || len(relative) >= 3 && relative[:3] == "../" {
		return -1, "", StateError{Unknown}
	}
	current, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, "", StateError{Unknown}
	}
	check := func(fd int, final bool) bool {
		var st unix.Stat_t
		return unix.Fstat(fd, &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFDIR &&
			st.Uid == 0 && st.Mode&0022 == 0 && (!final || st.Mode&0007 == 0)
	}
	if !check(current, relative == ".") {
		unix.Close(current)
		return -1, "", StateError{Unknown}
	}
	if relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			next, openErr := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			unix.Close(current)
			if openErr != nil {
				return -1, "", StateError{Unknown}
			}
			current = next
			if !check(current, part == filepath.Base(parent)) {
				unix.Close(current)
				return -1, "", StateError{Unknown}
			}
		}
	}
	return current, filepath.Base(s.path), nil
}

func (s store) verifyDirectory(dir int) error {
	other, _, err := s.openDirectory()
	if err != nil {
		return StateError{Unknown}
	}
	defer unix.Close(other)
	var held, named unix.Stat_t
	if unix.Fstat(dir, &held) != nil || unix.Fstat(other, &named) != nil || !sameFile(held, named) {
		return StateError{Unknown}
	}
	return nil
}

func readAt(dir int, name string) (RecordV1, error) {
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return RecordV1{}, StateError{Missing}
	}
	if err != nil {
		return RecordV1{}, StateError{Unknown}
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before, named, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || unix.Fstatat(dir, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!safeFile(before) || !sameFile(before, named) || before.Size == 0 || before.Size > MaxBytes {
		return RecordV1{}, StateError{Unknown}
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil || len(raw) != int(before.Size) || unix.Fstat(fd, &after) != nil || !sameFile(before, after) ||
		before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return RecordV1{}, StateError{Unknown}
	}
	return Decode(raw)
}

func safeFile(st unix.Stat_t) bool {
	return st.Mode == unix.S_IFREG|0600 && st.Uid == 0 && st.Gid == 0 && st.Nlink == 1
}
func sameFile(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size
}

func writeAt(dir int, name string, record RecordV1, before *RecordV1, s store, verify VerifyCurrent, checkpoint func(string)) error {
	raw, err := Encode(record)
	if err != nil {
		return err
	}
	if err = verify(); err != nil {
		return StateError{Unknown}
	}
	if countOrphanStages(dir) != nil {
		return StateError{Unknown}
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return StateError{Unknown}
	}
	stage := ".pdns-peer-challenge-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(dir, stage, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return StateError{Unknown}
	}
	retainStage := false
	defer func() {
		if !retainStage {
			_ = unix.Unlinkat(dir, stage, 0)
		}
	}()
	file := os.NewFile(uintptr(fd), stage)
	defer file.Close()
	if unix.Fchown(fd, 0, 0) != nil || file.Chmod(0600) != nil {
		return StateError{Unknown}
	}
	if _, err = file.Write(raw); err != nil {
		return StateError{Unknown}
	}
	if err = file.Sync(); err != nil {
		return StateError{Unknown}
	}
	if checkpoint != nil {
		checkpoint("staged")
	}
	if err = verify(); err != nil {
		return StateError{Unknown}
	}
	if err = s.verifyDirectory(dir); err != nil {
		return err
	}
	current, readErr := readAt(dir, name)
	if before == nil {
		if !IsCode(readErr, Missing) {
			return StateError{Unknown}
		}
	} else if readErr != nil || current != *before {
		return StateError{Unknown}
	}
	var oldStat unix.Stat_t
	if before != nil {
		if unix.Fstatat(dir, name, &oldStat, unix.AT_SYMLINK_NOFOLLOW) != nil ||
			!safeFile(oldStat) {
			return StateError{Unknown}
		}
	}
	var staged, named unix.Stat_t
	if unix.Fstat(fd, &staged) != nil || !safeFile(staged) || staged.Size != int64(len(raw)) ||
		unix.Fstatat(dir, stage, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameFile(staged, named) {
		return StateError{Unknown}
	}
	if checkpoint != nil {
		checkpoint("before_exchange")
	}
	// The caller-held locks serialize conforming writers. For an existing
	// journal exchange the two names, then inspect the displaced before-image.
	// A direct owner edit in the small check/exchange window is retained under
	// the private stage name and yields Unknown, never silent evidence loss.
	if before == nil {
		err = unix.Renameat2(dir, stage, dir, name, unix.RENAME_NOREPLACE)
	} else {
		err = unix.Renameat2(dir, stage, dir, name, unix.RENAME_EXCHANGE)
	}
	if err != nil {
		return StateError{Unknown}
	}
	if before != nil {
		retainStage = true
	}
	if before != nil {
		var displaced unix.Stat_t
		oldRecord, oldErr := readAt(dir, stage)
		if unix.Fstatat(dir, stage, &displaced, unix.AT_SYMLINK_NOFOLLOW) != nil ||
			oldErr != nil || oldRecord != *before || !sameFile(oldStat, displaced) ||
			oldStat.Mtim != displaced.Mtim {
			retainStage = true
			_ = unix.Fsync(dir)
			return StateError{Unknown}
		}
	}
	if checkpoint != nil {
		checkpoint("published")
	}
	if err = unix.Fsync(dir); err != nil {
		return StateError{Unknown}
	}
	if checkpoint != nil {
		checkpoint("parent_durable")
	}
	if before != nil {
		if unix.Unlinkat(dir, stage, 0) != nil || unix.Fsync(dir) != nil {
			return StateError{Unknown}
		}
		retainStage = false
	}
	stored, err := readAt(dir, name)
	if err != nil || stored != record || !bytes.Equal(raw, mustEncode(stored)) || s.verifyDirectory(dir) != nil || verify() != nil {
		return StateError{Unknown}
	}
	return nil
}

// Any unexplained private stage may be a displaced owner edit or an
// interrupted publication. It is evidence, never disposable scratch. Stop
// further proof work until an explicit owner recovery classifies it.
func countOrphanStages(dir int) error {
	duplicate, err := unix.Dup(dir)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(duplicate), "dns-peer-journal-directory")
	defer file.Close()
	names, err := file.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		if strings.HasPrefix(name, ".pdns-peer-challenge-") || strings.HasPrefix(name, ".pdns-peer-retired-") {
			return StateError{Unknown}
		}
	}
	return nil
}

func mustEncode(record RecordV1) []byte { raw, _ := Encode(record); return raw }
