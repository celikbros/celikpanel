package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// preflightManagedServiceInstall is the backend gate shared by package,
// portable-tool and runtime installs. The browser may disable an Install
// button, but every API path must independently prove that this host supports
// the component, its prerequisites exist, its exclusive seat is free and a
// required repository is enabled before the agent mutates the machine.
func (p *Panel) preflightManagedServiceInstall(ctx context.Context, serviceID string) error {
	managed := core.GetManagedServiceByID(serviceID)
	if managed == nil {
		return errors.New("unknown managed service")
	}

	family := p.packageFamily()
	if reason := core.ManagedServiceInstallDisabledReason(managed, family); reason != "" {
		return fmt.Errorf("%s: %s", managed.Name, reason)
	}

	services, err := p.scanManagedServices(ctx)
	if err != nil {
		return fmt.Errorf("service install preflight scan: %w", err)
	}
	installed := make(map[string]bool, len(services))
	for _, service := range services {
		if service.IsInstalled {
			installed[service.ID] = true
		}
	}
	if missing := core.RequirementsMissing(managed, installed); len(missing) > 0 {
		return fmt.Errorf("%s requires %s; install the missing component first",
			managed.Name, strings.Join(missing, ", "))
	}
	if taken := core.SeatTakenBy(managed, installed); taken != "" {
		return fmt.Errorf("%s cannot be installed while %s occupies the same service role",
			managed.Name, taken)
	}

	if family == "apt" && managed.Repo != nil && managed.Repo.Required {
		var status RepoStatusResp
		if err := p.agentClient.CallContext(ctx, "Agent.RepoStatus", &enableRepoReq{RepoID: managed.Repo.ID}, &status); err != nil {
			return fmt.Errorf("required repository status: %w", err)
		}
		if status.Error != "" {
			return fmt.Errorf("required repository status: %s", status.Error)
		}
		if !status.Enabled {
			return fmt.Errorf("%s requires the %s repository; enable it from Services first",
				managed.Name, managed.Repo.Name)
		}
	}
	return nil
}
