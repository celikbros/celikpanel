//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/pdnsvendor"
	"github.com/alicelik/celikpanel/internal/processidentity"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// The independent observer uses installed, fixed host paths. No environment
// override or journal-supplied path can redirect its evidence reads.
func installedDNSJournalPolicy(gid uint32) dnsengineartifact.JournalPolicy {
	return dnsengineartifact.JournalPolicy{
		StatePath: filepath.Join(hostingpath.ServiceMutationStateRoot(), "dns-engine-state.json"),
		StateUID:  0, StateGID: gid, RequireOwner: true,
		PDNSMainPath:     "/etc/powerdns/pdns.conf",
		PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
		PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
}

func localCelikPanelGroupID(path string) (uint32, error) {
	return localServiceGroupID(path, "celikpanel")
}

func localServiceGroupID(path, group string) (uint32, error) {
	// Local-only lookup keeps DNS recovery observation independent of NSS
	// services that may themselves be unavailable during a DNS incident.
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o022 != 0 {
		return 0, errors.New("local group file is absent or untrusted")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return 0, errors.New("local group file is absent or untrusted")
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return 0, errors.New("local group file changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return 0, errors.New("local group file cannot be read within its size limit")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) || after.ModTime() != opened.ModTime() || after.Size() != opened.Size() {
		return 0, errors.New("local group file changed while reading")
	}
	var found bool
	var gid uint32
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Split(line, ":")
		if fields[0] != group {
			continue
		}
		if len(fields) != 4 || (group != "celikpanel" && (fields[1] != "x" || fields[3] != "")) {
			return 0, errors.New("local service group record is unsafe")
		}
		number, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || number == 0 || number > uint64(1<<31-1) || strconv.FormatUint(number, 10) != fields[2] || found {
			return 0, errors.New("local service group identity is ambiguous")
		}
		gid, found = uint32(number), true
	}
	if !found {
		return 0, errors.New("local service group is absent")
	}
	return gid, nil
}

// This is a directory and package provenance observation only. It does not
// establish which immutable generation named loaded or authorize an inverse.
func installedBINDLayout() (bindroot.Layout, uint32, error) {
	profile, err := hostplatform.Detect()
	if err != nil {
		return "", 0, fmt.Errorf("detect installed host profile: %w", err)
	}
	var layout bindroot.Layout
	var group string
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		layout, group = bindroot.APT, "bind"
	case hostplatform.PackageManagerPacman:
		layout, group = bindroot.Pacman, "named"
	default:
		return "", 0, errors.New("this package family has no certified managed BIND root")
	}
	gid, err := localServiceGroupID("/etc/group", group)
	if err != nil {
		return "", 0, fmt.Errorf("verify local BIND service group: %w", err)
	}
	return layout, gid, nil
}

func verifyInstalledBINDVendorAndUnit(ctx context.Context) error {
	profile, err := hostplatform.Detect()
	if err != nil {
		return fmt.Errorf("detect installed host profile: %w", err)
	}
	before, err := bindroot.InspectInstalledVendor(ctx, profile)
	if err != nil {
		return err
	}
	if _, err := dnsenginerecovery.ProbeBINDVendorIdentity(ctx, profile, dnsenginerecovery.SystemdBINDIdentityRunner); err != nil {
		return err
	}
	after, err := bindroot.InspectInstalledVendor(ctx, profile)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("BIND vendor files changed around the systemd identity observation")
	}
	return nil
}

// A selected BIND target requires stable vendor and loaded systemd identity
// around a read-only process observation.
func verifyInstalledBINDRuntime(ctx context.Context) (uint64, error) {
	profile, err := hostplatform.Detect()
	if err != nil {
		return 0, fmt.Errorf("detect installed host profile: %w", err)
	}
	beforeVendor, err := bindroot.InspectInstalledVendor(ctx, profile)
	if err != nil {
		return 0, err
	}
	beforeIdentity, err := dnsenginerecovery.ProbeBINDVendorIdentity(ctx, profile, dnsenginerecovery.SystemdBINDIdentityRunner)
	if err != nil {
		return 0, err
	}
	beforeRuntime, err := dnsenginerecovery.ProbeBINDVendorRuntime(ctx, profile, dnsenginerecovery.SystemdBINDRuntimeRunner)
	if err != nil {
		return 0, err
	}
	beforeStarted, err := verifyNativeBINDExecutable(beforeRuntime.MainPID, beforeIdentity.ExecStartPath)
	if err != nil {
		return 0, err
	}
	afterRuntime, err := dnsenginerecovery.ProbeBINDVendorRuntime(ctx, profile, dnsenginerecovery.SystemdBINDRuntimeRunner)
	if err != nil {
		return 0, err
	}
	afterStarted, err := verifyNativeBINDExecutable(afterRuntime.MainPID, beforeIdentity.ExecStartPath)
	if err != nil {
		return 0, err
	}
	afterIdentity, err := dnsenginerecovery.ProbeBINDVendorIdentity(ctx, profile, dnsenginerecovery.SystemdBINDIdentityRunner)
	if err != nil {
		return 0, err
	}
	afterVendor, err := bindroot.InspectInstalledVendor(ctx, profile)
	if err != nil {
		return 0, err
	}
	if beforeVendor != afterVendor || !reflect.DeepEqual(beforeIdentity, afterIdentity) ||
		beforeRuntime != afterRuntime || beforeStarted != afterStarted {
		return 0, errors.New("BIND vendor, loaded unit or process identity changed around the runtime observation")
	}
	return beforeRuntime.MainPID, nil
}

func verifyInstalledPDNSRuntime(ctx context.Context) (uint64, error) {
	profile, err := hostplatform.Detect()
	if err != nil {
		return 0, fmt.Errorf("detect installed host profile: %w", err)
	}
	beforeVendor, err := pdnsvendor.InspectInstalledUnit(ctx, profile)
	if err != nil {
		return 0, err
	}
	beforeIdentity, err := dnsenginerecovery.ProbePDNSVendorIdentity(ctx, profile, dnsenginerecovery.SystemdPDNSIdentityRunner)
	if err != nil {
		return 0, err
	}
	before, err := dnsenginerecovery.ProbePDNSRuntime(ctx, profile, dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdPDNSRuntimeRunner)
	if err != nil {
		return 0, err
	}
	if uint64(int(before)) != before {
		return 0, errors.New("PowerDNS MainPID exceeds native process range")
	}
	started, err := verifyRunningExecutable(int(before), beforeIdentity.ExecStartPath)
	if err != nil {
		return 0, err
	}
	after, err := dnsenginerecovery.ProbePDNSRuntime(ctx, profile, dnsenginerecovery.SystemdUnitRunner, dnsenginerecovery.SystemdPDNSRuntimeRunner)
	if err != nil {
		return 0, err
	}
	if uint64(int(after)) != after {
		return 0, errors.New("PowerDNS MainPID exceeds native process range")
	}
	again, err := verifyRunningExecutable(int(after), beforeIdentity.ExecStartPath)
	if err != nil {
		return 0, err
	}
	afterIdentity, err := dnsenginerecovery.ProbePDNSVendorIdentity(ctx, profile, dnsenginerecovery.SystemdPDNSIdentityRunner)
	if err != nil {
		return 0, err
	}
	afterVendor, err := pdnsvendor.InspectInstalledUnit(ctx, profile)
	if err != nil {
		return 0, err
	}
	if before != after || started != again || beforeVendor != afterVendor || !reflect.DeepEqual(beforeIdentity, afterIdentity) {
		return 0, errors.New("PowerDNS native process identity changed during observation")
	}
	return before, nil
}

// verifyNativeBINDExecutable ties systemd's PID to the current native file
// inode and Linux process start token. It does not certify package bytes.
func verifyNativeBINDExecutable(pid uint64, path string) (string, error) {
	if pid == 0 || uint64(int(pid)) != pid ||
		(path != "/usr/sbin/named" && path != "/usr/bin/named") {
		return "", errors.New("invalid BIND process or executable identity")
	}
	return verifyRunningExecutable(int(pid), path)
}

func verifyRunningExecutable(pid int, path string) (string, error) {
	started, err := processidentity.StartToken(pid)
	if err != nil {
		return "", fmt.Errorf("read native DNS process start identity: %w", err)
	}
	installed, err := os.Lstat(path)
	if err != nil || !installed.Mode().IsRegular() {
		return "", errors.New("installed BIND executable is absent or is not a regular file")
	}
	running, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || !os.SameFile(installed, running) {
		return "", errors.New("running native DNS executable differs from its installed file")
	}
	again, err := processidentity.StartToken(pid)
	if err != nil || started != again {
		return "", errors.New("native DNS process changed while its executable was inspected")
	}
	return started, nil
}

// A disk-level check of the exact managed zone include. It does not prove
// that named loaded this anchor or the selected generation.
func verifyInstalledBINDConfig(ctx context.Context, layout bindroot.Layout, serviceGID uint32) error {
	if ctx == nil {
		return errors.New("BIND config observation requires a context")
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open BIND config root: %w", err)
	}
	defer unix.Close(rootFD)
	before, err := observeBINDConfigAt(rootFD, layout, serviceGID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	after, err := observeBINDConfigAt(rootFD, layout, serviceGID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("BIND native config changed during exact observation")
	}
	return nil
}

func observeBINDConfigAt(rootFD int, layout bindroot.Layout, serviceGID uint32) ([]bindroot.FileIdentity, error) {
	var paths []string
	var includePath string
	switch layout {
	case bindroot.APT:
		paths = []string{"/etc/bind/named.conf", "/etc/bind/named.conf.options", "/etc/bind/named.conf.local"}
		includePath = "/var/cache/bind/celikpanel/current/zones.conf"
	case bindroot.Pacman:
		paths = []string{"/etc/named.conf"}
		includePath = "/var/named/celikpanel/current/zones.conf"
	default:
		return nil, errors.New("unsupported native BIND config layout")
	}
	identities := make([]bindroot.FileIdentity, 0, len(paths))
	for _, path := range paths {
		raw, identity, err := bindroot.ReadExactBINDConfigAt(rootFD, layout, serviceGID, path)
		if err != nil {
			return nil, err
		}
		if layout == bindroot.APT && path == "/etc/bind/named.conf" {
			if err := bindconfig.VerifyMainIncludes(string(raw)); err != nil {
				return nil, fmt.Errorf("verify APT BIND main includes: %w", err)
			}
		}
		if path == paths[len(paths)-1] {
			if err := bindconfig.VerifyExactZoneInclude(string(raw), includePath); err != nil {
				return nil, fmt.Errorf("verify managed BIND zone include: %w", err)
			}
		}
		identities = append(identities, identity)
	}
	if len(identities) == 3 && identities[1].GID != identities[2].GID {
		return nil, errors.New("APT BIND config files have different owners")
	}
	return identities, nil
}
func verifyInstalledBINDRoot(ctx context.Context) error {
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return err
	}
	return bindroot.VerifyInstalled(ctx, layout, gid)
}

// A matching selected generation is a stronger observation than a state
// receipt, but remains distinct from the daemon's loaded configuration and
// authoritative answers. No mutation or daemon reload is performed here.
func verifySelectedBINDTarget(ctx context.Context, generation string, epoch int64) (binddns.Receipt, error) {
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return binddns.Receipt{}, err
	}
	before, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return binddns.Receipt{}, err
	}
	publisher, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return binddns.Receipt{}, err
	}
	first, err := publisher.LoadCurrent()
	if err != nil {
		return binddns.Receipt{}, fmt.Errorf("read selected managed BIND generation: %w", err)
	}
	receipt := first.CurrentReceipt()
	if receipt.Generation != generation || receipt.EngineEpoch != epoch {
		return binddns.Receipt{}, errors.New("selected managed BIND generation differs from the frozen target")
	}
	second, err := publisher.LoadCurrent()
	if err != nil {
		return binddns.Receipt{}, fmt.Errorf("reread selected managed BIND generation: %w", err)
	}
	after, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return binddns.Receipt{}, err
	}
	if before != after || !reflect.DeepEqual(receipt, second.CurrentReceipt()) {
		return binddns.Receipt{}, errors.New("selected managed BIND generation changed during observation")
	}
	return receipt, nil
}

type dnsObservationLocks struct {
	release *os.File
	host    *os.File
}

func (locks *dnsObservationLocks) Close() {
	if locks == nil {
		return
	}
	if locks.host != nil {
		_ = locks.host.Close()
	}
	if locks.release != nil {
		_ = locks.release.Close()
	}
}

func acquireDNSObservationLocks(releasePath, hostPath string, hostOwner hostmutationlock.Owner) (*dnsObservationLocks, error) {
	release, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		return nil, fmt.Errorf("release lock: %w", err)
	}
	locks := &dnsObservationLocks{release: release}
	host, err := hostmutationlock.AcquireExisting(hostPath, hostOwner)
	if err != nil {
		locks.Close()
		return nil, fmt.Errorf("host lock: %w", err)
	}
	locks.host = host
	return locks, nil
}

// A terminal retained inverse may have restored an originally inactive target.
// This is a read-only diagnostic selection from already validated evidence;
// it never grants permission to retire the journal or alter native DNS.
func rolledBackInactiveTargetUnit(evidence dnsenginerecovery.SwitchEvidence) (string, bool) {
	if evidence.Observation.Status != dnsenginerecovery.EvidenceTerminalRolledBack ||
		evidence.Journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		return "", false
	}
	var name string
	switch evidence.Observation.InverseKind {
	case dnsenginerecovery.NativeInverseBINDSwitch:
		name = "named.service"
	case dnsenginerecovery.NativeInversePDNSSwitch:
		name = "pdns.service"
	default:
		return "", false
	}
	for _, unit := range evidence.Journal.TargetUnitsBefore {
		if unit.Name == name && unit.ActiveState == "inactive" {
			return name, true
		}
	}
	return "", false
}

func runDNSSwitchStatus(args []string, uid int, out, diagnostic io.Writer) int {
	quiesced, requestID, validArgs := parseDNSSwitchStatusArgs(args)
	if !validArgs {
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, "Owner authentication is required. Use a root or authorized sudo session.")
		return exitNotOwner
	}
	groupID, groupErr := localCelikPanelGroupID("/etc/group")
	if groupErr != nil {
		fmt.Fprintln(diagnostic, "The installed CelikPanel group could not be verified from local /etc/group. The server owner must inspect that file before recovery observation can resume. "+groupErr.Error())
		return exitUnavailable
	}
	observationCtx, cancelObservation := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelObservation()
	root := hostingpath.ServiceMutationStateRoot()
	owner := servicemutationledger.FileOwner{UID: 0, GID: groupID}
	if quiesced {
		locks, lockErr := acquireDNSObservationLocks(
			"/var/lib/celikpanel-release-transaction/transaction.lock",
			"/run/celikpanel/service-mutation.lock",
			hostmutationlock.Owner{UID: owner.UID, GID: owner.GID},
		)
		if lockErr != nil {
			fmt.Fprintln(diagnostic, "A quiesced DNS observation cannot acquire the release and host locks. The server owner should follow the existing update or mutation, then retry this observation; no DNS operation was started. "+lockErr.Error())
			return exitUnavailable
		}
		defer locks.Close()
	}
	policy := installedDNSJournalPolicy(owner.GID)
	evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
	observation := evidence.Observation
	if err != nil {
		fmt.Fprintln(diagnostic, "DNS switch evidence could not be verified. Preserve the private journal and ledger; the server owner must inspect their ownership, native DNS state and compatibility before the same operation resumes. "+err.Error())
		return exitUnavailable
	}
	if observationCtx.Err() != nil {
		fmt.Fprintln(diagnostic, "DNS observation exceeded its deadline. Preserve the same operation and retry after native service responsiveness is restored; no DNS operation was started.")
		return exitUnavailable
	}
	if !present {
		if requestID != "" {
			status, readErr := readJournalAbsentDNSJob(observationCtx, root, owner, requestID)
			if readErr != nil {
				fmt.Fprintln(diagnostic, "The exact DNS request could not be verified without its journal. Preserve the private evidence and inspect the original operation and native DNS service; no DNS operation was started. "+readErr.Error())
				return exitUnavailable
			}
			fmt.Fprintf(out, "No DNS switch journal was observed for request %s. Its exact ledger job records status %s. This is a point-in-time ledger observation, not proof of DNS rollback, completion or current service health. Inspect the native DNS service and this same operation before any new switch.\n", requestID, status)
			return exitOK
		}
		fmt.Fprintln(out, "No DNS switch journal was observed. This does not prove historical completion or current DNS health. The server owner should inspect the native DNS service and the panel's operation status before starting another switch.")
		return exitOK
	}
	if requestID != "" && evidence.Journal.MutationRequestID != requestID {
		fmt.Fprintln(diagnostic, "A different DNS switch journal is present. Preserve its exact operation and inspect it before continuing; no DNS operation was started.")
		return exitUnavailable
	}
	units, unitErr := dnsenginerecovery.ProbeNativeUnits(observationCtx, observation.NativeUnits, dnsenginerecovery.SystemdUnitRunner)
	if quiesced && unitErr == nil {
		againEvidence, stillPresent, readErr := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
		again := againEvidence.Observation
		if readErr != nil || !stillPresent {
			fmt.Fprintln(diagnostic, "DNS switch evidence changed or became unreadable during the quiesced observation. The server owner should inspect the existing operation and native DNS service, then retry after owner changes settle; no DNS operation was started.")
			return exitUnavailable
		}
		againUnits, probeErr := dnsenginerecovery.ProbeNativeUnits(observationCtx, again.NativeUnits, dnsenginerecovery.SystemdUnitRunner)
		if probeErr != nil || !reflect.DeepEqual(evidence.Journal, againEvidence.Journal) ||
			!dnsenginerecovery.StableQuiescedObservation(observation, again, units, againUnits) {
			fmt.Fprintln(diagnostic, "DNS switch evidence or native DNS unit properties changed during the quiesced observation. The server owner should inspect the existing operation and native DNS service, then retry after owner changes settle; no DNS operation was started.")
			return exitUnavailable
		}
		evidence = againEvidence
	}
	fmt.Fprintf(out, "DNS switch request %s: %s (journal phase %s).\n", observation.RequestID, observation.Status, observation.Phase)
	if quiesced {
		switch observation.Status {
		case dnsenginerecovery.EvidenceActive,
			dnsenginerecovery.EvidenceLeaseExpired,
			dnsenginerecovery.EvidenceWorkerRecorded,
			dnsenginerecovery.EvidenceOrphanedWorker,
			dnsenginerecovery.EvidenceExpiredCancellation:
			id := dnsengineartifact.SwitchIdentity{
				RequestID: evidence.Journal.MutationRequestID,
				OwnerID:   evidence.Journal.MutationOwnerID,
				Target:    evidence.Journal.TargetEngine,
				Qualifier: evidence.Journal.ManifestQualifier,
			}
			worker, workerErr := dnsenginerecovery.InspectAcceptedWorker(
				id, &evidence.AcceptedJob, time.Now().UTC(),
			)
			if workerErr != nil {
				fmt.Fprintln(diagnostic, "The exact DNS switch worker could not be excluded under the release and host locks. Preserve the operation and inspect its native process before recovery; no DNS change was started. "+workerErr.Error())
				return exitUnavailable
			}
			fmt.Fprintf(out, "Quiesced worker observation: %s. This is only a point-in-time process check; it does not authorize a DNS inverse.\n", worker)
		}
	}
	switch observation.Status {
	case dnsenginerecovery.EvidenceLeaseExpired:
		fmt.Fprintln(out, "The active ledger lease has expired. The server owner should inspect the original operation and native DNS service; do not start another switch. A compatible recovery executor must establish worker liveness and host ownership before the same operation can resume.")
	case dnsenginerecovery.EvidenceWorkerRecorded, dnsenginerecovery.EvidenceOrphanedWorker:
		gone, probeErr := processidentity.RecordedWorkerGone(observation.WorkerPID, observation.WorkerStarted)
		switch {
		case probeErr != nil:
			fmt.Fprintln(out, "A worker is recorded, but its process identity could not be inspected. The server owner should check the same operation and native DNS service. Do not start another switch; recovery must prove worker and host state under the lock.")
		case !gone:
			fmt.Fprintln(out, "The recorded worker process matched at this instant. The server owner should follow the same operation in CelikPanel. Do not start another switch; the worker may change after this observation.")
		default:
			fmt.Fprintln(out, "No process matching the recorded worker was observed at this instant. The server owner should inspect the same operation and native DNS service. A compatible recovery executor must recheck the worker and host locks before the operation resumes; do not start another switch.")
		}
	case dnsenginerecovery.EvidenceExpiredCancellation:
		fmt.Fprintln(out, "The accepted lease expired and cancellation is recorded. The server owner should inspect the native DNS service and preserve both receipts. The same operation may resume only through a compatible recovery executor after host and worker checks.")
	case dnsenginerecovery.EvidenceReleasedUndecided:
		switch observation.ReleaseReason {
		case dnsengineartifact.ReleasedUnsupportedHostCode:
			fmt.Fprintln(out, "The Agent released this interrupted DNS switch lease because the host could not be inspected after restart. The server owner should inspect the host profile and native DNS authority, then retry observation of this same operation after the host is readable. The frozen journal remains; no inverse or new switch is authorized.")
		case dnsengineartifact.ReleasedHostWindowCode:
			fmt.Fprintln(out, "The Agent released this interrupted DNS switch lease because host startup did not finish within its recovery window. The server owner should confirm startup and native DNS authority, then retry observation of this same operation. The frozen journal remains; no inverse or new switch is authorized.")
		case dnsengineartifact.ReleasedNativeUnknownCode:
			fmt.Fprintln(out, "The Agent could not verify the interrupted DNS switch's native result after restart. The server owner should inspect the DNS service and the original operation, resolve the reported native error, then restart the Agent to retry that same journal. The journal remains and blocks a new DNS switch; this read-only status does not authorize an inverse.")
		default:
			fmt.Fprintln(diagnostic, "The released DNS switch has an unknown reason. Preserve its journal and ledger for owner review; no inverse or new switch is authorized.")
			return exitUnavailable
		}
	case dnsenginerecovery.EvidenceTerminalRolledBack:
		fmt.Fprintln(out, "The ledger records a failed DNS switch and its rolled-back journal is retained. The server owner should inspect the same operation and native DNS service. On a compatible Agent restart, the original inverse is re-proved under the host lock before this exact journal is retired; do not start another switch while it remains.")
	case dnsenginerecovery.EvidenceFinalized:
		fmt.Fprintln(out, "The ledger records finalization while a journal remains. The server owner should inspect native DNS health and the retained journal; this observation alone does not authorize cleanup or a new switch.")
	default:
		fmt.Fprintln(out, "The accepted operation is recorded. The server owner should follow its CelikPanel status and check native DNS health if progress stops. This read-only observation does not prove worker liveness or authorize another switch; recovery must recheck the same operation under the host lock.")
	}
	if quiesced {
		if stoppedName, required := rolledBackInactiveTargetUnit(evidence); required {
			if stopErr := dnsenginerecovery.ProbeStoppedUnit(
				observationCtx, stoppedName,
				dnsenginerecovery.SystemdUnitRunner,
				dnsenginerecovery.SystemdPDNSRuntimeRunner,
			); stopErr != nil {
				fmt.Fprintln(diagnostic, "The retained DNS rollback target could not be proved stopped. The server owner should inspect the native unit and preserve the same journal; no recovery mutation was started. "+stopErr.Error())
				return exitUnavailable
			}
			againEvidence, stillPresent, readErr := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
			if readErr != nil || !stillPresent || evidence.Observation.EvidenceSHA256 != againEvidence.Observation.EvidenceSHA256 ||
				!reflect.DeepEqual(evidence.Journal, againEvidence.Journal) {
				fmt.Fprintln(diagnostic, "DNS rollback evidence changed around the native stopped-target observation. Preserve the accepted journal and retry after the owner change settles; no recovery mutation was started.")
				return exitUnavailable
			}
			fmt.Fprintf(out, "The retained rollback target %s was a loaded inactive/dead unit with zero systemd main/control PIDs and an empty or absent native cgroup in two reads. This point-in-time observation does not prove later owner-edit exclusion, DNS health or recovery authority.\n", stoppedName)
		}
	}
	if observation.SourceEngine == "bind" || observation.TargetEngine == "bind" {
		var selectedReceipt binddns.Receipt
		if observation.TargetEngine == "bind" && observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact {
			var bindErr error
			selectedReceipt, bindErr = verifySelectedBINDTarget(observationCtx, observation.TargetGeneration, observation.TargetEpoch)
			if bindErr != nil {
				fmt.Fprintln(diagnostic, "Selected BIND generation is unknown or differs from the frozen target. The server owner should inspect the managed BIND generation, native configuration and DNS answers before the same operation resumes; no inverse was started. "+bindErr.Error())
				return exitUnavailable
			}
			fmt.Fprintln(out, "Managed BIND root and selected immutable generation matched the frozen target across two read-only observations. The daemon's loaded configuration, DNS answers, owner edits and recovery authority remain unproved.")
		} else {
			if bindErr := verifyInstalledBINDRoot(observationCtx); bindErr != nil {
				fmt.Fprintln(diagnostic, "Managed BIND root ownership is unknown. The server owner should inspect the native BIND directory, service group and package ownership before the same DNS operation resumes; no inverse was started. "+bindErr.Error())
				return exitUnavailable
			}
			fmt.Fprintln(out, "Managed BIND root directory and package ownership matched on two read-only walks. This does not prove the selected generation, DNS answers, owner edits or recovery authority.")
		}
		if vendorErr := verifyInstalledBINDVendorAndUnit(observationCtx); vendorErr != nil {
			fmt.Fprintln(diagnostic, "Native BIND vendor files or loaded systemd unit identity are unknown. The server owner should inspect the named service unit, its package ownership and startup options before the same operation resumes; no inverse was started. "+vendorErr.Error())
			return exitUnavailable
		}
		fmt.Fprintln(out, "Certified BIND vendor files and systemd unit identity matched across read-only checks. Process liveness, a pending daemon reload, loaded named configuration and DNS answers remain unproved.")
		if observation.TargetEngine == "bind" && observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact {
			mainPID, runtimeErr := verifyInstalledBINDRuntime(observationCtx)
			if runtimeErr != nil {
				fmt.Fprintln(diagnostic, "Selected BIND target has unknown running service state. The server owner should inspect named.service, its bind9 alias and any pending daemon reload before the same operation resumes; no inverse was started. "+runtimeErr.Error())
				return exitUnavailable
			}
			fmt.Fprintln(out, "Systemd reported a stable running BIND MainPID, matching APT alias and no pending daemon reload; the process start token and executable inode matched twice. Installed package bytes, loaded named configuration, DNS answers and owner edits remain unproved.")
			layout, serviceGID, layoutErr := installedBINDLayout()
			if layoutErr != nil {
				fmt.Fprintln(diagnostic, "Native BIND config owner is unknown. The server owner should inspect the service group and config before the same operation resumes; no inverse was started. "+layoutErr.Error())
				return exitUnavailable
			}
			if configErr := verifyInstalledBINDConfig(observationCtx, layout, serviceGID); configErr != nil {
				fmt.Fprintln(diagnostic, "Native BIND managed include is unknown or changed. The server owner should inspect the named configuration and selected generation before the same operation resumes; no inverse was started. "+configErr.Error())
				return exitUnavailable
			}
			if _, bindErr := verifySelectedBINDTarget(observationCtx, observation.TargetGeneration, observation.TargetEpoch); bindErr != nil {
				fmt.Fprintln(diagnostic, "Selected BIND generation changed around native config observation. The server owner should inspect named configuration and DNS answers before the same operation resumes; no inverse was started. "+bindErr.Error())
				return exitUnavailable
			}
			fmt.Fprintln(out, "Native BIND config files retained the exact managed zone include across secure read-only observations. When using APT, the main config also retained active includes. Named's loaded configuration and DNS answers remain unproved.")
			primaryIP := ""
			if selectedReceipt.Pairing != nil && selectedReceipt.Pairing.Role == binddns.PairRolePrimary {
				primaryIP = selectedReceipt.Pairing.LocalIP
			}
			if listenerErr := dnsenginerecovery.ProbeBINDListeners(observationCtx, mainPID, primaryIP, dnsenginerecovery.SSListenerRunner); listenerErr != nil {
				fmt.Fprintln(diagnostic, "Native BIND port-53 listener ownership is unknown or differs from the verified service. The server owner should inspect named.service and local DNS sockets before the same operation resumes; no inverse was started. "+listenerErr.Error())
				return exitUnavailable
			}
			afterListenerPID, runtimeErr := verifyInstalledBINDRuntime(observationCtx)
			if runtimeErr != nil || afterListenerPID != mainPID {
				fmt.Fprintln(diagnostic, "BIND process identity changed around listener observation. The server owner should inspect named.service and local DNS sockets before the same operation resumes; no inverse was started.")
				return exitUnavailable
			}
			fmt.Fprintln(out, "The local TCP and UDP port-53 listener inventory matched the verified named MainPID twice. This does not prove the daemon loaded the selected generation or that a DNS answer originated from that socket.")
			catalogSeen, catalogErr := dnsenginerecovery.ProbeInstalledPrimaryCatalogAnswer(observationCtx, selectedReceipt)
			if catalogErr != nil {
				fmt.Fprintln(diagnostic, "Local authoritative primary catalog answer is unknown or differs from the selected BIND generation. The server owner should inspect the native DNS listener and catalog SOA before the same operation resumes; no inverse was started. "+catalogErr.Error())
				return exitUnavailable
			}
			if catalogSeen {
				againReceipt, bindErr := verifySelectedBINDTarget(observationCtx, observation.TargetGeneration, observation.TargetEpoch)
				if bindErr != nil || !reflect.DeepEqual(selectedReceipt, againReceipt) {
					fmt.Fprintln(diagnostic, "Selected BIND generation changed around the local catalog answer. The server owner should inspect the native DNS service before the same operation resumes; no inverse was started.")
					return exitUnavailable
				}
				afterAnswerPID, runtimeErr := verifyInstalledBINDRuntime(observationCtx)
				if runtimeErr != nil || afterAnswerPID != mainPID {
					fmt.Fprintln(diagnostic, "BIND process identity changed around the local catalog answer. The server owner should inspect named.service and the catalog SOA before the same operation resumes; no inverse was started.")
					return exitUnavailable
				}
				fmt.Fprintln(out, "The host's selected primary IP returned the frozen catalog's exact authoritative SOA serial twice over DNS/TCP. DNS-answer/socket causality, member zones, AXFR, loaded config and recovery authority remain unproved.")
			} else {
				fmt.Fprintln(out, "No primary BIND catalog SOA applies to this receipt. Live zone and transfer answers remain unproved.")
			}
		}
	}
	if observation.TargetEngine == "pdns" && observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact {
		mainPID, runtimeErr := verifyInstalledPDNSRuntime(observationCtx)
		if runtimeErr != nil {
			fmt.Fprintln(diagnostic, "Selected PowerDNS native service state is unknown. The server owner should inspect pdns.service, the stopped BIND units and pending daemon reload before the same operation resumes; no inverse was started. "+runtimeErr.Error())
			return exitUnavailable
		}
		if listenerErr := dnsenginerecovery.ProbeAuthorityListeners(observationCtx, "pdns_server", mainPID, "", dnsenginerecovery.SSListenerRunner); listenerErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS port-53 listener ownership is unknown. The server owner should inspect pdns.service and local DNS sockets before the same operation resumes; no inverse was started. "+listenerErr.Error())
			return exitUnavailable
		}
		afterPID, runtimeErr := verifyInstalledPDNSRuntime(observationCtx)
		if runtimeErr != nil || afterPID != mainPID {
			fmt.Fprintln(diagnostic, "PowerDNS process identity changed around listener observation. The server owner should inspect pdns.service and local DNS sockets before the same operation resumes; no inverse was started.")
			return exitUnavailable
		}
		fmt.Fprintln(out, "The selected PowerDNS vendor unit identity and native process matched across read-only observations; both public DNS transports belonged to its MainPID. This process/socket observation does not establish database content, zone answers, loaded config or recovery authority.")
	}
	if quiesced && evidence.Journal.Mode == transport.DNSEngineSwitchModeAdopt {
		proofCtx, cancel := context.WithTimeout(observationCtx, 10*time.Second)
		defer cancel()
		manifest, manifestErr := dnsengineartifact.SwitchJournalManifest(evidence.Journal)
		if manifestErr != nil {
			fmt.Fprintln(diagnostic, "The PowerDNS adoption manifest could not be reconstructed from its frozen journal. Preserve the original operation for owner review; no recovery mutation was started. "+manifestErr.Error())
			return exitUnavailable
		}
		pdnsGID, groupErr := localServiceGroupID("/etc/group", "pdns")
		if groupErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption service group is unknown. The server owner should inspect the native pdns group before the same operation resumes; no recovery mutation was started. "+groupErr.Error())
			return exitUnavailable
		}
		if configErr := dnsenginerecovery.ProbeInstalledPDNSAdoptionConfigs(proofCtx, policy, evidence.Journal.ConfigBefore, pdnsGID); configErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption configuration no longer matches its frozen file, owner or path evidence. The server owner should inspect native PowerDNS configuration and preserve the original operation; no recovery mutation was started. "+configErr.Error())
			return exitUnavailable
		}
		if proofErr := dnsenginerecovery.ProbePDNSAdoptionDatabase(
			proofCtx, policy.PDNSDatabasePath,
			evidence.Journal.PDNSLiveSize, evidence.Journal.PDNSLiveSHA256, manifest,
		); proofErr != nil {
			fmt.Fprintln(diagnostic, "The PowerDNS adoption database or its zone rows could not be matched to the frozen journal. The server owner should inspect the native database and original operation; preserve the journal and do not start another DNS switch. No recovery mutation was started. "+proofErr.Error())
			return exitUnavailable
		}
		mainPID, runtimeErr := verifyInstalledPDNSRuntime(proofCtx)
		if runtimeErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption source process identity is unknown. The server owner should inspect pdns.service and the stopped BIND units before the same operation resumes; no recovery mutation was started. "+runtimeErr.Error())
			return exitUnavailable
		}
		if listenerErr := dnsenginerecovery.ProbeAuthorityListeners(proofCtx, "pdns_server", mainPID, "", dnsenginerecovery.SSListenerRunner); listenerErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption source listener ownership is unknown. The server owner should inspect local TCP/UDP port 53 and pdns.service; no recovery mutation was started. "+listenerErr.Error())
			return exitUnavailable
		}
		address, addressErr := dnsenginerecovery.ProbeAuthorityIPv4Address(proofCtx, "pdns_server", mainPID, dnsenginerecovery.SSListenerRunner)
		if addressErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption has no verified concrete local IPv4 answer endpoint. The server owner should inspect pdns.service and its TCP/UDP port-53 bindings; no recovery mutation was started. "+addressErr.Error())
			return exitUnavailable
		}
		activeSOA, deletedSOA, answerErr := dnsenginerecovery.ProbeInstalledPDNSAdoptionSOA(proofCtx, net.JoinHostPort(address, "53"), manifest)
		if answerErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption live authoritative SOA answers could not be matched to the frozen journal over TCP and UDP. The server owner should inspect native DNS answers and the original operation; preserve the journal and do not start another DNS switch. No recovery mutation was started. "+answerErr.Error())
			return exitUnavailable
		}
		if again, runtimeErr := verifyInstalledPDNSRuntime(proofCtx); runtimeErr != nil || again != mainPID {
			fmt.Fprintln(diagnostic, "PowerDNS adoption source process changed around the answer observation. Preserve the original operation and inspect native DNS; no recovery mutation was started.")
			return exitUnavailable
		}
		if listenerErr := dnsenginerecovery.ProbeAuthorityListeners(proofCtx, "pdns_server", mainPID, "", dnsenginerecovery.SSListenerRunner); listenerErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption listener ownership changed around the answer observation. Preserve the original operation and inspect native DNS; no recovery mutation was started. "+listenerErr.Error())
			return exitUnavailable
		}
		if againGID, groupErr := localServiceGroupID("/etc/group", "pdns"); groupErr != nil || againGID != pdnsGID {
			fmt.Fprintln(diagnostic, "PowerDNS adoption service group changed around the native answer observation. Preserve the original operation and inspect native DNS; no recovery mutation was started.")
			return exitUnavailable
		}
		if configErr := dnsenginerecovery.ProbeInstalledPDNSAdoptionConfigs(proofCtx, policy, evidence.Journal.ConfigBefore, pdnsGID); configErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption configuration changed around the native answer observation. Preserve the original operation and inspect native DNS; no recovery mutation was started. "+configErr.Error())
			return exitUnavailable
		}
		if proofErr := dnsenginerecovery.ProbePDNSDatabasePreimage(
			proofCtx, policy.PDNSDatabasePath,
			evidence.Journal.PDNSLiveSize, evidence.Journal.PDNSLiveSHA256,
		); proofErr != nil {
			fmt.Fprintln(diagnostic, "PowerDNS adoption database changed around the native process observation. Preserve the original operation and inspect native DNS; no recovery mutation was started. "+proofErr.Error())
			return exitUnavailable
		}
		againEvidence, stillPresent, readErr := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
		if readErr != nil || !stillPresent ||
			evidence.Observation.EvidenceSHA256 != againEvidence.Observation.EvidenceSHA256 ||
			!reflect.DeepEqual(evidence.Journal, againEvidence.Journal) {
			fmt.Fprintln(diagnostic, "PowerDNS adoption evidence changed around the native preimage observation. Preserve the original operation and inspect native DNS; no recovery mutation was started.")
			return exitUnavailable
		}
		fmt.Fprintf(out, "The installed PowerDNS config files, owners and paths, database bytes and read-only SQLite zone/peer/integrity transaction matched the frozen adoption manifest; the verified native process owned local TCP/UDP port 53 between secured reads. At that endpoint, %d active frozen zones returned exact authoritative SOA serials over TCP and UDP; %d deleted-zone absence claims remain unproved. Other records, loaded config, later owner edits and inverse authority remain unproved.\n", activeSOA, deletedSOA)
	}
	fmt.Fprintf(out, "Frozen native inverse shape: %s. This classification does not prove worker exclusion, owner authority or safe recovery execution.\n", observation.InverseKind)
	switch observation.TargetReceipt {
	case dnsenginerecovery.TargetReceiptExact:
		fmt.Fprintln(out, "The current DNS state receipt matched the frozen journal target at inspection time. The server owner must still verify native DNS service before recovery; this observation does not authorize a mutation.")
	case dnsenginerecovery.TargetReceiptDifferent:
		fmt.Fprintln(out, "The current DNS state receipt differs from the frozen journal target. Preserve both records and inspect owner changes and native DNS before the original operation resumes.")
	case dnsenginerecovery.TargetReceiptAbsent:
		fmt.Fprintln(out, "The current DNS state receipt is absent. This alone does not prove that an inverse is safe; preserve the journal and inspect native DNS before recovery.")
	}
	switch observation.SourceReceipt {
	case dnsenginerecovery.SourceReceiptExact:
		fmt.Fprintln(out, "The current DNS state receipt exactly matches the frozen journal source. This does not prove the native DNS service was restored; the server owner must verify native DNS before resuming this operation.")
	case dnsenginerecovery.SourceReceiptMutualAbsence:
		fmt.Fprintln(out, "The journal and current state both have no source receipt. This is not proof that DNS was rolled back or is healthy; the server owner must inspect native DNS before resuming this operation.")
	case dnsenginerecovery.SourceReceiptDifferent:
		fmt.Fprintln(out, "The current DNS state receipt does not match the frozen journal source. Preserve the evidence and inspect native DNS and owner changes before the original operation resumes.")
	}
	switch observation.SourceOwnership {
	case dnsenginerecovery.SourceOwnershipNotApplicable:
		fmt.Fprintln(out, "The switch has no previous DNS engine, so no frozen source ownership receipt is required. Native DNS still needs verification before the operation resumes.")
	case dnsenginerecovery.SourceOwnershipAbsent:
		fmt.Fprintln(out, "The frozen source ownership receipt is missing. The server owner must preserve the journal and inspect this engine's private ownership evidence before the original operation resumes; no inverse is admitted.")
	case dnsenginerecovery.SourceOwnershipExact:
		fmt.Fprintln(out, "The frozen source ownership receipt matches the journal. The server owner must still verify native DNS and worker exclusion before any recovery action.")
	case dnsenginerecovery.SourceOwnershipDifferent:
		fmt.Fprintln(out, "The source ownership receipt differs from the journal. Preserve both records and inspect owner changes before the original operation resumes; no inverse is admitted.")
	}

	if unitErr != nil {
		fmt.Fprintln(diagnostic, "Native DNS unit state is unknown. The server owner should inspect the named, bind9 and pdns systemd services before the original operation resumes; preserve the journal and do not start another switch. "+unitErr.Error())
	} else {
		for _, unit := range units {
			fmt.Fprintf(out, "Native unit %s: load=%s active=%s unit-file=%s.\n", unit.Name, unit.LoadState, unit.ActiveState, unit.UnitFileState)
		}
		fmt.Fprintln(out, "These systemd properties were observed at one instant; they do not prove DNS answers, zone content, owner edits or recovery authority.")
	}
	if quiesced {
		if unitErr == nil {
			fmt.Fprintln(out, "Release and host mutation locks were held; evidence bytes and native unit properties matched across two reads. Future worker liveness, unobserved DNS answers, owner edits and recovery authority remain unproved.")
		} else {
			fmt.Fprintln(out, "Release and host mutation locks were held, but native unit state could not be confirmed. No stable DNS observation or recovery authority was established.")
		}
	}
	if unitErr != nil {
		return exitUnavailable
	}
	if observationCtx.Err() != nil {
		fmt.Fprintln(diagnostic, "DNS observation exceeded its deadline. Preserve the same operation and retry after native service responsiveness is restored; no DNS operation was started.")
		return exitUnavailable
	}
	return exitOK
}
