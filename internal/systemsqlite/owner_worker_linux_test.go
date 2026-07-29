//go:build linux

package systemsqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	ownerWorkerHelperEnvironment         = "CELIKPANEL_SQLITE_OWNER_WORKER_TEST"
	ownerWorkerExplicitWriterEnvironment = "CELIKPANEL_SQLITE_OWNER_WORKER_EXPLICIT_WRITER_TEST"
	ownerWorkerExpectLimitsEnvironment   = "CELIKPANEL_SQLITE_OWNER_WORKER_LIMITS_TEST"
	ownerWorkerSnapshotEnvironment       = "CELIKPANEL_SQLITE_OWNER_WORKER_SNAPSHOT_TEST"
)

func createOwnerWorkspaceRootForTest(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", ".celikpanel-owner-workspace-root-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, os.ModeSticky|0o777); err != nil {
		_ = os.RemoveAll(root)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(root, 0o700)
		_ = os.RemoveAll(root)
	})
	return root
}

func TestOwnerWorkerDroppedProcessHelper(t *testing.T) {
	if os.Getenv(ownerWorkerHelperEnvironment) != "1" {
		return
	}
	if err := PrepareOwnerWorkerProcess(); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(OwnerWorkerResponse{Error: err.Error()})
		os.Exit(0)
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 {
		_ = json.NewEncoder(os.Stdout).Encode(OwnerWorkerResponse{
			Error: "dropped worker retained supplementary groups",
		})
		os.Exit(0)
	}
	path := os.Getenv("CELIKPANEL_SQLITE_OWNER_WORKER_TEST_DB")
	definition := testDefinition(DatabasePowerDNS, path, true)
	if os.Getenv(ownerWorkerExplicitWriterEnvironment) == "1" {
		definition.WriterUID = uint32(os.Geteuid())
		definition.WriterGID = uint32(os.Getegid())
		definition.WriterIdentitySet = true
	}
	action := OwnerWorkerCheck
	var destination *os.File
	var workspace *os.File
	limits := SnapshotLimits{}
	if os.Getenv(ownerWorkerSnapshotEnvironment) == "1" {
		action = OwnerWorkerSnapshot
		destination = os.NewFile(uintptr(OwnerWorkerDestinationFD), "test-snapshot-destination")
		workspace = os.NewFile(uintptr(OwnerWorkerWorkspaceFD), "test-snapshot-workspace")
		limits = SnapshotLimits{MaxBytes: 1 << 20, FreeSpaceFloor: 4096}
	}
	response := RunOwnerWorkerOperation(
		context.Background(),
		[]Definition{definition},
		action,
		DatabasePowerDNS,
		destination,
		workspace,
		limits,
	)
	if response.Success && os.Getenv(ownerWorkerExpectLimitsEnvironment) == "1" {
		if err := verifyOwnerWorkerResourceLimits(); err != nil {
			response = OwnerWorkerResponse{Error: err.Error()}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(response)
	os.Exit(0)
}

func TestOwnerWorkerCredentialDropsSupplementaryRootGroups(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential transition test requires a root test parent")
	}
	const ownerID = uint32(65534)
	root, err := os.MkdirTemp("/tmp", ".celikpanel-owner-worker-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	path := filepath.Join(root, "owner.sqlite3")
	database := createTestSQLite(t, path, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, int(ownerID), int(ownerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, int(ownerID), int(ownerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("/proc/self/exe", "-test.run=TestOwnerWorkerDroppedProcessHelper")
	command.Env = []string{
		ownerWorkerHelperEnvironment + "=1",
		"CELIKPANEL_SQLITE_OWNER_WORKER_TEST_DB=" + path,
		ownerWorkerExpectLimitsEnvironment + "=1",
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: ownerWorkerCredential(ownerID, ownerID),
		Pdeathsig:  syscall.SIGKILL,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("dropped owner worker failed: %v: %s", err, stderr.String())
	}
	var response OwnerWorkerResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("worker response %q: %v", stdout.String(), err)
	}
	if !response.Success || !response.Check.IntegrityOK || response.Error != "" {
		t.Fatalf("worker response = %+v", response)
	}
}

func TestOwnerWorkerResourceLimitPolicy(t *testing.T) {
	got := make(map[int]uint64)
	for _, policy := range ownerWorkerResourceLimits() {
		if _, exists := got[policy.resource]; exists {
			t.Fatalf("duplicate resource limit %d", policy.resource)
		}
		got[policy.resource] = policy.ceiling
	}
	want := map[int]uint64{
		unix.RLIMIT_CORE:   0,
		unix.RLIMIT_FSIZE:  uint64(maxOwnerWorkerSnapshotBytes),
		unix.RLIMIT_AS:     ownerWorkerAddressSpaceBytes,
		unix.RLIMIT_CPU:    ownerWorkerCPUSeconds,
		unix.RLIMIT_NOFILE: ownerWorkerOpenFiles,
	}
	if len(got) != len(want) {
		t.Fatalf("resource limits = %#v, want %#v", got, want)
	}
	for resource, ceiling := range want {
		if got[resource] != ceiling {
			t.Fatalf("resource %d ceiling = %d, want %d", resource, got[resource], ceiling)
		}
	}
}

func TestOwnerWorkerCredentialRequestsEmptySupplementaryGroups(t *testing.T) {
	credential := ownerWorkerCredential(1234, 5678)
	if credential.NoSetGroups || credential.Uid != 1234 || credential.Gid != 5678 {
		t.Fatalf("credential = %+v", credential)
	}
	if credential.Groups == nil || len(credential.Groups) != 0 {
		t.Fatalf("supplementary groups = %#v, want explicit empty list", credential.Groups)
	}
}

func TestOwnerSnapshotWorkspaceRejectsNonStickyRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("owner snapshot workspace root test requires root")
	}
	root, err := os.MkdirTemp("/tmp", ".celikpanel-owner-workspace-unsafe-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(root)
	})
	workspace, err := createOwnerSnapshotWorkspace(root, 65534, 65534)
	if err == nil {
		_ = workspace.remove()
		t.Fatal("non-sticky workspace root unexpectedly accepted")
	}
}

func TestOwnerProcessSnapshotUsesParentWorkspaceAndCleansIt(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("owner snapshot integration test requires a root test parent")
	}
	const writerID = uint32(65534)
	sourceRoot, err := os.MkdirTemp("/tmp", ".celikpanel-owner-snapshot-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sourceRoot) })
	databasePath := filepath.Join(sourceRoot, "owner.sqlite3")
	database := createTestSQLite(t, databasePath, "WAL")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(databasePath, int(writerID), int(writerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(sourceRoot, int(writerID), int(writerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabasePowerDNS, databasePath, true)
	workspaceRoot := createOwnerWorkspaceRootForTest(t)
	operations := &ownerProcessMutableOperations{
		definitions:   map[string]Definition{definition.ID: definition},
		workspaceRoot: workspaceRoot,
		newCommand: func(ctx context.Context, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "/proc/self/exe", "-test.run=TestOwnerWorkerDroppedProcessHelper")
		},
		extraEnv: []string{
			ownerWorkerHelperEnvironment + "=1",
			ownerWorkerSnapshotEnvironment + "=1",
			"CELIKPANEL_SQLITE_OWNER_WORKER_TEST_DB=" + databasePath,
		},
	}
	destinationPath := filepath.Join(t.TempDir(), "snapshot.sqlite3")
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operations.run(
		context.Background(),
		OwnerWorkerSnapshot,
		definition,
		destination,
		SnapshotLimits{MaxBytes: 1 << 20, FreeSpaceFloor: 4096},
	); err != nil {
		_ = destination.Close()
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace entries after successful snapshot = %v", entries)
	}
	snapshotSource, err := openManagedSource(destinationPath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotSource.close()
	snapshotDatabase, err := openSQLite(context.Background(), snapshotSource.databasePath(), "ro")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotDatabase.Close()
	var integrity string
	if err := snapshotDatabase.QueryRow("PRAGMA quick_check(1)").Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("snapshot quick_check = %q", integrity)
	}
}

func TestOwnerSnapshotWorkspaceIsRemovedAfterCanceledAndRepeatedFailures(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("owner snapshot workspace lifecycle test requires root")
	}
	const writerID = uint32(65534)
	databasePath := filepath.Join(t.TempDir(), "owner.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(databasePath, int(writerID), int(writerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabasePowerDNS, databasePath, true)
	workspaceRoot := createOwnerWorkspaceRootForTest(t)
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	contextCanceled := false
	operations := &ownerProcessMutableOperations{
		definitions:   map[string]Definition{definition.ID: definition},
		workspaceRoot: workspaceRoot,
		runCommand: func(command *exec.Cmd) error {
			if len(command.ExtraFiles) != 2 {
				return errors.New("test worker did not receive both snapshot descriptors")
			}
			rootEntries, err := os.ReadDir(workspaceRoot)
			if err != nil {
				return err
			}
			if len(rootEntries) != 1 {
				return errors.New("test workspace root does not contain exactly one outer directory")
			}
			outerInfo, err := rootEntries[0].Info()
			if err != nil {
				return err
			}
			outerStat, ok := outerInfo.Sys().(*syscall.Stat_t)
			if !ok || !outerInfo.IsDir() || outerInfo.Mode().Perm() != 0o710 ||
				outerStat.Uid != 0 || outerStat.Gid != writerID {
				return errors.New("outer snapshot workspace has unexpected ownership or mode")
			}
			stageInfo, err := os.Lstat(filepath.Join(workspaceRoot, rootEntries[0].Name(), ownerSnapshotStageName))
			if err != nil {
				return err
			}
			stageStat, ok := stageInfo.Sys().(*syscall.Stat_t)
			if !ok || !stageInfo.IsDir() || stageInfo.Mode().Perm() != 0o700 ||
				stageStat.Uid != writerID || stageStat.Gid != writerID {
				return errors.New("snapshot stage has unexpected ownership or mode")
			}
			workspaceFD := int(command.ExtraFiles[1].Fd())
			artifactFD, err := unix.Openat(
				workspaceFD,
				"snapshot.sqlite3",
				unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0o600,
			)
			if err != nil {
				return err
			}
			if _, err := unix.Write(artifactFD, []byte("partial")); err != nil {
				_ = unix.Close(artifactFD)
				return err
			}
			if err := unix.Close(artifactFD); err != nil {
				return err
			}
			if err := unix.Symlinkat(sentinel, workspaceFD, "snapshot.sqlite3-wal"); err != nil {
				return err
			}
			if err := unix.Mkdirat(workspaceFD, "nested", 0o700); err != nil {
				return err
			}
			nestedFD, err := unix.Openat(
				workspaceFD,
				"nested",
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0,
			)
			if err != nil {
				return err
			}
			nestedArtifactFD, err := unix.Openat(
				nestedFD,
				"partial",
				unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0o600,
			)
			if err != nil {
				_ = unix.Close(nestedFD)
				return err
			}
			if err := unix.Close(nestedArtifactFD); err != nil {
				_ = unix.Close(nestedFD)
				return err
			}
			if err := unix.Close(nestedFD); err != nil {
				return err
			}
			if contextCanceled {
				return context.Canceled
			}
			return errors.New("forced worker failure")
		},
	}
	limits := SnapshotLimits{MaxBytes: 1 << 20, FreeSpaceFloor: 4096}

	for attempt := 0; attempt < 3; attempt++ {
		destinationPath := filepath.Join(t.TempDir(), "snapshot.sqlite3")
		destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := operations.run(
			context.Background(), OwnerWorkerSnapshot, definition, destination, limits,
		); err == nil {
			_ = destination.Close()
			t.Fatal("forced owner worker failure unexpectedly succeeded")
		}
		if err := destination.Close(); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(workspaceRoot)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("workspace entries after failure %d = %v", attempt, entries)
		}
	}

	contextCanceled = true
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	destinationPath := filepath.Join(t.TempDir(), "snapshot.sqlite3")
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operations.run(
		canceledContext, OwnerWorkerSnapshot, definition, destination, limits,
	); err == nil {
		_ = destination.Close()
		t.Fatal("canceled owner worker unexpectedly succeeded")
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace entries after cancellation = %v", entries)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("cleanup followed a workspace symlink: %q, %v", data, err)
	}
}

func TestOwnerProcessOperationsRejectRootOwnedMutableDatabase(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned database policy test requires root")
	}
	path := filepath.Join(t.TempDir(), "root.sqlite3")
	database := createTestSQLite(t, path, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabasePowerDNS, path, true)
	operations, err := NewOwnerProcessMutableOperations(
		[]Definition{definition},
		filepath.Join(t.TempDir(), "snapshots"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operations.Check(context.Background(), definition); err == nil {
		t.Fatal("root-owned mutable database was accepted")
	}
}

func TestManagedSourceWriterIdentityAllowsExplicitGroupWriter(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("explicit group writer policy test requires root")
	}
	const writerID = uint32(65534)
	path := filepath.Join(t.TempDir(), "root-group.sqlite3")
	database := createTestSQLite(t, path, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, int(writerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabaseRoundcube, path, true)
	definition.WriterUID = writerID
	definition.WriterGID = writerID
	definition.WriterIdentitySet = true

	uid, gid, err := managedSourceWriterIdentity(definition)
	if err != nil || uid != writerID || gid != writerID {
		t.Fatalf("managedSourceWriterIdentity() = %d, %d, %v", uid, gid, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := managedSourceWriterIdentity(definition); err == nil {
		t.Fatal("explicit group writer was accepted without group read-write permission")
	}
}

func TestOwnerWorkerChecksRootOwnedGroupWritableDatabase(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("explicit group writer transition test requires a root test parent")
	}
	const writerID = uint32(65534)
	root, err := os.MkdirTemp("/tmp", ".celikpanel-owner-worker-group-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	path := filepath.Join(root, "roundcube.sqlite3")
	database := createTestSQLite(t, path, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, int(writerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 0, int(writerID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o770); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("/proc/self/exe", "-test.run=TestOwnerWorkerDroppedProcessHelper")
	command.Env = []string{
		ownerWorkerHelperEnvironment + "=1",
		ownerWorkerExplicitWriterEnvironment + "=1",
		ownerWorkerExpectLimitsEnvironment + "=1",
		"CELIKPANEL_SQLITE_OWNER_WORKER_TEST_DB=" + path,
	}
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: ownerWorkerCredential(writerID, writerID),
		Pdeathsig:  syscall.SIGKILL,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("explicit group owner worker failed: %v: %s", err, stderr.String())
	}
	var response OwnerWorkerResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("worker response %q: %v", stdout.String(), err)
	}
	if !response.Success || !response.Check.IntegrityOK || response.Error != "" {
		t.Fatalf("worker response = %+v", response)
	}
}
