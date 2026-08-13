//go:build linux

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestSecurityAuditFirewallParserRequiresOneDefaultDropInputPolicy(t *testing.T) {
	raw := []byte(`table inet celikpanel_fw {
	chain input {
		type filter hook input priority filter; policy drop;
		iif "lo" accept
		ct state established,related accept
		ct state invalid drop
		meta l4proto icmp accept
		meta l4proto ipv6-icmp accept
		tcp dport { 22, 443 } accept
		udp dport 53 accept
	}
}`)
	drop, known, tcp, udp, allowKnown := parseSecurityAuditFirewallRules(raw)
	if !drop || !known || !allowKnown ||
		!reflect.DeepEqual(tcp, []int{22, 443}) || !reflect.DeepEqual(udp, []int{53}) {
		t.Fatalf("firewall parse = drop:%v known:%v allow:%v tcp:%v udp:%v", drop, known, allowKnown, tcp, udp)
	}

	ambiguous := append(raw, []byte("\nchain surprise {\n  tcp dport 9999 accept\n}\n")...)
	_, _, _, _, allowKnown = parseSecurityAuditFirewallRules(ambiguous)
	if allowKnown {
		t.Fatal("unknown accept rule was treated as an effective allowlist")
	}

	multipleHooks := append(raw, []byte("\nchain input {\n  type filter hook input priority 10; policy accept;\n}\n")...)
	_, _, _, _, allowKnown = parseSecurityAuditFirewallRules(multipleHooks)
	if allowKnown {
		t.Fatal("multiple input base chains were treated as an authoritative allowlist")
	}

	for _, rule := range []string{
		`iifname != "lo" accept`,
		`ip saddr 0.0.0.0/0 accept comment "broad rule"`,
		`meta l4proto tcp counter accept`,
		`tcp dport 443 accept counter`,
		`ip saddr 192.0.2.1 drop`,
	} {
		modified := []byte(strings.Replace(string(raw), `iif "lo" accept`, rule, 1))
		_, _, _, _, allowKnown = parseSecurityAuditFirewallRules(modified)
		if allowKnown {
			t.Fatalf("noncanonical rule %q was treated as an effective allowlist", rule)
		}
	}
}

func TestSecurityAuditFirewallPersistenceRequiresEnabledRestoreUnit(t *testing.T) {
	tests := []struct {
		name      string
		unitState string
		commandOK bool
		want      string
	}{
		{name: "enabled", unitState: "enabled", commandOK: true, want: firewallPersistenceReady},
		{name: "disabled", unitState: "disabled", want: firewallPersistenceMissing},
		{name: "masked", unitState: "masked", want: firewallPersistenceInvalid},
		{name: "ambiguous", unitState: "static", commandOK: true, want: firewallPersistenceUnverified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifySecurityAuditFirewallPersistence(firewallPersistenceReady, test.unitState, test.commandOK); got != test.want {
				t.Fatalf("persistence = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSecurityAuditFirewallSnapshotClassificationFailsClosed(t *testing.T) {
	valid := encodeFirewallSnapshot([]int{443}, []int{53}, []int{22})
	verified := securityAuditFirewallInspection{
		live: true, liveKnown: true, defaultDrop: true, policyKnown: true, allowKnown: true,
		tcp: []int{22, 443}, udp: []int{53},
	}
	tests := []struct {
		name       string
		raw        []byte
		state      securityAuditObjectState
		inspection securityAuditFirewallInspection
		want       string
	}{
		{name: "live valid", raw: valid, state: securityAuditObjectOK, inspection: verified, want: firewallPersistenceReady},
		{name: "firewall off", raw: valid, state: securityAuditObjectOK, inspection: securityAuditFirewallInspection{liveKnown: true}, want: firewallPersistenceStale},
		{name: "live missing", state: securityAuditObjectMissing, inspection: securityAuditFirewallInspection{live: true, liveKnown: true}, want: firewallPersistenceMissing},
		{name: "off missing", state: securityAuditObjectMissing, inspection: securityAuditFirewallInspection{liveKnown: true}, want: firewallPersistenceDisabled},
		{name: "invalid", raw: []byte("not a snapshot"), state: securityAuditObjectOK, inspection: verified, want: firewallPersistenceInvalid},
		{name: "ambiguous live state", raw: valid, state: securityAuditObjectOK, want: firewallPersistenceUnverified},
		{name: "ambiguous live policy", raw: valid, state: securityAuditObjectOK, inspection: securityAuditFirewallInspection{live: true, liveKnown: true}, want: firewallPersistenceUnverified},
		{name: "live more open", raw: valid, state: securityAuditObjectOK, inspection: securityAuditFirewallInspection{live: true, liveKnown: true, defaultDrop: true, policyKnown: true, allowKnown: true, tcp: []int{22, 443, 8443}, udp: []int{53}}, want: firewallPersistenceStale},
		{name: "snapshot more open", raw: valid, state: securityAuditObjectOK, inspection: securityAuditFirewallInspection{live: true, liveKnown: true, defaultDrop: true, policyKnown: true, allowKnown: true, tcp: []int{22}, udp: []int{53}}, want: firewallPersistenceStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifySecurityAuditFirewallSnapshot(test.raw, test.state, test.inspection); got != test.want {
				t.Fatalf("persistence = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSecurityAuditListenersDistinguishLeakAndUnusedRule(t *testing.T) {
	raw := []byte("tcp LISTEN 0 4096 0.0.0.0:22 0.0.0.0:*\n" +
		"tcp LISTEN 0 4096 127.0.0.1:9000 0.0.0.0:*\n" +
		"tcp LISTEN 0 4096 [::]:8080 [::]:*\n" +
		"udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:*\n")
	listeners, err := parseSecurityAuditPublicListeners(raw)
	if err != nil {
		t.Fatal(err)
	}
	result := compareSecurityAuditListeners(listeners, []int{22, 443}, []int{53})
	if result.Check.Status != transport.SecurityAuditStatusFail || len(result.Findings) != 2 {
		t.Fatalf("listener result = %#v", result)
	}
	want := []transport.SecurityAuditListenerFinding{
		{Protocol: "tcp", Port: 443, Status: transport.SecurityAuditStatusWarning, Code: transport.SecurityAuditAllowedNoListener},
		{Protocol: "tcp", Port: 8080, Status: transport.SecurityAuditStatusFail, Code: transport.SecurityAuditListenerNotAllowed},
	}
	if !reflect.DeepEqual(result.Findings, want) {
		t.Fatalf("findings = %#v, want %#v", result.Findings, want)
	}
}

func TestSecurityAuditSSHDParserUsesEffectiveKeyOnlyPolicy(t *testing.T) {
	policy, err := parseSecurityAuditSSHDPolicy([]byte(
		"passwordauthentication no\nkbdinteractiveauthentication no\npermitrootlogin prohibit-password\n" +
			"pubkeyauthentication yes\nhostbasedauthentication no\ngssapiauthentication no\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if policy.passwordAuthentication != "no" ||
		policy.keyboardInteractiveAuthentication != "no" ||
		policy.permitRootLogin != "prohibit-password" ||
		policy.pubkeyAuthentication != "yes" ||
		policy.hostbasedAuthentication != "no" ||
		policy.gssapiAuthentication != "no" {
		t.Fatalf("policy = %#v", policy)
	}
	if _, err := parseSecurityAuditSSHDPolicy([]byte(
		"passwordauthentication no\nkbdinteractiveauthentication no\n",
	)); err == nil {
		t.Fatal("missing effective root policy was accepted")
	}
}

func TestSecurityAuditSSHDPolicyClassificationNeverClaimsLivePass(t *testing.T) {
	safe := securityAuditSSHDPolicy{
		passwordAuthentication: "no", keyboardInteractiveAuthentication: "no",
		permitRootLogin: "prohibit-password", pubkeyAuthentication: "yes",
		hostbasedAuthentication: "no", gssapiAuthentication: "no",
	}
	if got := classifySecurityAuditSSHDPolicy(safe); got.Code != "ssh_policy_live_unverified" ||
		got.Status != transport.SecurityAuditStatusUnknown {
		t.Fatalf("safe on-disk policy classification=%#v", got)
	}
	noPubkey := safe
	noPubkey.pubkeyAuthentication = "no"
	if got := classifySecurityAuditSSHDPolicy(noPubkey); got.Code != "ssh_policy_ambiguous" ||
		got.Status != transport.SecurityAuditStatusUnknown {
		t.Fatalf("pubkey-disabled policy classification=%#v", got)
	}
	rootYes := safe
	rootYes.permitRootLogin = "yes"
	if got := classifySecurityAuditSSHDPolicy(rootYes); got.Code != "ssh_root_login_unrestricted" ||
		got.Status != transport.SecurityAuditStatusWarning {
		t.Fatalf("unrestricted-root policy classification=%#v", got)
	}
}

func TestSecurityAuditSSHDConfigParserRejectsConditionalPolicy(t *testing.T) {
	includes, hasMatch, err := parseSecurityAuditSSHDConfig([]byte(
		"Include /etc/ssh/sshd_config.d/*.conf\n" +
			"PasswordAuthentication no\n" +
			"Match User root\n" +
			"  PasswordAuthentication yes\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasMatch || !reflect.DeepEqual(includes, []string{"/etc/ssh/sshd_config.d/*.conf"}) {
		t.Fatalf("includes=%#v hasMatch=%v", includes, hasMatch)
	}
}

func TestSecurityAuditSSHDConfigParserFailsClosedOnUnsupportedIncludeSyntax(t *testing.T) {
	for _, raw := range []string{
		"Include \"/etc/ssh/sshd_config.d/*.conf\"\n",
		"Include /etc/ssh/sshd_config.d/*.conf \\\\\ncontinued\n",
		"Include /tmp/../etc/ssh/sshd_config\n",
	} {
		includes, _, err := parseSecurityAuditSSHDConfig([]byte(raw))
		if err == nil && len(includes) == 1 {
			if _, resolveErr := resolveSecurityAuditSSHDInclude("/etc/ssh", includes[0]); resolveErr == nil {
				t.Fatalf("unsupported include syntax accepted: %q", raw)
			}
		}
	}
}

func TestSecurityAuditSSHDIncludeResolutionIsConfined(t *testing.T) {
	if got, err := resolveSecurityAuditSSHDInclude("/etc/ssh", "sshd_config.d/*.conf"); err != nil ||
		got != "/etc/ssh/sshd_config.d/*.conf" {
		t.Fatalf("resolved=%q err=%v", got, err)
	}
	for _, include := range []string{"/etc/ssh", "/etc/ssh/../passwd", "/tmp/*.conf", "../passwd"} {
		if _, err := resolveSecurityAuditSSHDInclude("/etc/ssh", include); err == nil {
			t.Fatalf("escaping include accepted: %q", include)
		}
	}
}

func TestSecurityAuditReleaseFloorRequiresCanonicalBytes(t *testing.T) {
	raw := []byte("format=celikpanel-release-sequence-floor-v1\nsequence=16\nversion=v0.1.0-alpha.16\n")
	sequence, version, ok := parseSecurityAuditReleaseFloor(raw)
	if !ok || sequence != "16" || version != "v0.1.0-alpha.16" {
		t.Fatalf("floor = %q %q %v", sequence, version, ok)
	}
	for _, invalid := range [][]byte{
		[]byte("format=celikpanel-release-sequence-floor-v1\nsequence=016\nversion=v0.1.0-alpha.16\n"),
		append(append([]byte(nil), raw...), '\n'),
	} {
		if _, _, ok := parseSecurityAuditReleaseFloor(invalid); ok {
			t.Fatalf("noncanonical floor accepted: %q", invalid)
		}
	}
}

func TestSecurityAuditReleaseFloorMustMatchRunningBuild(t *testing.T) {
	for _, test := range []struct {
		floor, build string
		want         bool
	}{
		{floor: "v0.1.0-alpha.16", build: "v0.1.0-alpha.16", want: true},
		{floor: "v0.1.0-alpha.15", build: "v0.1.0-alpha.16", want: false},
		{floor: "v0.1.0-alpha.16", build: " v0.1.0-alpha.16 ", want: true},
	} {
		if got := securityAuditReleaseFloorMatchesBuild(test.floor, test.build); got != test.want {
			t.Fatalf("floor=%q build=%q got=%v want=%v", test.floor, test.build, got, test.want)
		}
	}
}
