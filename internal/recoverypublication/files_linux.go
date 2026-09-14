//go:build linux

package recoverypublication

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type identity struct {
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

func id(st unix.Stat_t, root bool) identity {
	v := identity{uint64(st.Dev), st.Ino, st.Mode, st.Uid, st.Gid, uint64(st.Nlink), st.Size, st.Mtim, st.Ctim}
	// A directory exchange updates the moved root's ctime. Its inode, payload,
	// mode and child identities remain the durable pre/post discriminator.
	if root {
		v.Mtime = unix.Timespec{}
		v.Ctime = unix.Timespec{}
	}
	return v
}
func safe(st unix.Stat_t, directory bool) bool {
	kind := uint32(unix.S_IFREG)
	if directory {
		kind = unix.S_IFDIR
	}
	return st.Mode&unix.S_IFMT == kind && st.Uid == 0 && st.Gid == 0 && st.Mode&07022 == 0 && (directory || st.Nlink == 1)
}
func fstat(f *os.File) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstat(int(f.Fd()), &st)
	return st, err
}
func openAt(parent *os.File, name string, directory bool) (*os.File, error) {
	if filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "\n\r\\") {
		return nil, ErrUnavailable
	}
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOATIME
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	st, err := fstat(f)
	if err != nil || !safe(st, directory) {
		f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}
func openPath(anchor, path string) (*os.File, error) {
	if !filepath.IsAbs(anchor) || filepath.Clean(anchor) != anchor || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnavailable
	}
	rel, err := filepath.Rel(anchor, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(anchor, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOATIME, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "publication-root")
	st, err := fstat(f)
	if err != nil || !safe(st, true) {
		f.Close()
		return nil, ErrUnavailable
	}
	if rel == "." {
		return f, nil
	}
	for _, part := range strings.Split(rel, "/") {
		next, err := openAt(f, part, true)
		f.Close()
		if err != nil {
			return nil, ErrUnavailable
		}
		f = next
	}
	return f, nil
}
func samePath(parent *os.File, name string, file *os.File) bool {
	var path unix.Stat_t
	held, err := fstat(file)
	return err == nil && unix.Fstatat(int(parent.Fd()), name, &path, unix.AT_SYMLINK_NOFOLLOW) == nil && reflect.DeepEqual(id(path, false), id(held, false))
}
func readFile(parent *os.File, name string, limit int64) ([]byte, error) {
	return readModeFile(parent, name, limit, 0)
}
func readPrivateFile(parent *os.File, name string, limit int64) ([]byte, error) {
	return readModeFile(parent, name, limit, 0600)
}
func readModeFile(parent *os.File, name string, limit int64, mode uint32) ([]byte, error) {
	f, err := openAt(parent, name, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	before, err := fstat(f)
	if err != nil || before.Size < 0 || before.Size > limit || mode != 0 && before.Mode&07777 != mode {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	after, e := fstat(f)
	if err != nil || e != nil || int64(len(raw)) != before.Size || !reflect.DeepEqual(id(before, false), id(after, false)) || !samePath(parent, name, f) {
		return nil, ErrOwnerChanged
	}
	return raw, nil
}
func readRelative(root *os.File, path string, limit int64) ([]byte, error) {
	parent, name, close, err := relativeParent(root, path)
	if err != nil {
		return nil, err
	}
	defer close()
	return readFile(parent, name, limit)
}
func relativeParent(root *os.File, path string) (*os.File, string, func(), error) {
	if !validRelative(path) {
		return nil, "", func() {}, ErrUnavailable
	}
	parts := strings.Split(path, "/")
	f := root
	owned := false
	for _, part := range parts[:len(parts)-1] {
		next, err := openAt(f, part, true)
		if owned {
			f.Close()
		}
		if err != nil {
			return nil, "", func() {}, err
		}
		f = next
		owned = true
	}
	return f, parts[len(parts)-1], func() {
		if owned {
			f.Close()
		}
	}, nil
}
func validRelative(path string) bool {
	return path != "" && path != "." && !filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.HasPrefix(path, "../") && !strings.ContainsAny(path, "\x00\n\r\\")
}
func parseManifest(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || int64(len(raw)) > maxManifest || raw[len(raw)-1] != '\n' {
		return nil, ErrUnavailable
	}
	lines := strings.Split(string(raw[:len(raw)-1]), "\n")
	if len(lines) > 65536 {
		return nil, ErrUnavailable
	}
	result := map[string]string{}
	previous := ""
	for _, line := range lines {
		if len(line) < 69 || line[64:68] != "  ./" || !hex64.MatchString(line[:64]) {
			return nil, ErrUnavailable
		}
		name := line[68:]
		if !validRelative(name) || name <= previous {
			return nil, ErrUnavailable
		}
		previous = name
		result[name] = line[:64]
	}
	return result, nil
}
func manifest(root *os.File, expected string) (map[string]string, error) {
	raw, err := readFile(root, "SHA256SUMS", maxManifest)
	if err != nil || digest(raw) != expected {
		return nil, ErrUnavailable
	}
	return parseManifest(raw)
}
func manifestValue(root *os.File, rows map[string]string, name string) (string, error) {
	raw, err := readRelative(root, name, 4096)
	if err != nil || rows[name] == "" || digest(raw) != rows[name] {
		return "", ErrUnavailable
	}
	return string(raw), nil
}

// Only explicitly modeled non-privilege attributes are preserved. ACLs,
// capabilities and unknown namespaces require a separate supported contract.
type attribute struct {
	Name  string `json:"name"`
	Value []byte `json:"value"`
	SHA   string `json:"sha256"`
}

func readAttributes(f *os.File) ([]attribute, error) {
	n, err := unix.Flistxattr(int(f.Fd()), nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil, nil
	}
	if err != nil || n < 0 || n > 8192 {
		return nil, ErrUnavailable
	}
	if n == 0 {
		return nil, nil
	}
	raw := make([]byte, n)
	count, err := unix.Flistxattr(int(f.Fd()), raw)
	if err != nil || count != n || raw[n-1] != 0 {
		return nil, ErrOwnerChanged
	}
	names := strings.Split(string(raw[:n-1]), "\x00")
	if len(names) > 32 {
		return nil, ErrUnavailable
	}
	sort.Strings(names)
	var result []attribute
	total := 0
	previous := ""
	// SELinux labels depend on the destination policy, not the retained data
	// path. Until that transition is journaled, reject them before quiescence
	// instead of copying source labels and invalidating our proof via restorecon.
	for _, name := range names {
		if name == previous || len(name) > 255 || name == "" || strings.ContainsAny(name, "\n\r") || !(strings.HasPrefix(name, "user.") && len(name) > 5) {
			return nil, ErrUnsupportedMetadata
		}
		previous = name
		size, err := unix.Fgetxattr(int(f.Fd()), name, nil)
		if err != nil || size < 0 || size > 65536 {
			return nil, ErrUnavailable
		}
		total += size
		if total > 131072 {
			return nil, ErrUnavailable
		}
		value := make([]byte, size)
		count, err := unix.Fgetxattr(int(f.Fd()), name, value)
		if err != nil || count != size {
			return nil, ErrOwnerChanged
		}
		result = append(result, attribute{name, value, digest(value)})
	}
	return result, nil
}
func copyAttributes(f *os.File, wanted []attribute) error {
	current, err := readAttributes(f)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, a := range wanted {
		names[a.Name] = true
	}
	for _, a := range current {
		if !names[a.Name] && unix.Fremovexattr(int(f.Fd()), a.Name) != nil {
			return ErrUnavailable
		}
	}
	for _, a := range wanted {
		if digest(a.Value) != a.SHA || unix.Fsetxattr(int(f.Fd()), a.Name, a.Value, 0) != nil {
			return ErrUnavailable
		}
	}
	actual, err := readAttributes(f)
	if err != nil || !reflect.DeepEqual(actual, wanted) {
		return ErrOwnerChanged
	}
	return nil
}

type entry struct {
	Path       string      `json:"path"`
	Identity   identity    `json:"identity"`
	SHA        string      `json:"sha256,omitempty"`
	Attributes []attribute `json:"attributes,omitempty"`
}
type tree struct {
	Entries []entry `json:"entries"`
}

func (t tree) equal(other tree) bool { return reflect.DeepEqual(t, other) }
func semanticEntry(e entry) string {
	return fmt.Sprintf("%s\x00%d:%d:%d:%d:%s", e.Path, e.Identity.Mode, e.Identity.UID, e.Identity.GID, func() int64 {
		if e.Identity.Mode&unix.S_IFMT == unix.S_IFDIR {
			return 0
		}
		return e.Identity.Size
	}(), e.SHA) + string(canonical(e.Attributes))
}
func (t tree) semantic() string {
	var b strings.Builder
	for _, e := range t.Entries {
		b.WriteString(semanticEntry(e))
		b.WriteByte('\n')
	}
	return digest([]byte(b.String()))
}
func (t tree) entryMap() map[string]entry {
	m := map[string]entry{}
	for _, e := range t.Entries {
		m[e.Path] = e
	}
	return m
}
func scan(root *os.File) (tree, error) {
	result := tree{}
	total := int64(0)
	var visit func(*os.File, string) error
	visit = func(dir *os.File, path string) error {
		before, err := fstat(dir)
		if err != nil || !safe(before, true) {
			return ErrOwnerChanged
		}
		attrs, err := readAttributes(dir)
		if err != nil {
			return err
		}
		result.Entries = append(result.Entries, entry{path, id(before, path == "."), "", attrs})
		if len(result.Entries) > maxEntries {
			return ErrUnavailable
		}
		if _, err := dir.Seek(0, 0); err != nil {
			return ErrUnavailable
		}
		names, err := dir.Readdirnames(maxEntries + 1)
		if err != nil && err != io.EOF {
			return ErrUnavailable
		}
		if len(names) > maxEntries {
			return ErrUnavailable
		}
		sort.Strings(names)
		for _, name := range names {
			rel := name
			if path != "." {
				rel = path + "/" + name
			}
			if !validRelative(rel) {
				return ErrUnavailable
			}
			var st unix.Stat_t
			if unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
				return ErrOwnerChanged
			}
			if st.Dev != before.Dev {
				return ErrUnavailable
			}
			if st.Mode&unix.S_IFMT == unix.S_IFDIR {
				sub, err := openAt(dir, name, true)
				if err != nil {
					return ErrOwnerChanged
				}
				err = visit(sub, rel)
				bound := samePath(dir, name, sub)
				sub.Close()
				if err != nil {
					return err
				}
				if !bound {
					return ErrOwnerChanged
				}
			} else {
				f, err := openAt(dir, name, false)
				if err != nil {
					return ErrOwnerChanged
				}
				initial, err := fstat(f)
				if err != nil || initial.Size < 0 || initial.Size > maxFile {
					f.Close()
					return ErrUnavailable
				}
				attrs, err := readAttributes(f)
				if err != nil {
					f.Close()
					return err
				}
				total += initial.Size
				if total > maxTree {
					f.Close()
					return ErrUnavailable
				}
				h := sha256.New()
				n, err := io.Copy(h, io.LimitReader(f, maxFile+1))
				after, e := fstat(f)
				bound := samePath(dir, name, f)
				f.Close()
				if err != nil || e != nil || n != initial.Size || !reflect.DeepEqual(id(initial, false), id(after, false)) || !bound {
					return ErrOwnerChanged
				}
				result.Entries = append(result.Entries, entry{rel, id(initial, false), hex.EncodeToString(h.Sum(nil)), attrs})
				if len(result.Entries) > maxEntries {
					return ErrUnavailable
				}
			}
		}
		after, err := fstat(dir)
		if err != nil || !reflect.DeepEqual(id(before, false), id(after, false)) {
			return ErrOwnerChanged
		}
		return nil
	}
	if err := visit(root, "."); err != nil {
		return tree{}, err
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, nil
}
func checkManifestTree(t tree, rows map[string]string, prefix string) error {
	seen := map[string]bool{}
	for _, e := range t.Entries {
		if e.SHA != "" {
			name := prefix + "/" + e.Path
			if rows[name] != e.SHA {
				return ErrOwnerChanged
			}
			seen[name] = true
		}
	}
	for name := range rows {
		if strings.HasPrefix(name, prefix+"/") && !seen[name] {
			return ErrOwnerChanged
		}
	}
	return nil
}
func privateDir(parent *os.File, name string) (*os.File, error) {
	err := unix.Mkdirat(int(parent.Fd()), name, 0700)
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, ErrUnavailable
	}
	if err == nil && unix.Fsync(int(parent.Fd())) != nil {
		return nil, ErrUnavailable
	}
	f, err := openAt(parent, name, true)
	if err != nil {
		return nil, ErrUnavailable
	}
	st, err := fstat(f)
	if err != nil || st.Mode&07777 != 0700 {
		f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}
func writeNew(parent *os.File, name string, raw []byte) error {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if _, err = f.Write(raw); err != nil {
		return ErrUnavailable
	}
	if unix.Fsync(fd) != nil || unix.Fsync(int(parent.Fd())) != nil {
		return ErrUnavailable
	}
	return nil
}
func canonical(v any) []byte { raw, _ := json.Marshal(v); return append(raw, '\n') }
func decodeExact(raw []byte, v any) error {
	if json.Unmarshal(raw, v) != nil || !bytes.Equal(raw, canonical(v)) {
		return ErrUnavailable
	}
	return nil
}
