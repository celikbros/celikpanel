package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const legacyPowerDNSGuardTimeout = 15 * time.Second

var (
	legacyPowerDNSDurableAuthorityCheck  = inspectLegacyPowerDNSDurableAuthorityOnHost
	legacyPowerDNSMutationAuthorityCheck = inspectLegacyPowerDNSMutationAuthorityOnHost
	legacyPowerDNSRuntimeSafetyCheck     = inspectLegacyPowerDNSRuntimeSafety
	dnsPort53ConflictCheck               = inspectDNSPort53Conflict
)

func validateLegacyPowerDNSDurableAuthority(
	state dnsEngineStateReceipt,
	stateExists, journalExists, requireResolved bool,
) error {
	if journalExists {
		return errors.New("a DNS engine switch transaction is active")
	}
	if stateExists {
		if err := validateDNSEngineState(state); err != nil {
			return errors.New("the durable DNS engine state is invalid")
		}
		if state.Engine != transport.DNSEnginePowerDNS {
			return errors.New("the durable DNS engine state does not authorize PowerDNS")
		}
		return nil
	}
	if requireResolved {
		return errors.New("the durable DNS engine state is unresolved")
	}
	return nil
}

func validateLegacyPowerDNSMutationAuthority(
	state dnsEngineStateReceipt,
	stateExists, journalExists, requireResolved bool,
) error {
	if err := validateLegacyPowerDNSDurableAuthority(
		state, stateExists, journalExists, requireResolved,
	); err != nil {
		return err
	}
	if stateExists {
		if state.PairRole != "" || state.PairLocalIP != "" ||
			state.PairPeerIP != "" || state.PrimaryCatalogSerial != 0 {
			return errors.New("directional PowerDNS state requires V3 mutation authority")
		}
		return requireDNSPanelWriteAuthority(state)
	}
	return nil
}

func validateLegacyPowerDNSUnitStates(
	named, bindAlias, pdns dnsUnitState,
	requireActive bool,
) (bool, error) {
	if named.active() || bindAlias.active() {
		return false, errors.New("BIND is active")
	}
	if pdns.LoadState == "not-found" {
		return false, errors.New("PowerDNS is not installed")
	}
	if pdns.active() {
		return true, nil
	}
	if requireActive {
		return false, errors.New("PowerDNS is not active")
	}
	return false, nil
}

// requireLegacyPowerDNSMutationSafe prevents older configuration RPCs from
// bypassing the engine switch transaction. It is intentionally read-only.
func requireLegacyPowerDNSMutationSafe(
	parent context.Context,
	requireActive bool,
) error {
	return requireLegacyPowerDNSMutationSafeWithAuthority(
		parent, requireActive, false,
	)
}

// requireLegacyPowerDNSReadSafe proves that PowerDNS is the durable engine
// and the sole compatible runtime without requiring panel-local write
// authority. A paired secondary remains a managed, readable backend even
// though every legacy mutation endpoint must reject it.
func requireLegacyPowerDNSReadSafe(
	parent context.Context,
	requireActive bool,
) error {
	if err := legacyPowerDNSDurableAuthorityCheck(false); err != nil {
		return err
	}
	return legacyPowerDNSRuntimeSafetyCheck(parent, requireActive)
}

func requireLegacyPowerDNSMutationSafeWithAuthority(
	parent context.Context,
	requireActive, requireResolved bool,
) error {
	if err := legacyPowerDNSMutationAuthorityCheck(requireResolved); err != nil {
		return err
	}
	return legacyPowerDNSRuntimeSafetyCheck(parent, requireActive)
}

func inspectLegacyPowerDNSRuntimeSafety(
	parent context.Context,
	requireActive bool,
) error {
	ctx, cancel := context.WithTimeout(parent, legacyPowerDNSGuardTimeout)
	defer cancel()
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	named, err := captureDNSUnitState(ctx, systemctl, "named.service")
	if err != nil {
		return err
	}
	bindAlias, err := captureDNSUnitState(ctx, systemctl, "bind9.service")
	if err != nil {
		return err
	}
	pdns, err := captureDNSUnitState(ctx, systemctl, "pdns.service")
	if err != nil {
		return err
	}
	active, err := validateLegacyPowerDNSUnitStates(named, bindAlias, pdns, requireActive)
	if err != nil {
		return err
	}
	if active {
		return verifyOnlyPDNSActive(ctx, systemctl)
	}
	conflict, err := dnsPort53ConflictCheck(ctx, false, false)
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("another public port-53 authority is active")
	}
	return nil
}

func rejectLegacyPublicDNSListeners(output string) error {
	conflict := hasUnrelatedPublicDNSListener(output, false, false)
	if conflict {
		return errors.New("another public port-53 authority is active")
	}
	return nil
}

func inspectDNSPort53Conflict(
	parent context.Context,
	allowBIND, allowPowerDNS bool,
) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, legacyPowerDNSGuardTimeout)
	defer cancel()
	ss, err := firstTrustedExecutable([]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss")
	if err != nil {
		return false, err
	}
	output, err := serviceMutationCommand(
		ctx, ss, "-H", "-lntup", "sport = :53",
	).CombinedOutputLimited(64 << 10)
	if err != nil {
		return false, err
	}
	return hasUnrelatedPublicDNSListener(
		string(output), allowBIND, allowPowerDNS,
	), nil
}

func hasUnrelatedPublicDNSListener(
	output string,
	allowBIND, allowPowerDNS bool,
) bool {
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		row, err := parseCanonicalDNSPort53ListenerRow(line)
		if err != nil {
			return true
		}
		if row.address.IsLoopback() || row.address.IsLinkLocalUnicast() {
			continue
		}
		bindOwner := row.process == "named"
		pdnsOwner := row.process == "pdns_server"
		if bindOwner == pdnsOwner ||
			(bindOwner && !allowBIND) ||
			(pdnsOwner && !allowPowerDNS) {
			return true
		}
	}
	return false
}

type canonicalDNSPort53ListenerRow struct {
	protocol string
	address  net.IP
	process  string
	pid      uint64
}

func parseCanonicalDNSPort53ListenerRow(
	line string,
) (canonicalDNSPort53ListenerRow, error) {
	fields := strings.Fields(line)
	if len(fields) != 7 {
		return canonicalDNSPort53ListenerRow{},
			errors.New("ss returned a malformed public DNS listener row")
	}
	protocol := fields[0]
	if (protocol != "tcp" && protocol != "udp") ||
		(protocol == "tcp" && fields[1] != "LISTEN") ||
		(protocol == "udp" && fields[1] != "UNCONN") {
		return canonicalDNSPort53ListenerRow{},
			errors.New("ss returned a non-canonical DNS listener protocol or state")
	}
	for _, queue := range fields[2:4] {
		value, err := strconv.ParseUint(queue, 10, 64)
		if err != nil || strconv.FormatUint(value, 10) != queue {
			return canonicalDNSPort53ListenerRow{},
				errors.New("ss returned a non-canonical DNS listener queue")
		}
	}
	address, port, ok := parseCanonicalSSHostPort(fields[4], true)
	if !ok || port != "53" {
		return canonicalDNSPort53ListenerRow{},
			errors.New("ss returned a non-canonical public DNS listener endpoint")
	}
	_, peerPort, ok := parseCanonicalSSHostPort(fields[5], false)
	if !ok || peerPort != "*" {
		return canonicalDNSPort53ListenerRow{},
			errors.New("ss returned a non-canonical DNS listener peer endpoint")
	}
	process, pid, err := parseCanonicalSSProcessField(fields[6])
	if err != nil {
		return canonicalDNSPort53ListenerRow{}, err
	}
	return canonicalDNSPort53ListenerRow{
		protocol: protocol, address: address, process: process, pid: pid,
	}, nil
}

func parseCanonicalSSHostPort(
	endpoint string,
	allowScopedLocal bool,
) (net.IP, string, bool) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		var ok bool
		host, port, ok = splitCanonicalSSScopedIPv6HostPort(endpoint)
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
func splitCanonicalSSScopedIPv6HostPort(endpoint string) (string, string, bool) {
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

func parseCanonicalSSProcessField(field string) (string, uint64, error) {
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

func runDNSPort53PreMutationGuard(
	ctx context.Context,
	requireEmptyAuthority bool,
	mutation func() error,
) error {
	if mutation == nil {
		return errors.New("DNS engine mutation callback is required")
	}
	if requireEmptyAuthority {
		conflict, err := dnsPort53ConflictCheck(ctx, false, false)
		if err != nil {
			return err
		}
		if conflict {
			return errors.New("another public port-53 authority is active")
		}
	}
	return mutation()
}
