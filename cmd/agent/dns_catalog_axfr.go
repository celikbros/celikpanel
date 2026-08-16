package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sort"
	"strings"
	"time"
)

const (
	dnsTypePTR                = 12
	dnsTypeAXFR               = 252
	dnsCatalogAXFRMaxBytes    = 16 << 20
	dnsCatalogAXFRMaxMessages = 4096
	dnsCatalogAXFRMaxMembers  = 65536
)

type dnsCatalogAXFRResult struct {
	Serial  uint32
	Members []string
}

type dnsCatalogAXFRProbe func(
	context.Context, string, string,
) (dnsCatalogAXFRResult, error)

var probeDNSCatalogAXFR dnsCatalogAXFRProbe = queryDNSCatalogAXFR

func buildDNSCatalogAXFRQuery(domain string) ([]byte, uint16, error) {
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
	message = append(message, byte(dnsTypeAXFR>>8), byte(dnsTypeAXFR), 0, dnsClassIN)
	return message, id, nil
}

func queryDNSCatalogAXFR(
	ctx context.Context,
	address, catalog string,
) (dnsCatalogAXFRResult, error) {
	query, id, err := buildDNSCatalogAXFRQuery(catalog)
	if err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	connection, err := (&net.Dialer{}).DialContext(
		ctx, "tcp", net.JoinHostPort(address, "53"),
	)
	if err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	defer connection.Close()
	deadline := time.Now().Add(dnsPairProofLimit)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	frame := make([]byte, len(query)+2)
	binary.BigEndian.PutUint16(frame[:2], uint16(len(query)))
	copy(frame[2:], query)
	if _, err := connection.Write(frame); err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	seen := map[string]bool{}
	var soaSerials []uint32
	var totalBytes int
	for messageIndex := 0; messageIndex < dnsCatalogAXFRMaxMessages; messageIndex++ {
		var length [2]byte
		if _, err := io.ReadFull(connection, length[:]); err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		size := int(binary.BigEndian.Uint16(length[:]))
		if size < 12 || totalBytes+size > dnsCatalogAXFRMaxBytes {
			return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR exceeded its safe response bound")
		}
		message := make([]byte, size)
		if _, err := io.ReadFull(connection, message); err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		totalBytes += size
		serials, members, err := parseDNSCatalogAXFRMessage(message, id, catalog)
		if err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		soaSerials = append(soaSerials, serials...)
		for _, member := range members {
			if seen[member] {
				return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR contains a duplicate member")
			}
			seen[member] = true
			if len(seen) > dnsCatalogAXFRMaxMembers {
				return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR exceeds the member limit")
			}
		}
		if len(soaSerials) >= 2 {
			if len(soaSerials) != 2 || soaSerials[0] == 0 || soaSerials[0] != soaSerials[1] {
				return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR has an invalid SOA envelope")
			}
			result := dnsCatalogAXFRResult{Serial: soaSerials[0]}
			for member := range seen {
				result.Members = append(result.Members, member)
			}
			sort.Strings(result.Members)
			return result, nil
		}
	}
	return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR did not terminate")
}

func parseDNSCatalogAXFRMessage(
	message []byte,
	id uint16,
	catalog string,
) ([]uint32, []string, error) {
	if len(message) < 12 || binary.BigEndian.Uint16(message[:2]) != id {
		return nil, nil, errors.New("BIND catalog AXFR response identity mismatch")
	}
	flags := binary.BigEndian.Uint16(message[2:4])
	if flags&dnsResponseQR == 0 || flags&dnsResponseTC != 0 ||
		int(flags&dnsResponseRCode) != dnsRCodeNoError {
		return nil, nil, errors.New("BIND catalog AXFR response was refused or incomplete")
	}
	offset := 12
	questions := int(binary.BigEndian.Uint16(message[4:6]))
	answers := int(binary.BigEndian.Uint16(message[6:8]))
	if questions > 1 || answers == 0 {
		return nil, nil, errors.New("BIND catalog AXFR section counts are invalid")
	}
	for range questions {
		name, next, err := decodeDNSName(message, offset)
		if err != nil || next+4 > len(message) ||
			strings.ToLower(strings.TrimSuffix(name, ".")) != catalog ||
			binary.BigEndian.Uint16(message[next:next+2]) != dnsTypeAXFR ||
			binary.BigEndian.Uint16(message[next+2:next+4]) != dnsClassIN {
			return nil, nil, errors.New("BIND catalog AXFR question is invalid")
		}
		offset = next + 4
	}
	var serials []uint32
	var members []string
	for recordIndex := 0; recordIndex < answers; recordIndex++ {
		ownerRaw, next, err := decodeDNSName(message, offset)
		if err != nil || next+10 > len(message) {
			return nil, nil, errors.New("BIND catalog AXFR record is invalid")
		}
		owner := strings.ToLower(strings.TrimSuffix(ownerRaw, "."))
		recordType := binary.BigEndian.Uint16(message[next : next+2])
		recordClass := binary.BigEndian.Uint16(message[next+2 : next+4])
		rdataLength := int(binary.BigEndian.Uint16(message[next+8 : next+10]))
		rdataOffset := next + 10
		end := rdataOffset + rdataLength
		if end < rdataOffset || end > len(message) || recordClass != dnsClassIN {
			return nil, nil, errors.New("BIND catalog AXFR record exceeds its message")
		}
		switch recordType {
		case dnsTypeSOA:
			if owner != catalog || (recordIndex != 0 && recordIndex != answers-1) {
				return nil, nil, errors.New("BIND catalog AXFR contains a foreign SOA")
			}
			_, serialOffset, err := decodeDNSName(message, rdataOffset)
			if err != nil {
				return nil, nil, errors.New("BIND catalog AXFR SOA primary is invalid")
			}
			_, serialOffset, err = decodeDNSName(message, serialOffset)
			if err != nil || serialOffset+20 != end {
				return nil, nil, errors.New("BIND catalog AXFR SOA payload is invalid")
			}
			serials = append(serials, binary.BigEndian.Uint32(message[serialOffset:serialOffset+4]))
		case dnsTypePTR:
			if !strings.HasSuffix(owner, ".zones."+catalog) {
				return nil, nil, errors.New("BIND catalog AXFR PTR is outside the member namespace")
			}
			memberRaw, memberEnd, err := decodeDNSName(message, rdataOffset)
			member := strings.ToLower(strings.TrimSuffix(memberRaw, "."))
			if err != nil || memberEnd != end || !serviceMutationCanonicalFQDN(member) {
				return nil, nil, errors.New("BIND catalog AXFR member is invalid")
			}
			members = append(members, member)
		}
		offset = end
	}
	for _, count := range []int{
		int(binary.BigEndian.Uint16(message[8:10])),
		int(binary.BigEndian.Uint16(message[10:12])),
	} {
		for range count {
			_, next, err := decodeDNSName(message, offset)
			if err != nil || next+10 > len(message) {
				return nil, nil, errors.New("BIND catalog AXFR auxiliary record is invalid")
			}
			length := int(binary.BigEndian.Uint16(message[next+8 : next+10]))
			offset = next + 10 + length
			if offset > len(message) {
				return nil, nil, errors.New("BIND catalog AXFR auxiliary record exceeds its message")
			}
		}
	}
	if offset != len(message) {
		return nil, nil, errors.New("BIND catalog AXFR response contains trailing bytes")
	}
	return serials, members, nil
}
