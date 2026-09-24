package dnslistener

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

type Row struct {
	Protocol string
	Address  net.IP
	Process  string
	PID      uint64
}

func ParseRow(
	line string,
) (Row, error) {
	fields := strings.Fields(line)
	if len(fields) != 7 {
		return Row{},
			errors.New("ss returned a malformed public DNS listener row")
	}
	protocol := fields[0]
	if (protocol != "tcp" && protocol != "udp") ||
		(protocol == "tcp" && fields[1] != "LISTEN") ||
		(protocol == "udp" && fields[1] != "UNCONN") {
		return Row{},
			errors.New("ss returned a non-canonical DNS listener protocol or state")
	}
	for _, queue := range fields[2:4] {
		value, err := strconv.ParseUint(queue, 10, 64)
		if err != nil || strconv.FormatUint(value, 10) != queue {
			return Row{},
				errors.New("ss returned a non-canonical DNS listener queue")
		}
	}
	address, port, ok := ParseCanonicalSSHostPort(fields[4], true)
	if !ok || port != "53" {
		return Row{},
			errors.New("ss returned a non-canonical public DNS listener endpoint")
	}
	if !CanonicalWildcardPeerEndpoint(fields[5]) {
		return Row{},
			errors.New("ss returned a non-canonical DNS listener peer endpoint")
	}
	process, pid, err := ParseCanonicalSSProcessField(fields[6])
	if err != nil {
		return Row{}, err
	}
	return Row{
		Protocol: protocol, Address: address, Process: process, PID: pid,
	}, nil
}

// CanonicalWildcardPeerEndpoint reports whether ss rendered the peer column
// of a listening socket, which is the only thing this proof accepts: a
// listener has no peer, a connected socket does. iproute2 renders that empty
// peer with the same formatter it uses for the local address, so its spelling
// follows the socket family and the IPV6_V6ONLY flag the kernel reports
// through INET_DIAG_SKV6ONLY. An IPv4 socket prints "0.0.0.0:*", an
// IPv6-only socket prints "[::]:*", and an IPv6 socket that also accepts IPv4
// - or one whose v6only flag the kernel does not report - prints "*:*". All
// three mean "no peer" and are equally canonical; every other spelling,
// including a real remote address such as "192.0.2.7:53535", is refused.
func CanonicalWildcardPeerEndpoint(endpoint string) bool {
	switch endpoint {
	case "*:*", "0.0.0.0:*", "[::]:*":
		return true
	default:
		return false
	}
}

func ParseCanonicalSSHostPort(
	endpoint string,
	allowScopedLocal bool,
) (net.IP, string, bool) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		var ok bool
		host, port, ok = SplitCanonicalSSScopedIPv6HostPort(endpoint)
		if !ok {
			return nil, "", false
		}
	}
	if port == "" || strings.Count(host, "%") > 1 {
		return nil, "", false
	}
	addressText := host
	hasZone := false
	if zoneAt := strings.IndexByte(host, '%'); zoneAt >= 0 {
		hasZone = true
		addressText = host[:zoneAt]
		if !validLinuxInterfaceName(host[zoneAt+1:]) {
			return nil, "", false
		}
	}
	address := net.ParseIP(addressText)
	if address == nil {
		return nil, "", false
	}
	if hasZone && (!allowScopedLocal || (address.To4() != nil && !address.IsLoopback())) {
		return nil, "", false
	}
	return address, port, true
}

// iproute2 brackets a numeric IPv6 address before appending an interface name,
// so a scoped listener is rendered as "[fe80::1]%eth0:53". That is canonical
// ss output, but it is intentionally not RFC 3986 host:port syntax and is
// therefore rejected by net.SplitHostPort. Accept only that exact fallback
// grammar; ordinary endpoints continue through net.SplitHostPort above.
func SplitCanonicalSSScopedIPv6HostPort(endpoint string) (string, string, bool) {
	const scopeMarker = "]%"
	if !strings.HasPrefix(endpoint, "[") ||
		strings.Count(endpoint, "[") != 1 || strings.Count(endpoint, "]") != 1 {
		return "", "", false
	}
	closing := strings.Index(endpoint, scopeMarker)
	if closing <= 1 {
		return "", "", false
	}
	addressText := endpoint[1:closing]
	address := net.ParseIP(addressText)
	if address == nil || address.To4() != nil {
		return "", "", false
	}
	scopeAndPort := endpoint[closing+len(scopeMarker):]
	scope, port, found := strings.Cut(scopeAndPort, ":")
	if !found || !validLinuxInterfaceName(scope) || port == "" {
		return "", "", false
	}
	return addressText + "%" + scope, port, true
}

func ParseCanonicalSSProcessField(field string) (string, uint64, error) {
	const prefix = `users:(("`
	if !strings.HasPrefix(field, prefix) || !strings.HasSuffix(field, "))") ||
		strings.Count(field, "pid=") != 1 || strings.Count(field, "fd=") != 1 {
		return "", 0, errors.New("ss returned a non-canonical DNS listener process")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(field, prefix), "))")
	process, identity, found := strings.Cut(body, `",pid=`)
	if !found || process == "" ||
		strings.ContainsAny(process, "\x00\r\n\t ,()\"") {
		return "", 0, errors.New("ss returned a non-canonical DNS listener process")
	}
	pidText, fdText, found := strings.Cut(identity, ",fd=")
	if !found || pidText == "" || fdText == "" {
		return "", 0, errors.New("ss returned a non-canonical DNS listener process")
	}
	pid, pidErr := strconv.ParseUint(pidText, 10, 64)
	fd, fdErr := strconv.ParseUint(fdText, 10, 64)
	if pidErr != nil || pid == 0 || strconv.FormatUint(pid, 10) != pidText ||
		fdErr != nil || strconv.FormatUint(fd, 10) != fdText {
		return "", 0, errors.New("ss returned a non-canonical DNS listener process")
	}
	return process, pid, nil
}

func CanonicalPublicListeners(
	output string,
	expectedProcess string,
	expectedMainPID uint64,
) ([]string, error) {
	if expectedProcess == "" ||
		strings.ContainsAny(expectedProcess, "\x00\r\n\t ,()\"") ||
		expectedMainPID == 0 {
		return nil, errors.New("invalid DNS authority process identity")
	}
	foundTCP, foundUDP := false, false
	identities := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		row, err := ParseRow(line)
		if err != nil {
			return nil, err
		}
		if row.Address.IsLoopback() || row.Address.IsLinkLocalUnicast() {
			continue
		}
		if row.Process != expectedProcess {
			return nil, errors.New("an unexpected process is holding a public DNS listener")
		}
		if row.PID != expectedMainPID {
			return nil, errors.New("a DNS authority listener PID differs from its systemd MainPID")
		}
		identities[fmt.Sprintf(
			"%s|%s|%d", row.Protocol, row.Address.String(), row.PID,
		)] = struct{}{}
		if row.Protocol == "tcp" {
			foundTCP = true
		} else {
			foundUDP = true
		}
	}
	if !foundTCP || !foundUDP {
		return nil, errors.New("the DNS authority does not own both public TCP and UDP port 53 listeners")
	}
	result := make([]string, 0, len(identities))
	for identity := range identities {
		result = append(result, identity)
	}
	sort.Strings(result)
	return result, nil
}

// HasIPv4Listener reports whether the verified listener inventory contains
// both transports on the selected local IPv4 address or the IPv4 wildcard.
func HasIPv4Listener(identities []string, address string, pid uint64) bool {
	ip := net.ParseIP(address)
	if ip == nil || ip.To4() == nil || ip.To4().String() != address || pid == 0 {
		return false
	}
	for _, protocol := range []string{"tcp", "udp"} {
		exact := fmt.Sprintf("%s|%s|%d", protocol, address, pid)
		wildcard := fmt.Sprintf("%s|0.0.0.0|%d", protocol, pid)
		if !containsIdentity(identities, exact) && !containsIdentity(identities, wildcard) {
			return false
		}
	}
	return true
}

func containsIdentity(identities []string, target string) bool {
	for _, identity := range identities {
		if identity == target {
			return true
		}
	}
	return false
}
func validLinuxInterfaceName(value string) bool {
	if len(value) == 0 || len(value) > 15 || value == "." || value == ".." {
		return false
	}
	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9':
		case index > 0 && (char == '_' || char == '-' || char == '.'):
		default:
			return false
		}
	}
	return true
}
