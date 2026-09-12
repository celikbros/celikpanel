package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSBackendReadinessPeerTimeoutPreservesLocalEvidence(t *testing.T) {
	for _, engine := range []transport.DNSEngine{transport.DNSEngineBIND, transport.DNSEnginePowerDNS} {
		for _, role := range []string{"primary", "secondary"} {
			t.Run(string(engine)+"/"+role, func(t *testing.T) {
				localChecked := false
				previous := dnsPort53ConflictCheck
				dnsPort53ConflictCheck = func(ctx context.Context, bindRunning, pdnsRunning bool) (bool, error) {
					if ctx.Err() != nil || bindRunning != (engine == transport.DNSEngineBIND) || pdnsRunning != (engine == transport.DNSEnginePowerDNS) {
						t.Fatal("invalid or expired local runtime inspection")
					}
					localChecked = true
					return false, nil
				}
				t.Cleanup(func() { dnsPort53ConflictCheck = previous })
				ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
				defer cancel()
				peerQueried := false
				unavailableAXFR := func(proofCtx context.Context, _, _, _ string) (dnsCatalogAXFRResult, error) {
					if !localChecked {
						t.Fatal("peer proof ran before required local inspection")
					}
					peerQueried = true
					<-proofCtx.Done()
					return dnsCatalogAXFRResult{}, proofCtx.Err()
				}
				soa := func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
					t.Fatal("unavailable catalog must not grant an SOA proof")
					return dnsSOAProbeResult{}, nil
				}
				probes := dnsBackendPairReadinessProbes{}
				if role == "primary" {
					probes.primary = func(proofCtx context.Context) (bool, error) {
						evidence := dnsPrimaryCatalogEvidence{
							LocalIP: "192.0.2.10", PeerIP: "192.0.2.20",
							Domain: "catalog-c000020a.celikpanel.invalid", Serial: 7,
						}
						localAXFR := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
							return dnsCatalogAXFRResult{Serial: 7}, nil
						}
						err := verifyDNSPrimaryPairReadyAt(proofCtx, evidence, soa, localAXFR, unavailableAXFR)
						return err == nil, err
					}
				} else {
					probes.secondary = func(proofCtx context.Context) (bool, error) {
						err := verifyDNSSecondaryPairReadyAt(proofCtx, "192.0.2.20", "192.0.2.10", soa, unavailableAXFR)
						return err == nil, err
					}
				}
				local := transport.DNSBackendRuntimeState{Engine: engine, Installed: true, Running: true, Managed: true}
				got, err := completeDNSBackendReadiness(ctx, []transport.DNSBackendRuntimeState{local}, map[transport.DNSEngine]dnsBackendPairReadinessProbes{engine: probes})
				if err != nil || got.Error != "" || !localChecked || !peerQueried || len(got.Engines) != 1 {
					t.Fatalf("lost local evidence: response=%+v err=%v local=%v peer=%v", got, err, localChecked, peerQueried)
				}
				observed := got.Engines[0]
				if !observed.Installed || !observed.Running || !observed.Managed || observed.PairReady || observed.SecondaryReady {
					t.Fatalf("peer absence altered local proof or granted readiness: %+v", observed)
				}
				if ctx.Err() != nil {
					t.Fatal("optional peer proof consumed the RPC response budget")
				}
			})
		}
	}
}

func TestDNSBackendReadinessRequiredLocalProofFailsClosed(t *testing.T) {
	for _, kind := range []string{"expired_before_local", "cancelled_during_local", "local_error"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "expired_before_local" {
				cancel()
			}
			inspected := false
			previous := dnsPort53ConflictCheck
			dnsPort53ConflictCheck = func(context.Context, bool, bool) (bool, error) {
				inspected = true
				if kind == "cancelled_during_local" {
					cancel()
					return false, nil
				}
				return false, errors.New("local runtime evidence unavailable")
			}
			t.Cleanup(func() { dnsPort53ConflictCheck = previous })
			unexpectedPeer := func(context.Context) (bool, error) {
				t.Fatal("remote proof cannot replace missing local runtime evidence")
				return true, nil
			}
			local := transport.DNSBackendRuntimeState{Engine: transport.DNSEngineBIND, Installed: true, Running: true, Managed: true}
			got, err := completeDNSBackendReadiness(ctx, []transport.DNSBackendRuntimeState{local}, map[transport.DNSEngine]dnsBackendPairReadinessProbes{
				transport.DNSEngineBIND: {primary: unexpectedPeer, secondary: unexpectedPeer},
			})
			if err == nil || len(got.Engines) != 0 || (kind == "expired_before_local" && inspected) {
				t.Fatalf("unverified local state was exposed: response=%+v err=%v inspected=%v", got, err, inspected)
			}
		})
	}
}

func TestDNSBackendReadinessNoPeerBudgetRetainsOnlyLocalFacts(t *testing.T) {
	previous := dnsPort53ConflictCheck
	dnsPort53ConflictCheck = func(context.Context, bool, bool) (bool, error) { return true, nil }
	t.Cleanup(func() { dnsPort53ConflictCheck = previous })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	local := transport.DNSBackendRuntimeState{Engine: transport.DNSEngineBIND, Installed: true, Running: true, Managed: true, PairReady: true, SecondaryReady: true}
	unexpectedPeer := func(context.Context) (bool, error) {
		t.Fatal("no peer budget remains after reserving the RPC response")
		return true, nil
	}
	got, err := completeDNSBackendReadiness(ctx, []transport.DNSBackendRuntimeState{local}, map[transport.DNSEngine]dnsBackendPairReadinessProbes{
		transport.DNSEngineBIND: {primary: unexpectedPeer, secondary: unexpectedPeer},
	})
	if err != nil || !got.Port53Conflict || len(got.Engines) != 1 || !got.Engines[0].Managed || got.Engines[0].PairReady || got.Engines[0].SecondaryReady {
		t.Fatalf("exhausted optional budget must retain conflict and local facts only: %+v err=%v", got, err)
	}
}

func TestDNSBackendReadinessPairEvidenceRequiresSuccessAndLiveContext(t *testing.T) {
	for _, kind := range []string{"ready", "not_ready", "error", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := probeDNSBackendPairReadiness(ctx, transport.DNSEnginePowerDNS, "secondary", func(context.Context) (bool, error) {
				switch kind {
				case "not_ready":
					return false, nil
				case "error":
					return true, errors.New("private proof detail")
				case "cancelled":
					cancel()
				}
				return true, nil
			})
			if got != (kind == "ready") {
				t.Fatalf("unexpected pair readiness %v for %s", got, kind)
			}
		})
	}
}
