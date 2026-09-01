package hostingpath

import "testing"

// IsSiteHome is used before the agent acts as root on an account's behalf, so
// a false positive is a privilege boundary failure, not a cosmetic one.
func TestIsSiteHomeAcceptsOnlyPathsInsideTheHostingRoot(t *testing.T) {
	tests := []struct {
		name string
		home string
		want bool
	}{
		{name: "a real site home", home: "/var/www/celikpanel/subscriptions/4/sites/9", want: true},
		{name: "a subscription directory", home: "/var/www/celikpanel/subscriptions/4", want: true},
		{name: "an uncleaned but contained path", home: "/var/www/celikpanel/subscriptions/4/sites/../sites/9", want: true},

		{name: "empty", home: "", want: false},
		{name: "the root itself is not a home", home: "/var/www/celikpanel/subscriptions", want: false},
		{name: "root's home", home: "/root", want: false},
		{name: "a system account home", home: "/var/www", want: false},
		{name: "a sibling that merely shares the prefix", home: "/var/www/celikpanel/subscriptions-evil/1", want: false},
		{name: "escapes through a parent reference", home: "/var/www/celikpanel/subscriptions/../../../root", want: false},
		{name: "relative paths are never a proven home", home: "subscriptions/4/sites/9", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsSiteHome(test.home); got != test.want {
				t.Fatalf("IsSiteHome(%q) = %v, want %v", test.home, got, test.want)
			}
		})
	}
}

// SubscriptionsRoot is the parent every derived site home agrees with; if the
// two ever drift, containment checks silently stop containing.
func TestSubscriptionsRootIsTheParentOfEveryDerivedSiteHome(t *testing.T) {
	home, err := SiteHome(7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSiteHome(home) {
		t.Fatalf("SiteHome(7,3) = %q is not recognised as a site home", home)
	}
	if got := SubscriptionsRoot(); got != "/var/www/celikpanel/subscriptions" {
		t.Fatalf("SubscriptionsRoot() = %q", got)
	}
}
