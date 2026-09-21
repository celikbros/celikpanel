package firewallpolicy

import (
	"bytes"
	"strings"
	"testing"
)

// Pin actual persisted v2 wire shape, including null (not []) empty slices and
// saved SSH identity distinct from requested service ports.
func TestV2WireCompatibility(t *testing.T) {
	want := []byte("{\"version\":2,\"tcp_ports\":[80,2083],\"udp_ports\":null,\"ssh_ports_at_save\":[2222]}\n")
	data := Encode([]int{2083, 80, 80}, nil, []int{2222})
	if !bytes.Equal(data, want) {
		t.Fatalf("persisted v2 bytes changed: %s", data)
	}
	p, legacy, err := Decode(want)
	if err != nil || legacy || len(p.TCPPorts) != 2 || len(p.SSHPortsAtSave) != 1 || p.SSHPortsAtSave[0] != 2222 {
		t.Fatalf("v2 read: %+v %v %v", p, legacy, err)
	}
}

func TestLegacyOnlyExactGeneratedRulesAccepted(t *testing.T) {
	raw := []byte("table inet celikpanel_fw {\n  chain input {\n    type filter hook input priority 0; policy drop;\n    iif lo accept\n    ct state established,related accept\n    ct state invalid drop\n    meta l4proto icmp accept\n    meta l4proto ipv6-icmp accept\n    tcp dport { 22, 2083 } accept\n    udp dport { 53 } accept\n  }\n}\n")
	p, legacy, err := Decode(raw)
	if err != nil || !legacy || len(p.TCPPorts) != 2 || len(p.UDPPorts) != 1 || p.SSHPortsAtSave != nil {
		t.Fatalf("legacy: %+v %v %v", p, legacy, err)
	}
	for _, bad := range [][]byte{append([]byte("flush ruleset\n"), raw...), bytes.Replace(raw, []byte("policy drop"), []byte("policy accept"), 1), bytes.Replace(raw, []byte("{ 22, 2083 }"), []byte("{ 22, 2083, invalid }"), 1), append(raw, []byte("include \"/etc/other\"\n")...)} {
		if _, _, err := Decode(bad); err == nil {
			t.Fatalf("accepted arbitrary legacy rules: %s", bad)
		}
	}
}

func TestNoncanonicalVersionedInputRejected(t *testing.T) {
	good := string(Encode([]int{80}, []int{53}, []int{22}))
	for _, bad := range []string{"", strings.Repeat("x", MaxSnapshotSize+1), strings.Replace(good, "\"version\":2", "\"version\":3", 1), strings.Replace(good, "\"version\":2", "\"version\":2,\"version\":2", 1), strings.Replace(good, "[80]", "[80,80]", 1), strings.Replace(good, "[80]", "[0]", 1), strings.Replace(good, "[80]", "[65536]", 1), strings.Replace(good, "}", ",\"other\":true}", 1), good + "{}", strings.TrimSuffix(good, "\n"), " " + good} {
		if _, _, err := Decode([]byte(bad)); err == nil {
			t.Fatalf("accepted unsupported snapshot: %.200s", bad)
		}
	}
}

func TestRulesetOnlyReplacesOwnedTable(t *testing.T) {
	rules := Ruleset(true, []int{2083, 22}, []int{53})
	if !strings.HasPrefix(rules, "delete table inet celikpanel_fw\ntable inet celikpanel_fw {\n") || strings.Contains(rules, "flush") || !strings.Contains(rules, "tcp dport { 22, 2083 } accept") {
		t.Fatal(rules)
	}
}

func TestRestorePreservesCurrentAndSavedSSHWithoutMutatingPolicy(t *testing.T) {
	snapshot := Encode([]int{80, 2083}, []int{53}, []int{2222})
	original := append([]byte(nil), snapshot...)
	rules, err := RestoreRuleset(snapshot, []int{2200}, true)
	if err != nil || !strings.Contains(rules, "tcp dport { 80, 2083, 2200, 2222 } accept") || !strings.Contains(rules, "udp dport { 53 } accept") || !bytes.Equal(snapshot, original) {
		t.Fatalf("restore changed intent or SSH: %s %v", rules, err)
	}
	// Legacy has no separate saved-SSH list; its original TCP set is retained.
	rules, err = RestoreRuleset([]byte(Ruleset(false, []int{22, 2083}, nil)), []int{2200}, false)
	if err != nil || !strings.Contains(rules, "tcp dport { 22, 2083, 2200 } accept") || strings.HasPrefix(rules, "delete") {
		t.Fatalf("legacy restore: %s %v", rules, err)
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := RestoreRuleset(snapshot, []int{port}, false); err == nil {
			t.Fatalf("invalid configured SSH port %d accepted", port)
		}
	}
}
