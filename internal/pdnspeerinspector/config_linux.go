//go:build linux

package pdnspeerinspector

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/pdnsmanagedconf"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// configShape is one reviewed PowerDNS daemon configuration. Every shape is
// bound to the same fixed process, gsqlite3 database, control socket and
// catalog CONSUMER checks; the shape only decides which configuration files
// and listeners are expected. Anything else is refused.
type configShape uint8

const (
	// shapeFixture is the documented panel-free secondary: one pdns.conf
	// with exactly the reviewed fixture directives and no include-dir.
	shapeFixture configShape = iota + 1
	// shapeManagedSecondary is a CelikPanel-managed PowerDNS secondary: the
	// distribution's pdns.conf loading pdnsmanagedconf.IncludeDir, which holds
	// exactly the product's backend drop-in and its secondary pair drop-in,
	// each equal to the product renderer's output for the values read.
	shapeManagedSecondary
)

// requiresLoopbackListeners: the fixture configures
// local-address=127.0.0.1,<peer>, so its daemon must hold both loopback
// listeners. The product renders only public addresses, so a managed
// secondary has none; the inspector itself never queries DNS over loopback.
func (s configShape) requiresLoopbackListeners() bool { return s == shapeFixture }

const (
	fixtureConfigLimit = 4096
	// The Debian/Ubuntu package's pdns.conf is a commented settings template
	// (20579 bytes measured on Debian 13, PowerDNS 4.9).
	mainConfigLimit   = 64 << 10
	dropInConfigLimit = 4096
	maxIncludeEntries = 64
)

// configTree is one stable read of the files PowerDNS loads at startup.
type configTree struct {
	main    []byte
	mainGID uint32
	// includes holds the include-dir entries PowerDNS loads (not hidden,
	// ".conf" suffix), by name. It is nil when the main file does not load
	// the reviewed include-dir, and non-nil (possibly empty) when it does.
	includes map[string][]byte
}

func (t configTree) digest() [32]byte {
	if t.includes == nil {
		return sha256.Sum256(t.main)
	}
	h := sha256.New()
	frame := func(path string, data []byte) {
		fmt.Fprintf(h, "%s\x00%d\x00", path, len(data))
		h.Write(data)
	}
	frame(ConfigPath, t.main)
	names := make([]string, 0, len(t.includes))
	for name := range t.includes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		frame(pdnsmanagedconf.IncludeDir+"/"+name, t.includes[name])
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// readReviewedConfiguration reads the daemon's configuration and classifies
// it. Every failure carries the reviewed config_unreviewed token: the owner
// can act on it, and no file content leaves the inspector.
func readReviewedConfiguration(pid uint64, p OwnerPolicyV1) (configShape, [32]byte, error) {
	daemonGID, _ := processGID(pid)
	return readReviewedConfigurationAt("/", daemonGID, p)
}

func readReviewedConfigurationAt(root string, daemonGID uint32, p OwnerPolicyV1) (configShape, [32]byte, error) {
	unreviewed := func(message string) (configShape, [32]byte, error) {
		return 0, [32]byte{}, reasonError(transport.DNSPeerInspectorReasonConfigUnreviewed, message)
	}
	tree, err := readConfigTreeAt(root, daemonGID, nil)
	if err != nil {
		return unreviewed(err.Error())
	}
	shape, ok := reviewedConfigTree(tree, p)
	if !ok {
		return unreviewed("PowerDNS configuration matches no reviewed shape")
	}
	return shape, tree.digest(), nil
}

// processGID returns the verified daemon's group when its real, effective,
// saved and filesystem GIDs agree. A managed main pdns.conf may belong to
// that group (Debian ships it root:pdns 0640); otherwise only root's group.
func processGID(pid uint64) (uint32, bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil || len(raw) > 64<<10 {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		value, found := strings.CutPrefix(line, "Gid:")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 4 {
			return 0, false
		}
		for _, field := range fields[1:] {
			if field != fields[0] {
				return 0, false
			}
		}
		gid, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil || strconv.FormatUint(gid, 10) != fields[0] {
			return 0, false
		}
		return uint32(gid), true
	}
	return 0, false
}

// readTrustedConfigAt returns the main configuration file only; it is the
// descriptor discipline shared by every shape.
func readTrustedConfigAt(root string, afterRead func()) ([]byte, error) {
	tree, err := readConfigTreeAt(root, 0, afterRead)
	return tree.main, err
}

// readConfigTreeAt reads ConfigPath and, when it loads the reviewed
// include-dir, that directory, through no-follow descriptors under
// root-controlled ancestors. Each file is rechecked against its final path and
// the directory against its own identity after reading, so a swap or an owner
// edit during the read fails closed. The hook exists only for a deterministic
// owner-edit regression; production passes nil.
func readConfigTreeAt(root string, daemonGID uint32, afterRead func()) (configTree, error) {
	fail := func() (configTree, error) { return configTree{}, errors.New("PowerDNS config unavailable or changed") }
	if os.Geteuid() != 0 || filepath.Dir(pdnsmanagedconf.IncludeDir) != filepath.Dir(ConfigPath) {
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
		if unix.Fstat(fd, &st) != nil || !secureConfigDirStat(st) {
			return fail()
		}
	}
	mainGroup := func(gid uint32) bool { return gid == 0 || (daemonGID != 0 && gid == daemonGID) }
	main, mainStat, err := readStableConfigFileAt(dirFD, filepath.Base(ConfigPath), mainConfigLimit, mainGroup, afterRead)
	if err != nil {
		return fail()
	}
	tree := configTree{main: main, mainGID: mainStat.Gid}
	directives, ok := parseStrictDirectives(string(main))
	if !ok || directives["include-dir"] != pdnsmanagedconf.IncludeDir {
		return tree, nil
	}
	includes, err := readIncludeDirAt(dirFD, filepath.Base(pdnsmanagedconf.IncludeDir))
	if err != nil {
		return fail()
	}
	tree.includes = includes
	return tree, nil
}

func secureConfigDirStat(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Uid == 0 && st.Gid == 0 && st.Mode&0022 == 0
}

func readStableConfigFileAt(dirFD int, name string, limit int64, group func(uint32) bool, afterRead func()) ([]byte, unix.Stat_t, error) {
	fail := func() ([]byte, unix.Stat_t, error) {
		return nil, unix.Stat_t{}, errors.New("PowerDNS config file unavailable or changed")
	}
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fail()
	}
	file := os.NewFile(uintptr(fd), "pdns-native-config")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !secureConfigFileStat(before, limit, group) {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return fail()
	}
	if afterRead != nil {
		afterRead()
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(dirFD, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!samePolicyStat(before, after) || !samePolicyStat(after, named) || int64(len(raw)) != after.Size {
		return fail()
	}
	return raw, after, nil
}

func secureConfigFileStat(st unix.Stat_t, limit int64, group func(uint32) bool) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&0022 == 0 && st.Uid == 0 && group(st.Gid) &&
		st.Nlink == 1 && st.Size > 0 && st.Size <= limit
}

// readIncludeDirAt reads the entries PowerDNS would load from the include
// directory: names not starting with "." and ending in ".conf". Only the two
// product drop-in names are read; any other loaded entry makes the whole
// configuration unreviewed. Ignored names cannot change what PowerDNS loads.
func readIncludeDirAt(parentFD int, name string) (map[string][]byte, error) {
	fail := func() (map[string][]byte, error) {
		return nil, errors.New("PowerDNS include directory unavailable, unreviewed or changed")
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail()
	}
	dir := os.NewFile(uintptr(fd), "pdns-native-include-dir")
	defer dir.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !secureConfigDirStat(before) {
		return fail()
	}
	names, err := dir.Readdirnames(maxIncludeEntries + 1)
	if (err != nil && !(errors.Is(err, io.EOF) && len(names) == 0)) || len(names) > maxIncludeEntries {
		return fail()
	}
	includes := map[string][]byte{}
	rootGroup := func(gid uint32) bool { return gid == 0 }
	for _, entry := range names {
		if strings.HasPrefix(entry, ".") || !strings.HasSuffix(entry, ".conf") {
			continue
		}
		if entry != pdnsmanagedconf.ManagedFile && entry != pdnsmanagedconf.ClusterFile {
			return fail()
		}
		raw, _, err := readStableConfigFileAt(fd, entry, dropInConfigLimit, rootGroup, nil)
		if err != nil {
			return fail()
		}
		includes[entry] = raw
	}
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!samePolicyStat(before, after) || !samePolicyStat(after, named) {
		return fail()
	}
	return includes, nil
}

// parseStrictDirectives accepts only lines that are empty, start with "#",
// or are exactly key=value. Leading whitespace, inline comments, "+=" and
// duplicate keys are refused, and so is any line ending in a backslash:
// PowerDNS joins such a line with the next one before it strips comments.
func parseStrictDirectives(raw string) (map[string]string, bool) {
	seen := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		if strings.HasSuffix(strings.TrimRight(line, " \t\r\n\v\f"), `\`) {
			return nil, false
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return nil, false
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, false
		}
		seen[key] = value
	}
	return seen, true
}

func sameDirectives(observed map[string]string, rendered string) bool {
	want, ok := parseStrictDirectives(rendered)
	if !ok || len(want) != len(observed) {
		return false
	}
	for key, value := range want {
		if got, present := observed[key]; !present || got != value {
			return false
		}
	}
	return true
}

func reviewedConfigTree(tree configTree, p OwnerPolicyV1) (configShape, bool) {
	if tree.includes == nil {
		if len(tree.main) <= fixtureConfigLimit && tree.mainGID == 0 && reviewedConfig(string(tree.main), p) {
			return shapeFixture, true
		}
		return 0, false
	}
	if reviewedManagedMainConfig(string(tree.main)) && reviewedManagedSecondaryDropIns(tree.includes, p) {
		return shapeManagedSecondary, true
	}
	return 0, false
}

// reviewedConfig is the documented panel-free fixture shape.
func reviewedConfig(raw string, p OwnerPolicyV1) bool {
	seen, ok := parseStrictDirectives(raw)
	if !ok {
		return false
	}
	expected := map[string]string{"launch": "gsqlite3", "gsqlite3-database": DatabasePath, "local-address": "127.0.0.1," + p.PeerIP, "local-port": "53", "primary": "no", "secondary": "yes", "xfr-cycle-interval": "1", "autosecondary": "no", "allow-axfr-ips": p.PrimaryIP + "/32,127.0.0.1/32", "disable-axfr": "no", "setuid": "powerdns", "setgid": "powerdns"}
	if len(seen) != len(expected) {
		return false
	}
	for key, want := range expected {
		if got, present := seen[key]; !present || got != want {
			return false
		}
	}
	return true
}

// reviewedManagedMainConfig is the main file of a managed secondary: the
// distribution's package file (Debian and Ubuntu ship exactly these three
// active lines among comments), or an owner file to which the Agent appended
// only the include-dir. Every other active directive is refused; the product's
// own backend, listener and pair settings live in the drop-ins.
func reviewedManagedMainConfig(raw string) bool {
	seen, ok := parseStrictDirectives(raw)
	if !ok || seen["include-dir"] != pdnsmanagedconf.IncludeDir {
		return false
	}
	for key, value := range seen {
		switch key {
		case "include-dir":
		case "launch", "security-poll-suffix":
			if value != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// reviewedManagedSecondaryDropIns requires exactly the product's two drop-ins,
// each equal (directive for directive) to the product renderer's output:
// the backend drop-in for the reviewed database and the listen addresses it
// names (which must include this peer), and the pair drop-in for this
// secondary of the policy's primary in one of the product's own renderings.
// The pair drop-in's allow-axfr-ips names only the primary; that it lacks
// loopback does not matter here, because this inspector performs no AXFR.
func reviewedManagedSecondaryDropIns(includes map[string][]byte, p OwnerPolicyV1) bool {
	managedRaw, managedOK := includes[pdnsmanagedconf.ManagedFile]
	clusterRaw, clusterOK := includes[pdnsmanagedconf.ClusterFile]
	if len(includes) != 2 || !managedOK || !clusterOK {
		return false
	}
	managed, ok := parseStrictDirectives(string(managedRaw))
	if !ok {
		return false
	}
	addresses := strings.Split(managed["local-address"], ",")
	peerListed := false
	for _, address := range addresses {
		peerListed = peerListed || address == p.PeerIP
	}
	rendered, err := pdnsmanagedconf.Standalone(DatabasePath, addresses)
	if !peerListed || err != nil || !sameDirectives(managed, string(rendered)) {
		return false
	}
	cluster, ok := parseStrictDirectives(string(clusterRaw))
	if !ok {
		return false
	}
	for _, render := range []func(string, string, string) (string, error){
		pdnsmanagedconf.DirectionalCluster,
		pdnsmanagedconf.DirectionalClusterAutosecondary,
	} {
		expected, err := render(transport.DNSPairRoleSecondary, p.PeerIP, p.PrimaryIP)
		if err == nil && sameDirectives(cluster, expected) {
			return true
		}
	}
	return false
}
