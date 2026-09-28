package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestPendingExactBINDV3OwnerEditRequiresCommittedDeletionIdentity(t *testing.T) {
	const domain = "example.test"
	zone := testBINDV3Snapshot(t, domain, 2, 2, true)
	tree, generation := testBINDV3PrimaryTree(t, 2, zone)
	receipt := tree.CurrentReceipt()
	state := dnsEngineStateReceipt{Generation: generation.ID}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: zone.MutationRequestID,
		MutationOwnerID:   zone.MutationOwnerID,
	}
	conflicts := []error{
		fmt.Errorf("prepare BIND zone include: %w", bindconfig.ErrManagedZoneIncludeModified),
		fmt.Errorf("prepare BIND authoritative options: %w", errManagedBINDOptionsModified),
	}
	for _, conflict := range conflicts {
		pending := pendingExactBINDV3OwnerEdit(state, receipt, tree, domain, zone.Qualifier, binding, conflict)
		var reviewed *dnsZoneV3RecoveryPendingError
		if !errors.As(pending, &reviewed) || reviewed.code != transport.DNSPeerPendingOwnerEditUnknown {
			t.Fatalf("exact owner edit must remain pending, got %v", pending)
		}
	}
	if got := pendingExactBINDV3OwnerEdit(state, receipt, tree, domain, zone.Qualifier, binding, errors.New("unrelated disk error")); got != nil {
		t.Fatalf("unrelated failure was reclassified: %v", got)
	}
	stale := state
	stale.Generation = "stale-generation"
	if got := pendingExactBINDV3OwnerEdit(stale, receipt, tree, domain, zone.Qualifier, binding, conflicts[0]); got != nil {
		t.Fatalf("stale state was reclassified: %v", got)
	}
	wrong := binding
	wrong.MutationOwnerID = "other-owner"
	if got := pendingExactBINDV3OwnerEdit(state, receipt, tree, domain, zone.Qualifier, wrong, conflicts[0]); got != nil {
		t.Fatalf("wrong owner was reclassified: %v", got)
	}
	if got := pendingExactBINDV3OwnerEdit(state, receipt, tree, domain, "other-qualifier", binding, conflicts[0]); got != nil {
		t.Fatalf("wrong qualifier was reclassified: %v", got)
	}
	noPair := receipt
	noPair.Pairing = nil
	if got := pendingExactBINDV3OwnerEdit(state, noPair, tree, domain, zone.Qualifier, binding, conflicts[0]); got != nil {
		t.Fatalf("standalone receipt was reclassified: %v", got)
	}
	updateTree, updateGeneration := testBINDV3PrimaryTree(t, 2, testBINDV3Snapshot(t, domain, 2, 2, false))
	updateState := dnsEngineStateReceipt{Generation: updateGeneration.ID}
	if got := pendingExactBINDV3OwnerEdit(updateState, updateTree.CurrentReceipt(), updateTree, domain, zone.Qualifier, binding, conflicts[0]); got != nil {
		t.Fatalf("non-deletion was reclassified: %v", got)
	}
}

func TestManagedBINDOptionsOwnerEditHasTypedConflict(t *testing.T) {
	original := "options { directory \"/var/cache/bind\"; };\n"
	managed, err := managedBINDOptions(original, "")
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(managed, "recursion no;", "recursion yes;", 1)
	if edited == managed {
		t.Fatal("fixture did not edit the managed span")
	}
	_, err = managedBINDOptions(edited, "")
	if !errors.Is(err, errManagedBINDOptionsModified) {
		t.Fatalf("managed options edit lost typed conflict: %v", err)
	}
}
