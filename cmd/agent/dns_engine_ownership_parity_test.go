package main

import (
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// An ownership receipt left behind by an earlier epoch of the same engine is
// provenance, not a contradiction. Ownership files are per-engine and nothing
// retires them, so switching away from an engine and later switching back
// always finds one — and it can never equal the new state, because the epoch
// differs by construction. Refusing it poisons an ordinary operator action on
// every subsequent boot; accepting it lets the committed publish refresh it.
//
// The boundary matters in both directions: a receipt at an equal or higher
// epoch is a genuine contradiction and must still refuse.
func TestSupersededDNSEngineOwnershipBoundary(t *testing.T) {
	state := dnsEngineStateReceipt{
		Engine:            transport.DNSEngineBIND,
		EngineEpoch:       3,
		SourceRevision:    9,
		ManifestQualifier: "current",
		MutationRequestID: "11111111111111111111111111111111",
		MutationOwnerID:   "22222222222222222222222222222222",
	}

	tests := []struct {
		name       string
		ownership  dnsEngineStateReceipt
		superseded bool
	}{
		{
			name: "same engine at an older epoch is stale provenance",
			ownership: dnsEngineStateReceipt{
				Engine:            transport.DNSEngineBIND,
				EngineEpoch:       1,
				SourceRevision:    2,
				ManifestQualifier: "older",
				MutationRequestID: "33333333333333333333333333333333",
				MutationOwnerID:   "44444444444444444444444444444444",
			},
			superseded: true,
		},
		{
			name: "same epoch with different content is a contradiction",
			ownership: dnsEngineStateReceipt{
				Engine:            transport.DNSEngineBIND,
				EngineEpoch:       3,
				SourceRevision:    4,
				ManifestQualifier: "other",
				MutationRequestID: "55555555555555555555555555555555",
				MutationOwnerID:   "66666666666666666666666666666666",
			},
			superseded: false,
		},
		{
			name: "a receipt ahead of the committed state is refused",
			ownership: dnsEngineStateReceipt{
				Engine:      transport.DNSEngineBIND,
				EngineEpoch: 4,
			},
			superseded: false,
		},
		{
			name: "another engine's receipt is never this engine's provenance",
			ownership: dnsEngineStateReceipt{
				Engine:      transport.DNSEnginePowerDNS,
				EngineEpoch: 1,
			},
			superseded: false,
		},
		{
			name: "an epoch of zero is not a published epoch",
			ownership: dnsEngineStateReceipt{
				Engine:      transport.DNSEngineBIND,
				EngineEpoch: 0,
			},
			superseded: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := supersededDNSEngineOwnership(test.ownership, state); got != test.superseded {
				t.Fatalf("supersededDNSEngineOwnership = %v, want %v", got, test.superseded)
			}
		})
	}
}

// Every engine switch path must rebind a surviving install receipt when no
// package is missing. A rolled-back attempt restores config, state and units
// but touches neither the packages nor the receipt, so the retry finds nothing
// missing and — without this repair — carries a receipt bound to a dead request
// id straight into finalize, which poisons the operation permanently.
//
// BIND has had this branch since its own rollback bug; PowerDNS did not, and
// the omission was invisible because each engine's switch path is hand-written.
// This is a parity guard: it fails when a path loses the repair, and it fails
// for a third engine added without it. It reads source deliberately — the
// defect is "this call is absent from this function", which no behavioural test
// of the surrounding 200-line switch can express as cheaply.
func TestEveryDNSEngineSwitchPathRebindsSurvivingInstallOwnership(t *testing.T) {
	paths := []struct {
		file      string
		function  string
		terminate string
	}{
		{
			file:      "dns_engine_host.go",
			function:  "func (hostDNSEngineBackend) Switch(",
			terminate: "func bindConfigMutationSnapshots(",
		},
		{
			file:      "dns_engine_pdns_switch.go",
			function:  "func switchToPDNSOnCertifiedProfile(",
			terminate: "\nfunc ",
		},
	}

	for _, path := range paths {
		t.Run(path.file, func(t *testing.T) {
			raw, err := os.ReadFile(path.file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(raw)
			start := strings.Index(source, path.function)
			if start < 0 {
				t.Fatalf("switch entry point %q is missing", path.function)
			}
			body := source[start+len(path.function):]
			if end := strings.Index(body, path.terminate); end > 0 {
				body = body[:end]
			}
			if !strings.Contains(body, "handoffExistingDNSEngineInstallOwnership(") {
				t.Fatalf(
					"%s does not rebind a surviving install receipt; a rolled-back "+
						"attempt followed by a retry will poison the operation",
					path.function,
				)
			}
			if !strings.Contains(body, "len(missing) == 0") {
				t.Fatalf(
					"%s does not gate the rebind on an already-satisfied package set",
					path.function,
				)
			}
		})
	}
}
