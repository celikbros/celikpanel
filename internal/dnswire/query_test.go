package dnswire

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
)

func TestAddressAnswerIsBoundToExactCanonicalAliasChain(t *testing.T) {
	for _, scenario := range []string{"alias", "unrelated", "cycle", "conflicting", "alias-and-data"} {
		t.Run(scenario, func(t *testing.T) {
			question, id, err := buildQuery("ns.example.test")
			if err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint16(question[len(question)-4:len(question)-2], TypeA)
			binary.BigEndian.PutUint16(question[2:4], 0x8180)
			count := 0
			add := func(owner string, kind uint16, data []byte) {
				name, _ := encodeDNSName(owner)
				question = append(question, name...)
				question = append(question, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 60, 0, byte(len(data)))
				question = append(question, data...)
				count++
			}
			target, _ := encodeDNSName("NoDe.Example.TEST")
			add("NS.example.test", 5, target)
			owner := "node.example.test"
			if scenario == "unrelated" {
				owner = "foreign.example.test"
			}
			if scenario == "cycle" {
				target, _ := encodeDNSName("ns.example.test")
				add(owner, 5, target)
			} else {
				add(owner, TypeA, net.ParseIP("192.0.2.15").To4())
			}
			if scenario == "conflicting" {
				target, _ := encodeDNSName("other.example.test")
				add("ns.example.test", 5, target)
			}
			if scenario == "alias-and-data" {
				add("ns.example.test", TypeA, net.ParseIP("192.0.2.16").To4())
			}
			binary.BigEndian.PutUint16(question[6:8], uint16(count))
			got, err := ParseResponse(question, id, "ns.example.test", TypeA)
			switch scenario {
			case "alias":
				if err != nil || len(got) != 1 || got[0] != "192.0.2.15" {
					t.Fatalf("canonical alias answer lost: %v %v", got, err)
				}
			case "unrelated":
				if err != nil || len(got) != 0 {
					t.Fatalf("unrelated owner satisfied query: %v %v", got, err)
				}
			default:
				if err == nil {
					t.Fatal("ambiguous alias chain accepted")
				}
			}
		})
	}
}

func TestResolverEndpointCannotUseNSS(t *testing.T) {
	if _, err := Query(context.Background(), "localhost:53", "ns.example.test", TypeA); err == nil {
		t.Fatal("resolver endpoint could consult hosts/NSS")
	}
}
