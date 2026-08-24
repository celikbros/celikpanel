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

	"github.com/alicelik/celikpanel/internal/binddns"
)

const (
	dnsTypeNS                 = 2
	dnsTypePTR                = 12
	dnsTypeTXT                = 16
	dnsTypeAPL                = 42
	dnsTypeAXFR               = 252
	dnsCatalogAXFRTTL         = 60
	dnsCatalogMemberLabelSize = 56
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

type dnsBoundCatalogAXFRProbe func(
	context.Context, string, string, string,
) (dnsCatalogAXFRResult, error)

type dnsZoneAXFRState uint8

const (
	dnsZoneAXFRIndeterminate dnsZoneAXFRState = iota
	dnsZoneAXFRPresent
	dnsZoneAXFRAbsent
)

type dnsBoundZoneAXFRProbe func(
	context.Context, string, string, string,
) (dnsZoneAXFRState, error)

var (
	probeDNSBoundCatalogAXFR dnsBoundCatalogAXFRProbe = queryDNSBoundCatalogAXFR
	probeDNSBoundZoneAXFR    dnsBoundZoneAXFRProbe    = queryDNSBoundZoneAXFR
)

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
	return queryDNSCatalogAXFRFrom(ctx, "", address, catalog)
}

func queryDNSBoundCatalogAXFR(
	ctx context.Context,
	source, address, catalog string,
) (dnsCatalogAXFRResult, error) {
	if !canonicalPairReadinessIPv4(source) ||
		!canonicalPairReadinessIPv4(address) || source == address {
		return dnsCatalogAXFRResult{}, errors.New("peer catalog AXFR identity is invalid")
	}
	return queryDNSCatalogAXFRFrom(ctx, source, address, catalog)
}

func queryDNSCatalogAXFRFrom(
	ctx context.Context,
	source, address, catalog string,
) (dnsCatalogAXFRResult, error) {
	if !serviceMutationCanonicalFQDN(catalog) {
		return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR identity is invalid")
	}
	connection, id, err := dialDNSAXFR(
		ctx, source, address, catalog, dnsPairProofLimit,
	)
	if err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	defer connection.Close()
	return readDNSCatalogAXFR(connection, id, catalog)
}

func readDNSCatalogAXFR(
	reader io.Reader,
	id uint16,
	catalog string,
) (dnsCatalogAXFRResult, error) {
	state, err := newDNSCatalogAXFRState(id, catalog)
	if err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	var totalBytes int
	for messageIndex := 0; messageIndex < dnsCatalogAXFRMaxMessages; messageIndex++ {
		var length [2]byte
		if _, err := io.ReadFull(reader, length[:]); err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		size := int(binary.BigEndian.Uint16(length[:]))
		if size < 12 || totalBytes+size > dnsCatalogAXFRMaxBytes {
			return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR exceeded its safe response bound")
		}
		message := make([]byte, size)
		if _, err := io.ReadFull(reader, message); err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		totalBytes += size
		if err := state.parseMessage(message); err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		if state.closed {
			return state.result()
		}
	}
	return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR did not terminate")
}

func dialDNSAXFR(
	ctx context.Context,
	source, address, domain string,
	timeout time.Duration,
) (net.Conn, uint16, error) {
	query, id, err := buildDNSCatalogAXFRQuery(domain)
	if err != nil {
		return nil, 0, err
	}
	dialer := &net.Dialer{}
	if source != "" {
		parsed := net.ParseIP(source)
		if parsed == nil || parsed.To4() == nil || parsed.To4().String() != source {
			return nil, 0, errors.New("AXFR source address is invalid")
		}
		dialer.LocalAddr = &net.TCPAddr{IP: parsed.To4()}
	}
	connection, err := dialer.DialContext(
		ctx, "tcp", net.JoinHostPort(address, "53"),
	)
	if err != nil {
		return nil, 0, err
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		connection.Close()
		return nil, 0, err
	}
	frame := make([]byte, len(query)+2)
	binary.BigEndian.PutUint16(frame[:2], uint16(len(query)))
	copy(frame[2:], query)
	if _, err := connection.Write(frame); err != nil {
		connection.Close()
		return nil, 0, err
	}
	return connection, id, nil
}

func queryDNSBoundZoneAXFR(
	ctx context.Context,
	source, address, domain string,
) (dnsZoneAXFRState, error) {
	if !canonicalPairReadinessIPv4(source) ||
		!canonicalPairReadinessIPv4(address) || source == address ||
		!serviceMutationCanonicalFQDN(domain) {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR identity is invalid")
	}
	connection, id, err := dialDNSAXFR(
		ctx, source, address, domain, dnsProbeTimeout,
	)
	if err != nil {
		return dnsZoneAXFRIndeterminate, err
	}
	defer connection.Close()
	message, err := readDNSAXFRMessage(connection, 0)
	if err != nil {
		return dnsZoneAXFRIndeterminate, err
	}
	return parseDNSZoneAXFRState(message, id, domain)
}

func readDNSAXFRMessage(connection net.Conn, totalBytes int) ([]byte, error) {
	var length [2]byte
	if _, err := io.ReadFull(connection, length[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(length[:]))
	if size < 12 || totalBytes+size > dnsCatalogAXFRMaxBytes {
		return nil, errors.New("DNS AXFR exceeded its safe response bound")
	}
	message := make([]byte, size)
	if _, err := io.ReadFull(connection, message); err != nil {
		return nil, err
	}
	return message, nil
}

func parseDNSZoneAXFRState(
	message []byte,
	id uint16,
	domain string,
) (dnsZoneAXFRState, error) {
	if len(message) < 12 || binary.BigEndian.Uint16(message[:2]) != id {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR response identity mismatch")
	}
	flags := binary.BigEndian.Uint16(message[2:4])
	if flags&dnsResponseQR == 0 || flags&dnsResponseTC != 0 || flags&0x7800 != 0 {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR response flags are invalid")
	}
	if binary.BigEndian.Uint16(message[4:6]) != 1 {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR question count is invalid")
	}
	offset := 12
	name, next, err := decodeDNSName(message, offset)
	if err != nil || next+4 > len(message) ||
		strings.ToLower(strings.TrimSuffix(name, ".")) != domain ||
		binary.BigEndian.Uint16(message[next:next+2]) != dnsTypeAXFR ||
		binary.BigEndian.Uint16(message[next+2:next+4]) != dnsClassIN {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR question is invalid")
	}
	offset = next + 4
	answers := int(binary.BigEndian.Uint16(message[6:8]))
	rcode := int(flags & dnsResponseRCode)
	if rcode == dnsRCodeNoError {
		if answers == 0 {
			return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR success has no answers")
		}
		ownerRaw, recordHeader, recordErr := decodeDNSName(message, offset)
		owner := strings.ToLower(strings.TrimSuffix(ownerRaw, "."))
		if recordErr != nil || recordHeader+10 > len(message) ||
			owner != domain ||
			binary.BigEndian.Uint16(message[recordHeader:recordHeader+2]) != dnsTypeSOA ||
			binary.BigEndian.Uint16(message[recordHeader+2:recordHeader+4]) != dnsClassIN {
			return dnsZoneAXFRIndeterminate,
				errors.New("peer zone AXFR does not start with its apex SOA")
		}
		rdataOffset := recordHeader + 10
		rdataLength := int(binary.BigEndian.Uint16(
			message[recordHeader+8 : recordHeader+10],
		))
		rdataEnd := rdataOffset + rdataLength
		if rdataEnd < rdataOffset || rdataEnd > len(message) {
			return dnsZoneAXFRIndeterminate,
				errors.New("peer zone AXFR SOA exceeds its message")
		}
		_, serialOffset, recordErr := decodeDNSName(message, rdataOffset)
		if recordErr == nil {
			_, serialOffset, recordErr = decodeDNSName(message, serialOffset)
		}
		if recordErr != nil || serialOffset+20 != rdataEnd ||
			binary.BigEndian.Uint32(message[serialOffset:serialOffset+4]) == 0 {
			return dnsZoneAXFRIndeterminate,
				errors.New("peer zone AXFR SOA payload is invalid")
		}
		return dnsZoneAXFRPresent, nil
	}
	if answers != 0 ||
		(rcode != dnsRCodeRefused && rcode != dnsRCodeNotAuth &&
			rcode != dnsRCodeNameError) {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR absence is not exact")
	}
	for _, count := range []int{
		int(binary.BigEndian.Uint16(message[8:10])),
		int(binary.BigEndian.Uint16(message[10:12])),
	} {
		for range count {
			_, recordEnd, recordErr := skipDNSResourceRecord(message, offset)
			if recordErr != nil {
				return dnsZoneAXFRIndeterminate, recordErr
			}
			offset = recordEnd
		}
	}
	if offset != len(message) {
		return dnsZoneAXFRIndeterminate, errors.New("peer zone AXFR response contains trailing bytes")
	}
	return dnsZoneAXFRAbsent, nil
}

func skipDNSResourceRecord(message []byte, offset int) (string, int, error) {
	owner, next, err := decodeDNSName(message, offset)
	if err != nil || next+10 > len(message) {
		return "", 0, errors.New("DNS AXFR auxiliary record is invalid")
	}
	length := int(binary.BigEndian.Uint16(message[next+8 : next+10]))
	end := next + 10 + length
	if end < next+10 || end > len(message) {
		return "", 0, errors.New("DNS AXFR auxiliary record exceeds its message")
	}
	return owner, end, nil
}

type dnsCatalogAXFRState struct {
	id           uint16
	catalog      string
	questionSeen bool
	opened       bool
	closed       bool
	soaCount     int
	serial       uint32
	nsSeen       bool
	versionSeen  bool
	recordOwners map[string]bool
	members      map[string]bool
}

func newDNSCatalogAXFRState(id uint16, catalog string) (*dnsCatalogAXFRState, error) {
	if !serviceMutationCanonicalFQDN(catalog) {
		return nil, errors.New("BIND catalog AXFR identity is invalid")
	}
	return &dnsCatalogAXFRState{
		id:           id,
		catalog:      catalog,
		recordOwners: make(map[string]bool),
		members:      make(map[string]bool),
	}, nil
}

func (state *dnsCatalogAXFRState) parseMessage(message []byte) error {
	if state == nil || state.closed {
		return errors.New("BIND catalog AXFR contains data after its closing SOA")
	}
	if len(message) < 12 || binary.BigEndian.Uint16(message[:2]) != state.id {
		return errors.New("BIND catalog AXFR response identity mismatch")
	}
	// The proof is intentionally narrower than generic AXFR: both managed
	// authoritative peers must answer with AA and no unrelated DNS flags.
	if binary.BigEndian.Uint16(message[2:4]) != dnsResponseQR|dnsResponseAA {
		return errors.New("BIND catalog AXFR response flags are not exact")
	}
	questions := int(binary.BigEndian.Uint16(message[4:6]))
	answers := int(binary.BigEndian.Uint16(message[6:8]))
	authorities := int(binary.BigEndian.Uint16(message[8:10]))
	additionals := int(binary.BigEndian.Uint16(message[10:12]))
	// RFC 5936 requires the question in the first message and permits either
	// zero questions or an exact copy of it in each subsequent message.
	if (!state.questionSeen && questions != 1) ||
		(state.questionSeen && questions > 1) ||
		answers == 0 || authorities != 0 || additionals != 0 {
		return errors.New("BIND catalog AXFR section counts are not exact")
	}

	offset := 12
	if questions == 1 {
		name, next, err := decodeDNSName(message, offset)
		if err != nil || name != state.catalog+"." || next+4 > len(message) ||
			binary.BigEndian.Uint16(message[next:next+2]) != dnsTypeAXFR ||
			binary.BigEndian.Uint16(message[next+2:next+4]) != dnsClassIN {
			return errors.New("BIND catalog AXFR question is invalid")
		}
		offset = next + 4
		state.questionSeen = true
	}

	for recordIndex := 0; recordIndex < answers; recordIndex++ {
		ownerRaw, next, err := decodeDNSName(message, offset)
		if err != nil || next+10 > len(message) {
			return errors.New("BIND catalog AXFR record is invalid")
		}
		owner := strings.TrimSuffix(ownerRaw, ".")
		if ownerRaw != owner+"." || !serviceMutationCanonicalFQDN(owner) {
			return errors.New("BIND catalog AXFR owner is not canonical")
		}
		recordType := binary.BigEndian.Uint16(message[next : next+2])
		recordClass := binary.BigEndian.Uint16(message[next+2 : next+4])
		ttl := binary.BigEndian.Uint32(message[next+4 : next+8])
		rdataLength := int(binary.BigEndian.Uint16(message[next+8 : next+10]))
		rdataOffset := next + 10
		end := rdataOffset + rdataLength
		if end < rdataOffset || end > len(message) {
			return errors.New("BIND catalog AXFR record exceeds its message")
		}
		if recordClass != dnsClassIN || ttl != dnsCatalogAXFRTTL {
			return errors.New("BIND catalog AXFR record class or TTL is not exact")
		}
		if !state.opened && recordType != dnsTypeSOA {
			return errors.New("BIND catalog AXFR does not start with its SOA")
		}

		switch recordType {
		case dnsTypeSOA:
			serial, err := parseExactDNSCatalogSOA(
				message, owner, state.catalog, rdataOffset, end,
			)
			if err != nil {
				return err
			}
			switch state.soaCount {
			case 0:
				if recordIndex != 0 {
					return errors.New("BIND catalog AXFR opening SOA is misplaced")
				}
				state.opened = true
				state.serial = serial
			case 1:
				if serial != state.serial || recordIndex != answers-1 {
					return errors.New("BIND catalog AXFR closing SOA is not exact")
				}
				state.closed = true
			default:
				return errors.New("BIND catalog AXFR has more than two SOAs")
			}
			state.soaCount++
		case dnsTypeNS:
			if err := state.claimRecordOwner(owner); err != nil {
				return err
			}
			if owner != state.catalog || state.nsSeen {
				return errors.New("BIND catalog AXFR NS is not unique at the apex")
			}
			target, targetEnd, err := decodeDNSName(message, rdataOffset)
			if err != nil || targetEnd != end || target != "invalid." {
				return errors.New("BIND catalog AXFR NS target is not exact")
			}
			state.nsSeen = true
		case dnsTypeTXT:
			if err := state.claimRecordOwner(owner); err != nil {
				return err
			}
			if owner != "version."+state.catalog || state.versionSeen ||
				rdataLength != 2 || message[rdataOffset] != 1 || message[rdataOffset+1] != '2' {
				return errors.New("BIND catalog AXFR version property is not exact")
			}
			state.versionSeen = true
		case dnsTypePTR:
			if err := state.claimRecordOwner(owner); err != nil {
				return err
			}
			memberRaw, memberEnd, err := decodeDNSName(message, rdataOffset)
			member := strings.TrimSuffix(memberRaw, ".")
			if err != nil || memberEnd != end || memberRaw != member+"." ||
				!serviceMutationCanonicalFQDN(member) || member == state.catalog ||
				!exactDNSCatalogMemberOwner(owner, state.catalog, member) {
				return errors.New("BIND catalog AXFR member PTR is not exact")
			}
			if state.members[member] {
				return errors.New("BIND catalog AXFR contains a duplicate member")
			}
			state.members[member] = true
			if len(state.members) > dnsCatalogAXFRMaxMembers {
				return errors.New("BIND catalog AXFR exceeds the member limit")
			}
		case dnsTypeAPL:
			return errors.New("BIND catalog AXFR contains an unsupported transfer ACL property")
		default:
			return errors.New("BIND catalog AXFR contains an unsupported record type")
		}
		offset = end
	}
	if offset != len(message) {
		return errors.New("BIND catalog AXFR response contains trailing bytes")
	}
	return nil
}

func (state *dnsCatalogAXFRState) claimRecordOwner(owner string) error {
	if state.recordOwners[owner] {
		return errors.New("BIND catalog AXFR contains a duplicate record owner")
	}
	state.recordOwners[owner] = true
	return nil
}

func (state *dnsCatalogAXFRState) result() (dnsCatalogAXFRResult, error) {
	if state == nil || !state.questionSeen || !state.opened || !state.closed ||
		state.soaCount != 2 || state.serial == 0 {
		return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR has an invalid SOA envelope")
	}
	if !state.nsSeen || !state.versionSeen {
		return dnsCatalogAXFRResult{}, errors.New("BIND catalog AXFR base records are incomplete")
	}
	result := dnsCatalogAXFRResult{Serial: state.serial}
	for member := range state.members {
		result.Members = append(result.Members, member)
	}
	sort.Strings(result.Members)
	return result, nil
}

func parseExactDNSCatalogSOA(
	message []byte,
	owner, catalog string,
	rdataOffset, end int,
) (uint32, error) {
	if owner != catalog {
		return 0, errors.New("BIND catalog AXFR contains a foreign SOA")
	}
	mname, numbersOffset, err := decodeDNSName(message, rdataOffset)
	if err != nil || mname != "invalid." {
		return 0, errors.New("BIND catalog AXFR SOA primary is not exact")
	}
	rname, numbersOffset, err := decodeDNSName(message, numbersOffset)
	if err != nil || rname != "invalid." || numbersOffset+20 != end {
		return 0, errors.New("BIND catalog AXFR SOA payload is invalid")
	}
	serial := binary.BigEndian.Uint32(message[numbersOffset : numbersOffset+4])
	refresh := binary.BigEndian.Uint32(message[numbersOffset+4 : numbersOffset+8])
	retry := binary.BigEndian.Uint32(message[numbersOffset+8 : numbersOffset+12])
	expire := binary.BigEndian.Uint32(message[numbersOffset+12 : numbersOffset+16])
	minimum := binary.BigEndian.Uint32(message[numbersOffset+16 : numbersOffset+20])
	if serial == 0 || refresh != 60 || retry != 30 || expire != 3600 || minimum != 30 {
		return 0, errors.New("BIND catalog AXFR SOA timers are not exact")
	}
	return serial, nil
}

func exactDNSCatalogMemberOwner(owner, catalog, member string) bool {
	suffix := ".zones." + catalog
	if !strings.HasSuffix(owner, suffix) {
		return false
	}
	digestLabel := strings.TrimSuffix(owner, suffix)
	if len(digestLabel) != dnsCatalogMemberLabelSize || strings.Contains(digestLabel, ".") {
		return false
	}
	for _, value := range []byte(digestLabel) {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return false
		}
	}
	expected, err := binddns.CatalogMemberLabel(member)
	return err == nil && digestLabel == expected
}

func parseDNSCatalogAXFRMessage(
	message []byte,
	id uint16,
	catalog string,
) ([]uint32, []string, error) {
	state, err := newDNSCatalogAXFRState(id, catalog)
	if err != nil {
		return nil, nil, err
	}
	if err := state.parseMessage(message); err != nil {
		return nil, nil, err
	}
	result, err := state.result()
	if err != nil {
		return nil, nil, err
	}
	return []uint32{result.Serial, result.Serial}, result.Members, nil
}
