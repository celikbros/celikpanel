//go:build linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSSwitchStatusExactRequestArgs(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"dns-switch-status"},
		{"dns-switch-status", "--quiesced"},
		{"dns-switch-status", "--quiesced", "--request-id", id},
		{"dns-switch-status", "--request-id", id, "--quiesced"},
	} {
		_, gotID, ok := parseDNSSwitchStatusArgs(args)
		if !ok || (len(args) > 2 && gotID != id) {
			t.Fatalf("valid arguments rejected: %q", args)
		}
	}
	for _, args := range [][]string{
		nil,
		{"other"},
		{"dns-switch-status", "--request-id", id},
		{"dns-switch-status", "--quiesced", "--request-id"},
		{"dns-switch-status", "--quiesced", "--request-id", strings.Repeat("A", 32)},
		{"dns-switch-status", "--quiesced", "--quiesced"},
		{"dns-switch-status", "--quiesced", "--request-id", id, "--request-id", id},
		{"dns-switch-status", "--quiesced", "extra"},
	} {
		if _, _, ok := parseDNSSwitchStatusArgs(args); ok {
			t.Fatalf("unsafe arguments accepted: %q", args)
		}
	}
}

func TestJournalAbsentExactDNSJobReportsOnlyRecordedStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	requestID := strings.Repeat("a", 32)
	now := time.Now().UTC().Truncate(time.Second)
	job := &transport.ServiceMutationJob{
		RequestID: requestID, OwnerID: strings.Repeat("b", 32),
		Kind: "dns_engine_switch", Target: "pdns",
		PackageName: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
		Status:      servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Minute), UpdatedAt: now, DeadlineAt: now.Add(time.Hour),
		FinishedAt: now, ErrorCode: "interrupted", ErrorMessage: "private diagnostic",
	}
	path := filepath.Join(root, "service-mutations.json")
	var other *transport.ServiceMutationJob
	write := func() {
		t.Helper()
		ledger := &servicemutationledger.Ledger{
			Version: servicemutationledger.Version,
			Jobs:    map[string]*transport.ServiceMutationJob{requestID: job},
		}
		if other != nil {
			ledger.Jobs[other.RequestID] = other
			ledger.ActiveRequestID = other.RequestID
		}
		raw, err := servicemutationledger.Encode(ledger)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	status, err := readJournalAbsentDNSJob(context.Background(), root, owner, requestID)
	if err != nil || status != servicemutationledger.StatusFailed {
		t.Fatalf("exact failed job: %q, %v", status, err)
	}
	if _, err := readJournalAbsentDNSJob(context.Background(), root, owner, strings.Repeat("d", 32)); err == nil {
		t.Fatal("unrelated request was accepted")
	}
	job.Kind = "dns_zone_sync"
	write()
	if _, err := readJournalAbsentDNSJob(context.Background(), root, owner, requestID); err == nil {
		t.Fatal("non-switch job was accepted")
	}
	job.Kind = "dns_engine_switch"
	job.PackageName = "not-a-switch-qualifier"
	write()
	if _, err := readJournalAbsentDNSJob(context.Background(), root, owner, requestID); err == nil {
		t.Fatal("invalid switch identity was accepted")
	}
	job.PackageName = "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64)
	other = &transport.ServiceMutationJob{
		RequestID: strings.Repeat("d", 32), OwnerID: strings.Repeat("e", 32),
		Kind: "other", Target: "host", Status: servicemutationledger.StatusRunning,
		Phase: "leased", Attempt: 1, StartedAt: now, UpdatedAt: now,
		LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
	}
	write()
	if _, err := readJournalAbsentDNSJob(context.Background(), root, owner, requestID); err == nil {
		t.Fatal("another active host mutation was ignored")
	}
	other = nil
	write()
	if err := os.WriteFile(filepath.Join(root, "dns-engine-switch-journal.json"), []byte("untrusted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readJournalAbsentDNSJob(context.Background(), root, owner, requestID); err == nil {
		t.Fatal("present journal was accepted as absent")
	}
}

func TestJournalAbsentDNSJobRejectsMissingLedgerAndDeadline(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	id := strings.Repeat("a", 32)
	if _, err := readJournalAbsentDNSJob(context.Background(), root, owner, id); err == nil {
		t.Fatal("missing ledger was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readJournalAbsentDNSJob(ctx, root, owner, id); err == nil {
		t.Fatal("expired observation was accepted")
	}
}
