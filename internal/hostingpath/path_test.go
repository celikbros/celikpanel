package hostingpath

import (
	"strings"
	"testing"
)

func TestSitePathsAreDerivedFromPositiveIdentities(t *testing.T) {
	home, err := SiteHome(4, 13)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/var/www/celikpanel/subscriptions/4/sites/13"; home != want {
		t.Fatalf("home = %q, want %q", home, want)
	}
	docroot, err := DocumentRoot(4, 13)
	if err != nil {
		t.Fatal(err)
	}
	if want := home + "/public_html"; docroot != want {
		t.Fatalf("docroot = %q, want %q", docroot, want)
	}
	challengeRoot, err := ACMEChallengeRoot(4, 13)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/var/lib/celikpanel-agent/acme-http-01/subscriptions/4/domains/13"; challengeRoot != want {
		t.Fatalf("challenge root = %q, want %q", challengeRoot, want)
	}
	if strings.HasPrefix(challengeRoot, home+"/") || strings.HasPrefix(home, challengeRoot+"/") {
		t.Fatalf("challenge root %q overlaps tenant home %q", challengeRoot, home)
	}
	privateStateRoot := ServiceMutationStateRoot()
	if strings.HasPrefix(challengeRoot, privateStateRoot+"/") ||
		strings.HasPrefix(privateStateRoot, challengeRoot+"/") ||
		challengeRoot == privateStateRoot {
		t.Fatalf("challenge root %q overlaps private agent state %q", challengeRoot, privateStateRoot)
	}
	if want := "/var/lib/celikpanel-agent-private"; privateStateRoot != want {
		t.Fatalf("private agent state = %q, want %q", privateStateRoot, want)
	}
}

func TestValidateDocumentRootRejectsSiblingAndNginxText(t *testing.T) {
	cases := []string{
		"/var/www/celikpanel/subscriptions/4/sites/12/public_html",
		"/var/www/celikpanel/subscriptions/4/sites/13/public_html/../other",
		"/var/www/celikpanel/subscriptions/4/sites/13/public_html; return 200",
	}
	for _, candidate := range cases {
		if err := ValidateDocumentRoot(candidate, 4, 13); err == nil {
			t.Fatalf("ValidateDocumentRoot(%q) succeeded", candidate)
		}
	}
	if err := ValidateDocumentRoot(
		"/var/www/celikpanel/subscriptions/4/sites/13/public_html", 4, 13,
	); err != nil {
		t.Fatalf("canonical document root rejected: %v", err)
	}
}

func TestValidateACMEChallengeRootRejectsTenantAndSiblingPaths(t *testing.T) {
	for _, candidate := range []string{
		"/var/www/celikpanel/subscriptions/4/sites/13/public_html",
		"/var/lib/celikpanel-agent/acme-http-01/subscriptions/4/domains/12",
		"/var/lib/celikpanel-agent/acme-http-01/subscriptions/4/domains/13/../12",
		"/var/lib/celikpanel-agent/acme-http-01/subscriptions/4/domains/13; return 200",
	} {
		if err := ValidateACMEChallengeRoot(candidate, 4, 13); err == nil {
			t.Fatalf("ValidateACMEChallengeRoot(%q) succeeded", candidate)
		}
	}
	if err := ValidateACMEChallengeRoot(
		"/var/lib/celikpanel-agent/acme-http-01/subscriptions/4/domains/13", 4, 13,
	); err != nil {
		t.Fatalf("canonical challenge root rejected: %v", err)
	}
}

func TestSitePathsRejectNonPositiveIdentities(t *testing.T) {
	for _, ids := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := SiteHome(ids[0], ids[1]); err == nil {
			t.Fatalf("SiteHome(%d, %d) succeeded", ids[0], ids[1])
		}
		if _, err := ACMEChallengeRoot(ids[0], ids[1]); err == nil {
			t.Fatalf("ACMEChallengeRoot(%d, %d) succeeded", ids[0], ids[1])
		}
	}
}
