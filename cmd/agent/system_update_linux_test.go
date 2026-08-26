//go:build linux

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

func linuxSystemUpdateTestState(requestID string) *systemUpdateState {
	return &systemUpdateState{
		Version: systemUpdateStateVersion, RequestID: requestID, Status: systemUpdateQueued,
		TargetVersion: "v1.2.3", TargetCommit: strings.Repeat("a", 40), TargetSequence: "42",
		TargetOS: "linux", TargetArch: "amd64", TargetArchiveSHA256: strings.Repeat("b", 64),
		TargetArchiveSize: "123", ExpectedCurrentVersion: "v1.2.2", ExpectedCurrentCommit: strings.Repeat("c", 40),
		CreatedAt: "2026-08-12T12:00:00Z", UpdatedAt: "2026-08-12T12:00:00Z",
	}
}

func linuxSystemUpdateTestRoot(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "celikpanel-system-update-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	root := filepath.Join(base, "self-update")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func linuxSystemUpdateTestBase(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "celikpanel-system-update-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	return base
}

type testSystemUpdateIdleLease struct{ closed bool }

func (lease *testSystemUpdateIdleLease) Close() error { lease.closed = true; return nil }

func TestSystemUpdateStateCanonicalAtomicAndRejectsSymlinkHardlink(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	state := linuxSystemUpdateTestState(strings.Repeat("1", 32))
	if err := writeSystemUpdateState(root, state, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := readSystemUpdateState(root, state.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("loaded state = %#v, want %#v", loaded, state)
	}
	info, err := os.Lstat(systemUpdateStatePath(root, state.RequestID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o", info.Mode().Perm())
	}

	symlinkID := strings.Repeat("2", 32)
	if err := os.Symlink(systemUpdateStatePath(root, state.RequestID), systemUpdateStatePath(root, symlinkID)); err != nil {
		t.Fatal(err)
	}
	if _, err := readSystemUpdateState(root, symlinkID); err == nil {
		t.Fatal("symlink state accepted")
	}

	hardlinkID := strings.Repeat("3", 32)
	if err := os.Link(systemUpdateStatePath(root, state.RequestID), systemUpdateStatePath(root, hardlinkID)); err != nil {
		t.Fatal(err)
	}
	if _, err := readSystemUpdateState(root, hardlinkID); err == nil {
		t.Fatal("hard-linked state accepted")
	}
	state.UpdatedAt = "2026-08-12T12:00:01Z"
	if err := writeSystemUpdateState(root, state, nil); err == nil {
		t.Fatal("unsafe existing hard-linked state was overwritten")
	}
}

func TestSystemUpdatePublicKeyAcceptsOnlyOneEd25519PublicPEM(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	parsed, err := parseSystemUpdatePublicKey(raw)
	if err != nil || !reflect.DeepEqual(parsed, publicKey) {
		t.Fatalf("parsed key = %x, %v", parsed, err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	if _, err := parseSystemUpdatePublicKey(privatePEM); err == nil {
		t.Fatal("private key accepted as public key")
	}
	if _, err := parseSystemUpdatePublicKey(append(raw, []byte("trailing")...)); err == nil {
		t.Fatal("trailing public key data accepted")
	}
	if _, err := parseSystemUpdatePublicKey(append([]byte("\n"), raw...)); err == nil {
		t.Fatal("leading whitespace before public key accepted")
	}
	if _, err := parseSystemUpdatePublicKey(append(append([]byte(nil), raw...), '\n')); err == nil {
		t.Fatal("extra trailing whitespace after public key accepted")
	}
	if _, err := loadSystemUpdatePublicKey(filepath.Join(t.TempDir(), "key.pem")); err == nil {
		t.Fatal("unpinned public key path accepted")
	}
}

func TestSystemUpdatePinnedKeyDirectoryAllowsRootOwnedReadOnlyServiceGroup(t *testing.T) {
	safe := unix.Stat_t{Mode: unix.S_IFDIR | 0o750, Uid: 0, Gid: 987}
	if !trustedSystemUpdateDirectoryMetadata(&safe, 0, 0, false, false) {
		t.Fatal("root-owned root:celikpanel 0750 pinned-key directory was rejected")
	}
	groupWritable := safe
	groupWritable.Mode = unix.S_IFDIR | 0o770
	if trustedSystemUpdateDirectoryMetadata(&groupWritable, 0, 0, false, false) {
		t.Fatal("group-writable pinned-key directory was accepted")
	}
	if trustedSystemUpdateDirectoryMetadata(&safe, 0, 0, true, false) {
		t.Fatal("private release-state directory accepted a non-root group")
	}
	if systemUpdateDirectoryRequiresRootGroup(systemUpdateKeyPath, filepath.Dir(systemUpdateKeyPath)) {
		t.Fatal("pinned key direct parent did not allow the reviewed service group")
	}
	if !systemUpdateDirectoryRequiresRootGroup(systemUpdateKeyPath, "/etc") {
		t.Fatal("pinned key ancestor allowed a non-root group")
	}
}

func TestSystemUpdateCreatedDirectoryNormalizesEffectiveServiceGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root is required to exercise the production ownership transition")
	}
	root := filepath.Join(t.TempDir(), "created")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	const serviceGID = 12345
	if err := os.Chown(root, 0, serviceGID); err != nil {
		t.Fatal(err)
	}
	if err := normalizeCreatedSystemUpdateDirectoryForOwner(root, 0, serviceGID); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != 0o700 {
		t.Fatalf("normalized directory metadata = %#v mode=%o", stat, info.Mode().Perm())
	}
}

func TestSystemUpdateStartupRecoversParentAndChildCreatorDirectories(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root is required to exercise production creator-artifact recovery")
	}
	const serviceGID = 12345
	base := linuxSystemUpdateTestBase(t)
	parent := filepath.Join(base, "release-state")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(parent, 0, serviceGID); err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	parentInfo, err = recoverSystemUpdateDirectoryCreatorArtifact(parent, parentInfo, 0, serviceGID)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRootOwnedStateDirectory(parent, parentInfo); err != nil {
		t.Fatalf("parent creator artifact was not normalized: %v", err)
	}

	child := filepath.Join(parent, "self-update")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(child, 0, serviceGID); err != nil {
		t.Fatal(err)
	}
	childInfo, err := os.Lstat(child)
	if err != nil {
		t.Fatal(err)
	}
	childInfo, err = recoverSystemUpdateDirectoryCreatorArtifact(child, childInfo, 0, serviceGID)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRootOwnedStateDirectory(child, childInfo); err != nil {
		t.Fatalf("child creator artifact was not normalized: %v", err)
	}

	foreign := filepath.Join(parent, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(foreign, 0, serviceGID+1); err != nil {
		t.Fatal(err)
	}
	foreignInfo, err := os.Lstat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	foreignInfo, err = recoverSystemUpdateDirectoryCreatorArtifact(foreign, foreignInfo, 0, serviceGID)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRootOwnedStateDirectory(foreign, foreignInfo); err == nil {
		t.Fatal("foreign-group directory was normalized")
	}
}

func TestSystemUpdateStateWriteFaultBeforeRenamePreservesOldState(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	state := linuxSystemUpdateTestState(strings.Repeat("4", 32))
	if err := writeSystemUpdateState(root, state, nil); err != nil {
		t.Fatal(err)
	}
	updated := *state
	updated.Status = systemUpdateRunning
	updated.UpdatedAt = "2026-08-12T12:00:01Z"
	err := writeSystemUpdateState(root, &updated, func(point string) error {
		if point == "before_rename" {
			return errors.New("crash")
		}
		return nil
	})
	if err == nil {
		t.Fatal("injected pre-rename crash succeeded")
	}
	loaded, err := readSystemUpdateState(root, state.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != systemUpdateQueued {
		t.Fatalf("pre-rename crash published %q", loaded.Status)
	}
}

func TestSystemUpdateStartupMissingStateIsNoopAndUnsafeAliasFails(t *testing.T) {
	parent := linuxSystemUpdateTestBase(t)
	missing := filepath.Join(parent, "missing-self-update")
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = missing
	if err := backend.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Fatalf("startup reconciliation provisioned absent state: %v", err)
	}

	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	backend.stateRoot = alias
	if err := backend.Reconcile(context.Background()); err == nil {
		t.Fatal("startup reconciliation accepted a symlink state root")
	}
}

func TestSystemUpdateStartupRecoversOnlySafeStagingArtifacts(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	requestID := strings.Repeat("d", 32)
	stage := filepath.Join(root, ".state-"+requestID+"-12345")
	if err := os.WriteFile(stage, []byte("interrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = root
	if err := backend.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("safe stale staging artifact remains: %v", err)
	}

	unsafe := filepath.Join(root, ".state-"+requestID+"-not-canonical")
	if err := os.WriteFile(unsafe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := backend.Reconcile(context.Background()); err == nil {
		t.Fatal("non-canonical staging artifact was silently removed")
	}
	if _, err := os.Lstat(unsafe); err != nil {
		t.Fatalf("unsafe staging artifact was mutated: %v", err)
	}
}

func TestSystemUpdateStagingRecoveryAllowsOnlyRootEffectiveGroupCrashWindow(t *testing.T) {
	const serviceGID = 12345
	if !systemUpdateStagingOwnerAllowed(false, 0, serviceGID, 0, 0, 0, serviceGID) {
		t.Fatal("root:effective-group CreateTemp crash artifact was rejected")
	}
	if systemUpdateStagingOwnerAllowed(false, 0, serviceGID, 0, 0, 0, serviceGID+1) {
		t.Fatal("foreign-group staging artifact was accepted")
	}
	if systemUpdateStagingOwnerAllowed(false, 1000, serviceGID, 0, 0, 1000, serviceGID) {
		t.Fatal("non-root production staging artifact was accepted")
	}
}

func TestSystemUpdateLaunchAndInstallerUseExactFixedArguments(t *testing.T) {
	oldResolver, oldRunner := systemUpdateExecutableResolver, systemUpdateCommandRunner
	t.Cleanup(func() { systemUpdateExecutableResolver, systemUpdateCommandRunner = oldResolver, oldRunner })
	systemUpdateExecutableResolver = func(candidates ...string) (string, error) { return candidates[0], nil }
	type call struct {
		name string
		args []string
	}
	var calls []call
	systemUpdateCommandRunner = func(_ context.Context, name string, args []string) ([]byte, error) {
		calls = append(calls, call{name, append([]string(nil), args...)})
		return nil, nil
	}
	backend := newLinuxSystemUpdateBackend()
	requestID := strings.Repeat("5", 32)
	if err := backend.launchWorker(context.Background(), requestID); err != nil {
		t.Fatal(err)
	}
	wantLaunch := []string{"--unit=celikpanel-self-update-" + requestID + ".service", "--property=Type=exec", "--property=KillMode=mixed", "--property=TimeoutStartSec=50min", "--no-block", systemUpdateAgentPath, "--self-update-worker", requestID}
	if len(calls) != 1 || calls[0].name != "/usr/bin/systemd-run" || !reflect.DeepEqual(calls[0].args, wantLaunch) {
		t.Fatalf("launch calls = %#v", calls)
	}
	state := linuxSystemUpdateTestState(requestID)
	if err := runSystemUpdateInstaller(context.Background(), state, "41"); err != nil {
		t.Fatal(err)
	}
	wantInstaller := []string{
		"--update", "--version", state.TargetVersion, "--require-signed-manifest",
		"--expected-sequence", state.TargetSequence, "--minimum-sequence", "41",
		"--expected-commit", state.TargetCommit,
		"--expected-archive-sha256", state.TargetArchiveSHA256,
		"--expected-archive-size", state.TargetArchiveSize,
	}
	if len(calls) != 2 || calls[1].name != systemUpdateInstallerPath || !reflect.DeepEqual(calls[1].args, wantInstaller) {
		t.Fatalf("installer calls = %#v", calls)
	}
}

func TestSystemUpdateQueueRejectsConcurrentAndLaunchFailureIsDurable(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = root
	idleLease := &testSystemUpdateIdleLease{}
	backend.acquireIdle = func() (systemUpdateIdleLease, error) { return idleLease, nil }
	backend.now = func() time.Time { return time.Date(2026, 8, 12, 12, 0, 1, 0, time.UTC) }
	backend.launch = func(context.Context, string) error {
		if idleLease.closed {
			return errors.New("host mutation lock released before launch")
		}
		return nil
	}
	first := linuxSystemUpdateTestState(strings.Repeat("6", 32))
	if _, err := backend.QueueAndLaunch(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if !idleLease.closed {
		t.Fatal("host mutation lock was not released after launch")
	}
	if existing, err := backend.QueueAndLaunch(context.Background(), first); err != nil || existing.RequestID != first.RequestID {
		t.Fatalf("exact active retry was not idempotent: %#v, %v", existing, err)
	}
	second := linuxSystemUpdateTestState(strings.Repeat("7", 32))
	if _, err := backend.QueueAndLaunch(context.Background(), second); err == nil {
		t.Fatal("concurrent request accepted")
	}

	root2 := linuxSystemUpdateTestRoot(t)
	failedBackend := newLinuxSystemUpdateBackend()
	failedBackend.stateRoot = root2
	failedBackend.acquireIdle = nil
	failedBackend.checkIdle = func() error { return nil }
	failedBackend.now = backend.now
	failedBackend.launch = func(context.Context, string) error { return errors.New("systemd unavailable") }
	failed := linuxSystemUpdateTestState(strings.Repeat("8", 32))
	if _, err := failedBackend.QueueAndLaunch(context.Background(), failed); err == nil {
		t.Fatal("launch failure returned success")
	}
	loaded, err := readSystemUpdateState(root2, failed.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != systemUpdateFailed || !strings.Contains(loaded.Error, "systemd unavailable") {
		t.Fatalf("failed state = %#v", loaded)
	}
}

func TestSystemUpdateQueueReconcilesDeadActiveRequestBeforeNewStart(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	old := linuxSystemUpdateTestState(strings.Repeat("e", 32))
	old.Status = systemUpdateRunning
	if err := writeSystemUpdateState(root, old, nil); err != nil {
		t.Fatal(err)
	}
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = root
	backend.floorPath = filepath.Join(root, "missing-floor")
	backend.now = func() time.Time { return time.Date(2026, 8, 12, 12, 3, 0, 0, time.UTC) }
	backend.unitState = func(context.Context, string) (systemUpdateUnitState, error) {
		return systemUpdateUnitInactive, nil
	}
	backend.installedBuild = func(context.Context) (string, string, error) {
		return old.ExpectedCurrentVersion, old.ExpectedCurrentCommit, nil
	}
	backend.acquireIdle = func() (systemUpdateIdleLease, error) {
		return &testSystemUpdateIdleLease{}, nil
	}
	backend.launch = func(context.Context, string) error { return nil }
	next := linuxSystemUpdateTestState(strings.Repeat("f", 32))
	if _, err := backend.QueueAndLaunch(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	reconciled, err := readSystemUpdateState(root, old.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Status != systemUpdateFailed {
		t.Fatalf("dead active request remained %q", reconciled.Status)
	}
}

func TestSystemUpdateReconcileRetainsAmbiguityAndClosesInactiveCrash(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	state := linuxSystemUpdateTestState(strings.Repeat("9", 32))
	state.Status = systemUpdateRunning
	state.UpdatedAt = "2026-08-12T12:01:00Z"
	if err := writeSystemUpdateState(root, state, nil); err != nil {
		t.Fatal(err)
	}
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = root
	backend.floorPath = filepath.Join(root, "missing-floor")
	backend.now = func() time.Time { return time.Date(2026, 8, 12, 12, 2, 0, 0, time.UTC) }
	backend.installedBuild = func(context.Context) (string, string, error) { return "v1.2.2", strings.Repeat("c", 40), nil }
	backend.unitState = func(context.Context, string) (systemUpdateUnitState, error) {
		return systemUpdateUnitAmbiguous, errors.New("dbus ambiguous")
	}
	got, err := backend.Status(context.Background(), state.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != systemUpdateRunning {
		t.Fatalf("ambiguous unit changed status to %q", got.Status)
	}
	backend.unitState = func(context.Context, string) (systemUpdateUnitState, error) { return systemUpdateUnitInactive, nil }
	got, err = backend.Status(context.Background(), state.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != systemUpdateFailed {
		t.Fatalf("inactive crashed worker status = %q", got.Status)
	}
}

func TestSystemUpdateWorkerRevalidatesAndProvesSuccess(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	state := linuxSystemUpdateTestState(strings.Repeat("a", 32))
	if err := writeSystemUpdateState(root, state, nil); err != nil {
		t.Fatal(err)
	}
	manifest := systemUpdateManifest{
		Sequence: state.TargetSequence, Version: state.TargetVersion, Commit: state.TargetCommit,
		PublishedAt: "2026-08-12T12:00:00Z", OS: state.TargetOS, Arch: state.TargetArch,
		Archive:       "celikpanel-" + state.TargetVersion + "-linux-amd64.tar.gz",
		ArchiveSHA256: state.TargetArchiveSHA256, ArchiveSize: state.TargetArchiveSize,
	}
	fetcher := &fakeSystemUpdateFetcher{manifest: manifest}
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = root
	backend.floorPath = filepath.Join(root, "sequence.floor")
	if err := os.WriteFile(backend.floorPath, canonicalSystemUpdateFloor(systemUpdateFloor{Sequence: "41", Version: "v1.2.2"}), 0o600); err != nil {
		t.Fatal(err)
	}
	backend.now = func() time.Time { return time.Date(2026, 8, 12, 12, 2, 0, 0, time.UTC) }
	backend.acquireIdle = func() (systemUpdateIdleLease, error) {
		t.Fatal("worker retained the launch-time mutation lease across the reviewed updater")
		return nil, errors.New("unreachable")
	}
	backend.runInstaller = func(_ context.Context, _ *systemUpdateState, trustedMinimum string) error {
		if trustedMinimum != "41" {
			return errors.New("worker did not pass the trusted floor")
		}
		return os.WriteFile(backend.floorPath, canonicalSystemUpdateFloor(systemUpdateFloor{Sequence: state.TargetSequence, Version: state.TargetVersion}), 0o600)
	}
	backend.installedBuild = func(context.Context) (string, string, error) { return state.TargetVersion, state.TargetCommit, nil }
	if err := backend.RunWorker(context.Background(), state.RequestID, fetcher); err != nil {
		t.Fatal(err)
	}
	loaded, err := readSystemUpdateState(root, state.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != systemUpdateSucceeded || loaded.Error != "" {
		t.Fatalf("worker result = %#v", loaded)
	}
	if fetcher.fetchCalls != 1 {
		t.Fatalf("worker fetch calls = %d", fetcher.fetchCalls)
	}
}

func TestSystemUpdateReviewedUpdaterOwnsTheSecondMutationGate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "update.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`if ! flock -n -x "$MUTATION_LOCK_FD"; then`,
		`wait_for_post_apply_mutation_idle() {`,
		`for attempt in $(seq 1 60); do`,
		`sleep 0.5`,
		`--check-service-mutation-idle-under-external-lock`,
		`env -i \`,
		`CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \`,
		`CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \`,
		`CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \`,
		`"$BIN_DIR/agent" --prepare-bind-generation-root-under-external-lock`,
		`rollback intentionally retains them for alpha35 compatibility`,
	} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("reviewed updater is missing its pre-mutation gate %q", required)
		}
	}
	script := string(raw)
	anchor := strings.Index(script, "# Apply-only returns with both coordinators stopped.")
	if anchor < 0 {
		t.Fatal("reviewed updater apply-only safe window is missing")
	}
	window := script[anchor:]
	order := []string{
		`verify_installed_release_artifacts`,
		`wait_for_post_apply_mutation_idle`,
		`env -i \`,
		`"$BIN_DIR/agent" --prepare-bind-generation-root-under-external-lock`,
		`verify_installed_release_artifacts`,
		`find "$BIN_DIR" "$WEB_DIR" -type f`,
		`release_txn_mark_completion_pending \`,
	}
	cursor := 0
	for _, marker := range order {
		next := strings.Index(window[cursor:], marker)
		if next < 0 {
			t.Fatalf("reviewed updater safe-window order is missing %q", marker)
		}
		cursor += next + len(marker)
	}
}

func TestSystemUpdateLinuxPackageFamilyPolicy(t *testing.T) {
	oldDetector := detectHostPlatform
	t.Cleanup(func() { detectHostPlatform = oldDetector })
	for _, test := range []struct {
		name      string
		manager   hostplatform.PackageManager
		supported bool
	}{
		{name: "apt", manager: hostplatform.PackageManagerAPT, supported: true},
		{name: "pacman", manager: hostplatform.PackageManagerPacman, supported: true},
		{name: "dnf", manager: hostplatform.PackageManagerDNF, supported: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			detectHostPlatform = func() (hostplatform.Profile, error) {
				return hostplatform.Profile{PackageManager: test.manager}, nil
			}
			service, err := newPlatformSystemUpdateService()
			if err != nil {
				t.Fatal(err)
			}
			if service.supported() != test.supported {
				t.Fatalf("%s supported = %t, want %t", test.manager, service.supported(), test.supported)
			}
			if !test.supported {
				response, err := service.check(context.Background())
				if err == nil || response.Supported {
					t.Fatalf("dnf check = %#v, %v", response, err)
				}
			}
		})
	}
}

func TestSystemUpdateWorkerPersistsInstallerFailure(t *testing.T) {
	root := linuxSystemUpdateTestRoot(t)
	state := linuxSystemUpdateTestState(strings.Repeat("b", 32))
	if err := writeSystemUpdateState(root, state, nil); err != nil {
		t.Fatal(err)
	}
	manifest := systemUpdateManifest{Sequence: state.TargetSequence, Version: state.TargetVersion, Commit: state.TargetCommit, PublishedAt: "2026-08-12T12:00:00Z", OS: state.TargetOS, Arch: state.TargetArch, Archive: "celikpanel-" + state.TargetVersion + "-linux-amd64.tar.gz", ArchiveSHA256: state.TargetArchiveSHA256, ArchiveSize: state.TargetArchiveSize}
	backend := newLinuxSystemUpdateBackend()
	backend.stateRoot = root
	backend.floorPath = filepath.Join(root, "sequence.floor")
	if err := os.WriteFile(backend.floorPath, canonicalSystemUpdateFloor(systemUpdateFloor{Sequence: "41", Version: "v1.2.2"}), 0o600); err != nil {
		t.Fatal(err)
	}
	backend.now = func() time.Time { return time.Date(2026, 8, 12, 12, 2, 0, 0, time.UTC) }
	backend.runInstaller = func(context.Context, *systemUpdateState, string) error { return errors.New("installer stopped") }
	err := backend.RunWorker(context.Background(), state.RequestID, &fakeSystemUpdateFetcher{manifest: manifest})
	if err == nil {
		t.Fatal("worker reported installer failure as success")
	}
	loaded, readErr := readSystemUpdateState(root, state.RequestID)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if loaded.Status != systemUpdateFailed || !strings.Contains(loaded.Error, "installer stopped") {
		t.Fatalf("worker failure state = %#v", loaded)
	}
}
