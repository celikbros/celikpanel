package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRoundcubePHPCapabilityProbeCoversServedRuntime(t *testing.T) {
	for _, token := range []string{
		"mb_internal_encoding",
		"DOMDocument",
		"extension_loaded('intl')",
		"PDO::getAvailableDrivers()",
		"ZipArchive",
	} {
		if !strings.Contains(roundcubePHPCapabilityProbe, token) {
			t.Fatalf("PHP capability probe does not check %q", token)
		}
	}
}

func TestPacmanRoundcubePHPConfigEnablesEveryRequiredModule(t *testing.T) {
	if pacmanRoundcubePHPConfigFile != "celikpanel-sqlite.ini" {
		t.Fatalf("managed Arch PHP config moved away from the upgrade-safe filename: %q", pacmanRoundcubePHPConfigFile)
	}
	for _, module := range []string{"intl", "pdo_sqlite", "sqlite3", "zip"} {
		line := "extension=" + module + "\n"
		if strings.Count(pacmanRoundcubePHPConfig, line) != 1 {
			t.Fatalf("managed Arch PHP config count for %q = %d, want 1", line, strings.Count(pacmanRoundcubePHPConfig, line))
		}
	}
	for _, builtIn := range []string{"mbstring", "dom"} {
		if strings.Contains(pacmanRoundcubePHPConfig, "extension="+builtIn+"\n") {
			t.Fatalf("managed Arch PHP config tries to dynamically load built-in module %q", builtIn)
		}
	}
}

func TestRoundcubePHPExtensionPackagesAPTUsesDetectedVersion(t *testing.T) {
	got, err := roundcubePHPExtensionPackages("apt", "8.4", roundcubePHPCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"php8.4-mbstring",
		"php8.4-xml",
		"php8.4-intl",
		"php8.4-sqlite3",
		"php8.4-zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("APT Roundcube PHP packages = %#v, want %#v", got, want)
	}
}

func TestRoundcubePHPExtensionPackagesPacmanUsesSplitPackages(t *testing.T) {
	got, err := roundcubePHPExtensionPackages("pacman", "", roundcubePHPCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"php-sqlite"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pacman Roundcube PHP packages = %#v, want %#v", got, want)
	}
}

func TestRoundcubePHPExtensionPackagesDNFUsesStreamSubpackages(t *testing.T) {
	got, err := roundcubePHPExtensionPackages("dnf", "", roundcubePHPCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"php-mbstring",
		"php-xml",
		"php-intl",
		"php-pdo",
		"php-pecl-zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DNF Roundcube PHP packages = %#v, want %#v", got, want)
	}
}

func TestRoundcubePHPExtensionPackagesFailsClosed(t *testing.T) {
	if _, err := roundcubePHPExtensionPackages("apt", "", []string{"mbstring"}); err == nil {
		t.Fatal("APT package mapping accepted an undetected PHP version")
	}
	if _, err := roundcubePHPExtensionPackages("zypper", "8.4", []string{"mbstring"}); err == nil {
		t.Fatal("unsupported package manager was accepted")
	}
	if _, err := roundcubePHPExtensionPackages("pacman", "", []string{"imaginary"}); err == nil {
		t.Fatal("unknown PHP capability was accepted")
	}
}

func TestEnsureRoundcubePHPDependenciesAPTInstallsReloadsAndVerifiesOnce(t *testing.T) {
	probeCalls := 0
	installCalls := 0
	reloadCalls := 0
	enableCalls := 0
	var installed []string
	var reloadedUnit string

	ops := roundcubePHPDependencyOps{
		probe: func(context.Context) ([]string, error) {
			probeCalls++
			if probeCalls == 1 {
				return append([]string(nil), roundcubePHPCapabilities...), nil
			}
			return nil, nil
		},
		install: func(_ context.Context, family string, packages []string) (string, error) {
			installCalls++
			if family != "apt" {
				t.Fatalf("install family = %q, want apt", family)
			}
			installed = append([]string(nil), packages...)
			return "", nil
		},
		enablePacman: func() error {
			enableCalls++
			return nil
		},
		reload: func(_ context.Context, unit string) ([]byte, error) {
			reloadCalls++
			reloadedUnit = unit
			return nil, nil
		},
	}

	if err := ensureRoundcubePHPDependenciesWithOps(context.Background(), "apt", "8.4", ops); err != nil {
		t.Fatal(err)
	}
	wantPackages := []string{
		"php8.4-mbstring",
		"php8.4-xml",
		"php8.4-intl",
		"php8.4-sqlite3",
		"php8.4-zip",
	}
	if !reflect.DeepEqual(installed, wantPackages) {
		t.Fatalf("installed packages = %#v, want %#v", installed, wantPackages)
	}
	if probeCalls != 2 || installCalls != 1 || reloadCalls != 1 || enableCalls != 0 {
		t.Fatalf(
			"calls probe/install/reload/enable = %d/%d/%d/%d, want 2/1/1/0",
			probeCalls, installCalls, reloadCalls, enableCalls,
		)
	}
	if reloadedUnit != "php8.4-fpm" {
		t.Fatalf("reloaded unit = %q, want php8.4-fpm", reloadedUnit)
	}
}

func TestEnsureRoundcubePHPDependenciesDNFInstallsReloadsAndVerifiesOnce(t *testing.T) {
	probeCalls := 0
	installCalls := 0
	reloadCalls := 0
	enableCalls := 0
	var installed []string
	var reloadedUnit string

	ops := roundcubePHPDependencyOps{
		probe: func(context.Context) ([]string, error) {
			probeCalls++
			if probeCalls == 1 {
				return append([]string(nil), roundcubePHPCapabilities...), nil
			}
			return nil, nil
		},
		install: func(_ context.Context, family string, packages []string) (string, error) {
			installCalls++
			if family != "dnf" {
				t.Fatalf("install family = %q, want dnf", family)
			}
			installed = append([]string(nil), packages...)
			return "", nil
		},
		enablePacman: func() error {
			enableCalls++
			return nil
		},
		reload: func(_ context.Context, unit string) ([]byte, error) {
			reloadCalls++
			reloadedUnit = unit
			return nil, nil
		},
	}

	if err := ensureRoundcubePHPDependenciesWithOps(context.Background(), "dnf", "", ops); err != nil {
		t.Fatal(err)
	}
	wantPackages := []string{
		"php-mbstring",
		"php-xml",
		"php-intl",
		"php-pdo",
		"php-pecl-zip",
	}
	if !reflect.DeepEqual(installed, wantPackages) {
		t.Fatalf("installed packages = %#v, want %#v", installed, wantPackages)
	}
	if probeCalls != 2 || installCalls != 1 || reloadCalls != 1 || enableCalls != 0 {
		t.Fatalf(
			"calls probe/install/reload/enable = %d/%d/%d/%d, want 2/1/1/0",
			probeCalls, installCalls, reloadCalls, enableCalls,
		)
	}
	if reloadedUnit != "php-fpm" {
		t.Fatalf("reloaded unit = %q, want php-fpm", reloadedUnit)
	}
}

func TestEnsureRoundcubePHPDependenciesPacmanEnablesAndVerifiesOnce(t *testing.T) {
	probeCalls := 0
	enableCalls := 0
	reloadCalls := 0
	var installed []string
	var reloadedUnit string

	ops := roundcubePHPDependencyOps{
		probe: func(context.Context) ([]string, error) {
			probeCalls++
			if probeCalls == 1 {
				return append([]string(nil), roundcubePHPCapabilities...), nil
			}
			return nil, nil
		},
		install: func(_ context.Context, family string, packages []string) (string, error) {
			if family != "pacman" {
				t.Fatalf("install family = %q, want pacman", family)
			}
			installed = append([]string(nil), packages...)
			return "", nil
		},
		enablePacman: func() error {
			enableCalls++
			return nil
		},
		reload: func(_ context.Context, unit string) ([]byte, error) {
			reloadCalls++
			reloadedUnit = unit
			return nil, nil
		},
	}

	if err := ensureRoundcubePHPDependenciesWithOps(context.Background(), "pacman", "", ops); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(installed, []string{"php-sqlite"}) {
		t.Fatalf("installed packages = %#v", installed)
	}
	if probeCalls != 2 || enableCalls != 1 || reloadCalls != 1 {
		t.Fatalf(
			"calls probe/enable/reload = %d/%d/%d, want 2/1/1",
			probeCalls, enableCalls, reloadCalls,
		)
	}
	if reloadedUnit != "php-fpm" {
		t.Fatalf("reloaded unit = %q, want php-fpm", reloadedUnit)
	}
}

func TestEnsureRoundcubePHPDependenciesSkipsMutationWhenReady(t *testing.T) {
	mutations := 0
	ops := roundcubePHPDependencyOps{
		probe: func(context.Context) ([]string, error) { return nil, nil },
		install: func(context.Context, string, []string) (string, error) {
			mutations++
			return "", nil
		},
		enablePacman: func() error {
			mutations++
			return nil
		},
		reload: func(context.Context, string) ([]byte, error) {
			mutations++
			return nil, nil
		},
	}
	if err := ensureRoundcubePHPDependenciesWithOps(context.Background(), "apt", "8.4", ops); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 {
		t.Fatalf("ready PHP runtime caused %d mutations", mutations)
	}
}

func TestEnsureRoundcubePHPDependenciesFailsWhenFinalCapabilityIsMissing(t *testing.T) {
	probeCalls := 0
	ops := roundcubePHPDependencyOps{
		probe: func(context.Context) ([]string, error) {
			probeCalls++
			return []string{"mbstring"}, nil
		},
		install:      func(context.Context, string, []string) (string, error) { return "", nil },
		enablePacman: func() error { return nil },
		reload:       func(context.Context, string) ([]byte, error) { return nil, nil },
	}
	err := ensureRoundcubePHPDependenciesWithOps(context.Background(), "apt", "8.4", ops)
	if err == nil || !strings.Contains(err.Error(), "mbstring") {
		t.Fatalf("final capability error = %v, want missing mbstring", err)
	}
	if probeCalls != 2 {
		t.Fatalf("probe calls = %d, want initial and final verification", probeCalls)
	}
}

func TestEnsureRoundcubePHPDependenciesDoesNotReloadAfterInstallFailure(t *testing.T) {
	reloadCalls := 0
	ops := roundcubePHPDependencyOps{
		probe: func(context.Context) ([]string, error) { return []string{"mbstring"}, nil },
		install: func(context.Context, string, []string) (string, error) {
			return "", errors.New("repository unavailable")
		},
		enablePacman: func() error { return nil },
		reload: func(context.Context, string) ([]byte, error) {
			reloadCalls++
			return nil, nil
		},
	}
	err := ensureRoundcubePHPDependenciesWithOps(context.Background(), "apt", "8.4", ops)
	if err == nil || !strings.Contains(err.Error(), "repository unavailable") {
		t.Fatalf("install error = %v", err)
	}
	if reloadCalls != 0 {
		t.Fatalf("reload ran %d times after failed package install", reloadCalls)
	}
}

func TestInstallRoundcubeRepairsPHPBeforeExistingInstallReturns(t *testing.T) {
	source, err := os.ReadFile("webmail_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	bodyStart := strings.Index(string(source), "func (a *Agent) InstallRoundcube")
	bodyEnd := strings.Index(string(source), "type webmailCommandRunner")
	if bodyStart < 0 || bodyEnd <= bodyStart {
		t.Fatal("InstallRoundcube source boundaries not found")
	}
	body := string(source[bodyStart:bodyEnd])
	ensureAt := strings.Index(body, "a.ensureRoundcubePHPDependencies")
	existingReturnAt := strings.Index(body, "if installed {")
	if ensureAt < 0 || existingReturnAt < 0 || ensureAt > existingReturnAt {
		t.Fatal("existing Roundcube install can return before PHP dependencies are reconciled")
	}
}
