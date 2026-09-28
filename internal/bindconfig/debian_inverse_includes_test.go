package bindconfig

import (
	"strings"
	"testing"
)

func TestDebianInverseMainOnlyFixedIncludes(t *testing.T) {
	good := `// Debian header
include "/etc/bind/named.conf.options";
/* comment */ include "/etc/bind/named.conf.local";
include "/etc/bind/named.conf.default-zones";
`
	leaf, err := DebianInverseMainLeaf(good)
	if err != nil || leaf != "/etc/bind/named.conf.default-zones" {
		t.Fatalf("default leaf = %q, %v", leaf, err)
	}
	hints := strings.Replace(good, "named.conf.default-zones", "named.conf.root-hints", 1)
	leaf, err = DebianInverseMainLeaf(hints)
	if err != nil || leaf != "/etc/bind/named.conf.root-hints" {
		t.Fatalf("root-hints leaf = %q, %v", leaf, err)
	}
	if err := VerifyDebianInverseMainIncludes(good); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"new-zone":       good + `zone "owner.test" { type master; file "/tmp/owner"; };`,
		"extra-include":  good + `include "/etc/bind/owner.conf";`,
		"missing-local":  `include "/etc/bind/named.conf.options"; include "/etc/bind/named.conf.default-zones";`,
		"duplicate":      good + `include "/etc/bind/named.conf.local";`,
		"both-leaves":    good + `include "/etc/bind/named.conf.root-hints";`,
		"duplicate-leaf": good + `include "/etc/bind/named.conf.default-zones";`,
		"nested":         good + `options { include "/etc/bind/owner.conf"; };`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifyDebianInverseMainIncludes(bad); err == nil {
				t.Fatal("unsafe main config accepted")
			}
		})
	}
}

func TestDebianInverseLeafRejectsAnyActiveInclude(t *testing.T) {
	for _, bad := range []string{
		`include "/etc/bind/owner.conf";`,
		`options { include "/etc/bind/owner.conf"; };`,
		`view "x" { include "/etc/bind/owner.conf"; };`,
	} {
		if err := VerifyDebianInverseNoIncludes(bad); err == nil {
			t.Fatalf("active include accepted: %s", bad)
		}
	}
	if err := VerifyDebianInverseNoIncludes(`// include "/etc/bind/comment";
zone "example.test" { type master; file "/var/lib/bind/example"; };`); err != nil {
		t.Fatal(err)
	}
	managedPath := "/var/cache/bind/celikpanel/current/zones.conf"
	managed, err := ManagedZoneInclude("// local\n", managedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyDebianInverseManagedLeaf(managed, managedPath); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDebianInverseManagedLeaf(managed+`include "/etc/bind/owner.conf";`, managedPath); err == nil {
		t.Fatal("extra after-write include accepted")
	}
}
