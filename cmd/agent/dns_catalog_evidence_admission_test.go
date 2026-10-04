package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Measured CATALOG-HASH shapes (base64 SHA-256); the values are arbitrary.
const (
	testCatalogHashBefore = "1Rf5aW9oZ2N0cmZ1c2hkZWxldGVyZXN0YW1wMDAwMDA="
	testCatalogHashAfter  = "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
)

func testRestampEvidence(serial uint32, hash string, members []string, memberSerials []uint32) dnsPrimaryCatalogEvidence {
	evidence := testPDNSPrimaryPropagationEvidence(serial, members, memberSerials)
	evidence.CatalogHash = hash
	return evidence
}

// One rule, one table: what a PowerDNS daemon re-stamp is and what it is not.
func TestClassifyProducerCatalogEvidence(t *testing.T) {
	recorded := testRestampEvidence(1790745229, testCatalogHashBefore,
		[]string{"a.example.test", "b.example.test"}, []uint32{41, 7})
	restamp := func(e *dnsPrimaryCatalogEvidence) {
		e.Serial = 1790745288
		e.CatalogHash = testCatalogHashAfter
	}
	for _, tc := range []struct {
		name   string
		engine transport.DNSEngine
		edit   func(*dnsPrimaryCatalogEvidence)
		want   dnsProducerCatalogVerdict
	}{
		{"identical", transport.DNSEnginePowerDNS, func(*dnsPrimaryCatalogEvidence) {}, dnsProducerCatalogUnchanged},
		{"daemon re-stamp admitted", transport.DNSEnginePowerDNS, restamp, dnsProducerCatalogDaemonRestamp},
		{"higher serial with a member added", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			restamp(e)
			e.Members = []string{"a.example.test", "b.example.test", "c.example.test"}
			e.MemberSerials = []uint32{41, 7, 1}
		}, dnsProducerCatalogDiffers},
		{"higher serial with a member removed", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			restamp(e)
			e.Members, e.MemberSerials = []string{"a.example.test"}, []uint32{41}
		}, dnsProducerCatalogDiffers},
		{"higher serial with a member serial changed", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			restamp(e)
			e.MemberSerials = []uint32{42, 7}
		}, dnsProducerCatalogDiffers},
		{"lower serial with a new hash", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			e.Serial = 1790745228
			e.CatalogHash = testCatalogHashAfter
		}, dnsProducerCatalogDiffers},
		{"same serial with a new hash", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			e.CatalogHash = testCatalogHashAfter
		}, dnsProducerCatalogDiffers},
		// Decided: the daemon rewrites serial and hash together; a higher
		// serial under the same hash is not its re-stamp.
		{"higher serial with the hash unchanged", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			e.Serial = 1790745288
		}, dnsProducerCatalogDiffers},
		{"higher serial with the hash row removed", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			e.Serial = 1790745288
			e.CatalogHash = ""
		}, dnsProducerCatalogDiffers},
		{"catalog identity changed", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			restamp(e)
			e.Domain = "catalog-c0000214.celikpanel.invalid"
		}, dnsProducerCatalogDiffers},
		{"producer address changed", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			restamp(e)
			e.LocalIP = "192.0.2.98"
		}, dnsProducerCatalogDiffers},
		{"peer address changed", transport.DNSEnginePowerDNS, func(e *dnsPrimaryCatalogEvidence) {
			restamp(e)
			e.PeerIP = "192.0.2.99"
		}, dnsProducerCatalogDiffers},
		{"BIND has no daemon re-stamp", transport.DNSEngineBIND, restamp, dnsProducerCatalogDiffers},
		{"BIND identical", transport.DNSEngineBIND, func(*dnsPrimaryCatalogEvidence) {}, dnsProducerCatalogUnchanged},
		{"unknown engine", "", restamp, dnsProducerCatalogDiffers},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := cloneDNSPrimaryCatalogEvidence(recorded)
			tc.edit(&observed)
			if got := classifyProducerCatalogEvidence(tc.engine, recorded, observed); got != tc.want {
				t.Fatalf("verdict=%d want %d", got, tc.want)
			}
		})
	}
	// Zero-zone catalog (batch 11 z04): an empty and a nil member list are
	// the same catalog, and its post-delete re-stamp is admitted.
	zero := testRestampEvidence(1790745229, testCatalogHashBefore, nil, nil)
	empty := testRestampEvidence(1790745229, testCatalogHashBefore, []string{}, []uint32{})
	if classifyProducerCatalogEvidence(transport.DNSEnginePowerDNS, zero, empty) != dnsProducerCatalogUnchanged {
		t.Fatal("empty and nil member lists differ")
	}
	empty.Serial, empty.CatalogHash = 1790745288, testCatalogHashAfter
	if classifyProducerCatalogEvidence(transport.DNSEnginePowerDNS, zero, empty) != dnsProducerCatalogDaemonRestamp {
		t.Fatal("zero-zone daemon re-stamp was refused")
	}
}

// The record is re-stamped before the proof continues, later checks read
// the re-stamped evidence back through the same shared record, and a
// difference other than a re-stamp names producer_catalog.
func TestRecordedProducerCatalogRestampIsReadBack(t *testing.T) {
	logs := captureAgentLog(t)
	before := testRestampEvidence(1790745229, testCatalogHashBefore, nil, nil)
	after := testRestampEvidence(1790745288, testCatalogHashAfter, nil, nil)
	reads := 0
	plan := dnsV3PrimaryPropagationPlan{
		SourceState: dnsEngineStateReceipt{Engine: transport.DNSEnginePowerDNS},
		Evidence:    before,
		Changed:     expectedDNSZoneAuthority{Domain: "s2.s1-kill.test", Delete: true},
		RefreshEvidence: func(context.Context) (dnsPrimaryCatalogEvidence, error) {
			reads++
			return after, nil
		},
	}
	record := newDNSRecordedProducerCatalog(plan)
	plan.catalog = record
	if recordedProducerCatalogFor(plan) != record {
		t.Fatal("the native proof would not share the wave's record")
	}
	if !record.RefreshAndAdmit(context.Background()) || reads != 1 {
		t.Fatal("daemon re-stamp was not admitted")
	}
	got := recordedProducerCatalogFor(plan).Evidence()
	if got.Serial != after.Serial || got.CatalogHash != after.CatalogHash || !record.RestampedSince(before.Serial) {
		t.Fatalf("re-stamped record not read back: %+v", got)
	}
	// The recorded evidence is now the re-stamped one: the old serial is a
	// lower serial and no longer matches.
	if verdict, err := record.Classify(before); verdict != dnsProducerCatalogDiffers || err == nil {
		t.Fatalf("old evidence still accepted: %d %v", verdict, err)
	}
	if verdict, err := record.Admit(after); verdict != dnsProducerCatalogUnchanged || err != nil {
		t.Fatalf("re-read of the re-stamped producer: %d %v", verdict, err)
	}
	if !strings.Contains(logs.String(), "admitted the PowerDNS daemon's re-stamp of catalog catalog-c000020a.celikpanel.invalid") ||
		!strings.Contains(logs.String(), "serial 1790745229 -> 1790745288") ||
		!strings.Contains(logs.String(), "re-stamp 1 of at most 3") {
		t.Fatalf("admission not logged: %q", logs.String())
	}
	// A member change is refused and named, the record keeps its evidence.
	edited := testRestampEvidence(1790745348, "Q0VMSUtQQU5FTC1URVNULU9XTkVSLUVESVQtSEFTSDA=",
		[]string{"owner.example.test"}, []uint32{1})
	_, err := record.Admit(edited)
	var edit *dnsPeerOwnerEditError
	if !errors.As(err, &edit) || edit.check != transport.DNSPeerOwnerEditCheckProducerCatalog || edit.retryable ||
		!strings.Contains(err.Error(), "serial recorded=1790745288 observed=1790745348") ||
		!strings.Contains(err.Error(), "first differing member #0 recorded=(none)/0 observed=owner.example.test/1") ||
		record.Evidence().Serial != after.Serial {
		t.Fatalf("owner edit admitted or unnamed: %v", err)
	}
	// A half-written re-stamp (higher serial, old hash) is refused but may
	// be read again.
	partial := after
	partial.Serial++
	if _, err := record.Classify(partial); !retryableDNSPeerOwnerEdit(err) {
		t.Fatalf("half-written re-stamp not retryable: %v", err)
	}
}

// A daemon that keeps re-stamping an unchanged member set is followed at
// most dnsProducerCatalogRestampLimit times in one attempt.
func TestRecordedProducerCatalogLoopGuard(t *testing.T) {
	evidence := testRestampEvidence(100, "", nil, nil)
	record := newDNSRecordedProducerCatalog(dnsV3PrimaryPropagationPlan{
		SourceState: dnsEngineStateReceipt{Engine: transport.DNSEnginePowerDNS},
		Evidence:    evidence,
	})
	hashes := []string{testCatalogHashBefore, testCatalogHashAfter, testCatalogHashBefore, testCatalogHashAfter}
	for index, hash := range hashes {
		evidence.Serial += 60
		evidence.CatalogHash = hash
		verdict, err := record.Admit(evidence)
		if index < dnsProducerCatalogRestampLimit {
			if err != nil || verdict != dnsProducerCatalogDaemonRestamp {
				t.Fatalf("re-stamp %d refused: %v", index+1, err)
			}
			continue
		}
		var edit *dnsPeerOwnerEditError
		if !errors.As(err, &edit) || edit.check != transport.DNSPeerOwnerEditCheckProducerCatalog ||
			!strings.Contains(err.Error(), "more than 3 times") || record.Evidence().Serial != 280 {
			t.Fatalf("re-stamp %d beyond the guard: %d %v", index+1, verdict, err)
		}
	}
}

// Each owner-edit observation carries its reviewed check into the durable
// code; untyped text keeps the plain code; the log names the check.
func TestPeerOwnerEditCarriesTheCheckThatDiffered(t *testing.T) {
	logs := captureAgentLog(t)
	ok := func() error { return nil }
	attempt := func() error { return errors.New("native BIND proof lost its active operation attempt") }
	err := peerCurrentPendingCodeAt(attempt, ok, ok)
	if got := pendingDNSPeerCode(err); got != "dns_peer_owner_edit_unknown:operation_attempt" ||
		dnsZoneV3PendingLedgerCode(got) != got {
		t.Fatalf("attempt code=%q", got)
	}
	if !strings.Contains(logs.String(), "check=operation_attempt") ||
		!strings.Contains(logs.String(), "lost its active operation attempt") {
		t.Fatalf("attempt check not logged: %q", logs.String())
	}
	for _, check := range []string{
		transport.DNSPeerOwnerEditCheckEngineState, transport.DNSPeerOwnerEditCheckActiveEngine,
		transport.DNSPeerOwnerEditCheckNativeBinding, transport.DNSPeerOwnerEditCheckDeletionReceipt,
		transport.DNSPeerOwnerEditCheckProducerCatalog,
	} {
		logs.Reset()
		local := func() error {
			return dnsPeerOwnerEditf(check, "recorded value=1; observed value=2")
		}
		code := pendingDNSPeerCode(peerCurrentPendingCodeAt(ok, ok, local))
		reason, detail, valid := transport.SplitDNSPeerPendingCode(code)
		if !valid || reason != transport.DNSPeerPendingOwnerEditUnknown || detail != check ||
			!strings.Contains(logs.String(), "check="+check) ||
			!strings.Contains(logs.String(), "recorded value=1; observed value=2") {
			t.Fatalf("%s: code=%q log=%q", check, code, logs.String())
		}
	}
	logs.Reset()
	untyped := func() error { return errors.New("something observed") }
	if got := pendingDNSPeerCode(peerCurrentPendingCodeAt(ok, ok, untyped)); got != transport.DNSPeerPendingOwnerEditUnknown ||
		!strings.Contains(logs.String(), "check=unclassified") {
		t.Fatalf("untyped code=%q log=%q", got, logs.String())
	}
}

// fakeRestampPair is a PowerDNS primary whose daemon re-stamps its producer
// catalog, and a secondary that transfers it. Both serve only the catalog;
// the deleted zone is refused (the parentless case).
type fakeRestampPair struct {
	mu       sync.Mutex
	producer dnsPrimaryCatalogEvidence
	peer     dnsPrimaryCatalogEvidence
	// peerLag keeps the secondary on its previous catalog for this many
	// catalog transfers after a re-stamp.
	peerLag int
}

func (f *fakeRestampPair) db() dnsPrimaryCatalogEvidence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneDNSPrimaryCatalogEvidence(f.producer)
}

// restamp is the daemon's own write: new epoch serial and hash, same members.
func (f *fakeRestampPair) restamp(serial uint32, hash string, peerLag int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.producer.Serial, f.producer.CatalogHash = serial, hash
	f.peerLag = peerLag
	if peerLag == 0 {
		f.peer = cloneDNSPrimaryCatalogEvidence(f.producer)
	}
}

func (f *fakeRestampPair) ownerAddsMember(member string, serial uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.producer.Members = append(f.producer.Members, member)
	f.producer.MemberSerials = append(f.producer.MemberSerials, serial)
	f.producer.Serial++
	f.peer = cloneDNSPrimaryCatalogEvidence(f.producer)
}

func (f *fakeRestampPair) probes() nativePeerAfterInspectionProbes {
	return nativePeerAfterInspectionProbes{
		soa: func(_ context.Context, _, address, domain string) (dnsSOAProbeResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if domain == f.producer.Domain {
				serial := f.producer.Serial
				if address == f.producer.PeerIP {
					serial = f.peer.Serial
				}
				return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{serial}}, nil
			}
			for index, member := range f.producer.Members {
				if member == domain {
					return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError,
						SOASerials: []uint32{f.producer.MemberSerials[index]}}, nil
				}
			}
			return dnsSOAProbeResult{LocalIP: f.producer.LocalIP, RCode: dnsRCodeRefused}, nil
		},
		localCatalog: func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return dnsCatalogAXFRResult{Serial: f.producer.Serial, Members: append([]string(nil), f.producer.Members...)}, nil
		},
		peerCatalog: func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			result := dnsCatalogAXFRResult{Serial: f.peer.Serial, Members: append([]string(nil), f.peer.Members...)}
			if f.peerLag > 0 {
				f.peerLag--
				if f.peerLag == 0 {
					f.peer = cloneDNSPrimaryCatalogEvidence(f.producer)
				}
			}
			return result, nil
		},
		peerZone: func(context.Context, string, string, string) (dnsZoneAXFRState, error) {
			return dnsZoneAXFRNoTransfer, nil
		},
	}
}

func (f *fakeRestampPair) plan(domain string) dnsV3PrimaryPropagationPlan {
	return dnsV3PrimaryPropagationPlan{
		SourceState: dnsEngineStateReceipt{Engine: transport.DNSEnginePowerDNS},
		Evidence:    f.db(),
		Changed:     expectedDNSZoneAuthority{Domain: domain, Delete: true},
		RefreshEvidence: func(context.Context) (dnsPrimaryCatalogEvidence, error) {
			return f.db(), nil
		},
	}
}

// fakeNativePeerProof follows verifyEnrolled*PeerDeletion's contract with the
// production helpers: the current-evidence recheck judges the producer
// against the shared record (admitting a re-stamp), no challenge is minted
// when the record moved past the proved pair, and after the inspection the
// shared post-inspection checks run.
type fakeNativePeerProof struct {
	pair     *fakeRestampPair
	enrolled bool
	// beforeRecheck and afterMint run inside the proof, where the daemon's
	// periodic check may fire.
	beforeRecheck func(call int)
	afterMint     func()
	calls         int
	minted        []uint32
}

func (p *fakeNativePeerProof) proof(ctx context.Context, authority dnsPeerAXFRAuthority, plan dnsV3PrimaryPropagationPlan) error {
	p.calls++
	if !p.enrolled {
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentRequired)
	}
	record := recordedProducerCatalogFor(plan)
	ok := func() error { return nil }
	verifyCurrent := func() error {
		return peerCurrentPendingCodeAt(ok, ok, func() error {
			_, err := record.Admit(p.pair.db())
			return err
		})
	}
	if p.beforeRecheck != nil {
		p.beforeRecheck(p.calls)
	}
	if err := verifyCurrent(); err != nil {
		return err
	}
	plan, err := nativePeerChallengePlan(plan, record, authority)
	if err != nil {
		return err
	}
	if plan.Evidence.Serial != authority.catalogSerial {
		return errors.New("challenge minted from a serial the pair did not prove")
	}
	p.minted = append(p.minted, authority.catalogSerial)
	if p.afterMint != nil {
		p.afterMint()
	}
	if err := verifyCurrent(); err != nil {
		return err
	}
	if err := verifyNativePeerAfterInspectionAt(ctx, record, authority, plan.Changed.Domain,
		p.pair.probes(), verifyCurrent); err != nil {
		return err
	}
	return verifyCurrent()
}

func (p *fakeNativePeerProof) complete(t *testing.T, plan dnsV3PrimaryPropagationPlan) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probes := p.pair.probes()
	return completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, probes.soa, probes.localCatalog,
		probes.peerCatalog, probes.peerZone, p.proof)
}

func newZeroZoneRestampPair() *fakeRestampPair {
	// Batch 11 z04: after the delete the zero-member catalog is at
	// 1790745229 under the hash of the one-member set.
	producer := testRestampEvidence(1790745229, testCatalogHashBefore, nil, nil)
	return &fakeRestampPair{producer: producer, peer: cloneDNSPrimaryCatalogEvidence(producer)}
}

// Batch 11: a parentless deletion pending on enrollment is resumed after the
// owner enrolls. The daemon re-stamps the empty catalog either between the
// pending attempt and the resume (z05) or inside the resumed attempt (z04),
// or after the challenge was minted. Each resumed attempt completes; none
// is an owner edit.
func TestResumedDeletionCompletesAcrossDaemonRestamp(t *testing.T) {
	const zone = "s2.s1-kill.test"
	t.Run("pending then re-stamp then resume (z05)", func(t *testing.T) {
		pair := newZeroZoneRestampPair()
		native := &fakeNativePeerProof{pair: pair}
		err := native.complete(t, pair.plan(zone))
		if pendingDNSPeerCode(err) != transport.DNSPeerPendingEnrollmentRequired {
			t.Fatalf("publication attempt: %v", err)
		}
		pair.restamp(1790745288, testCatalogHashAfter, 0)
		native.enrolled = true
		native.calls = 0
		// RecoverDNSZoneV3 builds its plan from the producer at resume.
		if err := native.complete(t, pair.plan(zone)); err != nil || native.calls != 1 ||
			len(native.minted) != 1 || native.minted[0] != 1790745288 {
			t.Fatalf("resume: err=%v calls=%d minted=%v", err, native.calls, native.minted)
		}
	})
	t.Run("re-stamp inside the resumed attempt before the challenge (z04)", func(t *testing.T) {
		logs := captureAgentLog(t)
		pair := newZeroZoneRestampPair()
		native := &fakeNativePeerProof{pair: pair, enrolled: true}
		native.beforeRecheck = func(call int) {
			if call == 1 {
				pair.restamp(1790745288, testCatalogHashAfter, 0)
			}
		}
		plan := pair.plan(zone)
		if err := native.complete(t, plan); err != nil || native.calls != 2 ||
			len(native.minted) != 1 || native.minted[0] != 1790745288 {
			t.Fatalf("resume: err=%v calls=%d minted=%v", err, native.calls, native.minted)
		}
		if !strings.Contains(logs.String(), "serial 1790745229 -> 1790745288") ||
			strings.Contains(logs.String(), "observed different evidence") {
			t.Fatalf("log: %q", logs.String())
		}
	})
	t.Run("re-stamp after the challenge, secondary transfers late", func(t *testing.T) {
		pair := newZeroZoneRestampPair()
		native := &fakeNativePeerProof{pair: pair, enrolled: true}
		native.afterMint = func() { pair.restamp(1790745288, testCatalogHashAfter, 2) }
		if err := native.complete(t, pair.plan(zone)); err != nil || native.calls != 1 ||
			len(native.minted) != 1 || native.minted[0] != 1790745229 {
			t.Fatalf("resume: err=%v calls=%d minted=%v", err, native.calls, native.minted)
		}
	})
	t.Run("owner member change during the attempt stays an owner edit", func(t *testing.T) {
		pair := newZeroZoneRestampPair()
		native := &fakeNativePeerProof{pair: pair, enrolled: true}
		native.beforeRecheck = func(int) { pair.ownerAddsMember("owner.example.test", 1) }
		err := native.complete(t, pair.plan(zone))
		if got := pendingDNSPeerCode(err); got != "dns_peer_owner_edit_unknown:producer_catalog" ||
			len(native.minted) != 0 {
			t.Fatalf("owner edit: code=%q minted=%v err=%v", got, native.minted, err)
		}
	})
	t.Run("continuous re-stamping is refused after the loop guard", func(t *testing.T) {
		pair := newZeroZoneRestampPair()
		native := &fakeNativePeerProof{pair: pair, enrolled: true}
		serial := uint32(1790745229)
		native.beforeRecheck = func(call int) {
			serial += 60
			hash := testCatalogHashAfter
			if call%2 == 0 {
				hash = testCatalogHashBefore
			}
			pair.restamp(serial, hash, 0)
		}
		err := native.complete(t, pair.plan(zone))
		if got := pendingDNSPeerCode(err); got != "dns_peer_owner_edit_unknown:producer_catalog" ||
			native.calls != dnsProducerCatalogRestampLimit+1 || len(native.minted) != 0 {
			t.Fatalf("loop guard: code=%q calls=%d minted=%v err=%v", got, native.calls, native.minted, err)
		}
	})
}

// The add/edit publication wave follows the same rule (no native proof).
func TestPublicationWaveAdmitsOnlyTheDaemonRestamp(t *testing.T) {
	member := "example.test"
	producer := testRestampEvidence(1790745169, testCatalogHashBefore, []string{member}, []uint32{2026092801})
	pair := &fakeRestampPair{producer: producer, peer: cloneDNSPrimaryCatalogEvidence(producer)}
	plan := pair.plan(member)
	plan.Changed = expectedDNSZoneAuthority{Domain: member, Serial: 2026092801}
	pair.restamp(1790745228, testCatalogHashAfter, 0)
	probes := pair.probes()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, probes.soa, probes.localCatalog,
		probes.peerCatalog, probes.peerZone, nil); err != nil {
		t.Fatalf("publication did not follow the daemon re-stamp: %v", err)
	}
	pair.ownerAddsMember("zz-owner.example.test", 1)
	plan = pair.plan(member)
	plan.Evidence = producer
	plan.Changed = expectedDNSZoneAuthority{Domain: member, Serial: 2026092801}
	ctx, cancel = context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if err := completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, probes.soa, probes.localCatalog,
		probes.peerCatalog, probes.peerZone, nil); err == nil || !strings.Contains(err.Error(), "check=catalog_pair") {
		t.Fatalf("publication followed an owner member change: %v", err)
	}
}

// The post-inspection checks name the check that differed.
func TestNativePeerAfterInspectionNamesTheCheck(t *testing.T) {
	const zone = "s2.s1-kill.test"
	base := func() (*fakeRestampPair, *dnsRecordedProducerCatalog, dnsPeerAXFRAuthority) {
		pair := newZeroZoneRestampPair()
		record := newDNSRecordedProducerCatalog(pair.plan(zone))
		evidence := pair.db()
		return pair, record, dnsPeerAXFRAuthority{sourceIP: evidence.LocalIP, peerIP: evidence.PeerIP,
			catalog: evidence.Domain, catalogSerial: evidence.Serial}
	}
	ok := func() error { return nil }
	for _, tc := range []struct {
		name  string
		setup func(*fakeRestampPair, *nativePeerAfterInspectionProbes, *dnsPeerAXFRAuthority)
		want  string
	}{
		{"all hold", func(*fakeRestampPair, *nativePeerAfterInspectionProbes, *dnsPeerAXFRAuthority) {}, ""},
		{"catalog probe", func(pair *fakeRestampPair, _ *nativePeerAfterInspectionProbes, _ *dnsPeerAXFRAuthority) {
			pair.mu.Lock()
			pair.peer.Serial = 1790745100
			pair.mu.Unlock()
		}, "dns_peer_owner_edit_unknown:catalog_probe"},
		{"authority", func(_ *fakeRestampPair, _ *nativePeerAfterInspectionProbes, authority *dnsPeerAXFRAuthority) {
			authority.catalog = "catalog-c0000214.celikpanel.invalid"
		}, "dns_peer_owner_edit_unknown:authority"},
		{"transfer observed", func(_ *fakeRestampPair, probes *nativePeerAfterInspectionProbes, _ *dnsPeerAXFRAuthority) {
			probes.peerZone = func(context.Context, string, string, string) (dnsZoneAXFRState, error) {
				return dnsZoneAXFRPresent, nil
			}
		}, "dns_peer_owner_edit_unknown:transfer_observed"},
		{"zone answered", func(pair *fakeRestampPair, probes *nativePeerAfterInspectionProbes, _ *dnsPeerAXFRAuthority) {
			catalogSOA := probes.soa
			probes.soa = func(ctx context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
				if domain == zone {
					return dnsSOAProbeResult{LocalIP: pair.producer.LocalIP, Authoritative: true,
						RCode: dnsRCodeNoError, AnswerCount: 1, SOASerials: []uint32{2026092802}}, nil
				}
				return catalogSOA(ctx, network, address, domain)
			}
		}, "dns_peer_owner_edit_unknown:zone_answered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureAgentLog(t)
			pair, record, authority := base()
			probes := pair.probes()
			tc.setup(pair, &probes, &authority)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := verifyNativePeerAfterInspectionAt(ctx, record, authority, zone, probes, ok)
			if got := pendingDNSPeerCode(err); got != tc.want || (tc.want == "") != (err == nil) {
				t.Fatalf("code=%q err=%v", got, err)
			}
			if tc.want != "" {
				_, check, _ := strings.Cut(tc.want, ":")
				if !strings.Contains(logs.String(), "check="+check) {
					t.Fatalf("check not logged: %q", logs.String())
				}
			}
		})
	}
}
