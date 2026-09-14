//go:build linux

package recoveryruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

type resolveConfig struct {
	runtimeRoot, selectionPath, anchor string
	uid, gid                           uint32
	beforeFinalProof                   func() // Private deterministic test seam; production nil.
}
type pinnedDirectory struct {
	file       *os.File
	path, base string
	parent     *pinnedDirectory
	stat       unix.Stat_t
	exactMode  uint32
	inventory  []string
}
type pinnedFile struct {
	file    *os.File
	parent  *pinnedDirectory
	base    string
	stat    unix.Stat_t
	maxSize int64
	digest  string
}
type runtimeState struct {
	mu           sync.Mutex
	directories  []*pinnedDirectory
	files        []*pinnedFile
	config       resolveConfig
	root, digest string
	closed       bool
}

func Resolve() (*Runtime, error) {
	return resolveAt(resolveConfig{runtimeRoot: RuntimeRoot, selectionPath: SelectionPath, anchor: "/", uid: 0, gid: 0})
}

func resolveAt(config resolveConfig) (result *Runtime, returnErr error) {
	state := &runtimeState{config: config}
	defer func() {
		if returnErr != nil {
			state.close()
		}
	}()
	if !filepath.IsAbs(config.anchor) || filepath.Clean(config.anchor) != config.anchor ||
		!filepath.IsAbs(config.runtimeRoot) || filepath.Clean(config.runtimeRoot) != config.runtimeRoot ||
		!filepath.IsAbs(config.selectionPath) || filepath.Clean(config.selectionPath) != config.selectionPath {
		return nil, fail(ReasonUnsafeMetadata)
	}
	selectionParent, err := state.openPath(filepath.Dir(config.selectionPath))
	if err != nil {
		return nil, err
	}
	selection, err := state.openFile(selectionParent, filepath.Base(config.selectionPath), 0600, MaxSelectionSize)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, fail(ReasonNotSelected)
		}
		return nil, asReadError(err)
	}
	raw, err := selection.readBounded()
	if err != nil {
		return nil, err
	}
	selected, err := ParseSelection(raw)
	if err != nil {
		return nil, err
	}
	selection.digest = Digest(raw)
	runtimeParent, err := state.openPath(config.runtimeRoot)
	if err != nil {
		return nil, err
	}
	kit, err := state.openChildDirectory(runtimeParent, selected, 0700)
	if err != nil {
		return nil, asReadError(err)
	}
	if err := state.loadKit(kit, selected, false); err != nil {
		return nil, err
	}
	if config.beforeFinalProof != nil {
		config.beforeFinalProof()
	}
	if err := state.revalidate(); err != nil {
		return nil, err
	}
	return &Runtime{Root: state.root, Digest: selected, state: state}, nil
}

// VerifyBundle validates a root-owned package/enrolled kit without consulting
// the active selection. It does not install it or authorize choosing it.
func VerifyBundle(root string, packaged bool) (*Runtime, error) {
	return verifyBundleAt(root, packaged, resolveConfig{anchor: "/", uid: 0, gid: 0})
}

func verifyBundleAt(root string, packaged bool, config resolveConfig) (result *Runtime, returnErr error) {
	state := &runtimeState{config: config}
	defer func() {
		if returnErr != nil {
			state.close()
		}
	}()
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(config.anchor) || filepath.Clean(config.anchor) != config.anchor {
		return nil, fail(ReasonUnsafeMetadata)
	}
	kit, err := state.openPath(root)
	if err != nil {
		return nil, err
	}
	if err := state.loadKit(kit, "", packaged); err != nil {
		return nil, err
	}
	if config.beforeFinalProof != nil {
		config.beforeFinalProof()
	}
	if err := state.revalidate(); err != nil {
		return nil, err
	}
	return &Runtime{Root: state.root, Digest: state.digest, state: state}, nil
}

func (state *runtimeState) loadKit(kit *pinnedDirectory, selected string, packaged bool) error {
	directoryMode, manifestMode := uint32(0700), uint32(0600)
	if packaged {
		directoryMode, manifestMode = 0755, 0644
	}
	kit.exactMode = directoryMode
	if !state.validDirectory(kit.stat, directoryMode) {
		return fail(ReasonUnsafeMetadata)
	}
	manifestFile, err := state.openFile(kit, ManifestName, manifestMode, MaxManifestSize)
	if err != nil {
		return asReadError(err)
	}
	manifestRaw, err := manifestFile.readBounded()
	if err != nil {
		return err
	}
	if selected != "" && Digest(manifestRaw) != selected {
		return fail(ReasonDigestMismatch)
	}
	manifest, err := ParseManifest(manifestRaw)
	if err != nil {
		return err
	}
	selected = Digest(manifestRaw)
	manifestFile.digest = selected
	directories := map[string]*pinnedDirectory{"": kit}
	for _, path := range []string{"bin", "deploy", "deploy/recovery"} {
		parent := directories[filepath.Dir(path)]
		if filepath.Dir(path) == "." {
			parent = kit
		}
		directory, err := state.openChildDirectory(parent, filepath.Base(path), directoryMode)
		if err != nil {
			return asReadError(err)
		}
		directories[path] = directory
	}
	kit.inventory = []string{"bin", "deploy", ManifestName, "rollback.sh", "update.sh"}
	directories["deploy"].inventory = []string{"recovery"}
	var total int64
	for _, spec := range ExpectedFiles() {
		dir := filepath.Dir(spec.Path)
		if dir == "." {
			dir = ""
		}
		parent := directories[dir]
		if parent == nil {
			return fail(ReasonInvalidManifest)
		}
		if dir != "" {
			parent.inventory = append(parent.inventory, filepath.Base(spec.Path))
		}
		limit := int64(MaxScriptSize)
		if strings.HasPrefix(spec.Path, "bin/") {
			limit = MaxBinarySize
		}
		file, err := state.openFile(parent, filepath.Base(spec.Path), uint32(spec.Mode), limit)
		if err != nil {
			return asReadError(err)
		}
		total += file.stat.Size
		if total > MaxRuntimeSize {
			return fail(ReasonUnsafeMetadata)
		}
		file.digest = manifest.Files[spec.Path]
		if err := file.verifyContents(); err != nil {
			return err
		}
	}
	state.root = kit.path
	state.digest = selected
	return nil
}

func asReadError(err error) error {
	var bounded *Error
	if errors.As(err, &bounded) {
		return bounded
	}
	if errors.Is(err, unix.ENOENT) {
		return fail(ReasonMissing)
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fail(ReasonUnsafeMetadata)
	}
	return fail(ReasonReadFailed)
}

func (state *runtimeState) openPath(path string) (*pinnedDirectory, error) {
	relative, err := filepath.Rel(state.config.anchor, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return nil, fail(ReasonUnsafeMetadata)
	}
	fd, err := unix.Open(state.config.anchor, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NOATIME, 0)
	if err != nil {
		return nil, asReadError(err)
	}
	current := &pinnedDirectory{file: os.NewFile(uintptr(fd), "recovery-runtime-directory"), path: state.config.anchor}
	if current.file == nil {
		unix.Close(fd)
		return nil, fail(ReasonReadFailed)
	}
	state.directories = append(state.directories, current)
	if unix.Fstat(fd, &current.stat) != nil || !state.validDirectory(current.stat, 0) {
		return nil, fail(ReasonUnsafeMetadata)
	}
	if relative == "." {
		return current, nil
	}
	for _, base := range strings.Split(relative, "/") {
		if base == "" || base == "." || base == ".." {
			return nil, fail(ReasonUnsafeMetadata)
		}
		current, err = state.openChildDirectory(current, base, 0)
		if err != nil {
			return nil, asReadError(err)
		}
	}
	return current, nil
}
func (state *runtimeState) openChildDirectory(parent *pinnedDirectory, base string, exactMode uint32) (*pinnedDirectory, error) {
	fd, err := unix.Openat(int(parent.file.Fd()), base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NOATIME, 0)
	if err != nil {
		return nil, err
	}
	directory := &pinnedDirectory{file: os.NewFile(uintptr(fd), "recovery-runtime-directory"), path: filepath.Join(parent.path, base), base: base, parent: parent, exactMode: exactMode}
	if directory.file == nil {
		unix.Close(fd)
		return nil, fail(ReasonReadFailed)
	}
	state.directories = append(state.directories, directory)
	if unix.Fstat(fd, &directory.stat) != nil || !state.validDirectory(directory.stat, exactMode) {
		return nil, fail(ReasonUnsafeMetadata)
	}
	return directory, nil
}
func (state *runtimeState) validDirectory(stat unix.Stat_t, exactMode uint32) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Uid == state.config.uid && stat.Gid == state.config.gid &&
		stat.Mode&0o7022 == 0 && (exactMode == 0 || stat.Mode&0o7777 == exactMode)
}
func (state *runtimeState) openFile(parent *pinnedDirectory, base string, mode uint32, maxSize int64) (*pinnedFile, error) {
	fd, err := unix.Openat(int(parent.file.Fd()), base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOATIME, 0)
	if err != nil {
		return nil, err
	}
	file := &pinnedFile{file: os.NewFile(uintptr(fd), "recovery-runtime-file"), parent: parent, base: base, maxSize: maxSize}
	if file.file == nil {
		unix.Close(fd)
		return nil, fail(ReasonReadFailed)
	}
	state.files = append(state.files, file)
	if unix.Fstat(fd, &file.stat) != nil || file.stat.Mode&unix.S_IFMT != unix.S_IFREG || file.stat.Mode&0o7777 != mode || file.stat.Uid != state.config.uid ||
		file.stat.Gid != state.config.gid || file.stat.Nlink != 1 || file.stat.Size <= 0 || file.stat.Size > maxSize {
		return nil, fail(ReasonUnsafeMetadata)
	}
	return file, nil
}
func sameFile(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}
func (file *pinnedFile) verifyMetadata() error {
	var held, path unix.Stat_t
	if unix.Fstat(int(file.file.Fd()), &held) != nil || unix.Fstatat(int(file.parent.file.Fd()), file.base, &path, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameFile(file.stat, held) || !sameFile(file.stat, path) {
		return fail(ReasonChanged)
	}
	return nil
}
func (file *pinnedFile) readBounded() ([]byte, error) {
	if err := file.verifyMetadata(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.NewSectionReader(file.file, 0, file.maxSize+1))
	if err != nil {
		return nil, fail(ReasonReadFailed)
	}
	if int64(len(raw)) != file.stat.Size {
		return nil, fail(ReasonChanged)
	}
	if err := file.verifyMetadata(); err != nil {
		return nil, err
	}
	return raw, nil
}
func (file *pinnedFile) verifyContents() error {
	if err := file.verifyMetadata(); err != nil {
		return err
	}
	hash := sha256.New()
	read, err := io.Copy(hash, io.NewSectionReader(file.file, 0, file.maxSize+1))
	if err != nil {
		return fail(ReasonReadFailed)
	}
	if read != file.stat.Size {
		return fail(ReasonChanged)
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.digest {
		return fail(ReasonContentMismatch)
	}
	return file.verifyMetadata()
}
func (state *runtimeState) verifyDirectory(directory *pinnedDirectory) error {
	var held, path unix.Stat_t
	if unix.Fstat(int(directory.file.Fd()), &held) != nil {
		return fail(ReasonChanged)
	}
	if directory.parent == nil {
		if unix.Lstat(directory.path, &path) != nil {
			return fail(ReasonChanged)
		}
	} else if unix.Fstatat(int(directory.parent.file.Fd()), directory.base, &path, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return fail(ReasonChanged)
	}
	if !state.validDirectory(held, directory.exactMode) || !state.validDirectory(path, directory.exactMode) ||
		held.Dev != directory.stat.Dev || held.Ino != directory.stat.Ino || path.Dev != held.Dev || path.Ino != held.Ino ||
		held.Mode != directory.stat.Mode || held.Uid != directory.stat.Uid || held.Gid != directory.stat.Gid {
		return fail(ReasonChanged)
	}
	if directory.inventory == nil {
		return nil
	}
	fd, err := unix.Openat(int(directory.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NOATIME, 0)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	file := os.NewFile(uintptr(fd), "recovery-runtime-inventory")
	if file == nil {
		unix.Close(fd)
		return fail(ReasonReadFailed)
	}
	names, readErr := file.Readdirnames(len(directory.inventory) + 1)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fail(ReasonReadFailed)
	}
	if closeErr != nil {
		return fail(ReasonReadFailed)
	}
	if len(names) != len(directory.inventory) {
		return fail(ReasonInventoryMismatch)
	}
	sort.Strings(names)
	expected := append([]string(nil), directory.inventory...)
	sort.Strings(expected)
	for index, name := range names {
		if name != expected[index] {
			return fail(ReasonInventoryMismatch)
		}
	}
	return nil
}
func (state *runtimeState) revalidate() error {
	if state.closed {
		return fail(ReasonChanged)
	}
	for _, directory := range state.directories {
		if err := state.verifyDirectory(directory); err != nil {
			return err
		}
	}
	for _, file := range state.files {
		if err := file.verifyContents(); err != nil {
			return err
		}
	}
	// Payload reads precede a final path/inventory pass, so renaming the kit or
	// changing the selector during verification does not produce a known kit.
	for _, directory := range state.directories {
		if err := state.verifyDirectory(directory); err != nil {
			return err
		}
	}
	for _, file := range state.files {
		if err := file.verifyMetadata(); err != nil {
			return err
		}
	}
	return nil
}
func (runtime *Runtime) Revalidate() error {
	if runtime == nil || runtime.state == nil {
		return fail(ReasonChanged)
	}
	state := runtime.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if runtime.Root != state.root || runtime.Digest != state.digest {
		return fail(ReasonChanged)
	}
	return state.revalidate()
}
func (state *runtimeState) close() error {
	if state.closed {
		return nil
	}
	state.closed = true
	failed := false
	for _, file := range state.files {
		if file.file.Close() != nil {
			failed = true
		}
	}
	for index := len(state.directories) - 1; index >= 0; index-- {
		if state.directories[index].file.Close() != nil {
			failed = true
		}
	}
	if failed {
		return fail(ReasonReadFailed)
	}
	return nil
}
func (runtime *Runtime) Close() error {
	if runtime == nil || runtime.state == nil {
		return nil
	}
	runtime.state.mu.Lock()
	defer runtime.state.mu.Unlock()
	return runtime.state.close()
}
