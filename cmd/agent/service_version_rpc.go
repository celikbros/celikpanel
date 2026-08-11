package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

type serviceVersionCommand func(name string, args ...string) ([]byte, error)

// ServiceCandidateVersion returns a version only when the catalog lookup,
// package-family lookup and package-manager query all succeed.
func (a *Agent) ServiceCandidateVersion(req *InstallServiceRequest, reply *string) error {
	if req == nil {
		return fmt.Errorf("service candidate version request is required")
	}
	if reply == nil {
		return fmt.Errorf("service candidate version reply is required")
	}
	svc := core.GetManagedServiceByID(req.ID)
	if svc == nil {
		return fmt.Errorf("unknown catalog service %q", req.ID)
	}
	profile, err := verifiedHostProfile("apt")
	if err != nil {
		return err
	}
	aptCache, err := executableForProfile(profile, "apt", "apt-cache")
	if err != nil {
		return err
	}
	version, err := candidateVersionForService(svc, "apt", func(_ string, args ...string) ([]byte, error) {
		return serviceMutationCommand(context.Background(), aptCache, args...).Output()
	})
	if err != nil {
		return err
	}
	*reply = version
	return nil
}

func candidateVersionForService(svc *core.ManagedService, family string, run serviceVersionCommand) (string, error) {
	if svc == nil {
		return "", fmt.Errorf("catalog service is required")
	}
	if family != "apt" {
		if family == "" {
			return "", fmt.Errorf("cannot determine package-manager family for service %q", svc.ID)
		}
		return "", fmt.Errorf("candidate version lookup is not supported for package-manager family %q", family)
	}
	packages := svc.Packages[family]
	if len(packages) == 0 {
		return "", fmt.Errorf("service %q has no package mapping for family %q", svc.ID, family)
	}
	if run == nil {
		return "", fmt.Errorf("candidate version command runner is required")
	}
	out, err := run("apt-cache", "policy", packages[0])
	if err != nil {
		return "", fmt.Errorf("query candidate version for service %q: %w", svc.ID, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		candidate, ok := strings.CutPrefix(strings.TrimSpace(line), "Candidate:")
		if !ok {
			continue
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "(none)" {
			return "", fmt.Errorf("package index has no install candidate for service %q", svc.ID)
		}
		version := cleanAptVersion(candidate)
		if version == "" {
			return "", fmt.Errorf("package index returned an invalid candidate for service %q", svc.ID)
		}
		return version, nil
	}
	return "", fmt.Errorf("package index response has no Candidate field for service %q", svc.ID)
}

func cleanAptVersion(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[i+1:]
	}
	if i := strings.IndexAny(v, "-+~"); i >= 0 {
		v = v[:i]
	}
	return v
}
