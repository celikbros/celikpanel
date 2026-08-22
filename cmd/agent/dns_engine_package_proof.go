package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

type dnsPackageProofRunner func(context.Context, string, ...string) ([]byte, error)

type dnsPackageProofExitStatus interface {
	ExitCode() int
}

func exactDNSEnginePackageInstalled(
	ctx context.Context,
	profile hostplatform.Profile,
	packageName string,
) (bool, error) {
	if ctx == nil || !validPackageName(packageName) {
		return false, errors.New("invalid DNS package proof")
	}
	executable, err := executableForProfile(
		profile, string(profile.PackageManager),
		map[hostplatform.PackageManager]string{
			hostplatform.PackageManagerAPT:    "dpkg-query",
			hostplatform.PackageManagerPacman: "pacman",
		}[profile.PackageManager],
	)
	if err != nil {
		return false, err
	}
	return exactDNSEnginePackageInstalledWithRunner(
		ctx, profile, executable, packageName,
		func(commandCtx context.Context, name string, args ...string) ([]byte, error) {
			command := serviceMutationCommand(commandCtx, name, args...)
			command.Env = bindSafeAPTCommandEnvironment()
			return command.CombinedOutputLimited(64 << 10)
		},
	)
}

func exactDNSEnginePackageInstalledWithRunner(
	ctx context.Context,
	profile hostplatform.Profile,
	executable, packageName string,
	runner dnsPackageProofRunner,
) (bool, error) {
	if ctx == nil || executable == "" || !validPackageName(packageName) ||
		runner == nil {
		return false, errors.New("invalid DNS package command proof")
	}
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT:
		output, err := runner(
			ctx, executable, "-W", "-f", "${Status}", "--", packageName,
		)
		if err == nil {
			if string(output) != "install ok installed" {
				return false, errors.New("dpkg-query returned a non-canonical package status")
			}
			return true, nil
		}
		if ctx.Err() != nil {
			return false, fmt.Errorf("inspect APT DNS package %s: %w", packageName, ctx.Err())
		}
		missing := fmt.Sprintf(
			"dpkg-query: no packages found matching %s\n", packageName,
		)
		exitStatus, exactExitStatus := err.(dnsPackageProofExitStatus)
		if exactExitStatus && exitStatus.ExitCode() == 1 &&
			string(output) == missing {
			return false, nil
		}
		return false, fmt.Errorf("inspect APT DNS package %s: %w", packageName, err)
	case hostplatform.PackageManagerPacman:
		output, err := runner(ctx, executable, "-Q", "--", packageName)
		if err == nil {
			line := strings.TrimSuffix(string(output), "\n")
			fields := strings.Split(line, " ")
			if len(fields) != 2 || fields[0] != packageName ||
				fields[1] == "" || strings.ContainsAny(fields[1], "\x00\r\n\t ") {
				return false, errors.New("pacman returned a non-canonical package status")
			}
			return true, nil
		}
		if ctx.Err() != nil {
			return false, fmt.Errorf("inspect pacman DNS package %s: %w", packageName, ctx.Err())
		}
		missing := "error: package '" + packageName + "' was not found\n"
		exitStatus, exactExitStatus := err.(dnsPackageProofExitStatus)
		if exactExitStatus && exitStatus.ExitCode() == 1 &&
			string(output) == missing {
			return false, nil
		}
		return false, fmt.Errorf("inspect pacman DNS package %s: %w", packageName, err)
	default:
		return false, errors.New("DNS package proof is unsupported on this package manager")
	}
}
