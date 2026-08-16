package binddns

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"
)

type memoryNode struct {
	mode   fs.FileMode
	data   []byte
	target string
	uid    int
	gid    int
}

type memoryFS struct {
	nodes map[string]*memoryNode
	syncs []string
}

func newMemoryFS() *memoryFS {
	filesystem := &memoryFS{nodes: map[string]*memoryNode{}}
	for _, name := range []string{"/", "/var", "/var/lib", "/var/lib/celikpanel"} {
		filesystem.nodes[name] = &memoryNode{mode: fs.ModeDir | 0o755}
	}
	return filesystem
}

func cleanMemoryPath(name string) string { return path.Clean(name) }

func (filesystem *memoryFS) Lstat(name string) (Entry, error) {
	node, ok := filesystem.nodes[cleanMemoryPath(name)]
	if !ok {
		return Entry{}, fs.ErrNotExist
	}
	size := int64(len(node.data))
	if node.mode&fs.ModeSymlink != 0 {
		size = int64(len(node.target))
	}
	return Entry{Mode: node.mode, Size: size, UID: node.uid, GID: node.gid, OwnerKnown: true}, nil
}

func (filesystem *memoryFS) Mkdir(name string, mode fs.FileMode) error {
	name = cleanMemoryPath(name)
	if _, ok := filesystem.nodes[name]; ok {
		return fs.ErrExist
	}
	parent, ok := filesystem.nodes[path.Dir(name)]
	if !ok || !parent.mode.IsDir() {
		return fs.ErrNotExist
	}
	filesystem.nodes[name] = &memoryNode{mode: fs.ModeDir | mode.Perm()}
	return nil
}

func (filesystem *memoryFS) WriteFileExclusive(name string, data []byte, mode fs.FileMode) error {
	name = cleanMemoryPath(name)
	if _, ok := filesystem.nodes[name]; ok {
		return fs.ErrExist
	}
	parent, ok := filesystem.nodes[path.Dir(name)]
	if !ok || !parent.mode.IsDir() {
		return fs.ErrNotExist
	}
	filesystem.nodes[name] = &memoryNode{mode: mode.Perm(), data: append([]byte(nil), data...)}
	return nil
}

func (filesystem *memoryFS) ReadFile(name string) ([]byte, error) {
	node, ok := filesystem.nodes[cleanMemoryPath(name)]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if !node.mode.IsRegular() {
		return nil, errors.New("not regular")
	}
	return append([]byte(nil), node.data...), nil
}

func (filesystem *memoryFS) ReadDirNames(name string) ([]string, error) {
	name = cleanMemoryPath(name)
	node, ok := filesystem.nodes[name]
	if !ok || !node.mode.IsDir() {
		return nil, fs.ErrNotExist
	}
	prefix := strings.TrimSuffix(name, "/") + "/"
	seen := map[string]struct{}{}
	for candidate := range filesystem.nodes {
		if !strings.HasPrefix(candidate, prefix) {
			continue
		}
		rest := strings.TrimPrefix(candidate, prefix)
		if rest == "" {
			continue
		}
		seen[strings.SplitN(rest, "/", 2)[0]] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for item := range seen {
		names = append(names, item)
	}
	sort.Strings(names)
	return names, nil
}

func (filesystem *memoryFS) Chmod(name string, mode fs.FileMode) error {
	node, ok := filesystem.nodes[cleanMemoryPath(name)]
	if !ok {
		return fs.ErrNotExist
	}
	node.mode = node.mode.Type() | mode.Perm()
	return nil
}

func (filesystem *memoryFS) Chown(name string, uid, gid int) error {
	node, ok := filesystem.nodes[cleanMemoryPath(name)]
	if !ok {
		return fs.ErrNotExist
	}
	node.uid, node.gid = uid, gid
	return nil
}

func (filesystem *memoryFS) Lchown(name string, uid, gid int) error {
	return filesystem.Chown(name, uid, gid)
}

func (filesystem *memoryFS) RenameNoReplace(oldName, newName string) error {
	if _, ok := filesystem.nodes[cleanMemoryPath(newName)]; ok {
		return fs.ErrExist
	}
	return filesystem.rename(oldName, newName)
}

func (filesystem *memoryFS) RenameReplace(oldName, newName string) error {
	newName = cleanMemoryPath(newName)
	if target, ok := filesystem.nodes[newName]; ok {
		if target.mode.IsDir() {
			return errors.New("cannot replace directory")
		}
		delete(filesystem.nodes, newName)
	}
	return filesystem.rename(oldName, newName)
}

func (filesystem *memoryFS) rename(oldName, newName string) error {
	oldName, newName = cleanMemoryPath(oldName), cleanMemoryPath(newName)
	if _, ok := filesystem.nodes[oldName]; !ok {
		return fs.ErrNotExist
	}
	keys := make([]string, 0)
	for candidate := range filesystem.nodes {
		if candidate == oldName || strings.HasPrefix(candidate, oldName+"/") {
			keys = append(keys, candidate)
		}
	}
	sort.Strings(keys)
	for _, oldKey := range keys {
		newKey := newName + strings.TrimPrefix(oldKey, oldName)
		filesystem.nodes[newKey] = filesystem.nodes[oldKey]
		delete(filesystem.nodes, oldKey)
	}
	return nil
}

func (filesystem *memoryFS) Symlink(target, linkName string) error {
	linkName = cleanMemoryPath(linkName)
	if _, ok := filesystem.nodes[linkName]; ok {
		return fs.ErrExist
	}
	filesystem.nodes[linkName] = &memoryNode{mode: fs.ModeSymlink | 0o777, target: target}
	return nil
}

func (filesystem *memoryFS) Readlink(name string) (string, error) {
	node, ok := filesystem.nodes[cleanMemoryPath(name)]
	if !ok || node.mode&fs.ModeSymlink == 0 {
		return "", fs.ErrInvalid
	}
	return node.target, nil
}

func (filesystem *memoryFS) Remove(name string) error {
	name = cleanMemoryPath(name)
	if _, ok := filesystem.nodes[name]; !ok {
		return fs.ErrNotExist
	}
	delete(filesystem.nodes, name)
	return nil
}

func (filesystem *memoryFS) RemoveAll(name string) error {
	name = cleanMemoryPath(name)
	for candidate := range filesystem.nodes {
		if candidate == name || strings.HasPrefix(candidate, name+"/") {
			delete(filesystem.nodes, candidate)
		}
	}
	return nil
}

func (filesystem *memoryFS) Sync(name string) error {
	if _, ok := filesystem.nodes[cleanMemoryPath(name)]; !ok {
		return fs.ErrNotExist
	}
	filesystem.syncs = append(filesystem.syncs, cleanMemoryPath(name))
	return nil
}

type runnerCall struct {
	name string
	args []string
}

type recordingRunner struct {
	calls    []runnerCall
	failName string
}

func (runner *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	if name == runner.failName {
		return []byte("rejected"), errors.New("validation rejected")
	}
	return []byte("ok"), nil
}

func newTestPublisher(t *testing.T, filesystem *memoryFS, runner *recordingRunner) *Publisher {
	t.Helper()
	publisher, err := NewPublisher("/var/lib/celikpanel/bind", filesystem, runner)
	if err != nil {
		t.Fatal(err)
	}
	sequence := 0
	publisher.nonce = func() (string, error) {
		sequence++
		if sequence%2 == 0 {
			return "2222222222222222", nil
		}
		return "1111111111111111", nil
	}
	return publisher
}

func publisherGeneration(t *testing.T, generation int64, address string) Generation {
	t.Helper()
	snapshot := boundSnapshot("example.com", generation, testZoneRecords("example.com", address))
	snapshot.MutationRequestID = strings.Repeat(string(rune('0'+generation)), 32)
	result, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 1, Zones: []ZoneSnapshot{snapshot},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPublisherStagesRootOwnedImmutableTreeAndActivatesAtomically(t *testing.T) {
	filesystem := newMemoryFS()
	runner := &recordingRunner{}
	publisher := newTestPublisher(t, filesystem, runner)
	generation := publisherGeneration(t, 1, "192.0.2.1")
	if err := publisher.Stage(context.Background(), generation); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 || runner.calls[0].name != "named-checkzone" || runner.calls[1].name != "named-checkconf" {
		t.Fatalf("validator calls = %#v", runner.calls)
	}
	base := "/var/lib/celikpanel/bind/generations/" + generation.ID
	for _, check := range []struct {
		name string
		mode fs.FileMode
	}{
		{base, fs.ModeDir | 0o555},
		{base + "/zones", fs.ModeDir | 0o555},
		{base + "/zones.conf", 0o444},
		{base + "/receipt.json", 0o444},
		{base + "/zones/example.com.zone", 0o444},
	} {
		node := filesystem.nodes[check.name]
		if node == nil || node.mode != check.mode || node.uid != 0 || node.gid != 0 {
			t.Errorf("%s metadata = %#v, want mode %v root:root", check.name, node, check.mode)
		}
	}
	if err := publisher.Activate(generation.ID); err != nil {
		t.Fatal(err)
	}
	current := filesystem.nodes["/var/lib/celikpanel/bind/current"]
	if current == nil || current.mode&fs.ModeSymlink == 0 || current.target != "generations/"+generation.ID {
		t.Fatalf("current pointer = %#v", current)
	}
	id, exists, err := publisher.Current()
	if err != nil || !exists || id != generation.ID {
		t.Fatalf("Current = %q, %v, %v", id, exists, err)
	}
	before := len(runner.calls)
	if err := publisher.Stage(context.Background(), generation); err != nil {
		t.Fatalf("idempotent stage: %v", err)
	}
	if len(runner.calls) != before {
		t.Fatal("idempotent stage reran validators")
	}
}

func TestPublisherValidationFailureLeavesNoGeneration(t *testing.T) {
	filesystem := newMemoryFS()
	runner := &recordingRunner{failName: "named-checkzone"}
	publisher := newTestPublisher(t, filesystem, runner)
	generation := publisherGeneration(t, 1, "192.0.2.1")
	if err := publisher.Stage(context.Background(), generation); err == nil {
		t.Fatal("validation failure was accepted")
	}
	final := "/var/lib/celikpanel/bind/generations/" + generation.ID
	if _, ok := filesystem.nodes[final]; ok {
		t.Fatal("failed generation was published")
	}
	for name := range filesystem.nodes {
		if strings.Contains(name, "/.stage-") {
			t.Fatalf("staging tree was not cleaned: %s", name)
		}
	}
}

func TestPublisherFailsClosedOnTamperedImmutableGeneration(t *testing.T) {
	filesystem := newMemoryFS()
	runner := &recordingRunner{}
	publisher := newTestPublisher(t, filesystem, runner)
	generation := publisherGeneration(t, 1, "192.0.2.1")
	if err := publisher.Stage(context.Background(), generation); err != nil {
		t.Fatal(err)
	}
	zonePath := "/var/lib/celikpanel/bind/generations/" + generation.ID + "/zones/example.com.zone"
	filesystem.nodes[zonePath].data = []byte("tampered")
	if err := publisher.Stage(context.Background(), generation); err == nil {
		t.Fatal("tampered existing generation was accepted")
	}
	if err := publisher.Activate(generation.ID); err == nil {
		t.Fatal("tampered generation was activated")
	}
}

func TestPublisherSwitchRestoresPriorPointerAndReappliesIt(t *testing.T) {
	filesystem := newMemoryFS()
	runner := &recordingRunner{}
	publisher := newTestPublisher(t, filesystem, runner)
	first := publisherGeneration(t, 1, "192.0.2.1")
	second := publisherGeneration(t, 2, "192.0.2.2")
	for _, generation := range []Generation{first, second} {
		if err := publisher.Stage(context.Background(), generation); err != nil {
			t.Fatal(err)
		}
	}
	if err := publisher.Activate(first.ID); err != nil {
		t.Fatal(err)
	}
	applyCalls := 0
	err := publisher.Switch(context.Background(), second.ID, func(context.Context) error {
		applyCalls++
		if applyCalls == 1 {
			return errors.New("reload failed")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "previous BIND generation restored") {
		t.Fatalf("Switch error = %v", err)
	}
	if applyCalls != 2 {
		t.Fatalf("apply calls = %d, want 2", applyCalls)
	}
	id, exists, currentErr := publisher.Current()
	if currentErr != nil || !exists || id != first.ID {
		t.Fatalf("rollback current = %q, %v, %v", id, exists, currentErr)
	}
}

func TestPublisherRejectsHostileCurrentPointer(t *testing.T) {
	filesystem := newMemoryFS()
	runner := &recordingRunner{}
	publisher := newTestPublisher(t, filesystem, runner)
	if err := publisher.ensureBaseDirectories(); err != nil {
		t.Fatal(err)
	}
	filesystem.nodes["/var/lib/celikpanel/bind/current"] = &memoryNode{
		mode: fs.ModeSymlink | 0o777, target: "../../etc", uid: 0, gid: 0,
	}
	if _, _, err := publisher.Current(); err == nil {
		t.Fatal("escaping current symlink was accepted")
	}
}
