// Package dnswire provides bounded, resolver-specific DNS identity proofs.
// These queries deliberately bypass hosts files and NSS.
package dnswire

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/hostname"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	TypeA    uint16 = 1
	TypeAAAA uint16 = 28
	TypePTR  uint16 = 12
	classIN  uint16 = 1
)

func canonicalHostname(value string) bool {
	canonical, err := hostname.CanonicalFQDN(value)
	return err == nil && canonical == value
}

// Query contacts exactly the supplied literal resolver over bounded DNS/TCP.
// It never uses hosts/NSS, search domains or an operating-system resolver.
func Query(ctx context.Context, resolver, name string, qtype uint16) ([]string, error) {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	host, port, err := net.SplitHostPort(resolver)
	number, portErr := strconv.Atoi(port)
	if err != nil || net.ParseIP(host) == nil || portErr != nil || number < 1 || number > 65535 {
		return nil, errors.New("DNS resolver must be a literal IP and port")
	}

	if qtype != 1 && qtype != 28 && qtype != TypePTR {
		return nil, errors.New("unsupported DNS question")
	}
	query, id, err := buildQuery(name)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(query[2:4], 0x0100) // recursion desired
	binary.BigEndian.PutUint16(query[len(query)-4:len(query)-2], qtype)
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probeCtx, "tcp", resolver)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := probeCtx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	frame := make([]byte, len(query)+2)
	binary.BigEndian.PutUint16(frame, uint16(len(query)))
	copy(frame[2:], query)
	if _, err := conn.Write(frame); err != nil {
		return nil, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(size[:]))
	if length < 12 || length > 16384 {
		return nil, errors.New("DNS response exceeds its bounded frame")
	}
	message := make([]byte, length)
	if _, err := io.ReadFull(conn, message); err != nil {
		return nil, err
	}
	return ParseResponse(message, id, name, qtype)
}

func ParseResponse(message []byte, id uint16, name string, qtype uint16) ([]string, error) {
	if len(message) < 12 || binary.BigEndian.Uint16(message[:2]) != id {
		return nil, errors.New("DNS response identity differs")
	}
	flags := binary.BigEndian.Uint16(message[2:4])
	if flags&0x8000 == 0 || flags&0x7a00 != 0 || binary.BigEndian.Uint16(message[4:6]) != 1 {
		return nil, errors.New("DNS response is not a complete successful answer")
	}
	question, next, err := decodeDNSName(message, 12)
	if err != nil || next+4 > len(message) || strings.ToLower(strings.TrimSuffix(question, ".")) != name || binary.BigEndian.Uint16(message[next:next+2]) != qtype || binary.BigEndian.Uint16(message[next+2:next+4]) != classIN {
		return nil, errors.New("DNS response question differs")
	}
	switch flags & 15 {
	case 0:
	case 3:
		return nil, &net.DNSError{Err: "DNS name does not exist", Name: name, IsNotFound: true}
	default:
		return nil, fmt.Errorf("DNS resolver returned response code %d", flags&15)
	}
	offset := next + 4
	answers := int(binary.BigEndian.Uint16(message[6:8]))
	total := answers + int(binary.BigEndian.Uint16(message[8:10])) + int(binary.BigEndian.Uint16(message[10:12]))
	if total > 256 {
		return nil, errors.New("DNS response has too many records")
	}
	valuesByOwner := map[string][]string{}
	aliases := map[string]string{}
	for index := 0; index < total; index++ {
		owner, header, err := decodeDNSName(message, offset)
		if err != nil || header+10 > len(message) {
			return nil, errors.New("DNS response record is malformed")
		}
		typeID := binary.BigEndian.Uint16(message[header : header+2])
		class := binary.BigEndian.Uint16(message[header+2 : header+4])
		start := header + 10
		end := start + int(binary.BigEndian.Uint16(message[header+8:header+10]))
		if end > len(message) {
			return nil, errors.New("DNS response record is truncated")
		}
		owner = strings.ToLower(strings.TrimSuffix(owner, "."))
		if index < answers && typeID == 5 && class == classIN {
			target, after, err := decodeDNSName(message, start)
			target = strings.ToLower(strings.TrimSuffix(target, "."))
			if err != nil || after != end || target == "" {
				return nil, errors.New("DNS alias is malformed")
			}
			if previous, exists := aliases[owner]; exists && previous != target {
				return nil, errors.New("DNS alias has conflicting targets")
			}
			aliases[owner] = target
		}
		if index < answers && typeID == qtype && class == classIN {
			switch qtype {
			case 1, 28:
				if (qtype == 1 && end-start != 4) || (qtype == 28 && end-start != 16) {
					return nil, errors.New("DNS address is malformed")
				}
				valuesByOwner[owner] = append(valuesByOwner[owner], net.IP(message[start:end]).String())
			case TypePTR:
				value, after, err := decodeDNSName(message, start)
				value = strings.ToLower(strings.TrimSuffix(value, "."))
				if err != nil || after != end || !canonicalHostname(value) {
					return nil, errors.New("DNS reverse name is malformed")
				}
				valuesByOwner[owner] = append(valuesByOwner[owner], value)
			}
		}
		offset = end
	}
	if offset != len(message) {
		return nil, errors.New("DNS response has trailing data")
	}
	// Recursive public resolvers return the entire answer chain for ordinary
	// aliases and RFC 2317 classless reverse delegation. Only data at its
	// bounded, unambiguous terminal owner can satisfy the original question.
	seen := map[string]bool{}
	owner := name
	for depth := 0; depth < 16; depth++ {
		if seen[owner] {
			return nil, errors.New("DNS alias chain contains a cycle")
		}
		seen[owner] = true
		target, alias := aliases[owner]
		if !alias {
			return valuesByOwner[owner], nil
		}
		if len(valuesByOwner[owner]) != 0 {
			return nil, errors.New("DNS owner contains alias and terminal data")
		}
		owner = target
	}
	return nil, errors.New("DNS alias chain exceeds its limit")
}

func buildQuery(domain string) ([]byte, uint16, error) {
	encodedName, err := encodeDNSName(domain)
	if err != nil {
		return nil, 0, err
	}
	var rawID [2]byte
	if _, err := rand.Read(rawID[:]); err != nil {
		return nil, 0, err
	}
	id := binary.BigEndian.Uint16(rawID[:])
	message := make([]byte, 12, 12+len(encodedName)+4)
	binary.BigEndian.PutUint16(message[0:2], id)
	binary.BigEndian.PutUint16(message[4:6], 1)
	message = append(message, encodedName...)
	message = append(message, 0, 6, 0, 1)
	return message, id, nil
}

func encodeDNSName(domain string) ([]byte, error) {
	if domain == "" || strings.HasSuffix(domain, ".") {
		return nil, errors.New("DNS query name must be canonical")
	}
	encoded := make([]byte, 0, len(domain)+2)
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, errors.New("DNS query name has an invalid label")
		}
		encoded = append(encoded, byte(len(label)))
		encoded = append(encoded, label...)
	}
	encoded = append(encoded, 0)
	if len(encoded) > 255 {
		return nil, errors.New("DNS query name is too long")
	}
	return encoded, nil
}

func decodeDNSName(message []byte, offset int) (string, int, error) {
	if offset < 0 || offset >= len(message) {
		return "", 0, errors.New("DNS name offset is outside the packet")
	}
	labels := make([]string, 0, 8)
	wireLength := 1
	next := -1
	visited := map[int]bool{}
	for steps := 0; steps < 128; steps++ {
		if offset >= len(message) || visited[offset] {
			return "", 0, errors.New("DNS name compression is cyclic or truncated")
		}
		visited[offset] = true
		length := int(message[offset])
		switch length & 0xc0 {
		case 0xc0:
			if offset+1 >= len(message) {
				return "", 0, errors.New("DNS name pointer is truncated")
			}
			if next < 0 {
				next = offset + 2
			}
			offset = ((length & 0x3f) << 8) | int(message[offset+1])
			continue
		case 0:
			offset++
			if length == 0 {
				if next < 0 {
					next = offset
				}
				return strings.Join(labels, ".") + ".", next, nil
			}
			if length > 63 || offset+length > len(message) {
				return "", 0, errors.New("DNS name label is invalid")
			}
			wireLength += length + 1
			if wireLength > 255 {
				return "", 0, errors.New("DNS name exceeds the wire length limit")
			}
			for _, value := range message[offset : offset+length] {
				if value < 0x21 || value > 0x7e || value == '.' || value == '\\' {
					return "", 0, errors.New("DNS label cannot be represented unambiguously")
				}
			}
			labels = append(labels, string(message[offset:offset+length]))
			offset += length
		default:
			return "", 0, errors.New("DNS name uses an unsupported label encoding")
		}
	}
	return "", 0, errors.New("DNS name exceeds the compression step limit")
}
