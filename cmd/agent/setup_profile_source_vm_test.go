//go:build linux

package main

import (
	"os"
	"slices"
	"testing"
)

func TestSetupProfileDisposableCertificateSourceReadback(t *testing.T) {
	profile := os.Getenv("CELIKPANEL_SETUP_PROFILE_VM")
	if profile == "" {
		t.Skip("requires a disposable profile fixture")
	}
	if !slices.Contains([]string{"web", "application", "webmail"}, profile) || os.Geteuid() != 0 {
		t.Fatal("invalid disposable profile identity")
	}
	marker, err := os.ReadFile("/var/lib/celikpanel-profile-vm/fixture")
	if err != nil || string(marker) != "fresh-setup-profile-acceptance-v1\n" {
		t.Fatal("missing isolated fixture marker")
	}
	if _, err = os.Stat("/opt/celikpanel/bin/panel"); !os.IsNotExist(err) {
		t.Fatal("installed panel is not a fixture")
	}
	_, _, leaf, expiry, err := readPanelCertificateSource("panel." + profile + ".setup.test")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("trusted real Certbot source leaf=%s expires=%s", panelCertificateLeafSHA256(leaf), expiry)
}
