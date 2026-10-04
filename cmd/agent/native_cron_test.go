package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Any existing cron is the owner's and is left alone. Installing the distro
// package over another implementation would let apt remove the owner's cron;
// re-enabling a unit the owner disabled would overwrite a deliberate choice.
func TestNativeCronPresenceLeavesAnyExistingImplementationAlone(t *testing.T) {
	none := nativeCronProbe{unitExists: func(string) bool { return false }, fileExists: func(string) bool { return false }}
	if nativeCronPresent(false, none) {
		t.Fatal("a host without any cron must be offered the install")
	}
	if !nativeCronPresent(true, none) {
		t.Fatal("the catalogue unit (cron.service/cronie.service) must count as present")
	}
	for _, unit := range []string{"crond", "fcron", "dcron", "bcron", "systemd-cron"} {
		probe := nativeCronProbe{unitExists: func(name string) bool { return name == unit }, fileExists: func(string) bool { return false }}
		if !nativeCronPresent(false, probe) {
			t.Errorf("owner cron unit %s must be preserved", unit)
		}
	}
	for _, path := range []string{"/usr/bin/crontab", "/bin/crontab"} {
		probe := nativeCronProbe{unitExists: func(string) bool { return false }, fileExists: func(name string) bool { return name == path }}
		if !nativeCronPresent(false, probe) {
			t.Errorf("a crontab command at %s must be preserved", path)
		}
	}
}

func TestNativeCronIsNeverRemovedThroughTheCatalogue(t *testing.T) {
	cron := core.GetManagedServiceByID(core.NativeCronServiceID)
	if nativeCronRemovalRefusal(cron) == "" {
		t.Fatal("cron removal must be refused")
	}
	if nativeCronRemovalRefusal(core.GetManagedServiceByID("nginx")) != "" || nativeCronRemovalRefusal(nil) != "" {
		t.Fatal("the refusal must apply to cron only")
	}
	resp := UninstallServiceResponse{}
	removeCalled := false
	ops := serviceUninstallOps{
		detectPackageFamily: func() string { return "apt" },
		packageInstalled:    func(string) bool { return true },
		unitExists:          func(string) bool { return true },
		unitsMatching:       func(string) []string { return nil },
		disableUnit: func(string) error {
			removeCalled = true
			return nil
		},
		removePackages: func(string, []string) (string, error) {
			removeCalled = true
			return "", nil
		},
	}
	if err := (&Agent{}).uninstallServiceWithOps(&InstallServiceRequest{ID: core.NativeCronServiceID}, &resp, ops); err != nil {
		t.Fatal(err)
	}
	if removeCalled || resp.Removed || resp.MutationApplied || resp.Error != core.NativeCronRemovalRefusal {
		t.Fatalf("cron uninstall changed the host or was not refused: %+v removeCalled=%v", resp, removeCalled)
	}
}

// Every cron RPC reports the missing command with the exact transport text the
// Panel classifies; before this, a missing cron listed as "no jobs" and a
// change reported "cron job not found".
func TestCronRPCsReportMissingCronExactly(t *testing.T) {
	old := cronLookPath
	t.Cleanup(func() { cronLookPath = old })
	cronLookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := requireCronInstalled(); err == nil || err.Error() != transport.CronNotInstalled {
		t.Fatalf("missing cron error = %v, want %q", err, transport.CronNotInstalled)
	}
	cronLookPath = func(string) (string, error) { return "/usr/bin/crontab", nil }
	if err := requireCronInstalled(); err != nil {
		t.Fatalf("present cron refused: %v", err)
	}
	if !strings.Contains(transport.CronNotInstalled, "cron is not installed") {
		t.Fatal("the transport text must stay the one older Agents returned")
	}
}
