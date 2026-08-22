package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func rollbackEvidenceRequestForTest(
	t *testing.T,
	source transport.DNSEngine,
	sourceEpoch int64,
) (*transport.DNSEngineRollbackEvidenceRequest, mutationpayload.DNSEngineSwitchManifestCommitment) {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		source, transport.DNSEngineBIND,
		sourceEpoch, sourceEpoch+1, 0,
		transport.DNSTopologyStandalone,
		"", "", "", "", "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := &transport.DNSEngineRollbackEvidenceRequest{
		ServiceMutationBinding: transport.ServiceMutationBinding{
			MutationRequestID: strings.Repeat("a", 32),
			MutationOwnerID:   strings.Repeat("b", 32),
		},
		Mode:              manifest.Mode,
		SourceEngine:      manifest.SourceEngine,
		TargetEngine:      manifest.TargetEngine,
		SourceEpoch:       manifest.SourceEpoch,
		TargetEpoch:       manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		Topology:          manifest.Topology,
		Zones:             manifest.Zones,
		SnapshotBytes:     manifest.SnapshotBytes,
		ManifestQualifier: manifest.Qualifier,
	}
	return request, manifest
}

func installOwnershipForEvidenceTest(
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) dnsEngineInstallOwnershipReceipt {
	return dnsEngineInstallOwnershipReceipt{
		Schema:            dnsEngineInstallOwnershipSchema,
		Engine:            manifest.TargetEngine,
		PackageManager:    "apt",
		Packages:          []string{"bind9"},
		MissingBefore:     []string{"bind9"},
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: request.MutationRequestID,
		MutationOwnerID:   request.MutationOwnerID,
	}
}

func withRollbackEvidenceReadersForTest(t *testing.T) {
	t.Helper()
	journal := readRollbackEvidenceJournal
	state := readRollbackEvidenceState
	ownership := readRollbackEvidenceOwnership
	install := readRollbackEvidenceInstallOwnership
	targetHost := readRollbackEvidenceTargetHost
	seal := verifyRollbackEvidenceTargetSeal
	readRollbackEvidenceTargetHost = func(
		transport.DNSEngine,
	) (dnsEngineRollbackTargetHost, error) {
		return dnsEngineRollbackTargetHost{
			PackageManager: hostplatform.PackageManagerAPT,
			Packages:       []string{"bind9"},
			Systemctl:      "/usr/bin/systemctl",
		}, nil
	}
	t.Cleanup(func() {
		readRollbackEvidenceJournal = journal
		readRollbackEvidenceState = state
		readRollbackEvidenceOwnership = ownership
		readRollbackEvidenceInstallOwnership = install
		readRollbackEvidenceTargetHost = targetHost
		verifyRollbackEvidenceTargetSeal = seal
	})
}

func TestDNSEngineRollbackEvidenceAcceptsAndPreservesExactInstallOwnership(t *testing.T) {
	withRollbackEvidenceReadersForTest(t)
	request, manifest := rollbackEvidenceRequestForTest(t, "", 0)
	receipt := installOwnershipForEvidenceTest(request, manifest)
	before := receipt
	installReads := 0
	readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) {
		return dnsEngineSwitchJournal{}, false, nil
	}
	readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceInstallOwnership = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		installReads++
		return receipt, true, nil
	}
	sealCalls := 0
	verifyRollbackEvidenceTargetSeal = func(
		context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
	) error {
		sealCalls++
		return nil
	}

	outcome, err := classifyDNSEngineRollbackHostEvidence(
		context.Background(), request, manifest,
	)
	if err != nil || outcome != transport.DNSEngineRollbackSafe {
		t.Fatalf("outcome=%q err=%v", outcome, err)
	}
	if installReads != 1 || sealCalls != 1 || !reflect.DeepEqual(receipt, before) {
		t.Fatalf("install receipt changed=%v reads=%d seal=%d",
			!reflect.DeepEqual(receipt, before), installReads, sealCalls)
	}
}

func TestDNSEngineRollbackEvidenceRejectsEveryForwardOrUnsealedArtifact(t *testing.T) {
	tests := []struct {
		name      string
		want      string
		configure func(
			*transport.DNSEngineRollbackEvidenceRequest,
			mutationpayload.DNSEngineSwitchManifestCommitment,
		)
	}{
		{
			name: "switch journal",
			want: transport.DNSEngineRollbackJournalPresent,
			configure: func(_ *transport.DNSEngineRollbackEvidenceRequest, _ mutationpayload.DNSEngineSwitchManifestCommitment) {
				readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) {
					return dnsEngineSwitchJournal{}, true, nil
				}
			},
		},
		{
			name: "committed target state",
			want: transport.DNSEngineRollbackCommittedEvidence,
			configure: func(_ *transport.DNSEngineRollbackEvidenceRequest, manifest mutationpayload.DNSEngineSwitchManifestCommitment) {
				readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) {
					return dnsEngineStateReceipt{
						Engine:      manifest.TargetEngine,
						EngineEpoch: manifest.TargetEpoch,
					}, true, nil
				}
			},
		},
		{
			name: "current target ownership",
			want: transport.DNSEngineRollbackCommittedEvidence,
			configure: func(request *transport.DNSEngineRollbackEvidenceRequest, manifest mutationpayload.DNSEngineSwitchManifestCommitment) {
				readRollbackEvidenceOwnership = func(
					transport.DNSEngine,
				) (dnsEngineStateReceipt, bool, error) {
					return dnsEngineStateReceipt{
						Engine:            manifest.TargetEngine,
						EngineEpoch:       manifest.TargetEpoch,
						ManifestQualifier: manifest.Qualifier,
						MutationRequestID: request.MutationRequestID,
						MutationOwnerID:   request.MutationOwnerID,
					}, true, nil
				}
			},
		},
		{
			name: "mismatched install ownership",
			want: transport.DNSEngineRollbackInstallOwnershipMismatch,
			configure: func(request *transport.DNSEngineRollbackEvidenceRequest, manifest mutationpayload.DNSEngineSwitchManifestCommitment) {
				receipt := installOwnershipForEvidenceTest(request, manifest)
				receipt.MutationOwnerID = strings.Repeat("c", 32)
				readRollbackEvidenceInstallOwnership = func(
					transport.DNSEngine,
				) (dnsEngineInstallOwnershipReceipt, bool, error) {
					return receipt, true, nil
				}
			},
		},
		{
			name: "wrong install package manager",
			want: transport.DNSEngineRollbackInstallOwnershipMismatch,
			configure: func(request *transport.DNSEngineRollbackEvidenceRequest, manifest mutationpayload.DNSEngineSwitchManifestCommitment) {
				receipt := installOwnershipForEvidenceTest(request, manifest)
				receipt.PackageManager = "pacman"
				readRollbackEvidenceInstallOwnership = func(
					transport.DNSEngine,
				) (dnsEngineInstallOwnershipReceipt, bool, error) {
					return receipt, true, nil
				}
			},
		},
		{
			name: "wrong install package set",
			want: transport.DNSEngineRollbackInstallOwnershipMismatch,
			configure: func(request *transport.DNSEngineRollbackEvidenceRequest, manifest mutationpayload.DNSEngineSwitchManifestCommitment) {
				receipt := installOwnershipForEvidenceTest(request, manifest)
				receipt.Packages = []string{"bind9", "bind9-utils"}
				readRollbackEvidenceInstallOwnership = func(
					transport.DNSEngine,
				) (dnsEngineInstallOwnershipReceipt, bool, error) {
					return receipt, true, nil
				}
			},
		},
		{
			name: "target not sealed",
			want: transport.DNSEngineRollbackRuntimeUnsealed,
			configure: func(_ *transport.DNSEngineRollbackEvidenceRequest, _ mutationpayload.DNSEngineSwitchManifestCommitment) {
				verifyRollbackEvidenceTargetSeal = func(
					context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
				) error {
					return errors.New("not sealed")
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withRollbackEvidenceReadersForTest(t)
			request, manifest := rollbackEvidenceRequestForTest(t, "", 0)
			readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) {
				return dnsEngineSwitchJournal{}, false, nil
			}
			readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) {
				return dnsEngineStateReceipt{}, false, nil
			}
			readRollbackEvidenceOwnership = func(
				transport.DNSEngine,
			) (dnsEngineStateReceipt, bool, error) {
				return dnsEngineStateReceipt{}, false, nil
			}
			readRollbackEvidenceInstallOwnership = func(
				transport.DNSEngine,
			) (dnsEngineInstallOwnershipReceipt, bool, error) {
				return dnsEngineInstallOwnershipReceipt{}, false, nil
			}
			verifyRollbackEvidenceTargetSeal = func(
				context.Context, transport.DNSEngine, dnsEngineRollbackTargetHost,
			) error {
				return nil
			}
			test.configure(request, manifest)
			outcome, err := classifyDNSEngineRollbackHostEvidence(
				context.Background(), request, manifest,
			)
			if err != nil || outcome != test.want {
				t.Fatalf("outcome=%q want=%q err=%v", outcome, test.want, err)
			}
		})
	}
}

func TestDNSEngineRollbackEvidenceRejectsTargetOwnershipAtSourceEpoch(t *testing.T) {
	request, manifest := rollbackEvidenceRequestForTest(
		t, transport.DNSEnginePowerDNS, 2,
	)
	ownership := dnsEngineStateReceipt{
		Engine:            transport.DNSEngineBIND,
		EngineEpoch:       manifest.SourceEpoch,
		ManifestQualifier: strings.Repeat("d", 64),
		MutationRequestID: strings.Repeat("e", 32),
		MutationOwnerID:   strings.Repeat("f", 32),
	}
	if !conflictingDNSEngineRollbackTargetOwnership(
		ownership, request, manifest,
	) {
		t.Fatal("target ownership at the restored source epoch was accepted as historical")
	}
}

func TestDNSEngineRollbackEvidenceScopeIsInitialBINDInstallOnly(t *testing.T) {
	base := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode:         transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND,
		TargetEpoch:  1,
		Topology:     transport.DNSTopologyStandalone,
	}
	if !initialBINDInstallRollbackEvidenceScope(base) {
		t.Fatal("exact initial BIND install scope was rejected")
	}
	tests := []struct {
		name   string
		mutate func(*mutationpayload.DNSEngineSwitchManifestCommitment)
	}{
		{name: "source present", mutate: func(manifest *mutationpayload.DNSEngineSwitchManifestCommitment) {
			manifest.SourceEngine = transport.DNSEnginePowerDNS
		}},
		{name: "source epoch", mutate: func(manifest *mutationpayload.DNSEngineSwitchManifestCommitment) {
			manifest.SourceEpoch = 1
		}},
		{name: "adopt", mutate: func(manifest *mutationpayload.DNSEngineSwitchManifestCommitment) {
			manifest.Mode = transport.DNSEngineSwitchModeAdopt
		}},
		{name: "PowerDNS target", mutate: func(manifest *mutationpayload.DNSEngineSwitchManifestCommitment) {
			manifest.TargetEngine = transport.DNSEnginePowerDNS
		}},
		{name: "later target epoch", mutate: func(manifest *mutationpayload.DNSEngineSwitchManifestCommitment) {
			manifest.TargetEpoch = 2
		}},
		{name: "paired secondary", mutate: func(manifest *mutationpayload.DNSEngineSwitchManifestCommitment) {
			manifest.Topology = transport.DNSTopologyPaired
			manifest.PairRole = transport.DNSPairRoleSecondary
			manifest.LocalIP, manifest.LocalNS = "192.0.2.10", "ns1.example.test"
			manifest.PeerIP, manifest.PeerNS = "192.0.2.11", "ns2.example.test"
		}},
	}
	paired := base
	paired.Topology = transport.DNSTopologyPaired
	paired.PairRole = transport.DNSPairRolePrimary
	paired.LocalIP, paired.LocalNS = "192.0.2.10", "ns1.example.test"
	paired.PeerIP, paired.PeerNS = "192.0.2.11", "ns2.example.test"
	if !initialBINDInstallRollbackEvidenceScope(paired) {
		t.Fatal("exact paired-primary initial BIND scope was rejected")
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			test.mutate(&manifest)
			if initialBINDInstallRollbackEvidenceScope(manifest) {
				t.Fatalf("unsupported rollback evidence scope was accepted: %+v", manifest)
			}
		})
	}
}

func TestDNSEngineRollbackEvidenceBoundedContextTimesOutUnverified(t *testing.T) {
	withRollbackEvidenceReadersForTest(t)
	request, manifest := rollbackEvidenceRequestForTest(t, "", 0)
	readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) {
		return dnsEngineSwitchJournal{}, false, nil
	}
	readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceOwnership = func(
		transport.DNSEngine,
	) (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceInstallOwnership = func(
		transport.DNSEngine,
	) (dnsEngineInstallOwnershipReceipt, bool, error) {
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	deadlineSeen := false
	verifyRollbackEvidenceTargetSeal = func(
		ctx context.Context, _ transport.DNSEngine, _ dnsEngineRollbackTargetHost,
	) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("rollback evidence seal context has no deadline")
		}
		deadlineSeen = true
		<-ctx.Done()
		return ctx.Err()
	}

	outcome := classifyDNSEngineRollbackHostEvidenceWithin(
		request, manifest, 10*time.Millisecond,
	)
	if !deadlineSeen {
		t.Fatal("rollback evidence seal did not receive the bounded context")
	}
	if outcome != transport.DNSEngineRollbackUnverified {
		t.Fatalf("timed-out evidence outcome=%q, want unverified", outcome)
	}
}

func TestExactFailedDNSEngineEvidenceJobRejectsPublishedOrActiveIdentity(t *testing.T) {
	request, manifest := rollbackEvidenceRequestForTest(t, "", 0)
	now := time.Now().UTC()
	job := &ServiceMutationJob{
		RequestID:    request.MutationRequestID,
		OwnerID:      request.MutationOwnerID,
		Kind:         "dns_engine_switch",
		Target:       string(manifest.TargetEngine),
		PackageName:  manifest.Qualifier,
		Status:       serviceMutationStatusFailed,
		Phase:        "failed",
		ErrorCode:    "service_operation_failed",
		ErrorMessage: "bounded failure",
		Attempt:      1,
		StartedAt:    now.Add(-2 * time.Minute),
		UpdatedAt:    now,
		DeadlineAt:   now.Add(time.Hour),
		FinishedAt:   now,
	}
	if !exactFailedDNSEngineEvidenceJob(job, request, manifest) {
		t.Fatal("exact failed terminal job was rejected")
	}
	published := *job
	published.Phase = dnsEngineSwitchPublishedPhasePrefix +
		request.MutationRequestID + "/" + manifest.Qualifier
	if exactFailedDNSEngineEvidenceJob(&published, request, manifest) {
		t.Fatal("published success phase was accepted as failed evidence")
	}
	active := *job
	active.Status = serviceMutationStatusRunning
	active.FinishedAt = time.Time{}
	active.LeaseExpiresAt = now.Add(time.Minute)
	if exactFailedDNSEngineEvidenceJob(&active, request, manifest) {
		t.Fatal("active job was accepted as failed evidence")
	}
	noncanonicalTerminal := *job
	noncanonicalTerminal.UpdatedAt = now.Add(-time.Second)
	if exactFailedDNSEngineEvidenceJob(
		&noncanonicalTerminal, request, manifest,
	) {
		t.Fatal("terminal job without exact writer timestamps was accepted")
	}
	mismatch := *job
	mismatch.OwnerID = strings.Repeat("0", 32)
	if exactFailedDNSEngineEvidenceJob(&mismatch, request, manifest) {
		t.Fatal("mismatched job identity was accepted")
	}
}

func TestDNSEngineRollbackEvidenceClearsReusedCommitmentOnInvalidRequest(t *testing.T) {
	response := transport.DNSEngineRollbackEvidenceResponse{
		Outcome:           transport.DNSEngineRollbackSafe,
		ReceiptCommitment: strings.Repeat("a", 64),
	}
	if err := (&Agent{}).DNSEngineRollbackEvidenceV1(nil, &response); err != nil {
		t.Fatal(err)
	}
	if response.Outcome != transport.DNSEngineRollbackIdentityMismatch ||
		response.ReceiptCommitment != "" {
		t.Fatalf("invalid evidence reused stale response: %+v", response)
	}
}
