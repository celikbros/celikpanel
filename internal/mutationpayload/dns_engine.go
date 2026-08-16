package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"net"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsZoneSyncV3Schema                   = "dns-zone-sync/v3"
	dnsZoneSyncV3QualifierPrefix          = dnsZoneSyncV3Schema + ":sha256:"
	dnsEngineSwitchSchema                 = "dns-engine-switch/v1"
	dnsEngineSwitchQualifierPrefix        = dnsEngineSwitchSchema + ":sha256:"
	dnsEngineSwitchMaxZones               = 65536
	DNSEngineSwitchMaxSnapshotBytes int64 = 64 << 20
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
	Mode           string
	SourceEngine   transport.DNSEngine
	TargetEngine   transport.DNSEngine
	SourceEpoch    int64
	TargetEpoch    int64
	SourceRevision int64
	Topology       string
	PairRole       string
	LocalIP        string
	LocalNS        string
	PeerIP         string
	PeerNS         string
	Zones          []transport.DNSEngineSwitchZoneSnapshot
	SnapshotBytes  int64
	Qualifier      string
}

func CanonicalDNSEngineSwitchManifest(
	mode string,
	sourceEngine, targetEngine transport.DNSEngine,
	sourceEpoch, targetEpoch, sourceRevision int64,
	topology string,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) (DNSEngineSwitchManifestCommitment, error) {
	return CanonicalDNSEngineSwitchManifestWithPeer(
		mode, sourceEngine, targetEngine,
		sourceEpoch, targetEpoch, sourceRevision,
		topology, "", "", zones,
	)
}

// CanonicalDNSEngineSwitchManifestWithPeer binds the exact managed peer tuple
// used by paired PowerDNS adoption. Standalone operations must use an empty
// tuple, so crash recovery never needs to consult mutable panel settings.
func CanonicalDNSEngineSwitchManifestWithPeer(
	mode string,
	sourceEngine, targetEngine transport.DNSEngine,
	sourceEpoch, targetEpoch, sourceRevision int64,
	topology, peerIP, peerNS string,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) (DNSEngineSwitchManifestCommitment, error) {
	return CanonicalDNSEngineSwitchManifestWithPairIdentity(
		mode, sourceEngine, targetEngine,
		sourceEpoch, targetEpoch, sourceRevision,
		topology, "", "", "", peerIP, peerNS, zones,
	)
}

// CanonicalDNSEngineSwitchManifestWithPairIdentity extends the released
// standalone/adoption contract with an engine-neutral directional pair.
// Empty fields preserve the exact alpha.24 standalone commitment.
func CanonicalDNSEngineSwitchManifestWithPairIdentity(
	mode string,
	sourceEngine, targetEngine transport.DNSEngine,
	sourceEpoch, targetEpoch, sourceRevision int64,
	topology, pairRole, localIP, localNS, peerIP, peerNS string,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) (DNSEngineSwitchManifestCommitment, error) {
	if mode != transport.DNSEngineSwitchModeSwitch &&
		mode != transport.DNSEngineSwitchModeAdopt {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine operation mode must be switch or adopt")
	}
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
	if mode == transport.DNSEngineSwitchModeAdopt &&
		(sourceEngine != "" || targetEngine != transport.DNSEnginePowerDNS) {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine adoption requires an unresolved PowerDNS source")
	}
	if targetEpoch != sourceEpoch+1 || targetEpoch < 1 {
		return DNSEngineSwitchManifestCommitment{}, errors.New("target DNS engine epoch must immediately follow source epoch")
	}
	if sourceRevision < 0 {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine source revision must not be negative")
	}
	if mode == transport.DNSEngineSwitchModeSwitch &&
		topology != transport.DNSTopologyStandalone &&
		topology != transport.DNSTopologyPaired {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switching topology must be standalone or paired")
	}
	if mode == transport.DNSEngineSwitchModeAdopt &&
		topology != transport.DNSTopologyStandalone && topology != transport.DNSTopologyPaired {
		return DNSEngineSwitchManifestCommitment{}, errors.New("PowerDNS adoption topology must be standalone or paired")
	}
	cluster, err := CanonicalDNSClusterConfig(topology, peerIP, peerNS)
	if err != nil {
		return DNSEngineSwitchManifestCommitment{}, err
	}
	canonicalRole, canonicalLocalIP, canonicalLocalNS := "", "", ""
	if mode == transport.DNSEngineSwitchModeSwitch && topology == transport.DNSTopologyPaired {
		canonicalRole, canonicalLocalIP, canonicalLocalNS, err =
			canonicalDNSPairIdentity(pairRole, localIP, localNS, cluster.PeerIP, cluster.PeerNS)
		if err != nil {
			return DNSEngineSwitchManifestCommitment{}, err
		}
	} else if pairRole != "" || localIP != "" || localNS != "" {
		return DNSEngineSwitchManifestCommitment{}, errors.New("standalone or adoption DNS engine payload contains a BIND pair identity")
	}
	if len(zones) > dnsEngineSwitchMaxZones {
		return DNSEngineSwitchManifestCommitment{}, errors.New("DNS engine switch manifest exceeds the zone limit")
	}

	frozen := make([]transport.DNSEngineSwitchZoneSnapshot, len(zones))
	var snapshotBytes int64
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
		recordsJSON, err := MarshalDNSZoneSnapshotRecords(commitment.Records)
		if err != nil {
			return DNSEngineSwitchManifestCommitment{}, err
		}
		snapshotBytes, err = checkedDNSEngineSnapshotBytes(snapshotBytes, len(recordsJSON))
		if err != nil {
			return DNSEngineSwitchManifestCommitment{}, err
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
		Mode:         mode,
		SourceEngine: sourceEngine, TargetEngine: targetEngine,
		SourceEpoch: sourceEpoch, TargetEpoch: targetEpoch,
		SourceRevision: sourceRevision, Topology: cluster.Role,
		PairRole: canonicalRole, LocalIP: canonicalLocalIP, LocalNS: canonicalLocalNS,
		PeerIP: cluster.PeerIP, PeerNS: cluster.PeerNS, Zones: frozen,
		SnapshotBytes: snapshotBytes,
	}
	commitment.Qualifier = qualifyDNSEngineSwitch(commitment)
	return commitment, nil
}

// MarshalDNSZoneSnapshotRecords returns the exact compact JSON stored in a
// durable switch-zone row. Empty deletion snapshots are always [] rather than
// null so SQL constraints and RPC canonicalization share one representation.
func MarshalDNSZoneSnapshotRecords(records []transport.ZoneRecord) ([]byte, error) {
	if len(records) == 0 {
		return []byte("[]"), nil
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return nil, errors.New("encode DNS zone snapshot records")
	}
	return encoded, nil
}

func checkedDNSEngineSnapshotBytes(current int64, additional int) (int64, error) {
	if current < 0 || additional < 0 ||
		int64(additional) > DNSEngineSwitchMaxSnapshotBytes-current {
		return 0, errors.New("DNS engine switch snapshot exceeds the 64 MiB limit")
	}
	return current + int64(additional), nil
}

func canonicalDNSPairIdentity(
	pairRole, localIP, localNS, peerIP, peerNS string,
) (string, string, string, error) {
	if pairRole != transport.DNSPairRolePrimary &&
		pairRole != transport.DNSPairRoleSecondary {
		return "", "", "", errors.New("DNS pair role must be primary or secondary")
	}
	parsedLocal := net.ParseIP(localIP)
	parsedPeer := net.ParseIP(peerIP)
	if parsedLocal == nil || parsedLocal.To4() == nil ||
		parsedLocal.String() != localIP || !parsedLocal.IsGlobalUnicast() {
		return "", "", "", errors.New("DNS pair local IPv4 address must be canonical")
	}
	if parsedPeer == nil || parsedPeer.To4() == nil || parsedPeer.Equal(parsedLocal) {
		return "", "", "", errors.New("BIND pair peer IPv4 address must be distinct")
	}
	canonicalLocalNS, err := hostname.CanonicalFQDN(localNS)
	if err != nil || canonicalLocalNS != localNS || canonicalLocalNS == peerNS {
		return "", "", "", errors.New("BIND pair local nameserver must be canonical and distinct")
	}
	return pairRole, localIP, canonicalLocalNS, nil
}

func qualifyDNSEngineSwitch(commitment DNSEngineSwitchManifestCommitment) string {
	digest := sha256.New()
	for _, frame := range [][]byte{
		[]byte("celikpanel/service-mutation-payload"),
		[]byte(dnsEngineSwitchSchema),
		[]byte("dns_engine_switch"),
		[]byte("Agent.SwitchDNSEngineV1"),
		[]byte(commitment.Mode),
	} {
		writeDNSEngineDigestFrame(digest, frame)
	}
	writeDNSEngineDigestFrame(digest, []byte(commitment.SourceEngine))
	writeDNSEngineDigestFrame(digest, []byte(commitment.TargetEngine))
	writeDNSEngineUint64(digest, commitment.SourceEpoch)
	writeDNSEngineUint64(digest, commitment.TargetEpoch)
	writeDNSEngineUint64(digest, commitment.SourceRevision)
	writeDNSEngineDigestFrame(digest, []byte(commitment.Topology))
	if commitment.PairRole != "" {
		writeDNSEngineDigestFrame(digest, []byte("bind-pair/v1"))
		writeDNSEngineDigestFrame(digest, []byte(commitment.PairRole))
		writeDNSEngineDigestFrame(digest, []byte(commitment.LocalIP))
		writeDNSEngineDigestFrame(digest, []byte(commitment.LocalNS))
	}
	writeDNSEngineDigestFrame(digest, []byte(commitment.PeerIP))
	writeDNSEngineDigestFrame(digest, []byte(commitment.PeerNS))
	writeDNSEngineUint64(digest, commitment.SnapshotBytes)
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
