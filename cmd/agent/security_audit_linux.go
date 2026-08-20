//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

const (
	securityAuditReleasePublicKey = "/etc/celikpanel/release-signing-ed25519.pem"
	securityAuditFirewallSnapshot = "/etc/celikpanel/firewall.nft"
	securityAuditConfigurationDir = "/etc/celikpanel"
	securityAuditReleaseStateDir  = "/var/lib/celikpanel-release-state"
	securityAuditReleaseFloor     = "/var/lib/celikpanel-release-state/sequence.floor"
	securityAuditReleaseLock      = "/var/lib/celikpanel-release-state/update.lock"
	securityAuditRebootMarker     = "/var/run/reboot-required"
	securityAuditSSHDConfig       = "/etc/ssh/sshd_config"
	securityAuditProbeTimeout     = 20 * time.Second
	securityAuditCommandOutputMax = 2 << 20
	securityAuditSSHDFileMax      = 256 << 10
	securityAuditSSHDTotalMax     = 1 << 20
	securityAuditSSHDFileCountMax = 64
	securityAuditSSHDIncludeDepth = 8
)

var securityAuditReleaseVersionPattern = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

type securityAuditSocket struct {
	protocol string
	port     int
}

type securityAuditCommandRunner struct {
	ctx context.Context
}

func (securityAuditCommandRunner) LookPath(file string) (string, error) {
	return trustedCommandExecutablePath(file)
}

func (runner securityAuditCommandRunner) Output(name string, args ...string) ([]byte, error) {
	path, err := trustedCommandExecutablePath(name)
	if err != nil {
		return nil, err
	}
	return runBoundedSecurityAuditCommand(runner.ctx, path, args...)
}

func runBoundedSecurityAuditCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return serviceMutationCommand(ctx, path, args...).CombinedOutputLimited(securityAuditCommandOutputMax)
}

type securityAuditFirewallInspection struct {
	engineReady bool
	engineKnown bool
	live        bool
	liveKnown   bool
	defaultDrop bool
	policyKnown bool
	allowKnown  bool
	tcp         []int
	udp         []int
	persistence string
}

func collectHostSecurityAudit(_ time.Time) transport.SecurityAuditAgentResponse {
	response := unknownSecurityAuditResponse("platform_unsupported")
	collectFirewallAndListenerAudit(&response)
	response.SSH = collectSSHSecurityAudit()
	response.Reboot = collectRebootSecurityAudit()
	response.SignedUpdate = collectSignedUpdateSecurityAudit()
	return response
}

func collectFirewallAndListenerAudit(response *transport.SecurityAuditAgentResponse) {
	inspection, listeners, listenersErr := inspectSecurityAuditFirewallAndListeners()
	classifyFirewallAndListenerAudit(response, inspection, listeners, listenersErr)
}

func classifyFirewallAndListenerAudit(
	response *transport.SecurityAuditAgentResponse,
	inspection securityAuditFirewallInspection,
	listeners []securityAuditSocket,
	listenersErr error,
) {
	response.Firewall.TCPAllowlist = append([]int(nil), inspection.tcp...)
	response.Firewall.UDPAllowlist = append([]int(nil), inspection.udp...)

	switch {
	case !inspection.engineKnown:
		response.Firewall.Engine = securityAuditCheck(transport.SecurityAuditStatusUnknown, "firewall_state_unreadable")
	case !inspection.engineReady:
		response.Firewall.Engine = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_engine_unavailable")
	default:
		response.Firewall.Engine = securityAuditCheck(transport.SecurityAuditStatusPass, "firewall_engine_available")
	}

	switch {
	case !inspection.liveKnown:
		response.Firewall.DefaultDrop = securityAuditCheck(transport.SecurityAuditStatusUnknown, "firewall_state_unreadable")
	case !inspection.live:
		response.Firewall.DefaultDrop = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_disabled")
	case !inspection.policyKnown:
		response.Firewall.DefaultDrop = securityAuditCheck(transport.SecurityAuditStatusUnknown, "firewall_policy_ambiguous")
	case inspection.defaultDrop:
		response.Firewall.DefaultDrop = securityAuditCheck(transport.SecurityAuditStatusPass, "firewall_policy_drop")
	default:
		response.Firewall.DefaultDrop = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_policy_not_drop")
	}

	switch inspection.persistence {
	case firewallPersistenceReady:
		// Snapshot/live equality and an enabled unit do not prove the exact
		// restore command, its current SSH-port discovery, or nft preflight.
		// Phase 1 therefore never promotes persistence to PASS.
		response.Firewall.Persistence = securityAuditCheck(transport.SecurityAuditStatusUnknown, "firewall_persistence_unverified")
	case firewallPersistenceMissing:
		response.Firewall.Persistence = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_persistence_missing")
	case firewallPersistenceStale:
		response.Firewall.Persistence = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_persistence_stale")
	case firewallPersistenceInvalid:
		response.Firewall.Persistence = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_persistence_invalid")
	case firewallPersistenceDisabled:
		response.Firewall.Persistence = securityAuditCheck(transport.SecurityAuditStatusFail, "firewall_persistence_missing")
	default:
		response.Firewall.Persistence = securityAuditCheck(transport.SecurityAuditStatusUnknown, "firewall_persistence_unverified")
	}

	if response.Firewall.DefaultDrop.Status != transport.SecurityAuditStatusPass || !inspection.allowKnown {
		response.Listeners = transport.SecurityAuditListenersResponse{
			Check:    securityAuditCheck(transport.SecurityAuditStatusUnknown, "listener_state_ambiguous"),
			Findings: []transport.SecurityAuditListenerFinding{},
		}
		return
	}
	if listenersErr != nil {
		response.Listeners = transport.SecurityAuditListenersResponse{
			Check:    securityAuditCheck(transport.SecurityAuditStatusUnknown, "listener_state_unreadable"),
			Findings: []transport.SecurityAuditListenerFinding{},
		}
		return
	}
	response.Listeners = compareSecurityAuditListeners(listeners, inspection.tcp, inspection.udp)
}

func inspectSecurityAuditFirewallAndListeners() (result securityAuditFirewallInspection, listeners []securityAuditSocket, listenersErr error) {
	result.persistence = firewallPersistenceUnverified
	ctx, cancel := context.WithTimeout(context.Background(), securityAuditProbeTimeout)
	defer cancel()
	runner := securityAuditCommandRunner{ctx: ctx}

	firewallMu.Lock()
	defer firewallMu.Unlock()
	defer func() {
		result.persistence = inspectSecurityAuditFirewallSnapshot(
			firewallSnapshotPath, result,
		)
		if result.persistence == firewallPersistenceReady {
			unitOutput, unitErr := runner.Output("systemctl", "is-enabled", firewallRestoreUnitName)
			result.persistence = classifySecurityAuditFirewallPersistence(
				result.persistence, strings.TrimSpace(string(unitOutput)), unitErr == nil,
			)
		}
	}()

	if _, err := runner.LookPath("nft"); err != nil {
		result.engineKnown = true
		return
	}
	tables, err := runner.Output("nft", "list", "tables")
	if err != nil {
		return
	}
	result.engineKnown = true
	result.engineReady = true
	result.liveKnown = true
	result.live = firewallTablePresent(tables)
	if !result.live {
		return
	}
	rules, err := runner.Output("nft", "list", "table", "inet", fwTable)
	if err != nil {
		result.liveKnown = false
		return
	}
	result.defaultDrop, result.policyKnown, result.tcp, result.udp, result.allowKnown = parseSecurityAuditFirewallRules(rules)
	if !result.defaultDrop || !result.policyKnown || !result.allowKnown {
		result.tcp = []int{}
		result.udp = []int{}
		return
	}
	listeners, listenersErr = readSecurityAuditPublicListenersWithRunner(runner)
	return
}

func inspectSecurityAuditFirewallSnapshot(path string, inspection securityAuditFirewallInspection) string {
	// The installer deliberately makes /etc/celikpanel root:celikpanel 0750,
	// and the root agent runs with celikpanel as its service group. Files the
	// agent creates there are therefore root:celikpanel, not root:root. Keep
	// that one exception exact: no caller-selected path, no arbitrary group,
	// and no relaxation for /etc or any other ancestor.
	if !securityAuditFirewallSnapshotPathSafe(path) || serviceMutationRequiredOwnerGID == 0 {
		return classifySecurityAuditFirewallSnapshot(nil, securityAuditObjectUnsafe, inspection)
	}
	state := inspectSecurityAuditFirewallDirectoryChain(filepath.Dir(path), serviceMutationRequiredOwnerGID)
	if state != securityAuditObjectOK {
		return classifySecurityAuditFirewallSnapshot(nil, state, inspection)
	}
	raw, state := readSecurityAuditRegularFileWithOwner(
		path, 0o600, 1, maxFirewallSnapshotSize, 0, serviceMutationRequiredOwnerGID,
	)
	return classifySecurityAuditFirewallSnapshot(raw, state, inspection)
}

func securityAuditFirewallSnapshotPathSafe(path string) bool {
	return path == securityAuditFirewallSnapshot
}

func inspectSecurityAuditFirewallDirectoryChain(path string, expectedGID uint32) securityAuditObjectState {
	if path != securityAuditConfigurationDir || expectedGID == 0 {
		return securityAuditObjectUnsafe
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return securityAuditObjectMissing
	}
	if err != nil {
		return securityAuditObjectUnreadable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !securityAuditFirewallDirectoryMetadataSafe(info.Mode(), stat.Uid, stat.Gid, expectedGID) {
		return securityAuditObjectUnsafe
	}
	return inspectSecurityAuditRootDirectoryChain(filepath.Dir(path))
}

func securityAuditFirewallDirectoryMetadataSafe(mode os.FileMode, uid, gid, expectedGID uint32) bool {
	return expectedGID != 0 && mode.IsDir() && mode&os.ModeSymlink == 0 &&
		mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 &&
		mode.Perm() == 0o750 && uid == 0 && gid == expectedGID
}

func classifySecurityAuditFirewallSnapshot(
	raw []byte,
	state securityAuditObjectState,
	inspection securityAuditFirewallInspection,
) string {
	switch state {
	case securityAuditObjectMissing:
		if !inspection.liveKnown {
			return firewallPersistenceUnverified
		}
		if inspection.live {
			return firewallPersistenceMissing
		}
		return firewallPersistenceDisabled
	case securityAuditObjectUnsafe:
		return firewallPersistenceInvalid
	case securityAuditObjectUnreadable:
		return firewallPersistenceUnverified
	}
	policy, legacy, err := decodeFirewallSnapshot(raw)
	if err != nil {
		return firewallPersistenceInvalid
	}
	if !inspection.liveKnown || (inspection.live &&
		(!inspection.policyKnown || !inspection.defaultDrop || !inspection.allowKnown)) {
		return firewallPersistenceUnverified
	}
	if !inspection.live {
		return firewallPersistenceStale
	}
	expectedTCP := append([]int(nil), policy.TCPPorts...)
	if !legacy {
		expectedTCP = append(expectedTCP, policy.SSHPortsAtSave...)
	}
	expectedTCP = dedupeSorted(expectedTCP)
	expectedUDP := dedupeSorted(append([]int(nil), policy.UDPPorts...))
	if !equalSecurityAuditPorts(expectedTCP, inspection.tcp) ||
		!equalSecurityAuditPorts(expectedUDP, inspection.udp) {
		return firewallPersistenceStale
	}
	return firewallPersistenceReady
}

func equalSecurityAuditPorts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func classifySecurityAuditFirewallPersistence(snapshotState, unitState string, unitCommandOK bool) string {
	if snapshotState != firewallPersistenceReady {
		return snapshotState
	}
	if unitCommandOK && unitState == "enabled" {
		return firewallPersistenceReady
	}
	switch unitState {
	case "disabled", "not-found":
		return firewallPersistenceMissing
	case "masked", "masked-runtime":
		return firewallPersistenceInvalid
	default:
		return firewallPersistenceUnverified
	}
}

func parseLegacySecurityAuditFirewallRules(raw []byte) (defaultDrop, policyKnown bool, tcp, udp []int, allowKnown bool) {
	inputPolicies := []string{}
	allowKnown = true
	currentChain := ""
	inputChains := 0
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "chain ") && strings.HasSuffix(line, " {") {
			fields := strings.Fields(line)
			if len(fields) != 3 || fields[2] != "{" || currentChain != "" {
				allowKnown = false
				continue
			}
			currentChain = fields[1]
			if currentChain == "input" {
				inputChains++
			} else {
				allowKnown = false
			}
			continue
		}
		if line == "}" {
			if currentChain != "" {
				currentChain = ""
			}
			continue
		}
		if currentChain != "input" {
			if strings.Contains(line, " accept") || strings.Contains(line, " jump ") || strings.Contains(line, " goto ") {
				allowKnown = false
			}
			continue
		}
		if strings.Contains(line, "hook input") {
			switch {
			case strings.Contains(line, "policy drop"):
				inputPolicies = append(inputPolicies, "drop")
			case strings.Contains(line, "policy accept"):
				inputPolicies = append(inputPolicies, "accept")
			default:
				inputPolicies = append(inputPolicies, "unknown")
			}
		}
		if strings.Contains(line, " jump ") || strings.Contains(line, " goto ") {
			allowKnown = false
		}
		if !strings.HasSuffix(line, " accept") {
			continue
		}
		switch {
		case strings.Contains(line, "tcp dport"):
			ports, ok := parseSecurityAuditPortAccept(line, "tcp dport")
			if !ok {
				allowKnown = false
				continue
			}
			tcp = append(tcp, ports...)
		case strings.Contains(line, "udp dport"):
			ports, ok := parseSecurityAuditPortAccept(line, "udp dport")
			if !ok {
				allowKnown = false
				continue
			}
			udp = append(udp, ports...)
		case strings.Contains(line, "iif") && strings.Contains(line, `"lo"`),
			strings.Contains(line, "ct state established,related"),
			strings.Contains(line, "l4proto icmp"),
			strings.Contains(line, "l4proto ipv6-icmp"),
			strings.Contains(line, "protocol icmp"),
			strings.Contains(line, "nexthdr ipv6-icmp"):
			// Exact non-port accepts emitted by CelikPanel.
		default:
			allowKnown = false
		}
	}
	tcp = dedupeSorted(tcp)
	udp = dedupeSorted(udp)
	if inputChains == 1 && len(inputPolicies) == 1 {
		policyKnown = inputPolicies[0] != "unknown"
		defaultDrop = inputPolicies[0] == "drop"
	}
	return defaultDrop, policyKnown, tcp, udp, allowKnown
}

func parseSecurityAuditFirewallRules(raw []byte) (defaultDrop, policyKnown bool, tcp, udp []int, allowKnown bool) {
	return parseCanonicalSecurityAuditFirewallRules(raw)
}

func parseCanonicalSecurityAuditFirewallRules(raw []byte) (defaultDrop, policyKnown bool, tcp, udp []int, allowKnown bool) {
	actual, err := canonicalFirewallRulesetReadback(raw)
	if err != nil {
		return false, false, []int{}, []int{}, false
	}
	return parseCanonicalSecurityAuditFirewallLines(actual)
}

func parseCanonicalSecurityAuditFirewallLines(actual string) (defaultDrop, policyKnown bool, tcp, udp []int, allowKnown bool) {
	policyCount := 0
	for _, line := range strings.Split(actual, "\n") {
		if line == "type filter hook input priority 0; policy drop;" {
			policyCount++
			defaultDrop = true
		}
		if ports, found, err := parseExactFirewallPortRule(line, "tcp"); found {
			if err != nil || tcp != nil {
				return defaultDrop, false, []int{}, []int{}, false
			}
			tcp = ports
		}
		if ports, found, err := parseExactFirewallPortRule(line, "udp"); found {
			if err != nil || udp != nil {
				return defaultDrop, false, []int{}, []int{}, false
			}
			udp = ports
		}
	}
	policyKnown = policyCount == 1
	expected, err := canonicalFirewallRulesetReadback([]byte(buildFirewallRuleset(false, tcp, udp)))
	if err != nil || actual != expected {
		return defaultDrop, policyKnown, []int{}, []int{}, false
	}
	return true, true, tcp, udp, true
}

func parseSecurityAuditPortAccept(line, prefix string) ([]int, bool) {
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, " accept") {
		return nil, false
	}
	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, prefix), " accept"))
	if strings.HasPrefix(value, "{") || strings.HasSuffix(value, "}") {
		if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
			return nil, false
		}
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	if value == "" {
		return nil, false
	}
	ports := []int{}
	for _, token := range strings.Split(value, ",") {
		port, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || port < 1 || port > 65535 {
			return nil, false
		}
		ports = append(ports, port)
	}
	ports = dedupeSorted(ports)
	return ports, len(ports) > 0
}

func readSecurityAuditPublicListeners() ([]securityAuditSocket, error) {
	ctx, cancel := context.WithTimeout(context.Background(), securityAuditProbeTimeout)
	defer cancel()
	runner := securityAuditCommandRunner{ctx: ctx}
	return readSecurityAuditPublicListenersWithRunner(runner)
}

func readSecurityAuditPublicListenersWithRunner(runner securityAuditCommandRunner) ([]securityAuditSocket, error) {
	raw, err := runner.Output("ss", "-H", "-lntu")
	if err != nil {
		return nil, err
	}
	return parseSecurityAuditPublicListeners(raw)
}

func parseSecurityAuditPublicListeners(raw []byte) ([]securityAuditSocket, error) {
	seen := map[securityAuditSocket]bool{}
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			return nil, fmt.Errorf("listener row is malformed")
		}
		protocol := strings.TrimSuffix(fields[0], "6")
		if protocol != "tcp" && protocol != "udp" {
			return nil, fmt.Errorf("listener protocol is unsupported")
		}
		host, port, err := parseSecurityAuditSocketEndpoint(fields[len(fields)-2])
		if err != nil {
			return nil, err
		}
		if host != "*" {
			withoutZone := host
			if index := strings.LastIndexByte(withoutZone, '%'); index >= 0 {
				withoutZone = withoutZone[:index]
			}
			ip := net.ParseIP(withoutZone)
			if ip == nil {
				return nil, fmt.Errorf("listener address is not numeric")
			}
			if ip.IsLoopback() {
				continue
			}
		}
		seen[securityAuditSocket{protocol: protocol, port: port}] = true
		if len(seen) > transport.SecurityAuditMaxListenerFindings {
			return nil, fmt.Errorf("listener count exceeds the audit limit")
		}
	}
	result := make([]securityAuditSocket, 0, len(seen))
	for socket := range seen {
		result = append(result, socket)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].protocol == result[j].protocol {
			return result[i].port < result[j].port
		}
		return result[i].protocol < result[j].protocol
	})
	return result, nil
}

func parseSecurityAuditSocketEndpoint(endpoint string) (string, int, error) {
	index := strings.LastIndexByte(endpoint, ':')
	if index <= 0 || index == len(endpoint)-1 {
		return "", 0, fmt.Errorf("listener endpoint is malformed")
	}
	host := endpoint[:index]
	port, err := strconv.Atoi(endpoint[index+1:])
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("listener port is invalid")
	}
	if host == "*" || host == "0.0.0.0" || host == "[::]" || host == "::" {
		return "*", port, nil
	}
	if strings.HasPrefix(host, "[") {
		closing := strings.IndexByte(host, ']')
		if closing < 0 {
			return "", 0, fmt.Errorf("listener IPv6 address is malformed")
		}
		host = host[1:closing] + host[closing+1:]
	}
	if host == "" {
		return "", 0, fmt.Errorf("listener address is empty")
	}
	return host, port, nil
}

func compareSecurityAuditListeners(listeners []securityAuditSocket, tcpAllowlist, udpAllowlist []int) transport.SecurityAuditListenersResponse {
	allowed := map[securityAuditSocket]bool{}
	for _, port := range tcpAllowlist {
		allowed[securityAuditSocket{protocol: "tcp", port: port}] = true
	}
	for _, port := range udpAllowlist {
		allowed[securityAuditSocket{protocol: "udp", port: port}] = true
	}
	live := map[securityAuditSocket]bool{}
	findings := []transport.SecurityAuditListenerFinding{}
	for _, socket := range listeners {
		// This comparison is reached only after the effective default-drop
		// policy and its exact allowlist have both been verified. A listener
		// outside that allowlist is blocked at the host boundary and is not a
		// public exposure. Do not infer an owning process from port numbers or
		// special-case LLMNR/5355; the firewall truth applies to every blocked
		// listener. The raw listener count remains bounded by the parser, so a
		// hostile or unreadable socket table still fails closed before here.
		if allowed[socket] {
			live[socket] = true
		}
	}
	for socket := range allowed {
		if !live[socket] {
			findings = append(findings, transport.SecurityAuditListenerFinding{
				Protocol: socket.protocol, Port: socket.port,
				Status: transport.SecurityAuditStatusWarning, Code: transport.SecurityAuditAllowedNoListener,
			})
		}
	}
	if len(findings) > transport.SecurityAuditMaxListenerFindings {
		return transport.SecurityAuditListenersResponse{
			Check:    securityAuditCheck(transport.SecurityAuditStatusUnknown, "finding_limit_exceeded"),
			Findings: []transport.SecurityAuditListenerFinding{},
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Protocol != findings[j].Protocol {
			return findings[i].Protocol < findings[j].Protocol
		}
		if findings[i].Port != findings[j].Port {
			return findings[i].Port < findings[j].Port
		}
		return findings[i].Code < findings[j].Code
	})
	status := transport.SecurityAuditStatusPass
	code := "listeners_match_allowlist"
	if len(findings) != 0 {
		status, code = transport.SecurityAuditStatusWarning, transport.SecurityAuditAllowedNoListener
	}
	return transport.SecurityAuditListenersResponse{
		Check: securityAuditCheck(status, code), Findings: findings,
	}
}

type securityAuditSSHDPolicy struct {
	passwordAuthentication            string
	keyboardInteractiveAuthentication string
	permitRootLogin                   string
	pubkeyAuthentication              string
	hostbasedAuthentication           string
	gssapiAuthentication              string
}

func collectSSHSecurityAudit() transport.SecurityAuditSSHResponse {
	response := transport.SecurityAuditSSHResponse{
		Check:                             securityAuditCheck(transport.SecurityAuditStatusUnknown, "ssh_policy_unreadable"),
		PasswordAuthentication:            "unknown",
		KeyboardInteractiveAuthentication: "unknown",
		PermitRootLogin:                   "unknown",
		PubkeyAuthentication:              "unknown",
		HostbasedAuthentication:           "unknown",
		GSSAPIAuthentication:              "unknown",
	}
	hasMatch, err := inspectSecurityAuditSSHDConfigGraph("/etc/ssh", securityAuditSSHDConfig)
	if err != nil || hasMatch {
		response.Check = securityAuditCheck(transport.SecurityAuditStatusUnknown, "ssh_policy_ambiguous")
		return response
	}
	path, err := trustedSSHDExecutablePath()
	if err != nil {
		return response
	}
	ctx, cancel := context.WithTimeout(context.Background(), securityAuditProbeTimeout)
	defer cancel()
	raw, err := runBoundedSecurityAuditCommand(ctx, path, "-T")
	if err != nil {
		return response
	}
	policy, err := parseSecurityAuditSSHDPolicy(raw)
	if err != nil {
		response.Check = securityAuditCheck(transport.SecurityAuditStatusUnknown, "ssh_policy_ambiguous")
		return response
	}
	response.PasswordAuthentication = policy.passwordAuthentication
	response.KeyboardInteractiveAuthentication = policy.keyboardInteractiveAuthentication
	response.PermitRootLogin = policy.permitRootLogin
	response.PubkeyAuthentication = policy.pubkeyAuthentication
	response.HostbasedAuthentication = policy.hostbasedAuthentication
	response.GSSAPIAuthentication = policy.gssapiAuthentication
	response.Check = classifySecurityAuditSSHDPolicy(policy)
	return response
}

func classifySecurityAuditSSHDPolicy(policy securityAuditSSHDPolicy) transport.SecurityAuditCheck {
	if policy.passwordAuthentication == "yes" || policy.keyboardInteractiveAuthentication == "yes" {
		return securityAuditCheck(transport.SecurityAuditStatusFail, "ssh_password_auth_enabled")
	}
	if policy.hostbasedAuthentication == "yes" || policy.gssapiAuthentication == "yes" {
		return securityAuditCheck(transport.SecurityAuditStatusFail, "ssh_non_key_auth_enabled")
	}
	if policy.pubkeyAuthentication != "yes" {
		return securityAuditCheck(transport.SecurityAuditStatusUnknown, "ssh_policy_ambiguous")
	}
	if policy.permitRootLogin == "yes" {
		return securityAuditCheck(transport.SecurityAuditStatusWarning, "ssh_root_login_unrestricted")
	}
	rootSafe := policy.permitRootLogin == "no" ||
		policy.permitRootLogin == "prohibit-password" ||
		policy.permitRootLogin == "without-password" ||
		policy.permitRootLogin == "forced-commands-only"
	if !rootSafe {
		return securityAuditCheck(transport.SecurityAuditStatusUnknown, "ssh_policy_ambiguous")
	}
	// A safe-looking sshd -T result describes the securely read on-disk
	// default configuration. It does not prove that the live daemon was
	// launched without -f/-o overrides or reloaded after the last edit.
	return securityAuditCheck(transport.SecurityAuditStatusUnknown, "ssh_policy_live_unverified")
}

func parseSecurityAuditSSHDPolicy(raw []byte) (securityAuditSSHDPolicy, error) {
	values := map[string]string{}
	for _, rawLine := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimSpace(rawLine))
		if len(fields) == 0 {
			continue
		}
		key := strings.ToLower(fields[0])
		switch key {
		case "passwordauthentication", "kbdinteractiveauthentication", "permitrootlogin",
			"pubkeyauthentication", "hostbasedauthentication", "gssapiauthentication":
			if len(fields) != 2 {
				return securityAuditSSHDPolicy{}, fmt.Errorf("effective SSH policy is malformed")
			}
			if _, duplicate := values[key]; duplicate {
				return securityAuditSSHDPolicy{}, fmt.Errorf("effective SSH policy is duplicated")
			}
			values[key] = strings.ToLower(fields[1])
		}
	}
	policy := securityAuditSSHDPolicy{
		passwordAuthentication:            values["passwordauthentication"],
		keyboardInteractiveAuthentication: values["kbdinteractiveauthentication"],
		permitRootLogin:                   values["permitrootlogin"],
		pubkeyAuthentication:              values["pubkeyauthentication"],
		hostbasedAuthentication:           values["hostbasedauthentication"],
		gssapiAuthentication:              values["gssapiauthentication"],
	}
	if (policy.passwordAuthentication != "yes" && policy.passwordAuthentication != "no") ||
		(policy.keyboardInteractiveAuthentication != "yes" && policy.keyboardInteractiveAuthentication != "no") ||
		(policy.pubkeyAuthentication != "yes" && policy.pubkeyAuthentication != "no") ||
		(policy.hostbasedAuthentication != "yes" && policy.hostbasedAuthentication != "no") ||
		(policy.gssapiAuthentication != "yes" && policy.gssapiAuthentication != "no") {
		return securityAuditSSHDPolicy{}, fmt.Errorf("configured SSH authentication policy is missing or unsupported")
	}
	switch policy.permitRootLogin {
	case "yes", "no", "prohibit-password", "without-password", "forced-commands-only":
	default:
		return securityAuditSSHDPolicy{}, fmt.Errorf("effective SSH root policy is missing or unsupported")
	}
	return policy, nil
}

type securityAuditSSHDGraphState struct {
	root      string
	seen      map[string]bool
	active    map[string]bool
	fileCount int
	totalSize int
}

func inspectSecurityAuditSSHDConfigGraph(root, configPath string) (bool, error) {
	root = filepath.Clean(root)
	configPath = filepath.Clean(configPath)
	if !filepath.IsAbs(root) || !filepath.IsAbs(configPath) ||
		(configPath != root && !strings.HasPrefix(configPath, root+string(filepath.Separator))) {
		return false, fmt.Errorf("SSH config path escapes its fixed root")
	}
	state := &securityAuditSSHDGraphState{
		root: root, seen: map[string]bool{}, active: map[string]bool{},
	}
	return state.inspect(configPath, 0)
}

func (state *securityAuditSSHDGraphState) inspect(configPath string, depth int) (bool, error) {
	if depth > securityAuditSSHDIncludeDepth {
		return false, fmt.Errorf("SSH include depth exceeds the fixed limit")
	}
	if state.active[configPath] {
		return false, fmt.Errorf("SSH include cycle detected")
	}
	if state.seen[configPath] {
		return false, nil
	}
	state.fileCount++
	if state.fileCount > securityAuditSSHDFileCountMax {
		return false, fmt.Errorf("SSH include count exceeds the fixed limit")
	}
	raw, err := readPinnedSecurityAuditSSHDFile(configPath)
	if err != nil {
		return false, err
	}
	state.totalSize += len(raw)
	if state.totalSize > securityAuditSSHDTotalMax {
		return false, fmt.Errorf("SSH config graph exceeds the fixed byte limit")
	}
	includes, hasMatch, err := parseSecurityAuditSSHDConfig(raw)
	if err != nil || hasMatch {
		return hasMatch, err
	}
	state.active[configPath] = true
	defer delete(state.active, configPath)
	for _, include := range includes {
		pattern, err := resolveSecurityAuditSSHDInclude(state.root, include)
		if err != nil {
			return false, err
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return false, fmt.Errorf("SSH include pattern is invalid")
		}
		sort.Strings(matches)
		if len(matches)+state.fileCount > securityAuditSSHDFileCountMax {
			return false, fmt.Errorf("SSH include count exceeds the fixed limit")
		}
		for _, match := range matches {
			foundMatch, err := state.inspect(filepath.Clean(match), depth+1)
			if err != nil || foundMatch {
				return foundMatch, err
			}
		}
	}
	state.seen[configPath] = true
	return false, nil
}

func parseSecurityAuditSSHDConfig(raw []byte) ([]string, bool, error) {
	if bytes.IndexByte(raw, 0) >= 0 {
		return nil, false, fmt.Errorf("SSH config contains NUL")
	}
	includes := []string{}
	for _, rawLine := range strings.Split(string(raw), "\n") {
		if strings.ContainsRune(rawLine, '\r') || strings.HasSuffix(strings.TrimSpace(rawLine), string(rune(92))) {
			return nil, false, fmt.Errorf("SSH config uses unsupported line syntax")
		}
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "match":
			if len(fields) < 2 {
				return nil, false, fmt.Errorf("SSH Match directive is malformed")
			}
			return includes, true, nil
		case "include":
			if len(fields) < 2 || strings.ContainsAny(line, "\"'\\") {
				return nil, false, fmt.Errorf("SSH Include directive is unsupported or malformed")
			}
			includes = append(includes, fields[1:]...)
			if len(includes) > securityAuditSSHDFileCountMax {
				return nil, false, fmt.Errorf("SSH include count exceeds the fixed limit")
			}
		}
	}
	return includes, false, nil
}

func resolveSecurityAuditSSHDInclude(root, include string) (string, error) {
	if include == "" || filepath.Clean(include) != include {
		return "", fmt.Errorf("SSH include path is not canonical")
	}
	resolved := include
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, resolved)
	}
	resolved = filepath.Clean(resolved)
	if resolved == root || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("SSH include path escapes its fixed root")
	}
	return resolved, nil
}

func readPinnedSecurityAuditSSHDFile(configPath string) ([]byte, error) {
	parentFD, err := openTrustedRootOwnedDirectory(filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, filepath.Base(configPath), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), configPath)
	if file == nil {
		unix.Close(fd)
		return nil, fmt.Errorf("SSH config descriptor is invalid")
	}
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 ||
		before.Mode&0o022 != 0 || before.Nlink != 1 || before.Size < 0 || before.Size > securityAuditSSHDFileMax {
		return nil, fmt.Errorf("SSH config metadata is unsafe")
	}
	raw, err := io.ReadAll(io.LimitReader(file, securityAuditSSHDFileMax+1))
	if err != nil || len(raw) > securityAuditSSHDFileMax {
		return nil, fmt.Errorf("SSH config exceeds the fixed byte limit")
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil ||
		before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size ||
		before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid || before.Nlink != after.Nlink ||
		int64(len(raw)) != before.Size {
		return nil, fmt.Errorf("SSH config changed while it was read")
	}
	return raw, nil
}

func collectRebootSecurityAudit() transport.SecurityAuditRebootResponse {
	response := transport.SecurityAuditRebootResponse{
		Check: securityAuditCheck(transport.SecurityAuditStatusUnknown, "reboot_state_unknown"),
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil || profile.DistroFamily != hostplatform.DistroFamilyDebian {
		return response
	}
	_, err = os.Lstat(securityAuditRebootMarker)
	switch {
	case err == nil:
		response.Required = true
		response.Check = securityAuditCheck(transport.SecurityAuditStatusWarning, "reboot_required")
	case errors.Is(err, os.ErrNotExist):
		response.Check = securityAuditCheck(transport.SecurityAuditStatusPass, "reboot_not_required")
	}
	return response
}

type securityAuditObjectState uint8

const (
	securityAuditObjectOK securityAuditObjectState = iota
	securityAuditObjectMissing
	securityAuditObjectUnsafe
	securityAuditObjectUnreadable
)

func collectSignedUpdateSecurityAudit() transport.SecurityAuditSignedUpdateResponse {
	response := transport.SecurityAuditSignedUpdateResponse{
		Check: securityAuditCheck(transport.SecurityAuditStatusUnknown, "signed_update_trust_unreadable"),
	}
	for _, directory := range []struct {
		path      string
		exactMode os.FileMode
		rootGroup bool
	}{
		{path: "/etc", exactMode: 0},
		{path: "/etc/celikpanel", exactMode: 0, rootGroup: true},
		{path: "/var", exactMode: 0},
		{path: "/var/lib", exactMode: 0},
		{path: securityAuditReleaseStateDir, exactMode: 0o700},
	} {
		state := inspectSecurityAuditDirectoryWithGroup(directory.path, directory.exactMode, directory.rootGroup)
		if state != securityAuditObjectOK {
			return signedUpdateAuditFailure(response, state)
		}
	}
	keyRaw, state := readSecurityAuditRegularFile(securityAuditReleasePublicKey, 0o644, 1, 16<<10)
	if state != securityAuditObjectOK {
		return signedUpdateAuditFailure(response, state)
	}
	fingerprint, ok := securityAuditEd25519PublicKeyFingerprint(keyRaw)
	if !ok {
		return signedUpdateAuditFailure(response, securityAuditObjectUnsafe)
	}
	floorRaw, state := readSecurityAuditRegularFile(securityAuditReleaseFloor, 0o600, 1, 512)
	if state != securityAuditObjectOK {
		return signedUpdateAuditFailure(response, state)
	}
	sequence, version, ok := parseSecurityAuditReleaseFloor(floorRaw)
	if !ok || !securityAuditReleaseFloorMatchesBuild(version, buildVersion) {
		return signedUpdateAuditFailure(response, securityAuditObjectUnsafe)
	}
	lockRaw, state := readSecurityAuditRegularFile(securityAuditReleaseLock, 0o600, 0, 0)
	if state != securityAuditObjectOK || len(lockRaw) != 0 {
		if state == securityAuditObjectOK {
			state = securityAuditObjectUnsafe
		}
		return signedUpdateAuditFailure(response, state)
	}
	response.Check = securityAuditCheck(transport.SecurityAuditStatusWarning, "signed_update_identity_unverified")
	response.Enrolled = true
	response.Sequence = sequence
	response.Version = version
	response.KeyFingerprint = fingerprint
	return response
}

func securityAuditReleaseFloorMatchesBuild(floorVersion, runningBuildVersion string) bool {
	return floorVersion == strings.TrimSpace(runningBuildVersion)
}

func signedUpdateAuditFailure(response transport.SecurityAuditSignedUpdateResponse, state securityAuditObjectState) transport.SecurityAuditSignedUpdateResponse {
	switch state {
	case securityAuditObjectMissing:
		response.Check = securityAuditCheck(transport.SecurityAuditStatusFail, "signed_update_trust_not_enrolled")
	case securityAuditObjectUnsafe:
		response.Check = securityAuditCheck(transport.SecurityAuditStatusFail, "signed_update_trust_unsafe")
	default:
		response.Check = securityAuditCheck(transport.SecurityAuditStatusUnknown, "signed_update_trust_unreadable")
	}
	return response
}

func inspectSecurityAuditDirectory(path string, exactMode os.FileMode) securityAuditObjectState {
	return inspectSecurityAuditDirectoryWithGroup(path, exactMode, false)
}

func inspectSecurityAuditDirectoryWithGroup(path string, exactMode os.FileMode, allowRootNonRootGroup bool) securityAuditObjectState {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return securityAuditObjectMissing
	}
	if err != nil {
		return securityAuditObjectUnreadable
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		(!securityAuditRootOwned(info) && !(allowRootNonRootGroup && securityAuditRootUIDOwned(info))) ||
		info.Mode().Perm()&0o022 != 0 {
		return securityAuditObjectUnsafe
	}
	if exactMode != 0 && info.Mode().Perm() != exactMode {
		return securityAuditObjectUnsafe
	}
	return securityAuditObjectOK
}

func securityAuditRootUIDOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func inspectSecurityAuditRootDirectoryChain(path string) securityAuditObjectState {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path {
		return securityAuditObjectUnsafe
	}
	for current := clean; ; current = filepath.Dir(current) {
		state := inspectSecurityAuditDirectory(current, 0)
		if state != securityAuditObjectOK {
			return state
		}
		parent := filepath.Dir(current)
		if parent == current {
			return securityAuditObjectOK
		}
	}
}

func readSecurityAuditRegularFile(path string, mode os.FileMode, minimumSize, maximumSize int64) ([]byte, securityAuditObjectState) {
	return readSecurityAuditRegularFileWithOwner(path, mode, minimumSize, maximumSize, 0, 0)
}

func readSecurityAuditRegularFileWithOwner(
	path string,
	mode os.FileMode,
	minimumSize, maximumSize int64,
	expectedUID, expectedGID uint32,
) ([]byte, securityAuditObjectState) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, securityAuditObjectMissing
	}
	if err != nil {
		return nil, securityAuditObjectUnreadable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !securityAuditRegularFileMetadataSafe(
		info.Mode(), stat.Uid, stat.Gid, uint64(stat.Nlink), info.Size(),
		mode, minimumSize, maximumSize, expectedUID, expectedGID,
	) {
		return nil, securityAuditObjectUnsafe
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, securityAuditObjectUnreadable
	}
	if int64(len(raw)) != info.Size() {
		return nil, securityAuditObjectUnreadable
	}
	return raw, securityAuditObjectOK
}

func securityAuditRegularFileMetadataSafe(
	actualMode os.FileMode,
	uid, gid uint32,
	linkCount uint64,
	size int64,
	expectedMode os.FileMode,
	minimumSize, maximumSize int64,
	expectedUID, expectedGID uint32,
) bool {
	return actualMode.IsRegular() && actualMode&os.ModeSymlink == 0 &&
		actualMode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 &&
		actualMode.Perm() == expectedMode && uid == expectedUID && gid == expectedGID &&
		linkCount == 1 && size >= minimumSize && size <= maximumSize
}

func securityAuditOwnedBy(info os.FileInfo, expectedUID, expectedGID uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == expectedUID && stat.Gid == expectedGID
}

func securityAuditRootOwned(info os.FileInfo) bool {
	return securityAuditOwnedBy(info, 0, 0)
}

func securityAuditLinkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Nlink)
}

func securityAuditEd25519PublicKeyFingerprint(raw []byte) (string, bool) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || len(rest) != 0 ||
		!bytes.Equal(raw, pem.EncodeToMemory(block)) {
		return "", false
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", false
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return "", false
	}
	digest := sha256.Sum256(publicKey)
	return fmt.Sprintf("sha256:%x", digest[:]), true
}

func parseSecurityAuditReleaseFloor(raw []byte) (string, string, bool) {
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 4 || lines[3] != "" || lines[0] != "format=celikpanel-release-sequence-floor-v1" ||
		!strings.HasPrefix(lines[1], "sequence=") || !strings.HasPrefix(lines[2], "version=") {
		return "", "", false
	}
	sequence := strings.TrimPrefix(lines[1], "sequence=")
	version := strings.TrimPrefix(lines[2], "version=")
	parsed, err := strconv.ParseUint(sequence, 10, 63)
	if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != sequence ||
		!securityAuditReleaseVersionPattern.MatchString(version) {
		return "", "", false
	}
	canonical := fmt.Sprintf("format=celikpanel-release-sequence-floor-v1\nsequence=%s\nversion=%s\n", sequence, version)
	if canonical != string(raw) {
		return "", "", false
	}
	return sequence, version, true
}
