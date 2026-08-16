package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsTypeSOA        = 6
	dnsClassIN        = 1
	dnsResponseAA     = 1 << 10
	dnsResponseTC     = 1 << 9
	dnsResponseQR     = 1 << 15
	dnsResponseRCode  = 0x000f
	dnsRCodeNoError   = 0
	dnsRCodeNameError = 3
	dnsRCodeRefused   = 5
	dnsProbeTimeout   = 4 * time.Second
)

type expectedDNSZoneAuthority struct {
	Domain string
	Delete bool
	Serial uint32
}

type dnsSOAProbeResult struct {
	Authoritative bool
	RCode         int
	SOASerials    []uint32
}

var probeDNSZoneSOA = queryDNSZoneSOA

func expectedDNSZoneAuthorities(
	zones []transport.DNSEngineSwitchZoneSnapshot,
) ([]expectedDNSZoneAuthority, error) {
	expected := make([]expectedDNSZoneAuthority, len(zones))
	for index, zone := range zones {
		expected[index] = expectedDNSZoneAuthority{Domain: zone.Domain, Delete: zone.Delete}
		if zone.Delete {
			continue
		}
		soaCount := 0
		for _, record := range zone.Records {
			if record.Disabled || strings.ToUpper(strings.TrimSpace(record.Type)) != "SOA" {
				continue
			}
			owner := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Name), "."))
			if owner != zone.Domain {
				return nil, fmt.Errorf("zone %s has an enabled SOA outside its apex", zone.Domain)
			}
			fields := strings.Fields(record.Content)
			if len(fields) != 7 {
				return nil, fmt.Errorf("zone %s has an invalid SOA field count", zone.Domain)
			}
			serial, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("zone %s has an invalid SOA serial", zone.Domain)
			}
			soaCount++
			expected[index].Serial = uint32(serial)
		}
		if soaCount != 1 {
			return nil, fmt.Errorf("zone %s must have exactly one enabled apex SOA", zone.Domain)
		}
	}
	return expected, nil
}

func verifyDNSZoneManifestAuthority(
	ctx context.Context,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) error {
	expected, err := expectedDNSZoneAuthorities(zones)
	if err != nil {
		return err
	}
	return verifyDNSZoneAuthorities(ctx, expected)
}

func expectedDNSZoneAuthorityFromBINDTree(
	receipt binddns.ZoneReceipt,
	data []byte,
) (expectedDNSZoneAuthority, error) {
	expected := expectedDNSZoneAuthority{Domain: receipt.Domain, Delete: receipt.Delete}
	if receipt.Delete {
		if data != nil {
			return expectedDNSZoneAuthority{}, errors.New("BIND deletion tombstone unexpectedly has zone bytes")
		}
		return expected, nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 ||
		lines[0] != "; Managed by CelikPanel. DO NOT EDIT." ||
		lines[1] != "$ORIGIN "+receipt.Domain+"." ||
		lines[len(lines)-1] != "" {
		return expectedDNSZoneAuthority{}, errors.New("verified BIND zone has an unsupported master-file shape")
	}
	soaCount := 0
	for _, line := range lines[2 : len(lines)-1] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return expectedDNSZoneAuthority{}, errors.New("verified BIND zone contains a malformed record")
		}
		if fields[3] != "SOA" {
			continue
		}
		if len(fields) != 11 || fields[0] != receipt.Domain+"." ||
			fields[2] != "IN" {
			return expectedDNSZoneAuthority{}, errors.New("verified BIND zone contains an invalid apex SOA")
		}
		serial, err := strconv.ParseUint(fields[6], 10, 32)
		if err != nil {
			return expectedDNSZoneAuthority{}, errors.New("verified BIND zone contains an invalid SOA serial")
		}
		soaCount++
		expected.Serial = uint32(serial)
	}
	if soaCount != 1 {
		return expectedDNSZoneAuthority{}, errors.New("verified BIND zone must contain exactly one apex SOA")
	}
	return expected, nil
}

func verifyDNSZoneAuthorities(
	ctx context.Context,
	expected []expectedDNSZoneAuthority,
) error {
	addresses := strings.Split(publicListenAddresses(), ",")
	if len(addresses) == 0 || strings.TrimSpace(addresses[0]) == "" {
		return errors.New("no public address is available for authoritative DNS verification")
	}
	address := strings.TrimSpace(addresses[0])
	if ip := net.ParseIP(address); ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return errors.New("authoritative DNS verification selected an unsafe address")
	}
	for _, zone := range expected {
		for _, network := range []string{"udp", "tcp"} {
			probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
			result, err := probeDNSZoneSOA(probeCtx, network, address, zone.Domain)
			cancel()
			if err != nil {
				return fmt.Errorf("verify %s SOA for %s: %w", network, zone.Domain, err)
			}
			if zone.Delete {
				if result.Authoritative || len(result.SOASerials) != 0 ||
					(result.RCode != dnsRCodeNameError && result.RCode != dnsRCodeRefused) {
					return fmt.Errorf("deleted zone %s remains authoritative over %s", zone.Domain, network)
				}
				continue
			}
			if !result.Authoritative || result.RCode != dnsRCodeNoError ||
				len(result.SOASerials) != 1 || result.SOASerials[0] != zone.Serial {
				return fmt.Errorf("zone %s did not serve its exact SOA serial over %s", zone.Domain, network)
			}
		}
	}
	return nil
}

func queryDNSZoneSOA(
	ctx context.Context,
	network, address, domain string,
) (dnsSOAProbeResult, error) {
	query, id, err := buildDNSZoneSOAQuery(domain)
	if err != nil {
		return dnsSOAProbeResult{}, err
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(address, "53"))
	if err != nil {
		return dnsSOAProbeResult{}, err
	}
	defer connection.Close()
	deadline := time.Now().Add(dnsProbeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return dnsSOAProbeResult{}, err
	}
	var response []byte
	switch network {
	case "udp":
		if _, err := connection.Write(query); err != nil {
			return dnsSOAProbeResult{}, err
		}
		buffer := make([]byte, 65535)
		read, err := connection.Read(buffer)
		if err != nil {
			return dnsSOAProbeResult{}, err
		}
		response = buffer[:read]
	case "tcp":
		if len(query) > 65535 {
			return dnsSOAProbeResult{}, errors.New("DNS query exceeds the TCP frame limit")
		}
		frame := make([]byte, len(query)+2)
		binary.BigEndian.PutUint16(frame, uint16(len(query)))
		copy(frame[2:], query)
		if _, err := connection.Write(frame); err != nil {
			return dnsSOAProbeResult{}, err
		}
		var length [2]byte
		if _, err := io.ReadFull(connection, length[:]); err != nil {
			return dnsSOAProbeResult{}, err
		}
		size := int(binary.BigEndian.Uint16(length[:]))
		if size < 12 {
			return dnsSOAProbeResult{}, errors.New("DNS TCP response is truncated")
		}
		response = make([]byte, size)
		if _, err := io.ReadFull(connection, response); err != nil {
			return dnsSOAProbeResult{}, err
		}
	default:
		return dnsSOAProbeResult{}, errors.New("unsupported DNS probe network")
	}
	return parseDNSZoneSOAResponse(response, id, domain)
}

func buildDNSZoneSOAQuery(domain string) ([]byte, uint16, error) {
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
	message = append(message, 0, dnsTypeSOA, 0, dnsClassIN)
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

func parseDNSZoneSOAResponse(message []byte, id uint16, domain string) (dnsSOAProbeResult, error) {
	if len(message) < 12 || binary.BigEndian.Uint16(message[0:2]) != id {
		return dnsSOAProbeResult{}, errors.New("DNS response identity mismatch")
	}
	flags := binary.BigEndian.Uint16(message[2:4])
	if flags&dnsResponseQR == 0 || flags&dnsResponseTC != 0 {
		return dnsSOAProbeResult{}, errors.New("DNS response is not a complete answer")
	}
	offset := 12
	questions := int(binary.BigEndian.Uint16(message[4:6]))
	answers := int(binary.BigEndian.Uint16(message[6:8]))
	for range questions {
		_, next, err := decodeDNSName(message, offset)
		if err != nil || next+4 > len(message) {
			return dnsSOAProbeResult{}, errors.New("DNS response has an invalid question")
		}
		offset = next + 4
	}
	result := dnsSOAProbeResult{
		Authoritative: flags&dnsResponseAA != 0,
		RCode:         int(flags & dnsResponseRCode),
	}
	for range answers {
		name, next, err := decodeDNSName(message, offset)
		if err != nil || next+10 > len(message) {
			return dnsSOAProbeResult{}, errors.New("DNS response has an invalid answer")
		}
		recordType := binary.BigEndian.Uint16(message[next : next+2])
		recordClass := binary.BigEndian.Uint16(message[next+2 : next+4])
		rdataLength := int(binary.BigEndian.Uint16(message[next+8 : next+10]))
		rdataOffset := next + 10
		end := rdataOffset + rdataLength
		if end < rdataOffset || end > len(message) {
			return dnsSOAProbeResult{}, errors.New("DNS response answer exceeds its packet")
		}
		if recordType == dnsTypeSOA && recordClass == dnsClassIN &&
			strings.EqualFold(strings.TrimSuffix(name, "."), domain) {
			_, serialOffset, err := decodeDNSName(message, rdataOffset)
			if err != nil {
				return dnsSOAProbeResult{}, errors.New("DNS SOA primary name is invalid")
			}
			_, serialOffset, err = decodeDNSName(message, serialOffset)
			if err != nil || serialOffset+20 != end {
				return dnsSOAProbeResult{}, errors.New("DNS SOA payload is invalid")
			}
			result.SOASerials = append(result.SOASerials, binary.BigEndian.Uint32(message[serialOffset:serialOffset+4]))
		}
		offset = end
	}
	return result, nil
}

func decodeDNSName(message []byte, offset int) (string, int, error) {
	if offset < 0 || offset >= len(message) {
		return "", 0, errors.New("DNS name offset is outside the packet")
	}
	labels := make([]string, 0, 8)
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
			labels = append(labels, string(message[offset:offset+length]))
			offset += length
		default:
			return "", 0, errors.New("DNS name uses an unsupported label encoding")
		}
	}
	return "", 0, errors.New("DNS name exceeds the compression step limit")
}
