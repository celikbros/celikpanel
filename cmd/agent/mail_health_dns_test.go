package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

func mailDNSFixture(t *testing.T, reply func(string, uint16) []byte, modify ...func([]byte) []byte) (string, *sync.Map) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	seen := &sync.Map{}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				query := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				name, next, err := decodeDNSName(query, 12)
				if err != nil || next+4 != len(query) {
					return
				}
				qtype := binary.BigEndian.Uint16(query[next : next+2])
				name = strings.ToLower(strings.TrimSuffix(name, "."))
				seen.Store(name, qtype)
				response := append([]byte(nil), query...)
				binary.BigEndian.PutUint16(response[2:4], 0x8180)
				data := reply(name, qtype)
				if len(data) != 0 {
					binary.BigEndian.PutUint16(response[6:8], 1)
					response = append(response, 0xc0, 0x0c, byte(qtype>>8), byte(qtype), 0, 1, 0, 0, 0, 60, byte(len(data)>>8), byte(len(data)))
					response = append(response, data...)
				}
				if len(modify) > 0 {
					response = modify[0](response)
				}
				binary.BigEndian.PutUint16(size[:], uint16(len(response)))
				_, _ = conn.Write(append(size[:], response...))
			}()
		}
	}()
	return listener.Addr().String(), seen
}

func TestMailDNSAddressUsesWireAnswerInsteadOfLocalhostHostsEntry(t *testing.T) {
	// localhost is the real OS/NSS loopback entry. A custom Go Resolver alone
	// can still return it without contacting DNS; this proof must use the wire.
	addresses, err := net.LookupHost("localhost")
	if err != nil || len(addresses) == 0 {
		t.Fatal("OS localhost prerequisite unavailable")
	}
	for _, value := range addresses {
		if !net.ParseIP(value).IsLoopback() {
			t.Fatalf("unexpected localhost: %v", addresses)
		}
	}
	server, seen := mailDNSFixture(t, func(name string, kind uint16) []byte { return net.ParseIP("192.0.2.15").To4() })
	values, err := queryMailDNS(context.Background(), server, "localhost", 1)
	if err != nil || len(values) != 1 || values[0] != "192.0.2.15" {
		t.Fatalf("public wire proof replaced by hosts: %v %v", values, err)
	}
	if _, ok := seen.Load("localhost"); !ok {
		t.Fatal("no actual DNS request")
	}
}

func TestMailDNSIdentityRequiresExactPublicForwardReversePair(t *testing.T) {
	for _, scenario := range []string{"matching", "mixed-case", "wrong-ptr", "wrong-address", "no-ptr", "ipv6"} {
		t.Run(scenario, func(t *testing.T) {
			source := "192.0.2.15"
			if scenario == "ipv6" {
				source = "2001:db8::15"
			}
			server, _ := mailDNSFixture(t, func(name string, kind uint16) []byte {
				if kind == dnsTypePTR {
					if scenario == "no-ptr" {
						return nil
					}
					target := "mail.example.test"
					if scenario == "mixed-case" {
						target = "MaIl.Example.TEST"
					}
					if scenario == "wrong-ptr" {
						target = "other.example.test"
					}
					data, _ := encodeDNSName(target)
					return data
				}
				if scenario == "wrong-address" {
					return net.ParseIP("192.0.2.16").To4()
				}
				if kind == 28 {
					return net.ParseIP(source).To16()
				}
				return net.ParseIP(source).To4()
			})
			ptr, aligned, forward, err := mailDNSIdentityAt(context.Background(), source, "mail.example.test", []string{server})
			if err != nil {
				t.Fatal(err)
			}
			wantReady := scenario == "matching" || scenario == "mixed-case" || scenario == "ipv6"
			if forward != wantReady || (wantReady && (!aligned || ptr != "mail.example.test")) {
				t.Fatalf("incorrect DNS evidence: ptr=%q aligned=%v forward=%v", ptr, aligned, forward)
			}
			if scenario == "wrong-address" && !aligned {
				t.Fatal("lost independent PTR alignment fact")
			}
		})
	}
}

func TestMailDNSResponseRejectsWrongQuestionAndMalformedAddress(t *testing.T) {
	query, id, err := buildDNSZoneSOAQuery("mail.example.test")
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint16(query[len(query)-4:len(query)-2], 1)
	binary.BigEndian.PutUint16(query[2:4], 0x8180)
	if _, err = parseMailDNSResponse(query, id, "other.example.test", 1); err == nil {
		t.Fatal("accepted different question")
	}
	if _, err = parseMailDNSResponse(query, id+1, "mail.example.test", 1); err == nil {
		t.Fatal("accepted different transaction")
	}
	binary.BigEndian.PutUint16(query[6:8], 1)
	query = append(query, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 3, 192, 0, 2)
	if _, err = parseMailDNSResponse(query, id, "mail.example.test", 1); err == nil {
		t.Fatal("accepted truncated address")
	}
}

func TestMailDNSIdentityFollowsBoundedClasslessReverseAnswerChain(t *testing.T) {
	for _, scenario := range []string{"classless", "outside-reverse-zone", "unrelated-owner", "cycle", "conflicting-target"} {
		t.Run(scenario, func(t *testing.T) {
			reverse := "15.2.0.192.in-addr.arpa"
			target := "15.0/25.2.0.192.in-addr.arpa"
			if scenario == "outside-reverse-zone" {
				target = "reverse.example.test"
			}
			server, _ := mailDNSFixture(t, func(name string, kind uint16) []byte {
				if kind == dnsTypePTR {
					v, _ := encodeDNSName("mail.example.test")
					return v
				}
				return net.ParseIP("192.0.2.15").To4()
			}, func(response []byte) []byte {
				_, next, _ := decodeDNSName(response, 12)
				if binary.BigEndian.Uint16(response[next:next+2]) != dnsTypePTR {
					return response
				}
				response = response[:next+4]
				count := 0
				add := func(owner string, kind uint16, value string) {
					name, _ := encodeDNSName(owner)
					data, _ := encodeDNSName(value)
					response = append(response, name...)
					response = append(response, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 60, byte(len(data)>>8), byte(len(data)))
					response = append(response, data...)
					count++
				}
				add(reverse, 5, target)
				if scenario == "cycle" {
					add(target, 5, reverse)
				} else {
					owner := target
					if scenario == "unrelated-owner" {
						owner = "unrelated.example.test"
					}
					add(owner, dnsTypePTR, "MaIl.Example.TEST")
				}
				if scenario == "conflicting-target" {
					add(reverse, 5, "conflict.example.test")
				}
				binary.BigEndian.PutUint16(response[6:8], uint16(count))
				return response
			})
			ptr, aligned, forward, err := mailDNSIdentityAt(context.Background(), "192.0.2.15", "mail.example.test", []string{server})
			if scenario == "cycle" || scenario == "conflicting-target" {
				if err == nil {
					t.Fatal("ambiguous reverse chain accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unrelated-owner" {
				if aligned || forward || ptr != "" {
					t.Fatal("unrelated PTR satisfied queried reverse identity")
				}
				return
			}
			if !aligned || !forward || ptr != "mail.example.test" {
				t.Fatalf("classless reverse proof lost: %q %v %v", ptr, aligned, forward)
			}
		})
	}
}
