//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/dnslistener"
)

// ResolvedStubProcess is the kernel command name (comm, truncated to 15
// bytes) of systemd-resolved, and ResolvedStubUnit the systemd unit whose
// cgroup must own a stub listener. The name alone never admits a listener.
const (
	ResolvedStubProcess = "systemd-resolve"
	ResolvedStubUnit    = "systemd-resolved.service"
)

// LocalDNSAddress reports whether a port-53 socket is outside the public
// authority inventory: loopback or link-local unicast. The public proofs skip
// these sockets; ProbeLocalDNSListeners checks their owners.
func LocalDNSAddress(address net.IP) bool {
	return address.IsLoopback() || address.IsLinkLocalUnicast()
}

// resolvedStubAddress lists the only addresses systemd-resolved binds for its
// DNS stub listeners: the stub on 127.0.0.53 and the proxy on 127.0.0.54.
func resolvedStubAddress(address net.IP) bool {
	v4 := address.To4()
	return v4 != nil && (v4.Equal(net.IPv4(127, 0, 0, 53)) || v4.Equal(net.IPv4(127, 0, 0, 54)))
}

func localListenerIdentity(row dnslistener.Row) string {
	return fmt.Sprintf("%s|%s|%s|%d", row.Protocol, row.Address.String(), row.Process, row.PID)
}

// localDNSListeners is one strict classification of the loopback and
// link-local port-53 sockets of an ss inventory. source holds sockets of the
// verified source daemon; stubCandidates holds sockets that name the
// systemd-resolved stub on its own addresses and still need the cgroup proof.
type localDNSListeners struct {
	source         []string
	stubCandidates []dnslistener.Row
}

// classifyLocalDNSListeners inspects every loopback and link-local port-53
// row of a canonical ss inventory. Public rows are ignored here; the public
// authority proofs cover them. A local row is accepted only when it belongs to
// the verified source daemon (sourceProcess with sourcePID; sourcePID 0 means
// no source daemon may hold a local socket) or names the systemd-resolved stub
// on 127.0.0.53 or 127.0.0.54, which the caller must still bind to its unit
// cgroup. A named or pdns_server socket outside the verified source, any
// other process and malformed rows refuse.
func classifyLocalDNSListeners(output, sourceProcess string, sourcePID uint64) (localDNSListeners, error) {
	if (sourcePID == 0) != (sourceProcess == "") ||
		strings.ContainsAny(sourceProcess, "\x00\r\n\t ,()\"") {
		return localDNSListeners{}, errors.New("invalid local DNS listener source identity")
	}
	source := map[string]struct{}{}
	stubs := map[string]dnslistener.Row{}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		row, err := dnslistener.ParseRow(line)
		if err != nil {
			return localDNSListeners{}, err
		}
		if !LocalDNSAddress(row.Address) {
			continue
		}
		switch {
		case sourcePID != 0 && row.Process == sourceProcess && row.PID == sourcePID:
			source[localListenerIdentity(row)] = struct{}{}
		case row.Process == ResolvedStubProcess && resolvedStubAddress(row.Address):
			stubs[localListenerIdentity(row)] = row
		case row.Process == "named" || row.Process == "pdns_server":
			return localDNSListeners{}, fmt.Errorf("a %s process (PID %d) holds local port-53 listener %s outside the verified DNS source", row.Process, row.PID, row.Address)
		default:
			return localDNSListeners{}, fmt.Errorf("an unrecognized process %q (PID %d) holds local port-53 listener %s", row.Process, row.PID, row.Address)
		}
	}
	result := localDNSListeners{source: make([]string, 0, len(source))}
	for identity := range source {
		result.source = append(result.source, identity)
	}
	sort.Strings(result.source)
	keys := make([]string, 0, len(stubs))
	for identity := range stubs {
		keys = append(keys, identity)
	}
	sort.Strings(keys)
	for _, identity := range keys {
		result.stubCandidates = append(result.stubCandidates, stubs[identity])
	}
	return result, nil
}

// ProcessCgroupReader returns the cgroup-v2 path ("0::" line) of one live
// process, bound to that process's kernel start identity.
type ProcessCgroupReader func(context.Context, uint64) (string, error)

// verifyLocalDNSListeners reads the inventory twice and requires the same
// classification both times. Each stub candidate's PID must be in
// ResolvedStubUnit's system-slice cgroup in each read. It returns the accepted
// stub listeners as "protocol|address|process|pid|unit" records, which the
// caller must record; accepted source sockets are not returned.
func verifyLocalDNSListeners(
	ctx context.Context, sourceProcess string, sourcePID uint64,
	runner BINDListenerRunner, cgroup ProcessCgroupReader,
) ([]string, error) {
	if ctx == nil || runner == nil || cgroup == nil {
		return nil, errors.New("invalid local DNS listener observation")
	}
	want := "/system.slice/" + ResolvedStubUnit
	var first []string
	var firstSource []string
	for attempt := range 2 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := runner(ctx)
		if err != nil {
			return nil, err
		}
		if len(raw) > 64<<10 {
			return nil, errors.New("DNS listener output exceeds its bound")
		}
		seen, err := classifyLocalDNSListeners(string(raw), sourceProcess, sourcePID)
		if err != nil {
			return nil, err
		}
		stubs := make([]string, 0, len(seen.stubCandidates))
		for _, row := range seen.stubCandidates {
			path, err := cgroup(ctx, row.PID)
			if err != nil {
				return nil, fmt.Errorf("prove owner of local port-53 listener %s (PID %d): %w", row.Address, row.PID, err)
			}
			if path != want {
				return nil, fmt.Errorf("local port-53 listener %s is named %s but PID %d is not in %s", row.Address, row.Process, row.PID, ResolvedStubUnit)
			}
			stubs = append(stubs, localListenerIdentity(row)+"|"+ResolvedStubUnit)
		}
		if attempt == 0 {
			first, firstSource = stubs, seen.source
		} else if !reflect.DeepEqual(first, stubs) || !reflect.DeepEqual(firstSource, seen.source) {
			return nil, errors.New("local port-53 listener inventory changed during observation")
		}
	}
	recordLocalDNSListeners(ctx, first)
	return first, ctx.Err()
}

// LocalDNSListenerRecord collects accepted resolver-stub listeners observed
// by the local listener proofs run under one context, in first-seen order.
type LocalDNSListenerRecord struct {
	mu      sync.Mutex
	seen    map[string]bool
	entries []string
}

type localDNSListenerRecordKey struct{}

// WithLocalDNSListenerRecord returns a context whose local listener proofs
// add each accepted stub listener to the returned record once.
func WithLocalDNSListenerRecord(ctx context.Context) (context.Context, *LocalDNSListenerRecord) {
	record := &LocalDNSListenerRecord{seen: map[string]bool{}}
	return context.WithValue(ctx, localDNSListenerRecordKey{}, record), record
}

// Entries returns the recorded stub listeners.
func (r *LocalDNSListenerRecord) Entries() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.entries...)
}

func recordLocalDNSListeners(ctx context.Context, entries []string) {
	record, _ := ctx.Value(localDNSListenerRecordKey{}).(*LocalDNSListenerRecord)
	if record == nil {
		return
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	for _, entry := range entries {
		if !record.seen[entry] {
			record.seen[entry] = true
			record.entries = append(record.entries, entry)
		}
	}
}

// LocalDNSListenerRecordText is the owner-facing line for recorded stub
// listeners, or "" when none were recorded.
func LocalDNSListenerRecordText(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	return "Local port-53 listeners accepted as the systemd-resolved stub resolver (unit cgroup and address proved; not DNS authority): " + strings.Join(entries, ", ") + "."
}
