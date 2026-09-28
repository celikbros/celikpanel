//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

func TestOwnerBINDAdoptionCommandKeepsExactRequestAndOwner(t *testing.T) {
	id := strings.Repeat("a", 32)
	good := []string{"recover-dns-bind-adoption", "--request-id", id, "--lang", "tr"}
	req, lang, ok := parseOwnerBINDAdoptionInverseArgs(good)
	if !ok || req != id || lang != "tr" {
		t.Fatal("exact owner command rejected")
	}
	for _, args := range [][]string{
		{"recover-dns-bind-adoption"},
		{"recover-dns-bind-adoption", "--request-id", "invalid"},
		{"recover-dns-bind-adoption", "--request-id", id, "--request-id", id},
		{"recover-dns-bind-adoption", "--request-id", id, "--root", "/tmp/foreign"},
		{"recover-dns-bind-adoption", "--request-id", id, "--lang", "fr"},
		{"recover-dns-bind-switch", "--request-id", id},
	} {
		if _, _, ok := parseOwnerBINDAdoptionInverseArgs(args); ok {
			t.Fatal("unexpected command accepted", args)
		}
	}
	var out, diag bytes.Buffer
	called := false
	run := func(context.Context, string) error { called = true; return errors.New("owner file differs") }
	if code := dispatchOwnerBINDAdoptionInverse(good, 1000, run, &out, &diag); code != exitNotOwner || called {
		t.Fatal("nonowner reached inverse")
	}
	good = good[:3]
	if code := dispatchOwnerBINDAdoptionInverse(good, 0, run, &out, &diag); code != exitUnavailable || !called || !strings.Contains(diag.String(), "same request") {
		t.Fatal("unknown result lost retained-evidence guidance")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := completeInstalledBINDAdoptionInverse(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled owner command reached host")
	}
}
func TestBINDAdoptionControlRequiresSameLocalNamed(t *testing.T) {
	line := "LISTEN 0 10 127.0.0.1:953 0.0.0.0:* users:((\"named\",pid=1234,fd=12))"
	if !bindAdoptionControlMatches(line, 1234) {
		t.Fatal("local named control rejected")
	}
	for _, bad := range []string{
		strings.ReplaceAll(line, "1234", "1235"),
		strings.ReplaceAll(line, "named", "other"),
		strings.ReplaceAll(line, "127.0.0.1", "192.0.2.10"),
		line + "\n" + line,
		strings.ReplaceAll(line, "LISTEN", "ESTAB"),
	} {
		if bindAdoptionControlMatches(bad, 1234) {
			t.Fatal("foreign control accepted", bad)
		}
	}
	absent := []byte("rndc: 'zonestatus' failed: not found\nno matching zone 'new.test' in any view\n")
	if !exactBINDAdoptionZoneUnloaded("new.test", absent, errors.New("exit status 1")) {
		t.Fatal("exact control absence rejected")
	}
	if exactBINDAdoptionZoneUnloaded("new.test", absent, nil) || exactBINDAdoptionZoneUnloaded("other.test", absent, errors.New("exit status 1")) {
		t.Fatal("foreign or successful control result accepted")
	}
}

func TestBINDAdoptionExactUnitsRejectsAppearedAlias(t *testing.T) {
	saved := dnsengineartifact.SwitchJournalV1{TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
	}}
	observed := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "pdns.service", LoadState: "not-found", ActiveState: "inactive"},
	}
	if !bindAdoptionExactUnits(observed, saved) {
		t.Fatal("unchanged Debian alias absence refused")
	}
	observed[1] = dnsenginerecovery.NativeUnitObservation{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	if bindAdoptionExactUnits(observed, saved) {
		t.Fatal("appeared alias accepted as switch compensation")
	}
	observed[1] = dnsenginerecovery.NativeUnitObservation{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"}
	observed[0].UnitFileState = "disabled"
	if bindAdoptionExactUnits(observed, saved) {
		t.Fatal("changed named service accepted")
	}
}
