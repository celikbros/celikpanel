package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func TestValidateRepoPackageSelectionRejectsEmptyCataloguePattern(t *testing.T) {
	service := core.GetManagedServiceByID("netdata")
	if service == nil || service.Repo == nil {
		t.Fatal("netdata repository is missing from the catalogue")
	}
	if _, err := validateRepoPackageSelection(service, "bash"); err == nil {
		t.Fatal("Netdata accepted arbitrary package bash with an empty PackagePattern")
	}
}

func TestValidateRepoPackageSelectionRequiresWholeNameMatch(t *testing.T) {
	service := &core.ManagedService{
		Name: "test runtime",
		Repo: &core.ManagedRepo{PackagePattern: `php[0-9]+\.[0-9]+-fpm`},
	}
	if _, err := validateRepoPackageSelection(service, "prefix-php8.3-fpm-suffix"); err == nil {
		t.Fatal("partial package-name match was accepted")
	}
	match, err := validateRepoPackageSelection(service, "php8.3-fpm")
	if err != nil {
		t.Fatalf("exact package-name match was rejected: %v", err)
	}
	if len(match) == 0 || match[0] != "php8.3-fpm" {
		t.Fatalf("match = %v, want full package name", match)
	}
}

func TestInstallAndUninstallRejectNetdataPackageOverrideBeforeMutation(t *testing.T) {
	agent := &Agent{}
	request := &InstallServiceRequest{ID: "netdata", Package: "bash"}

	var install InstallServiceResponse
	if err := agent.InstallService(request, &install); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(install.Error, "does not offer version selection") {
		t.Fatalf("install error = %q, want version-selection rejection", install.Error)
	}

	var uninstall UninstallServiceResponse
	if err := agent.UninstallService(request, &uninstall); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uninstall.Error, "does not offer version selection") {
		t.Fatalf("uninstall error = %q, want version-selection rejection", uninstall.Error)
	}
}

func TestGenericDNSEngineInstallAndUninstallRejectBeforeLeaseOrHostProbe(t *testing.T) {
	originalDetect := detectHostPlatform
	detectCalls := 0
	detectHostPlatform = func() (hostplatform.Profile, error) {
		detectCalls++
		return hostplatform.Profile{}, errors.New("host probe must not run")
	}
	t.Cleanup(func() { detectHostPlatform = originalDetect })

	agent := &Agent{}
	for _, serviceID := range []string{"bind", "pdns"} {
		t.Run(serviceID, func(t *testing.T) {
			var install InstallServiceResponse
			if err := agent.InstallService(
				&InstallServiceRequest{ID: serviceID}, &install,
			); err != nil {
				t.Fatal(err)
			}
			if install.Error != genericDNSEngineWorkflowRequired {
				t.Fatalf("install error = %q", install.Error)
			}

			var uninstall UninstallServiceResponse
			if err := agent.UninstallService(
				&InstallServiceRequest{ID: serviceID}, &uninstall,
			); err != nil {
				t.Fatal(err)
			}
			if uninstall.Error != genericDNSEngineWorkflowRequired {
				t.Fatalf("uninstall error = %q", uninstall.Error)
			}
		})
	}
	if detectCalls != 0 {
		t.Fatalf("generic DNS refusal performed %d host probes", detectCalls)
	}
}

func TestInjectedGenericDNSUninstallPerformsNoHostOperations(t *testing.T) {
	operationCalls := 0
	called := func() { operationCalls++ }
	ops := serviceUninstallOps{
		detectPackageFamily: func() string { called(); return "pacman" },
		packageInstalled:    func(string) bool { called(); return true },
		unitExists:          func(string) bool { called(); return true },
		unitsMatching:       func(string) []string { called(); return []string{"pdns"} },
		disableUnit:         func(string) error { called(); return nil },
		removePackages: func(string, []string) (string, error) {
			called()
			return "", nil
		},
		installedRepoPackages: func(*core.ManagedService) ([]string, error) {
			called()
			return []string{"powerdns"}, nil
		},
	}

	var response UninstallServiceResponse
	if err := (&Agent{}).uninstallServiceWithOps(
		&InstallServiceRequest{ID: "pdns"}, &response, ops,
	); err != nil {
		t.Fatal(err)
	}
	if response.Error != genericDNSEngineWorkflowRequired {
		t.Fatalf("uninstall error = %q", response.Error)
	}
	if operationCalls != 0 {
		t.Fatalf("generic DNS uninstall performed %d injected host operations", operationCalls)
	}
}

func TestGeneralPostgreSQLUninstallPurgesInstalledRepoPackagesNotUnits(t *testing.T) {
	var disabled []string
	var removed []string
	ops := serviceUninstallOps{
		detectPackageFamily: func() string { return "apt" },
		packageInstalled:    func(string) bool { return false },
		unitExists:          func(unit string) bool { return unit == "postgresql" },
		unitsMatching: func(pattern string) []string {
			if pattern != `^postgresql@` {
				t.Fatalf("unit pattern = %q", pattern)
			}
			return []string{"postgresql@17-main"}
		},
		disableUnit: func(unit string) error {
			disabled = append(disabled, unit)
			return nil
		},
		removePackages: func(family string, packages []string) (string, error) {
			if family != "apt" {
				t.Fatalf("package family = %q", family)
			}
			removed = append([]string(nil), packages...)
			return "", nil
		},
		installedRepoPackages: func(service *core.ManagedService) ([]string, error) {
			if service == nil || service.ID != "postgresql" {
				t.Fatalf("repo package service = %+v", service)
			}
			return []string{"postgresql-17"}, nil
		},
	}

	var response UninstallServiceResponse
	if err := (&Agent{}).uninstallServiceWithOps(
		&InstallServiceRequest{ID: "postgresql"},
		&response,
		ops,
	); err != nil {
		t.Fatal(err)
	}
	if !response.Removed || response.Error != "" {
		t.Fatalf("response = %+v", response)
	}
	if want := []string{"postgresql", "postgresql-17"}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed packages = %v, want %v", removed, want)
	}
	for _, packageName := range removed {
		if packageName == "postgresql@17-main" {
			t.Fatal("systemd instance name was passed to the package manager")
		}
	}
	if want := []string{"postgresql", "postgresql@17-main"}; !reflect.DeepEqual(disabled, want) {
		t.Fatalf("disabled units = %v, want %v", disabled, want)
	}
}

func TestUninstallPurgeFailureReportsAppliedStopAsPartialSuccess(t *testing.T) {
	ops := serviceUninstallOps{
		detectPackageFamily: func() string { return "apt" },
		packageInstalled:    func(string) bool { return false },
		unitExists:          func(unit string) bool { return unit == "redis-server" },
		unitsMatching:       func(string) []string { return nil },
		disableUnit:         func(string) error { return nil },
		removePackages: func(string, []string) (string, error) {
			return "", errors.New("apt purge failed")
		},
		installedRepoPackages: func(*core.ManagedService) ([]string, error) {
			return nil, nil
		},
	}

	var response UninstallServiceResponse
	if err := (&Agent{}).uninstallServiceWithOps(
		&InstallServiceRequest{ID: "redis"},
		&response,
		ops,
	); err != nil {
		t.Fatal(err)
	}
	if response.Removed || !response.PartialSuccess || !response.MutationApplied {
		t.Fatalf("partial failure contract lost: %+v", response)
	}
	if !strings.Contains(response.Error, "apt purge failed") {
		t.Fatalf("error = %q", response.Error)
	}
}

func TestUninstallDisableFailureAbortsPurgeAndMarksStateUncertain(t *testing.T) {
	disableCalls := 0
	removeCalls := 0
	ops := serviceUninstallOps{
		detectPackageFamily: func() string { return "apt" },
		packageInstalled:    func(string) bool { return false },
		unitExists:          func(unit string) bool { return unit == "redis-server" },
		unitsMatching:       func(string) []string { return nil },
		disableUnit: func(unit string) error {
			disableCalls++
			if unit != "redis-server" {
				t.Fatalf("disable unit = %q", unit)
			}
			return errors.New("systemctl disable failed after stop")
		},
		removePackages: func(string, []string) (string, error) {
			removeCalls++
			return "", nil
		},
		installedRepoPackages: func(*core.ManagedService) ([]string, error) {
			return nil, nil
		},
	}

	var response UninstallServiceResponse
	if err := (&Agent{}).uninstallServiceWithOps(
		&InstallServiceRequest{ID: "redis"},
		&response,
		ops,
	); err != nil {
		t.Fatal(err)
	}
	if disableCalls != 1 || removeCalls != 0 {
		t.Fatalf("calls: disable=%d remove=%d, want disable=1 remove=0", disableCalls, removeCalls)
	}
	if response.Removed || !response.PartialSuccess || !response.MutationApplied {
		t.Fatalf("state-uncertain failure contract lost: %+v", response)
	}
	if !strings.Contains(response.Error, "service state may have changed") {
		t.Fatalf("error = %q", response.Error)
	}
}

func TestUninstallRepoDiscoveryFailsBeforeStoppingUnits(t *testing.T) {
	disableCalls := 0
	ops := serviceUninstallOps{
		detectPackageFamily: func() string { return "apt" },
		packageInstalled:    func(string) bool { return false },
		unitExists:          func(string) bool { return false },
		unitsMatching:       func(string) []string { return []string{"postgresql@17-main"} },
		disableUnit: func(string) error {
			disableCalls++
			return nil
		},
		removePackages: func(string, []string) (string, error) {
			t.Fatal("package removal ran after discovery failure")
			return "", nil
		},
		installedRepoPackages: func(*core.ManagedService) ([]string, error) {
			return nil, errors.New("dpkg query failed")
		},
	}

	var response UninstallServiceResponse
	if err := (&Agent{}).uninstallServiceWithOps(
		&InstallServiceRequest{ID: "postgresql"},
		&response,
		ops,
	); err != nil {
		t.Fatal(err)
	}
	if disableCalls != 0 || response.MutationApplied || response.PartialSuccess {
		t.Fatalf("pre-mutation failure changed host contract: calls=%d response=%+v", disableCalls, response)
	}
	if !strings.Contains(response.Error, "installed package discovery failed") {
		t.Fatalf("error = %q", response.Error)
	}
}
