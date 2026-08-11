package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	serviceOperationKindMailProfileInstall = "mail_profile_install"
	mailProfileInstallPath                 = "/api/v1/service/profile/install"

	mailProfileStatusUnknown   = "unknown"
	mailProfileStatusAvailable = "available"
	mailProfileStatusPartial   = "partial"
	mailProfileStatusComplete  = "complete"
	mailProfileStatusBlocked   = "blocked"

	mailProfileFallbackWarning       = "No active trusted Secure Mail certificate was found. Mail submission is using the local self-signed fallback; activate a trusted certificate with Secure Mail enabled."
	mailProfileReconciliationWarning = "Components are running. Run Repair to verify and reconcile the complete mail profile, including mail TLS and authenticated submission."
)

// readMailProfileHostname is a test seam around the host fact used by the
// production preflight. It always points at os.Hostname outside tests.
var readMailProfileHostname = os.Hostname

type mailProfileDefinition struct {
	ID          string
	Name        string
	Description string
	Services    []string
}

var mailProfileDefinitions = [...]mailProfileDefinition{
	{
		ID:          "core-mail",
		Name:        "Core Mail",
		Description: "Postfix SMTP and Dovecot IMAP/POP3 with authenticated submission.",
		Services:    []string{"postfix", "dovecot"},
	},
	{
		ID:          "webmail",
		Name:        "Webmail",
		Description: "Core mail plus Nginx, PHP-FPM and Roundcube webmail.",
		Services:    []string{"postfix", "dovecot", "nginx", "php-fpm", "roundcube"},
	},
	{
		ID:          "protected-mail",
		Name:        "Spam-Protected Mail",
		Description: "Core mail with Rspamd spam filtering.",
		Services:    []string{"postfix", "dovecot", "rspamd"},
	},
}

type mailProfileInstallRequest struct {
	ProfileID string `json:"profile_id"`
	RequestID string `json:"request_id"`
}

type mailProfileTLSResult struct {
	Configured   bool `json:"configured"`
	SNICount     int  `json:"sni_count"`
	FallbackOnly bool `json:"fallback_only"`
}

type deferServiceInstallFirewallKey struct{}

func withDeferredServiceInstallFirewall(ctx context.Context) context.Context {
	return context.WithValue(ctx, deferServiceInstallFirewallKey{}, true)
}

func serviceInstallFirewallDeferred(ctx context.Context) bool {
	deferred, _ := ctx.Value(deferServiceInstallFirewallKey{}).(bool)
	return deferred
}

// MailProfileResponse is derived entirely on the server. Clients choose one
// profile id; they can never supply or reorder the privileged service plan.
type MailProfileResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Services      []string `json:"services"`
	Status        string   `json:"status"`
	Available     bool     `json:"available"`
	BlockedReason string   `json:"blocked_reason,omitempty"`
	Warning       string   `json:"warning,omitempty"`
}

func mailProfileByID(id string) (mailProfileDefinition, bool) {
	for _, profile := range mailProfileDefinitions {
		if profile.ID != id {
			continue
		}
		profile.Services = append([]string(nil), profile.Services...)
		return profile, true
	}
	return mailProfileDefinition{}, false
}

func allMailProfiles() []mailProfileDefinition {
	profiles := make([]mailProfileDefinition, 0, len(mailProfileDefinitions))
	for _, definition := range mailProfileDefinitions {
		profile, _ := mailProfileByID(definition.ID)
		profiles = append(profiles, profile)
	}
	return profiles
}

// handleMailProfileInstall creates one durable operation and one privileged
// agent lease for the complete, server-owned plan.
func (p *Panel) handleMailProfileInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	caller := currentCaller(r)
	if caller == nil || !caller.hasAccountRole(roleAdmin) {
		writeCodedError(w, http.StatusForbidden, errCodeAdminOnly, "administrator access required", "")
		return
	}

	var request mailProfileInstallRequest
	if err := decodeServiceOperationJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.ProfileID = strings.TrimSpace(request.ProfileID)
	if _, ok := mailProfileByID(request.ProfileID); !ok {
		writeClientError(w, http.StatusBadRequest, "unknown mail profile")
		return
	}
	if !validServiceOperationID(request.RequestID) {
		writeClientError(w, http.StatusBadRequest, "invalid request_id")
		return
	}

	existing, found, err := p.idempotentServiceOperation(
		r.Context(), request.RequestID, serviceOperationKindMailProfileInstall, request.ProfileID, "",
	)
	if err != nil {
		if errors.Is(err, errServiceOperationRequestConflict) {
			writeServiceOperationRequestConflict(w)
			return
		}
		writeServerError(w, err)
		return
	}
	if found {
		writeAcceptedServiceOperation(w, existing)
		return
	}

	release, busy := p.beginServiceMutation(w, r)
	if busy {
		return
	}
	releaseInHandler := true
	defer func() {
		if releaseInHandler {
			release()
		}
	}()

	actor := captureServiceOperationActor(r)
	op, err := p.createServiceOperationRequest(
		r.Context(), serviceOperationKindMailProfileInstall, request.ProfileID, "", request.RequestID, actor,
	)
	switch {
	case errors.Is(err, errServiceOperationBusy):
		writeServiceOperationBusy(w)
		return
	case errors.Is(err, errServiceOperationReplay):
		writeAcceptedServiceOperation(w, op)
		return
	case errors.Is(err, errServiceOperationRequestConflict):
		writeServiceOperationRequestConflict(w)
		return
	case err != nil:
		writeServerError(w, err)
		return
	}

	p.launchServiceOperation(
		op,
		actor,
		mailProfilePhase(request.ProfileID, "preflight"),
		"mail.profile.install:"+request.ProfileID,
		"mail.profile.install.failed:"+request.ProfileID,
		release,
		func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
			return p.runMailProfileInstall(ctx, request.ProfileID, advance)
		},
	)
	releaseInHandler = false
	writeAcceptedServiceOperation(w, op)
}

func mailProfilePhase(profileID string, parts ...string) string {
	phase := "profile/" + profileID
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			phase += "/" + part
		}
	}
	return phase
}

func newMailProfileResult(profile mailProfileDefinition) serviceOperationResult {
	return serviceOperationResult{
		"success":               false,
		"profile_id":            profile.ID,
		"services":              append([]string(nil), profile.Services...),
		"completed_services":    []string{},
		"mail_tls":              mailProfileTLSResult{},
		"submission_configured": false,
		"warnings":              []string{},
	}
}

func mailProfileInstallFailure(cause error) *serviceOperationFailure {
	return operationFailure(
		"mail_profile_install_failed",
		"The mail profile could not be installed and verified.",
		cause,
	)
}

func (p *Panel) runMailProfileInstall(
	ctx context.Context,
	profileID string,
	advance func(string) error,
) (serviceOperationResult, *serviceOperationFailure) {
	profile, ok := mailProfileByID(profileID)
	if !ok {
		return serviceOperationResult{"success": false},
			mailProfileInstallFailure(errors.New("unknown mail profile"))
	}
	result := newMailProfileResult(profile)
	if err := advance(mailProfilePhase(profile.ID, "preflight")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	if _, err := p.preflightMailProfileInstall(ctx, profile); err != nil {
		return result, mailProfileInstallFailure(err)
	}
	binding, err := panelMutationBinding(ctx)
	if err != nil {
		return result, mailProfileInstallFailure(err)
	}

	completed := make([]string, 0, len(profile.Services))
	componentCtx := withDeferredServiceInstallFirewall(ctx)
	for _, serviceID := range profile.Services {
		if err := advance(mailProfilePhase(profile.ID, serviceID, "installing")); err != nil {
			return result, operationAdvanceFailure(err)
		}
		serviceAdvance := func(phase string) error {
			return advance(mailProfilePhase(profile.ID, serviceID, phase))
		}
		_, failure := p.runServiceInstall(componentCtx, serviceInstallRequest{
			ServiceID: serviceID,
			RequestID: binding.MutationRequestID,
		}, serviceAdvance)
		if failure != nil {
			if failure.Cause != nil {
				failure.Cause = fmt.Errorf("install profile service %s: %w", serviceID, failure.Cause)
			}
			return result, failure
		}
		completed = append(completed, serviceID)
		result["completed_services"] = append([]string(nil), completed...)
	}

	mutationRequest := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
	if err := advance(mailProfilePhase(profile.ID, "mail-stack")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	var mailStack transport.ConfigureMailStackResponse
	if err := p.agentClient.CallContext(ctx, "Agent.ConfigureMailStack", &mutationRequest, &mailStack); err != nil {
		return result, mailProfileInstallFailure(fmt.Errorf("final mail stack configuration: %w", err))
	}
	if mailStack.Error != "" {
		return result, mailProfileInstallFailure(fmt.Errorf("final mail stack configuration: %s", mailStack.Error))
	}
	if !mailStack.Configured {
		return result, mailProfileInstallFailure(errors.New("agent did not confirm final mail stack configuration"))
	}

	if err := advance(mailProfilePhase(profile.ID, "mail-tls")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	mailTLS, err := p.reconcileMailTLSMutation(ctx)
	if err != nil {
		return result, mailProfileInstallFailure(fmt.Errorf("mail TLS reconciliation: %w", err))
	}
	tlsResult := mailProfileTLSResult{
		Configured:   true,
		SNICount:     mailTLS.SNICount,
		FallbackOnly: mailTLS.SNICount == 0,
	}
	result["mail_tls"] = tlsResult
	if tlsResult.FallbackOnly {
		result["warnings"] = []string{mailProfileFallbackWarning}
	}

	if err := advance(mailProfilePhase(profile.ID, "submission")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	var submission transport.ConfigureMailSubmissionResponse
	if err := p.agentClient.CallContext(ctx, "Agent.ConfigureMailSubmission", &mutationRequest, &submission); err != nil {
		return result, mailProfileInstallFailure(fmt.Errorf("mail submission configuration: %w", err))
	}
	if submission.Error != "" {
		return result, mailProfileInstallFailure(fmt.Errorf("mail submission configuration: %s", submission.Error))
	}
	if !submission.Configured {
		return result, mailProfileInstallFailure(errors.New("agent did not confirm mail submission configuration"))
	}
	result["submission_configured"] = true

	if err := advance(mailProfilePhase(profile.ID, "verifying")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	services, err := p.scanManagedServices(ctx)
	if err != nil {
		return result, mailProfileInstallFailure(fmt.Errorf("final mail profile scan: %w", err))
	}
	if err := verifyMailProfileReady(profile, services); err != nil {
		return result, mailProfileInstallFailure(err)
	}
	if err := p.verifyMailProfileRuntimeProof(ctx, profile); err != nil {
		return result, mailProfileInstallFailure(err)
	}

	if err := advance(mailProfilePhase(profile.ID, "firewall")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	if err := p.syncFirewall(ctx); err != nil {
		return result, firewallSyncFailure(err)
	}
	result["success"] = true
	return result, nil
}

// preflightMailProfileInstall proves the whole plan before the first package
// or portable-application mutation. Requirements are checked against the
// projected final set, never incrementally against client-supplied ordering.
func (p *Panel) preflightMailProfileInstall(
	ctx context.Context,
	profile mailProfileDefinition,
) ([]ManagedServiceResponse, error) {
	if _, err := panelMutationBinding(ctx); err != nil {
		return nil, err
	}
	if err := p.validateMailProfileHostAndCatalog(ctx, profile); err != nil {
		return nil, err
	}
	services, err := p.scanManagedServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("mail profile preflight scan: %w", err)
	}
	installed := make(map[string]bool, len(services)+len(profile.Services))
	observed := make(map[string]bool, len(services))
	for _, service := range services {
		observed[service.ID] = true
		if service.IsInstalled {
			installed[service.ID] = true
		}
	}
	projected := make(map[string]bool, len(installed)+len(profile.Services))
	for id, present := range installed {
		projected[id] = present
	}
	for _, id := range profile.Services {
		if !observed[id] {
			return nil, fmt.Errorf("fresh scan did not return catalogue service %s", id)
		}
		projected[id] = true
	}
	for _, id := range profile.Services {
		managed := core.GetManagedServiceByID(id)
		if taken := core.SeatTakenBy(managed, projected); taken != "" {
			return nil, fmt.Errorf("%s cannot be installed while %s occupies the same service role", managed.Name, taken)
		}
		if missing := core.RequirementsMissing(managed, projected); len(missing) > 0 {
			return nil, fmt.Errorf("%s requires %s outside this profile", managed.Name, strings.Join(missing, ", "))
		}
	}
	return services, nil
}

func (p *Panel) validateMailProfileHostAndCatalog(ctx context.Context, profile mailProfileDefinition) error {
	if err := p.requireMatchingAgentBuild(ctx); err != nil {
		return err
	}
	rawHostname, err := readMailProfileHostname()
	if err != nil {
		return fmt.Errorf("read mail profile server hostname: %w", err)
	}
	if _, err := hostname.CanonicalFQDN(rawHostname); err != nil {
		return fmt.Errorf("mail profile requires a canonical server FQDN: %w", err)
	}
	family := p.packageFamily()
	for _, id := range profile.Services {
		managed := core.GetManagedServiceByID(id)
		if managed == nil {
			return fmt.Errorf("mail profile contains unknown managed service %s", id)
		}
		if reason := core.ManagedServiceInstallDisabledReason(managed, family); reason != "" {
			return fmt.Errorf("%s: %s", managed.Name, reason)
		}
		if err := p.preflightManagedServiceRepository(ctx, managed, family, ""); err != nil {
			return err
		}
	}
	return nil
}

func verifyMailProfileReady(profile mailProfileDefinition, services []ManagedServiceResponse) error {
	for _, serviceID := range profile.Services {
		managed := core.GetManagedServiceByID(serviceID)
		if managed == nil {
			return fmt.Errorf("final scan contains unknown profile service %s", serviceID)
		}
		ready := false
		for _, service := range services {
			if service.ID != serviceID || !service.IsInstalled {
				continue
			}
			ready = managed.Kind == core.KindTool ||
				strings.HasPrefix(strings.ToLower(strings.TrimSpace(service.Status)), "active")
			break
		}
		if !ready {
			return fmt.Errorf("final scan did not find %s in its required state", serviceID)
		}
	}
	return nil
}

func (p *Panel) verifyMailProfileRuntimeProof(ctx context.Context, profile mailProfileDefinition) error {
	if profile.ID != "webmail" {
		return nil
	}
	ready, proven, err := p.cachedWebmailReadinessProof(ctx)
	if err != nil {
		return fmt.Errorf("read final webmail readiness proof: %w", err)
	}
	if !proven || !ready {
		return errors.New("Roundcube webmail did not pass its final Unix-socket readiness probe")
	}
	return nil
}

// reconstructSucceededMailProfileResult closes the narrow crash window after
// the agent durably committed success but before the panel stored its result.
// The agent terminal state proves all mutation steps completed; current fresh
// reads reconstruct the truthful profile membership and fallback warning.
func (p *Panel) reconstructSucceededMailProfileResult(
	ctx context.Context,
	profile mailProfileDefinition,
) (serviceOperationResult, error) {
	if err := p.validateMailProfileHostAndCatalog(ctx, profile); err != nil {
		return nil, err
	}
	services, err := p.scanManagedServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("recover mail profile scan: %w", err)
	}
	if err := verifyMailProfileReady(profile, services); err != nil {
		return nil, err
	}
	if err := p.verifyMailProfileRuntimeProof(ctx, profile); err != nil {
		return nil, err
	}

	mailTLSSyncMu.Lock()
	_, sni, err := p.loadMailTLSSnapshotLocked(ctx, 0)
	mailTLSSyncMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("reconstruct mail TLS result: %w", err)
	}
	result := newMailProfileResult(profile)
	result["completed_services"] = append([]string(nil), profile.Services...)
	result["mail_tls"] = mailProfileTLSResult{
		Configured:   true,
		SNICount:     len(sni),
		FallbackOnly: len(sni) == 0,
	}
	result["submission_configured"] = true
	result["recovered"] = true
	result["success"] = true
	if len(sni) == 0 {
		result["warnings"] = []string{mailProfileFallbackWarning}
	}
	return result, nil
}

func mailProfilesView(
	services []ManagedServiceResponse,
	verified bool,
	packageFamily string,
	webmailReady bool,
	webmailProven bool,
) []MailProfileResponse {
	byID := make(map[string]ManagedServiceResponse, len(services))
	installed := make(map[string]bool, len(services))
	for _, service := range services {
		byID[service.ID] = service
		if service.IsInstalled {
			installed[service.ID] = true
		}
	}

	profiles := make([]MailProfileResponse, 0, len(mailProfileDefinitions))
	for _, definition := range allMailProfiles() {
		view := MailProfileResponse{
			ID:          definition.ID,
			Name:        definition.Name,
			Description: definition.Description,
			Services:    append([]string(nil), definition.Services...),
			Status:      mailProfileStatusUnknown,
			Available:   false,
		}
		if !verified {
			view.BlockedReason = "Service state is unverified; run a fresh scan."
			profiles = append(profiles, view)
			continue
		}

		projected := make(map[string]bool, len(installed)+len(definition.Services))
		for id, present := range installed {
			projected[id] = present
		}
		incompleteObservation := false
		for _, id := range definition.Services {
			if _, ok := byID[id]; !ok {
				incompleteObservation = true
			}
			projected[id] = true
		}
		if incompleteObservation {
			view.BlockedReason = "Fresh service state is incomplete; run another scan."
			profiles = append(profiles, view)
			continue
		}

		blockedReason := ""
		for _, id := range definition.Services {
			managed := core.GetManagedServiceByID(id)
			if managed == nil {
				blockedReason = "The server profile catalogue is incomplete."
				break
			}
			if reason := core.ManagedServiceInstallDisabledReason(managed, packageFamily); reason != "" {
				blockedReason = managed.Name + ": " + reason
				break
			}
			if taken := core.SeatTakenBy(managed, projected); taken != "" {
				blockedReason = fmt.Sprintf("%s conflicts with installed %s.", managed.Name, taken)
				break
			}
			if missing := core.RequirementsMissing(managed, projected); len(missing) > 0 {
				blockedReason = fmt.Sprintf("%s requires %s outside this profile.", managed.Name, strings.Join(missing, ", "))
				break
			}
		}
		if blockedReason != "" {
			view.Status = mailProfileStatusBlocked
			view.BlockedReason = blockedReason
			profiles = append(profiles, view)
			continue
		}

		anyInstalled := false
		allReady := true
		for _, id := range definition.Services {
			service := byID[id]
			if service.IsInstalled {
				anyInstalled = true
			}
			if verifyMailProfileReady(mailProfileDefinition{Services: []string{id}}, services) != nil {
				allReady = false
			}
		}
		webmailWarning := ""
		if definition.ID == "webmail" && allReady {
			switch {
			case !webmailProven:
				allReady = false
				webmailWarning = "Webmail runtime readiness is unverified; run a fresh scan."
			case !webmailReady:
				allReady = false
				webmailWarning = "Roundcube is installed, but its Unix-socket runtime is not ready."
			}
		}
		view.Available = true
		switch {
		case allReady:
			view.Status = mailProfileStatusComplete
			// A service scan can prove component membership and runtime state, but
			// only the bound profile operation proves its final MailStack, TLS and
			// submission reconciliations. Keep that distinction visible instead of
			// over-claiming a complete mail setup from component state alone.
			view.Warning = mailProfileReconciliationWarning
		case anyInstalled:
			view.Status = mailProfileStatusPartial
			if webmailWarning != "" {
				view.Warning = webmailWarning
			} else {
				view.Warning = "Some components are already present. Installing this profile will reconcile the full plan without removing completed services."
			}
		default:
			view.Status = mailProfileStatusAvailable
		}
		profiles = append(profiles, view)
	}
	return profiles
}
