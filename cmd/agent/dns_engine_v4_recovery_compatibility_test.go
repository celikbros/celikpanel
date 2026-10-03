//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func testV4PDNSTargetJournal(t *testing.T) dnsEngineSwitchJournal {
	t.Helper()
	// The Alpha81 fixture froze a root:root state snapshot. Bind the Agent's
	// managed-owner contract to that same identity so dnsJournalPolicy() agrees
	// with the policy below regardless of the test process's own uid/gid.
	previousUID, previousGID := serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, 0
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = previousUID, previousGID
	})
	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	statePath := filepath.Join(root, "dns-engine-state.json")
	policy := dnsengineartifact.JournalPolicy{
		StatePath: statePath, StateUID: 0, StateGID: 0, RequireOwner: true,
		PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("/var/lib/celikpanel-agent-private/dns-engine-state.json"), []byte(statePath))
	base, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	base.TargetUnitsBefore[0].LoadState = "loaded"
	base.TargetUnitsBefore[0].UnitFileState = "disabled"
	state, exists, err := dnsengineartifact.SourceStateFromSwitchJournal(base)
	if err != nil || !exists {
		t.Fatalf("source state exists=%v err=%v", exists, err)
	}
	after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
	for i := range after {
		after[i].Data = append([]byte(nil), after[i].Data...)
		if after[i].Path == policy.PDNSManagedPath {
			after[i] = v4AgentSnapshot(policy.PDNSManagedPath, []byte("launch=gsqlite3\\n"))
			after[i].GID = 0
		}
	}
	local, err := bindconfig.ManagedZoneInclude("// original\n", "/var/cache/bind/celikpanel/current/zones.conf")
	if err != nil {
		t.Fatal(err)
	}
	source := dnsengineartifact.ManagedBINDSourceProofV4{
		Kind: dnsengineartifact.ManagedBINDSourceProofKindV4, HostLayout: "apt", Generation: state.Generation,
		EngineEpoch: state.EngineEpoch, ReceiptSHA256: strings.Repeat("e", 64),
		ConfigBefore: []dnsengineartifact.FileSnapshot{
			v4AgentSnapshot("/etc/bind/named.conf", []byte("include \"/etc/bind/named.conf.options\";\ninclude \"/etc/bind/named.conf.local\";\ninclude \"/etc/bind/named.conf.default-zones\";\n")),
			v4AgentSnapshot("/etc/bind/named.conf.default-zones", []byte("// defaults\n")),
			v4AgentSnapshot("/etc/bind/named.conf.local", []byte(local)),
			v4AgentSnapshot("/etc/bind/named.conf.options", []byte("// options\n")),
		},
	}
	candidate := dnsengineartifact.PDNSTargetCandidateProofV4{
		Path:   filepath.Join(filepath.Dir(policy.StatePath), ".celikpanel-switch-"+base.MutationRequestID+".sqlite3"),
		Device: 9, Inode: 11, Mode: 0o640, UID: 0, GID: 42, Size: 4096, SHA256: strings.Repeat("f", 64), NoSidecars: true,
	}
	journal, err := policy.BuildPDNSTargetInverseJournalV4(base, after, source, candidate)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func v4AgentSnapshot(path string, data []byte) dnsengineartifact.FileSnapshot {
	return dnsengineartifact.FileSnapshot{Path: path, Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 42,
		Data: data, SHA256: dnsengineartifact.DigestBytes(data)}
}

func TestV4PDNSTargetIsRefusedBySignedUpdatePreflightAndRecovery(t *testing.T) {
	ops := signedUpdateBINDPreflightOps(t)
	journal := testV4PDNSTargetJournal(t)
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) { return journal, true, nil }
	ops.detectProfile = func() (hostplatform.Profile, error) {
		t.Fatal("host probe ran for active V4 journal")
		return hostplatform.Profile{}, nil
	}
	err := checkBINDSignedUpdateCompatibleWithOps(context.Background(), ops)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "v4") {
		t.Fatalf("signed-update preflight did not explicitly refuse V4: %v", err)
	}

	prepare, _ := signedUpdateBINDPreparationOps(t)
	journal = testV4PDNSTargetJournal(t)
	prepare.readJournal = func() (dnsEngineSwitchJournal, bool, error) { return journal, true, nil }
	prepare.readLedger = func() (servicemutationledger.Ledger, error) {
		t.Fatal("ledger read ran for active V4 journal")
		return servicemutationledger.Ledger{}, nil
	}
	recovered, err := recoverDNSEngineSwitchJournalForSignedUpdate(context.Background(), prepare)
	if err == nil || recovered || !strings.Contains(strings.ToLower(err.Error()), "v4") {
		t.Fatalf("signed-update recovery result recovered=%v err=%v", recovered, err)
	}
}

func TestV4PDNSTargetRollbackNeverFallsThroughToV1SwitchInverse(t *testing.T) {
	journal := testV4PDNSTargetJournal(t)
	err := rollbackDNSSwitchJournal(context.Background(), journal)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "v4") {
		t.Fatalf("V4 rollback was not stopped by its explicit guard: %v", err)
	}
}
