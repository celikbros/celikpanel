package dnswire

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
)

// QueryDeletedZoneSOA proves a frozen zone's apex is no longer served by the
// verified literal DNS endpoint. The caller must separately bind that endpoint
// to the owner-managed native process and bracket this point-in-time answer.
func QueryDeletedZoneSOA(ctx context.Context, network, endpoint, zone string) error {
	message, id, err := querySOAPacket(ctx, network, endpoint, zone)
	if err != nil {
		return err
	}
	return parseDeletedZoneSOAResponse(message, id, zone)
}

func parseDeletedZoneSOAResponse(message []byte, id uint16, zone string) error {
	if !canonicalHostname(zone) || len(message) < 12 || binary.BigEndian.Uint16(message[:2]) != id {
		return errors.New("deleted-zone SOA response identity differs")
	}
	flags := binary.BigEndian.Uint16(message[2:4])
	if flags&0x8000 == 0 || flags&0x0200 != 0 || flags&0x7800 != 0 {
		return errors.New("deleted-zone SOA response is not a complete ordinary answer")
	}
	if binary.BigEndian.Uint16(message[4:6]) != 1 ||
		binary.BigEndian.Uint16(message[6:8]) != 0 ||
		binary.BigEndian.Uint16(message[10:12]) != 0 {
		return errors.New("deleted-zone SOA response has an unexpected question, answer or additional record")
	}
	authorities := binary.BigEndian.Uint16(message[8:10])
	question, next, err := decodeDNSName(message, 12)
	if err != nil || next+4 > len(message) ||
		strings.ToLower(strings.TrimSuffix(question, ".")) != zone ||
		binary.BigEndian.Uint16(message[next:next+2]) != 6 ||
		binary.BigEndian.Uint16(message[next+2:next+4]) != classIN {
		return errors.New("deleted-zone SOA response question differs")
	}
	offset := next + 4
	rcode := flags & 0x000f
	// REFUSED or non-authoritative NXDOMAIN may describe an access policy or
	// referral, not absence of the frozen child zone.
	if flags&0x0400 == 0 || (rcode != 0 && rcode != 3) || authorities != 1 {
		return errors.New("authoritative deleted-zone response lacks one parent SOA")
	}
	owner, header, err := decodeDNSName(message, offset)
	if err != nil || header+10 > len(message) {
		return errors.New("deleted-zone parent SOA is malformed")
	}
	parent := strings.ToLower(strings.TrimSuffix(owner, "."))
	if !canonicalHostname(parent) || parent == zone || !strings.HasSuffix(zone, "."+parent) ||
		binary.BigEndian.Uint16(message[header:header+2]) != 6 ||
		binary.BigEndian.Uint16(message[header+2:header+4]) != classIN {
		return errors.New("deleted-zone response does not identify a strict parent SOA")
	}
	start := header + 10
	end := start + int(binary.BigEndian.Uint16(message[header+8:header+10]))
	if end > len(message) {
		return errors.New("deleted-zone parent SOA is truncated")
	}
	_, afterMNAME, err := decodeDNSName(message, start)
	if err != nil || afterMNAME >= end {
		return errors.New("deleted-zone parent SOA primary name is malformed")
	}
	_, afterRNAME, err := decodeDNSName(message, afterMNAME)
	if err != nil || afterRNAME+20 != end || end != len(message) {
		return errors.New("deleted-zone parent SOA payload is malformed")
	}
	return nil
}
