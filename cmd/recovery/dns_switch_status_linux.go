//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
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
		if len(fields) != 4 || fields[0] != "celikpanel" {
			continue
		}
		number, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || number == 0 || found {
			return 0, errors.New("local CelikPanel group identity is ambiguous")
		}
		gid, found = uint32(number), true
	}
	if !found {
		return 0, errors.New("local CelikPanel group is absent")
	}
	return gid, nil
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
	fmt.Fprintf(out, "DNS switch request %s: %s (journal phase %s).\n", observation.RequestID, observation.Status, observation.Phase)
	switch observation.TargetReceipt {
	case dnsenginerecovery.TargetReceiptExact:
		fmt.Fprintln(out, "The current DNS state receipt matched the frozen journal target at inspection time. The server owner must still verify native DNS service before recovery; this observation does not authorize a mutation.")
	case dnsenginerecovery.TargetReceiptDifferent:
		fmt.Fprintln(out, "The current DNS state receipt differs from the frozen journal target. Preserve both records and inspect owner changes and native DNS before the original operation resumes.")
	case dnsenginerecovery.TargetReceiptAbsent:
		fmt.Fprintln(out, "The current DNS state receipt is absent. This alone does not prove that an inverse is safe; preserve the journal and inspect native DNS before recovery.")
	}
	if len(args) == 2 {
		fmt.Fprintln(out, "Release and host mutation locks were held during this evidence read. Native DNS state and future worker liveness remain unproved.")
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
	return exitOK
}
