package binddns

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

const (
	rootDirectoryMode      = fs.FileMode(0o755)
	immutableDirectoryMode = fs.FileMode(0o555)
	immutableFileMode      = fs.FileMode(0o444)
	maxGenerationFileBytes = int64(16 << 20)
)

// Publisher stages immutable, root-owned BIND generations and atomically
// changes the current symlink. Cross-process serialization remains the agent's
// service-mutation lock responsibility; mu protects callers sharing an object.
type Publisher struct {
	mu        sync.Mutex
	fs        FileSystem
	runner    CommandRunner
	root      string
	nonce     func() (string, error)
	checkZone string
	checkConf string
}

func NewPublisher(root string, filesystem FileSystem, runner CommandRunner) (*Publisher, error) {
	if err := validateRoot(root); err != nil {
		return nil, err
	}
	if filesystem == nil || runner == nil {
		return nil, errors.New("BIND publisher requires a filesystem and command runner")
	}
	return &Publisher{
		fs: filesystem, runner: runner, root: root,
		nonce: randomNonce, checkZone: "named-checkzone", checkConf: "named-checkconf",
	}, nil
}

func NewOSPublisher(root string) (*Publisher, error) {
	return NewPublisher(root, OSFileSystem{}, ExecRunner{})
}

// StagePlan renders and stages one already-verified path-independent plan.
func (publisher *Publisher) StagePlan(ctx context.Context, plan TreePlan) (Generation, error) {
	generation, err := RenderTree(publisher.root, plan)
	if err != nil {
		return Generation{}, err
	}
	if err := publisher.Stage(ctx, generation); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

// Stage writes and validates a complete immutable generation without changing
// the current pointer. Re-staging the exact same verified generation is safe.
func (publisher *Publisher) Stage(ctx context.Context, generation Generation) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	canonical, err := publisher.canonicalGeneration(generation)
	if err != nil {
		return err
	}
	if err := publisher.ensureBaseDirectories(); err != nil {
		return err
	}
	finalPath := path.Join(publisher.root, "generations", canonical.ID)
	if _, err := publisher.fs.Lstat(finalPath); err == nil {
		return publisher.verifyExistingMatches(finalPath, canonical)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect BIND generation target: %w", err)
	}

	nonce, err := publisher.nonce()
	if err != nil || !validNonce(nonce) {
		return errors.New("create safe BIND staging nonce")
	}
	stagePath := path.Join(publisher.root, ".stage-"+canonical.ID[:16]+"-"+nonce)
	if err := publisher.createDirectory(stagePath, rootDirectoryMode); err != nil {
		return fmt.Errorf("create BIND generation staging directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = publisher.fs.RemoveAll(stagePath)
		}
	}()
	zonesPath := path.Join(stagePath, "zones")
	if err := publisher.createDirectory(zonesPath, rootDirectoryMode); err != nil {
		return err
	}
	for _, zone := range canonical.Zones {
		zonePath := path.Join(zonesPath, zone.FileName)
		if err := publisher.writeImmutableFile(zonePath, zone.Data); err != nil {
			return fmt.Errorf("stage BIND zone %s: %w", zone.Domain, err)
		}
		if _, err := publisher.runner.Run(ctx, publisher.checkZone, zone.Domain, zonePath); err != nil {
			return fmt.Errorf("validate BIND zone %s: %w", zone.Domain, err)
		}
	}
	configPath := path.Join(stagePath, "zones.conf")
	if err := publisher.writeImmutableFile(configPath, canonical.Config); err != nil {
		return fmt.Errorf("stage BIND zone configuration: %w", err)
	}
	if _, err := publisher.runner.Run(ctx, publisher.checkConf, configPath); err != nil {
		return fmt.Errorf("validate BIND zone configuration: %w", err)
	}
	if err := publisher.writeImmutableFile(path.Join(stagePath, "receipt.json"), canonical.Receipt); err != nil {
		return fmt.Errorf("stage BIND generation receipt: %w", err)
	}
	if err := publisher.fs.Chmod(zonesPath, immutableDirectoryMode); err != nil {
		return err
	}
	if err := publisher.fs.Sync(zonesPath); err != nil {
		return err
	}
	if err := publisher.fs.Chmod(stagePath, immutableDirectoryMode); err != nil {
		return err
	}
	if err := publisher.fs.Sync(stagePath); err != nil {
		return err
	}
	if err := publisher.fs.RenameNoReplace(stagePath, finalPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return publisher.verifyExistingMatches(finalPath, canonical)
		}
		return fmt.Errorf("publish immutable BIND generation: %w", err)
	}
	cleanup = false
	if err := publisher.fs.Sync(path.Join(publisher.root, "generations")); err != nil {
		return fmt.Errorf("sync BIND generation catalog: %w", err)
	}
	return nil
}

func (publisher *Publisher) canonicalGeneration(generation Generation) (Generation, error) {
	files := make(map[string][]byte, len(generation.Zones))
	for _, zone := range generation.Zones {
		file := path.Join("zones", zone.FileName)
		if _, exists := files[file]; exists {
			return Generation{}, fmt.Errorf("BIND generation repeats zone file %q", file)
		}
		files[file] = append([]byte(nil), zone.Data...)
	}
	tree, err := VerifyTree(generation.Receipt, generation.Config, files)
	if err != nil {
		return Generation{}, err
	}
	canonical, err := RenderTree(publisher.root, TreePlan{
		engineEpoch: tree.receipt.EngineEpoch,
		zones:       cloneTreeZones(tree.zones),
	})
	if err != nil {
		return Generation{}, err
	}
	if generation.ID != canonical.ID || !bytes.Equal(generation.Receipt, canonical.Receipt) ||
		!bytes.Equal(generation.Config, canonical.Config) {
		return Generation{}, errors.New("BIND generation is not canonical for this publisher root")
	}
	return canonical, nil
}

func (publisher *Publisher) ensureBaseDirectories() error {
	if err := publisher.ensureDirectory(publisher.root, rootDirectoryMode, false); err != nil {
		return err
	}
	return publisher.ensureDirectory(path.Join(publisher.root, "generations"), rootDirectoryMode, false)
}

func (publisher *Publisher) ensureDirectory(name string, mode fs.FileMode, immutable bool) error {
	entry, err := publisher.fs.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		if err := publisher.createDirectory(name, mode); err != nil {
			return err
		}
		entry, err = publisher.fs.Lstat(name)
	}
	if err != nil {
		return fmt.Errorf("inspect BIND directory %s: %w", name, err)
	}
	return verifyDirectoryEntry(name, entry, immutable)
}

func (publisher *Publisher) createDirectory(name string, mode fs.FileMode) error {
	if _, err := publisher.fs.Lstat(name); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := publisher.fs.Mkdir(name, mode); err != nil {
		return err
	}
	if err := publisher.fs.Chown(name, 0, 0); err != nil {
		return err
	}
	return publisher.fs.Chmod(name, mode)
}

func (publisher *Publisher) writeImmutableFile(name string, content []byte) error {
	if len(content) > int(maxGenerationFileBytes) {
		return errors.New("BIND generation file exceeds the size limit")
	}
	if err := publisher.fs.WriteFileExclusive(name, content, immutableFileMode); err != nil {
		return err
	}
	if err := publisher.fs.Chown(name, 0, 0); err != nil {
		return err
	}
	if err := publisher.fs.Chmod(name, immutableFileMode); err != nil {
		return err
	}
	return publisher.fs.Sync(name)
}

func (publisher *Publisher) verifyExistingMatches(finalPath string, expected Generation) error {
	tree, receiptBytes, configBytes, err := publisher.readGeneration(finalPath, expected.ID)
	if err != nil {
		return err
	}
	if !bytes.Equal(receiptBytes, expected.Receipt) || !bytes.Equal(configBytes, expected.Config) {
		return errors.New("existing BIND generation ID has different immutable content")
	}
	canonical, err := RenderTree(publisher.root, TreePlan{
		engineEpoch: tree.receipt.EngineEpoch,
		zones:       cloneTreeZones(tree.zones),
	})
	if err != nil || canonical.ID != expected.ID {
		return errors.New("existing BIND generation cannot be reconstructed exactly")
	}
	return nil
}

func (publisher *Publisher) readGeneration(generationPath, expectedID string) (VerifiedTree, []byte, []byte, error) {
	entry, err := publisher.fs.Lstat(generationPath)
	if err != nil {
		return VerifiedTree{}, nil, nil, err
	}
	if err := verifyDirectoryEntry(generationPath, entry, true); err != nil {
		return VerifiedTree{}, nil, nil, err
	}
	rootNames, err := publisher.fs.ReadDirNames(generationPath)
	if err != nil {
		return VerifiedTree{}, nil, nil, err
	}
	if !equalNames(rootNames, []string{"receipt.json", "zones", "zones.conf"}) {
		return VerifiedTree{}, nil, nil, errors.New("BIND generation contains unexpected top-level entries")
	}
	receiptBytes, err := publisher.readImmutableFile(path.Join(generationPath, "receipt.json"))
	if err != nil {
		return VerifiedTree{}, nil, nil, err
	}
	receipt, err := DecodeReceipt(receiptBytes)
	if err != nil || receipt.Generation != expectedID {
		return VerifiedTree{}, nil, nil, errors.New("BIND generation receipt identity mismatch")
	}
	configBytes, err := publisher.readImmutableFile(path.Join(generationPath, "zones.conf"))
	if err != nil {
		return VerifiedTree{}, nil, nil, err
	}
	zonesPath := path.Join(generationPath, "zones")
	zonesEntry, err := publisher.fs.Lstat(zonesPath)
	if err != nil || verifyDirectoryEntry(zonesPath, zonesEntry, true) != nil {
		return VerifiedTree{}, nil, nil, errors.New("BIND generation zones directory is unsafe")
	}
	zoneNames, err := publisher.fs.ReadDirNames(zonesPath)
	if err != nil {
		return VerifiedTree{}, nil, nil, err
	}
	expectedNames := make([]string, 0, len(receipt.Zones))
	files := make(map[string][]byte, len(receipt.Zones))
	for _, zone := range receipt.Zones {
		if zone.Delete {
			continue
		}
		name := path.Base(zone.File)
		expectedNames = append(expectedNames, name)
		data, err := publisher.readImmutableFile(path.Join(zonesPath, name))
		if err != nil {
			return VerifiedTree{}, nil, nil, err
		}
		files[zone.File] = data
	}
	if !equalNames(zoneNames, expectedNames) {
		return VerifiedTree{}, nil, nil, errors.New("BIND generation zone directory does not match its receipt")
	}
	tree, err := VerifyTree(receiptBytes, configBytes, files)
	return tree, receiptBytes, configBytes, err
}

func (publisher *Publisher) readImmutableFile(name string) ([]byte, error) {
	entry, err := publisher.fs.Lstat(name)
	if err != nil {
		return nil, err
	}
	if entry.Mode&fs.ModeSymlink != 0 || !entry.Mode.IsRegular() || entry.Size < 0 ||
		entry.Size > maxGenerationFileBytes || entry.Mode.Perm()&0o222 != 0 ||
		(entry.OwnerKnown && (entry.UID != 0 || entry.GID != 0)) {
		return nil, fmt.Errorf("BIND generation file is unsafe: %s", name)
	}
	return publisher.fs.ReadFile(name)
}

func verifyDirectoryEntry(name string, entry Entry, immutable bool) error {
	if entry.Mode&fs.ModeSymlink != 0 || !entry.Mode.IsDir() ||
		entry.Mode.Perm()&0o022 != 0 || (immutable && entry.Mode.Perm()&0o222 != 0) ||
		(entry.OwnerKnown && (entry.UID != 0 || entry.GID != 0)) {
		return fmt.Errorf("BIND directory is unsafe: %s", name)
	}
	return nil
}

func equalNames(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

// LoadCurrent verifies and returns the complete currently selected tree.
func (publisher *Publisher) LoadCurrent() (VerifiedTree, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	id, exists, err := publisher.currentLocked()
	if err != nil {
		return VerifiedTree{}, err
	}
	if !exists {
		return VerifiedTree{}, fs.ErrNotExist
	}
	tree, _, _, err := publisher.readGeneration(path.Join(publisher.root, "generations", id), id)
	return tree, err
}

// Current returns the verified generation ID selected by the current symlink.
func (publisher *Publisher) Current() (string, bool, error) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.currentLocked()
}

// Activate atomically points current at an already verified immutable tree.
func (publisher *Publisher) Activate(generationID string) error {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.activateLocked(generationID)
}

func (publisher *Publisher) activateLocked(generationID string) error {
	if !validDigest(generationID) {
		return errors.New("invalid BIND generation ID")
	}
	if _, _, _, err := publisher.readGeneration(
		path.Join(publisher.root, "generations", generationID), generationID,
	); err != nil {
		return fmt.Errorf("verify BIND generation before activation: %w", err)
	}
	currentID, exists, err := publisher.currentLocked()
	if err != nil {
		return err
	}
	if exists && currentID == generationID {
		return nil
	}
	nonce, err := publisher.nonce()
	if err != nil || !validNonce(nonce) {
		return errors.New("create safe BIND activation nonce")
	}
	temporary := path.Join(publisher.root, ".current-"+nonce)
	if _, err := publisher.fs.Lstat(temporary); err == nil {
		return errors.New("BIND activation pointer candidate already exists")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	target := path.Join("generations", generationID)
	if err := publisher.fs.Symlink(target, temporary); err != nil {
		return fmt.Errorf("create BIND activation pointer: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = publisher.fs.Remove(temporary)
		}
	}()
	if err := publisher.fs.Lchown(temporary, 0, 0); err != nil {
		return err
	}
	if err := publisher.fs.Sync(publisher.root); err != nil {
		return err
	}
	if err := publisher.fs.RenameReplace(temporary, path.Join(publisher.root, "current")); err != nil {
		return fmt.Errorf("activate BIND generation pointer: %w", err)
	}
	cleanup = false
	return publisher.fs.Sync(publisher.root)
}

func (publisher *Publisher) currentLocked() (string, bool, error) {
	currentPath := path.Join(publisher.root, "current")
	entry, err := publisher.fs.Lstat(currentPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if entry.Mode&fs.ModeSymlink == 0 || (entry.OwnerKnown && (entry.UID != 0 || entry.GID != 0)) {
		return "", false, errors.New("BIND current pointer is not a root-owned symlink")
	}
	target, err := publisher.fs.Readlink(currentPath)
	if err != nil {
		return "", false, err
	}
	if !strings.HasPrefix(target, "generations/") {
		return "", false, errors.New("BIND current pointer escapes the generation catalog")
	}
	id := strings.TrimPrefix(target, "generations/")
	if !validDigest(id) || target != path.Join("generations", id) {
		return "", false, errors.New("BIND current pointer target is invalid")
	}
	return id, true, nil
}

// Switch atomically selects generationID and calls apply (normally rndc
// reconfig or a unit start). On failure it restores the prior pointer and calls
// apply again, surfacing both activation and rollback errors.
func (publisher *Publisher) Switch(ctx context.Context, generationID string, apply func(context.Context) error) error {
	if apply == nil {
		return errors.New("BIND switch requires an activation callback")
	}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	previous, hadPrevious, err := publisher.currentLocked()
	if err != nil {
		return err
	}
	if err := publisher.activateLocked(generationID); err != nil {
		return err
	}
	if err := apply(ctx); err == nil {
		return nil
	} else {
		activationErr := fmt.Errorf("activate BIND generation %s: %w", generationID, err)
		var pointerErr error
		if hadPrevious {
			pointerErr = publisher.activateLocked(previous)
		} else {
			current, exists, inspectErr := publisher.currentLocked()
			switch {
			case inspectErr != nil:
				pointerErr = inspectErr
			case !exists || current != generationID:
				pointerErr = errors.New("BIND current pointer changed during rollback")
			default:
				pointerErr = publisher.fs.Remove(path.Join(publisher.root, "current"))
				if pointerErr == nil {
					pointerErr = publisher.fs.Sync(publisher.root)
				}
			}
		}
		if pointerErr != nil {
			return errors.Join(activationErr, fmt.Errorf("restore BIND generation pointer: %w", pointerErr))
		}
		rollbackErr := apply(ctx)
		if rollbackErr != nil {
			return errors.Join(activationErr, fmt.Errorf("reload restored BIND generation: %w", rollbackErr))
		}
		return errors.Join(activationErr, errors.New("previous BIND generation restored and applied"))
	}
}

func randomNonce() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func validNonce(value string) bool {
	if len(value) != 16 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
