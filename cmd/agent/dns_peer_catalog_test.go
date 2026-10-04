package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	peerCatalogTestLocal  = "192.0.2.20"
	peerCatalogTestPeer   = "192.0.2.10"
	peerCatalogTestMember = "s1-kill.test"
	// Captured from a native PowerDNS 4.9.17 producer (see
	// TestPowerDNSCatalogAXFRProducerIsExplicitAndBounded).
	peerCatalogTestPDNSLabel = "lf5eijnqp9ob8kmq5mv0vhjaevtcfuus"
)

func peerCatalogTestDomain(t *testing.T) string {
	t.Helper()
	domain, err := binddns.CatalogDomain(peerCatalogTestPeer)
	if err != nil {
		t.Fatal(err)
	}
	return domain
}

type peerCatalogTestShape int

const (
	peerCatalogBIND peerCatalogTestShape = iota
	peerCatalogPowerDNS
	// PowerDNS member labels with BIND TTLs: matches neither producer.
	peerCatalogMixed
	// PowerDNS format with a foreign SOA timer: refused in both formats by a
	// check they share, so it is never retried.
	peerCatalogPowerDNSBadSOA
	// BIND format with a TTL neither producer emits.
	peerCatalogBINDBadTTL
)

func peerCatalogTestRecords(t *testing.T, catalog string, shape peerCatalogTestShape) []catalogAXFRTestRR {
	t.Helper()
	records := exactCatalogAXFRTestRecords(t, catalog, peerCatalogTestMember)
	switch shape {
	case peerCatalogPowerDNS:
		records[2].ttl, records[3].ttl = 0, 0
		records[3].owner = peerCatalogTestPDNSLabel + ".zones." + catalog
	case peerCatalogMixed:
		records[3].owner = peerCatalogTestPDNSLabel + ".zones." + catalog
	case peerCatalogPowerDNSBadSOA:
		records[2].ttl, records[3].ttl = 0, 0
		records[3].owner = peerCatalogTestPDNSLabel + ".zones." + catalog
		records[0] = catalogAXFRTestSOA(t, catalog, "invalid", "invalid", 17, 60, 30, 86400, 30)
		records[4] = records[0]
	case peerCatalogBINDBadTTL:
		records[2].ttl = 61
	}
	return records
}

// peerCatalogWireRead parses a fresh copy of the transfer on every call with
// the requested producer, as a real second transfer would be read.
func peerCatalogWireRead(t *testing.T, catalog string, records []catalogAXFRTestRR) func(dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
	t.Helper()
	const id = uint16(0x5150)
	message := buildCatalogAXFRTestMessage(t, id, catalog, dnsResponseQR|dnsResponseAA, true, records, nil, nil)
	stream := frameCatalogAXFRTestMessages(message)
	return func(producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
		return readDNSCatalogAXFRWithProducer(bytes.NewReader(append([]byte(nil), stream...)), id, catalog, producer)
	}
}

func quietPeerCatalogLog(t *testing.T) *int {
	t.Helper()
	previous := logDNSPeerCatalogProducer
	count := 0
	logDNSPeerCatalogProducer = func(string, ...any) { count++ }
	t.Cleanup(func() { logDNSPeerCatalogProducer = previous })
	return &count
}

func TestCatalogAXFRFormatRefusalIsProducerSpecific(t *testing.T) {
	catalog := peerCatalogTestDomain(t)
	for _, tc := range []struct {
		name       string
		shape      peerCatalogTestShape
		producer   dnsCatalogAXFRProducer
		wantFormat bool
		wantOK     bool
	}{
		{"BIND reader, BIND catalog", peerCatalogBIND, dnsCatalogAXFRBIND, false, true},
		{"BIND reader, PowerDNS catalog", peerCatalogPowerDNS, dnsCatalogAXFRBIND, true, false},
		{"PowerDNS reader, PowerDNS catalog", peerCatalogPowerDNS, dnsCatalogAXFRPowerDNS, false, true},
		{"PowerDNS reader, BIND catalog", peerCatalogBIND, dnsCatalogAXFRPowerDNS, true, false},
		{"BIND reader, PowerDNS labels with BIND TTLs", peerCatalogMixed, dnsCatalogAXFRBIND, true, false},
		{"BIND reader, shared SOA refusal", peerCatalogPowerDNSBadSOA, dnsCatalogAXFRBIND, false, false},
		{"BIND reader, foreign TTL", peerCatalogBINDBadTTL, dnsCatalogAXFRBIND, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, tc.shape))(tc.producer)
			if (err == nil) != tc.wantOK {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if got := errors.Is(err, errDNSCatalogAXFRProducerFormat); got != tc.wantFormat {
				t.Fatalf("format refusal=%v, want %v (err=%v)", got, tc.wantFormat, err)
			}
			if tc.wantOK && (result.Producer != tc.producer || result.Serial != 17 ||
				len(result.Members) != 1 || result.Members[0] != peerCatalogTestMember) {
				t.Fatalf("accepted result is not exact: %+v", result)
			}
		})
	}
	// A 56-hex label that is not the member's SHA-224 is neither producer's.
	records := peerCatalogTestRecords(t, catalog, peerCatalogBIND)
	records[3].owner = strings.Repeat("0", 56) + ".zones." + catalog
	if _, err := peerCatalogWireRead(t, catalog, records)(dnsCatalogAXFRBIND); err == nil ||
		errors.Is(err, errDNSCatalogAXFRProducerFormat) {
		t.Fatalf("wrong BIND hash label was classified as another producer: %v", err)
	}
}

func TestPeerCatalogProducerSelection(t *testing.T) {
	quietPeerCatalogLog(t)
	catalog := peerCatalogTestDomain(t)
	transportErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	refusedFlags := errors.New("BIND catalog AXFR response flags are not exact")
	for _, tc := range []struct {
		name         string
		read         func(dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error)
		wantProducer dnsCatalogAXFRProducer
		wantCalls    []dnsCatalogAXFRProducer
		check        func(*testing.T, error)
	}{
		{
			name:         "BIND catalog accepted without a second transfer",
			read:         peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogBIND)),
			wantProducer: dnsCatalogAXFRBIND,
			wantCalls:    []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND},
		},
		{
			name:         "PowerDNS catalog accepted after the BIND format refusal",
			read:         peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogPowerDNS)),
			wantProducer: dnsCatalogAXFRPowerDNS,
			wantCalls:    []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND, dnsCatalogAXFRPowerDNS},
		},
		{
			name:      "neither format names both reasons",
			read:      peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogMixed)),
			wantCalls: []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND, dnsCatalogAXFRPowerDNS},
			check: func(t *testing.T, err error) {
				var formatErr *dnsPeerCatalogFormatError
				if !errors.As(err, &formatErr) || !strings.Contains(err.Error(), "BIND format: member PTR is not exact") ||
					!strings.Contains(err.Error(), "PowerDNS format: record class or TTL is not exact") ||
					!strings.Contains(err.Error(), catalog) {
					t.Fatalf("error does not name both refusals: %v", err)
				}
			},
		},
		{
			name: "transport error is not retried",
			read: func(dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
				return dnsCatalogAXFRResult{}, transportErr
			},
			wantCalls: []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, transportErr) {
					t.Fatalf("transport error changed: %v", err)
				}
			},
		},
		{
			name: "truncated transfer is not retried",
			read: func(dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
				return dnsCatalogAXFRResult{}, io.ErrUnexpectedEOF
			},
			wantCalls: []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND},
		},
		{
			name: "refused transfer is not retried",
			read: func(dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
				return dnsCatalogAXFRResult{}, refusedFlags
			},
			wantCalls: []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND},
		},
		{
			name:      "refusal both formats share is not retried",
			read:      peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogPowerDNSBadSOA)),
			wantCalls: []dnsCatalogAXFRProducer{dnsCatalogAXFRBIND},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []dnsCatalogAXFRProducer
			ctx := withDNSPeerCatalogSession(context.Background(), "test")
			result, err := selectDNSPeerCatalogAXFR(ctx, peerCatalogTestPeer, catalog,
				func(_ context.Context, producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
					calls = append(calls, producer)
					return tc.read(producer)
				})
			if len(calls) != len(tc.wantCalls) {
				t.Fatalf("transfers=%v, want %v", calls, tc.wantCalls)
			}
			for index := range calls {
				if calls[index] != tc.wantCalls[index] {
					t.Fatalf("transfers=%v, want %v", calls, tc.wantCalls)
				}
			}
			if tc.wantProducer != 0 {
				if err != nil || result.Producer != tc.wantProducer ||
					len(result.Members) != 1 || result.Members[0] != peerCatalogTestMember {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				return
			}
			if err == nil || errors.Is(err, errDNSPeerCatalogProducerChanged) {
				t.Fatalf("refusal expected, got result=%+v err=%v", result, err)
			}
			if tc.check != nil {
				tc.check(t, err)
			}
		})
	}
}

func TestPeerCatalogProducerIsPinnedForOneOperationAndLoggedOnce(t *testing.T) {
	logs := quietPeerCatalogLog(t)
	catalog := peerCatalogTestDomain(t)
	bindRead := peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogBIND))
	pdnsRead := peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogPowerDNS))
	read := func(source func(dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error)) dnsPeerCatalogRead {
		return func(_ context.Context, producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
			return source(producer)
		}
	}
	ctx := withDNSPeerCatalogSession(context.Background(), "DNS engine change test")
	// A nested operation keeps the outer pin.
	nested := withDNSPeerCatalogSession(ctx, "nested")
	for range 2 {
		if _, err := selectDNSPeerCatalogAXFR(nested, peerCatalogTestPeer, catalog, read(pdnsRead)); err != nil {
			t.Fatal(err)
		}
	}
	if *logs != 1 {
		t.Fatalf("producer logged %d times in one operation", *logs)
	}
	_, err := selectDNSPeerCatalogAXFR(ctx, peerCatalogTestPeer, catalog, read(bindRead))
	if !errors.Is(err, errDNSPeerCatalogProducerChanged) ||
		!strings.Contains(err.Error(), "peer catalog producer changed during the operation") ||
		!strings.Contains(err.Error(), "first read in the PowerDNS format and now in the BIND format") {
		t.Fatalf("producer change was not refused: %v", err)
	}
	if wrapped := dnsPeerCatalogReadError("paired primary catalog is unavailable", err); !errors.Is(wrapped, errDNSPeerCatalogProducerChanged) {
		t.Fatalf("caller hid the producer change: %v", wrapped)
	}
	// A new operation reads the peer afresh.
	if result, err := selectDNSPeerCatalogAXFR(
		withDNSPeerCatalogSession(context.Background(), "next"), peerCatalogTestPeer, catalog, read(bindRead),
	); err != nil || result.Producer != dnsCatalogAXFRBIND {
		t.Fatalf("new operation inherited the old pin: %+v %v", result, err)
	}
	if *logs != 2 {
		t.Fatalf("new operation logged %d times in total, want 2", *logs)
	}
	// Transport errors keep the caller's fixed text.
	if got := dnsPeerCatalogReadError("fixed", &net.OpError{Op: "dial", Err: errors.New("x")}); got.Error() != "fixed" {
		t.Fatalf("transport detail leaked into the caller text: %v", got)
	}
}

// installPDNSFormatPeer makes every catalog probe serve the same PowerDNS
// producer catalog through the real parser; the BIND probes therefore refuse
// it as another producer's format. flip switches it to the BIND format.
func installPDNSFormatPeer(t *testing.T) (flip func()) {
	t.Helper()
	quietPeerCatalogLog(t)
	catalog := peerCatalogTestDomain(t)
	current := peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogPowerDNS))
	bindFormat := peerCatalogWireRead(t, catalog, peerCatalogTestRecords(t, catalog, peerCatalogBIND))
	previousBIND, previousPDNS := probeDNSCatalogAXFR, probeDNSPDNSCatalogAXFR
	previousBoundBIND, previousBoundPDNS := probeDNSBoundCatalogAXFR, probeDNSBoundPDNSCatalogAXFR
	t.Cleanup(func() {
		probeDNSCatalogAXFR, probeDNSPDNSCatalogAXFR = previousBIND, previousPDNS
		probeDNSBoundCatalogAXFR, probeDNSBoundPDNSCatalogAXFR = previousBoundBIND, previousBoundPDNS
	})
	serve := func(address, name string, producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
		if address != peerCatalogTestPeer || name != catalog {
			return dnsCatalogAXFRResult{}, errors.New("unexpected catalog transfer")
		}
		return current(producer)
	}
	probeDNSCatalogAXFR = func(_ context.Context, address, name string) (dnsCatalogAXFRResult, error) {
		return serve(address, name, dnsCatalogAXFRBIND)
	}
	probeDNSPDNSCatalogAXFR = func(_ context.Context, address, name string) (dnsCatalogAXFRResult, error) {
		return serve(address, name, dnsCatalogAXFRPowerDNS)
	}
	probeDNSBoundCatalogAXFR = func(_ context.Context, source, address, name string) (dnsCatalogAXFRResult, error) {
		if source != peerCatalogTestLocal {
			return dnsCatalogAXFRResult{}, errors.New("unbound peer transfer")
		}
		return serve(address, name, dnsCatalogAXFRBIND)
	}
	probeDNSBoundPDNSCatalogAXFR = func(_ context.Context, source, address, name string) (dnsCatalogAXFRResult, error) {
		if source != peerCatalogTestLocal {
			return dnsCatalogAXFRResult{}, errors.New("unbound peer transfer")
		}
		return serve(address, name, dnsCatalogAXFRPowerDNS)
	}
	return func() { current = bindFormat }
}

func peerCatalogTestSOA(t *testing.T) dnsZoneSOAProbe {
	t.Helper()
	catalog := peerCatalogTestDomain(t)
	return func(_ context.Context, _, address, name string) (dnsSOAProbeResult, error) {
		if address != peerCatalogTestPeer && address != peerCatalogTestLocal {
			return dnsSOAProbeResult{}, errors.New("unexpected SOA address")
		}
		serial := uint32(2026092901)
		if name == catalog {
			serial = 17
		}
		return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{serial}}, nil
	}
}

func peerCatalogSecondaryManifest() mutationpayload.DNSEngineSwitchManifestCommitment {
	return mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode: transport.DNSEngineSwitchModeSwitch, TargetEngine: transport.DNSEnginePowerDNS,
		Topology: transport.DNSTopologyPaired, PairRole: transport.DNSPairRoleSecondary,
		LocalIP: peerCatalogTestLocal, PeerIP: peerCatalogTestPeer,
	}
}

// Call site: a fresh BIND secondary's pre-intent peer catalog proof
// (hostDNSEngineBackend.Switch -> requireBINDSecondaryPeerCatalog).
func TestBINDSecondaryPreflightAcceptsPowerDNSPrimaryCatalog(t *testing.T) {
	installPDNSFormatPeer(t)
	manifest := peerCatalogSecondaryManifest()
	manifest.TargetEngine = transport.DNSEngineBIND
	if err := requireBINDSecondaryPeerCatalog(context.Background(), manifest); err != nil {
		t.Fatalf("PowerDNS primary catalog refused: %v", err)
	}
}

// Call site: peerPDNSCatalog (PowerDNS secondary staging, verification,
// member retrieval and pairing authority), including the operation pin.
func TestPowerDNSSecondaryPeerCatalogAcceptsPowerDNSPrimaryAndPinsIt(t *testing.T) {
	flip := installPDNSFormatPeer(t)
	ctx := withDNSPeerCatalogSession(context.Background(), "test switch")
	catalog, domain, err := peerPDNSCatalog(ctx, peerCatalogSecondaryManifest())
	if err != nil || domain != peerCatalogTestDomain(t) || catalog.Producer != dnsCatalogAXFRPowerDNS ||
		len(catalog.Members) != 1 || catalog.Members[0] != peerCatalogTestMember {
		t.Fatalf("catalog=%+v domain=%q err=%v", catalog, domain, err)
	}
	flip()
	if _, _, err := peerPDNSCatalog(ctx, peerCatalogSecondaryManifest()); !errors.Is(err, errDNSPeerCatalogProducerChanged) ||
		!strings.Contains(err.Error(), "paired primary catalog is unavailable") {
		t.Fatalf("producer change inside one operation was accepted: %v", err)
	}
}

// Call site: the legacy PowerDNS secondary reconfiguration's bound peer read.
func TestLegacyPowerDNSSecondaryReadsPowerDNSPrimaryCatalog(t *testing.T) {
	installPDNSFormatPeer(t)
	previousSOA := probeDNSZoneSOA
	probeDNSZoneSOA = peerCatalogTestSOA(t)
	t.Cleanup(func() { probeDNSZoneSOA = previousSOA })
	catalog, err := readLegacyPDNSPeerCatalogAuthority(context.Background(), peerCatalogSecondaryManifest())
	if err != nil || catalog.Producer != dnsCatalogAXFRPowerDNS || catalog.Serial != 17 {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
}

// Call site: secondary pair readiness (both of its catalog reads).
func TestSecondaryPairReadinessAcceptsPowerDNSPrimaryAndRefusesProducerChange(t *testing.T) {
	flip := installPDNSFormatPeer(t)
	soa := peerCatalogTestSOA(t)
	if err := verifyDNSSecondaryPairReadyAt(
		context.Background(), peerCatalogTestLocal, peerCatalogTestPeer, soa, queryDNSBoundPeerCatalogAXFR,
	); err != nil {
		t.Fatalf("PowerDNS primary readiness refused: %v", err)
	}
	reads := 0
	axfr := func(ctx context.Context, source, address, name string) (dnsCatalogAXFRResult, error) {
		reads++
		if reads == 2 {
			flip()
		}
		return queryDNSBoundPeerCatalogAXFR(ctx, source, address, name)
	}
	err := verifyDNSSecondaryPairReadyAt(context.Background(), peerCatalogTestLocal, peerCatalogTestPeer, soa, axfr)
	if !errors.Is(err, errDNSPeerCatalogProducerChanged) {
		t.Fatalf("producer change between the two readiness reads was not named: %v", err)
	}
}

// Call site: verifyBINDPairingAuthority for a BIND secondary.
func TestBINDSecondaryPairingAuthorityAcceptsPowerDNSPrimaryCatalog(t *testing.T) {
	installPDNSFormatPeer(t)
	previousSOA, previousOwned := probeDNSZoneSOA, dnsPairHostAddressOwned
	probeDNSZoneSOA = peerCatalogTestSOA(t)
	dnsPairHostAddressOwned = func(address string) (bool, error) { return address == peerCatalogTestLocal, nil }
	t.Cleanup(func() { probeDNSZoneSOA, dnsPairHostAddressOwned = previousSOA, previousOwned })
	localCatalog, err := binddns.CatalogDomain(peerCatalogTestLocal)
	if err != nil {
		t.Fatal(err)
	}
	receipt := binddns.Receipt{Pairing: &binddns.PairingReceipt{
		Role: binddns.PairRoleSecondary, LocalIP: peerCatalogTestLocal, PeerIP: peerCatalogTestPeer,
		LocalCatalog: localCatalog, PeerCatalog: peerCatalogTestDomain(t), CatalogSerial: 1,
	}}
	if err := verifyBINDPairingAuthority(context.Background(), receipt); err != nil {
		t.Fatalf("BIND secondary refused a PowerDNS primary catalog: %v", err)
	}
}

// Call site: verifyPDNSPairingAuthority for a PowerDNS secondary.
func TestPowerDNSSecondaryPairingAuthorityAcceptsPowerDNSPrimaryCatalog(t *testing.T) {
	installPDNSFormatPeer(t)
	previousSOA, previousOwned := probeDNSZoneSOA, dnsPairHostAddressOwned
	probeDNSZoneSOA = peerCatalogTestSOA(t)
	dnsPairHostAddressOwned = func(address string) (bool, error) { return address == peerCatalogTestLocal, nil }
	t.Cleanup(func() { probeDNSZoneSOA, dnsPairHostAddressOwned = previousSOA, previousOwned })
	if err := verifyPDNSPairingAuthority(context.Background(), peerCatalogSecondaryManifest()); err != nil {
		t.Fatalf("PowerDNS secondary refused a PowerDNS primary catalog: %v", err)
	}
}
