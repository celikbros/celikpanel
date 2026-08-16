package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsZoneSyncV3Schema            = "dns-zone-sync/v3"
	dnsZoneSyncV3QualifierPrefix   = dnsZoneSyncV3Schema + ":sha256:"
	dnsEngineSwitchSchema          = "dns-engine-switch/v1"
	dnsEngineSwitchQualifierPrefix = dnsEngineSwitchSchema + ":sha256:"
	dnsEngineSwitchMaxZones        = 65536
)

// DNSZoneSyncV3Commitment is an engine- and epoch-bound full-zone snapshot.
// The legacy DNSZoneSyncCommitment remains the exact PowerDNS V1/V2 contract.
type DNSZoneSyncV3Commitment struct {
	Engine            string
	EngineEpoch       int64
	DesiredGeneration int64
	Domain            string
	Delete            bool
	ZoneType          string
	Records           []transport.ZoneRecord
	Qualifier         string
}

// CanonicalDNSZoneSyncV3 validates and freezes the V2 full-zone tuple, then
// additionally commits the exact target engine and monotonic activation epoch.
func CanonicalDNSZoneSyncV3(
	engine transport.DNSEngine,
	engineEpoch, desiredGeneration int64,
	domain string,
	deleteZone bool,
	zoneType string,
	records []transport.ZoneRecord,
) (DNSZoneSyncV3Commitment, error) {
	if !transport.ValidDNSEngine(engine) {
		return DNSZoneSyncV3Commitment{}, errors.New("DNS engine must be pdns or bind")
	}
	if engineEpoch < 1 {
		return DNSZoneSyncV3Commitment{}, errors.New("DNS engine epoch must be positive")
	}
	base, err := CanonicalDNSZoneSync(
		desiredGeneration, domain, deleteZone, zoneType, records,
	)
	if err != nil {
		return DNSZoneSyncV3Commitment{}, err
	}
	commitment := DNSZoneSyncV3Commitment{
		Engine: string(engine), EngineEpoch: engineEpoch,
		DesiredGeneration: base.DesiredGeneration,
		Domain:            base.Domain, Delete: base.Delete, ZoneType: base.ZoneType,
		Records: base.Records,
	}
	commitment.Qualifier = qualifyDNSZoneSyncV3(commitment)
	return commitment, nil
}

func qualifyDNSZoneSyncV3(commitment DNSZoneSyncV3Commitment) string {
	digest := sha256.New()
	for _, frame := range [][]byte{
		[]byte("celikpanel/service-mutation-payload"),
		[]byte(dnsZoneSyncV3Schema),
		[]byte("dns_zone_sync"),
		[]byte(commitment.Engine),
		[]byte("Agent.SyncDNSZoneV3"),
	} {
		writeDNSEngineDigestFrame(digest, frame)
	}
	writeDNSEngineUint64(digest, commitment.EngineEpoch)
	writeDNSEngineUint64(digest, commitment.DesiredGeneration)
	writeDNSEngineDigestFrame(digest, []byte(commitment.Domain))
	if commitment.Delete {
		writeDNSEngineDigestFrame(digest, []byte("delete"))
	} else {
		writeDNSEngineDigestFrame(digest, []byte("sync"))
	}
	writeDNSEngineDigestFrame(digest, []byte(commitment.ZoneType))
	writeDNSEngineUint32(digest, len(commitment.Records))
	for _, record := range commitment.Records {
		writeDNSEngineDigestFrame(digest, []byte(record.Name))
		writeDNSEngineDigestFrame(digest, []byte(record.Type))
		writeDNSEngineDigestFrame(digest, []byte(record.Content))
		writeDNSEngineUint32(digest, record.TTL)
		var priority [2]byte
		binary.BigEndian.PutUint16(priority[:], uint16(record.Prio))
		_, _ = digest.Write(priority[:])
		if record.Disabled {
			_, _ = digest.Write([]byte{1})
		} else {
			_, _ = digest.Write([]byte{0})
		}
	}
	return dnsZoneSyncV3QualifierPrefix + hex.EncodeToString(digest.Sum(nil))
}

// DNSEngineSwitchManifestCommitment is a detached, canonical whole-server DNS
// publication snapshot. Each zone qualifier commits its full record snapshot.
type DNSEngineSwitchManifestCommitment struct {
	SourceEngine   transport.DNSEngine
	TargetEngine   transport.DNSEngine
	SourceEpoch    int64
	TargetEpoch    int64
	SourceRevision int64
	Topology       string
	Zones          []transport.DNSEngineSwitchZoneSnapshot
	Qualifier      string
}

func CanonicalDNSEngineSwitchManifest(
	sourceEngine, targetEngine transport.DNSEngine,
	sourceEpoch, targetEpoch, sourceRevision int64,
	topology string,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) (DNSEngineSwitchManifestCommitment, error) {
	if sourceEngine == "" {
		if sourceEpoch != 0 {
			return DNSEngineSwitchManifestCommitment{}, errors.New("unresolved DNS engine must use epoch zero")
		}
	} else if !transport.ValidDNSEngine(sourceEngine) || sourceEpoch < 1 {
		return DNSEngineSwitchManifestCommitment{}, errors.New("invalid source DNS engine identity")
	}
	if !transport.ValidDNSEngine(targetEngine) {
		return DNSEngineSwitchManifestCommitment{}, errors.New("target DNS engine must be pdns or bind")
	}
	if sourceEngine == targetEngine {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switch target must differ from source")
	}
	if targetEpoch != sourceEpoch+1 || targetEpoch < 1 {
		return DNSEngineSwitchManifestCommitment{}, errors.New("target DNS engine epoch must immediately follow source epoch")
	}
	if sourceRevision < 0 {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine source revision must not be negative")
	}
	if topology != transport.DNSTopologyStandalone {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switching currently requires standalone topology")
	}
	if len(zones) > dnsEngineSwitchMaxZones {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switch manifest exceeds the zone limit")
	}

	frozen := make([]transport.DNSEngineSwitchZoneSnapshot, len(zones))
	for index, zone := range zones {
		commitment, err := CanonicalDNSZoneSyncV3(
			targetEngine, targetEpoch, zone.DesiredGeneration,
			zone.Domain, zone.Delete, zone.ZoneType, zone.Records,
		)
		if err != nil {
			return DNSEngineSwitchManifestCommitment{}, err
		}
		if zone.ZoneQualifier != "" && zone.ZoneQualifier != commitment.Qualifier {
			return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switch zone qualifier mismatch")
		}
		frozen[index] = transport.DNSEngineSwitchZoneSnapshot{
			Domain: commitment.Domain, DesiredGeneration: commitment.DesiredGeneration,
			Delete: commitment.Delete, ZoneType: commitment.ZoneType,
			Records: commitment.Records, ZoneQualifier: commitment.Qualifier,
		}
	}
	sort.Slice(frozen, func(left, right int) bool {
		return frozen[left].Domain < frozen[right].Domain
	})
	for index := range frozen {
		if index > 0 && frozen[index-1].Domain == frozen[index].Domain {
			return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switch manifest contains a duplicate zone")
		}
		frozen[index].Ordinal = index
	}

	commitment := DNSEngineSwitchManifestCommitment{
		SourceEngine: sourceEngine, TargetEngine: targetEngine,
		SourceEpoch: sourceEpoch, TargetEpoch: targetEpoch,
		SourceRevision: sourceRevision, Topology: topology, Zones: frozen,
	}
	commitment.Qualifier = qualifyDNSEngineSwitch(commitment)
	return commitment, nil
}

func qualifyDNSEngineSwitch(commitment DNSEngineSwitchManifestCommitment) string {
	digest := sha256.New()
	for _, frame := range [][]byte{
		[]byte("celikpanel/service-mutation-payload"),
		[]byte(dnsEngineSwitchSchema),
		[]byte("dns_engine_switch"),
		[]byte("Agent.SwitchDNSEngineV1"),
	} {
		writeDNSEngineDigestFrame(digest, frame)
	}
	writeDNSEngineDigestFrame(digest, []byte(commitment.SourceEngine))
	writeDNSEngineDigestFrame(digest, []byte(commitment.TargetEngine))
	writeDNSEngineUint64(digest, commitment.SourceEpoch)
	writeDNSEngineUint64(digest, commitment.TargetEpoch)
	writeDNSEngineUint64(digest, commitment.SourceRevision)
	writeDNSEngineDigestFrame(digest, []byte(commitment.Topology))
	writeDNSEngineUint32(digest, len(commitment.Zones))
	for _, zone := range commitment.Zones {
		writeDNSEngineUint32(digest, zone.Ordinal)
		writeDNSEngineDigestFrame(digest, []byte(zone.Domain))
		writeDNSEngineUint64(digest, zone.DesiredGeneration)
		if zone.Delete {
			writeDNSEngineDigestFrame(digest, []byte("delete"))
		} else {
			writeDNSEngineDigestFrame(digest, []byte("sync"))
		}
		writeDNSEngineDigestFrame(digest, []byte(zone.ZoneType))
		writeDNSEngineDigestFrame(digest, []byte(zone.ZoneQualifier))
	}
	return dnsEngineSwitchQualifierPrefix + hex.EncodeToString(digest.Sum(nil))
}

func writeDNSEngineDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func writeDNSEngineUint64(destination hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = destination.Write(encoded[:])
}

func writeDNSEngineUint32(destination hash.Hash, value int) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], uint32(value))
	_, _ = destination.Write(encoded[:])
}

func validDNSEngineQualifier(value, prefix string) bool {
	if len(value) != len(prefix)+sha256.Size*2 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(value, prefix) {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func ValidDNSZoneSyncV3Qualifier(value string) bool {
	return validDNSEngineQualifier(value, dnsZoneSyncV3QualifierPrefix)
}

func ValidDNSEngineSwitchQualifier(value string) bool {
	return validDNSEngineQualifier(value, dnsEngineSwitchQualifierPrefix)
}
