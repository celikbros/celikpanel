//go:build linux

package bindpeerinspector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnslistener"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

type Command func(context.Context, string, ...string) ([]byte, error)

// NativeReader uses only fixed read-only commands. It requires owner-enabled
// local AXFR of the transferred catalog; without that it returns unknown.
type NativeReader struct{ Run Command }

func (n NativeReader) Read(ctx context.Context, request dnspeerproof.RequestV1) (Snapshot, error) {
	if request.Validate() != nil {
		return Snapshot{}, errors.New("invalid native peer request")
	}
	run := n.Run
	if run == nil {
		run = runBounded
	}
	show, err := run(ctx, "/usr/bin/systemctl", "show", "named.service", "-p", "ActiveState", "-p", "MainPID")
	if err != nil {
		return Snapshot{}, errors.New("named service state is unavailable")
	}
	pid, err := parseNamedProcess(string(show))
	if err != nil {
		return Snapshot{}, err
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || (exe != "/usr/sbin/named" && exe != "/usr/bin/named") {
		return Snapshot{}, errors.New("named executable identity is unverified")
	}
	cmdline, err := readNamedCmdline(pid)
	if err != nil {
		return Snapshot{}, errors.New("named invocation is unavailable")
	}
	configPath, err := namedDefaultConfigForInvocation(exe, cmdline)
	if err != nil {
		return Snapshot{}, err
	}
	startTicks, err := readNamedStartTicks(pid)
	if err != nil {
		return Snapshot{}, errors.New("named process start identity is unavailable")
	}
	sockets, err := run(ctx, "/usr/bin/ss", "-H", "-lnupt", "( sport = :53 )")
	if err != nil {
		return Snapshot{}, errors.New("native DNS listeners are unavailable")
	}
	identities, err := dnslistener.CanonicalPublicListeners(string(sockets), "named", pid)
	if err != nil || !dnslistener.HasIPv4Listener(identities, request.PeerIP, pid) || !localCatalogListenerMatches(string(sockets), pid) {
		return Snapshot{}, errors.New("native DNS listeners do not match the peer")
	}
	controlSockets, err := run(ctx, "/usr/bin/ss", "-H", "-lnpt", "( sport = :953 )")
	if err != nil || !controlListenerMatches(string(controlSockets), pid) {
		return Snapshot{}, errors.New("native BIND control listener does not match named")
	}
	config, err := run(ctx, "/usr/bin/named-checkconf", "-p", configPath)
	if err != nil || len(config) == 0 {
		return Snapshot{}, errors.New("named configuration is unavailable")
	}
	if !configBindsCatalog(string(config), request) {
		return Snapshot{}, errors.New("native catalog subscription is unverified")
	}
	configHash := sha256.Sum256(config)
	catalogStatus, statusErr := run(ctx, "/usr/sbin/rndc", "-s", "127.0.0.1", "zonestatus", request.CatalogName)
	if statusErr != nil || !strings.Contains(string(catalogStatus), "type: secondary") {
		return Snapshot{}, errors.New("native transferred catalog is unavailable")
	}
	axfr, err := run(ctx, "/usr/bin/dig", "@127.0.0.1", request.CatalogName, "AXFR", "+tcp", "+noall", "+answer", "+ttlid", "+nocmd", "+nostats", "+nocomments", "+noquestion")
	if err != nil {
		return Snapshot{}, errors.New("local catalog AXFR is unavailable; enroll read-only inspector transfer access")
	}
	serial, members, err := parseCatalogAXFR(string(axfr), request)
	if err != nil {
		return Snapshot{}, err
	}
	zoneOutput, zoneErr := run(ctx, "/usr/sbin/rndc", "-s", "127.0.0.1", "zonestatus", request.DeletedZone)
	zoneState := "unknown"
	if zoneErr != nil {
		if strings.TrimSpace(string(zoneOutput)) == "rndc: 'zonestatus' failed: not found\nno matching zone '"+request.DeletedZone+"' in any view" {
			zoneState = "unloaded"
		}
	} else if len(zoneOutput) > 0 && strings.Contains(string(zoneOutput), "type:") {
		zoneState = "loaded"
	}
	return Snapshot{ProcessID: pid, ProcessStartTicks: startTicks, ConfigSHA256: hex.EncodeToString(configHash[:]), ListenersVerified: true, CatalogSerial: serial, CatalogMembers: members, CatalogTransferred: true, ZoneState: zoneState}, nil
}

type boundedOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(p) > 1<<20 {
		b.overflow = true
		return len(p), nil
	}
	_, _ = b.data.Write(p)
	return len(p), nil
}
func runBounded(ctx context.Context, path string, args ...string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, path, args...)
	command.Env = []string{"LC_ALL=C", "LANG=C", "PATH=/usr/sbin:/usr/bin:/bin"}
	output := &boundedOutput{}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if output.overflow {
		return nil, errors.New("native BIND output exceeded its safe bound")
	}
	return output.data.Bytes(), err
}

func localCatalogListenerMatches(output string, pid uint64) bool {
	var tcp, udp int
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		row, err := dnslistener.ParseRow(line)
		if err != nil {
			return false
		}
		if row.Address.String() == "127.0.0.1" {
			if row.Process != "named" || row.PID != pid {
				return false
			}
			if row.Protocol == "tcp" {
				tcp++
			} else {
				udp++
			}
		}
	}
	// Debian BIND may expose two fds for each transport on the same
	// address. Bound that shape, and require both transports for local AXFR.
	return tcp >= 1 && tcp <= 8 && udp >= 1 && udp <= 8
}

func controlListenerMatches(output string, pid uint64) bool {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 1 || len(lines) > 2 {
		return false
	}
	var ipv4, ipv6 bool
	for _, line := range lines {
		fields := strings.Fields(line)
		// ss -H -lnpt omits the protocol column because -t already
		// selects TCP. Refuse any other row shape.
		if len(fields) != 6 || fields[0] != "LISTEN" ||
			!dnslistener.CanonicalWildcardPeerEndpoint(fields[4]) {
			return false
		}
		for _, queue := range fields[1:3] {
			value, err := strconv.ParseUint(queue, 10, 64)
			if err != nil || strconv.FormatUint(value, 10) != queue {
				return false
			}
		}
		process, foundPID, err := dnslistener.ParseCanonicalSSProcessField(fields[5])
		if err != nil || process != "named" || foundPID != pid {
			return false
		}

		switch fields[3] {
		case "127.0.0.1:953":
			if ipv4 {
				return false
			}
			ipv4 = true
		case "[::1]:953":
			if ipv6 {
				return false
			}
			ipv6 = true
		default:
			return false
		}
	}
	return ipv4
}

func readNamedStartTicks(pid uint64) (uint64, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return 0, errors.New("named process stat is unavailable")
	}
	return parseNamedStartTicks(pid, string(raw))
}

func parseNamedStartTicks(pid uint64, line string) (uint64, error) {
	prefix := strconv.FormatUint(pid, 10) + " (named) "
	if !strings.HasPrefix(line, prefix) {
		return 0, errors.New("named process stat identity is invalid")
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
	if len(fields) < 20 || (fields[0] != "S" && fields[0] != "R" && fields[0] != "D" && fields[0] != "I") {
		return 0, errors.New("named process stat fields are invalid")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || ticks == 0 || strconv.FormatUint(ticks, 10) != fields[19] {
		return 0, errors.New("named process start ticks are invalid")
	}
	return ticks, nil
}

func readNamedCmdline(pid uint64) ([]byte, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return nil, errors.New("named invocation exceeds safe bound")
	}
	return raw, nil
}

// Only the certified vendor default invocations have a known mapping between
// a running named and the config parsed by named-checkconf. In particular -c,
// chroot and arbitrary OPTIONS are not accepted.
func namedDefaultConfigForInvocation(exe string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 4096 || raw[len(raw)-1] != 0 {
		return "", errors.New("named invocation is malformed")
	}
	args := strings.Split(string(raw[:len(raw)-1]), "\x00")
	var want []string
	var config string
	switch exe {
	case "/usr/sbin/named":
		want = []string{exe, "-f", "-u", "bind"}
		config = "/etc/bind/named.conf"
	case "/usr/bin/named":
		want = []string{exe, "-f", "-u", "named"}
		config = "/etc/named.conf"
	default:
		return "", errors.New("named executable is unsupported")
	}
	if len(args) != len(want) {
		return "", errors.New("named invocation does not use the reviewed default config")
	}
	for i := range want {
		if args[i] != want[i] {
			return "", errors.New("named invocation does not use the reviewed default config")
		}
	}
	return config, nil
}

func parseNamedProcess(output string) (uint64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		return 0, errors.New("named service state is ambiguous")
	}
	values := map[string]string{}
	for _, line := range lines {
		k, v, ok := strings.Cut(line, "=")
		if !ok || (k != "ActiveState" && k != "MainPID") || values[k] != "" {
			return 0, errors.New("named service state is invalid")
		}
		values[k] = v
	}
	if values["ActiveState"] != "active" {
		return 0, errors.New("named service is not active")
	}
	pid, err := strconv.ParseUint(values["MainPID"], 10, 64)
	if err != nil || pid == 0 || strconv.FormatUint(pid, 10) != values["MainPID"] {
		return 0, errors.New("named process identity is invalid")
	}
	return pid, nil
}

func configBindsCatalog(config string, request dnspeerproof.RequestV1) bool {
	escaped := regexp.QuoteMeta(request.CatalogName)
	ip := regexp.QuoteMeta(request.PrimaryIP)
	declaration := regexp.MustCompile("(?s)zone\\s+\\\"" + escaped + "\\\"\\s*\\{\\s*type\\s+secondary;\\s*(?:file\\s+\\\"[^\\\";\\n]+\\\";\\s*)?primaries\\s*\\{\\s*" + ip + ";\\s*\\};")
	subscription := regexp.MustCompile("(?s)zone\\s+\\\"" + escaped + "\\\"\\s+default-primaries\\s*\\{\\s*" + ip + ";\\s*\\}\\s+in-memory\\s+yes;")
	return !regexp.MustCompile("(?m)^\\s*view\\s+").MatchString(config) &&
		strings.Count(config, "zone \""+request.CatalogName+"\"") == 2 &&
		strings.Count(config, "catalog-zones") == 1 &&
		declaration.MatchString(config) && subscription.MatchString(config)
}

func parseCatalogAXFR(output string, request dnspeerproof.RequestV1) (uint32, []string, error) {
	if len(output) == 0 || len(output) > 1<<20 {
		return 0, nil, errors.New("catalog AXFR output is empty or oversized")
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 4 || len(lines) > 65540 {
		return 0, nil, errors.New("catalog AXFR record count is invalid")
	}
	catalog := request.CatalogName + "."
	members := []string{}
	var serial uint32
	soaCount, nsCount, versionCount := 0, 0, 0
	seen := map[string]bool{}
	seenLabels := map[string]bool{}
	metadataTTL := uint64(2)
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] == "" || fields[2] != "IN" {
			return 0, nil, errors.New("catalog AXFR record is malformed")
		}
		ttl, err := strconv.ParseUint(fields[1], 10, 32)
		if err != nil || (ttl != 0 && ttl != 60) || ((fields[3] == "SOA" || fields[3] == "NS") && ttl != 60) {
			return 0, nil, errors.New("catalog AXFR TTL is unsupported")
		}
		if fields[3] == "TXT" || fields[3] == "PTR" {
			if metadataTTL == 2 {
				metadataTTL = ttl
			} else if ttl != metadataTTL {
				return 0, nil, errors.New("catalog metadata TTLs disagree")
			}
		}
		switch fields[3] {
		case "SOA":
			if fields[0] != catalog || len(fields) != 11 || fields[4] != "invalid." || fields[5] != "invalid." || strings.Join(fields[7:], " ") != "60 30 3600 30" || (index != 0 && index != len(lines)-1) {
				return 0, nil, errors.New("catalog AXFR SOA is invalid")
			}
			value, e := strconv.ParseUint(fields[6], 10, 32)
			if e != nil || value == 0 || (serial != 0 && serial != uint32(value)) {
				return 0, nil, errors.New("catalog AXFR serial is invalid")
			}
			serial = uint32(value)
			soaCount++
		case "NS":
			if fields[0] != catalog || len(fields) != 5 || fields[4] != "invalid." || nsCount != 0 {
				return 0, nil, errors.New("catalog AXFR NS is invalid")
			}
			nsCount++
		case "TXT":
			if fields[0] != "version."+catalog || len(fields) != 5 || fields[4] != `"2"` || versionCount != 0 {
				return 0, nil, errors.New("catalog AXFR version is invalid")
			}
			versionCount++
		case "PTR":
			if len(fields) != 5 || !strings.HasSuffix(fields[0], ".zones."+catalog) || !strings.HasSuffix(fields[4], ".") {
				return 0, nil, errors.New("catalog AXFR member is invalid")
			}
			member := strings.TrimSuffix(fields[4], ".")
			label := strings.TrimSuffix(fields[0], ".zones."+catalog)
			deterministic, e := binddns.CatalogMemberLabel(member)
			validLabel := (ttl == 60 && label == deterministic) || (ttl == 0 && nativePDNSCatalogLabel(label))
			if e != nil || !validLabel || seen[member] || seenLabels[label] {
				return 0, nil, errors.New("catalog AXFR member binding is invalid")
			}
			seen[member] = true
			seenLabels[label] = true
			members = append(members, member)
		default:
			return 0, nil, errors.New("catalog AXFR contains an unsupported record")
		}
	}
	if soaCount != 2 || nsCount != 1 || versionCount != 1 {
		return 0, nil, errors.New("catalog AXFR envelope is incomplete")
	}
	return serial, members, nil
}

// PowerDNS's native catalog producer chooses a 32-character base32hex label
// rather than BIND's deterministic SHA-224 label. The semantic proof binds the
// resulting member set and serial, while the transferred owner must stay in
// this exact RFC 9432 namespace and be unique.
func nativePDNSCatalogLabel(label string) bool {
	if len(label) != 32 {
		return false
	}
	for _, value := range []byte(label) {
		if (value < '0' || value > '9') && (value < 'a' || value > 'v') {
			return false
		}
	}
	return true
}
