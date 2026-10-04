//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/dnslistener"
	"github.com/alicelik/celikpanel/internal/dnswire"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// A session pins the process across all effects, including terminal publication.
// A later command may establish a fresh process proof but cannot reuse a stale PID.
type bindAdoptionNativeSession struct {
	pid               uint64
	started, endpoint string
}

// Running adoption never installs or changes a BIND unit. A formerly absent
// alias becoming loaded is an owner edit, unlike switch-install compensation.
func bindAdoptionExactUnits(units []dnsenginerecovery.NativeUnitObservation, j dnsengineartifact.SwitchJournalV1) bool {
	if len(units) != 3 || len(j.TargetUnitsBefore) != 2 {
		return false
	}
	seen := map[string]bool{}
	for _, frozen := range j.TargetUnitsBefore {
		if frozen.Name != "named.service" && frozen.Name != "bind9.service" || seen[frozen.Name] {
			return false
		}
		seen[frozen.Name] = true
		matched := false
		for _, current := range units[:2] {
			if current.Name == frozen.Name && current.LoadState == frozen.LoadState &&
				current.ActiveState == frozen.ActiveState && current.UnitFileState == frozen.UnitFileState {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return seen["named.service"] && seen["bind9.service"]
}

func (s *bindAdoptionNativeSession) guard(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) error {
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return err
	}
	if layout != bindroot.APT || j.InversePlan == nil || j.InversePlan.SourceBIND == nil {
		return errors.New("owner BIND inverse lacks the Debian source proof")
	}
	if _, err := dnsenginerecovery.BINDAdoptionInstallReceiptAbsent(policy, j); err != nil {
		return err
	}
	units, err := bindInverseUnits(ctx)
	if err != nil {
		return err
	}
	if !bindAdoptionExactUnits(units, j) || units[2].ActiveState != "inactive" {
		return errors.New("owner BIND unit state changed; recovery will not stop or start a DNS service")
	}
	pid, err := verifyInstalledBINDAdoptionRuntime(ctx, units[1].LoadState == "not-found")
	if err != nil {
		return err
	}
	started, err := verifyNativeBINDExecutable(pid, "/usr/sbin/named")
	if err != nil {
		return err
	}
	if s.pid != 0 && (s.pid != pid || s.started != started) {
		return errors.New("owner BIND process changed during no-stop recovery")
	}
	if err := dnsenginerecovery.ProbeAuthorityListeners(ctx, "named", pid, "", dnsenginerecovery.SSListenerRunner); err != nil {
		return err
	}
	address, err := dnsenginerecovery.ProbeAuthorityIPv4Address(ctx, "named", pid, dnsenginerecovery.SSListenerRunner)
	if err != nil {
		return err
	}
	endpoint := net.JoinHostPort(address, "53")
	if s.pid != 0 && endpoint != s.endpoint {
		return errors.New("owner BIND authority endpoint changed during recovery")
	}
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceFiles(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	if _, err := bindInverseConfigs(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	s.pid, s.started, s.endpoint = pid, started, endpoint
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceAnswers(ctx, j, s.probe); err != nil {
		return err
	}
	// File edits occurring during DNS queries are not silently loaded on reload.
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceFiles(ctx, policy, j, layout, gid); err != nil {
		return err
	}
	again, err := verifyNativeBINDExecutable(pid, "/usr/sbin/named")
	if err != nil || again != started {
		return errors.Join(errors.New("owner BIND process moved during source proof"), err)
	}
	return ctx.Err()
}
func (s *bindAdoptionNativeSession) probe(ctx context.Context, name, network string) (uint32, error) {
	switch network {
	case "udp":
		return dnswire.QueryAuthoritativeSOAUDP(ctx, s.endpoint, name)
	case "tcp":
		return dnswire.QueryAuthoritativeSOA(ctx, s.endpoint, name)
	default:
		return 0, errors.New("unexpected BIND proof transport")
	}
}
func bindAdoptionTargetReceiptAbsent(policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) (bool, error) {
	raw, present, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID})
	if err != nil || !present {
		return !present, err
	}
	state, _, err := dnsengineartifact.DecodeStateDocument(raw)
	if err != nil || !dnsengineartifact.ExactSwitchTargetStateV1(state, j) {
		return false, errors.Join(errors.New("owner BIND target state is not this accepted operation"), err)
	}
	return false, nil
}

// Native control proves these exact top-level zones are unloaded. A DNS
// REFUSED response alone cannot distinguish an absent zone from an ACL refusal.
func (s *bindAdoptionNativeSession) targetZonesAbsent(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
	if err := proveBINDAdoptionControl(ctx, s.pid); err != nil {
		return err
	}
	for _, z := range j.Zones {
		output, err := runBINDAdoptionTool(ctx, "/usr/sbin/rndc", true, "-k", "/etc/bind/rndc.key", "-s", "127.0.0.1", "-p", "953", "zonestatus", z.Domain)
		if !exactBINDAdoptionZoneUnloaded(z.Domain, output, err) {
			return fmt.Errorf("adoption-only zone %s is not proved unloaded by the same named process", z.Domain)
		}
	}
	return proveBINDAdoptionControl(ctx, s.pid)
}
func exactBINDAdoptionZoneUnloaded(zone string, output []byte, err error) bool {
	return err != nil && strings.TrimSpace(string(output)) == "rndc: 'zonestatus' failed: not found\nno matching zone '"+zone+"' in any view"
}
func proveBINDAdoptionControl(ctx context.Context, pid uint64) error {
	out, err := runBINDAdoptionTool(ctx, "/usr/bin/ss", false, "-H", "-lnpt", "( sport = :953 )")
	if err != nil || !bindAdoptionControlMatches(string(out), pid) {
		return errors.New("BIND control endpoint does not belong to the proved named process")
	}
	return nil
}
func bindAdoptionControlMatches(output string, pid uint64) bool {
	found := false
	seen := map[string]bool{}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 1 || len(lines) > 2 || pid == 0 {
		return false
	}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) != 6 || f[0] != "LISTEN" || !dnslistener.CanonicalWildcardPeerEndpoint(f[4]) {
			return false
		}
		for _, queue := range f[1:3] {
			n, err := strconv.ParseUint(queue, 10, 64)
			if err != nil || strconv.FormatUint(n, 10) != queue {
				return false
			}
		}
		process, actual, err := dnslistener.ParseCanonicalSSProcessField(f[5])
		if err != nil || process != "named" || actual != pid || seen[f[3]] {
			return false
		}
		switch f[3] {
		case "127.0.0.1:953":
			found = true
		case "[::1]:953":
		default:
			return false
		}
		seen[f[3]] = true
	}
	return found
}
func bindAdoptionFrozenGeneration(j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout) error {
	p, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return err
	}
	tree, err := p.LoadGeneration(j.TargetGeneration)
	if err != nil {
		return err
	}
	expected, err := bindInverseExpectedTarget(j, layout)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(tree.CurrentReceipt(), expected) {
		return errors.New("adopted BIND generation differs from frozen request")
	}
	return nil
}
func (s *bindAdoptionNativeSession) assess(ctx context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.BINDSwitchNativeState, error) {
	unknown := dnsenginerecovery.BINDSwitchNativeUnknown
	if err := s.guard(ctx, policy, j); err != nil {
		return unknown, err
	}
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return unknown, err
	}
	target, err := bindInversePointer(ctx, j, layout, gid)
	if err != nil {
		return unknown, err
	}
	if err := bindAdoptionFrozenGeneration(j, layout); err != nil {
		return unknown, err
	}
	before, err := bindInverseConfigs(ctx, policy, j, layout, gid)
	if err != nil {
		return unknown, err
	}
	absent, err := bindAdoptionTargetReceiptAbsent(policy, j)
	if err != nil {
		return unknown, err
	}
	installAbsent, err := dnsenginerecovery.BINDAdoptionInstallReceiptAbsent(policy, j)
	if err != nil {
		return unknown, err
	}
	if before && !target && absent && installAbsent {
		inventory, err := installedBINDAdoptionInventory(ctx)
		if err != nil {
			return unknown, err
		}
		if err := dnsenginerecovery.VerifyBINDAdoptionSourceInventory(j, inventory); err != nil {
			return unknown, err
		}
		if err := s.targetZonesAbsent(ctx, j); err == nil {
			if err := s.guard(ctx, policy, j); err != nil {
				return unknown, err
			}
			return dnsenginerecovery.BINDSwitchNativeRestored, nil
		} else if j.Phase == dnsengineartifact.SwitchPhaseRolledBack {
			return unknown, err
		}
	}
	if j.Phase != dnsengineartifact.SwitchPhaseRollingBack {
		return unknown, errors.New("rolled-back owner BIND still differs from its frozen preimage")
	}
	return dnsenginerecovery.BINDSwitchNativeNeedsRestore, nil
}
func (s *bindAdoptionNativeSession) restore(ctx context.Context, policy dnsengineartifact.JournalPolicy, owner servicemutationledger.FileOwner, j dnsengineartifact.SwitchJournalV1) error {
	layout, gid, err := installedBINDLayout()
	if err != nil {
		return err
	}
	guard := func(ctx context.Context) error { return s.guard(ctx, policy, j) }
	// This path intentionally has no systemctl start, stop, restart, enable or disable.
	if err := guard(ctx); err != nil {
		return err
	}
	if _, err := bindInversePointer(ctx, j, layout, gid); err != nil {
		return err
	}
	if err := bindAdoptionFrozenGeneration(j, layout); err != nil {
		return err
	}
	if err := dnsenginerecovery.RestoreInstalledBINDAdoptionConfigsV2(ctx, policy, j, layout, gid, guard); err != nil {
		return err
	}
	inventory, err := installedBINDAdoptionInventory(ctx)
	if err != nil {
		return err
	}
	if err := dnsenginerecovery.VerifyBINDAdoptionSourceInventory(j, inventory); err != nil {
		return err
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if before, err := bindInverseConfigs(ctx, policy, j, layout, gid); err != nil || !before {
		return errors.Join(errors.New("owner BIND original config was not restored before reload"), err)
	}
	if _, err := bindInverseSystemd(ctx, "/usr/bin/systemctl", "reload", "named.service"); err != nil {
		return fmt.Errorf("reload original owner BIND: %w", err)
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if err := s.targetZonesAbsent(ctx, j); err != nil {
		return err
	}
	p, err := binddns.NewOSPublisher(string(layout))
	if err != nil {
		return err
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if before, err := bindInverseConfigs(ctx, policy, j, layout, gid); err != nil || !before {
		return errors.Join(errors.New("owner BIND config changed before pointer restore"), err)
	}
	if err := p.RestorePointer(j.TargetGeneration, j.PreviousGeneration, j.HadPrevious); err != nil {
		return err
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if before, err := bindInverseConfigs(ctx, policy, j, layout, gid); err != nil || !before {
		return errors.Join(errors.New("owner BIND config changed before receipt removal"), err)
	}
	if err := dnsenginerecovery.RemoveExactBINDAdoptionTargetReceipt(policy, owner, j); err != nil {
		return err
	}
	if err := guard(ctx); err != nil {
		return err
	}
	if err := dnsenginerecovery.RemoveExactBINDAdoptionInstallReceipt(policy, owner, j); err != nil {
		return err
	}
	state, err := s.assess(ctx, policy, j)
	if err != nil || state != dnsenginerecovery.BINDSwitchNativeRestored {
		return errors.Join(errors.New("restored owner BIND could not be proved"), err)
	}
	return nil
}

type bindAdoptionToolOutput struct{ bytes.Buffer }

func (b *bindAdoptionToolOutput) Write(p []byte) (int, error) {
	if len(p) > 1<<20-b.Len() {
		return 0, errors.New("BIND inventory output exceeds bound")
	}
	return b.Buffer.Write(p)
}
func installedBINDAdoptionInventory(ctx context.Context) ([]dnsenginerecovery.BINDAdoptionNativeZoneV1, error) {
	tool := ""
	for _, candidate := range []string{"/usr/bin/named-checkconf", "/usr/sbin/named-checkconf"} {
		err := verifyBINDAdoptionTool(candidate)
		if err == nil {
			tool = candidate
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if tool == "" {
		return nil, errors.New("trusted named-checkconf is unavailable")
	}
	out, err := runBINDAdoptionTool(ctx, tool, false, "-l", "/etc/bind/named.conf")
	if err != nil {
		return nil, fmt.Errorf("native BIND inventory failed: %w", err)
	}
	return dnsenginerecovery.ParseBINDAdoptionNativeInventory(string(out))
}
func runBINDAdoptionTool(ctx context.Context, tool string, diagnostic bool, args ...string) ([]byte, error) {
	if tool != "/usr/bin/named-checkconf" && tool != "/usr/sbin/named-checkconf" && tool != "/usr/sbin/rndc" && tool != "/usr/bin/ss" {
		return nil, errors.New("unsupported BIND proof tool")
	}
	if err := verifyBINDAdoptionTool(tool); err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(c, tool, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin", "LC_ALL=C", "LANG=C"}
	var output bindAdoptionToolOutput
	command.Stdout = &output
	if diagnostic {
		command.Stderr = &output
	}
	err := command.Run()
	if check := verifyBINDAdoptionTool(tool); check != nil {
		return nil, check
	}
	return output.Bytes(), err
}
func verifyBINDAdoptionTool(name string) error {
	for current := name; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		var st unix.Stat_t
		if err := unix.Lstat(current, &st); err != nil {
			return err
		}
		if st.Uid != 0 || info.Mode().Perm()&0o022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("BIND inventory tool path is not root protected")
		}
		if current == name {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				return errors.New("BIND inventory tool is not executable")
			}
		} else if !info.IsDir() {
			return errors.New("BIND inventory tool parent is not a directory")
		}
		if current == "/" {
			break
		}
	}
	return nil
}
