package binddns

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"path"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const catalogSuffix = ".celikpanel.invalid"

func clonePairing(pairing *Pairing) *Pairing {
	if pairing == nil {
		return nil
	}
	copy := *pairing
	return &copy
}

func pairingFromReceipt(receipt *PairingReceipt) *Pairing {
	if receipt == nil {
		return nil
	}
	return &Pairing{
		Role: receipt.Role, LocalIP: receipt.LocalIP, LocalNS: receipt.LocalNS,
		PeerIP: receipt.PeerIP, PeerNS: receipt.PeerNS,
	}
}

func canonicalPairing(pairing Pairing) (Pairing, error) {
	if pairing.Role != PairRolePrimary && pairing.Role != PairRoleSecondary {
		return Pairing{}, errors.New("BIND pair role must be primary or secondary")
	}
	localIP := net.ParseIP(pairing.LocalIP)
	peerIP := net.ParseIP(pairing.PeerIP)
	if localIP == nil || localIP.To4() == nil || localIP.String() != pairing.LocalIP ||
		!localIP.IsGlobalUnicast() {
		return Pairing{}, errors.New("BIND local pair IPv4 address must be canonical")
	}
	if peerIP == nil || peerIP.To4() == nil || peerIP.String() != pairing.PeerIP ||
		!peerIP.IsGlobalUnicast() || peerIP.Equal(localIP) {
		return Pairing{}, errors.New("BIND peer IPv4 address must be canonical and distinct")
	}
	localNS, err := hostname.CanonicalFQDN(pairing.LocalNS)
	if err != nil || localNS != pairing.LocalNS {
		return Pairing{}, errors.New("BIND local nameserver must be canonical")
	}
	peerNS, err := hostname.CanonicalFQDN(pairing.PeerNS)
	if err != nil || peerNS != pairing.PeerNS || peerNS == localNS {
		return Pairing{}, errors.New("BIND peer nameserver must be canonical and distinct")
	}
	return Pairing{
		Role: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: localNS,
		PeerIP: pairing.PeerIP, PeerNS: peerNS,
	}, nil
}

func catalogDomain(address string) string {
	ip := net.ParseIP(address).To4()
	return fmt.Sprintf(
		"catalog-%02x%02x%02x%02x%s",
		ip[0], ip[1], ip[2], ip[3], catalogSuffix,
	)
}

// CatalogDomain returns the deterministic catalog zone used by either BIND or
// PowerDNS when it is the primary side of a mixed-engine DNS pair.
func CatalogDomain(address string) (string, error) {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() == nil || ip.String() != address || !ip.IsGlobalUnicast() {
		return "", errors.New("catalog primary IPv4 address must be canonical")
	}
	return catalogDomain(address), nil
}

// CatalogMemberLabel returns the deterministic RFC 9432 member-node label
// shared by the BIND and PowerDNS producers and the AXFR verifier. SHA-224's
// 56 lowercase hexadecimal octets fit in one RFC 1035 DNS label.
func CatalogMemberLabel(member string) (string, error) {
	canonical, err := hostname.CanonicalFQDN(member)
	if err != nil || canonical != member {
		return "", errors.New("catalog member is not canonical")
	}
	digest := sha256.Sum224([]byte(canonical))
	return fmt.Sprintf("%x", digest), nil
}

// CatalogZoneRecords renders the RFC 9432 catalog-zone v2 records that a
// PowerDNS primary publishes for a BIND secondary. The catalog identity uses
// only the primary address, so both panels derive it without a private API.
func CatalogZoneRecords(
	address string,
	serial uint32,
	members []string,
) (string, []transport.ZoneRecord, error) {
	domain, err := CatalogDomain(address)
	if err != nil {
		return "", nil, err
	}
	if serial == 0 {
		return "", nil, errors.New("catalog serial must be positive")
	}
	canonicalMembers := make([]string, len(members))
	seen := make(map[string]bool, len(members))
	for index, member := range members {
		canonical, canonicalErr := hostname.CanonicalFQDN(member)
		if canonicalErr != nil || canonical != member || canonical == domain || seen[canonical] {
			return "", nil, errors.New("catalog member set is not canonical")
		}
		seen[canonical] = true
		canonicalMembers[index] = canonical
	}
	sort.Strings(canonicalMembers)
	records := []transport.ZoneRecord{
		{
			Name: domain, Type: "SOA",
			Content: fmt.Sprintf(
				"invalid. invalid. %d 60 30 3600 30",
				serial,
			),
			TTL: 60,
		},
		{Name: domain, Type: "NS", Content: "invalid.", TTL: 60},
		{Name: "version." + domain, Type: "TXT", Content: "\"2\"", TTL: 60},
	}
	for _, member := range canonicalMembers {
		label, labelErr := CatalogMemberLabel(member)
		if labelErr != nil {
			return "", nil, labelErr
		}
		records = append(records, transport.ZoneRecord{
			Name: label + ".zones." + domain,
			Type: "PTR", Content: member, TTL: 60,
		})
	}
	return domain, records, nil
}

func pairingReceipt(_ string, pairing Pairing, serial uint32, catalog []byte) PairingReceipt {
	receipt := PairingReceipt{
		Role: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: pairing.LocalNS,
		PeerIP: pairing.PeerIP, PeerNS: pairing.PeerNS,
		LocalCatalog:  catalogDomain(pairing.LocalIP),
		PeerCatalog:   catalogDomain(pairing.PeerIP),
		CatalogSerial: serial,
	}
	if pairing.Role == PairRolePrimary {
		receipt.CatalogFile = path.Join("zones", zoneFileName(receipt.LocalCatalog))
		receipt.CatalogSHA256 = sha256Hex(catalog)
	} else {
		receipt.InMemory = true
	}
	return receipt
}

func renderCatalogZone(pairing Pairing, serial uint32, zones []treeZone) ([]byte, error) {
	if pairing.Role != PairRolePrimary {
		return nil, nil
	}
	if serial == 0 {
		return nil, errors.New("BIND catalog serial must be positive")
	}
	members := make([]string, 0, len(zones))
	for _, zone := range zones {
		if !zone.receipt.Delete {
			members = append(members, zone.receipt.Domain)
		}
	}
	sort.Strings(members)
	var output strings.Builder
	output.WriteString("$ORIGIN ")
	catalog := catalogDomain(pairing.LocalIP)
	output.WriteString(catalog)
	output.WriteString(".\n$TTL 60\n")
	// RFC 9432 uses the deliberately out-of-bailiwick invalid. name for the
	// catalog's otherwise-unused SOA and NS targets. Keeping it out of this
	// synthetic zone also makes an empty catalog valid to named-checkzone.
	output.WriteString("@ IN SOA invalid. invalid. ")
	output.WriteString(fmt.Sprintf("%d 60 30 3600 30\n", serial))
	output.WriteString("@ IN NS invalid.\nversion IN TXT \"2\"\n")
	for _, domain := range members {
		label, err := CatalogMemberLabel(domain)
		if err != nil {
			return nil, err
		}
		output.WriteString(fmt.Sprintf("%s.zones IN PTR %s.\n", label, domain))
	}
	return []byte(output.String()), nil
}

func appendPrimaryZoneConfig(
	config *strings.Builder,
	domain, absoluteFile string,
	pairing Pairing,
) {
	appendPrimaryZoneConfigWithTransferPolicy(
		config, domain, absoluteFile, pairing, false,
	)
}

func appendLegacyPrimaryZoneConfig(
	config *strings.Builder,
	domain, absoluteFile string,
	pairing Pairing,
) {
	appendPrimaryZoneConfigWithTransferPolicy(
		config, domain, absoluteFile, pairing, true,
	)
}

func appendPrimaryZoneConfigWithTransferPolicy(
	config *strings.Builder,
	domain, absoluteFile string,
	pairing Pairing,
	legacyPeerOnly bool,
) {
	config.WriteString("zone \"")
	config.WriteString(domain)
	config.WriteString("\" {\n\ttype master;\n\tfile \"")
	config.WriteString(absoluteFile)
	config.WriteString("\";\n\tallow-transfer { ")
	if !legacyPeerOnly {
		config.WriteString(pairing.LocalIP)
		config.WriteString(`; `)
	}
	config.WriteString(pairing.PeerIP)
	config.WriteString("; };\n\talso-notify { ")
	config.WriteString(pairing.PeerIP)
	config.WriteString("; };\n\tnotify yes;\n};\n")
}

func appendPrimaryCatalogConfig(
	config *strings.Builder,
	root, generationID string,
	pairing Pairing,
) {
	appendPrimaryCatalogConfigWithTransferPolicy(
		config, root, generationID, pairing, false,
	)
}

func appendLegacyPrimaryCatalogConfig(
	config *strings.Builder,
	root, generationID string,
	pairing Pairing,
) {
	appendPrimaryCatalogConfigWithTransferPolicy(
		config, root, generationID, pairing, true,
	)
}

func appendPrimaryCatalogConfigWithTransferPolicy(
	config *strings.Builder,
	root, generationID string,
	pairing Pairing,
	legacyPeerOnly bool,
) {
	domain := catalogDomain(pairing.LocalIP)
	file := path.Join(root, "generations", generationID, "zones", zoneFileName(domain))
	if legacyPeerOnly {
		appendLegacyPrimaryZoneConfig(config, domain, file, pairing)
		return
	}
	appendPrimaryZoneConfig(config, domain, file, pairing)
}

func appendSecondaryCatalogConfig(
	config *strings.Builder,
	_ string,
	pairing Pairing,
) {
	config.WriteString("catalog-zones {\n\tzone \"")
	config.WriteString(catalogDomain(pairing.PeerIP))
	config.WriteString("\" {\n\t\tdefault-primaries { ")
	config.WriteString(pairing.PeerIP)
	config.WriteString("; };\n\t\tin-memory yes;\n\t};\n};\n")
}

func validatePairingReceipt(root string, receipt *PairingReceipt) error {
	if receipt == nil {
		return errors.New("BIND paired receipt is missing")
	}
	pairing, err := canonicalPairing(Pairing{
		Role: receipt.Role, LocalIP: receipt.LocalIP, LocalNS: receipt.LocalNS,
		PeerIP: receipt.PeerIP, PeerNS: receipt.PeerNS,
	})
	if err != nil {
		return err
	}
	if receipt.LocalCatalog != catalogDomain(pairing.LocalIP) ||
		receipt.PeerCatalog != catalogDomain(pairing.PeerIP) ||
		receipt.CatalogSerial == 0 {
		return errors.New("BIND paired receipt catalog identity is invalid")
	}
	if pairing.Role == PairRolePrimary {
		expectedFile := path.Join("zones", zoneFileName(receipt.LocalCatalog))
		if receipt.CatalogFile != expectedFile || !validDigest(receipt.CatalogSHA256) ||
			receipt.InMemory {
			return errors.New("BIND primary catalog receipt is invalid")
		}
		return nil
	}
	if receipt.CatalogFile != "" || receipt.CatalogSHA256 != "" || !receipt.InMemory {
		return errors.New("BIND secondary catalog receipt is invalid")
	}
	return nil
}
