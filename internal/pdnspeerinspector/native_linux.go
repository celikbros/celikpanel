//go:build linux

package pdnspeerinspector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnslistener"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

type Command func(context.Context, string, ...string) ([]byte, error)
type NativeReader struct{ Run Command }

func (n NativeReader) Read(ctx context.Context, r pdnspeerproof.RequestV1, p OwnerPolicyV1) (Snapshot, error) {
	if p.ValidateRequest(r) != nil {
		return Snapshot{}, errors.New("PowerDNS native request is not owner-authorized")
	}
	run := n.Run
	if run == nil {
		run = runReadOnly
	}
	show, err := run(ctx, "/usr/bin/systemctl", "show", "pdns.service", "-p", "ActiveState", "-p", "MainPID")
	if err != nil {
		return Snapshot{}, errors.New("PowerDNS service state unavailable")
	}
	pid, err := parseServicePID(string(show))
	if err != nil {
		return Snapshot{}, err
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || (exe != "/usr/bin/pdns_server" && exe != "/usr/sbin/pdns_server") {
		return Snapshot{}, errors.New("PowerDNS executable is unverified")
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !reviewedInvocation(exe, cmdline) {
		return Snapshot{}, errors.New("PowerDNS invocation is unreviewed")
	}
	ticks, err := processStartTicks(pid)
	if err != nil {
		return Snapshot{}, err
	}
	listeners, err := run(ctx, "/usr/bin/ss", "-H", "-lnupt", "( sport = :53 )")
	if err != nil {
		return Snapshot{}, errors.New("PowerDNS listeners unavailable")
	}
	identities, err := dnslistener.CanonicalPublicListeners(string(listeners), "pdns_server", pid)
	if err != nil || !dnslistener.HasIPv4Listener(identities, r.PeerIP, pid) || !hasLoopbackListeners(string(listeners), pid) {
		return Snapshot{}, errors.New("PowerDNS listeners do not match service")
	}
	config, err := readTrustedConfig(ConfigPath)
	if err != nil || !reviewedConfig(string(config), p) {
		return Snapshot{}, errors.New("PowerDNS SQLite backend configuration is unreviewed")
	}
	hash := sha256.Sum256(config)
	dbDev, dbIno, err := boundDatabase(pid, DatabasePath)
	if err != nil {
		return Snapshot{}, err
	}
	serial, members, zoneInDB, err := readCatalogDatabase(ctx, DatabasePath, dbDev, dbIno, r, p)
	if err != nil {
		return Snapshot{}, err
	}
	socketPath, socketInode, err := findControlSocket(pid)
	if err != nil {
		return Snapshot{}, err
	}
	control, err := readLiveZonesAuthenticated(ctx, socketPath, socketInode, pid, ticks, exe)
	if err != nil {
		return Snapshot{}, err
	}
	zones, err := parseLiveZones(control)
	if err != nil {
		return Snapshot{}, err
	}
	if _, ok := zones[r.CatalogName]; !ok {
		return Snapshot{}, errors.New("PowerDNS live catalog is not loaded")
	}
	_, zoneInDaemon := zones[r.DeletedZone]
	state := classifyAuthenticatedControlZone(zoneInDB, zoneInDaemon)
	if !controlSocketMatches(pid, socketPath, socketInode) {
		return Snapshot{}, errors.New("PowerDNS control socket changed")
	}
	afterDev, afterIno, err := boundDatabase(pid, DatabasePath)
	if err != nil || afterDev != dbDev || afterIno != dbIno {
		return Snapshot{}, errors.New("PowerDNS database binding changed")
	}
	afterTicks, err := processStartTicks(pid)
	if err != nil || afterTicks != ticks {
		return Snapshot{}, errors.New("PowerDNS process changed")
	}
	return Snapshot{ProcessID: pid, ProcessStartTicks: ticks, ConfigSHA256: hex.EncodeToString(hash[:]), DatabaseDevice: dbDev, DatabaseInode: dbIno, ListenersVerified: true, ControlSocketVerified: true, CatalogSerial: serial, CatalogMembers: members, CatalogTransferred: true, ZoneState: state}, nil
}
func classifyAuthenticatedControlZone(inDB, inDaemon bool) string {
	if inDB || inDaemon {
		return "loaded"
	}
	return "unloaded"
}
func runReadOnly(ctx context.Context, path string, args ...string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bounded, path, args...)
	cmd.Env = []string{"LC_ALL=C", "LANG=C", "PATH=/usr/sbin:/usr/bin:/bin"}
	output := &boundedNativeOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	if output.overflow {
		return nil, errors.New("PowerDNS native output exceeds bound")
	}
	return output.data.Bytes(), err
}

type boundedNativeOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
}

func (b *boundedNativeOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len()+len(p) > 1<<20 {
		b.overflow = true
		return len(p), nil
	}
	_, _ = b.data.Write(p)
	return len(p), nil
}
func parseServicePID(raw string) (uint64, error) {
	values := map[string]string{}
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 2 {
		return 0, errors.New("PowerDNS service state ambiguous")
	}
	for _, line := range lines {
		k, v, ok := strings.Cut(line, "=")
		if !ok || (k != "ActiveState" && k != "MainPID") || values[k] != "" {
			return 0, errors.New("PowerDNS service state malformed")
		}
		values[k] = v
	}
	pid, err := strconv.ParseUint(values["MainPID"], 10, 64)
	if values["ActiveState"] != "active" || err != nil || pid == 0 || strconv.FormatUint(pid, 10) != values["MainPID"] {
		return 0, errors.New("PowerDNS service inactive or PID invalid")
	}
	return pid, nil
}
func reviewedInvocation(exe string, raw []byte) bool {
	if len(raw) == 0 || len(raw) > 4096 || raw[len(raw)-1] != 0 {
		return false
	}
	args := strings.Split(string(raw[:len(raw)-1]), "\x00")
	if args[0] != exe {
		return false
	}
	// Arch's packaged service may pass only these daemon-mode arguments. Any
	// config path, backend override, chroot or alternate socket is rejected.
	allowed := map[string]bool{"--daemon=no": true, "--guardian=no": true, "--write-pid=no": true, "--disable-syslog": true, "--log-timestamp=no": true}
	seen := map[string]bool{}
	for _, arg := range args[1:] {
		if !allowed[arg] || seen[arg] {
			return false
		}
		seen[arg] = true
	}
	return true
}
func processStartTicks(pid uint64) (uint64, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil || len(raw) > 4096 {
		return 0, errors.New("PowerDNS process stat unavailable")
	}
	prefix := strconv.FormatUint(pid, 10) + " (pdns_server) "
	if !strings.HasPrefix(string(raw), prefix) {
		return 0, errors.New("PowerDNS process stat identity differs")
	}
	fields := strings.Fields(strings.TrimPrefix(string(raw), prefix))
	if len(fields) < 20 {
		return 0, errors.New("PowerDNS process stat malformed")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || ticks == 0 {
		return 0, errors.New("PowerDNS process start time invalid")
	}
	return ticks, nil
}
func hasLoopbackListeners(raw string, pid uint64) bool {
	var tcp, udp bool
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		row, err := dnslistener.ParseRow(line)
		if err != nil {
			return false
		}
		if row.Address.String() == "127.0.0.1" {
			if row.PID != pid || row.Process != "pdns_server" {
				return false
			}
			if row.Protocol == "tcp" {
				tcp = true
			}
			if row.Protocol == "udp" {
				udp = true
			}
		}
	}
	return tcp && udp
}
func readTrustedConfig(path string) ([]byte, error) {
	if path != ConfigPath {
		return nil, errors.New("PowerDNS config path is not reviewed")
	}
	return readTrustedConfigAt("/", nil)
}

// The hook exists only for a deterministic owner-edit regression. Production
// passes nil and cannot choose a path or run code between read and recheck.
func readTrustedConfigAt(root string, afterRead func()) ([]byte, error) {
	fail := func() ([]byte, error) { return nil, errors.New("PowerDNS config unavailable or changed") }
	if os.Geteuid() != 0 {
		return fail()
	}
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	defer unix.Close(rootFD)
	etcFD, err := unix.Openat(rootFD, "etc", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	defer unix.Close(etcFD)
	dirFD, err := unix.Openat(etcFD, "powerdns", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	defer unix.Close(dirFD)
	for _, fd := range []int{rootFD, etcFD, dirFD} {
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
			return fail()
		}
	}
	fd, err := unix.Openat(dirFD, "pdns.conf", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fail()
	}
	file := os.NewFile(uintptr(fd), "pdns-native-config")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !secureNativeConfigStat(before) {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return fail()
	}
	if afterRead != nil {
		afterRead()
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, "pdns.conf", &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!samePolicyStat(before, after) || !samePolicyStat(after, named) || int64(len(raw)) != after.Size {
		return fail()
	}
	return raw, nil
}
func secureNativeConfigStat(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&0022 == 0 && st.Uid == 0 && st.Gid == 0 && st.Nlink == 1 && st.Size > 0 && st.Size <= 4096
}
func reviewedConfig(raw string, p OwnerPolicyV1) bool {
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	seen := map[string]string{}
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || seen[key] != "" {
			return false
		}
		seen[key] = value
	}
	expected := map[string]string{"launch": "gsqlite3", "gsqlite3-database": DatabasePath, "local-address": "127.0.0.1," + p.PeerIP, "local-port": "53", "primary": "no", "secondary": "yes", "xfr-cycle-interval": "1", "autosecondary": "no", "allow-axfr-ips": p.PrimaryIP + "/32,127.0.0.1/32", "disable-axfr": "no", "setuid": "powerdns", "setgid": "powerdns"}
	if len(seen) != len(expected) {
		return false
	}
	for key, want := range expected {
		if seen[key] != want {
			return false
		}
	}
	return true
}
func boundDatabase(pid uint64, path string) (uint64, uint64, error) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return 0, 0, errors.New("PowerDNS database unavailable")
	}
	info, ok := st.Sys().(*syscall.Stat_t)
	if !ok || info.Ino == 0 {
		return 0, 0, errors.New("PowerDNS database identity unavailable")
	}
	found, err := processHasDatabaseInode(pid, path, uint64(info.Dev), info.Ino)
	if err != nil || !found {
		return 0, 0, errors.New("PowerDNS process database inode differs")
	}
	return uint64(info.Dev), info.Ino, nil
}
func processHasDatabaseInode(pid uint64, path string, dev, ino uint64) (bool, error) {
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return false, err
	}
	found := false
	for _, fd := range fds {
		fdpath := fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name())
		target, e := os.Readlink(fdpath)
		if e != nil || !(target == path || target == path+" (deleted)") {
			continue
		}
		open, e := os.Stat(fdpath)
		if e != nil {
			return false, e
		}
		identity, ok := open.Sys().(*syscall.Stat_t)
		if !ok || uint64(identity.Dev) != dev || identity.Ino != ino {
			return false, errors.New("PowerDNS database descriptor differs")
		}
		found = true
	}
	return found, nil
}
func selfSQLiteConnectionBound(path string, dev, ino uint64) error {
	found, err := processHasDatabaseInode(uint64(os.Getpid()), path, dev, ino)
	if err != nil || !found {
		return errors.New("inspector SQLite connection is not bound to daemon database inode")
	}
	return nil
}
func findControlSocket(pid uint64) (string, string, error) {
	found, foundInode := "", ""
	for _, candidate := range [...]string{"/run/pdns.controlsocket", "/var/run/pdns.controlsocket", "/run/pdns/pdns.controlsocket", "/var/run/pdns/pdns.controlsocket"} {
		inode, err := ownedControlSocketInode(pid, candidate)
		if err == nil {
			if found != "" {
				return "", "", errors.New("PowerDNS control socket identity ambiguous")
			}
			found, foundInode = candidate, inode
		}
	}
	if found == "" {
		return "", "", errors.New("PowerDNS control socket does not belong to active service")
	}
	return found, foundInode, nil
}
func controlSocketMatches(pid uint64, path, wantInode string) bool {
	inode, err := ownedControlSocketInode(pid, path)
	return err == nil && inode == wantInode
}
func ownedControlSocketInode(pid uint64, path string) (string, error) {
	data, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return "", errors.New("PowerDNS control socket table unavailable")
	}
	inode, err := listeningUnixSocketInode(string(data), path)
	if err != nil {
		return "", err
	}
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return "", errors.New("PowerDNS control process unavailable")
	}
	for _, fd := range fds {
		target, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
		if e == nil && target == "socket:["+inode+"]" {
			return inode, nil
		}
	}
	return "", errors.New("PowerDNS control socket is not held by service")
}

// PowerDNS 5.1.4 DynMessenger sends one newline-terminated command over a
// Unix stream and reads the raw response until EOF. LIST-ZONES is a fixed,
// read-only DynListener command; request-controlled commands are never accepted.
func readLiveZonesAuthenticated(ctx context.Context, path, listenerInode string, pid, startTicks uint64, exe string) (string, error) {
	bound, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(bound, "unix", path)
	if err != nil {
		return "", errors.New("PowerDNS live control connection unavailable")
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return "", errors.New("PowerDNS control connection is not Unix")
	}
	if err := conn.SetDeadline(time.Now().Add(4 * time.Second)); err != nil {
		return "", errors.New("PowerDNS control deadline unavailable")
	}
	// SO_PEERCRED is obtained from this exact fd before any command is sent.
	// A pathname swap to an attacker socket cannot preserve the daemon PID.
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return "", errors.New("PowerDNS control descriptor unavailable")
	}
	var cred *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		cred, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil || controlErr != nil || cred == nil || cred.Pid <= 0 || uint64(cred.Pid) != pid {
		return "", errors.New("PowerDNS control peer PID differs")
	}
	if err := verifyControlProcess(pid, startTicks, exe, path, listenerInode); err != nil {
		return "", err
	}
	if _, err := io.WriteString(conn, "LIST-ZONES\n"); err != nil {
		return "", errors.New("PowerDNS control request failed")
	}
	response, err := io.ReadAll(io.LimitReader(conn, (1<<20)+1))
	if err != nil || len(response) == 0 || len(response) > 1<<20 {
		return "", errors.New("PowerDNS control response unavailable or too large")
	}
	if err := verifyControlProcess(pid, startTicks, exe, path, listenerInode); err != nil {
		return "", err
	}
	return string(response), nil
}
func verifyControlProcess(pid, startTicks uint64, exe, path, listenerInode string) error {
	actualExe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || actualExe != exe {
		return errors.New("PowerDNS control executable changed")
	}
	actualTicks, err := processStartTicks(pid)
	if err != nil || actualTicks != startTicks {
		return errors.New("PowerDNS control process changed")
	}
	if !controlSocketMatches(pid, path, listenerInode) {
		return errors.New("PowerDNS control listener changed")
	}
	return nil
}

// Linux lists an accepted stream socket under the same pathname while a
// control exchange is in flight. Only the listening socket has SO_ACCEPTCON
// (Flags 00010000), stream Type 0001, and unconnected St 01. Ambiguous or
// unfamiliar listener rows never establish a daemon-owned control endpoint.
func listeningUnixSocketInode(raw, path string) (string, error) {
	var inode string
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 8 || fields[7] != path {
			continue
		}
		if fields[3] != "00010000" || fields[4] != "0001" || fields[5] != "01" {
			// An accepted connection has Flags 00000000 and St 03.
			// Any other row for this path is unreviewed.
			if fields[3] != "00000000" || fields[4] != "0001" || fields[5] != "03" {
				return "", errors.New("PowerDNS control socket framing unknown")
			}
			continue
		}
		if inode != "" {
			return "", errors.New("PowerDNS control listener ambiguous")
		}
		inode = fields[6]
	}
	if inode == "" {
		return "", errors.New("PowerDNS control listener missing")
	}
	return inode, nil
}

func parseLiveZones(raw string) (map[string]struct{}, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errors.New("PowerDNS control zone list unavailable or too large")
	}
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return nil, errors.New("PowerDNS control zone count unavailable")
	}
	countLine := lines[len(lines)-1]
	if !strings.HasPrefix(countLine, "All zonecount: ") {
		return nil, errors.New("PowerDNS control zone count framing unknown")
	}
	countText := strings.TrimPrefix(countLine, "All zonecount: ")
	count, err := strconv.ParseUint(countText, 10, 32)
	if err != nil || strconv.FormatUint(count, 10) != countText || count != uint64(len(lines)-1) {
		return nil, errors.New("PowerDNS control zone count differs")
	}
	zones := map[string]struct{}{}
	for _, line := range lines[:len(lines)-1] {
		if strings.TrimSpace(line) != line || !strings.HasSuffix(line, ".") {
			return nil, errors.New("PowerDNS control zone framing unknown")
		}
		value := strings.TrimSuffix(line, ".")
		if _, err := binddns.CatalogMemberLabel(value); err != nil {
			return nil, errors.New("PowerDNS control zone name invalid")
		}
		if _, exists := zones[value]; exists {
			return nil, errors.New("PowerDNS control zone list has duplicates")
		}
		zones[value] = struct{}{}
	}
	return zones, nil
}
func readCatalogDatabase(ctx context.Context, path string, dev, ino uint64, r pdnspeerproof.RequestV1, p OwnerPolicyV1) (uint32, []string, bool, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, nil, false, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, nil, false, err
	}
	defer conn.Close()
	var health string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&health); err != nil || health != "ok" {
		return 0, nil, false, errors.New("PowerDNS SQLite integrity unverified")
	}
	if err := selfSQLiteConnectionBound(path, dev, ino); err != nil {
		return 0, nil, false, err
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, nil, false, err
	}
	defer tx.Rollback()
	var countCatalog int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains WHERE name=?", r.CatalogName).Scan(&countCatalog); err != nil || countCatalog != 1 {
		return 0, nil, false, errors.New("PowerDNS catalog row is not unique")
	}
	var id int64
	var name, kind, master, account string
	var catalog sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT id,name,type,master,account,catalog FROM domains WHERE name=?", r.CatalogName).Scan(&id, &name, &kind, &master, &account, &catalog)
	if err != nil || id <= 0 || name != r.CatalogName || kind != "CONSUMER" || master != r.PrimaryIP || account != p.CatalogAccount || catalog.Valid {
		return 0, nil, false, errors.New("PowerDNS catalog CONSUMER identity differs")
	}
	var serial uint32
	var members []string
	var soa, ns, version int
	rows, err := tx.QueryContext(ctx, "SELECT name,type,content FROM records WHERE domain_id=? ORDER BY name,type,content", id)
	if err != nil {
		return 0, nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, typ, content string
		if rows.Scan(&name, &typ, &content) != nil {
			return 0, nil, false, errors.New("PowerDNS catalog record malformed")
		}
		switch typ {
		case "SOA":
			if name != r.CatalogName || soa != 0 {
				return 0, nil, false, errors.New("PowerDNS catalog SOA invalid")
			}
			fields := strings.Fields(content)
			if len(fields) != 7 || fields[0] != "invalid" || fields[1] != "invalid" || strings.Join(fields[3:], " ") != "60 30 3600 30" {
				return 0, nil, false, errors.New("PowerDNS catalog SOA content invalid")
			}
			value, e := strconv.ParseUint(fields[2], 10, 32)
			if e != nil || value == 0 {
				return 0, nil, false, errors.New("PowerDNS catalog serial invalid")
			}
			serial = uint32(value)
			soa++
		case "NS":
			if name != r.CatalogName || content != "invalid" || ns != 0 {
				return 0, nil, false, errors.New("PowerDNS catalog NS invalid")
			}
			ns++
		case "TXT":
			if name != "version."+r.CatalogName || content != `"2"` || version != 0 {
				return 0, nil, false, errors.New("PowerDNS catalog version invalid")
			}
			version++
		case "PTR":
			suffix := ".zones." + r.CatalogName
			if !strings.HasSuffix(name, suffix) {
				return 0, nil, false, errors.New("PowerDNS catalog PTR owner invalid")
			}
			member := content
			label, e := binddns.CatalogMemberLabel(member)
			if e != nil || name != label+suffix {
				return 0, nil, false, errors.New("PowerDNS catalog PTR digest invalid")
			}
			members = append(members, member)
		default:
			if name != "zones."+r.CatalogName || typ != "" || content != "" {
				return 0, nil, false, errors.New("PowerDNS catalog record unsupported")
			}
		}
	}
	if rows.Err() != nil || soa != 1 || ns != 1 || version != 1 {
		return 0, nil, false, errors.New("PowerDNS catalog incomplete")
	}
	sort.Strings(members)
	for i := 1; i < len(members); i++ {
		if members[i] == members[i-1] {
			return 0, nil, false, errors.New("PowerDNS catalog duplicate member")
		}
	}
	var count int
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains WHERE name=?", r.DeletedZone).Scan(&count)
	if err != nil || count < 0 || count > 1 {
		return 0, nil, false, errors.New("PowerDNS member lookup unavailable")
	}
	if err := selfSQLiteConnectionBound(path, dev, ino); err != nil {
		return 0, nil, false, err
	}
	return serial, members, count == 1, nil
}
