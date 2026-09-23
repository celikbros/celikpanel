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
func runDNSSwitchStatus(args []string, uid int, out, diagnostic io.Writer) int {
	if len(args) != 1 || args[0] != "dns-switch-status" {
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
	journalRaw, present, err := servicemutationledger.ReadFile(filepath.Join(root, "dns-engine-switch-journal.json"), dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil {
		fmt.Fprintln(diagnostic, "DNS switch evidence could not be read. Preserve the private state directory; the server owner must inspect its path and ownership before retrying. "+err.Error())
		return exitUnavailable
	}
	if !present {
		fmt.Fprintln(out, "No DNS switch journal was observed. This does not prove historical completion or current DNS health. The server owner should inspect the native DNS service and the panel's operation status before starting another switch.")
		return exitOK
	}
	policy := installedDNSJournalPolicy(owner.GID)
	journal, err := policy.DecodeSwitchJournal(journalRaw)
	if err != nil {
		fmt.Fprintln(diagnostic, "DNS switch journal is unrecognized. Preserve the file; the server owner must use a compatible recovery version and inspect the native DNS service. "+err.Error())
		return exitUnavailable
	}
	ledgerRaw, present, err := servicemutationledger.ReadFile(filepath.Join(root, "service-mutations.json"), servicemutationledger.MaxSize, owner)
	if err != nil || !present {
		fmt.Fprintln(diagnostic, "DNS switch ledger could not be verified. Preserve the journal and ledger; the server owner must inspect the private state directory before the same operation can resume.")
		return exitUnavailable
	}
	ledger, err := servicemutationledger.Decode(ledgerRaw)
	if err != nil {
		fmt.Fprintln(diagnostic, "DNS switch ledger is unrecognized. Preserve both files; the server owner must use a compatible recovery version before the same operation can resume. "+err.Error())
		return exitUnavailable
	}
	observation, err := dnsenginerecovery.InspectEvidence(policy, journal, ledger, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(diagnostic, "DNS switch evidence disagrees. Preserve both files; the server owner must review the operation and native DNS state before retrying. "+err.Error())
		return exitUnavailable
	}
	fmt.Fprintf(out, "DNS switch request %s: %s (journal phase %s).\n", observation.RequestID, observation.Status, observation.Phase)
	switch observation.Status {
	case dnsenginerecovery.EvidenceLeaseExpired:
		fmt.Fprintln(out, "The active ledger lease has expired. The server owner should inspect the original operation and native DNS service; do not start another switch. A compatible recovery executor must establish worker liveness and host ownership before the same operation can resume.")
	case dnsenginerecovery.EvidenceWorkerRecorded:
		fmt.Fprintln(out, "A worker is recorded, but its liveness is unknown. The server owner should check the existing operation in CelikPanel; if unavailable, inspect its process and native DNS service. Do not start a second switch. Recovery resumes only after the same worker and host state are verified.")
	case dnsenginerecovery.EvidenceExpiredCancellation:
		fmt.Fprintln(out, "The accepted lease expired and cancellation is recorded. The server owner should inspect the native DNS service and preserve both receipts. The same operation may resume only through a compatible recovery executor after host and worker checks.")
	case dnsenginerecovery.EvidenceFinalized:
		fmt.Fprintln(out, "The ledger records finalization while a journal remains. The server owner should inspect native DNS health and the retained journal; this observation alone does not authorize cleanup or a new switch.")
	default:
		fmt.Fprintln(out, "The accepted operation is recorded. The server owner should follow its CelikPanel status and check native DNS health if progress stops. This read-only observation does not prove worker liveness or authorize another switch; recovery must recheck the same operation under the host lock.")
	}
	return exitOK
}
