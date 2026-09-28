//go:build linux

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnspeerjournal"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A forged in-memory active job is insufficient: the callback must prove both
// external locks and reread the durable ledger before minting a challenge.
func TestNativeBINDPeerAttemptRejectsRuntimeWithoutExternalLocks(t *testing.T) {
	job := &transport.ServiceMutationJob{
		RequestID: strings.Repeat("a", 32), OwnerID: strings.Repeat("b", 32),
		Kind: "dns_zone_sync", Target: "gone.example.test", PackageName: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		Attempt: 2,
	}
	runtime := &serviceMutationRuntime{job: job, steps: 1}
	manager := &serviceMutationManager{active: runtime}
	plan := dnsV3PrimaryPropagationPlan{
		Changed:   expectedDNSZoneAuthority{Domain: job.Target, Delete: true},
		Operation: dnsV3DeletionOperation{RequestID: job.RequestID, OwnerID: job.OwnerID, Qualifier: job.PackageName},
	}
	if attempt, err := currentBINDPeerLedgerAttempt(manager, runtime, plan); err == nil || attempt != 0 {
		t.Fatalf("unlocked active runtime authorized peer attempt %d: %v", attempt, err)
	}
}

func TestNativeBINDPeerProofRejectsUntrackedOrphanContext(t *testing.T) {
	plan := dnsV3PrimaryPropagationPlan{Changed: expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true}}
	err := verifyEnrolledBINDPeerDeletion(context.Background(), dnsPeerAXFRAuthority{}, plan)
	if err == nil || !strings.Contains(err.Error(), "active mutation attempt") {
		t.Fatalf("untracked orphan context must remain pending: %v", err)
	}
}

// The PDNS route supplies no native callback. A BIND completion may supply
// one, and a quick negative result cannot create repeated SSH/challenge work.
func TestNativeBINDProofIsOneAttemptPerCompletion(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := dnsV3PrimaryPropagationPlan{
		Evidence: evidence,
		Changed:  expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	localAXFR := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: evidence.Serial}, nil
	}
	peerCatalog := exactTestPeerCatalogAXFR(evidence)
	peerZone := absentTestPeerZoneAXFR(evidence)
	zoneSOACalls := 0
	soa := func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
		if domain == evidence.Domain {
			return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{evidence.Serial}}, nil
		}
		zoneSOACalls++
		return dnsSOAProbeResult{LocalIP: evidence.LocalIP, RCode: dnsRCodeRefused}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	calls := 0
	native := func(context.Context, dnsPeerAXFRAuthority, dnsV3PrimaryPropagationPlan) error {
		calls++
		return errors.New("peer unavailable")
	}
	err := completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, soa, localAXFR,
		peerCatalog, peerZone, native)
	if err == nil || calls != 1 ||
		pendingDNSPeerCode(err) != transport.DNSPeerPendingNativeUnknown ||
		strings.Contains(err.Error(), "peer unavailable") {
		t.Fatalf("BIND completion attempts=%d err=%v", calls, err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	typedWithRaw := func(context.Context, dnsPeerAXFRAuthority, dnsV3PrimaryPropagationPlan) error {
		return errors.Join(pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged),
			errors.New("private remote output"))
	}
	err = completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, soa, localAXFR,
		peerCatalog, peerZone, typedWithRaw)
	if err == nil || pendingDNSPeerCode(err) != transport.DNSPeerPendingEnrollmentChanged ||
		strings.Contains(err.Error(), "private remote output") {
		t.Fatalf("typed pending completion leaked callback output: %v", err)
	}
	// A single uncertain native inspection returns its reviewed reason now.
	// Repeating DNS probes cannot reuse the one challenge or mask that reason
	// with whichever generic check happens to run when the deadline expires.
	zoneSOACalls = 0
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, soa, localAXFR,
		peerCatalog, peerZone, typedWithRaw)
	if err == nil || pendingDNSPeerCode(err) != transport.DNSPeerPendingEnrollmentChanged ||
		zoneSOACalls != 2 || strings.Contains(err.Error(), "private remote output") {
		t.Fatalf("one-shot native reason lost after %d zone SOA probes: %v", zoneSOACalls, err)
	}
	// The PDNS/native-disabled route cannot turn REFUSED into success.
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, soa, localAXFR,
		peerCatalog, peerZone, nil); err == nil || calls != 1 {
		t.Fatalf("native-disabled completion accepted REFUSED or called native: %v / %d", err, calls)
	}
}

func TestNativePowerDNSProofRejectsUntrackedAndWrongSource(t *testing.T) {
	plan := dnsV3PrimaryPropagationPlan{
		SourceState: dnsEngineStateReceipt{
			Engine:   transport.DNSEnginePowerDNS,
			Mode:     transport.DNSEngineSwitchModeSwitch,
			PairRole: transport.DNSPairRolePrimary,
		},
		Changed: expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	if err := verifyEnrolledPDNSPeerDeletion(context.Background(), dnsPeerAXFRAuthority{}, plan); err == nil ||
		!strings.Contains(err.Error(), "active mutation attempt") {
		t.Fatalf("untracked PowerDNS primary could inspect a peer: %v", err)
	}
	for name, edit := range map[string]func(*dnsV3PrimaryPropagationPlan){
		"wrong engine": func(p *dnsV3PrimaryPropagationPlan) { p.SourceState.Engine = transport.DNSEngineBIND },
		"secondary":    func(p *dnsV3PrimaryPropagationPlan) { p.SourceState.PairRole = transport.DNSPairRoleSecondary },
		"legacy":       func(p *dnsV3PrimaryPropagationPlan) { p.SourceState.Mode = "" },
		"not deletion": func(p *dnsV3PrimaryPropagationPlan) { p.Changed.Delete = false },
	} {
		t.Run(name, func(t *testing.T) {
			changed := plan
			edit(&changed)
			if err := recheckPDNSPeerLocalEvidence(context.Background(), changed); err == nil {
				t.Fatal("unbound PowerDNS source accepted before native inspection")
			}
		})
	}
}

// Reproduce a crash after a terminal V3 ledger write but before the sidecar
// retirement. The next distinct deletion may retire only that exact old proof.
func TestNativeBINDPeerCrashGapRetiresHistoricalChallengeForNextDeletion(t *testing.T) {
	previous, ledger, nextPlan := historicalBINDPeerChallengeFixture(t)
	retired := false
	reads := 0
	read := func() (dnspeerjournal.RecordV1, error) {
		reads++
		if retired {
			return dnspeerjournal.RecordV1{}, dnspeerjournal.StateError{Code: dnspeerjournal.Missing}
		}
		return previous, nil
	}
	verifyCurrent := func() error {
		state := servicemutationledger.ClassifyDNSZoneV3PeerOperation(&ledger,
			nextPlan.Operation.RequestID, nextPlan.Operation.OwnerID,
			nextPlan.Changed.Domain, nextPlan.Operation.Qualifier, 1)
		if state != servicemutationledger.DNSZoneV3PeerOperationActiveApplied {
			return errors.New("new accepted deletion changed")
		}
		return nil
	}
	load := func() (serviceMutationLedger, error) { return ledger, nil }
	retire := func(request dnspeerproof.RequestV1, enrollment, digest string,
		attempt uint64, verify dnspeerjournal.VerifyCurrent) error {
		if retired || !reflect.DeepEqual(request, previous.Request) ||
			enrollment != previous.EnrollmentSHA256 || digest != previous.RequestSHA256 ||
			attempt != previous.LedgerAttempt || verify() != nil {
			t.Fatal("unverified historical challenge retired")
		}
		retired = true
		return nil
	}
	if err := reconcileHistoricalBINDPeerChallengeAt(nextPlan, verifyCurrent,
		load, read, retire); err != nil || !retired || reads != 2 {
		t.Fatalf("terminal crash gap retained: retired=%v reads=%d err=%v", retired, reads, err)
	}
	// A second independent deletion sees no stale challenge and need not
	// adopt the prior nonce, attempt, enrollment or operation identity.
	other := nextPlan
	other.Operation.RequestID = strings.Repeat("9", 32)
	if err := reconcileHistoricalBINDPeerChallengeAt(other, func() error { return nil },
		load, read, retire); err != nil || reads != 3 {
		t.Fatalf("retired journal blocked later deletion: reads=%d err=%v", reads, err)
	}
}

func TestNativeBINDPeerCrashGapKeepsUnknownOrNonterminalEvidence(t *testing.T) {
	previous, ledger, nextPlan := historicalBINDPeerChallengeFixture(t)
	cases := []struct {
		name      string
		edit      func(*serviceMutationLedger)
		readError error
	}{
		{name: "nonterminal", edit: func(l *serviceMutationLedger) {
			old := l.Jobs[previous.Request.MutationRequestID]
			old.Status = serviceMutationStatusPending
			old.Phase, _ = formatDNSZoneSyncV3Phase(dnsZoneSyncV3PropagationPending,
				old.RequestID, old.Target, old.PackageName)
			old.FinishedAt = time.Time{}
		}},
		{name: "wrong owner", edit: func(l *serviceMutationLedger) {
			l.Jobs[previous.Request.MutationRequestID].OwnerID = strings.Repeat("8", 32)
		}},
		{name: "wrong attempt", edit: func(l *serviceMutationLedger) {
			l.Jobs[previous.Request.MutationRequestID].Attempt++
		}},
		{name: "unsafe journal", readError: dnspeerjournal.StateError{Code: dnspeerjournal.Unknown}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneServiceMutationLedger(ledger)
			if tc.edit != nil {
				tc.edit(&changed)
			}
			retired := false
			read := func() (dnspeerjournal.RecordV1, error) {
				if tc.readError != nil {
					return dnspeerjournal.RecordV1{}, tc.readError
				}
				return previous, nil
			}
			retire := func(dnspeerproof.RequestV1, string, string, uint64,
				dnspeerjournal.VerifyCurrent) error {
				retired = true
				return nil
			}
			err := reconcileHistoricalBINDPeerChallengeAt(nextPlan, func() error { return nil },
				func() (serviceMutationLedger, error) { return changed, nil }, read, retire)
			if err == nil || retired {
				t.Fatalf("unsafe prior evidence retired=%v err=%v", retired, err)
			}
		})
	}
}

func historicalBINDPeerChallengeFixture(t *testing.T) (dnspeerjournal.RecordV1, serviceMutationLedger, dnsV3PrimaryPropagationPlan) {
	t.Helper()
	primary := "192.0.2.10"
	catalog, err := binddns.CatalogDomain(primary)
	if err != nil {
		t.Fatal(err)
	}
	members, err := dnspeerproof.CatalogMembersSHA256(primary, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	qualifier := "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64)
	request := dnspeerproof.RequestV1{
		Schema:            dnspeerproof.RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 2, DeletionQualifier: qualifier,
		PrimaryIP: primary, PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("e", 64),
		CatalogName: catalog, CatalogSerial: 2, CatalogMembersSHA256: members,
		DeletedZone: "old.example.test", View: dnspeerproof.DefaultView,
		Nonce: strings.Repeat("f", 64), Attempt: 1,
		IssuedAtUnix:  time.Now().Add(-time.Minute).Unix(),
		ExpiresAtUnix: time.Now().Add(-30 * time.Second).Unix(),
	}
	digest, err := dnspeerproof.RequestSHA256(request)
	if err != nil {
		t.Fatal(err)
	}
	previous := dnspeerjournal.RecordV1{
		Schema: dnspeerjournal.SchemaV1, State: dnspeerjournal.StateConsumed,
		LedgerAttempt: 1, EnrollmentSHA256: strings.Repeat("d", 64),
		RequestSHA256: digest, Request: request,
	}
	if err := previous.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	oldPhase, err := formatDNSZoneSyncV3PublishedPhase(request.MutationRequestID,
		request.DeletedZone, qualifier)
	if err != nil {
		t.Fatal(err)
	}
	old := &ServiceMutationJob{
		RequestID: request.MutationRequestID, OwnerID: request.MutationOwnerID,
		Kind: "dns_zone_sync", Target: request.DeletedZone, PackageName: qualifier,
		Status: serviceMutationStatusSucceeded, Phase: oldPhase, Attempt: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now, FinishedAt: now,
		DeadlineAt: now.Add(time.Hour),
	}
	nextID, nextOwner, nextZone := strings.Repeat("1", 32), strings.Repeat("2", 32), "next.example.test"
	nextPhase, err := formatDNSZoneSyncV3Phase(dnsZoneSyncV3Applied,
		nextID, nextZone, qualifier)
	if err != nil {
		t.Fatal(err)
	}
	next := &ServiceMutationJob{
		RequestID: nextID, OwnerID: nextOwner, Kind: "dns_zone_sync",
		Target: nextZone, PackageName: qualifier, Status: serviceMutationStatusRunning,
		Phase: nextPhase, Attempt: 1, StartedAt: now, UpdatedAt: now,
		LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
	}
	ledger := serviceMutationLedger{
		Version: serviceMutationLedgerVersion, ActiveRequestID: nextID,
		Jobs: map[string]*ServiceMutationJob{old.RequestID: old, next.RequestID: next},
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		t.Fatal(err)
	}
	plan := dnsV3PrimaryPropagationPlan{
		Changed: expectedDNSZoneAuthority{Domain: nextZone, Delete: true},
		Operation: dnsV3DeletionOperation{RequestID: nextID, OwnerID: nextOwner,
			Generation: 3, Qualifier: qualifier},
	}
	return previous, ledger, plan
}
