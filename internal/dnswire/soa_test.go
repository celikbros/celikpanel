package dnswire

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

const soaTestZone = "catalog-c000020a.celikpanel.invalid"

func soaTestResponse(t *testing.T) ([]byte, uint16, int) {
	t.Helper()
	message, id, err := buildQuery(soaTestZone)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint16(message[2:4], 0x8400)
	binary.BigEndian.PutUint16(message[6:8], 1)
	answerStart := len(message)
	message = append(message, 0xc0, 12, 0, 6, 0, 1, 0, 0, 0, 60, 0, 22)
	message = append(message, 0, 0)
	message = append(message, 0, 0, 0, 7, 0, 0, 0, 60, 0, 0, 0, 30, 0, 0, 14, 16, 0, 0, 0, 30)
	return message, id, answerStart
}

func TestAuthoritativeSOARejectsUnrelatedAndMalformedEvidence(t *testing.T) {
	for _, scenario := range []string{"exact", "wrong-id", "not-authoritative", "truncated", "wrong-opcode", "refused", "nxdomain", "servfail", "wrong-question", "alias", "wrong-class", "unrelated-owner", "authority-only", "no-answer", "extra-answer", "malformed-soa", "compression-cycle", "trailing-data", "too-many-records", "short-packet"} {
		t.Run(scenario, func(t *testing.T) {
			message, id, answer := soaTestResponse(t)
			switch scenario {
			case "wrong-id":
				id++
			case "not-authoritative":
				binary.BigEndian.PutUint16(message[2:4], 0x8000)
			case "truncated":
				binary.BigEndian.PutUint16(message[2:4], 0x8600)
			case "wrong-opcode":
				binary.BigEndian.PutUint16(message[2:4], 0x8c00)
			case "refused":
				binary.BigEndian.PutUint16(message[2:4], 0x8405)
			case "nxdomain":
				binary.BigEndian.PutUint16(message[2:4], 0x8403)
			case "servfail":
				binary.BigEndian.PutUint16(message[2:4], 0x8402)
			case "wrong-question":
				message[13] = 'x'
			case "alias":
				binary.BigEndian.PutUint16(message[answer+2:answer+4], 5)
			case "wrong-class":
				binary.BigEndian.PutUint16(message[answer+4:answer+6], 3)
			case "unrelated-owner":
				message[answer+1] = byte(answer - 5)
			case "authority-only":
				binary.BigEndian.PutUint16(message[6:8], 0)
				binary.BigEndian.PutUint16(message[8:10], 1)
			case "no-answer":
				binary.BigEndian.PutUint16(message[6:8], 0)
				message = message[:answer]
			case "extra-answer":
				binary.BigEndian.PutUint16(message[6:8], 2)
				message = append(message, message[answer:]...)
			case "malformed-soa":
				message = message[:len(message)-1]
			case "compression-cycle":
				message[answer+1] = byte(answer)
			case "trailing-data":
				message = append(message, 0)
			case "too-many-records":
				binary.BigEndian.PutUint16(message[8:10], 256)
			case "short-packet":
				message = message[:11]
			}
			serial, err := parseAuthoritativeSOA(message, id, soaTestZone)
			if scenario == "exact" {
				if err != nil || serial != 7 {
					t.Fatalf("exact SOA rejected: serial=%d err=%v", serial, err)
				}
			} else if err == nil {
				t.Fatalf("invalid SOA accepted: serial=%d", serial)
			}
		})
	}
}

func TestAuthoritativeSOAQueriesExactLiteralEndpointWithoutRecursion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	querySeen := make(chan []byte, 1)
	response, _, _ := soaTestResponse(t)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return
		}
		query := make([]byte, int(binary.BigEndian.Uint16(size[:])))
		if _, err := io.ReadFull(conn, query); err != nil {
			return
		}
		querySeen <- query
		copy(response[:2], query[:2])
		binary.BigEndian.PutUint16(size[:], uint16(len(response)))
		_, _ = conn.Write(size[:1])
		_, _ = conn.Write(size[1:])
		_, _ = conn.Write(response)
	}()
	serial, err := QueryAuthoritativeSOA(context.Background(), listener.Addr().String(), soaTestZone)
	if err != nil || serial != 7 {
		t.Fatalf("TCP SOA failed: serial=%d err=%v", serial, err)
	}
	select {
	case query := <-querySeen:
		name, after, err := decodeDNSName(query, 12)
		if err != nil || name != soaTestZone+"." || binary.BigEndian.Uint16(query[2:4]) != 0 || binary.BigEndian.Uint16(query[after:after+2]) != 6 {
			t.Fatalf("query did not use nonrecursive exact SOA: %x", query)
		}
	default:
		t.Fatal("literal DNS endpoint did not receive the query")
	}
}

func TestAuthoritativeSOARejectsResolverNamesAndHonorsDeadline(t *testing.T) {
	for _, endpoint := range []string{"localhost:53", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:65536"} {
		if _, err := QueryAuthoritativeSOA(context.Background(), endpoint, soaTestZone); err == nil {
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := QueryAuthoritativeSOA(ctx, listener.Addr().String(), soaTestZone); err == nil {
		t.Fatal("unresponsive DNS endpoint passed")
	}
	if time.Since(started) > time.Second {
		t.Fatal("SOA query ignored its parent deadline")
	}
}
