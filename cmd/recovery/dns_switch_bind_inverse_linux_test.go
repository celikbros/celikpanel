//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

func TestOwnerBINDSwitchCommandRequiresExactIdentityAndOwner(t *testing.T) {
	id := strings.Repeat("a", 32)
	good := []string{"recover-dns-bind-switch", "--request-id", id}
	request, lang, ok := parseOwnerBINDSwitchInverseArgs(good)
	if !ok || request != id || lang != "en" {
		t.Fatal("valid exact command rejected")
	}
	for _, args := range [][]string{
		{"recover-dns-bind-switch"}, {"recover-dns-bind-switch", "--request-id", "bad"},
		{"recover-dns-bind-switch", "--request-id", id, "--request-id", id},
		{"recover-dns-bind-switch", "--request-id", id, "--root", "/tmp/other"},
		{"recover-dns-bind-switch", "--request-id", id, "--lang", "fr"},
	} {
		if _, _, ok := parseOwnerBINDSwitchInverseArgs(args); ok {
			t.Fatalf("unsafe command accepted: %v", args)
		}
	}
	called := false
	var out, diagnostic bytes.Buffer
	if code := dispatchOwnerBINDSwitchInverse(good, 1000, func(context.Context, string) error { called = true; return nil }, &out, &diagnostic); code != exitNotOwner || called {
		t.Fatal("non-owner reached mutation callback")
	}
	if code := dispatchOwnerBINDSwitchInverse(good, 0, func(_ context.Context, request string) error {
		called = true
		if request != id {
			t.Fatal("wrong request")
		}
		return errors.New("unknown source")
	}, &out, &diagnostic); code != exitUnavailable || !called || !strings.Contains(diagnostic.String(), "same request") {
		t.Fatal("unknown result lost same-request guidance")
	}
}

func TestInstalledBINDSwitchInverseRejectsInvalidRequestBeforeHostAccess(t *testing.T) {
	if err := completeInstalledBINDSwitchInverse(context.Background(), "invalid"); err == nil {
		t.Fatal("invalid request reached installed paths")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := completeInstalledBINDSwitchInverse(ctx, strings.Repeat("a", 32)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request reached host: %v", err)
	}
}

func TestJournalAbsentBINDInverseRequiresExactHistoricalVerdict(t *testing.T) {
	id := strings.Repeat("a", 32)
	now := time.Now().UTC().Truncate(time.Second)
	job := &transport.ServiceMutationJob{
		RequestID: id, OwnerID: strings.Repeat("b", 32), Kind: "dns_engine_switch", Target: string(transport.DNSEngineBIND),
		PackageName: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
		Status:      servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1, StartedAt: now.Add(-time.Minute), UpdatedAt: now, DeadlineAt: now.Add(time.Hour), FinishedAt: now, ErrorCode: "dns_engine_switch_rolled_back_by_owner_recovery",
		ErrorMessage: "The interrupted DNS engine switch was rolled back to the verified previous state.",
	}
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version, Jobs: map[string]*transport.ServiceMutationJob{id: job}}
	if err := classifyJournalAbsentBINDInverseLedger(ledger, id); err != nil {
		t.Fatal(err)
	}
	job.Target = string(transport.DNSEnginePowerDNS)
	if err := classifyJournalAbsentBINDInverseLedger(ledger, id); err == nil {
		t.Fatal("foreign target accepted")
	}
}

func TestBINDNativePreimageClassificationRequiresEveryUnit(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
	}, SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}}
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	if !bindInverseTargetPreimage(units, j) || !bindInverseSourcePreimage(units, j) {
		t.Fatal("exact units rejected")
	}
	units[1].UnitFileState = "enabled"
	if bindInverseTargetPreimage(units, j) {
		t.Fatal("changed target alias accepted")
	}
}

func TestBINDNativePreimageAcceptsOnlyFreshInstallSafeCompensation(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
		{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
	}}
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	if !bindInverseTargetPreimage(units, j) {
		t.Fatal("safe native package compensation was refused")
	}
	for _, changed := range []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "disabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "enabled"},
		{Name: "named.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"},
	} {
		modified := append([]dnsenginerecovery.NativeUnitObservation(nil), units...)
		modified[0] = changed
		if bindInverseTargetPreimage(modified, j) {
			t.Fatalf("owner-modified target unit accepted: %+v", changed)
		}
	}
}

func TestBINDNativeUnitCheckpointAllowsOnlyOperationTransitions(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
		{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
	}, SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}}}
	source := dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	for _, unit := range []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
	} {
		units := []dnsenginerecovery.NativeUnitObservation{unit, {Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}, source}
		if err := bindInverseAllowedNativeUnits(units, j); err != nil {
			t.Fatalf("operation checkpoint rejected: %v", err)
		}
	}
	source.ActiveState = "inactive"
	source.UnitFileState = "disabled"
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}, source,
	}
	if err := bindInverseAllowedNativeUnits(units, j); err != nil {
		t.Fatalf("activated target rejected: %v", err)
	}
	for _, change := range []func([]dnsenginerecovery.NativeUnitObservation){
		func(u []dnsenginerecovery.NativeUnitObservation) { u[0].UnitFileState = "enabled-runtime" },
		func(u []dnsenginerecovery.NativeUnitObservation) {
			u[0].LoadState = "masked"
			u[0].ActiveState = "inactive"
			u[0].UnitFileState = "masked-runtime"
		},
		func(u []dnsenginerecovery.NativeUnitObservation) { u[2].UnitFileState = "masked" },
		func(u []dnsenginerecovery.NativeUnitObservation) { u[2].ActiveState = "failed" },
	} {
		changed := append([]dnsenginerecovery.NativeUnitObservation(nil), units...)
		change(changed)
		if err := bindInverseAllowedNativeUnits(changed, j); err == nil {
			t.Fatalf("owner or unknown unit state accepted: %+v", changed)
		}
	}
	units[2].ActiveState = "active"
	units[2].UnitFileState = "enabled"
	if err := bindInverseAllowedNativeUnits(units, j); err == nil {
		t.Fatal("two active DNS authorities admitted")
	}
}

func TestBINDTargetRestoreDoesNotRunAfterUnchangedConfigFailure(t *testing.T) {
	stopped := false
	err := restoreBINDTargetAfterUnchangedProof(
		func() error { return errors.New("owner changed BIND main config") },
		func() error { stopped = true; return nil },
	)
	if err == nil || stopped {
		t.Fatalf("unproved BIND main config reached target stop: %v", err)
	}
	if err := restoreBINDTargetAfterUnchangedProof(nil, func() error { stopped = true; return nil }); err == nil || stopped {
		t.Fatal("missing BIND main config proof reached target stop")
	}
}

func TestBINDMaskedVendorProofUsesExactMasksAndPackageFiles(t *testing.T) {
	masked := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"},
		{Name: "bind9.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	var calls []string
	mask := func(unit string) error { calls = append(calls, "mask:"+unit); return nil }
	files := func() error { calls = append(calls, "vendor-files"); return nil }
	loaded := func() error { calls = append(calls, "loaded-unit"); return nil }
	if err := bindInverseVendorProofForUnits(masked, mask, files, loaded); err != nil {
		t.Fatal(err)
	}
	want := []string{"mask:named.service", "mask:bind9.service", "vendor-files"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("masked BIND proof path %v, want %v", calls, want)
	}
	calls = nil
	if err := bindInverseVendorProofForUnits(masked, func(string) error { return errors.New("foreign mask") }, files, loaded); err == nil || len(calls) != 0 {
		t.Fatalf("foreign mask reached vendor or loaded proof: %v %v", err, calls)
	}
	unmasked := append([]dnsenginerecovery.NativeUnitObservation(nil), masked...)
	unmasked[0] = dnsenginerecovery.NativeUnitObservation{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	unmasked[1] = dnsenginerecovery.NativeUnitObservation{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"}
	if err := bindInverseVendorProofForUnits(unmasked, mask, files, loaded); err != nil || !reflect.DeepEqual(calls, []string{"loaded-unit"}) {
		t.Fatalf("unmasked BIND failed full loaded identity path: %v %v", err, calls)
	}
	unmasked[0].LoadState = "masked"
	unmasked[0].UnitFileState = "masked-runtime"
	if err := bindInverseVendorProofForUnits(unmasked, mask, files, loaded); err == nil {
		t.Fatal("runtime mask accepted")
	}
}

func TestBINDPersistentMaskRequiresExactRootOwnedDevNullSymlink(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned mask fixture requires root")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	mask := filepath.Join(parent, "named.service")
	if err := os.Symlink("/dev/null", mask); err != nil {
		t.Fatal(err)
	}
	if err := bindInversePersistentMaskAt(fd, "named.service"); err != nil {
		t.Fatalf("exact mask refused: %v", err)
	}
	if err := os.Lchown(mask, 0, 12345); err != nil {
		t.Fatal(err)
	}
	if err := bindInversePersistentMaskAt(fd, "named.service"); err == nil {
		t.Fatal("foreign group mask accepted")
	}
	if err := os.Remove(mask); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/owner", mask); err != nil {
		t.Fatal(err)
	}
	if err := bindInversePersistentMaskAt(fd, "named.service"); err == nil {
		t.Fatal("foreign mask target accepted")
	}
	if err := bindInversePersistentMaskAt(fd, "ssh.service"); err == nil {
		t.Fatal("foreign unit mask accepted")
	}
}

func TestBINDActivePredecessorRequiresExactRollingBackUnits(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{Schema: dnsengineartifact.SwitchJournalSchemaV2, Phase: dnsengineartifact.SwitchPhaseRollingBack,
		SourceEngine: transport.DNSEnginePowerDNS, TargetEngine: transport.DNSEngineBIND, Topology: transport.DNSTopologyStandalone,
		InversePlan: &dnsengineartifact.BINDSwitchInversePlanV2{}}
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
	}
	if err := bindInversePredecessorActiveShape(j, units); err != nil {
		t.Fatal(err)
	}
	j.Phase = dnsengineartifact.SwitchPhaseTargetStarted
	if err := bindInversePredecessorActiveShape(j, units); err == nil {
		t.Fatal("pre-rollback phase accepted")
	}
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	for _, change := range []func([]dnsenginerecovery.NativeUnitObservation){
		func(u []dnsenginerecovery.NativeUnitObservation) { u[0].UnitFileState = "disabled" },
		func(u []dnsenginerecovery.NativeUnitObservation) { u[1].ActiveState = "inactive" },
		func(u []dnsenginerecovery.NativeUnitObservation) { u[2].ActiveState = "active" },
		func(u []dnsenginerecovery.NativeUnitObservation) { u[2].UnitFileState = "enabled" },
	} {
		modified := append([]dnsenginerecovery.NativeUnitObservation(nil), units...)
		change(modified)
		if err := bindInversePredecessorActiveShape(j, modified); err == nil {
			t.Fatalf("changed native state accepted: %+v", modified)
		}
	}
}

func TestBINDInversePlanLayoutUsesInstalledRootFamily(t *testing.T) {
	for _, test := range []struct {
		layout bindroot.Layout
		frozen string
		want   bool
	}{
		{bindroot.APT, "apt", true},
		{bindroot.Pacman, "pacman", true},
		{bindroot.APT, string(bindroot.APT), false},
		{bindroot.Pacman, string(bindroot.Pacman), false},
		{bindroot.APT, "pacman", false},
		{bindroot.Pacman, "apt", false},
		{bindroot.Layout("/tmp/owner"), "apt", false},
	} {
		if got := bindInversePlanLayoutMatches(test.layout, test.frozen); got != test.want {
			t.Fatalf("layout %q, frozen %q: got %v, want %v", test.layout, test.frozen, got, test.want)
		}
	}
}
