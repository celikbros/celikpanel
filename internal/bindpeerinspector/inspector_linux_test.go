package bindpeerinspector

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

const testTime int64 = 1800000000

func request(t *testing.T) dnspeerproof.RequestV1 {
	t.Helper()
	digest, err := dnspeerproof.CatalogMembersSHA256("192.0.2.10", 12, []string{"other.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return dnspeerproof.RequestV1{
		Schema:            dnspeerproof.RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 17, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64),
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 12, CatalogMembersSHA256: digest,
		DeletedZone: "gone.example.test", View: dnspeerproof.DefaultView,
		Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: testTime, ExpiresAtUnix: testTime + 60,
	}
}

type sequenceReader struct {
	snapshots []Snapshot
	calls     int
}

func (s *sequenceReader) Read(_ context.Context, _ dnspeerproof.RequestV1) (Snapshot, error) {
	current := s.snapshots[s.calls]
	s.calls++
	return current, nil
}

type fixedPolicy struct {
	value  OwnerPolicyV1
	hashes []string
	calls  int
}

func (p *fixedPolicy) Read(context.Context) (OwnerPolicyV1, string, error) {
	hash := p.hashes[p.calls]
	p.calls++
	return p.value, hash, nil
}
func policyFor(r dnspeerproof.RequestV1) *fixedPolicy {
	return &fixedPolicy{value: OwnerPolicyV1{Schema: PolicySchemaV1, PrimaryIP: r.PrimaryIP, PeerIP: r.PeerIP, CatalogName: r.CatalogName, View: r.View}, hashes: []string{"stable", "stable"}}
}

func TestNativeDeletionRequiresTwoStableCompleteReads(t *testing.T) {
	r := request(t)
	baseline := Snapshot{ProcessID: 42, ProcessStartTicks: 123456, ConfigSHA256: strings.Repeat("f", 64), ListenersVerified: true, CatalogSerial: 12, CatalogMembers: []string{"other.example.test"}, CatalogTransferred: true, ZoneState: "unloaded"}
	now := func() time.Time { return time.Unix(testTime+2, 0) }
	reader := &sequenceReader{snapshots: []Snapshot{baseline, baseline}}
	response, err := Inspect(context.Background(), r, reader, policyFor(r), now)
	if err != nil || reader.calls != 2 || response.CatalogState != "transferred" || response.MemberState != "absent" || response.NativeState != "unloaded" {
		t.Fatalf("stable proof %+v %v", response, err)
	}
	tests := []struct {
		name string
		edit func(*Snapshot)
	}{
		{"process changed", func(s *Snapshot) { s.ProcessID++ }},
		{"PID reused", func(s *Snapshot) { s.ProcessStartTicks++ }},
		{"config changed", func(s *Snapshot) { s.ConfigSHA256 = strings.Repeat("1", 64) }},
		{"catalog changed", func(s *Snapshot) { s.CatalogMembers = []string{"other.example.test", "new.example.test"} }},
		{"serial changed", func(s *Snapshot) { s.CatalogSerial++ }},
		{"zone loaded", func(s *Snapshot) { s.ZoneState = "loaded" }},
		{"listeners unverified", func(s *Snapshot) { s.ListenersVerified = false }},
		{"transfer unavailable", func(s *Snapshot) { s.CatalogTransferred = false }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			second := baseline
			tc.edit(&second)
			got, e := Inspect(context.Background(), r, &sequenceReader{snapshots: []Snapshot{baseline, second}}, policyFor(r), now)
			if e != nil {
				t.Fatal(e)
			}
			if got.CatalogState == "transferred" && got.MemberState == "absent" && got.NativeState == "unloaded" {
				t.Fatalf("unstable native state produced success: %+v", got)
			}
		})
	}
}

func TestEmptyTransferredCatalogCanProveFinalMemberUnloaded(t *testing.T) {
	r := request(t)
	digest, err := dnspeerproof.CatalogMembersSHA256(r.PrimaryIP, r.CatalogSerial, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.CatalogMembersSHA256 = digest
	snapshot := Snapshot{
		ProcessID: 42, ProcessStartTicks: 123456, ConfigSHA256: strings.Repeat("f", 64),
		ListenersVerified: true, CatalogSerial: r.CatalogSerial,
		CatalogTransferred: true, ZoneState: "unloaded",
	}
	response, err := Inspect(context.Background(), r,
		&sequenceReader{snapshots: []Snapshot{snapshot, snapshot}}, policyFor(r),
		func() time.Time { return time.Unix(testTime+2, 0) })
	if err != nil {
		t.Fatal(err)
	}
	consumed := false
	_, err = dnspeerproof.Verify(r, response,
		dnspeerproof.PeerAuthentication{
			Established: true, IdentitySHA256: r.PeerIdentitySHA256, PeerIP: r.PeerIP,
		}, time.Unix(testTime+3, 0), func(string) bool {
			consumed = true
			return true
		})
	if err != nil || !consumed {
		t.Fatalf("valid empty catalog failed authenticated proof: consumed=%t err=%v", consumed, err)
	}
}

func TestAXFRParserRejectsForgedMemberAndMissingEnvelope(t *testing.T) {
	r := request(t)
	label, err := binddns.CatalogMemberLabel("other.example.test")
	if err != nil {
		t.Fatal(err)
	}
	cat := r.CatalogName + "."
	soa := cat + " 60 IN SOA invalid. invalid. 12 60 30 3600 30"
	good := strings.Join([]string{soa, cat + " 60 IN NS invalid.", "version." + cat + " 60 IN TXT \"2\"", label + ".zones." + cat + " 60 IN PTR other.example.test.", soa}, "\n")
	serial, members, err := parseCatalogAXFR(good, r)
	if err != nil || serial != 12 || len(members) != 1 || members[0] != "other.example.test" {
		t.Fatalf("valid AXFR rejected: %d %v %v", serial, members, err)
	}
	// Native PowerDNS presents catalog TXT/PTR metadata with TTL zero;
	// managed BIND catalogs retain TTL 60. Both have the same member set.
	zeroMetadata := strings.ReplaceAll(strings.ReplaceAll(good, " 60 IN TXT ", " 0 IN TXT "), " 60 IN PTR ", " 0 IN PTR ")
	zeroMetadata = strings.Replace(zeroMetadata, label+".zones.", "tlhnkturltuku63bhersj40lbi0vkdvt.zones.", 1)
	if gotSerial, gotMembers, e := parseCatalogAXFR(zeroMetadata, r); e != nil || gotSerial != 12 || len(gotMembers) != 1 || gotMembers[0] != "other.example.test" {
		t.Fatalf("native PowerDNS catalog rejected: serial=%d members=%v err=%v", gotSerial, gotMembers, e)
	}
	if _, _, e := parseCatalogAXFR(strings.Replace(zeroMetadata, "tlhnkturltuku63bhersj40lbi0vkdvt", "wlhnkturltuku63bhersj40lbi0vkdvt", 1), r); e == nil {
		t.Fatal("invalid native PowerDNS catalog label accepted")
	}
	if _, _, e := parseCatalogAXFR(strings.Replace(zeroMetadata, " 0 IN PTR ", " 60 IN PTR ", 1), r); e == nil {
		t.Fatal("mixed native catalog metadata TTLs accepted")
	}
	if _, _, e := parseCatalogAXFR(strings.Replace(good, " 60 IN PTR ", " 30 IN PTR ", 1), r); e == nil {
		t.Fatal("unsupported catalog metadata TTL accepted")
	}

	for _, bad := range []string{
		strings.Replace(good, soa+"\n", "", 1),
		strings.Replace(good, label+".zones.", strings.Repeat("0", 56)+".zones.", 1),
		strings.Replace(good, " 12 60 30 ", " 11 60 30 ", 1),
		strings.Replace(good, "other.example.test.", "gone.example.test.", 1),
	} {
		if _, _, e := parseCatalogAXFR(bad, r); e == nil {
			t.Fatalf("forged catalog accepted: %q", bad)
		}
	}
}

func TestAXFRParserAcceptsEmptyCatalogAfterFinalMemberDeletion(t *testing.T) {
	r := request(t)
	cat := r.CatalogName + "."
	soa := cat + " 60 IN SOA invalid. invalid. 12 60 30 3600 30"
	// A transferred catalog with no member PTR has exactly these four
	// records. Rejecting it would prevent native proof of the last deletion.
	axfr := strings.Join([]string{
		soa,
		cat + " 60 IN NS invalid.",
		"version." + cat + " 60 IN TXT \"2\"",
		soa,
	}, "\n")
	serial, members, err := parseCatalogAXFR(axfr, r)
	if err != nil || serial != 12 || len(members) != 0 {
		t.Fatalf("empty transferred catalog rejected: serial=%d members=%v err=%v",
			serial, members, err)
	}
}

func TestConfigMustBindExactSecondaryAndSource(t *testing.T) {
	r := request(t)
	config := "options { catalog-zones { zone \"" + r.CatalogName + "\" default-primaries { " + r.PrimaryIP + "; } in-memory yes; }; }; zone \"" + r.CatalogName + "\" { type secondary; primaries { " + r.PrimaryIP + "; }; };"
	if !configBindsCatalog(config, r) {
		t.Fatal("reviewed native subscription rejected")
	}
	// named-checkconf canonicalizes listener and AXFR ACL IPs with /32;
	// these unrelated ACLs must not change the exact subscription check.
	canonical := "options { listen-on { 127.0.0.1/32; " + r.PeerIP + "/32; }; catalog-zones { zone \"" + r.CatalogName + "\" default-primaries { " + r.PrimaryIP + "; } in-memory yes; }; allow-transfer { \"none\"; }; }; zone \"" + r.CatalogName + "\" { type secondary; primaries { " + r.PrimaryIP + "; }; allow-transfer { " + r.PrimaryIP + "/32; 127.0.0.1/32; }; };"
	if !configBindsCatalog(canonical, r) {
		t.Fatal("certified named-checkconf /32 ACL shape rejected")
	}
	withFile := strings.Replace(canonical, "type secondary; primaries", "type secondary; file \"celikpanel-fixture-catalog.zone\"; primaries", 1)
	if !configBindsCatalog(withFile, r) {
		t.Fatal("native secondary catalog file directive rejected")
	}
	if configBindsCatalog(strings.Replace(withFile, "type secondary;", "type primary;", 1), r) {
		t.Fatal("catalog file directive hid a primary role")
	}

	for _, bad := range []string{strings.Replace(config, r.PrimaryIP, "192.0.2.99", 1), strings.Replace(config, "type secondary", "type primary", 1), strings.Replace(config, "in-memory yes", "in-memory no", 1)} {
		if configBindsCatalog(bad, r) {
			t.Fatalf("wrong config accepted: %q", bad)
		}
	}
}

func TestNativeControlListenerMustBelongToNamedPID(t *testing.T) {
	ipv4 := "LISTEN 0 5 127.0.0.1:953 0.0.0.0:* users:((\"named\",pid=42,fd=31))"
	ipv6 := "LISTEN 0 5 [::1]:953 [::]:* users:((\"named\",pid=42,fd=32))"
	good := ipv4 + "\n" + ipv6
	if !controlListenerMatches(good, 42) || !controlListenerMatches(ipv4, 42) {
		t.Fatal("certified local named control listeners rejected")
	}
	for _, bad := range []string{
		ipv6,
		strings.Replace(good, "pid=42", "pid=43", 1),
		strings.Replace(good, "named", "other", 1),
		strings.Replace(good, "127.0.0.1:953", "0.0.0.0:953", 1),
		strings.Replace(good, "[::1]:953", "[::]:953", 1),
		good + "\n" + ipv4,
		ipv4 + "\n" + ipv4,
		strings.Replace(good, "0 5", "00 5", 1),
		"tcp " + good,
	} {
		if controlListenerMatches(bad, 42) {
			t.Fatalf("unsafe control listener accepted: %q", bad)
		}
	}
}

func TestLocalCatalogAXFRCertifiedDebianListeners(t *testing.T) {
	public := "tcp LISTEN 0 10 192.0.2.11:53 0.0.0.0:* users:((\"named\",pid=42,fd=27))"
	localTCP := "tcp LISTEN 0 10 127.0.0.1:53 0.0.0.0:* users:((\"named\",pid=42,fd=22))"
	localUDP := "udp UNCONN 0 0 127.0.0.1:53 0.0.0.0:* users:((\"named\",pid=42,fd=19))"
	resolved := "tcp LISTEN 0 4096 127.0.0.53%lo:53 0.0.0.0:* users:((\"systemd-resolve\",pid=7,fd=19))"
	rows := strings.Join([]string{
		public, strings.Replace(public, "fd=27", "fd=26", 1),
		localTCP, strings.Replace(localTCP, "fd=22", "fd=20", 1),
		localUDP, strings.Replace(localUDP, "fd=19", "fd=18", 1),
		resolved,
	}, "\n")
	if !localCatalogListenerMatches(rows, 42) {
		t.Fatalf("certified Debian ss inventory rejected: %q", rows)
	}
	for _, bad := range []string{
		public + "\n" + localUDP,
		public + "\n" + localTCP,
		strings.Replace(rows, "pid=42,fd=22", "pid=43,fd=22", 1),
		strings.Replace(rows, "named\",pid=42,fd=19", "other\",pid=42,fd=19", 1),
		rows + "\n" + strings.Repeat(localTCP+"\n", 8),
		rows + "\n" + strings.Repeat(localUDP+"\n", 8),
	} {
		if localCatalogListenerMatches(bad, 42) {
			t.Fatalf("unsafe local AXFR listener accepted: %q", bad)
		}
	}
}

func TestNamedInvocationBindsExactDefaultConfig(t *testing.T) {
	tests := []struct{ exe, cmdline, config string }{
		{"/usr/sbin/named", "/usr/sbin/named\x00-f\x00-u\x00bind\x00", "/etc/bind/named.conf"},
		{"/usr/bin/named", "/usr/bin/named\x00-f\x00-u\x00named\x00", "/etc/named.conf"},
	}
	for _, tc := range tests {
		got, err := namedDefaultConfigForInvocation(tc.exe, []byte(tc.cmdline))
		if err != nil || got != tc.config {
			t.Fatalf("certified invocation rejected: %q %q %v", tc.exe, got, err)
		}
		bads := []string{
			tc.cmdline[:len(tc.cmdline)-1] + "-c\x00/tmp/other.conf\x00",
			tc.cmdline[:len(tc.cmdline)-1] + "-t\x00/tmp/jail\x00",
			strings.Replace(tc.cmdline, "-f\x00", "-c\x00", 1),
		}
		for _, bad := range bads {
			if _, err := namedDefaultConfigForInvocation(tc.exe, []byte(bad)); err == nil {
				t.Fatalf("alternate named invocation accepted: %q", bad)
			}
		}
	}
	if _, err := namedDefaultConfigForInvocation("/usr/sbin/named", []byte("/usr/sbin/named\x00-f\x00-u\x00bind")); err == nil {
		t.Fatal("unterminated cmdline accepted")
	}
}

func TestExplicitBINDViewsCannotMasqueradeAsDefaultView(t *testing.T) {
	r := request(t)
	config := "options { catalog-zones { zone \"" + r.CatalogName + "\" default-primaries { " + r.PrimaryIP + "; } in-memory yes; }; }; zone \"" + r.CatalogName + "\" { type secondary; primaries { " + r.PrimaryIP + "; }; };"
	if !configBindsCatalog(config, r) {
		t.Fatal("default-view config rejected")
	}
	for _, bad := range []string{
		"view \"external\" { " + config + " };",
		config + "\n view \"private\" { match-clients { 127.0.0.1; }; };",
	} {
		if configBindsCatalog(bad, r) {
			t.Fatalf("explicit view accepted: %q", bad)
		}
	}
}

func TestNamedStarttimeBindsPIDAndProcessIdentity(t *testing.T) {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = "12345"
	line := "42 (named) " + strings.Join(fields, " ") + "\n"
	ticks, err := parseNamedStartTicks(42, line)
	if err != nil || ticks != 12345 {
		t.Fatalf("valid process stat rejected: %d %v", ticks, err)
	}
	for _, bad := range []string{
		strings.Replace(line, "42 (named)", "43 (named)", 1),
		strings.Replace(line, "(named)", "(other)", 1),
		strings.Replace(line, "12345", "0", 1),
	} {
		if _, err := parseNamedStartTicks(42, bad); err == nil {
			t.Fatalf("invalid process identity accepted: %q", bad)
		}
	}
}
