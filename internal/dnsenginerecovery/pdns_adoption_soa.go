package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/dnswire"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// AdoptionSOAQuery is an exact authoritative, nonrecursive SOA query over one
// named transport. Its endpoint is a literal address selected from verified
// local DNS listeners, never a resolver or journal-supplied hostname.
type AdoptionSOAQuery func(context.Context, string, string, string) (uint32, error)

func installedAdoptionSOAQuery(ctx context.Context, network, endpoint, zone string) (uint32, error) {
	switch network {
	case "udp":
		return dnswire.QueryAuthoritativeSOAUDP(ctx, endpoint, zone)
	case "tcp":
		return dnswire.QueryAuthoritativeSOA(ctx, endpoint, zone)
	default:
		return 0, errors.New("unsupported authoritative DNS transport")
	}
}

// ProbeInstalledPDNSAdoptionSOA uses bounded DNS wire probes for the
// previously selected and verified native listener address.
func ProbeInstalledPDNSAdoptionSOA(ctx context.Context, endpoint string, manifest mutationpayload.DNSEngineSwitchManifestCommitment) (active, deleted int, err error) {
	return ProbePDNSAdoptionSOA(ctx, endpoint, manifest, installedAdoptionSOAQuery)
}

// ProbePDNSAdoptionSOA compares live UDP and TCP authority for every active
// frozen zone. Deleted-zone absence and other record contents remain separate
// proofs; their count is returned so a caller cannot claim full zone coverage.
// The operation must already have a trusted journal and a verified native
// process/listener at endpoint. This never authorizes an inverse.
func ProbePDNSAdoptionSOA(ctx context.Context, endpoint string, manifest mutationpayload.DNSEngineSwitchManifestCommitment, query AdoptionSOAQuery) (active, deleted int, err error) {
	if ctx == nil || query == nil || manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		manifest.SourceEngine != "" || manifest.TargetEngine != transport.DNSEnginePowerDNS {
		return 0, 0, errors.New("PowerDNS adoption answer proof received an invalid manifest")
	}
	host, port, splitErr := net.SplitHostPort(endpoint)
	ip := net.ParseIP(host)
	if splitErr != nil || ip == nil || ip.To4() == nil || ip.To4().String() != host ||
		ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || port != "53" {
		return 0, 0, errors.New("PowerDNS adoption answer endpoint is not a concrete local IPv4 port 53")
	}
	for _, zone := range manifest.Zones {
		if zone.Delete {
			deleted++
			continue
		}
		serial, serialErr := frozenAdoptionSOASerial(zone)
		if serialErr != nil {
			return active, deleted, serialErr
		}
		for _, network := range []string{"udp", "tcp"} {
			if err := ctx.Err(); err != nil {
				return active, deleted, err
			}
			actual, queryErr := query(ctx, network, endpoint, zone.Domain)
			if queryErr != nil {
				return active, deleted, fmt.Errorf("query %s authoritative SOA for %s: %w", network, zone.Domain, queryErr)
			}
			if actual != serial {
				return active, deleted, fmt.Errorf("%s authoritative SOA for %s differs from the frozen serial", network, zone.Domain)
			}
		}
		active++
	}
	return active, deleted, nil
}

func frozenAdoptionSOASerial(zone transport.DNSEngineSwitchZoneSnapshot) (uint32, error) {
	var count int
	var serial uint32
	for _, record := range zone.Records {
		if record.Disabled || strings.ToUpper(strings.TrimSpace(record.Type)) != "SOA" {
			continue
		}
		owner := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(record.Name), "."))
		if owner != zone.Domain {
			return 0, fmt.Errorf("zone %s has an enabled SOA outside its apex", zone.Domain)
		}
		fields := strings.Fields(record.Content)
		if len(fields) != 7 {
			return 0, fmt.Errorf("zone %s has an invalid SOA field count", zone.Domain)
		}
		value, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("zone %s has an invalid SOA serial", zone.Domain)
		}
		serial = uint32(value)
		count++
	}
	if count != 1 {
		return 0, fmt.Errorf("zone %s does not have exactly one enabled apex SOA", zone.Domain)
	}
	return serial, nil
}
