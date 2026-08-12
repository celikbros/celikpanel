package mutationpayload

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"net/netip"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	vpnPeerSyncSchema          = "vpn-peer-sync/v1"
	vpnPeerSyncQualifierPrefix = vpnPeerSyncSchema + ":sha256:"
	vpnPeerSyncMaxPeers        = 253
)

var vpnPeerSyncDigestFrames = [][]byte{
	[]byte("celikpanel/service-mutation-payload"),
	[]byte(vpnPeerSyncSchema),
	[]byte("vpn_peer_sync"),
	[]byte("wireguard"),
	[]byte("Agent.SyncVPNPeersV2"),
	[]byte("sync"),
}

// VPNPeerSyncCommitment contains the canonical, detached payload that must be
// sent to the agent together with the qualifier committed by the durable
// service-mutation job.
type VPNPeerSyncCommitment struct {
	DesiredGeneration int64
	Peers             []transport.VPNPeerSpec
	Qualifier         string
}

type canonicalVPNPeer struct {
	spec         transport.VPNPeerSpec
	publicKeyRaw []byte
	presharedRaw []byte
	address      [4]byte
}

// CanonicalVPNPeerSync validates and freezes a complete WireGuard peer
// snapshot before either side hashes or applies it. The returned peer slice
// never aliases the caller's slice.
func CanonicalVPNPeerSync(
	desiredGeneration int64,
	peers []transport.VPNPeerSpec,
) (VPNPeerSyncCommitment, error) {
	if desiredGeneration < 0 {
		return VPNPeerSyncCommitment{}, errors.New("VPN peer generation must not be negative")
	}
	if len(peers) > vpnPeerSyncMaxPeers {
		return VPNPeerSyncCommitment{}, errors.New("VPN peer snapshot exceeds the WireGuard address pool")
	}

	canonical := make([]canonicalVPNPeer, len(peers))
	publicKeys := make(map[string]struct{}, len(peers))
	addresses := make(map[string]struct{}, len(peers))
	for index, peer := range peers {
		publicKey, publicKeyRaw, err := canonicalWireGuardKey(peer.PublicKey)
		if err != nil {
			return VPNPeerSyncCommitment{}, errors.New("VPN peer snapshot contains an invalid public key")
		}
		presharedKey, presharedRaw, err := canonicalWireGuardKey(peer.PresharedKey)
		if err != nil {
			return VPNPeerSyncCommitment{}, errors.New("VPN peer snapshot contains an invalid preshared key")
		}
		address, addressBytes, err := canonicalVPNPeerAddress(peer.IP)
		if err != nil {
			return VPNPeerSyncCommitment{}, err
		}
		if _, duplicate := publicKeys[publicKey]; duplicate {
			return VPNPeerSyncCommitment{}, errors.New("VPN peer snapshot contains a duplicate public key")
		}
		if _, duplicate := addresses[address]; duplicate {
			return VPNPeerSyncCommitment{}, errors.New("VPN peer snapshot contains a duplicate address")
		}
		publicKeys[publicKey] = struct{}{}
		addresses[address] = struct{}{}
		canonical[index] = canonicalVPNPeer{
			spec: transport.VPNPeerSpec{
				PublicKey:    publicKey,
				PresharedKey: presharedKey,
				IP:           address,
			},
			publicKeyRaw: publicKeyRaw,
			presharedRaw: presharedRaw,
			address:      addressBytes,
		}
	}

	sort.Slice(canonical, func(left, right int) bool {
		if comparison := bytes.Compare(canonical[left].address[:], canonical[right].address[:]); comparison != 0 {
			return comparison < 0
		}
		return bytes.Compare(canonical[left].publicKeyRaw, canonical[right].publicKeyRaw) < 0
	})

	digest := sha256.New()
	for _, frame := range vpnPeerSyncDigestFrames {
		writeVPNPeerSyncDigestFrame(digest, frame)
	}
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], uint64(desiredGeneration))
	_, _ = digest.Write(number[:])
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(canonical)))
	_, _ = digest.Write(count[:])
	frozen := make([]transport.VPNPeerSpec, len(canonical))
	for index, peer := range canonical {
		writeVPNPeerSyncDigestFrame(digest, peer.publicKeyRaw)
		writeVPNPeerSyncDigestFrame(digest, peer.presharedRaw)
		writeVPNPeerSyncDigestFrame(digest, []byte(peer.spec.IP))
		frozen[index] = peer.spec
	}

	return VPNPeerSyncCommitment{
		DesiredGeneration: desiredGeneration,
		Peers:             frozen,
		Qualifier:         vpnPeerSyncQualifierPrefix + hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func writeVPNPeerSyncDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func canonicalWireGuardKey(value string) (string, []byte, error) {
	// A 32-byte WireGuard key has exactly 44 canonical padded-base64 bytes.
	// DecodeString deliberately ignores CR/LF, so both the exact length and
	// round-trip equality are security properties rather than conveniences.
	if len(value) != 44 || strings.TrimSpace(value) != value {
		return "", nil, errors.New("invalid WireGuard key")
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != 32 {
		return "", nil, errors.New("invalid WireGuard key")
	}
	canonical := base64.StdEncoding.EncodeToString(raw)
	if canonical != value {
		return "", nil, errors.New("invalid WireGuard key")
	}
	return canonical, raw, nil
}

func canonicalVPNPeerAddress(value string) (string, [4]byte, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", [4]byte{}, errors.New("VPN peer snapshot contains an invalid address")
	}
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() {
		return "", [4]byte{}, errors.New("VPN peer snapshot contains an invalid address")
	}
	octets := address.As4()
	if octets[0] != 10 || octets[1] != 8 || octets[2] != 0 ||
		octets[3] < 2 || octets[3] > 254 {
		return "", [4]byte{}, errors.New("VPN peer snapshot contains an invalid address")
	}
	canonical := address.String()
	if canonical != value {
		return "", [4]byte{}, errors.New("VPN peer snapshot contains an invalid address")
	}
	return canonical, octets, nil
}

// ValidVPNPeerSyncQualifier accepts only the canonical v1 lowercase SHA-256
// representation stored in ServiceMutationJob.PackageName.
func ValidVPNPeerSyncQualifier(value string) bool {
	if len(value) != len(vpnPeerSyncQualifierPrefix)+sha256.Size*2 ||
		!strings.HasPrefix(value, vpnPeerSyncQualifierPrefix) {
		return false
	}
	encoded := strings.TrimPrefix(value, vpnPeerSyncQualifierPrefix)
	for _, character := range encoded {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
