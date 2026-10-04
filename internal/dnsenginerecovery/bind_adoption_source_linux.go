//go:build linux

package dnsenginerecovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/bindconfig"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"golang.org/x/sys/unix"
)

// BINDAdoptionNativeZoneV1 is one entry from named-checkconf -l. The native
// inventory deliberately carries no file path; paths come only from frozen
// static configuration.
type BINDAdoptionNativeZoneV1 struct{ Name, Class, View, Type string }
type BINDAdoptionSOAProbe func(context.Context, string, string) (uint32, error)

// ParseBINDAdoptionNativeInventory parses bounded named-checkconf -l output;
// unlike -p, this output contains no expanded config or shared secrets.
func ParseBINDAdoptionNativeInventory(output string) ([]BINDAdoptionNativeZoneV1, error) {
	if len(output) > 1<<20 {
		return nil, errors.New("BIND inventory exceeds limit")
	}
	var out []BINDAdoptionNativeZoneV1
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || len(out) >= 128 {
			return nil, errors.New("BIND inventory line has unsupported shape")
		}
		if f[1] != "IN" || f[2] != "_default" {
			return nil, errors.New("BIND inventory has unsupported class or view")
		}
		if f[3] != "master" && f[3] != "primary" && f[3] != "hint" {
			return nil, errors.New("BIND inventory has unsupported zone type")
		}
		name := f[0]
		if strings.HasPrefix(name, `"`) || strings.HasSuffix(name, `"`) {
			if len(name) < 3 || !strings.HasPrefix(name, `"`) || !strings.HasSuffix(name, `"`) {
				return nil, errors.New("BIND inventory zone quoting is invalid")
			}
			name = name[1 : len(name)-1]
		}
		if name == "" || strings.ContainsAny(name, "/\\\x00\r\n") {
			return nil, errors.New("BIND inventory zone name is unsafe")
		}
		out = append(out, BINDAdoptionNativeZoneV1{Name: name, Class: f[1], View: f[2], Type: f[3]})
	}
	if len(out) == 0 {
		return nil, errors.New("BIND inventory is empty")
	}
	return out, nil
}

func adoptionStaticZones(j dnsengineartifact.SwitchJournalV1) ([]bindconfig.StaticZone, error) {
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV2 || j.InversePlan == nil || j.InversePlan.HostLayout != "apt" || j.InversePlan.Kind != dnsengineartifact.BINDSwitchInversePlanKindV2 || j.SourceEngine != "" || j.TargetEngine != "bind" || j.Mode != "switch" || j.Topology != "standalone" || j.StateBefore.Exists || len(j.ConfigBefore) != 2 || len(j.InversePlan.BINDUnchangedConfig) != 2 || len(j.SourceUnitsBefore) != 0 {
		return nil, errors.New("BIND adoption has unsupported journal envelope")
	}
	m, e := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(j.Mode, j.SourceEngine, j.TargetEngine, j.SourceEpoch, j.TargetEpoch, j.SourceRevision, j.Topology, j.PairRole, j.LocalIP, j.LocalNS, j.PeerIP, j.PeerNS, j.Zones)
	if e != nil {
		return nil, e
	}
	running, e := dnsengineartifact.RunningBINDAdoptionJournal(m, j)
	if e != nil || !running {
		return nil, errors.New("BIND adoption requires active owner unit preimage")
	}
	if j.InversePlan.BINDUnchangedConfig[0].Path != "/etc/bind/named.conf" {
		return nil, errors.New("BIND adoption main config missing")
	}
	leaf, e := bindconfig.DebianInverseMainLeaf(string(j.InversePlan.BINDUnchangedConfig[0].Data))
	if e != nil || leaf != j.InversePlan.BINDUnchangedConfig[1].Path {
		return nil, errors.New("BIND adoption main include envelope differs")
	}
	return bindconfig.ParseAdoptionStaticZones(string(j.ConfigBefore[0].Data), string(j.ConfigBefore[1].Data), string(j.InversePlan.BINDUnchangedConfig[1].Data))
}
func adoptionInventoryEqual(zones []bindconfig.StaticZone, inventory []BINDAdoptionNativeZoneV1) error {
	if len(zones) != len(inventory) {
		return errors.New("BIND native zone inventory differs from frozen static config")
	}
	seen := map[string]bool{}
	for _, n := range inventory {
		if n.View != "" && n.View != "_default" {
			return errors.New("BIND native view is unsupported")
		}
		name := strings.ToLower(strings.TrimSuffix(n.Name, "."))
		if n.Name == "." {
			name = "."
		}
		typ := n.Type
		if typ == "primary" {
			typ = "master"
		}
		key := name + "/" + n.Class + "/" + typ
		if seen[key] {
			return errors.New("duplicate BIND native zone")
		}
		seen[key] = true
	}
	for _, z := range zones {
		typ := z.Type
		if typ == "primary" {
			typ = "master"
		}
		if !seen[z.Name+"/"+z.Class+"/"+typ] {
			return errors.New("BIND native zone inventory differs from frozen static config")
		}
	}
	return nil
}
func CaptureBINDAdoptionSourceProof(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32, inventory []BINDAdoptionNativeZoneV1, probe BINDAdoptionSOAProbe) (dnsengineartifact.BINDAdoptionSourceProofV1, error) {
	var empty dnsengineartifact.BINDAdoptionSourceProofV1
	if ctx == nil || ctx.Err() != nil || layout != bindroot.APT || probe == nil {
		return empty, errors.New("BIND adoption source capture lacks trusted inputs")
	}
	zones, e := adoptionStaticZones(j)
	if e != nil {
		return empty, e
	}
	if e = adoptionInventoryEqual(zones, inventory); e != nil {
		return empty, e
	}
	unchanged, e := CaptureInstalledBINDUnchangedConfigV2(ctx, layout, gid)
	if e != nil {
		return empty, e
	}
	if !reflect.DeepEqual(unchanged, j.InversePlan.BINDUnchangedConfig) {
		return empty, errors.New("BIND adoption unchanged config differs from frozen evidence")
	}
	if e = adoptionConfigBeforeStable(ctx, j, layout, gid); e != nil {
		return empty, e
	}
	files, e := captureAdoptionFiles(ctx, zones)
	if e != nil {
		return empty, e
	}
	proof := dnsengineartifact.BINDAdoptionSourceProofV1{Kind: dnsengineartifact.BINDAdoptionSourceProofKindV1, Files: files}
	for _, z := range zones {
		item := dnsengineartifact.BINDAdoptionSourceZoneV1{Name: z.Name, Class: z.Class, Type: z.Type, File: z.File}
		if z.Type == "master" || z.Type == "primary" {
			udp, e := probe(ctx, z.Name, "udp")
			if e != nil {
				return empty, fmt.Errorf("BIND adoption UDP SOA %s: %w", z.Name, e)
			}
			tcp, e := probe(ctx, z.Name, "tcp")
			if e != nil {
				return empty, fmt.Errorf("BIND adoption TCP SOA %s: %w", z.Name, e)
			}
			if udp != tcp {
				return empty, errors.New("BIND adoption owner SOA protocols disagree")
			}
			item.SOASerial = udp
		}
		proof.Zones = append(proof.Zones, item)
	}
	if e = adoptionConfigBeforeStable(ctx, j, layout, gid); e != nil {
		return empty, e
	}
	again, e := captureAdoptionFiles(ctx, zones)
	if e != nil {
		return empty, e
	}
	if !reflect.DeepEqual(files, again) {
		return empty, errors.New("BIND adoption zone file changed during answer proof")
	}
	return proof, ctx.Err()
}
func VerifyBINDAdoptionSourceFiles(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	if j.InversePlan == nil || j.InversePlan.SourceBIND == nil {
		return errors.New("BIND adoption source proof missing")
	}
	if e := policy.ValidateSwitchJournal(j); e != nil {
		return e
	}
	zones, e := adoptionStaticZones(j)
	if e != nil {
		return e
	}
	// The main and packaged leaf are never changed by the producer.
	if e = VerifyInstalledBINDUnchangedConfigV2(ctx, policy, j, layout, gid); e != nil {
		return e
	}
	got, e := captureAdoptionFiles(ctx, zones)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(got, j.InversePlan.SourceBIND.Files) {
		return errors.New("BIND adoption source file differs from frozen evidence")
	}
	return nil
}
func VerifyBINDAdoptionSourceAnswers(ctx context.Context, j dnsengineartifact.SwitchJournalV1, probe BINDAdoptionSOAProbe) error {
	if ctx == nil || probe == nil || j.InversePlan == nil || j.InversePlan.SourceBIND == nil {
		return errors.New("BIND adoption answer proof missing")
	}
	for _, z := range j.InversePlan.SourceBIND.Zones {
		if z.Type == "hint" {
			continue
		}
		for _, network := range []string{"udp", "tcp"} {
			serial, e := probe(ctx, z.Name, network)
			if e != nil || serial != z.SOASerial {
				return errors.New("BIND adoption authoritative SOA changed")
			}
		}
	}
	return ctx.Err()
}
func VerifyBINDAdoptionSourceInventory(j dnsengineartifact.SwitchJournalV1, inventory []BINDAdoptionNativeZoneV1) error {
	zones, e := adoptionStaticZones(j)
	if e != nil {
		return e
	}
	return adoptionInventoryEqual(zones, inventory)
}
func captureAdoptionFiles(ctx context.Context, zones []bindconfig.StaticZone) ([]dnsengineartifact.BINDAdoptionSourceFileV1, error) {
	paths := map[string]bool{}
	for _, z := range zones {
		paths[z.File] = true
	}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]dnsengineartifact.BINDAdoptionSourceFileV1, 0, len(names))
	for _, name := range names {
		f, e := readAdoptionFile(ctx, name)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, nil
}
func readAdoptionFile(ctx context.Context, name string) (dnsengineartifact.BINDAdoptionSourceFileV1, error) {
	return readAdoptionFileChecked(ctx, name, adoptionSafeParents)
}

// The injected parent check lets fixture tests exercise file identity beneath
// a disposable /tmp parent; production always passes adoptionSafeParents.
func readAdoptionFileChecked(ctx context.Context, name string, checkParents func(string, uint32) error) (dnsengineartifact.BINDAdoptionSourceFileV1, error) {
	var empty dnsengineartifact.BINDAdoptionSourceFileV1
	if checkParents == nil {
		return empty, errors.New("BIND adoption parent verifier is absent")
	}
	if ctx == nil || ctx.Err() != nil || !strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return empty, errors.New("unsafe BIND adoption file path")
	}
	for _, suffix := range []string{".jnl", ".signed", ".jbk"} {
		if _, e := os.Lstat(name + suffix); e == nil {
			return empty, errors.New("BIND adoption zone has mutable sidecar")
		} else if !errors.Is(e, os.ErrNotExist) {
			return empty, e
		}
	}
	fd, e := unix.Openat2(unix.AT_FDCWD, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if e != nil {
		return empty, e
	}
	defer unix.Close(fd)
	var before, after, named unix.Stat_t
	if e = unix.Fstat(fd, &before); e != nil {
		return empty, e
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Size <= 0 || before.Size > 16<<20 || before.Mode&0o7777&^0o777 != 0 || before.Mode&0o022 != 0 || before.Uid != 0 || before.Gid > 1<<31-1 {
		return empty, errors.New("unsafe BIND adoption zone file metadata")
	}
	if e = checkParents(name, before.Uid); e != nil {
		return empty, e
	}
	if _, e = unix.Fgetxattr(fd, "system.posix_acl_access", nil); e == nil {
		return empty, errors.New("BIND adoption zone file has access ACL")
	} else if e != unix.ENODATA && e != unix.ENOTSUP && e != unix.EOPNOTSUPP {
		return empty, e
	}
	h := sha256.New()
	data := make([]byte, before.Size)
	for offset := 0; offset < len(data); {
		n, readErr := unix.Pread(fd, data[offset:], int64(offset))
		if readErr != nil {
			return empty, readErr
		}
		if n == 0 {
			return empty, errors.New("BIND adoption zone file ended early")
		}
		offset += n
	}
	if _, e = h.Write(data); e != nil {
		return empty, e
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "$INCLUDE") {
			return empty, errors.New("BIND adoption zone file has an include")
		}
	}
	if e = unix.Fstat(fd, &after); e != nil {
		return empty, e
	}
	same := before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode && before.Size == after.Size && before.Uid == after.Uid && before.Gid == after.Gid && before.Mtim == after.Mtim && before.Ctim == after.Ctim
	namedFD, e := unix.Openat2(unix.AT_FDCWD, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if e != nil {
		return empty, e
	}
	e = unix.Fstat(namedFD, &named)
	unix.Close(namedFD)
	if e != nil {
		return empty, e
	}
	if !same || named.Dev != after.Dev || named.Ino != after.Ino || named.Mtim != after.Mtim || named.Ctim != after.Ctim {
		return empty, errors.New("BIND adoption zone file moved during observation")
	}
	if e = checkParents(name, before.Uid); e != nil {
		return empty, e
	}
	for _, suffix := range []string{".jnl", ".signed", ".jbk"} {
		if _, e := os.Lstat(name + suffix); e == nil {
			return empty, errors.New("BIND adoption zone sidecar appeared")
		} else if !errors.Is(e, os.ErrNotExist) {
			return empty, e
		}
	}
	return dnsengineartifact.BINDAdoptionSourceFileV1{Path: name, SHA256: hex.EncodeToString(h.Sum(nil)), Size: before.Size, Mode: before.Mode & 0o777, UID: before.Uid, GID: before.Gid, Device: uint64(before.Dev), Inode: before.Ino}, ctx.Err()
}

// Every parent must be a stable directory owned by root or the file owner,
// without group/world write or ACLs. openat2 above separately excludes
// symlink traversal on the complete path.
func adoptionSafeParents(name string, owner uint32) error {
	current := "/"
	for _, part := range strings.Split(strings.Trim(name, "/"), "/")[:len(strings.Split(strings.Trim(name, "/"), "/"))-1] {
		current = path.Join(current, part)
		fd, e := unix.Openat2(unix.AT_FDCWD, current, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
		if e != nil {
			return e
		}
		var st unix.Stat_t
		e = unix.Fstat(fd, &st)
		if e == nil && (st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0o022 != 0 || (st.Uid != 0 && st.Uid != owner)) {
			e = errors.New("BIND adoption zone parent has unsafe ownership or mode")
		}
		if e == nil {
			_, xe := unix.Fgetxattr(fd, "system.posix_acl_access", nil)
			if xe == nil {
				e = errors.New("BIND adoption zone parent has access ACL")
			} else if xe != unix.ENODATA && xe != unix.ENOTSUP && xe != unix.EOPNOTSUPP {
				e = xe
			}
		}
		unix.Close(fd)
		if e != nil {
			return e
		}
	}
	return nil
}

func adoptionConfigBeforeStable(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) error {
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	snapshots, _, e := readBINDSwitchConfigPass(ctx, fd, j, layout, gid)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(snapshots, j.ConfigBefore) {
		return errors.New("BIND adoption owner config differs from frozen before-image")
	}
	return nil
}
