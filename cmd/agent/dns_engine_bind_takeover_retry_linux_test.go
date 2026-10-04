//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func stoppedBINDTakeoverTestManifest(t *testing.T) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEngineBIND,
		0, 1, 1, transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !adoptableRunningBINDManifest(manifest, false) {
		t.Fatal("fixture is not the takeover-shaped manifest")
	}
	return manifest
}

// The takeover mode still refuses what it cannot read as its own statement;
// that refusal is covered unchanged by TestBINDTakeoverRefusesByNameWhatItCannotRead.
//
// Register row 12: a stopped, unmanaged BIND taken over through the
// fresh-install transaction. Its first attempt finds the packages present,
// selects the takeover options mode and writes an adopted-present install
// receipt bound to the request; a rollback keeps that receipt. Before the fix
// the same-request retry read the receipt as CelikPanel authority and
// selected the exclusive mode, refusing the directives the first attempt had
// adopted.
func TestStoppedBINDTakeoverRetryMakesTheFirstAttemptsOptionsDecision(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	manifest := stoppedBINDTakeoverTestManifest(t)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("7", 32), MutationOwnerID: strings.Repeat("8", 32),
	}
	packages := []string{"bind9", "bind9-utils"}

	first, err := bindSwitchOptionsAuthority(manifest, false, binding)
	if err != nil || first != bindOptionsTakeover {
		t.Fatalf("first attempt authority=%v err=%v", first, err)
	}
	// The first attempt's own adopted-present receipt (packages were there).
	if err := assumeExistingDNSEnginePackageOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT, packages, manifest, binding,
	); err != nil {
		t.Fatal(err)
	}
	receipt, exists, err := readDNSEngineInstallOwnership(transport.DNSEngineBIND)
	if err != nil || !exists || !receipt.AdoptedPresent {
		t.Fatalf("first attempt receipt=%+v exists=%v err=%v", receipt, exists, err)
	}
	retry, err := bindSwitchOptionsAuthority(manifest, false, binding)
	if err != nil || retry != first {
		t.Fatalf("same-request retry authority=%v, first attempt=%v, err=%v", retry, first, err)
	}

	// Anything that is not this request's own adopted-present receipt keeps
	// the exclusive mode, as before.
	other := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("9", 32), MutationOwnerID: binding.MutationOwnerID,
	}
	if got, err := bindSwitchOptionsAuthority(manifest, false, other); err != nil || got != bindOptionsExclusive {
		t.Fatalf("another request's surviving receipt selected %v err=%v", got, err)
	}
	if got, err := bindSwitchOptionsAuthority(manifest, true, binding); err != nil || got != bindOptionsExclusive {
		t.Fatalf("an existing engine state selected %v err=%v", got, err)
	}
	installed, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT, packages, []string{"bind9"}, manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineInstallOwnership(installed); err != nil {
		t.Fatal(err)
	}
	if got, err := bindSwitchOptionsAuthority(manifest, false, binding); err != nil || got != bindOptionsExclusive {
		t.Fatalf("a receipt recording that CelikPanel installed BIND selected %v err=%v", got, err)
	}
	if err := writeDNSEngineInstallOwnership(receipt); err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineOwnership(legacyDurableDNSState(transport.DNSEngineBIND)); err != nil {
		t.Fatal(err)
	}
	if got, err := bindSwitchOptionsAuthority(manifest, false, binding); err != nil || got != bindOptionsExclusive {
		t.Fatalf("an active ownership receipt selected %v err=%v", got, err)
	}
}
