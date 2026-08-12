//go:build linux

package systemsqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	ownerWorkerAddressSpaceBytes = uint64(4 << 30)
	ownerWorkerCPUSeconds        = uint64(120)
	ownerWorkerOpenFiles         = uint64(64)
)

type ownerWorkerResourceLimit struct {
	resource int
	ceiling  uint64
}

type ownerProcessMutableOperations struct {
	definitions   map[string]Definition
	workspaceRoot string
	runCommand    func(*exec.Cmd) error
	newCommand    func(context.Context, ...string) *exec.Cmd
	extraEnv      []string
}

// NewOwnerProcessMutableOperations creates the Linux process boundary used by the root agent.
// NewOwnerProcessMutableOperations, root ajanın kullandığı Linux süreç sınırını oluşturur.
func NewOwnerProcessMutableOperations(
	definitions []Definition,
	workspaceRoot string,
) (MutableOperations, error) {
	if os.Geteuid() != 0 {
		return unavailableMutableOperations{}, errors.New("owner-isolated SQLite operations require a root parent")
	}
	cleanWorkspaceRoot := filepath.Clean(workspaceRoot)
	if cleanWorkspaceRoot == "." || !filepath.IsAbs(cleanWorkspaceRoot) ||
		filepath.Dir(cleanWorkspaceRoot) == cleanWorkspaceRoot {
		return unavailableMutableOperations{}, errors.New("owner-isolated SQLite snapshot root is unsafe")
	}
	known := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		if !knownDatabaseID(definition.ID) {
			return unavailableMutableOperations{}, errors.New("unknown system SQLite database")
		}
		if _, exists := known[definition.ID]; exists {
			return unavailableMutableOperations{}, errors.New("duplicate system SQLite database")
		}
		if definition.Mutable {
			if err := validateWriterIdentityDefinition(definition); err != nil {
				return unavailableMutableOperations{}, err
			}
		}
		known[definition.ID] = definition
	}
	return &ownerProcessMutableOperations{
		definitions:   known,
		workspaceRoot: cleanWorkspaceRoot,
	}, nil
}

func (operations *ownerProcessMutableOperations) Inspect(
	ctx context.Context,
	definition Definition,
) (MutableInspection, error) {
	response, err := operations.run(ctx, OwnerWorkerInspect, definition, nil, SnapshotLimits{})
	if err != nil {
		return MutableInspection{}, err
	}
	return response.Inspection, nil
}

func (operations *ownerProcessMutableOperations) Check(
	ctx context.Context,
	definition Definition,
) (CheckResult, error) {
	response, err := operations.run(ctx, OwnerWorkerCheck, definition, nil, SnapshotLimits{})
	if err != nil {
		return CheckResult{}, err
	}
	return response.Check, nil
}

func (operations *ownerProcessMutableOperations) Snapshot(
	ctx context.Context,
	definition Definition,
	destination *os.File,
	limits SnapshotLimits,
) error {
	_, err := operations.run(ctx, OwnerWorkerSnapshot, definition, destination, limits)
	return err
}

func (operations *ownerProcessMutableOperations) Optimize(
	ctx context.Context,
	definition Definition,
) error {
	_, err := operations.run(ctx, OwnerWorkerOptimize, definition, nil, SnapshotLimits{})
	return err
}

func (operations *ownerProcessMutableOperations) run(
	ctx context.Context,
	action string,
	definition Definition,
	destination *os.File,
	limits SnapshotLimits,
) (result OwnerWorkerResponse, resultErr error) {
	expected, exists := operations.definitions[definition.ID]
	if !exists || expected != definition || !definition.Mutable {
		return OwnerWorkerResponse{}, errors.New("isolated SQLite definition mismatch")
	}
	uid, gid, err := managedSourceWriterIdentity(definition)
	if err != nil {
		return OwnerWorkerResponse{}, publicDatabaseError(err)
	}
	if uid == 0 || gid == 0 {
		return OwnerWorkerResponse{}, errors.New("isolated SQLite writer identity must be non-root")
	}

	args := []string{"--system-sqlite-owner-worker", action, definition.ID}
	var workspace *ownerSnapshotWorkspace
	if action == OwnerWorkerSnapshot {
		if destination == nil || limits.validate() != nil {
			return OwnerWorkerResponse{}, errors.New("invalid isolated SQLite snapshot request")
		}
		args = append(
			args,
			strconv.FormatInt(limits.MaxBytes, 10),
			strconv.FormatInt(limits.FreeSpaceFloor, 10),
		)
		workspace, err = createOwnerSnapshotWorkspace(operations.workspaceRoot, uid, gid)
		if err != nil {
			return OwnerWorkerResponse{}, err
		}
		defer func() {
			if cleanupErr := workspace.remove(); cleanupErr != nil {
				result = OwnerWorkerResponse{}
				resultErr = errors.New("could not clean isolated SQLite workspace")
			}
		}()
	}
	newCommand := operations.newCommand
	if newCommand == nil {
		newCommand = func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "/proc/self/exe", args...)
		}
	}
	command := newCommand(ctx, args...)
	command.Env = append(ownerWorkerEnvironment(), operations.extraEnv...)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: ownerWorkerCredential(uid, gid),
		Pdeathsig:  syscall.SIGKILL,
	}
	if destination != nil {
		command.ExtraFiles = []*os.File{destination, workspace.directory}
	}
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	runCommand := operations.runCommand
	if runCommand == nil {
		runCommand = func(command *exec.Cmd) error { return command.Run() }
	}
	if err := runCommand(command); err != nil {
		return OwnerWorkerResponse{}, errors.New("owner-isolated SQLite worker failed")
	}
	if stdout.Len() == 0 || stdout.Len() > 64*1024 {
		return OwnerWorkerResponse{}, errors.New("owner-isolated SQLite worker returned an invalid response")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var response OwnerWorkerResponse
	if err := decoder.Decode(&response); err != nil {
		return OwnerWorkerResponse{}, errors.New("owner-isolated SQLite worker returned an invalid response")
	}
	if err := requireJSONEnd(decoder); err != nil {
		return OwnerWorkerResponse{}, errors.New("owner-isolated SQLite worker returned an invalid response")
	}
	if !response.Success || response.Error != "" {
		if response.Error == "" {
			return OwnerWorkerResponse{}, errors.New("owner-isolated SQLite worker refused the operation")
		}
		return OwnerWorkerResponse{}, errors.New(response.Error)
	}
	return response, nil
}

func ownerWorkerCredential(uid, gid uint32) *syscall.Credential {
	return &syscall.Credential{
		Uid: uid, Gid: gid, Groups: []uint32{}, NoSetGroups: false,
	}
}

func requireJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func ownerWorkerEnvironment() []string {
	environment := []string{
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"TMPDIR=/tmp",
	}
	for _, name := range []string{
		"CELIKPANEL_PANEL_DB",
		"CELIKPANEL_DATA_DIR",
		"CELIKPANEL_PDNS_DB",
		"CELIKPANEL_COMPONENT_CATALOG",
	} {
		if value, exists := os.LookupEnv(name); exists {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func managedSourceWriterIdentity(definition Definition) (uint32, uint32, error) {
	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		return 0, 0, err
	}
	defer source.close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(source.file.Fd()), &stat); err != nil {
		return 0, 0, err
	}
	uid, gid, err := writerIdentityForManagedStat(definition, &stat)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, source.verifyIdentity()
}

func validateOwnerWorkerProcess() error {
	if os.Geteuid() == 0 || os.Getuid() != os.Geteuid() {
		return errors.New("isolated SQLite worker did not drop privileges")
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 {
		return errors.New("isolated SQLite worker retained supplementary groups")
	}
	return applyOwnerWorkerResourceLimits()
}

func verifyOwnerWorkerSource(source *managedSource, definition Definition) error {
	if err := validateOwnerWorkerProcess(); err != nil {
		return err
	}
	if source == nil || source.file == nil {
		return errors.New("managed database is not pinned")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(source.file.Fd()), &stat); err != nil {
		return err
	}
	uid, gid, err := writerIdentityForManagedStat(definition, &stat)
	if err != nil {
		return err
	}
	if uid != uint32(os.Geteuid()) || gid != uint32(os.Getegid()) {
		return errors.New("isolated SQLite worker does not match the database writer")
	}
	return nil
}

func writerIdentityForManagedStat(
	definition Definition,
	stat *unix.Stat_t,
) (uint32, uint32, error) {
	if stat == nil {
		return 0, 0, errors.New("managed database identity is unavailable")
	}
	if err := validateWriterIdentityDefinition(definition); err != nil {
		return 0, 0, err
	}
	if stat.Mode&0o002 != 0 {
		return 0, 0, errors.New("managed database is world-writable")
	}
	if definition.WriterIdentitySet {
		if stat.Uid == definition.WriterUID {
			if stat.Mode&0o600 != 0o600 {
				return 0, 0, errors.New("managed database owner cannot write safely")
			}
			return definition.WriterUID, definition.WriterGID, nil
		}
		if stat.Gid != definition.WriterGID || stat.Mode&0o060 != 0o060 {
			return 0, 0, errors.New("managed database does not grant the explicit writer group read-write access")
		}
		return definition.WriterUID, definition.WriterGID, nil
	}
	if stat.Uid == 0 || stat.Gid == 0 {
		return 0, 0, errors.New("root-owned mutable SQLite database has no explicit writer identity")
	}
	if stat.Mode&0o600 != 0o600 {
		return 0, 0, errors.New("managed database owner cannot write safely")
	}
	return stat.Uid, stat.Gid, nil
}

func ownerWorkerResourceLimits() []ownerWorkerResourceLimit {
	return []ownerWorkerResourceLimit{
		{resource: unix.RLIMIT_CORE, ceiling: 0},
		{resource: unix.RLIMIT_FSIZE, ceiling: uint64(maxOwnerWorkerSnapshotBytes)},
		{resource: unix.RLIMIT_AS, ceiling: ownerWorkerAddressSpaceBytes},
		{resource: unix.RLIMIT_CPU, ceiling: ownerWorkerCPUSeconds},
		{resource: unix.RLIMIT_NOFILE, ceiling: ownerWorkerOpenFiles},
	}
}

func applyOwnerWorkerResourceLimits() error {
	for _, policy := range ownerWorkerResourceLimits() {
		var inherited unix.Rlimit
		if err := unix.Getrlimit(policy.resource, &inherited); err != nil {
			return errors.New("could not read isolated SQLite worker resource limits")
		}
		target := inherited
		if target.Cur > policy.ceiling {
			target.Cur = policy.ceiling
		}
		if target.Max > policy.ceiling {
			target.Max = policy.ceiling
		}
		if target.Cur > target.Max {
			target.Cur = target.Max
		}
		if err := unix.Setrlimit(policy.resource, &target); err != nil {
			return errors.New("could not enforce isolated SQLite worker resource limits")
		}
	}
	return verifyOwnerWorkerResourceLimits()
}

func verifyOwnerWorkerResourceLimits() error {
	for _, policy := range ownerWorkerResourceLimits() {
		var current unix.Rlimit
		if err := unix.Getrlimit(policy.resource, &current); err != nil {
			return errors.New("could not verify isolated SQLite worker resource limits")
		}
		if current.Cur > policy.ceiling || current.Max > policy.ceiling {
			return errors.New("isolated SQLite worker resource limit is too broad")
		}
	}
	return nil
}

func validateOwnerWorkerDestination(destination *os.File) error {
	if destination == nil {
		return errors.New("isolated snapshot destination is missing")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(destination.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
		stat.Uid != 0 || stat.Mode&0o777 != 0o600 || stat.Size != 0 {
		return errors.New("isolated snapshot destination is unsafe")
	}
	flags, err := unix.FcntlInt(destination.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE == unix.O_RDONLY {
		return errors.New("isolated snapshot destination is not writable")
	}
	return nil
}
