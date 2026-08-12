package mutationpayload

import (
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalFirewallApplyStableDetachedAndDomainSeparated(t *testing.T) {
	tcp := []int{443, 80, 443}
	udp := []int{53, 53}
	commitment, err := CanonicalFirewallApply(true, true, tcp, udp)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := CanonicalFirewallApply(true, true, []int{80, 443}, []int{53})
	if err != nil {
		t.Fatal(err)
	}
	if commitment.Qualifier != reordered.Qualifier {
		t.Fatalf("equivalent firewall requests produced different qualifiers: %q / %q", commitment.Qualifier, reordered.Qualifier)
	}
	if !ValidFirewallApplyQualifier(commitment.Qualifier) {
		t.Fatalf("generated qualifier is invalid: %q", commitment.Qualifier)
	}
	if !reflect.DeepEqual(commitment.TCPPorts, []int{80, 443}) ||
		!reflect.DeepEqual(commitment.UDPPorts, []int{53}) {
		t.Fatalf("canonical ports = TCP %#v UDP %#v", commitment.TCPPorts, commitment.UDPPorts)
	}
	tcp[0], udp[0] = 1, 2
	if !reflect.DeepEqual(commitment.TCPPorts, []int{80, 443}) ||
		!reflect.DeepEqual(commitment.UDPPorts, []int{53}) {
		t.Fatal("canonical firewall request aliases caller memory")
	}

	for name, input := range map[string]struct {
		enabled bool
		persist bool
		tcp     []int
		udp     []int
	}{
		"enabled":    {enabled: false, persist: true},
		"persist":    {enabled: true, persist: false, tcp: []int{80}, udp: []int{53}},
		"tcp":        {enabled: true, persist: true, tcp: []int{81}, udp: []int{53}},
		"udp":        {enabled: true, persist: true, tcp: []int{80}, udp: []int{54}},
		"tcp vs udp": {enabled: true, persist: true, tcp: []int{53, 80}, udp: nil},
	} {
		changed, changedErr := CanonicalFirewallApply(input.enabled, input.persist, input.tcp, input.udp)
		if changedErr != nil {
			t.Fatalf("%s: %v", name, changedErr)
		}
		if changed.Qualifier == commitment.Qualifier {
			t.Fatalf("%s change did not change qualifier", name)
		}
	}
}

func TestCanonicalFirewallApplyNilAndEmptySetsMatch(t *testing.T) {
	nilSets, err := CanonicalFirewallApply(true, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	emptySets, err := CanonicalFirewallApply(true, false, []int{}, []int{})
	if err != nil {
		t.Fatal(err)
	}
	if nilSets.Qualifier != emptySets.Qualifier || nilSets.TCPPorts != nil || nilSets.UDPPorts != nil {
		t.Fatalf("nil/empty mismatch: %#v / %#v", nilSets, emptySets)
	}
}

func TestCanonicalFirewallApplyRejectsUnsafePortsAndHiddenDisabledPayload(t *testing.T) {
	tooMany := make([]int, firewallApplyMaxPorts+1)
	for index := range tooMany {
		tooMany[index] = index + 1
	}
	for name, input := range map[string]struct {
		enabled bool
		tcp     []int
		udp     []int
	}{
		"zero TCP":      {enabled: true, tcp: []int{0}},
		"negative UDP":  {enabled: true, udp: []int{-1}},
		"oversized TCP": {enabled: true, tcp: []int{65536}},
		"too many":      {enabled: true, tcp: tooMany},
		"disabled TCP":  {enabled: false, tcp: []int{443}},
		"disabled UDP":  {enabled: false, udp: []int{53}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CanonicalFirewallApply(input.enabled, false, input.tcp, input.udp); err == nil {
				t.Fatal("unsafe firewall payload was accepted")
			}
		})
	}
}

func TestValidFirewallApplyQualifierRejectsNonCanonicalValues(t *testing.T) {
	valid := firewallApplyQualifierPrefix + strings.Repeat("a", 64)
	if !ValidFirewallApplyQualifier(valid) {
		t.Fatal("canonical qualifier was rejected")
	}
	for _, invalid := range []string{
		"", firewallApplyQualifierPrefix,
		firewallApplyQualifierPrefix + strings.Repeat("a", 63),
		firewallApplyQualifierPrefix + strings.Repeat("A", 64),
		"firewall-apply/v2:sha256:" + strings.Repeat("a", 64),
	} {
		if ValidFirewallApplyQualifier(invalid) {
			t.Fatalf("invalid qualifier accepted: %q", invalid)
		}
	}
}
