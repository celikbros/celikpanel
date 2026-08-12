//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestVPNPeerSyncCommitPhaseCanonicalRoundTripPreservesQualifierSlash(t *testing.T) {
	requestID := strings.Repeat("a", 32)
	qualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("b", 64)
	for _, state := range []string{vpnPeerSyncCommitIntent, vpnPeerSyncCommitPublished} {
		phase, err := formatVPNPeerSyncCommitPhase(state, requestID, qualifier)
		if err != nil {
			t.Fatal(err)
		}
		want := vpnPeerSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier
		if phase != want {
			t.Fatalf("phase=%q want=%q", phase, want)
		}
		gotState, gotRequestID, gotQualifier, err := parseVPNPeerSyncCommitPhase(phase)
		if err != nil {
			t.Fatal(err)
		}
		if gotState != state || gotRequestID != requestID || gotQualifier != qualifier {
			t.Fatalf("parsed phase=%q request=%q qualifier=%q", gotState, gotRequestID, gotQualifier)
		}
	}
}

func TestVPNPeerSyncReceiptMarkerIsExactAndReplaceable(t *testing.T) {
	requestID := strings.Repeat("c", 32)
	qualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("d", 64)
	config, err := replaceVPNPeerSyncReceiptMarker(
		"[Interface]\nPrivateKey = key\n",
		requestID,
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	gotRequestID, gotQualifier, found, err := parseVPNPeerSyncReceiptMarker([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if !found || gotRequestID != requestID || gotQualifier != qualifier {
		t.Fatalf("receipt found=%v request=%q qualifier=%q", found, gotRequestID, gotQualifier)
	}
	withoutReceipt, err := removeVPNPeerSyncReceiptMarker(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutReceipt, vpnPeerSyncReceiptMarkerPrefix) {
		t.Fatalf("receipt was not removed: %q", withoutReceipt)
	}
	if _, _, _, err := parseVPNPeerSyncReceiptMarker([]byte(config + strings.TrimSpace(config) + "\n")); err == nil {
		t.Fatal("duplicate VPN peer receipt was accepted")
	}
}

func TestVPNPeerSyncCommitIntentCannotBeOverwrittenByHeartbeat(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	qualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("e", 64)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"vpn_peer_sync",
		"wireguard",
		qualifier,
	)
	intent, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitIntent,
		testMutationRequestID,
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.Phase = intent
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()

	job, err := manager.heartbeat(&ServiceMutationHeartbeatRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Phase:     "panel_progress_must_not_erase_commit_intent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Phase != intent {
		t.Fatalf("heartbeat erased durable commit intent: %+v", job)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}
}
