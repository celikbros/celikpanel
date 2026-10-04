package bindconfig

import "testing"

func TestAdoptionStaticZonesBounded(t *testing.T) {
	options := "options { directory \"/var/cache/bind\"; dnssec-validation auto; allow-update { none; }; };"
	local := "zone \"owner.example\" IN { type master; file \"/etc/bind/owner.db\"; allow-update { none; }; };"
	root := "zone \".\" { type hint; file \"/usr/share/dns/root.hints\"; };"
	zones, e := ParseAdoptionStaticZones(local, options, root)
	if e != nil || len(zones) != 2 || zones[0].Name != "owner.example" || zones[1].Name != "." {
		t.Fatalf("zones=%+v err=%v", zones, e)
	}
	for name, bad := range map[string]string{
		"view":             "view \"external\" { " + local + " };",
		"dynamic":          "zone \"owner.example\" { type master; file \"/etc/bind/owner.db\"; allow-update { any; }; };",
		"relative":         "zone \"owner.example\" { type master; file \"owner.db\"; };",
		"mutable-dir":      "zone \"owner.example\" { type master; file \"/var/lib/bind/owner.db\"; };",
		"bad-name":         "zone \"*.example\" { type master; file \"/etc/bind/owner.db\"; };",
		"truncated-update": "zone \"owner.example\" { type master; file \"/etc/bind/owner.db\"; allow-update { none;",
		"include":          "include \"/etc/bind/private\"; " + local,
		"inline":           "zone \"owner.example\" { type master; file \"/etc/bind/owner.db\"; inline-signing yes; };",
		"secondary":        "zone \"owner.example\" { type slave; file \"/etc/bind/owner.db\"; };",
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseAdoptionStaticZones(bad, options, root); e == nil {
				t.Fatal("unsafe zone accepted")
			}
		})
	}
	if _, e := ParseAdoptionStaticZones(local, "options { allow-update { any; }; };", root); e == nil {
		t.Fatal("inherited dynamic update accepted")
	}
}
