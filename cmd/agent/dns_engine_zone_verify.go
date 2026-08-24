package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
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
	dnsRCodeNotAuth   = 9
	dnsProbeTimeout   = 4 * time.Second
	dnsPairProofLimit = 15 * time.Second
)

type expectedDNSZoneAuthority struct {
	Domain string
	Delete bool
	Serial uint32
}

func exactDNSZoneSerialAt(
	ctx context.Context,
	address, domain string,
) (uint32, error) {
	return exactDNSZoneSerialAtWithProbe(
		ctx, address, domain, probeDNSZoneSOA,
	)
}

func exactDNSZoneSerialAtWithProbe(
	ctx context.Context,
	address, domain string,
	probe dnsZoneSOAProbe,
) (uint32, error) {
	var serial uint32
	for index, network := range []string{"udp", "tcp"} {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := probe(probeCtx, network, address, domain)
		cancel()
		if err != nil || !result.Authoritative || result.RCode != dnsRCodeNoError ||
			len(result.SOASerials) != 1 {
			if err == nil {
				err = errors.New("DNS zone did not return one authoritative SOA")
			}
			return 0, fmt.Errorf("verify %s DNS SOA: %w", network, err)
		}
		if index == 0 {
			serial = result.SOASerials[0]
		} else if result.SOASerials[0] != serial {
			return 0, errors.New("DNS zone UDP and TCP serials differ")
		}
	}
	return serial, nil
}

func bindLocalProofAddress() (string, error) {
	addresses, err := publicListenAddresses(context.Background())
	if err != nil {
		return "", fmt.Errorf("discover BIND pair proof address: %w", err)
	}
	return addresses[0], nil
}

var dnsPairLocalProofAddress = bindLocalProofAddress
var dnsPairHostOwnedAddresses = hostOwnedDNSPairAddresses
var dnsPairHostAddressOwned = hostOwnsDNSPairAddress

func hostOwnedDNSPairAddresses() ([]string, error) {
	listenAddresses, err := publicListenAddresses(context.Background())
	if err != nil {
		return nil, fmt.Errorf("discover DNS pair host addresses: %w", err)
	}
	var addresses []string
	seen := map[string]struct{}{}
	for _, candidate := range listenAddresses {
		address := strings.TrimSpace(candidate)
		parsed := net.ParseIP(address)
		if parsed == nil || parsed.To4() == nil ||
			parsed.To4().String() != address || !parsed.IsGlobalUnicast() {
			continue
		}
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	if len(addresses) == 0 {
		return nil, errors.New("no usable global unicast IPv4 address is available for DNS pairing")
	}
	return addresses, nil
}

func hostOwnsDNSPairAddress(address string) (bool, error) {
	expected := net.ParseIP(address)
	if expected == nil || expected.To4() == nil ||
		expected.To4().String() != address || !expected.IsGlobalUnicast() {
		return false, nil
	}
	addresses, err := dnsPairHostOwnedAddresses()
	if err != nil {
		return false, err
	}
	for _, candidate := range addresses {
		actual := net.ParseIP(candidate)
		if actual != nil && actual.Equal(expected) {
			return true, nil
		}
	}
	return false, nil
}

func requireHostOwnedDNSPairAddress(address string) error {
	owned, err := dnsPairHostAddressOwned(address)
	if err != nil {
		return fmt.Errorf("discover DNS pair host identity: %w", err)
	}
	if !owned {
		return errors.New("DNS pair local address is not owned by this host")
	}
	return nil
}

func verifyBINDPairingAuthority(
	ctx context.Context,
	receipt binddns.Receipt,
) error {
	pairing := receipt.Pairing
	if pairing == nil {
		return nil
	}
	if err := requireHostOwnedDNSPairAddress(pairing.LocalIP); err != nil {
		return err
	}
	return verifyBINDPairingAuthorityAt(
		ctx, receipt, pairing.LocalIP, probeDNSZoneSOA, probeDNSCatalogAXFR,
	)
}

func verifyBINDPairingAuthorityAt(
	ctx context.Context,
	receipt binddns.Receipt,
	localAddress string,
	probe dnsZoneSOAProbe,
	axfr dnsCatalogAXFRProbe,
) error {
	pairing := receipt.Pairing
	if pairing == nil {
		return nil
	}
	if pairing.Role == binddns.PairRolePrimary {
		catalog, err := axfr(ctx, localAddress, pairing.LocalCatalog)
		if err != nil {
			return err
		}
		if catalog.Serial != pairing.CatalogSerial {
			return errors.New("BIND primary catalog serial differs from its receipt")
		}
		expected := make([]string, 0, len(receipt.Zones))
		for _, zone := range receipt.Zones {
			if !zone.Delete {
				expected = append(expected, zone.Domain)
			}
		}
		sort.Strings(expected)
		if strings.Join(expected, "\x00") != strings.Join(catalog.Members, "\x00") {
			return errors.New("BIND primary catalog members differ from its receipt")
		}
		if err := waitForExactBINDPairZoneSet(
			ctx, localAddress, pairing.PeerIP, expected, probe,
		); err != nil {
			return errors.New("BIND primary zones did not converge on the paired peer")
		}
		return nil
	}
	if pairing.Role != binddns.PairRoleSecondary {
		return errors.New("BIND pair receipt has an unknown role")
	}
	peerCatalog, err := axfr(ctx, pairing.PeerIP, pairing.PeerCatalog)
	if err != nil {
		return errors.New("BIND peer catalog is not available")
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	for {
		localSerial, localErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, localAddress, pairing.PeerCatalog, probe,
		)
		if localErr == nil && localSerial == peerCatalog.Serial {
			if err := waitForExactBINDPairZoneSet(
				proofCtx, localAddress, pairing.PeerIP, peerCatalog.Members, probe,
			); err != nil {
				return errors.New("BIND secondary did not receive every exact peer zone")
			}
			return nil
		}
		select {
		case <-proofCtx.Done():
			return errors.New("BIND secondary did not receive the exact peer catalog")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func waitForExactBINDPairZoneSet(
	ctx context.Context,
	localAddress, peerAddress string,
	members []string,
	probe dnsZoneSOAProbe,
) error {
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	for {
		exact := true
		for _, member := range members {
			peerSerial, peerErr := exactDNSZoneSerialAtWithProbe(
				proofCtx, peerAddress, member, probe,
			)
			localSerial, localErr := exactDNSZoneSerialAtWithProbe(
				proofCtx, localAddress, member, probe,
			)
			if peerErr != nil || localErr != nil || peerSerial != localSerial {
				exact = false
				break
			}
		}
		if exact {
			return nil
		}
		select {
		case <-proofCtx.Done():
			return proofCtx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func verifyPDNSPairingAuthority(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if manifest.Topology != transport.DNSTopologyPaired {
		return nil
	}
	if err := requireHostOwnedDNSPairAddress(manifest.LocalIP); err != nil {
		return err
	}
	localAddress := manifest.LocalIP
	if manifest.PairRole == transport.DNSPairRolePrimary {
		domain, err := binddns.CatalogDomain(manifest.LocalIP)
		if err != nil {
			return err
		}
		catalog, err := probeDNSCatalogAXFR(ctx, localAddress, domain)
		if err != nil {
			return errors.New("PowerDNS primary catalog is unavailable")
		}
		expected := make([]string, 0, len(manifest.Zones))
		for _, zone := range manifest.Zones {
			if !zone.Delete {
				expected = append(expected, zone.Domain)
			}
		}
		sort.Strings(expected)
		if strings.Join(expected, "\x00") != strings.Join(catalog.Members, "\x00") {
			return errors.New("PowerDNS primary catalog members differ from the switch manifest")
		}
		return waitForExactBINDPairZoneSet(
			ctx, localAddress, manifest.PeerIP, expected, probeDNSZoneSOA,
		)
	}
	if manifest.PairRole != transport.DNSPairRoleSecondary {
		return errors.New("PowerDNS pair role is invalid")
	}
	catalog, domain, err := peerPDNSCatalog(ctx, manifest)
	if err != nil {
		return err
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	for {
		serial, localErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, localAddress, domain, probeDNSZoneSOA,
		)
		if localErr == nil && serial == catalog.Serial {
			return waitForExactBINDPairZoneSet(
				proofCtx, localAddress, manifest.PeerIP,
				catalog.Members, probeDNSZoneSOA,
			)
		}
		select {
		case <-proofCtx.Done():
			return errors.New("PowerDNS secondary did not receive the exact peer catalog")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

type dnsSOAProbeResult struct {
	Authoritative      bool
	RCode              int
	SOASerials         []uint32
	AnswerSOAOwners    []string
	AuthoritySOAOwners []string
}

type dnsZoneSOAProbe func(
	context.Context, string, string, string,
) (dnsSOAProbeResult, error)

var probeDNSZoneSOA dnsZoneSOAProbe = queryDNSZoneSOA

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
	addresses, err := publicListenAddresses(ctx)
	if err != nil {
		return fmt.Errorf("discover authoritative DNS verification address: %w", err)
	}
	address := strings.TrimSpace(addresses[0])
	if ip := net.ParseIP(address); ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return errors.New("authoritative DNS verification selected an unsafe address")
	}
	return verifyDNSZoneAuthoritiesAt(ctx, address, expected, probeDNSZoneSOA)
}

func verifyDNSZoneAuthoritiesAt(
	ctx context.Context,
	address string,
	expected []expectedDNSZoneAuthority,
	probe dnsZoneSOAProbe,
) error {
	if probe == nil {
		return errors.New("authoritative DNS probe is unavailable")
	}
	for _, zone := range expected {
		for _, network := range []string{"udp", "tcp"} {
			probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
			result, err := probe(probeCtx, network, address, zone.Domain)
			cancel()
			if err != nil {
				return fmt.Errorf("verify %s SOA for %s: %w", network, zone.Domain, err)
			}
			if zone.Delete {
				if !validDeletedDNSZoneProof(zone.Domain, result) {
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

func validDeletedDNSZoneProof(domain string, result dnsSOAProbeResult) bool {
	if len(result.SOASerials) != 0 || len(result.AnswerSOAOwners) != 0 {
		return false
	}
	if !result.Authoritative {
		return result.RCode == dnsRCodeNameError || result.RCode == dnsRCodeRefused
	}
	if result.RCode != dnsRCodeNameError && result.RCode != dnsRCodeNoError {
		return false
	}
	return len(result.AuthoritySOAOwners) == 1 &&
		strictDNSAncestor(result.AuthoritySOAOwners[0], domain)
}

func strictDNSAncestor(parent, child string) bool {
	return parent != child && strings.HasSuffix(child, "."+parent)
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
	if !serviceMutationCanonicalFQDN(domain) {
		return dnsSOAProbeResult{}, errors.New("DNS response query domain is not canonical")
	}
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
	authorities := int(binary.BigEndian.Uint16(message[8:10]))
	additionals := int(binary.BigEndian.Uint16(message[10:12]))
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
	parseRecords := func(count, section int) error {
		for range count {
			name, next, err := decodeDNSName(message, offset)
			if err != nil || next+10 > len(message) {
				return errors.New("DNS response has an invalid resource record")
			}
			recordType := binary.BigEndian.Uint16(message[next : next+2])
			recordClass := binary.BigEndian.Uint16(message[next+2 : next+4])
			rdataLength := int(binary.BigEndian.Uint16(message[next+8 : next+10]))
			rdataOffset := next + 10
			end := rdataOffset + rdataLength
			if end < rdataOffset || end > len(message) {
				return errors.New("DNS response resource record exceeds its packet")
			}
			if recordType == dnsTypeSOA && recordClass == dnsClassIN {
				owner := strings.ToLower(strings.TrimSuffix(name, "."))
				if !serviceMutationCanonicalFQDN(owner) {
					return errors.New("DNS SOA owner is not canonical")
				}
				_, serialOffset, err := decodeDNSName(message, rdataOffset)
				if err != nil {
					return errors.New("DNS SOA primary name is invalid")
				}
				_, serialOffset, err = decodeDNSName(message, serialOffset)
				if err != nil || serialOffset+20 != end {
					return errors.New("DNS SOA payload is invalid")
				}
				switch section {
				case 0:
					result.AnswerSOAOwners = append(result.AnswerSOAOwners, owner)
					if owner == domain {
						result.SOASerials = append(
							result.SOASerials,
							binary.BigEndian.Uint32(message[serialOffset:serialOffset+4]),
						)
					}
				case 1:
					result.AuthoritySOAOwners = append(result.AuthoritySOAOwners, owner)
				}
			}
			offset = end
		}
		return nil
	}
	if err := parseRecords(answers, 0); err != nil {
		return dnsSOAProbeResult{}, err
	}
	if err := parseRecords(authorities, 1); err != nil {
		return dnsSOAProbeResult{}, err
	}
	if err := parseRecords(additionals, 2); err != nil {
		return dnsSOAProbeResult{}, err
	}
	if offset != len(message) {
		return dnsSOAProbeResult{}, errors.New("DNS response contains trailing bytes")
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
