package bindconfig

import "testing"

func TestVerifyMainIncludesRequiresActiveTopLevelStatements(t *testing.T) {
	good := `// owner header
include "/etc/bind/named.conf.options";
/* owner comment */
include "/etc/bind/named.conf.local";
include "/etc/bind/named.conf.default-zones";
`
	if err := VerifyMainIncludes(good); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{
		`// include "/etc/bind/named.conf.options";
include "/etc/bind/named.conf.local";`,
		`/* include "/etc/bind/named.conf.options"; */
include "/etc/bind/named.conf.local";`,
		`options { include "/etc/bind/named.conf.options"; };
include "/etc/bind/named.conf.local";`,
		`foo "include \"/etc/bind/named.conf.options\";";
include "/etc/bind/named.conf.local";`,
		good + `include "/etc/bind/named.conf.local";`,
		`include "/etc/bind/named.conf.options"
include "/etc/bind/named.conf.local";`,
		good + "\n/* unclosed",
		good + "\n}",
	} {
		if err := VerifyMainIncludes(candidate); err == nil {
			t.Fatalf("inert, duplicate or malformed include accepted: %q", candidate)
		}
	}
}
