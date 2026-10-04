package bindconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestVerifyExactZoneIncludePreservesOwnerTextOutsideManagedSpan(t *testing.T) {
	const include = "/var/lib/celikpanel/bind-generations/current/zones.conf"
	source := "// owner header\noptions { recursion no; };\n"
	managed, err := ManagedZoneInclude(source, include)
	if err != nil {
		t.Fatalf("prepare managed include: %v", err)
	}
	withOwnerComment := managed + "// owner footer\n"
	if err := VerifyExactZoneInclude(withOwnerComment, include); err != nil {
		t.Fatalf("unchanged managed span with separate owner text was rejected: %v", err)
	}
	after, err := ManagedZoneInclude(withOwnerComment, include)
	if err != nil || after != withOwnerComment {
		t.Fatalf("owner text was rewritten: %v", err)
	}
	if err := VerifyExactZoneInclude(source, include); err == nil {
		t.Fatal("missing managed include was accepted as active")
	}
}

func TestVerifyExactZoneIncludeRejectsOwnerEditInsideManagedSpan(t *testing.T) {
	const include = "/var/lib/celikpanel/bind-generations/current/zones.conf"
	managed, err := ManagedZoneInclude("options { recursion no; };\n", include)
	if err != nil {
		t.Fatalf("prepare managed include: %v", err)
	}
	original := "include \"" + include + "\";\n"
	edited := strings.Replace(managed, original, "// owner edit within panel-owned span\n"+original, 1)
	if edited == managed {
		t.Fatal("fixture did not change the managed span")
	}
	if err := VerifyExactZoneInclude(edited, include); !errors.Is(err, ErrManagedZoneIncludeModified) {
		t.Fatal("owner edit inside managed span was accepted")
	}
	inert := strings.Replace(managed, "// BEGIN CELIKPANEL MANAGED BIND ZONES", "/* // BEGIN CELIKPANEL MANAGED BIND ZONES */", 1)
	if err := VerifyExactZoneInclude(inert, include); err == nil {
		t.Fatal("inert managed marker was accepted")
	}
}
