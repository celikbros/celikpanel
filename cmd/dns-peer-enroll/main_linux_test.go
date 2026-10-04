//go:build linux

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnspeerenrollowner"
)

type recorder struct {
	calls      []string
	activation activation
	options    dnspeerenrollowner.SecondaryOptions
	disabled   map[dnspeerenrollowner.Engine]bool
	revoke     dnspeerenrollowner.RevokeResult
}

func fakeBackends(r *recorder) backends {
	primary := func(engine dnspeerenrollowner.Engine) primaryBackend {
		name := string(engine)
		return primaryBackend{
			prepare: func() (prepared, error) {
				r.calls = append(r.calls, name+":prepare")
				return prepared{"id", "ssh-ed25519 AAAA", "digest"}, nil
			},
			activate: func(a activation) (string, error) {
				r.calls = append(r.calls, name+":activate")
				r.activation = a
				return "enrollment", nil
			},
			status: func() (primaryRecord, bool, error) {
				r.calls = append(r.calls, name+":status")
				return primaryRecord{EnrollmentID: "enrollment"}, r.disabled[engine], nil
			},
			revoke: func() error { r.calls = append(r.calls, name+":revoke"); return nil },
		}
	}
	return backends{
		primary: map[dnspeerenrollowner.Engine]primaryBackend{
			dnspeerenrollowner.EngineBIND: primary(dnspeerenrollowner.EngineBIND),
			dnspeerenrollowner.EnginePDNS: primary(dnspeerenrollowner.EnginePDNS),
		},
		secondary: secondaryBackend{
			install: func(e dnspeerenrollowner.Engine, o dnspeerenrollowner.SecondaryOptions) error {
				r.calls = append(r.calls, string(e)+":install")
				r.options = o
				return nil
			},
			resume: func(e dnspeerenrollowner.Engine, o dnspeerenrollowner.SecondaryOptions) error {
				r.calls = append(r.calls, string(e)+":resume")
				r.options = o
				return nil
			},
			status: func(e dnspeerenrollowner.Engine) (dnspeerenrollowner.Status, error) {
				r.calls = append(r.calls, string(e)+":secondary-status")
				return dnspeerenrollowner.Status{State: "disabled"}, nil
			},
			revoke: func(e dnspeerenrollowner.Engine) (dnspeerenrollowner.RevokeResult, error) {
				r.calls = append(r.calls, string(e)+":secondary-revoke")
				return r.revoke, nil
			},
			hostKey: func() (string, error) { r.calls = append(r.calls, "host-key"); return "hk", nil },
		},
	}
}

func invoke(t *testing.T, r *recorder, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runWith(args, &out, fakeBackends(r))
	return out.String(), err
}

// Historical BIND invocations have no selector and must keep their exact output.
func TestDefaultEngineKeepsHistoricalBINDOutput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		call string
		want string
	}{
		{[]string{"primary-prepare"}, "bind:prepare", `{"state":"prepared","credential_id":"id","public_key":"ssh-ed25519 AAAA","public_key_sha256":"digest","next_action":"Give only the displayed public key to the secondary owner; ask them to run secondary-install locally and return their independently reviewed Ed25519 SSH host-key SHA-256."}` + "\n"},
		{[]string{"primary-activate", "--credential-id", "c", "--primary-ip", "192.0.2.10", "--peer-ip", "192.0.2.11", "--catalog", "x", "--host-key-sha256", "h"}, "bind:activate", `{"state":"configured","enrollment_id":"enrollment","next_action":"For an admitted pending DNS deletion, use that exact operation's recovery action; enrollment alone does not enable a blocked DNS topology, and status polling does not retry the mutation."}` + "\n"},
		{[]string{"primary-revoke"}, "bind:revoke", `{"next_action":"The secondary owner must also run secondary-revoke. Pending deletion stays pending until the exact request is reverified.","state":"revoked"}` + "\n"},
		{[]string{"secondary-revoke"}, "bind:secondary-revoke", `{"next_action":"The primary owner must also run primary-revoke. Standard DNS transfer continues independently.","state":"revoked"}` + "\n"},
		{[]string{"secondary-host-key"}, "host-key", `{"host_key_sha256":"hk","next_action":"Compare this digest through a trusted channel before primary-activate; the primary must not learn it from an unauthenticated SSH connection."}` + "\n"},
	} {
		r := &recorder{}
		got, err := invoke(t, r, tc.args...)
		if err != nil || got != tc.want {
			t.Fatalf("%v: output changed\n got %q\nwant %q\nerr %v", tc.args, got, tc.want, err)
		}
		if len(r.calls) != 1 || r.calls[0] != tc.call {
			t.Fatalf("%v: calls %v", tc.args, r.calls)
		}
		// The explicit selector is equivalent to the default.
		r2 := &recorder{}
		explicit, err := invoke(t, r2, append(append([]string(nil), tc.args...), "--engine", "bind")...)
		if err != nil || explicit != tc.want || len(r2.calls) != 1 || r2.calls[0] != tc.call {
			t.Fatalf("%v --engine bind differs: %q %v %v", tc.args, explicit, r2.calls, err)
		}
	}
}

func TestPowerDNSSelectorRoutesEveryPrimaryAction(t *testing.T) {
	r := &recorder{}
	out, err := invoke(t, r, "primary-prepare", "--engine", "pdns")
	if err != nil || !strings.Contains(out, "secondary-install --engine pdns") {
		t.Fatalf("prepare guidance: %q %v", out, err)
	}
	if _, err := invoke(t, r, "primary-activate", "--engine=pdns", "--credential-id", "c", "--primary-ip", "192.0.2.10",
		"--peer-ip", "192.0.2.11", "--catalog", "x", "--host-key-sha256", "h"); err != nil {
		t.Fatal(err)
	}
	if r.activation.SSHUsername != dnspeerenrollowner.Account || r.activation.CredentialID != "c" {
		t.Fatalf("activation lost fixed account or inputs: %+v", r.activation)
	}
	out, err = invoke(t, r, "primary-revoke", "--engine", "pdns")
	if err != nil || !strings.Contains(out, "secondary-revoke --engine pdns") {
		t.Fatalf("revoke guidance: %q %v", out, err)
	}
	if _, err := invoke(t, r, "primary-status", "--engine", "pdns"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pdns:prepare", "pdns:activate", "pdns:revoke", "pdns:status"}
	if strings.Join(r.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v", r.calls)
	}
}

func TestPrimaryStatusNamesTheOtherEnrolledEngine(t *testing.T) {
	r := &recorder{disabled: map[dnspeerenrollowner.Engine]bool{dnspeerenrollowner.EngineBIND: true}}
	out, err := invoke(t, r, "primary-status")
	if err != nil || !strings.Contains(out, `"state":"disabled"`) || !strings.Contains(out, "primary-status --engine pdns") {
		t.Fatalf("other engine enrollment not surfaced: %q %v", out, err)
	}
	r = &recorder{disabled: map[dnspeerenrollowner.Engine]bool{dnspeerenrollowner.EngineBIND: true, dnspeerenrollowner.EnginePDNS: true}}
	out, err = invoke(t, r, "primary-status")
	if err != nil || out != `{"next_action":"Prepare an owner enrollment only if parentless deletion proof is required.","state":"disabled"}`+"\n" {
		t.Fatalf("historical disabled output changed: %q %v", out, err)
	}
}

func TestSecondaryInstallEngineAndCatalogAccount(t *testing.T) {
	base := []string{"--primary-ip", "192.0.2.10", "--peer-ip", "192.0.2.11", "--catalog", "x",
		"--primary-public-key", "/root/primary.pub", "--inspector", "/root/inspector"}
	r := &recorder{}
	out, err := invoke(t, r, append([]string{"secondary-install", "--engine", "pdns", "--catalog-account", "celikpanel-peer-catalog-v1"}, base...)...)
	if err != nil || r.calls[0] != "pdns:install" || r.options.CatalogAccount != "celikpanel-peer-catalog-v1" {
		t.Fatalf("pdns install: %v %+v %v", r.calls, r.options, err)
	}
	if strings.Contains(out, "verify a live") || !strings.Contains(out, "No owner command tests live authentication") ||
		!strings.Contains(out, "primary-activate --engine pdns") {
		t.Fatalf("install guidance claims an unperformed check: %q", out)
	}
	r = &recorder{}
	if _, err := invoke(t, r, append([]string{"secondary-resume", "--engine", "pdns", "--catalog-account", "a"}, base...)...); err != nil || r.calls[0] != "pdns:resume" {
		t.Fatalf("pdns resume: %v %v", r.calls, err)
	}
	r = &recorder{}
	out, err = invoke(t, r, append([]string{"secondary-install"}, base...)...)
	if err != nil || r.calls[0] != "bind:install" || r.options.CatalogAccount != "" || strings.Contains(out, "--engine") {
		t.Fatalf("default bind install: %v %+v %q %v", r.calls, r.options, out, err)
	}
	for _, args := range [][]string{
		append([]string{"secondary-install", "--catalog-account", "a"}, base...),
		append([]string{"secondary-install", "--engine", "bind", "--catalog-account", "a"}, base...),
		append([]string{"secondary-install", "--engine", "unbound"}, base...),
		append([]string{"secondary-install", "--engine", "pdns", "--engine", "bind"}, base...),
		{"secondary-status", "--engine"},
		{"secondary-status", "extra"},
		{"primary-prepare", "--engine", "PDNS"},
	} {
		r := &recorder{}
		if _, err := invoke(t, r, args...); err == nil || len(r.calls) != 0 {
			t.Fatalf("%v accepted: %v", args, r.calls)
		}
	}
}

func TestPowerDNSSecondaryRevokeReportsOwnerSSHOutcome(t *testing.T) {
	for _, tc := range []struct {
		result dnspeerenrollowner.RevokeResult
		want   string
	}{
		{dnspeerenrollowner.RevokeResult{SSHConfig: "restored"}, "restored and OpenSSH reloaded"},
		{dnspeerenrollowner.RevokeResult{SSHConfig: "preserved_owner_edit", Reason: "sshd_config was changed after enrollment and was left as is"}, "remove the line 'Include /etc/pdns-peer-inspector/sshd-match.conf' yourself"},
		{dnspeerenrollowner.RevokeResult{SSHConfig: "restore_failed", Reason: "sshd rejects the recorded original"}, "channel is disabled"},
	} {
		r := &recorder{revoke: tc.result}
		out, err := invoke(t, r, "secondary-revoke", "--engine", "pdns")
		if err != nil || !strings.Contains(out, `"ssh_config":"`+tc.result.SSHConfig+`"`) || !strings.Contains(out, tc.want) ||
			!strings.Contains(out, "primary-revoke --engine pdns") {
			t.Fatalf("%s: %q %v", tc.result.SSHConfig, out, err)
		}
	}
}

func TestBackendErrorsAreReturnedUnchanged(t *testing.T) {
	b := fakeBackends(&recorder{})
	b.primary[dnspeerenrollowner.EnginePDNS] = primaryBackend{prepare: func() (prepared, error) {
		return prepared{}, errors.New("a BIND peer enrollment exists")
	}}
	var out bytes.Buffer
	if err := runWith([]string{"primary-prepare", "--engine", "pdns"}, &out, b); err == nil || out.Len() != 0 {
		t.Fatal("exclusion error was hidden")
	}
}
