package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// preflightManagedServiceInstall is the backend gate shared by package,
// portable-tool and runtime installs. The browser may disable an Install
// button, but every API path must independently prove that this host supports
// the component, its prerequisites exist, its exclusive seat is free and a
// required repository is enabled before the agent mutates the machine.
// preflightManagedServiceInstall; paket, taşınabilir araç ve runtime
// kurulumlarının ortak backend kapısıdır. Tarayıcı düğmeyi kapatsa da her API
// yolu; agent makineyi değiştirmeden önce host desteğini, önkoşulları, özel
// servis yuvasının boş olduğunu ve zorunlu deponun etkinliğini bağımsız olarak
// doğrular.
func (p *Panel) preflightManagedServiceInstall(ctx context.Context, serviceID, selectedPackage string) error {
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

	requiresRepo, err := core.InstallRequiresManagedRepository(managed, selectedPackage)
	if err != nil {
		return fmt.Errorf("%s repository policy: %w", managed.Name, err)
	}
	if family == "apt" && requiresRepo {
		var status transport.RepoStatusResponse
		if err := p.callAgentContext(ctx, "Agent.RepoStatus", &transport.EnableRepoRequest{RepoID: managed.Repo.ID}, &status); err != nil {
			log.Printf("[repo][%s][install-preflight][transport] %v", managed.Repo.ID, err)
			return errors.New("required repository status could not be verified")
		}
		if status.Error != "" {
			code := normalizeRepoErrorCode(status.ErrorCode, errCodeRepoStatusFailed)
			if status.Repairable {
				code = normalizeRepoErrorCode(status.ErrorCode, errCodeRepoConfigurationInvalid)
			}
			log.Printf("[repo][%s][install-preflight][agent][%s] %s", managed.Repo.ID, code, status.Error)
			if status.Repairable {
				return errors.New("required repository configuration needs repair")
			}
			return errors.New("required repository status could not be verified")
		}
		if !status.Enabled {
			if selectedPackage = strings.TrimSpace(selectedPackage); selectedPackage != "" {
				return fmt.Errorf("selected package %s requires the %s repository; enable it from Services first",
					selectedPackage, managed.Repo.Name)
			}
			return fmt.Errorf("%s requires the %s repository; enable it from Services first",
				managed.Name, managed.Repo.Name)
		}
	}
	return nil
}
