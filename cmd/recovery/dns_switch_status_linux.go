//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/processidentity"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
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
func verifySelectedBINDTarget(ctx context.Context, generation string, epoch int64) error {
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return err
	}
	before, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return err
	}
	publisher, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return err
	}
	first, err := publisher.LoadCurrent()
	if err != nil {
		return fmt.Errorf("read selected managed BIND generation: %w", err)
	}
	receipt := first.CurrentReceipt()
	if receipt.Generation != generation || receipt.EngineEpoch != epoch {
		return errors.New("selected managed BIND generation differs from the frozen target")
	}
	second, err := publisher.LoadCurrent()
	if err != nil {
		return fmt.Errorf("reread selected managed BIND generation: %w", err)
	}
	after, err := bindroot.VerifyInstalledCatalog(ctx, layout, gid)
	if err != nil {
		return err
	}
	if before != after || !reflect.DeepEqual(receipt, second.CurrentReceipt()) {
		return errors.New("selected managed BIND generation changed during observation")
	}
	return nil
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
func runDNSSwitchStatus(args []string, uid int, out, diagnostic io.Writer) int {
	if (len(args) != 1 && !(len(args) == 2 && args[1] == "--quiesced")) || args[0] != "dns-switch-status" {
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
	root := hostingpath.ServiceMutationStateRoot()
	owner := servicemutationledger.FileOwner{UID: 0, GID: groupID}
	if len(args) == 2 {
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
	observation, present, err := dnsenginerecovery.InspectFiles(root, owner, policy, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(diagnostic, "DNS switch evidence could not be verified. Preserve the private journal and ledger; the server owner must inspect their ownership, native DNS state and compatibility before the same operation resumes. "+err.Error())
		return exitUnavailable
	}
	if !present {
		fmt.Fprintln(out, "No DNS switch journal was observed. This does not prove historical completion or current DNS health. The server owner should inspect the native DNS service and the panel's operation status before starting another switch.")
		return exitOK
	}
	units, unitErr := dnsenginerecovery.ProbeNativeUnits(context.Background(), observation.NativeUnits, dnsenginerecovery.SystemdUnitRunner)
	if len(args) == 2 && unitErr == nil {
		again, stillPresent, readErr := dnsenginerecovery.InspectFiles(root, owner, policy, time.Now().UTC())
		if readErr != nil || !stillPresent {
			fmt.Fprintln(diagnostic, "DNS switch evidence changed or became unreadable during the quiesced observation. The server owner should inspect the existing operation and native DNS service, then retry after owner changes settle; no DNS operation was started.")
			return exitUnavailable
		}
		againUnits, probeErr := dnsenginerecovery.ProbeNativeUnits(context.Background(), again.NativeUnits, dnsenginerecovery.SystemdUnitRunner)
		if probeErr != nil || !dnsenginerecovery.StableQuiescedObservation(observation, again, units, againUnits) {
			fmt.Fprintln(diagnostic, "DNS switch evidence or native DNS unit properties changed during the quiesced observation. The server owner should inspect the existing operation and native DNS service, then retry after owner changes settle; no DNS operation was started.")
			return exitUnavailable
		}
	}
	if observation.SourceEngine == "bind" || observation.TargetEngine == "bind" {
		if observation.TargetEngine == "bind" && observation.TargetReceipt == dnsenginerecovery.TargetReceiptExact {
			if bindErr := verifySelectedBINDTarget(context.Background(), observation.TargetGeneration, observation.TargetEpoch); bindErr != nil {
				fmt.Fprintln(diagnostic, "Selected BIND generation is unknown or differs from the frozen target. The server owner should inspect the managed BIND generation, native configuration and DNS answers before the same operation resumes; no inverse was started. "+bindErr.Error())
				return exitUnavailable
			}
			fmt.Fprintln(out, "Managed BIND root and selected immutable generation matched the frozen target across two read-only observations. The daemon's loaded configuration, DNS answers, owner edits and recovery authority remain unproved.")
		} else {
			if bindErr := verifyInstalledBINDRoot(context.Background()); bindErr != nil {
				fmt.Fprintln(diagnostic, "Managed BIND root ownership is unknown. The server owner should inspect the native BIND directory, service group and package ownership before the same DNS operation resumes; no inverse was started. "+bindErr.Error())
				return exitUnavailable
			}
			fmt.Fprintln(out, "Managed BIND root directory and package ownership matched on two read-only walks. This does not prove the selected generation, DNS answers, owner edits or recovery authority.")
		}
		if vendorErr := verifyInstalledBINDVendorAndUnit(context.Background()); vendorErr != nil {
			fmt.Fprintln(diagnostic, "Native BIND vendor files or loaded systemd unit identity are unknown. The server owner should inspect the named service unit, its package ownership and startup options before the same operation resumes; no inverse was started. "+vendorErr.Error())
			return exitUnavailable
		}
		fmt.Fprintln(out, "Certified BIND vendor files and systemd unit identity matched across read-only checks. Process liveness, a pending daemon reload, loaded named configuration and DNS answers remain unproved.")
	}
	fmt.Fprintf(out, "DNS switch request %s: %s (journal phase %s).\n", observation.RequestID, observation.Status, observation.Phase)
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
	if len(args) == 2 {
		if unitErr == nil {
			fmt.Fprintln(out, "Release and host mutation locks were held; evidence bytes and native unit properties matched across two reads. Future worker liveness, DNS answers, owner edits and recovery authority remain unproved.")
		} else {
			fmt.Fprintln(out, "Release and host mutation locks were held, but native unit state could not be confirmed. No stable DNS observation or recovery authority was established.")
		}
	}
	switch observation.Status {
	case dnsenginerecovery.EvidenceLeaseExpired:
		fmt.Fprintln(out, "The active ledger lease has expired. The server owner should inspect the original operation and native DNS service; do not start another switch. A compatible recovery executor must establish worker liveness and host ownership before the same operation can resume.")
	case dnsenginerecovery.EvidenceWorkerRecorded:
		matches, probeErr := processidentity.Matches(observation.WorkerPID, observation.WorkerStarted)
		switch {
		case probeErr != nil:
			fmt.Fprintln(out, "A worker is recorded, but its process identity could not be inspected. The server owner should check the same operation and native DNS service. Do not start another switch; recovery must prove worker and host state under the lock.")
		case matches:
			fmt.Fprintln(out, "The recorded worker process matched at this instant. The server owner should follow the same operation in CelikPanel. Do not start another switch; the worker may change after this observation.")
		default:
			fmt.Fprintln(out, "No process matching the recorded worker was observed at this instant. The server owner should inspect the same operation and native DNS service. A compatible recovery executor must recheck the worker and host locks before the operation resumes; do not start another switch.")
		}
	case dnsenginerecovery.EvidenceExpiredCancellation:
		fmt.Fprintln(out, "The accepted lease expired and cancellation is recorded. The server owner should inspect the native DNS service and preserve both receipts. The same operation may resume only through a compatible recovery executor after host and worker checks.")
	case dnsenginerecovery.EvidenceFinalized:
		fmt.Fprintln(out, "The ledger records finalization while a journal remains. The server owner should inspect native DNS health and the retained journal; this observation alone does not authorize cleanup or a new switch.")
	default:
		fmt.Fprintln(out, "The accepted operation is recorded. The server owner should follow its CelikPanel status and check native DNS health if progress stops. This read-only observation does not prove worker liveness or authorize another switch; recovery must recheck the same operation under the host lock.")
	}
	if unitErr != nil {
		return exitUnavailable
	}
	return exitOK
}
