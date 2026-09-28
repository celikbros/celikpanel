//go:build linux

package main

import (
	"os"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestRunningBINDAdoptionInstallPublishedOnlyIntoAbsentPath(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	journal, receipt, encoded := runningBINDAdoptionInstallFixture(t)
	path, err := dnsEngineInstallOwnershipPath(transport.DNSEngineBIND)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishNewRunningBINDAdoptionInstall(receipt); err != nil {
		t.Fatal(err)
	}
	if err := verifyExactRunningBINDAdoptionInstall(receipt); err != nil {
		t.Fatal(err)
	}
	if err := publishNewRunningBINDAdoptionInstall(receipt); err == nil {
		t.Fatal("publisher rewrote an existing receipt")
	}
	changed := append(append([]byte(nil), encoded...), ' ')
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyExactRunningBINDAdoptionInstall(receipt); err == nil {
		t.Fatal("owner edit passed pre-rollback proof")
	}
	if err := retireExactRunningBINDAdoptionInstall(journal, receipt); err == nil {
		t.Fatal("owner edit was removed")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(changed) {
		t.Fatalf("owner evidence changed: data=%q err=%v", data, err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := retireExactRunningBINDAdoptionInstall(journal, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary receipt remains: %v", err)
	}
}
