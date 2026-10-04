//go:build linux

package main

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Component tests for the PowerDNS-to-BIND rollback end state of
// recover-dns-bind-switch: the target ends under the package guard's
// persistent mask when the journal froze BIND absent or guard-masked, the
// exact staged generation is removed, an owner-modified tree is kept, and a
// journal that froze an existing BIND keeps restoring that preimage. systemd,
// sockets, the generation tree and files are the simulated host of
// dns_switch_bind_never_started_linux_test.go. These are not native evidence.

func requireGuardSealed(t *testing.T, f *neverStartedBINDHost) {
	t.Helper()
	for _, name := range []string{"named.service", "bind9.service"} {
		if u := f.units[name]; u.load != "masked" || u.file != "masked" || u.active != "inactive" || u.pid != 0 {
			t.Fatalf("%s is not under the guard's persistent mask: %+v", name, u)
		}
	}
}

func requirePDNSServing(t *testing.T, f *neverStartedBINDHost) {
	t.Helper()
	if pdns := f.units["pdns.service"]; pdns.load != "loaded" || pdns.active != "active" || pdns.file != "enabled" || pdns.pid == 0 {
		t.Fatalf("PowerDNS source is not serving at its frozen preimage: %+v", pdns)
	}
}

// startedBINDHost is the native state at a cut after BIND started (the
// critical target-started cell): BIND active and enabled on both names,
// PowerDNS stopped and disabled, the pointer on the target, managed config.
func startedBINDHost() *neverStartedBINDHost {
	f := newNeverStartedBINDHost(true, "after", true)
	f.units["named.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 7461}
	f.units["bind9.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 7461}
	f.generation = "staged"
	f.runtimeFiles = []string{"/var/cache/bind/managed-keys.bind.jnl (512 bytes, owner 104:104)", "/var/cache/bind/tmp-1K50Kr2S9C (0 bytes, owner 104:104)"}
	return f
}

func runOwnerBINDSwitchCommand(t *testing.T, f *neverStartedBINDHost, h releasedInverseHost, request, lang string) (int, string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := dispatchOwnerBINDSwitchInverse([]string{ownerBINDSwitchInverseCommand, "--request-id", request, "--lang", lang}, 0,
		func(ctx context.Context, request string) error { return f.run(ctx, h, request) }, &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}

func TestOwnerBINDSwitchInverseStartedTargetEndsUnderGuardMask(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := startedBINDHost()
	code, out, diagnostic := runOwnerBINDSwitchCommand(t, f, h, j.MutationRequestID, "en")
	if code != exitOK || diagnostic != "" {
		t.Fatalf("started target rollback failed: code=%d diagnostic=%q", code, diagnostic)
	}
	requireGuardSealed(t, f)
	requirePDNSServing(t, f)
	if f.config != "before" || f.pointerTarget || f.generation != "none" {
		t.Fatalf("config, pointer or generation not restored: %+v", f)
	}
	wantEffects := []string{"restore BIND config before-images", "restore BIND predecessor pointer", "remove staged BIND generation"}
	if !reflect.DeepEqual(f.effects, wantEffects) {
		t.Fatalf("effects = %q, want %q", f.effects, wantEffects)
	}
	// BIND is stopped and disabled before it is masked, never started again.
	var named []string
	for _, call := range f.systemctl {
		if strings.HasSuffix(call, " named.service") {
			named = append(named, call)
		}
		if strings.HasPrefix(call, "start named") || strings.HasPrefix(call, "enable named") {
			t.Fatalf("rollback re-enabled or started BIND: %q", call)
		}
	}
	wantNamed := []string{"stop named.service", "unmask named.service", "unmask --runtime named.service", "disable named.service", "mask named.service", "unmask --runtime named.service"}
	if !reflect.DeepEqual(named, wantNamed) {
		t.Fatalf("named systemctl sequence = %q, want %q", named, wantNamed)
	}
	if _, present := h.files(t)["dns-engine-switch-journal.json"]; present {
		t.Fatal("journal not retired")
	}
	for _, want := range []string{
		"reached its terminal verdict",
		"Restored: the PowerDNS service",
		"under the package guard's persistent mask, inactive and not enabled",
		"Removed: the staged BIND generation " + j.TargetGeneration,
		"Intentionally kept as rollback standby: the installed bind9 packages, the rndc key and the BIND install-ownership record. This command does not remove packages.",
		"Left in BIND's working directory /var/cache/bind, which is outside the managed BIND root",
		"managed-keys.bind.jnl", "tmp-1K50Kr2S9C",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("final output lacks %q:\n%s", want, out)
		}
	}
}

func TestOwnerBINDSwitchInverseSummaryIsTurkish(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := startedBINDHost()
	code, out, _ := runOwnerBINDSwitchCommand(t, f, h, j.MutationRequestID, "tr")
	if code != exitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"Geri yüklenenler:", "paket korumasının kalıcı maskesi altında", "Silinenler: bu geçişin hazırladığı " + j.TargetGeneration,
		"Yedek olarak bilerek bırakılanlar", "Bu komut paket kaldırmaz.", "yönetilen BIND kökünün dışında olduğu için silinmedi"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Turkish output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Restored:") {
		t.Fatalf("Turkish output contains English summary:\n%s", out)
	}
}

// A target that never started keeps its guard mask with no target systemctl
// call; the staged generation is still removed.
func TestOwnerBINDSwitchInverseNeverStartedTargetRemovesStagedGeneration(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := newNeverStartedBINDHost(true, "after", true)
	f.generation = "staged"
	if code, out, diagnostic := runOwnerBINDSwitchCommand(t, f, h, j.MutationRequestID, "en"); code != exitOK ||
		!strings.Contains(out, "Removed: the staged BIND generation") {
		t.Fatalf("code=%d out=%q diagnostic=%q", code, out, diagnostic)
	}
	for _, call := range f.systemctl {
		if strings.Contains(call, "named.service") || strings.Contains(call, "bind9.service") {
			t.Fatalf("sealed target received %q", call)
		}
	}
	requireGuardSealed(t, f)
	requirePDNSServing(t, f)
	if f.generation != "none" {
		t.Fatal("staged generation left")
	}
}

// An owner-modified generation tree is kept and recorded; the rollback still
// completes.
func TestOwnerBINDSwitchInverseKeepsOwnerModifiedGeneration(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := startedBINDHost()
	f.generation = "retained"
	code, out, diagnostic := runOwnerBINDSwitchCommand(t, f, h, j.MutationRequestID, "en")
	if code != exitOK || diagnostic != "" {
		t.Fatalf("owner-modified tree failed the rollback: code=%d diagnostic=%q", code, diagnostic)
	}
	for _, effect := range f.effects {
		if strings.Contains(effect, "generation") {
			t.Fatalf("owner-modified tree was removed: %q", f.effects)
		}
	}
	if f.generation != "retained" || strings.Contains(out, "Removed:") ||
		!strings.Contains(out, "Not removed: the BIND generation "+j.TargetGeneration) ||
		!strings.Contains(out, "not exactly what this switch staged (BIND generation contains unexpected top-level entries)") {
		t.Fatalf("retained tree not recorded:\n%s", out)
	}
	requireGuardSealed(t, f)
	if _, present := h.files(t)["dns-engine-switch-journal.json"]; present {
		t.Fatal("journal not retired")
	}
}

// A run interrupted after every native effect but before the generation was
// removed resumes at rolling-back: assess reports the exact staged tree as
// incomplete work, and the replay changes nothing else.
func TestOwnerBINDSwitchInverseResumesInterruptedGenerationRemoval(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := newNeverStartedBINDHost(false, "before", false)
	f.generation = "staged"
	if err := f.run(context.Background(), h, j.MutationRequestID); err != nil {
		t.Fatalf("resume refused: %v", err)
	}
	if len(f.systemctl) != 0 || !reflect.DeepEqual(f.effects, []string{"remove staged BIND generation"}) {
		t.Fatalf("resume replayed more than the removal: systemctl=%q effects=%q", f.systemctl, f.effects)
	}
	// At the rolled-back checkpoint (an earlier binary), a staged tree is left
	// and recorded instead of blocking the retirement.
	h, j = stageNeverStartedBINDSwitch(t, nil)
	j.Phase = dnsengineartifact.SwitchPhaseRolledBack
	raw, err := h.policy.EncodeSwitchJournal(j)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, "dns-engine-switch-journal.json", raw)
	f = newNeverStartedBINDHost(false, "before", false)
	f.generation = "staged"
	code, out, diagnostic := runOwnerBINDSwitchCommand(t, f, h, j.MutationRequestID, "en")
	if code != exitOK || len(f.effects) != 0 || !strings.Contains(out, "An earlier recovery run had already reached the rolled-back checkpoint") {
		t.Fatalf("rolled-back checkpoint with staged tree: code=%d effects=%q out=%q diagnostic=%q", code, f.effects, out, diagnostic)
	}
}

// A journal that froze an existing BIND (named installed and disabled) keeps
// restoring that exact preimage: no mask, no generation removal.
func TestOwnerBINDSwitchInverseExistingBINDPreimageUnchanged(t *testing.T) {
	targets := []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
	}
	h, j := stageNeverStartedBINDSwitch(t, targets)
	if dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) {
		t.Fatal("existing BIND preimage entered the standby class")
	}
	f := newNeverStartedBINDHost(true, "after", true)
	f.units["named.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 7461}
	f.units["bind9.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 7461}
	f.generation = "staged"
	code, out, diagnostic := runOwnerBINDSwitchCommand(t, f, h, j.MutationRequestID, "en")
	if code != exitOK {
		t.Fatalf("existing preimage rollback failed: %d %q", code, diagnostic)
	}
	for _, call := range f.systemctl {
		if strings.HasPrefix(call, "mask ") {
			t.Fatalf("existing BIND preimage was masked: %q", call)
		}
	}
	if u := f.units["named.service"]; u.load != "loaded" || u.file != "disabled" || u.active != "inactive" {
		t.Fatalf("named not at its frozen preimage: %+v", u)
	}
	if f.generation != "staged" || strings.Contains(out, "Removed:") || strings.Contains(out, "Intentionally kept") ||
		!strings.Contains(out, "back in the state recorded before the switch") {
		t.Fatalf("existing preimage summary or residue changed:\n%s", out)
	}
	requirePDNSServing(t, f)
}

// Rollback, then the same switch again: the forward path freezes the guard's
// mask as the retry's target preimage (both names masked/inactive). That
// journal is in the standby class, so a rollback of the retry after BIND
// started ends under the same mask instead of failing the loaded-unit proof.
func TestOwnerBINDSwitchInverseRollbackThenSameSwitchAgain(t *testing.T) {
	h, j := stageNeverStartedBINDSwitch(t, nil)
	f := startedBINDHost()
	if err := f.run(context.Background(), h, j.MutationRequestID); err != nil {
		t.Fatalf("first rollback: %v", err)
	}
	requireGuardSealed(t, f)

	retryTargets := []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: f.units["bind9.service"].load, ActiveState: f.units["bind9.service"].active, UnitFileState: f.units["bind9.service"].file},
		{Name: "named.service", LoadState: f.units["named.service"].load, ActiveState: f.units["named.service"].active, UnitFileState: f.units["named.service"].file},
	}
	h2, retry := stageNeverStartedBINDSwitch(t, retryTargets)
	if !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(retry) {
		t.Fatal("retry journal that froze the guard mask is outside the standby class")
	}
	// The retry lifted the mask, started BIND and was cut at target-started.
	f.units["named.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 8100}
	f.units["bind9.service"] = &simulatedDNSUnit{load: "loaded", active: "active", file: "enabled", pid: 8100}
	f.units["pdns.service"] = &simulatedDNSUnit{load: "loaded", active: "inactive", file: "disabled"}
	f.pointerTarget, f.config, f.generation = true, "after", "staged"
	f.systemctl, f.effects = nil, nil
	if err := f.run(context.Background(), h2, retry.MutationRequestID); err != nil {
		t.Fatalf("rollback of the retried switch refused: %v", err)
	}
	requireGuardSealed(t, f)
	requirePDNSServing(t, f)
	if f.generation != "none" {
		t.Fatal("retry generation left")
	}
	// A retry cut before BIND started keeps the mask with no target call.
	h3, retry3 := stageNeverStartedBINDSwitch(t, retryTargets)
	f3 := newNeverStartedBINDHost(true, "after", true)
	if err := f3.run(context.Background(), h3, retry3.MutationRequestID); err != nil {
		t.Fatalf("pre-start rollback of the retry refused: %v", err)
	}
	for _, call := range f3.systemctl {
		if strings.Contains(call, "named.service") || strings.Contains(call, "bind9.service") {
			t.Fatalf("sealed retry target received %q", call)
		}
	}
	requireGuardSealed(t, f3)
}
