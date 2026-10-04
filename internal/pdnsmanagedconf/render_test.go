package pdnsmanagedconf

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The batch 6b native run (deploy/e2e/dns-kill-matrix/evidence/
// batch6b-pdns-secondary-20260930, c1 post-state) measured a CelikPanel-managed
// Debian 13 PowerDNS secondary 192.0.2.10 of primary 192.0.2.11 with these
// drop-in digests. The shared renderer must reproduce them byte for byte.
func TestRendererReproducesMeasuredManagedSecondaryDropIns(t *testing.T) {
	managed, err := Standalone("/var/lib/powerdns/pdns.sqlite3", []string{"10.0.2.15", "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	cluster, err := DirectionalCluster(transport.DNSPairRoleSecondary, "192.0.2.10", "192.0.2.11")
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]byte{
		"0485300f3503e60e09a5ec29a43483c29faf4506c91dd3ee44935367307bde69": managed,
		"84bf02538a5efc4f5a4ef1a16775165fb6a6a3001b4feb2890c636f4a675c970": []byte(cluster),
	} {
		sum := sha256.Sum256(got)
		if hex.EncodeToString(sum[:]) != name {
			t.Fatalf("rendering differs from the measured drop-in %s:\n%s", name, got)
		}
	}
}

// v0.1.0-alpha.29 to alpha.38 (commit 4be98fc9) rendered this drop-in with
// autosecondary=yes; the historical bytes are pinned here.
func TestAutosecondaryRenderingIsTheEarlierProductBytes(t *testing.T) {
	secondary, err := DirectionalClusterAutosecondary(transport.DNSPairRoleSecondary, "192.0.2.10", "192.0.2.11")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Managed by CelikPanel - do not edit by hand / elle duzenlemeyin\n" +
		"# Directional DNS pair: AXFR is restricted to the trusted local proof and exact peer.\n" +
		"# Yonlu DNS cifti: AXFR guvenilir yerel kanit ve tam es ile sinirlidir.\n" +
		"primary=yes\nsecondary=yes\nautosecondary=yes\nallow-axfr-ips=192.0.2.11\n"
	if secondary != want {
		t.Fatalf("historical secondary rendering differs:\n%s", secondary)
	}
	primary, err := DirectionalClusterAutosecondary(transport.DNSPairRolePrimary, "192.0.2.11", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	wantPrimary := "# Managed by CelikPanel - do not edit by hand / elle duzenlemeyin\n" +
		"# Directional DNS pair: AXFR is restricted to the trusted local proof and exact peer.\n" +
		"# Yonlu DNS cifti: AXFR guvenilir yerel kanit ve tam es ile sinirlidir.\n" +
		"primary=yes\nsecondary=yes\nautosecondary=yes\nallow-axfr-ips=192.0.2.11,192.0.2.10\nalso-notify=192.0.2.10\n"
	if primary != wantPrimary {
		t.Fatalf("historical primary rendering differs:\n%s", primary)
	}
}

func TestRendererRefusesNonCanonicalInput(t *testing.T) {
	for _, addresses := range [][]string{nil, {"127.0.0.1"}, {"0.0.0.0"}, {"192.0.2.10", "192.0.2.10"}, {"192.000.2.10"}} {
		if _, err := Standalone("/var/lib/powerdns/pdns.sqlite3", addresses); err == nil {
			t.Fatalf("accepted listen addresses %v", addresses)
		}
	}
	for _, args := range [][3]string{
		{"standalone", "192.0.2.10", "192.0.2.11"},
		{transport.DNSPairRoleSecondary, "192.0.2.10", "192.0.2.10"},
		{transport.DNSPairRoleSecondary, "192.0.2.10", "::1"},
	} {
		if _, err := DirectionalCluster(args[0], args[1], args[2]); err == nil {
			t.Fatalf("accepted pair %v", args)
		}
	}
}
