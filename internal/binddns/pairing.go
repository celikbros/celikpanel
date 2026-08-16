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
)

const catalogPrefix = "celikpanel-catalog."

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
	if len(catalogPrefix+localNS) > 253 || len(catalogPrefix+peerNS) > 253 {
		return Pairing{}, errors.New("BIND pair nameserver is too long for its catalog zone")
	}
	return Pairing{
		Role: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: localNS,
		PeerIP: pairing.PeerIP, PeerNS: peerNS,
	}, nil
}

func catalogDomain(nameserver string) string { return catalogPrefix + nameserver }

func pairingReceipt(_ string, pairing Pairing, serial uint32, catalog []byte) PairingReceipt {
	receipt := PairingReceipt{
		Role: pairing.Role, LocalIP: pairing.LocalIP, LocalNS: pairing.LocalNS,
		PeerIP: pairing.PeerIP, PeerNS: pairing.PeerNS,
		LocalCatalog:  catalogDomain(pairing.LocalNS),
		PeerCatalog:   catalogDomain(pairing.PeerNS),
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
	output.WriteString(catalogDomain(pairing.LocalNS))
	output.WriteString(".\n$TTL 60\n")
	output.WriteString("@ IN SOA ")
	output.WriteString(pairing.LocalNS)
	output.WriteString(". hostmaster.")
	output.WriteString(pairing.LocalNS)
	output.WriteString(". ")
	output.WriteString(fmt.Sprintf("%d 60 30 3600 30\n", serial))
	output.WriteString("@ IN NS ")
	output.WriteString(pairing.LocalNS)
	output.WriteString(".\nversion IN TXT \"2\"\n")
	for _, domain := range members {
		digest := sha256.Sum256([]byte(domain))
		output.WriteString(fmt.Sprintf("%x.zones IN PTR %s.\n", digest, domain))
	}
	return []byte(output.String()), nil
}

func appendPrimaryZoneConfig(
	config *strings.Builder,
	domain, absoluteFile string,
	pairing Pairing,
) {
	config.WriteString("zone \"")
	config.WriteString(domain)
	config.WriteString("\" {\n\ttype master;\n\tfile \"")
	config.WriteString(absoluteFile)
	config.WriteString("\";\n\tallow-transfer { ")
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
	domain := catalogDomain(pairing.LocalNS)
	file := path.Join(root, "generations", generationID, "zones", zoneFileName(domain))
	appendPrimaryZoneConfig(config, domain, file, pairing)
}

func appendSecondaryCatalogConfig(
	config *strings.Builder,
	_ string,
	pairing Pairing,
) {
	config.WriteString("catalog-zones {\n\tzone \"")
	config.WriteString(catalogDomain(pairing.PeerNS))
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
	if receipt.LocalCatalog != catalogDomain(pairing.LocalNS) ||
		receipt.PeerCatalog != catalogDomain(pairing.PeerNS) ||
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
