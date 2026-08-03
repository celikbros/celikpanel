package main

import (
	"errors"
	"strings"
	"testing"
)

type fakeStrictServiceStateProbe struct {
	units          map[string]struct{}
	unitErr        error
	canonical      map[string]string
	canonicalErr   error
	packages       map[string]struct{}
	packagesErr    error
	roundcube      bool
	roundcubeErr   error
	unitCalls      int
	canonicalCalls int
	packageCalls   int
}

func (f *fakeStrictServiceStateProbe) UnitFiles() (map[string]struct{}, error) {
	f.unitCalls++
	return f.units, f.unitErr
}

func (f *fakeStrictServiceStateProbe) CanonicalUnit(unit string) (string, error) {
	f.canonicalCalls++
	if f.canonicalErr != nil {
		return "", f.canonicalErr
	}
	return f.canonical[unit], nil
}

func (f *fakeStrictServiceStateProbe) InstalledPackages(string) (map[string]struct{}, error) {
	f.packageCalls++
	return f.packages, f.packagesErr
}

func (f *fakeStrictServiceStateProbe) RoundcubeInstalled() (bool, error) {
	return f.roundcube, f.roundcubeErr
}

func TestStrictServiceDiscoveryStopsOnSystemdFailure(t *testing.T) {
	probe := &fakeStrictServiceStateProbe{unitErr: errors.New("forced systemctl failure")}

	_, err := discoverInstalledServiceIDsStrict(probe, "apt")
	if err == nil || !strings.Contains(err.Error(), "forced systemctl failure") {
		t.Fatalf("discovery error = %v, want systemctl failure", err)
	}
	if probe.canonicalCalls != 0 || probe.packageCalls != 0 {
		t.Fatalf("continued after unit discovery failure: canonical=%d package=%d", probe.canonicalCalls, probe.packageCalls)
	}
}

func TestStrictServiceDiscoveryStopsOnPackageDatabaseFailure(t *testing.T) {
	probe := &fakeStrictServiceStateProbe{
		units:       map[string]struct{}{},
		packagesErr: errors.New("forced dpkg-query failure"),
	}

	_, err := discoverInstalledServiceIDsStrict(probe, "apt")
	if err == nil || !strings.Contains(err.Error(), "forced dpkg-query failure") {
		t.Fatalf("discovery error = %v, want package database failure", err)
	}
	if probe.packageCalls != 1 {
		t.Fatalf("package database calls = %d, want 1", probe.packageCalls)
	}
}

func TestStrictServiceDiscoveryRejectsUnknownPackageFamilyBeforeProbing(t *testing.T) {
	probe := &fakeStrictServiceStateProbe{}

	_, err := discoverInstalledServiceIDsStrict(probe, `unknown`)
	if err == nil || !strings.Contains(err.Error(), `unsupported package family`) {
		t.Fatalf(`discovery error = %v, want unsupported package family`, err)
	}
	if probe.unitCalls != 0 {
		t.Fatalf(`unit discovery calls = %d, want 0`, probe.unitCalls)
	}
}

func TestStrictServiceDiscoveryStopsOnRoundcubeStatFailure(t *testing.T) {
	probe := &fakeStrictServiceStateProbe{
		units:        map[string]struct{}{},
		roundcubeErr: errors.New(`forced roundcube stat failure`),
	}

	_, err := discoverInstalledServiceIDsStrict(probe, `apt`)
	if err == nil || !strings.Contains(err.Error(), `forced roundcube stat failure`) {
		t.Fatalf(`discovery error = %v, want Roundcube stat failure`, err)
	}
}
