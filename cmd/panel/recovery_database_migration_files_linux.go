//go:build linux

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// These are file identities, not publication-tree roots: mtime remains bound.
// Only the ctime caused by our recorded rename may differ during replay.
type databaseMigrationIdentity struct {
	Dev   uint64        `json:"dev"`
	Ino   uint64        `json:"ino"`
	Mode  uint32        `json:"mode"`
	UID   uint32        `json:"uid"`
	GID   uint32        `json:"gid"`
	Links uint64        `json:"links"`
	Size  int64         `json:"size"`
	Mtime unix.Timespec `json:"mtime"`
	Ctime unix.Timespec `json:"ctime"`
}
type databaseMigrationFile struct {
	Identity databaseMigrationIdentity `json:"identity"`
	SHA256   string                    `json:"sha256"`
}
type databaseMigrationDirectory struct {
	Dev  uint64 `json:"dev"`
	Ino  uint64 `json:"ino"`
	Mode uint32 `json:"mode"`
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
}

func databaseMigrationID(st unix.Stat_t) databaseMigrationIdentity {
	return databaseMigrationIdentity{uint64(st.Dev), st.Ino, st.Mode, st.Uid, st.Gid, uint64(st.Nlink), st.Size, st.Mtim, st.Ctim}
}
func databaseMigrationDirID(st unix.Stat_t) databaseMigrationDirectory {
	return databaseMigrationDirectory{uint64(st.Dev), st.Ino, st.Mode, st.Uid, st.Gid}
}
func databaseMigrationDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func databaseMigrationCanonical(v any) []byte { b, _ := json.Marshal(v); return append(b, '\n') }
func databaseMigrationDecode(b []byte, v any) error {
	if len(b) == 0 || len(b) > 1024*1024 {
		return fmt.Errorf("database migration record size is invalid")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if !bytes.Equal(b, databaseMigrationCanonical(v)) {
		return fmt.Errorf("database migration record is not canonical")
	}
	return nil
}
func databaseMigrationNoAttributes(f *os.File) error {
	n, e := unix.Flistxattr(int(f.Fd()), nil)
	if errors.Is(e, unix.ENOTSUP) {
		return nil
	}
	if e != nil {
		return e
	}
	if n != 0 {
		return fmt.Errorf("database migration does not support extended attributes; preserve owner metadata")
	}
	return nil
}
func databaseMigrationStat(f *os.File) (unix.Stat_t, error) {
	var st unix.Stat_t
	e := unix.Fstat(int(f.Fd()), &st)
	return st, e
}
func databaseMigrationOpenAt(parent *os.File, name string, dir bool) (*os.File, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "\n\r\\") {
		return nil, fmt.Errorf("unsafe database migration entry")
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if dir {
		flags |= unix.O_DIRECTORY
	}
	fd, e := unix.Openat(int(parent.Fd()), name, flags, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name)), nil
}
func databaseMigrationOpenDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("unsafe database migration directory")
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "/")
	for _, name := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if name == "" {
			continue
		}
		next, e := databaseMigrationOpenAt(f, name, true)
		f.Close()
		if e != nil {
			return nil, e
		}
		f = next
	}
	return f, nil
}
func databaseMigrationDirectoryProof(f *os.File, uid, gid uint32, mode uint32) (databaseMigrationDirectory, error) {
	st, e := databaseMigrationStat(f)
	if e != nil {
		return databaseMigrationDirectory{}, e
	}
	if st.Mode != unix.S_IFDIR|mode || st.Uid != uid || st.Gid != gid {
		return databaseMigrationDirectory{}, fmt.Errorf("unsafe database migration directory metadata: %s", f.Name())
	}
	if e = databaseMigrationNoAttributes(f); e != nil {
		return databaseMigrationDirectory{}, e
	}
	return databaseMigrationDirID(st), nil
}
func databaseMigrationSameDirectory(f *os.File, id databaseMigrationDirectory) error {
	st, e := databaseMigrationStat(f)
	if e != nil {
		return e
	}
	if databaseMigrationDirID(st) != id {
		return fmt.Errorf("database migration directory metadata changed")
	}
	current, e := databaseMigrationOpenDirectory(f.Name())
	if e != nil {
		return e
	}
	defer current.Close()
	now, e := databaseMigrationStat(current)
	if e != nil {
		return e
	}
	if databaseMigrationDirID(now) != id {
		return fmt.Errorf("database migration directory path changed")
	}
	return databaseMigrationNoAttributes(f)
}
func databaseMigrationSameEntry(parent *os.File, name string, f *os.File) error {
	var path unix.Stat_t
	if e := unix.Fstatat(int(parent.Fd()), name, &path, unix.AT_SYMLINK_NOFOLLOW); e != nil {
		return e
	}
	pinned, e := databaseMigrationStat(f)
	if e != nil {
		return e
	}
	if !sameExactUnixFileMetadata(path, pinned) {
		return fmt.Errorf("database migration file path changed")
	}
	return nil
}
func databaseMigrationInspect(parent *os.File, name string, uid, gid uint32) (databaseMigrationFile, error) {
	f, e := databaseMigrationOpenAt(parent, name, false)
	if e != nil {
		return databaseMigrationFile{}, e
	}
	defer f.Close()
	return databaseMigrationInspectPinned(parent, name, f, uid, gid)
}
func databaseMigrationInspectPinned(parent *os.File, name string, f *os.File, uid, gid uint32) (databaseMigrationFile, error) {
	st, e := databaseMigrationStat(f)
	if e != nil {
		return databaseMigrationFile{}, e
	}
	if st.Mode != unix.S_IFREG|0600 || st.Uid != uid || st.Gid != gid || st.Nlink != 1 || st.Size < 0 {
		return databaseMigrationFile{}, fmt.Errorf("unsafe database migration file metadata: %s", name)
	}
	if e = databaseMigrationNoAttributes(f); e != nil {
		return databaseMigrationFile{}, e
	}
	sum, e := digestPinnedServiceOperationFile(f, st.Size)
	if e != nil {
		return databaseMigrationFile{}, e
	}
	now, e := databaseMigrationStat(f)
	if e != nil {
		return databaseMigrationFile{}, e
	}
	if !sameExactUnixFileMetadata(st, now) {
		return databaseMigrationFile{}, fmt.Errorf("database migration file changed during hashing")
	}
	if e = databaseMigrationSameEntry(parent, name, f); e != nil {
		return databaseMigrationFile{}, e
	}
	return databaseMigrationFile{databaseMigrationID(st), hex.EncodeToString(sum[:])}, nil
}

// InspectLive binds the published object without requiring its normal runtime
// writes to stop. Content is separately checked through the WAL-aware reader.
func databaseMigrationInspectLive(parent *os.File, name string, uid, gid uint32) (databaseMigrationFile, error) {
	f, e := databaseMigrationOpenAt(parent, name, false)
	if e != nil {
		return databaseMigrationFile{}, e
	}
	defer f.Close()
	st, e := databaseMigrationStat(f)
	if e != nil {
		return databaseMigrationFile{}, e
	}
	if st.Mode != unix.S_IFREG|0600 || st.Uid != uid || st.Gid != gid || st.Nlink != 1 {
		return databaseMigrationFile{}, fmt.Errorf("running database metadata is unsafe")
	}
	if e = databaseMigrationNoAttributes(f); e != nil {
		return databaseMigrationFile{}, e
	}
	var path unix.Stat_t
	if e = unix.Fstatat(int(parent.Fd()), name, &path, unix.AT_SYMLINK_NOFOLLOW); e != nil {
		return databaseMigrationFile{}, e
	}
	a, b := databaseMigrationFile{Identity: databaseMigrationID(st)}, databaseMigrationFile{Identity: databaseMigrationID(path)}
	if !databaseMigrationStableFile(a, b) {
		return databaseMigrationFile{}, fmt.Errorf("running database path changed")
	}
	return a, nil
}
func databaseMigrationSameFile(a, b databaseMigrationFile, renamed bool) bool {
	if renamed {
		a.Identity.Ctime = unix.Timespec{}
		b.Identity.Ctime = unix.Timespec{}
	}
	return a == b
}
func databaseMigrationStableFile(a, b databaseMigrationFile) bool {
	x, y := a.Identity, b.Identity
	return x.Dev == y.Dev && x.Ino == y.Ino && x.Mode == y.Mode && x.UID == y.UID && x.GID == y.GID && x.Links == y.Links
}
func databaseMigrationAbsent(parent *os.File, name string) error {
	var st unix.Stat_t
	e := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(e, unix.ENOENT) {
		return nil
	}
	if e != nil {
		return e
	}
	return fmt.Errorf("unexpected database migration entry: %s", name)
}
func databaseMigrationNoSidecars(parent *os.File, name string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if e := databaseMigrationAbsent(parent, name+suffix); e != nil {
			return e
		}
	}
	return nil
}
func databaseMigrationNames(parent *os.File) ([]string, error) {
	if _, e := parent.Seek(0, 0); e != nil {
		return nil, e
	}
	entries, e := parent.ReadDir(-1)
	if e != nil {
		return nil, e
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
func databaseMigrationRandom(prefix string) (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
func databaseMigrationWrite(parent *os.File, name string, b []byte) error {
	fd, e := unix.Openat(int(parent.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if e = f.Chown(0, 0); e != nil {
		return e
	}
	n, e := f.Write(b)
	if e != nil {
		return e
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return f.Sync()
}
func databaseMigrationRead(parent *os.File, name string) ([]byte, error) {
	f, e := databaseMigrationOpenAt(parent, name, false)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := databaseMigrationStat(f)
	if e != nil {
		return nil, e
	}
	if st.Mode != unix.S_IFREG|0600 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Size > 1024*1024 {
		return nil, fmt.Errorf("unsafe database migration record: %s", name)
	}
	if e = databaseMigrationNoAttributes(f); e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if e != nil {
		return nil, e
	}
	after, e := databaseMigrationStat(f)
	if e != nil {
		return nil, e
	}
	if !sameExactUnixFileMetadata(st, after) {
		return nil, fmt.Errorf("database migration record changed")
	}
	if e = databaseMigrationSameEntry(parent, name, f); e != nil {
		return nil, e
	}
	return b, nil
}
func databaseMigrationRecord(parent *os.File, name string, v any) error {
	return databaseMigrationRecordAt(parent, name, v, nil)
}
func databaseMigrationRecordAt(parent *os.File, name string, v any, staged func()) error {
	want := databaseMigrationCanonical(v)
	got, e := databaseMigrationRead(parent, name)
	if e == nil {
		if !bytes.Equal(got, want) {
			return fmt.Errorf("database migration record conflicts: %s", name)
		}
		return parent.Sync()
	}
	if !errors.Is(e, unix.ENOENT) {
		return e
	}
	tmp, e := databaseMigrationRandom(".record-")
	if e != nil {
		return e
	}
	if e = databaseMigrationWrite(parent, tmp, want); e != nil {
		return e
	}
	if staged != nil {
		staged()
	}
	if e = unix.Renameat2(int(parent.Fd()), tmp, int(parent.Fd()), name, unix.RENAME_NOREPLACE); e != nil {
		return e
	}
	return parent.Sync()
}
func databaseMigrationMkdir(parent *os.File, name string, uid, gid uint32, mode uint32) (*os.File, error) {
	if e := unix.Mkdirat(int(parent.Fd()), name, 0700); e != nil {
		return nil, e
	}
	f, e := databaseMigrationOpenAt(parent, name, true)
	if e != nil {
		return nil, e
	}
	if e = f.Chown(int(uid), int(gid)); e == nil {
		e = f.Chmod(os.FileMode(mode))
	}
	if e == nil {
		e = f.Sync()
	}
	if e == nil {
		e = parent.Sync()
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func databaseMigrationCopy(parent *os.File, name string, source *os.File, size int64, uid, gid uint32) error {
	fd, e := unix.Openat(int(parent.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name))
	defer f.Close()
	if e = f.Chown(int(uid), int(gid)); e != nil {
		return e
	}
	if e = f.Chmod(0600); e != nil {
		return e
	}
	n, e := io.Copy(f, io.NewSectionReader(source, 0, size))
	if e != nil {
		return e
	}
	if n != size {
		return io.ErrShortWrite
	}
	if e = f.Sync(); e != nil {
		return e
	}
	return parent.Sync()
}
