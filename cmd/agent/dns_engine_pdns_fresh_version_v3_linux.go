//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

// A fresh paired PowerDNS primary is admitted only on the measured PowerDNS
// build (freshPDNSDebian13PackageVersionV3 plus the binary identity). That pin
// was enforced only after apt-get had installed whatever version it chose, so
// a host offering another version got packages installed, a receipt written
// and the unit masked before a refusal that did not say what to do. The
// version the package manager WOULD install is now read before any receipt,
// mask, package or journal effect, and the install itself passes
// "name=version". The post-install identity check stays.
//
// Eşli PowerDNS birincilinin ilk kurulumu yalnız ölçülmüş sürümle kabul
// edilir. Paket yöneticisinin kuracağı sürüm artık her etkiden önce okunur;
// kurulum "ad=sürüm" ile yapılır; kurulum sonrası kimlik denetimi kalır.

type freshPDNSVersionOpsV3 struct {
	// installed returns the installed version of a package that is present.
	installed func(string) (string, error)
	// candidate returns the version apt would install ("" when none).
	candidate func(string) (string, error)
}

func freshPDNSVersionText(value string) string {
	return boundedStatOverrideText(value)
}

// preflightFreshPDNSPackageVersionsWithOpsV3 refuses, before any mutation,
// unless every package of the set is (or would be installed) at the pin.
func preflightFreshPDNSPackageVersionsWithOpsV3(packages, missing []string, ops freshPDNSVersionOpsV3) error {
	if ops.installed == nil || ops.candidate == nil || len(packages) == 0 {
		return errors.New("v3 PowerDNS version preflight is incomplete")
	}
	absent := map[string]bool{}
	for _, name := range missing {
		absent[name] = true
	}
	pin := freshPDNSDebian13PackageVersionV3
	for _, name := range packages {
		if absent[name] {
			offered, err := ops.candidate(name)
			if err != nil {
				return &hostOperatorRefusal{
					sentence: fmt.Sprintf(
						"PowerDNS was not installed and nothing was changed: CelikPanel could not read which %s version the package manager on this server would install. "+
							"Start the same change again; if this repeats, the server owner checks the package sources with `apt-cache policy %s`.",
						name, name),
					cause: err,
				}
			}
			if offered == "" {
				return &hostOperatorRefusal{sentence: fmt.Sprintf(
					"PowerDNS was not installed and nothing was changed: the package manager on this server offers no installable %s, "+
						"and CelikPanel installs a fresh paired PowerDNS primary only with the measured version %s. "+
						"The server owner makes %s %s installable for APT on this server (the Debian 13 repository that carries it), then starts the same change again.",
					name, pin, name, pin)}
			}
			if offered != pin {
				return &hostOperatorRefusal{sentence: fmt.Sprintf(
					"PowerDNS was not installed and nothing was changed: the package manager on this server would install %s %s, "+
						"but CelikPanel installs a fresh paired PowerDNS primary only with the measured version %s. "+
						"The server owner makes %s %s installable for APT on this server (the Debian 13 repository that carries it), "+
						"or waits for a CelikPanel release that names %s as measured, then starts the same change again.",
					name, freshPDNSVersionText(offered), pin, name, pin, freshPDNSVersionText(offered))}
			}
			continue
		}
		installed, err := ops.installed(name)
		if err != nil {
			return &hostOperatorRefusal{
				sentence: fmt.Sprintf(
					"PowerDNS was not installed and nothing was changed: CelikPanel could not read the installed %s version. "+
						"Start the same change again; if this repeats, the server owner checks it with `dpkg -s %s`.",
					name, name),
				cause: err,
			}
		}
		if installed != pin {
			return &hostOperatorRefusal{sentence: fmt.Sprintf(
				"PowerDNS was not installed and nothing was changed: %s %s is already installed on this server, "+
					"but CelikPanel runs a fresh paired PowerDNS primary only with the measured version %s. "+
					"The server owner replaces it with version %s, or waits for a CelikPanel release that names %s as measured, then starts the same change again.",
				name, freshPDNSVersionText(installed), pin, pin, freshPDNSVersionText(installed))}
		}
	}
	return nil
}

func preflightFreshPDNSPackageVersionsV3(ctx context.Context, profile hostplatform.Profile, packages, missing []string) error {
	if ctx == nil || profile.PackageManager != hostplatform.PackageManagerAPT {
		return errors.New("v3 PowerDNS version preflight requires an APT host")
	}
	dpkg, err := executableForProfile(profile, string(profile.PackageManager), "dpkg-query")
	if err != nil {
		return err
	}
	aptGet, err := executableForProfile(profile, string(profile.PackageManager), "apt-get")
	if err != nil {
		return err
	}
	aptCache, err := executableForProfile(profile, string(profile.PackageManager), "apt-cache")
	if err != nil {
		return err
	}
	refreshed := false
	return preflightFreshPDNSPackageVersionsWithOpsV3(packages, missing, freshPDNSVersionOpsV3{
		installed: func(name string) (string, error) {
			command := serviceMutationCommand(ctx, dpkg, "-W", "-f", "${Status}\t${Version}", "--", name)
			command.Env = bindSafeAPTCommandEnvironment()
			output, err := command.CombinedOutputLimited(64 << 10)
			if err != nil {
				return "", err
			}
			version, ok := strings.CutPrefix(string(output), "install ok installed\t")
			if !ok || !validDebianVersion(version) {
				return "", errors.New("dpkg-query returned a non-canonical installed package record")
			}
			return version, nil
		},
		candidate: func(name string) (string, error) {
			// The same lock and freshness rule the install uses; the install
			// re-checks the candidate under the lock and passes name=version.
			packageOperationMu.Lock()
			defer packageOperationMu.Unlock()
			if !refreshed {
				refreshAptListsIfStaleWithExecutable(ctx, time.Hour, aptGet)
				refreshed = true
			}
			return aptInstallCandidateWithExecutable(ctx, aptCache, name)
		},
	})
}

// installFreshPDNSPackagesV3 installs the missing packages at the measured
// version only.
func installFreshPDNSPackagesV3(ctx context.Context, missing []string) (string, error) {
	return installPackagesAtVersionContext(ctx, missing, freshPDNSDebian13PackageVersionV3)
}
