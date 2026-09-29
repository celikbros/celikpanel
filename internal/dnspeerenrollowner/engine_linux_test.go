//go:build linux

package dnspeerenrollowner

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/pdnspeerinspector"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"golang.org/x/crypto/ssh"
)

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func pdnsOptions() SecondaryOptions {
	return SecondaryOptions{PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11",
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogAccount: "celikpanel-peer-catalog-v1"}
}

// The BIND profile must reproduce the pre-engine rendering byte for byte.
// The expected values are the historical expressions over the BIND constants.
func TestBINDProfileRendersHistoricalBytes(t *testing.T) {
	o := SecondaryOptions{PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", CatalogName: "catalog-c000020a.celikpanel.invalid"}
	key := testKey(t)
	wantPolicy, _ := json.Marshal(bindpeerinspector.OwnerPolicyV1{Schema: bindpeerinspector.PolicySchemaV1,
		PrimaryIP: o.PrimaryIP, PeerIP: o.PeerIP, CatalogName: o.CatalogName, View: dnspeerproof.DefaultView})
	wantWrapper := []byte("#!/bin/sh\n[ \"${SSH_ORIGINAL_COMMAND-}\" = \"celikpanel-bind-peer-inspect-v1\" ] || exit 1\nunset SSH_ORIGINAL_COMMAND\nexec /usr/bin/sudo -n -- " + InspectorPath + "\n")
	wantSudoers := []byte(Account + " ALL=(root) NOPASSWD: " + InspectorPath + " \"\"\n")
	wantSSHD := []byte("Match User " + Account + "\n" +
		"    AuthenticationMethods publickey\n    PubkeyAuthentication yes\n" +
		"    PasswordAuthentication no\n    KbdInteractiveAuthentication no\n" +
		"    AuthorizedKeysFile " + AuthorizedKeysPath + "\n" +
		"    AuthorizedKeysCommand /usr/bin/false\n    AuthorizedKeysCommandUser nobody\n    PubkeyAcceptedAlgorithms ssh-ed25519\n" +
		"    ForceCommand " + WrapperPath + "\n" +
		"    DisableForwarding yes\n    PermitTTY no\n    PermitUserRC no\n")
	wantAuthorized := []byte("restrict,from=\"" + o.PrimaryIP + "\",command=\"" + WrapperPath + "\" " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + "\n")
	policy, wrapper, sudoers, sshd, authorized := renderFiles(o, key)
	for name, pair := range map[string][2][]byte{
		"policy": {policy, wantPolicy}, "wrapper": {wrapper, wantWrapper}, "sudoers": {sudoers, wantSudoers},
		"sshd": {sshd, wantSSHD}, "authorized": {authorized, wantAuthorized},
	} {
		if !bytes.Equal(pair[0], pair[1]) {
			t.Fatalf("BIND %s rendering changed:\n%s\n%s", name, pair[0], pair[1])
		}
	}
	if bindProfile.sshdInclude() != sshdInclude || bindProfile.receiptSchema != "celikpanel-bind-peer-sshd-include/v1" ||
		bindProfile.serviceUnit != "named.service" || bindProfile.ownerDir != "/etc/bind-peer-inspector" ||
		bindProfile.keyDir != "/etc/ssh/bind-peer-inspector" || bindProfile.home != HomePath ||
		bindProfile.fixedCommand != "celikpanel-bind-peer-inspect-v1" || bindProfile.restoreSSHDOnRevoke || bindProfile.cliArg != "" {
		t.Fatal("BIND profile layout changed")
	}
}

func TestPowerDNSPolicyIsTheInspectorFormat(t *testing.T) {
	o := pdnsOptions()
	policy, wrapper, sudoers, sshd, authorized := pdnsProfile.renderFiles(o, testKey(t))
	var decoded pdnspeerinspector.OwnerPolicyV1
	dec := json.NewDecoder(bytes.NewReader(policy))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(decoded)
	if !bytes.Equal(canonical, policy) {
		t.Fatal("policy is not the canonical inspector encoding")
	}
	digest, err := pdnspeerproof.CatalogMembersSHA256(o.PrimaryIP, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := pdnspeerproof.RequestV1{Schema: pdnspeerproof.RequestSchemaV1, MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32), DeletionGeneration: 3, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64), PrimaryIP: o.PrimaryIP, PeerIP: o.PeerIP, PeerIdentitySHA256: strings.Repeat("d", 64), CatalogName: o.CatalogName, CatalogSerial: 2, CatalogMembersSHA256: digest, DeletedZone: "s1-kill.test", View: dnspeerproof.DefaultView, Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060}
	if err := decoded.ValidateRequest(request); err != nil {
		t.Fatalf("inspector rejects the rendered owner policy: %v", err)
	}
	if !bytes.Contains(wrapper, []byte(`= "`+dnspeertransport.FixedPowerDNSCommand+`" ]`)) ||
		!bytes.HasSuffix(wrapper, []byte("exec /usr/bin/sudo -n -- "+PDNSInspectorPath+"\n")) || bytes.Contains(wrapper, []byte("bind")) {
		t.Fatalf("wrapper is not the PowerDNS forced command: %s", wrapper)
	}
	if string(sudoers) != Account+" ALL=(root) NOPASSWD: "+PDNSInspectorPath+" \"\"\n" {
		t.Fatalf("sudoers permits more than the inspector: %s", sudoers)
	}
	for _, want := range []string{"Match User " + Account, "AuthorizedKeysFile " + PDNSAuthorizedKeysPath, "ForceCommand " + PDNSWrapperPath, "PasswordAuthentication no", "DisableForwarding yes", "AuthorizedKeysCommand /usr/bin/false"} {
		if !strings.Contains(string(sshd), want) {
			t.Fatalf("missing sshd restriction %q", want)
		}
	}
	if _, err := pdnsProfile.enrolledPrimaryKey(authorized, o.PrimaryIP); err != nil {
		t.Fatal(err)
	}
	if _, err := bindProfile.enrolledPrimaryKey(authorized, o.PrimaryIP); err == nil {
		t.Fatal("BIND accepted a PowerDNS-bound key")
	}
	_, _, _, _, bindAuthorized := renderFiles(o, testKey(t))
	if _, err := pdnsProfile.enrolledPrimaryKey(bindAuthorized, o.PrimaryIP); err == nil {
		t.Fatal("PowerDNS accepted a BIND-bound key")
	}
	if pdnsProfile.policyPath != pdnspeerinspector.OwnerPolicyPath || pdnsProfile.serviceUnit != "pdns.service" ||
		pdnsProfile.ownerDir != filepath.Dir(pdnspeerinspector.OwnerPolicyPath) {
		t.Fatal("PowerDNS layout differs from the inspector's fixed policy path or service")
	}
}

func TestCatalogAccountRuleMatchesInspector(t *testing.T) {
	base := pdnspeerinspector.OwnerPolicyV1{Schema: pdnspeerinspector.PolicySchemaV1, PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", CatalogName: "catalog-c000020a.celikpanel.invalid"}
	digest, _ := pdnspeerproof.CatalogMembersSHA256(base.PrimaryIP, 2, nil)
	request := pdnspeerproof.RequestV1{Schema: pdnspeerproof.RequestSchemaV1, MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32), DeletionGeneration: 3, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64), PrimaryIP: base.PrimaryIP, PeerIP: base.PeerIP, PeerIdentitySHA256: strings.Repeat("d", 64), CatalogName: base.CatalogName, CatalogSerial: 2, CatalogMembersSHA256: digest, DeletedZone: "s1-kill.test", View: dnspeerproof.DefaultView, Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060}
	for _, account := range []string{"", "a", "celikpanel-peer-catalog-v1", "fixture_pdns", "Upper", "has space", "dot.ted", strings.Repeat("a", 128), strings.Repeat("a", 129), "ü"} {
		p := base
		p.CatalogAccount = account
		if got, want := validCatalogAccount(account), p.ValidateRequest(request) == nil; got != want {
			t.Fatalf("account %q: owner rule %v, inspector %v", account, got, want)
		}
	}
}

func TestEngineOptionsRequireOnlyTheirCatalogAccount(t *testing.T) {
	o := fixtureOptions(t)
	if _, _, err := bindProfile.validateOptions(o); err != nil {
		t.Fatalf("BIND input: %v", err)
	}
	if _, _, err := pdnsProfile.validateOptions(o); err == nil {
		t.Fatal("PowerDNS accepted a missing catalog account")
	}
	o.CatalogAccount = "celikpanel-peer-catalog-v1"
	if _, _, err := pdnsProfile.validateOptions(o); err != nil {
		t.Fatalf("PowerDNS input: %v", err)
	}
	if _, _, err := bindProfile.validateOptions(o); err == nil {
		t.Fatal("BIND accepted a PowerDNS catalog account")
	}
	o.CatalogAccount = "Bad Account"
	if _, _, err := pdnsProfile.validateOptions(o); err == nil {
		t.Fatal("PowerDNS accepted an account the inspector rejects")
	}
}

func pdnsReceipt(original []byte) ([]byte, []byte) {
	active := append(append([]byte(nil), original...), []byte(pdnsProfile.sshdInclude())...)
	oldHash, newHash := sha256.Sum256(original), sha256.Sum256(active)
	receipt, _ := json.Marshal(sshdReceipt{Schema: pdnsSSHDReceiptSchema, OriginalSHA256: hex.EncodeToString(oldHash[:]), ActiveSHA256: hex.EncodeToString(newHash[:])})
	return active, receipt
}

func TestPowerDNSSSHDResumeCheckpointsAndOwnerEdits(t *testing.T) {
	original := []byte("Port 22\nPasswordAuthentication no\n")
	active, receipt := pdnsReceipt(original)
	if !bytes.HasSuffix(active, []byte("\nInclude "+PDNSSSHDConfigPath+"\n")) {
		t.Fatal("PowerDNS include does not name its own Match file")
	}
	for _, tc := range []struct {
		name        string
		live, proof []byte
		published   bool
	}{
		{"backup-before-receipt", original, nil, false},
		{"receipt-before-rename", original, receipt, false},
		{"rename-before-return", active, receipt, true},
	} {
		got, err := pdnsProfile.classifySSHDPublication(original, tc.live, tc.proof)
		if err != nil || got != tc.published {
			t.Fatalf("%s: published=%v err=%v", tc.name, got, err)
		}
	}
	bindActive := append(append([]byte(nil), original...), []byte(sshdInclude)...)
	for _, tc := range []struct {
		name                string
		before, live, proof []byte
	}{
		{"unrecorded-publication", original, active, nil},
		{"owner-edit-after", original, append(append([]byte(nil), active...), []byte("# owner edit\n")...), receipt},
		{"bind-include", original, bindActive, receipt},
		{"duplicate-include", active, active, receipt},
	} {
		if _, err := pdnsProfile.classifySSHDPublication(tc.before, tc.live, tc.proof); err == nil {
			t.Fatalf("%s: unsafe resume accepted", tc.name)
		}
	}
	if err := pdnsProfile.validateSSHDReceipt(original, active, receipt); err != nil {
		t.Fatal(err)
	}
	if err := bindProfile.validateSSHDReceipt(original, active, receipt); err == nil {
		t.Fatal("BIND accepted a PowerDNS receipt")
	}
}

func TestPowerDNSRevokeRestoresOnlyTheRecordedPublication(t *testing.T) {
	original := []byte("Port 22\n")
	active, receipt := pdnsReceipt(original)
	if restore, same := pdnsProfile.sshdRestoreDecision(original, active, receipt); !restore || same {
		t.Fatal("exact publication was not restorable")
	}
	if restore, same := pdnsProfile.sshdRestoreDecision(original, original, receipt); restore || !same {
		t.Fatal("already original file was rewritten")
	}
	edited := append(append([]byte(nil), active...), []byte("# owner edit\n")...)
	if restore, same := pdnsProfile.sshdRestoreDecision(original, edited, receipt); restore || same {
		t.Fatal("owner edit would be overwritten")
	}
	if restore, _ := pdnsProfile.sshdRestoreDecision(original, active, append(append([]byte(nil), receipt...), ' ')); restore {
		t.Fatal("noncanonical receipt authorized a restore")
	}
	if !pdnsProfile.restoreSSHDOnRevoke || bindProfile.restoreSSHDOnRevoke {
		t.Fatal("revoke contract per engine changed")
	}
}

func TestPowerDNSAccountUsesItsOwnHome(t *testing.T) {
	pass := []byte(Account + ":x:991:991::" + PDNSHomePath + ":/bin/sh\n")
	shadow := []byte(Account + ":!:20000:0:99999:7:::\n")
	group := []byte(Account + ":x:991:\n")
	if err := pdnsProfile.validateRestrictedAccount(pass, shadow, []byte("991\n"), group); err != nil {
		t.Fatal(err)
	}
	if err := validateRestrictedAccount(pass, shadow, []byte("991\n"), group); err == nil {
		t.Fatal("BIND accepted the PowerDNS account home")
	}
	bindPass := []byte(Account + ":x:991:991::" + HomePath + ":/bin/sh\n")
	if err := pdnsProfile.validateRestrictedAccount(bindPass, shadow, []byte("991\n"), group); err == nil {
		t.Fatal("PowerDNS reused a BIND-created account")
	}
	if err := pdnsProfile.validateRestrictedAccount(pass, shadow, []byte("991 27\n"), group); err == nil {
		t.Fatal("supplementary group accepted")
	}
}

func TestOneEngineChannelPerSecondary(t *testing.T) {
	root := t.TempDir()
	for _, p := range []*profile{bindProfile, pdnsProfile} {
		if name, err := p.otherEngineArtifactAt(root); err != nil || name != "" {
			t.Fatalf("%s: empty host reported %q %v", p.label, name, err)
		}
	}
	for _, tc := range []struct {
		owner   *profile
		file    string
		blocked *profile
	}{
		{bindProfile, bindProfile.sshdReceipt, pdnsProfile},
		{bindProfile, bindProfile.policyPath, pdnsProfile},
		{pdnsProfile, pdnsProfile.authorized, bindProfile},
		{pdnsProfile, pdnsProfile.sudoers, bindProfile},
	} {
		dir := t.TempDir()
		name := filepath.Join(dir, tc.file)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := tc.blocked.otherEngineArtifactAt(dir); err != nil || got != tc.file {
			t.Fatalf("%s leftover %s not detected: %q %v", tc.owner.label, tc.file, got, err)
		}
		if got, err := tc.owner.otherEngineArtifactAt(dir); err != nil || got != "" {
			t.Fatalf("%s treated its own file as the other engine: %q %v", tc.owner.label, got, err)
		}
	}
	for _, e := range []string{"bind", "pdns"} {
		if engine, err := ParseEngine(e); err != nil || string(engine) != e {
			t.Fatal("reviewed engine rejected")
		}
	}
	for _, e := range []string{"", "BIND", "powerdns", "named"} {
		if _, err := ParseEngine(e); err == nil {
			t.Fatalf("engine %q accepted", e)
		}
	}
}
