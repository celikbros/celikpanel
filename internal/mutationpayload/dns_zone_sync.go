package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsZoneSyncSchema                  = "dns-zone-sync/v1"
	dnsZoneSyncQualifierPrefix         = dnsZoneSyncSchema + ":sha256:"
	dnsZoneSyncMaxRecords              = 16384
	dnsZoneSyncMaxRecordTypeBytes      = 10
	dnsZoneSyncMaxRecordContentBytes   = 65535
	dnsZoneSyncMaxSnapshotPayloadBytes = 8 << 20
	dnsZoneSyncMaxTTL                  = 1<<31 - 1
	dnsZoneSyncMaxPriority             = 1<<16 - 1
)

var dnsZoneSyncDigestFrames = [][]byte{
	[]byte("celikpanel/service-mutation-payload"),
	[]byte(dnsZoneSyncSchema),
	[]byte("dns_zone_sync"),
	[]byte("powerdns"),
	[]byte("Agent.SyncDNSZoneV2"),
}

// DNSZoneSyncCommitment is the canonical, detached full-zone snapshot that a
// durable direct mutation authorizes. Records are sorted, but duplicates are
// deliberately retained: duplicate multiplicity is part of PowerDNS state.
type DNSZoneSyncCommitment struct {
	DesiredGeneration int64
	Domain            string
	Delete            bool
	ZoneType          string
	Records           []transport.ZoneRecord
	Qualifier         string
}

// CanonicalDNSZoneSync validates and freezes every effective field before
// either side stores a lease, hashes a payload or touches the PowerDNS DB.
func CanonicalDNSZoneSync(
	desiredGeneration int64,
	domain string,
	deleteZone bool,
	zoneType string,
	records []transport.ZoneRecord,
) (DNSZoneSyncCommitment, error) {
	if desiredGeneration < 0 {
		return DNSZoneSyncCommitment{}, errors.New("DNS zone generation must not be negative")
	}
	canonicalDomain, err := hostname.CanonicalFQDN(domain)
	if err != nil {
		return DNSZoneSyncCommitment{}, errors.New("invalid DNS zone domain")
	}
	canonicalZoneType, err := canonicalDNSZoneType(zoneType)
	if err != nil {
		return DNSZoneSyncCommitment{}, err
	}
	if deleteZone && len(records) != 0 {
		return DNSZoneSyncCommitment{}, errors.New("DNS zone deletion must not contain hidden records")
	}
	if !deleteZone && len(records) == 0 {
		return DNSZoneSyncCommitment{}, errors.New("DNS zone sync requires a full non-empty snapshot")
	}
	if len(records) > dnsZoneSyncMaxRecords {
		return DNSZoneSyncCommitment{}, errors.New("DNS zone snapshot exceeds the record limit")
	}

	frozen := make([]transport.ZoneRecord, len(records))
	payloadBytes := 0
	for index, record := range records {
		canonical, size, err := canonicalDNSZoneRecord(canonicalDomain, record)
		if err != nil {
			return DNSZoneSyncCommitment{}, err
		}
		if size > dnsZoneSyncMaxSnapshotPayloadBytes-payloadBytes {
			return DNSZoneSyncCommitment{}, errors.New("DNS zone snapshot exceeds the payload limit")
		}
		payloadBytes += size
		frozen[index] = canonical
	}
	if deleteZone {
		// Make deletion's empty snapshot representation independent of whether
		// the caller supplied nil or a non-nil zero-length slice.
		frozen = nil
	}

	sort.SliceStable(frozen, func(left, right int) bool {
		return lessDNSZoneRecord(frozen[left], frozen[right])
	})

	digest := sha256.New()
	for _, frame := range dnsZoneSyncDigestFrames {
		writeDNSZoneSyncDigestFrame(digest, frame)
	}
	var generation [8]byte
	binary.BigEndian.PutUint64(generation[:], uint64(desiredGeneration))
	_, _ = digest.Write(generation[:])
	writeDNSZoneSyncDigestFrame(digest, []byte(canonicalDomain))
	if deleteZone {
		writeDNSZoneSyncDigestFrame(digest, []byte("delete"))
	} else {
		writeDNSZoneSyncDigestFrame(digest, []byte("sync"))
	}
	writeDNSZoneSyncDigestFrame(digest, []byte(canonicalZoneType))
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(frozen)))
	_, _ = digest.Write(count[:])
	for _, record := range frozen {
		writeDNSZoneSyncDigestFrame(digest, []byte(record.Name))
		writeDNSZoneSyncDigestFrame(digest, []byte(record.Type))
		writeDNSZoneSyncDigestFrame(digest, []byte(record.Content))
		var ttl [4]byte
		binary.BigEndian.PutUint32(ttl[:], uint32(record.TTL))
		_, _ = digest.Write(ttl[:])
		var priority [2]byte
		binary.BigEndian.PutUint16(priority[:], uint16(record.Prio))
		_, _ = digest.Write(priority[:])
		if record.Disabled {
			_, _ = digest.Write([]byte{1})
		} else {
			_, _ = digest.Write([]byte{0})
		}
	}

	return DNSZoneSyncCommitment{
		DesiredGeneration: desiredGeneration,
		Domain:            canonicalDomain,
		Delete:            deleteZone,
		ZoneType:          canonicalZoneType,
		Records:           frozen,
		Qualifier: dnsZoneSyncQualifierPrefix +
			hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func canonicalDNSZoneType(raw string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value != "NATIVE" && value != "MASTER" {
		return "", errors.New("DNS zone type must be NATIVE or MASTER")
	}
	return value, nil
}

func canonicalDNSZoneRecord(
	domain string,
	record transport.ZoneRecord,
) (transport.ZoneRecord, int, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Name), "."))
	if !validCanonicalDNSRecordName(name) ||
		(name != domain && !strings.HasSuffix(name, "."+domain)) {
		return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains an invalid record name")
	}
	recordType := strings.ToUpper(strings.TrimSpace(record.Type))
	if len(recordType) == 0 || len(recordType) > dnsZoneSyncMaxRecordTypeBytes {
		return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains an invalid record type")
	}
	for _, character := range recordType {
		if (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains an invalid record type")
		}
	}
	if len(record.Content) == 0 ||
		len(record.Content) > dnsZoneSyncMaxRecordContentBytes ||
		!utf8.ValidString(record.Content) {
		return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains invalid record content")
	}
	for _, character := range record.Content {
		if character < 0x20 || character == 0x7f {
			return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains invalid record content")
		}
	}
	if record.TTL < 0 || record.TTL > dnsZoneSyncMaxTTL {
		return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains an invalid TTL")
	}
	if record.Prio < 0 || record.Prio > dnsZoneSyncMaxPriority {
		return transport.ZoneRecord{}, 0, errors.New("DNS zone snapshot contains an invalid priority")
	}
	canonical := transport.ZoneRecord{
		Name: name, Type: recordType, Content: record.Content,
		TTL: record.TTL, Prio: record.Prio, Disabled: record.Disabled,
	}
	return canonical, len(name) + len(recordType) + len(record.Content) + 7, nil
}

func validCanonicalDNSRecordName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for index, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label == "*" {
			if index != 0 {
				return false
			}
			continue
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') &&
				character != '-' && character != '_' {
				return false
			}
		}
	}
	return true
}

func lessDNSZoneRecord(left, right transport.ZoneRecord) bool {
	if left.Name != right.Name {
		return left.Name < right.Name
	}
	if left.Type != right.Type {
		return left.Type < right.Type
	}
	if left.Content != right.Content {
		return left.Content < right.Content
	}
	if left.TTL != right.TTL {
		return left.TTL < right.TTL
	}
	if left.Prio != right.Prio {
		return left.Prio < right.Prio
	}
	return !left.Disabled && right.Disabled
}

func writeDNSZoneSyncDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

// ValidDNSZoneSyncQualifier accepts only the canonical v1 lowercase SHA-256
// representation stored in ServiceMutationJob.PackageName.
func ValidDNSZoneSyncQualifier(value string) bool {
	if len(value) != len(dnsZoneSyncQualifierPrefix)+sha256.Size*2 ||
		!strings.HasPrefix(value, dnsZoneSyncQualifierPrefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(value, dnsZoneSyncQualifierPrefix) {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
