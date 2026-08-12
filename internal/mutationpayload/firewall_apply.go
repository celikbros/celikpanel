package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"sort"
	"strings"
)

const (
	firewallApplySchema          = "firewall-apply/v1"
	firewallApplyQualifierPrefix = firewallApplySchema + ":sha256:"
	firewallApplyMaxPorts        = 4096
)

var firewallApplyDigestFrames = [][]byte{
	[]byte("celikpanel/service-mutation-payload"),
	[]byte(firewallApplySchema),
	[]byte("nftables"),
	[]byte("Agent.ApplyFirewallV2"),
	[]byte("apply"),
}

// FirewallApplyCommitment is a canonical, detached firewall request. SSH
// listener ports are deliberately absent: the privileged agent discovers and
// protects them immediately before committing the host plan.
type FirewallApplyCommitment struct {
	Enabled   bool
	Persist   bool
	TCPPorts  []int
	UDPPorts  []int
	Qualifier string
}

// CanonicalFirewallApply validates, sorts, de-duplicates and freezes every
// caller-controlled firewall field before either side authorizes or applies it.
func CanonicalFirewallApply(
	enabled, persist bool,
	tcpPorts, udpPorts []int,
) (FirewallApplyCommitment, error) {
	if !enabled && (len(tcpPorts) != 0 || len(udpPorts) != 0) {
		return FirewallApplyCommitment{}, errors.New("disabled firewall request must not contain hidden ports")
	}
	tcp, err := canonicalFirewallPorts(tcpPorts)
	if err != nil {
		return FirewallApplyCommitment{}, errors.New("invalid TCP firewall ports")
	}
	udp, err := canonicalFirewallPorts(udpPorts)
	if err != nil {
		return FirewallApplyCommitment{}, errors.New("invalid UDP firewall ports")
	}

	digest := sha256.New()
	for _, frame := range firewallApplyDigestFrames {
		writeFirewallApplyDigestFrame(digest, frame)
	}
	if enabled {
		_, _ = digest.Write([]byte{1})
	} else {
		_, _ = digest.Write([]byte{0})
	}
	if persist {
		_, _ = digest.Write([]byte{1})
	} else {
		_, _ = digest.Write([]byte{0})
	}
	writeFirewallPortSet(digest, tcp)
	writeFirewallPortSet(digest, udp)

	return FirewallApplyCommitment{
		Enabled:   enabled,
		Persist:   persist,
		TCPPorts:  tcp,
		UDPPorts:  udp,
		Qualifier: firewallApplyQualifierPrefix + hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func canonicalFirewallPorts(ports []int) ([]int, error) {
	if len(ports) > firewallApplyMaxPorts {
		return nil, errors.New("firewall port set exceeds the limit")
	}
	frozen := append([]int(nil), ports...)
	for _, port := range frozen {
		if port < 1 || port > 65535 {
			return nil, errors.New("firewall port is outside 1..65535")
		}
	}
	sort.Ints(frozen)
	result := frozen[:0]
	for _, port := range frozen {
		if len(result) == 0 || result[len(result)-1] != port {
			result = append(result, port)
		}
	}
	return result, nil
}

func writeFirewallApplyDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func writeFirewallPortSet(destination hash.Hash, ports []int) {
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(ports)))
	_, _ = destination.Write(count[:])
	var encoded [2]byte
	for _, port := range ports {
		binary.BigEndian.PutUint16(encoded[:], uint16(port))
		_, _ = destination.Write(encoded[:])
	}
}

// ValidFirewallApplyQualifier accepts only the canonical v1 lowercase SHA-256
// representation stored in ServiceMutationJob.PackageName.
func ValidFirewallApplyQualifier(value string) bool {
	if len(value) != len(firewallApplyQualifierPrefix)+sha256.Size*2 ||
		!strings.HasPrefix(value, firewallApplyQualifierPrefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(value, firewallApplyQualifierPrefix) {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
