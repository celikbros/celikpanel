// Package firewallpolicy defines the persisted firewall producer/reader contract.
// It performs no filesystem access, host execution, licensing or Agent calls.
package firewallpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const (
	Table           = "celikpanel_fw"
	Version         = 2
	MaxSnapshotSize = 64 << 10
)

// Policy separates owner/service ports from observed SSH ports at save time.
type Policy struct {
	Version        int   `json:"version"`
	TCPPorts       []int `json:"tcp_ports"`
	UDPPorts       []int `json:"udp_ports"`
	SSHPortsAtSave []int `json:"ssh_ports_at_save"`
}

// Version 2 keeps operator/service ports separate from automatically protected
// SSH ports, so boot can add the current trusted sshd configuration safely.
// Sürüm 2, operatör/servis portlarını otomatik korunan SSH portlarından ayırır;
// böylece açılış güncel ve güvenilir sshd yapılandırmasını güvenle ekleyebilir.
func Encode(tcp, udp, ssh []int) []byte {
	policy := Policy{
		Version:        Version,
		TCPPorts:       dedupeSorted(tcp),
		UDPPorts:       dedupeSorted(udp),
		SSHPortsAtSave: dedupeSorted(ssh),
	}
	data, _ := json.Marshal(policy)
	return append(data, '\n')
}

// Decode accepts only canonical V2 JSON or the exact legacy
// ruleset emitted by older CelikPanel builds. Arbitrary nft text never reaches
// the privileged `nft -f` command.
// Decode yalnız kanonik V2 JSON'u veya eski CelikPanel
// sürümlerinin ürettiği tam kural kümesini kabul eder. Keyfi nft metni
// ayrıcalıklı `nft -f` komutuna asla ulaşmaz.
func Decode(data []byte) (Policy, bool, error) {
	if len(data) == 0 || len(data) > MaxSnapshotSize {
		return Policy{}, false, fmt.Errorf("persistent firewall snapshot has invalid size %d", len(data))
	}
	if data[0] == '{' {
		var policy Policy
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policy); err != nil {
			return Policy{}, false, fmt.Errorf("decode persistent firewall snapshot: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return Policy{}, false, fmt.Errorf("persistent firewall snapshot has trailing data")
		}
		if policy.Version != Version {
			return Policy{}, false, fmt.Errorf("unsupported persistent firewall snapshot version %d", policy.Version)
		}
		if !bytes.Equal(data, Encode(policy.TCPPorts, policy.UDPPorts, policy.SSHPortsAtSave)) {
			return Policy{}, false, fmt.Errorf("persistent firewall snapshot is not canonical")
		}
		return policy, false, nil
	}

	var tcp, udp []int
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if p, ok := parsePortLine(line, "tcp dport"); ok {
			tcp = p
		}
		if p, ok := parsePortLine(line, "udp dport"); ok {
			udp = p
		}
	}
	expected := Ruleset(false, dedupeSorted(tcp), dedupeSorted(udp))
	if string(data) != expected {
		return Policy{}, false, fmt.Errorf("persistent firewall snapshot does not match CelikPanel's exact legacy ruleset")
	}
	return Policy{
		Version:  Version,
		TCPPorts: dedupeSorted(tcp),
		UDPPorts: dedupeSorted(udp),
	}, true, nil
}

// RestoreRuleset combines persisted intent with the independently verified current
// SSH configuration. It retains saved SSH ports during transitions. Absence or
// discovery failure must be distinguished by the caller before invoking this.
// A successful result is a plan, not evidence that the kernel accepted it.
func RestoreRuleset(snapshot []byte, configuredSSH []int, replace bool) (string, error) {
	policy, legacy, err := Decode(snapshot)
	if err != nil {
		return "", err
	}
	for _, port := range configuredSSH {
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("invalid verified SSH port %d", port)
		}
	}
	tcp := append(append([]int{}, policy.TCPPorts...), configuredSSH...)
	if !legacy {
		tcp = append(tcp, policy.SSHPortsAtSave...)
	}
	return Ruleset(replace, tcp, policy.UDPPorts), nil
}

// Ruleset renders only the fixed owned table; it never flushes other tables.
// Callers must discover current SSH ports and validate authority separately.
func Ruleset(replace bool, tcp, udp []int) string {
	tcp = dedupeSorted(tcp)
	udp = dedupeSorted(udp)
	var b strings.Builder
	if replace {
		b.WriteString(fmt.Sprintf("delete table inet %s\n", Table))
	}
	b.WriteString(fmt.Sprintf("table inet %s {\n", Table))
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority 0; policy drop;\n")
	b.WriteString("    iif lo accept\n")
	b.WriteString("    ct state established,related accept\n")
	b.WriteString("    ct state invalid drop\n")
	b.WriteString("    meta l4proto icmp accept\n")
	b.WriteString("    meta l4proto ipv6-icmp accept\n")
	if len(tcp) > 0 {
		b.WriteString(fmt.Sprintf("    tcp dport { %s } accept\n", joinInts(tcp)))
	}
	if len(udp) > 0 {
		b.WriteString(fmt.Sprintf("    udp dport { %s } accept\n", joinInts(udp)))
	}
	b.WriteString("  }\n}\n")
	return b.String()
}

func parsePortLine(line, prefix string) ([]int, bool) {
	if !strings.HasPrefix(line, prefix) {
		return nil, false
	}
	rest := line[len(prefix):]
	// `udp dports` and `udp dport` are different rules; a prefix match alone
	// would read the first as the second.
	//
	// `udp dports` ile `udp dport` ayrı kurallardır; yalnız önek eşleşmesi
	// birincisini ikincisi diye okurdu.
	if rest == "" || (rest[0] != ' ' && rest[0] != '	') {
		return nil, false
	}
	rest = strings.TrimLeft(rest, " 	")
	if strings.HasPrefix(rest, "{") {
		closing := strings.IndexByte(rest, '}')
		if closing < 0 {
			return nil, false
		}
		return parseFirewallPortSet(rest[1:closing]), true
	}
	// The unbraced form carries exactly one port. Anything else in that
	// position - a range, a negation, a named set - is not a port list this
	// reader can claim to understand, and it says so by not answering rather
	// than by answering nothing, which would erase a real rule read earlier.
	//
	// Parantezsiz biçim tam olarak bir port taşır. O konumdaki başka bir şey -
	// bir aralık, bir olumsuzlama, adlandırılmış bir küme - bu okuyucunun
	// anladığını iddia edebileceği bir port listesi değildir; bunu, daha önce
	// okunmuş gerçek bir kuralı silecek olan "hiç" cevabını vermek yerine hiç
	// cevap vermeyerek söyler.
	field := rest
	if cut := strings.IndexAny(field, " 	"); cut >= 0 {
		field = field[:cut]
	}
	port, err := strconv.Atoi(field)
	if err != nil {
		return nil, false
	}
	return []int{port}, true
}

func parseFirewallPortSet(body string) []int {
	var ports []int
	for _, token := range strings.Split(body, ",") {
		if port, err := strconv.Atoi(strings.TrimSpace(token)); err == nil {
			ports = append(ports, port)
		}
	}
	return ports
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func dedupeSorted(ns []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range ns {
		if n > 0 && n < 65536 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}
