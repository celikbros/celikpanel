package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"net"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
)

const (
	dnsClusterConfigSchema          = "dns-cluster-config/v1"
	dnsClusterConfigQualifierPrefix = dnsClusterConfigSchema + ":sha256:"
)

var dnsClusterConfigDigestFrames = [][]byte{
	[]byte("celikpanel/service-mutation-payload"),
	[]byte(dnsClusterConfigSchema),
	[]byte("dns_cluster_configure"),
	[]byte("pdns"),
	[]byte("Agent.ConfigureDNSClusterV2"),
	[]byte("configure"),
}

type DNSClusterConfigCommitment struct {
	Role      string
	PeerIP    string
	PeerNS    string
	Qualifier string
}

func CanonicalDNSClusterConfig(
	role, peerIP, peerNS string,
) (DNSClusterConfigCommitment, error) {
	if role != strings.TrimSpace(role) || peerIP != strings.TrimSpace(peerIP) ||
		peerNS != strings.TrimSpace(peerNS) {
		return DNSClusterConfigCommitment{}, errors.New("DNS cluster payload must be canonical")
	}
	commitment := DNSClusterConfigCommitment{Role: role}
	switch role {
	case "standalone":
		if peerIP != "" || peerNS != "" {
			return DNSClusterConfigCommitment{}, errors.New("standalone DNS cluster payload must not contain peer fields")
		}
	case "paired":
		parsed := net.ParseIP(peerIP)
		if parsed == nil || parsed.To4() == nil || parsed.String() != peerIP ||
			!parsed.IsGlobalUnicast() {
			return DNSClusterConfigCommitment{}, errors.New("DNS cluster peer IPv4 address must be canonical")
		}
		canonicalNS, err := hostname.CanonicalFQDN(peerNS)
		if err != nil || canonicalNS != peerNS {
			return DNSClusterConfigCommitment{}, errors.New("DNS cluster peer nameserver must be canonical")
		}
		commitment.PeerIP = peerIP
		commitment.PeerNS = canonicalNS
	default:
		return DNSClusterConfigCommitment{}, errors.New("DNS cluster role must be standalone or paired")
	}

	digest := sha256.New()
	for _, frame := range dnsClusterConfigDigestFrames {
		writeDNSClusterConfigDigestFrame(digest, frame)
	}
	writeDNSClusterConfigDigestFrame(digest, []byte(commitment.Role))
	writeDNSClusterConfigDigestFrame(digest, []byte(commitment.PeerIP))
	writeDNSClusterConfigDigestFrame(digest, []byte(commitment.PeerNS))
	commitment.Qualifier = dnsClusterConfigQualifierPrefix +
		hex.EncodeToString(digest.Sum(nil))
	return commitment, nil
}

func writeDNSClusterConfigDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func ValidDNSClusterConfigQualifier(value string) bool {
	if len(value) != len(dnsClusterConfigQualifierPrefix)+sha256.Size*2 ||
		!strings.HasPrefix(value, dnsClusterConfigQualifierPrefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(value, dnsClusterConfigQualifierPrefix) {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
