package dnswire

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func deletedSOATestReply(t *testing.T, zone string, flags uint16, parent string) ([]byte, uint16) {
	t.Helper()
	query, id, err := buildQuery(zone)
	if err != nil {
		t.Fatal(err)
	}
	message := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(message[2:4], flags)
	if parent != "" {
		parentQuery, _, err := buildQuery(parent)
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint16(message[8:10], 1)
		message = append(message, parentQuery[12:len(parentQuery)-4]...)
		message = append(message, 0, 6, 0, 1, 0, 0, 0, 60, 0, 22)
		message = append(message, 0, 0) // root MNAME and RNAME
		message = append(message, make([]byte, 20)...)
	}
	return message, id
}

func TestDeletedZoneSOARequiresExactNegativeWireEvidence(t *testing.T) {
	const zone = "mail.example.test"
	for _, tc := range []struct {
		name   string
		flags  uint16
		parent string
		change func([]byte)
		valid  bool
	}{
		{name: "non-authoritative REFUSED", flags: 0x8005},
		{name: "non-authoritative NXDOMAIN", flags: 0x8003},
		{name: "authoritative parent NXDOMAIN", flags: 0x8403, parent: "example.test", valid: true},
		{name: "authoritative parent NODATA", flags: 0x8400, parent: "example.test", valid: true},
		{name: "child still authoritative", flags: 0x8403, parent: zone},
		{name: "unrelated authority", flags: 0x8403, parent: "other.test"},
		{name: "authoritative refusal", flags: 0x8405},
		{name: "non-authoritative success", flags: 0x8000},
		{name: "truncated", flags: 0x8205},
		{name: "wrong opcode", flags: 0x8805},
		{name: "wrong question", flags: 0x8005, change: func(b []byte) { b[13] = 'x' }},
		{name: "wrong question type", flags: 0x8005, change: func(b []byte) { b[len(b)-3] = 1 }},
		{name: "answer count", flags: 0x8005, change: func(b []byte) { binary.BigEndian.PutUint16(b[6:8], 1) }},
		{name: "additional count", flags: 0x8005, change: func(b []byte) { binary.BigEndian.PutUint16(b[10:12], 1) }},
		{name: "missing parent", flags: 0x8403},
		{name: "extra parent", flags: 0x8403, parent: "example.test", change: func(b []byte) { binary.BigEndian.PutUint16(b[8:10], 2) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message, id := deletedSOATestReply(t, zone, tc.flags, tc.parent)
			if tc.change != nil {
				tc.change(message)
			}
			err := parseDeletedZoneSOAResponse(message, id, zone)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	message, id := deletedSOATestReply(t, zone, 0x8403, "example.test")
	message = append(message, 0)
	if err := parseDeletedZoneSOAResponse(message, id, zone); err == nil {
		t.Fatal("trailing bytes passed as deleted-zone proof")
	}
	if err := parseDeletedZoneSOAResponse(message[:len(message)-2], id, zone); err == nil {
		t.Fatal("truncated parent SOA passed as deleted-zone proof")
	}
	if err := parseDeletedZoneSOAResponse(message[:len(message)-1], id+1, zone); err == nil {
		t.Fatal("wrong request ID passed as deleted-zone proof")
	}
}

func TestDeletedZoneSOAQueriesLiteralUDPWithoutRecursion(t *testing.T) {
	const zone = "mail.example.test"
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	querySeen := make(chan []byte, 1)
	response, _ := deletedSOATestReply(t, zone, 0x8403, "example.test")
	go func() {
		buffer := make([]byte, 512)
		_ = listener.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, peer, err := listener.ReadFrom(buffer)
		if err != nil || n < 12 {
			return
		}
		query := append([]byte(nil), buffer[:n]...)
		querySeen <- query
		copy(response[:2], query[:2])
		_, _ = listener.WriteTo(response, peer)
	}()
	if err := QueryDeletedZoneSOA(context.Background(), "udp", listener.LocalAddr().String(), zone); err != nil {
		t.Fatalf("exact parent negative answer rejected: %v", err)
	}
	select {
	case query := <-querySeen:
		name, next, err := decodeDNSName(query, 12)
		if err != nil || name != zone+"." || next+4 != len(query) ||
			binary.BigEndian.Uint16(query[2:4]) != 0 ||
			binary.BigEndian.Uint16(query[next:next+2]) != 6 ||
			binary.BigEndian.Uint16(query[next+2:next+4]) != classIN {
			t.Fatalf("negative proof did not send an exact nonrecursive SOA query: %x", query)
		}
	default:
		t.Fatal("literal UDP endpoint did not receive the query")
	}
}
