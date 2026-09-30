//go:build linux

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerenrollment"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnspeerenrollment"
	"github.com/alicelik/celikpanel/internal/pdnspeerjournal"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnspeertransport"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestNativePeerEngineSelectionFailsClosed(t *testing.T) {
	authority := dnsPeerAXFRAuthority{sourceIP: "192.0.2.10", peerIP: "192.0.2.11", catalog: "catalog-c000020a.celikpanel.invalid"}
	bind := dnspeerenrollment.Snapshot{Record: dnspeerenrollment.RecordV1{
		PrimaryIP: authority.sourceIP, PeerIP: authority.peerIP, CatalogName: authority.catalog,
		View: dnspeerproof.DefaultView,
	}}
	pdns := pdnspeerenrollment.Snapshot{Record: pdnspeerenrollment.RecordV1{
		Engine: "pdns", PrimaryIP: authority.sourceIP, PeerIP: authority.peerIP,
		CatalogName: authority.catalog, View: dnspeerproof.DefaultView,
	}}
	disabledBIND := dnspeerenrollment.StateError{Code: dnspeerenrollment.Disabled}
	disabledPDNS := pdnspeerenrollment.StateError{Code: pdnspeerenrollment.Disabled}
	tests := []struct {
		name    string
		bind    dnspeerenrollment.Snapshot
		bindErr error
		pdns    pdnspeerenrollment.Snapshot
		pdnsErr error
		want    nativePeerEngine
		code    string
	}{
		{name: "bind", bind: bind, pdnsErr: disabledPDNS, want: nativePeerBIND},
		{name: "pdns", bindErr: disabledBIND, pdns: pdns, want: nativePeerPDNS},
		{name: "none", bindErr: disabledBIND, pdnsErr: disabledPDNS, code: transport.DNSPeerPendingEnrollmentRequired},
		{name: "ambiguous", bind: bind, pdns: pdns, code: transport.DNSPeerPendingEnrollmentChanged},
		{name: "unsafe bind", bindErr: dnspeerenrollment.StateError{Code: dnspeerenrollment.Unknown}, pdns: pdns, code: transport.DNSPeerPendingEnrollmentChanged},
		{name: "unsafe pdns", bind: bind, pdnsErr: pdnspeerenrollment.StateError{Code: pdnspeerenrollment.Unknown}, code: transport.DNSPeerPendingEnrollmentChanged},
		{name: "wrong pdns peer", bindErr: disabledBIND, pdns: func() pdnspeerenrollment.Snapshot { c := pdns; c.Record.PeerIP = "192.0.2.12"; return c }(), code: transport.DNSPeerPendingEnrollmentChanged},
		{name: "wrong pdns engine", bindErr: disabledBIND, pdns: func() pdnspeerenrollment.Snapshot { c := pdns; c.Record.Engine = "bind"; return c }(), code: transport.DNSPeerPendingEnrollmentChanged},
		{name: "wrong bind catalog", bind: func() dnspeerenrollment.Snapshot { c := bind; c.Record.CatalogName = "other.example.test"; return c }(), pdnsErr: disabledPDNS, code: transport.DNSPeerPendingEnrollmentChanged},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseNativePeerEngineAt(authority,
				func() (dnspeerenrollment.Snapshot, error) { return tc.bind, tc.bindErr },
				func() (pdnspeerenrollment.Snapshot, error) { return tc.pdns, tc.pdnsErr })
			if got != tc.want || pendingDNSPeerCode(err) != tc.code {
				t.Fatalf("selected %q code %q; want %q %q", got, pendingDNSPeerCode(err), tc.want, tc.code)
			}
		})
	}
}

func TestNativePDNSProofRejectsUntrackedContext(t *testing.T) {
	plan := dnsV3PrimaryPropagationPlan{Changed: expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true}}
	if err := verifyEnrolledPDNSPeerDeletion(context.Background(), dnsPeerAXFRAuthority{}, plan); err == nil || !strings.Contains(err.Error(), "active mutation attempt") {
		t.Fatalf("untracked context authorized PowerDNS challenge: %v", err)
	}
}

func TestNativePDNSHistoricalChallengeRetiresOnlyExactTerminal(t *testing.T) {
	base, ledger, next := historicalBINDPeerChallengeFixture(t)
	request := pdnspeerproof.RequestV1(base.Request)
	request.Schema = pdnspeerproof.RequestSchemaV1
	digest, err := pdnspeerproof.RequestSHA256(request)
	if err != nil {
		t.Fatal(err)
	}
	previous := pdnspeerjournal.RecordV1{
		Schema: pdnspeerjournal.SchemaV1, State: pdnspeerjournal.StateConsumed,
		LedgerAttempt: base.LedgerAttempt, EnrollmentSHA256: base.EnrollmentSHA256,
		RequestSHA256: digest, Request: request,
	}
	if err := previous.Validate(); err != nil {
		t.Fatal(err)
	}
	reads, retired := 0, false
	read := func() (pdnspeerjournal.RecordV1, error) {
		reads++
		if retired {
			return pdnspeerjournal.RecordV1{}, pdnspeerjournal.StateError{Code: pdnspeerjournal.Missing}
		}
		return previous, nil
	}
	current := func() error {
		if servicemutationledger.ClassifyDNSZoneV3PeerOperation(&ledger,
			next.Operation.RequestID, next.Operation.OwnerID, next.Changed.Domain, next.Operation.Qualifier, 1) != servicemutationledger.DNSZoneV3PeerOperationActiveApplied {
			return errors.New("current operation changed")
		}
		return nil
	}
	retire := func(r pdnspeerproof.RequestV1, enrollment, gotDigest string, attempt uint64, verify pdnspeerjournal.VerifyCurrent) error {
		if !reflect.DeepEqual(r, request) || enrollment != previous.EnrollmentSHA256 || gotDigest != digest ||
			attempt != previous.LedgerAttempt || verify() != nil {
			t.Fatal("unverified retirement")
		}
		retired = true
		return nil
	}
	if err := reconcileHistoricalPDNSPeerChallengeAt(next, current,
		func() (serviceMutationLedger, error) { return ledger, nil }, read, retire); err != nil || !retired || reads != 2 {
		t.Fatalf("exact prior result not retired: %v retired=%v reads=%d", err, retired, reads)
	}
	for _, edit := range []func(*serviceMutationLedger){
		func(l *serviceMutationLedger) {
			l.Jobs[previous.Request.MutationRequestID].OwnerID = strings.Repeat("8", 32)
		},
		func(l *serviceMutationLedger) { l.Jobs[previous.Request.MutationRequestID].Attempt++ },
		func(l *serviceMutationLedger) {
			old := l.Jobs[previous.Request.MutationRequestID]
			old.Status = serviceMutationStatusPending
			old.FinishedAt = time.Time{}
		},
	} {
		changed := cloneServiceMutationLedger(ledger)
		edit(&changed)
		retired = false
		if err := reconcileHistoricalPDNSPeerChallengeAt(next, func() error { return nil },
			func() (serviceMutationLedger, error) { return changed, nil },
			func() (pdnspeerjournal.RecordV1, error) { return previous, nil }, retire); err == nil || retired {
			t.Fatalf("nonterminal history retired: %v", err)
		}
	}
}

// The PowerDNS path maps an authenticated inspector's reviewed reason exactly
// as the BIND path does; anything else stays the plain inspection code.
func TestPDNSInspectionPendingCodeCarriesTheInspectorReason(t *testing.T) {
	for err, want := range map[error]string{
		pdnspeertransport.Unknown{Code: pdnspeertransport.CodeUnavailable, Reason: "config_unreviewed"}: "dns_peer_inspection_unknown:config_unreviewed",
		pdnspeertransport.Unknown{Code: pdnspeertransport.CodeUnavailable}:                              transport.DNSPeerPendingInspectionUnknown,
		pdnspeertransport.Unknown{Code: pdnspeertransport.CodeMalformed, Reason: "config_unreviewed"}:   transport.DNSPeerPendingInspectionUnknown,
		errors.New("config_unreviewed"): transport.DNSPeerPendingInspectionUnknown,
	} {
		got := pdnsInspectionPendingCode(err)
		if got != want || !transport.ValidDNSPeerPendingCode(got) {
			t.Fatalf("%v -> %q, want %q", err, got, want)
		}
		if pendingDNSPeerCode(pendingBINDPeer(got)) != want || dnsZoneV3PendingLedgerCode(got) != want {
			t.Fatalf("%v lost through the pending error or ledger", err)
		}
	}
}
