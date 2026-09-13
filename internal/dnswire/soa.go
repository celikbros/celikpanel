package dnswire

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// QueryAuthoritativeSOA asks the literal DNS/TCP endpoint for the exact zone's
// SOA without recursion. It does not prove AXFR permission or replication.
func QueryAuthoritativeSOA(ctx context.Context, endpoint, zone string) (uint32, error) {
	host, port, err := net.SplitHostPort(endpoint)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || net.ParseIP(host) == nil || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return 0, errors.New("DNS endpoint must be a literal IP and port")
	}
	if !canonicalHostname(zone) {
		return 0, errors.New("DNS zone must be canonical")
	}
	query, id, err := buildQuery(zone) // SOA with recursion disabled.
	if err != nil {
		return 0, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", endpoint)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	stop := context.AfterFunc(probeCtx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := probeCtx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, err
	}
	frame := make([]byte, len(query)+2)
	binary.BigEndian.PutUint16(frame, uint16(len(query)))
	copy(frame[2:], query)
	if _, err := io.Copy(conn, bytes.NewReader(frame)); err != nil {
		return 0, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return 0, err
	}
	length := int(binary.BigEndian.Uint16(size[:]))
	if length < 12 || length > 16384 {
		return 0, errors.New("DNS SOA response exceeds its bounded frame")
	}
	message := make([]byte, length)
	if _, err := io.ReadFull(conn, message); err != nil {
		return 0, err
	}
	return parseAuthoritativeSOA(message, id, zone)
}

func parseAuthoritativeSOA(message []byte, id uint16, zone string) (uint32, error) {
	if len(message) < 12 || binary.BigEndian.Uint16(message[:2]) != id {
		return 0, errors.New("DNS SOA response identity differs")
	}
	flags := binary.BigEndian.Uint16(message[2:4])
	if flags&0x8400 != 0x8400 || flags&0x7a0f != 0 || binary.BigEndian.Uint16(message[4:6]) != 1 {
		return 0, errors.New("DNS zone has no complete authoritative SOA answer")
	}
	question, next, err := decodeDNSName(message, 12)
	if err != nil || next+4 > len(message) || strings.ToLower(strings.TrimSuffix(question, ".")) != zone || binary.BigEndian.Uint16(message[next:next+2]) != 6 || binary.BigEndian.Uint16(message[next+2:next+4]) != classIN {
		return 0, errors.New("DNS SOA response question differs")
	}
	answers := int(binary.BigEndian.Uint16(message[6:8]))
	total := answers + int(binary.BigEndian.Uint16(message[8:10])) + int(binary.BigEndian.Uint16(message[10:12]))
	if total > 256 {
		return 0, errors.New("DNS SOA response has too many records")
	}
	offset := next + 4
	var serial uint32
	found := false
	for index := 0; index < total; index++ {
		owner, header, err := decodeDNSName(message, offset)
		if err != nil || header+10 > len(message) {
			return 0, errors.New("DNS SOA response record is malformed")
		}
		kind := binary.BigEndian.Uint16(message[header : header+2])
		class := binary.BigEndian.Uint16(message[header+2 : header+4])
		start := header + 10
		end := start + int(binary.BigEndian.Uint16(message[header+8:header+10]))
		if end > len(message) {
			return 0, errors.New("DNS SOA response record is truncated")
		}
		if index < answers {
			// No aliases, referrals or unrelated answers can satisfy this probe.
			if found || kind != 6 || class != classIN || strings.ToLower(strings.TrimSuffix(owner, ".")) != zone {
				return 0, errors.New("DNS SOA answer does not identify the exact zone")
			}
			_, afterMNAME, nameErr := decodeDNSName(message, start)
			if nameErr != nil || afterMNAME >= end {
				return 0, errors.New("DNS SOA primary name is malformed")
			}
			_, afterRNAME, nameErr := decodeDNSName(message, afterMNAME)
			if nameErr != nil || afterRNAME+20 != end {
				return 0, errors.New("DNS SOA data is malformed")
			}
			serial = binary.BigEndian.Uint32(message[afterRNAME : afterRNAME+4])
			found = true
		}
		offset = end
	}
	if !found || offset != len(message) {
		return 0, errors.New("DNS SOA answer is missing or contains trailing data")
	}
	return serial, nil
}
