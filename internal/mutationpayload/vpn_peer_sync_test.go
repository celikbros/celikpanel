package mutationpayload

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func vpnCommitmentTestKey(value byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32))
}

func TestCanonicalVPNPeerSyncIsStableAndDetached(t *testing.T) {
	first := transport.VPNPeerSpec{
		PublicKey: vpnCommitmentTestKey(1), PresharedKey: vpnCommitmentTestKey(2), IP: "10.8.0.9",
	}
	second := transport.VPNPeerSpec{
		PublicKey: vpnCommitmentTestKey(3), PresharedKey: vpnCommitmentTestKey(4), IP: "10.8.0.2",
	}
	input := []transport.VPNPeerSpec{first, second}

	commitment, err := CanonicalVPNPeerSync(7, input)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := CanonicalVPNPeerSync(7, []transport.VPNPeerSpec{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if commitment.Qualifier != reordered.Qualifier {
		t.Fatalf("equivalent snapshots produced different qualifiers: %q / %q", commitment.Qualifier, reordered.Qualifier)
	}
	if !ValidVPNPeerSyncQualifier(commitment.Qualifier) {
		t.Fatalf("generated qualifier is invalid: %q", commitment.Qualifier)
	}
	const wantQualifier = "vpn-peer-sync/v1:sha256:0d06acceb36f3008f161599e3d64ff4f5e4f19182316d5da34b51d7bb54804b6"
	if commitment.Qualifier != wantQualifier {
		t.Fatalf("qualifier=%q want=%q", commitment.Qualifier, wantQualifier)
	}
	if len(commitment.Peers) != 2 || commitment.Peers[0].IP != "10.8.0.2" || commitment.Peers[1].IP != "10.8.0.9" {
		t.Fatalf("canonical peers=%#v", commitment.Peers)
	}
	input[0].IP = "10.8.0.44"
	if commitment.Peers[1].IP != "10.8.0.9" {
		t.Fatal("canonical peer snapshot aliases caller memory")
	}
}

func TestCanonicalVPNPeerSyncSeparatesGenerationAndPayload(t *testing.T) {
	peer := transport.VPNPeerSpec{
		PublicKey: vpnCommitmentTestKey(5), PresharedKey: vpnCommitmentTestKey(6), IP: "10.8.0.7",
	}
	base, err := CanonicalVPNPeerSync(10, []transport.VPNPeerSpec{peer})
	if err != nil {
		t.Fatal(err)
	}
	nextGeneration, err := CanonicalVPNPeerSync(11, []transport.VPNPeerSpec{peer})
	if err != nil {
		t.Fatal(err)
	}
	changedPSKSpec := peer
	changedPSKSpec.PresharedKey = vpnCommitmentTestKey(8)
	changedPSK, err := CanonicalVPNPeerSync(10, []transport.VPNPeerSpec{changedPSKSpec})
	if err != nil {
		t.Fatal(err)
	}
	changedPublicSpec := peer
	changedPublicSpec.PublicKey = vpnCommitmentTestKey(7)
	changedPublic, err := CanonicalVPNPeerSync(10, []transport.VPNPeerSpec{changedPublicSpec})
	if err != nil {
		t.Fatal(err)
	}
	changedAddressSpec := peer
	changedAddressSpec.IP = "10.8.0.8"
	changedAddress, err := CanonicalVPNPeerSync(10, []transport.VPNPeerSpec{changedAddressSpec})
	if err != nil {
		t.Fatal(err)
	}
	for name, commitment := range map[string]VPNPeerSyncCommitment{
		"generation":    nextGeneration,
		"preshared key": changedPSK,
		"public key":    changedPublic,
		"address":       changedAddress,
	} {
		if base.Qualifier == commitment.Qualifier {
			t.Fatalf("%s change did not change the commitment", name)
		}
	}
}

func TestCanonicalVPNPeerSyncCommitsEmptySnapshot(t *testing.T) {
	commitment, err := CanonicalVPNPeerSync(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidVPNPeerSyncQualifier(commitment.Qualifier) || commitment.Qualifier == "" {
		t.Fatalf("empty snapshot qualifier=%q", commitment.Qualifier)
	}
	empty, err := CanonicalVPNPeerSync(0, []transport.VPNPeerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Qualifier != commitment.Qualifier {
		t.Fatal("nil and empty peer snapshots produced different commitments")
	}
}

func TestCanonicalVPNPeerSyncRejectsUnsafeSnapshots(t *testing.T) {
	key := vpnCommitmentTestKey(9)
	psk := vpnCommitmentTestKey(10)
	tests := []struct {
		name       string
		generation int64
		peers      []transport.VPNPeerSpec
	}{
		{name: "negative generation", generation: -1},
		{name: "invalid public key", peers: []transport.VPNPeerSpec{{PublicKey: "bad", PresharedKey: psk, IP: "10.8.0.2"}}},
		{name: "public key with ignored newline", peers: []transport.VPNPeerSpec{{PublicKey: key[:8] + "\n" + key[8:], PresharedKey: psk, IP: "10.8.0.2"}}},
		{name: "invalid preshared key", peers: []transport.VPNPeerSpec{{PublicKey: key, PresharedKey: "bad", IP: "10.8.0.2"}}},
		{name: "noncanonical address", peers: []transport.VPNPeerSpec{{PublicKey: key, PresharedKey: psk, IP: "10.8.0.02"}}},
		{name: "address outside pool", peers: []transport.VPNPeerSpec{{PublicKey: key, PresharedKey: psk, IP: "10.8.1.2"}}},
		{name: "too many peers", peers: make([]transport.VPNPeerSpec, vpnPeerSyncMaxPeers+1)},
		{name: "duplicate public key", peers: []transport.VPNPeerSpec{
			{PublicKey: key, PresharedKey: psk, IP: "10.8.0.2"},
			{PublicKey: key, PresharedKey: vpnCommitmentTestKey(11), IP: "10.8.0.3"},
		}},
		{name: "duplicate address", peers: []transport.VPNPeerSpec{
			{PublicKey: key, PresharedKey: psk, IP: "10.8.0.2"},
			{PublicKey: vpnCommitmentTestKey(12), PresharedKey: vpnCommitmentTestKey(11), IP: "10.8.0.2"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CanonicalVPNPeerSync(test.generation, test.peers); err == nil {
				t.Fatal("unsafe snapshot was accepted")
			}
		})
	}
}

func TestValidVPNPeerSyncQualifierRejectsNonCanonicalValues(t *testing.T) {
	valid := vpnPeerSyncQualifierPrefix + strings.Repeat("a", 64)
	if !ValidVPNPeerSyncQualifier(valid) {
		t.Fatal("canonical qualifier was rejected")
	}
	for _, invalid := range []string{
		"", vpnPeerSyncQualifierPrefix, vpnPeerSyncQualifierPrefix + strings.Repeat("a", 63),
		vpnPeerSyncQualifierPrefix + strings.Repeat("A", 64),
		"vpn-peer-sync/v2:sha256:" + strings.Repeat("a", 64),
	} {
		if ValidVPNPeerSyncQualifier(invalid) {
			t.Fatalf("invalid qualifier accepted: %q", invalid)
		}
	}
}
