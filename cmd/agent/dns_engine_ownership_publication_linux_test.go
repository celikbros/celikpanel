//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

func publishedBINDOwnershipFixture(t *testing.T) (dnsEngineStateReceipt, dnsEngineStateReceipt, binddns.Generation) {
	t.Helper()
	prepareDNSEngineOwnershipTest(t)
	previousHostOwned := dnsPairHostAddressOwned
	t.Cleanup(func() { dnsPairHostAddressOwned = previousHostOwned })
	dnsPairHostAddressOwned = func(address string) (bool, error) { return address == "72.62.38.15", nil }
	previous, receipt := verifiedSignedUpdateBINDTree(t, &binddns.Pairing{
		Role:    binddns.PairRolePrimary,
		LocalIP: "72.62.38.15", LocalNS: "ns1.celikhost.com",
		PeerIP: "2.25.80.4", PeerNS: "ns2.celikhost.com",
	}, 1)
	ownership := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: "switch", Engine: transport.DNSEngineBIND,
		EngineEpoch: 1, Generation: receipt.Generation,
		PairRole: binddns.PairRolePrimary, PairLocalIP: "72.62.38.15", PairPeerIP: "2.25.80.4",
		PrimaryCatalogSerial: 1, SourceRevision: 1,
		ManifestQualifier: "dns-engine-switch/v1:sha256:c862964b22a866c429a58611abed2c2c0efd6575c3e0ef0ff202a43e987326ee",
		MutationRequestID: "24f995a13dbd09c0b99aecd074431213",
		MutationOwnerID:   "013e3c1f520b6a1063addc75c23ce6b5",
	}
	if err := writeDNSEngineOwnership(ownership); err != nil {
		t.Fatal(err)
	}
	if err := persistExactDNSEngineState(ownership); err != nil {
		t.Fatal(err)
	}
	// Use the same delta renderer and state writer used by hostDNSEngineBackend.Sync.
	plan, err := binddns.ApplyDelta(previous, testBINDV3Snapshot(t, "celikhost.com", 1, 1, false))
	if err != nil {
		t.Fatal(err)
	}
	generation, err := binddns.RenderTree(aptBINDGenerationRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	state, err := bindStateForPublishedReceipt(ownership, generation.ReceiptValue)
	if err != nil {
		t.Fatal(err)
	}
	if state.Generation == ownership.Generation || state.PrimaryCatalogSerial != 2 {
		t.Fatal("fixture did not advance the real publication state")
	}
	if err := persistExactDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	actual, exists, err := readDNSEngineOwnership(transport.DNSEngineBIND)
	if err != nil || !exists || actual != ownership {
		t.Fatalf("publication rewrote ownership: %+v %v", actual, err)
	}
	return ownership, state, generation
}

func publicationGenerationTree(generation binddns.Generation) (binddns.VerifiedTree, error) {
	files := make(map[string][]byte)
	for _, zone := range generation.Zones {
		files["zones/"+zone.FileName] = zone.Data
	}
	if generation.Catalog != nil {
		files[generation.ReceiptValue.Pairing.CatalogFile] = generation.Catalog.Data
	}
	return binddns.VerifyTree(generation.Receipt, generation.Config, files)
}

func publicationTreeVerifier(t *testing.T, generation *binddns.Generation) func(dnsEngineStateReceipt) error {
	t.Helper()
	layout := signedUpdateBINDRuntimeLayout(t, generation.ReceiptValue, false)
	return func(state dnsEngineStateReceipt) error {
		tree, err := publicationGenerationTree(*generation)
		if err != nil {
			return err
		}
		return verifyExistingManagedBINDTreeForSignedUpdateWithSnapshotReader(layout, state, tree, modeledRootOwnedBINDConfigSnapshot)
	}
}

func TestSignedUpdateBINDPreparationAfterRealZonePublication(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		name := "valid"
		if corrupt {
			name = "corrupted-current-tree"
		}
		t.Run(name, func(t *testing.T) {
			ownership, state, generation := publishedBINDOwnershipFixture(t)
			verify := publicationTreeVerifier(t, &generation)
			if corrupt {
				generation.Config = append(generation.Config, []byte("// owner modification\n")...)
			}
			ops, events := signedUpdateBINDPreparationOps(t)
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				return dnsEngineInstallOwnershipReceipt{}, false, nil
			}
			ops.readState = readDNSEngineState
			ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) { return readDNSEngineOwnership(transport.DNSEngineBIND) }
			verified := false
			ops.verifyExisting = func(_ context.Context, candidate dnsEngineStateReceipt) error {
				verified = true
				if candidate != state {
					return errors.New("verifier received stale acquisition generation")
				}
				return verify(candidate)
			}
			err := prepareBINDGenerationRootForSignedUpdateWithOps(context.Background(), ops)
			if corrupt && err == nil {
				t.Fatal("corrupted tree passed update preparation")
			}
			if !corrupt && err != nil {
				t.Fatalf("ordinary zone publication blocked update: %v", err)
			}
			if !verified {
				t.Fatal("update skipped current generation verification")
			}
			for _, event := range *events {
				if event == "write-ownership" {
					t.Fatal("update rewrote acquisition evidence")
				}
			}
			actual, _, err := readDNSEngineOwnership(transport.DNSEngineBIND)
			if err != nil || actual != ownership {
				t.Fatalf("ownership changed: %+v %v", actual, err)
			}
		})
	}
}

func TestFinalizedBINDOwnershipAfterRealZonePublication(t *testing.T) {
	ownership, state, generation := publishedBINDOwnershipFixture(t)
	verify := publicationTreeVerifier(t, &generation)
	path, _ := dnsEngineOwnershipPath(transport.DNSEngineBIND)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sameOperation := range []bool{true, false} {
		binding := transport.ServiceMutationBinding{MutationRequestID: state.MutationRequestID, MutationOwnerID: state.MutationOwnerID}
		if !sameOperation {
			binding.MutationRequestID = strings.Repeat("a", 32)
		}
		calls := 0
		finalized, err := exactFinalizedDNSEngineSwitchProvenanceWithBINDVerifier(transport.DNSEngineBIND, state.ManifestQualifier, binding, func(candidate dnsEngineStateReceipt) error {
			calls++
			if candidate != state {
				return errors.New("finalization used stale ownership state")
			}
			return verify(candidate)
		})
		if err != nil || finalized != sameOperation || calls != 1 {
			t.Fatalf("same=%v finalized=%v calls=%d err=%v", sameOperation, finalized, calls, err)
		}
	}
	if err := verifyCurrentDNSEngineOwnership(ownership, state, nil); err == nil {
		t.Fatal("unverified publication relationship was accepted")
	}
	generation.Config = append(generation.Config, 'x')
	binding := transport.ServiceMutationBinding{MutationRequestID: state.MutationRequestID, MutationOwnerID: state.MutationOwnerID}
	if _, err := exactFinalizedDNSEngineSwitchProvenanceWithBINDVerifier(transport.DNSEngineBIND, state.ManifestQualifier, binding, verify); err == nil {
		t.Fatal("finalization accepted corrupt current tree")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("finalization changed acquisition evidence")
	}
}

func TestBINDPublicationOwnershipRejectsIdentityDrift(t *testing.T) {
	ownership, state, generation := publishedBINDOwnershipFixture(t)
	verify := publicationTreeVerifier(t, &generation)
	for _, test := range []struct {
		name   string
		change func(*dnsEngineStateReceipt)
	}{
		{"epoch", func(s *dnsEngineStateReceipt) { s.EngineEpoch++ }},
		{"mode", func(s *dnsEngineStateReceipt) { s.Mode = "adopt" }},
		{"role", func(s *dnsEngineStateReceipt) { s.PairRole = binddns.PairRoleSecondary; s.PrimaryCatalogSerial = 0 }},
		{"local-ip", func(s *dnsEngineStateReceipt) { s.PairLocalIP = "192.0.2.10" }},
		{"peer-ip", func(s *dnsEngineStateReceipt) { s.PairPeerIP = "192.0.2.20" }},
		{"qualifier", func(s *dnsEngineStateReceipt) {
			s.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("b", 64)
		}},
		{"request", func(s *dnsEngineStateReceipt) { s.MutationRequestID = strings.Repeat("a", 32) }},
		{"owner", func(s *dnsEngineStateReceipt) { s.MutationOwnerID = strings.Repeat("b", 32) }},
		{"source-revision", func(s *dnsEngineStateReceipt) { s.SourceRevision++ }},
		{"same-generation-different-serial", func(s *dnsEngineStateReceipt) { s.Generation = ownership.Generation }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := state
			test.change(&candidate)
			if bindPublicationPreservesEngineOwnership(ownership, candidate) {
				t.Fatal("identity drift accepted as publication")
			}
			if err := verifyCurrentDNSEngineOwnership(ownership, candidate, verify); err == nil {
				t.Fatal("identity drift passed tree-backed proof")
			}
		})
	}
	newerOwnership := ownership
	newerOwnership.PrimaryCatalogSerial = 3
	if bindPublicationPreservesEngineOwnership(newerOwnership, state) {
		t.Fatal("catalog serial regression accepted")
	}
}

func TestBINDPublicationOwnershipAcceptsFrankfurtEvidence(t *testing.T) {
	ownership, state, _ := publishedBINDOwnershipFixture(t)
	ownership.Generation = "b19ee90a81d87342cba30751f1aede6342706643c243130f37c3f164006926c7"
	state.Generation = "ea6ea68b1b7caa4fa9b0a4260e567842353f3667aa5ad4df98ba5b70cb1f1469"
	if !bindPublicationPreservesEngineOwnership(ownership, state) {
		t.Fatal("Frankfurt acquisition and later publication were treated as contradictory tenures")
	}
	// The owner's JSON alone must never stand in for actual generation proof.
	if err := verifyCurrentDNSEngineOwnership(ownership, state, nil); err == nil {
		t.Fatal("owner JSON alone was treated as a verified live tree")
	}
}

func TestBINDPublicationOwnershipAfterRecordEditKeepsCatalogSerial(t *testing.T) {
	ownership, state, generation := publishedBINDOwnershipFixture(t)
	previous, err := publicationGenerationTree(generation)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := binddns.ApplyDelta(previous, testBINDV3Snapshot(t, "celikhost.com", 2, 2, false))
	if err != nil {
		t.Fatal(err)
	}
	generation, err = binddns.RenderTree(aptBINDGenerationRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := bindStateForPublishedReceipt(state, generation.ReceiptValue)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Generation == state.Generation || edited.PrimaryCatalogSerial != state.PrimaryCatalogSerial {
		t.Fatal("record edit fixture did not retain catalog membership")
	}
	verify := publicationTreeVerifier(t, &generation)
	// Both an acquisition receipt and a source receipt refreshed after the
	// first publication describe the same engine tenure after a record edit.
	for _, prior := range []dnsEngineStateReceipt{ownership, state} {
		if err := verifyCurrentDNSEngineOwnership(prior, edited, verify); err != nil {
			t.Fatal(err)
		}
	}
}
