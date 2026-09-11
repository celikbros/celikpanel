//go:build linux

package main

import (
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"testing"
)

func TestServerSetupRenewalRequiresSupportedUnambiguousRoute(t *testing.T) {
	for _, tc := range []struct{ config, want string }{
		{"[renewalparams]\nauthenticator = standalone\n", "standalone"},
		{"[renewalparams]\nauthenticator = webroot\nwebroot_path = " + hostingpath.PanelACMEChallengeRoot() + ",\n", "webroot"},
		{"authenticator = webroot\n", ""},
		{"authenticator = webroot\nwebroot_path = /tenant/public_html\n", ""},
		{"authenticator = webroot\nwebroot_path = " + hostingpath.PanelACMEChallengeRoot() + "\nwebroot_path = /other\n", ""},
		{"authenticator = standalone\nauthenticator = webroot\n", ""},
		{"authenticator = manual\n", ""},
		{"# authenticator = standalone\n", ""},
	} {
		if got := setupPanelRenewalAuthenticator([]byte(tc.config)); got != tc.want {
			t.Fatalf("config %q returned %q, want %q", tc.config, got, tc.want)
		}
	}
}
