package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	serviceOperationKindMailProfileInstall   = "mail_profile_install"
	mailProfileProofVersion                  = 1
	mailProfileInstallPath                   = "/api/v1/service/profile/install"
	errCodeMailProfileConfirmationRequired   = "mail_profile_confirmation_required"
	errCodeMailProfileServerHostnameInvalid  = "mail_profile_server_hostname_invalid"
	errCodeMailProfileServerHostnameRequired = "mail_profile_server_hostname_required"

	mailProfileStatusUnknown   = "unknown"
	mailProfileStatusAvailable = "available"
	mailProfileStatusPartial   = "partial"
	mailProfileStatusComplete  = "complete"
	mailProfileStatusBlocked   = "blocked"

	mailProfileAttemptNone       = "none"
	mailProfileAttemptInProgress = "in_progress"
	mailProfileAttemptSucceeded  = "succeeded"
	mailProfileAttemptFailed     = "failed"

	mailProfileFallbackWarning        = "No active trusted Secure Mail certificate was found. Mail submission is using the local self-signed fallback; activate a trusted certificate with Secure Mail enabled."
	mailProfileReconciliationWarning  = "Components are running. Run Repair to verify and reconcile the complete mail profile, including mail TLS and authenticated submission."
	mailProfileServerHostnameMessage  = "The mail hostname must be a fully qualified domain name, such as mail.example.com."
	mailProfileServerHostnameRequired = "This server does not have a fully qualified name yet and CelikPanel has none saved for it. Enter the name the mail server should answer as, such as mail.example.com; the installation gives this server that name."
	mailProfileServerHostnameUnknown  = "This server's own hostname could not be read, so the mail hostname cannot be decided."
)

var errMailProfileServerHostnameInvalid = errors.New("mail profile server hostname is not a canonical FQDN")

// errMailProfileServerHostnameRequired is the refusal that now replaces the
// dead end: the panel holds no fully qualified name for this server and the
// operator was not asked for one. It names a field to fill rather than a fact
// to fix somewhere else.
// errMailProfileServerHostnameRequired, çıkmaz sokağın yerini alan reddir:
// panelin bu sunucu için tam nitelikli bir adı yok ve operatöre sorulmamış.
// Başka bir yerde düzeltilecek bir olguyu değil, doldurulacak bir alanı
// adlandırır.
var errMailProfileServerHostnameRequired = errors.New("mail profile server hostname was not supplied and could not be derived")

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
		ID:          core.MailProfileCore,
		Name:        "Core Mail",
		Description: "Postfix SMTP and Dovecot IMAP/POP3 with authenticated submission.",
		Services:    mustMailProfileServiceIDs(core.MailProfileCore),
	},
	{
		ID:          core.MailProfileWebmail,
		Name:        "Webmail",
		Description: "Core mail plus Nginx, PHP-FPM and Roundcube webmail.",
		Services:    mustMailProfileServiceIDs(core.MailProfileWebmail),
	},
	{
		ID:          core.MailProfileProtected,
		Name:        "Spam-Protected Mail",
		Description: "Core mail with Rspamd spam filtering.",
		Services:    mustMailProfileServiceIDs(core.MailProfileProtected),
	},
}

func mustMailProfileServiceIDs(profileID string) []string {
	services, ok := core.MailProfileServiceIDs(profileID)
	if !ok {
		panic("unknown compiled mail profile: " + profileID)
	}
	return services
}

type mailProfileInstallRequest struct {
	ProfileID string `json:"profile_id"`
	RequestID string `json:"request_id"`
	Confirmed bool   `json:"confirmed"`
	// MailHostname is the name the operator gave on the install screen when
	// the panel held no identity to derive one from. It is validated here and
	// saved as the panel's own mail hostname before the operation starts, so a
	// restart mid-install resumes with exactly the name that was confirmed.
	// MailHostname, panelin türetecek bir kimliği olmadığında operatörün
	// kurulum ekranında verdiği addır. Burada doğrulanır ve işlem başlamadan
	// önce panelin kendi posta ana bilgisayar adı olarak kaydedilir; böylece
	// kurulumun ortasında bir yeniden başlatma, tam olarak onaylanan adla
	// devam eder.
	MailHostname string `json:"mail_hostname"`
}

type mailProfileTLSResult struct {
	Configured   bool `json:"configured"`
	SNICount     int  `json:"sni_count"`
	FallbackOnly bool `json:"fallback_only"`
}

type mailProfileProofReceipt struct {
	Success              bool                       `json:"success"`
	ProofVersion         int                        `json:"proof_version"`
	ProfileID            string                     `json:"profile_id"`
	Services             []string                   `json:"services"`
	CompletedServices    []string                   `json:"completed_services"`
	MailTLS              mailProfileTLSProofReceipt `json:"mail_tls"`
	SubmissionConfigured bool                       `json:"submission_configured"`
}

type mailProfileTLSProofReceipt struct {
	Configured   bool  `json:"configured"`
	SNICount     *int  `json:"sni_count"`
	FallbackOnly *bool `json:"fallback_only"`
}

// MailProfileResponse is derived entirely on the server. Clients choose one
// profile id; they can never supply or reorder the privileged service plan.
type MailProfileResponse struct {
	Verified            bool     `json:"verified"`
	LatestAttemptStatus string   `json:"latest_attempt_status"`
	LatestAttemptError  string   `json:"latest_attempt_error,omitempty"`
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	Services            []string `json:"services"`
	Status              string   `json:"status"`
	Available           bool     `json:"available"`
	BlockedReason       string   `json:"blocked_reason,omitempty"`
	Warning             string   `json:"warning,omitempty"`
}

type mailProfileAttemptProof struct {
	Status   string
	Error    string
	Verified bool
}

// latestMailProfileAttemptProofs reads only the newest durable attempt for
// each profile. The current scan still has to prove every component ready
// before a profile may expose Verified; a receipt alone never makes a stopped
// profile healthy.
func (p *Panel) latestMailProfileAttemptProofs(ctx context.Context) (map[string]mailProfileAttemptProof, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT service_id, status, COALESCE(result_json, ''), COALESCE(error_message, '')
		FROM service_operations
		WHERE kind=?
		ORDER BY started_at DESC, updated_at DESC, created_at DESC, id DESC`,
		serviceOperationKindMailProfileInstall,
	)
	if err != nil {
		return nil, fmt.Errorf("read verified mail profiles: %w", err)
	}
	defer rows.Close()

	proofs := make(map[string]mailProfileAttemptProof, len(mailProfileDefinitions))
	seen := make(map[string]bool, len(mailProfileDefinitions))
	for rows.Next() {
		var profileID, status, resultJSON, errorMessage string
		if err := rows.Scan(&profileID, &status, &resultJSON, &errorMessage); err != nil {
			return nil, fmt.Errorf("decode verified mail profile: %w", err)
		}
		if seen[profileID] {
			continue
		}
		if profile, known := mailProfileByID(profileID); known {
			seen[profileID] = true
			proof := mailProfileAttemptProof{}
			switch status {
			case serviceOperationQueued, serviceOperationRunning:
				proof.Status = mailProfileAttemptInProgress
			case serviceOperationSucceeded:
				proof.Status = mailProfileAttemptSucceeded
				proof.Verified = mailProfileReceiptVerified(profile, resultJSON)
			case serviceOperationFailed:
				proof.Status = mailProfileAttemptFailed
				proof.Error = strings.TrimSpace(errorMessage)
			default:
				// The database status constraint should make this impossible. If
				// storage is corrupted, fail closed as an unresolved attempt.
				proof.Status = mailProfileAttemptInProgress
			}
			proofs[profileID] = proof
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read verified mail profiles: %w", err)
	}
	return proofs, nil
}

// mailProfileReceiptVerified accepts only the versioned proof emitted after
// every server-owned profile phase completed. Legacy or malformed success
// rows remain useful history, but cannot silently acquire today's stronger
// meaning and mark the Dashboard journey done.
func mailProfileReceiptVerified(profile mailProfileDefinition, resultJSON string) bool {
	var receipt mailProfileProofReceipt
	if err := json.Unmarshal([]byte(resultJSON), &receipt); err != nil {
		return false
	}
	return receipt.Success &&
		receipt.ProofVersion == mailProfileProofVersion &&
		receipt.ProfileID == profile.ID &&
		slices.Equal(receipt.Services, profile.Services) &&
		slices.Equal(receipt.CompletedServices, profile.Services) &&
		receipt.SubmissionConfigured &&
		receipt.MailTLS.Configured &&
		receipt.MailTLS.SNICount != nil &&
		*receipt.MailTLS.SNICount >= 0 &&
		receipt.MailTLS.FallbackOnly != nil &&
		*receipt.MailTLS.FallbackOnly == (*receipt.MailTLS.SNICount == 0)
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
	if !request.Confirmed {
		writeCodedError(w, http.StatusBadRequest, errCodeMailProfileConfirmationRequired,
			"confirm the reviewed mail profile plan before starting", "")
		return
	}
	if !validServiceOperationID(request.RequestID) {
		writeClientError(w, http.StatusBadRequest, "invalid request_id")
		return
	}
	// A supplied mail hostname is validated and saved before anything starts,
	// so the operator learns it is unusable in the dialog they typed it in
	// rather than from a failed operation minutes later.
	// Verilen bir posta ana bilgisayar adı, hiçbir şey başlamadan önce
	// doğrulanır ve kaydedilir; böylece operatör adın kullanılamaz olduğunu
	// dakikalar sonra başarısız bir işlemden değil, yazdığı pencerede öğrenir.
	if supplied := strings.TrimSpace(request.MailHostname); supplied != "" {
		canonical, err := hostname.CanonicalFQDN(supplied)
		if err != nil {
			writeCodedError(w, http.StatusBadRequest,
				errCodeMailProfileServerHostnameInvalid,
				mailProfileServerHostnameMessage, "")
			return
		}
		if err := p.setSetting(r.Context(), settingMailHostname, canonical); err != nil {
			writeServerError(w, err)
			return
		}
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
	if err := p.authorizeAgentRPCContext(r.Context(), "Agent.SyncMailTLSV2"); err != nil {
		writeAgentError(w, err, "mail profile Mail TLS V2 platform preflight")
		return
	}
	// Fail closed before taking the process slot or creating the outer row: an
	// older agent must never install the mail stack and discover only afterward
	// that it cannot commit the required direct Mail TLS child.
	if err := p.requireMailTLSSyncV2Agent(r.Context()); err != nil {
		writeAgentError(w, err, "mail profile Mail TLS V2 preflight")
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
		"proof_version":         mailProfileProofVersion,
		"profile_id":            profile.ID,
		"services":              append([]string(nil), profile.Services...),
		"completed_services":    []string{},
		"mail_tls":              mailProfileTLSResult{},
		"submission_configured": false,
		"warnings":              []string{},
	}
}

func mailProfileInstallFailure(cause error) *serviceOperationFailure {
	if errors.Is(cause, errMailProfileDNSIdentityNotReady) {
		return operationFailure(
			errCodeMailProfileDNSIdentityNotReady,
			mailProfileDNSIdentityMessage,
			cause,
		)
	}
	if errors.Is(cause, errMailProfileServerHostnameRequired) {
		return operationFailure(
			errCodeMailProfileServerHostnameRequired,
			mailProfileServerHostnameRequired,
			cause,
		)
	}
	if errors.Is(cause, errMailProfileServerHostnameInvalid) {
		return operationFailure(
			errCodeMailProfileServerHostnameInvalid,
			mailProfileServerHostnameMessage,
			cause,
		)
	}
	if failure := platformServiceOperationFailure(cause); failure != nil {
		return failure
	}
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

	// Give this server its fully qualified name before anything is installed.
	// Postfix announces it and the mail certificate is issued for it, so the
	// name has to be true on the host before the mail software reads it. When
	// the server already carries exactly this name, nothing is touched.
	// Herhangi bir şey kurulmadan önce bu sunucuya tam nitelikli adını ver.
	// Postfix onu duyurur ve posta sertifikası onun için verilir; bu yüzden
	// posta yazılımı adı okumadan önce ad sunucuda doğru olmalıdır. Sunucu
	// zaten tam olarak bu adı taşıyorsa hiçbir şeye dokunulmaz.
	desiredHostname, err := p.resolveMailProfileHostname(ctx)
	if err != nil {
		return result, mailProfileInstallFailure(err)
	}
	currentHostname, err := readMailProfileHostname()
	if err != nil {
		return result, mailProfileInstallFailure(
			fmt.Errorf("read mail profile server hostname: %w", err))
	}
	canonicalCurrent, canonicalErr := hostname.CanonicalFQDN(currentHostname)
	if canonicalErr != nil || canonicalCurrent != desiredHostname {
		if err := advance(mailProfilePhase(profile.ID, "hostname")); err != nil {
			return result, operationAdvanceFailure(err)
		}
		hostnameRequest := transport.SetServerHostnameRequest{
			ServiceMutationBinding: binding,
			Hostname:               desiredHostname,
		}
		var hostnameResponse transport.SetServerHostnameResponse
		if err := p.callAgentContext(
			ctx, "Agent.SetServerHostname", &hostnameRequest, &hostnameResponse,
		); err != nil {
			return result, mailProfileInstallFailure(
				fmt.Errorf("set the server hostname: %w", err))
		}
		if hostnameResponse.Error != "" {
			return result, mailProfileInstallFailure(
				fmt.Errorf("set the server hostname: %s", hostnameResponse.Error))
		}
		if hostnameResponse.Hostname != desiredHostname {
			return result, mailProfileInstallFailure(
				errors.New("agent did not confirm the exact server hostname"))
		}
		result["server_hostname"] = desiredHostname
	}

	completed := make([]string, 0, len(profile.Services))
	for _, serviceID := range profile.Services {
		if err := advance(mailProfilePhase(profile.ID, serviceID, "installing")); err != nil {
			return result, operationAdvanceFailure(err)
		}
		serviceAdvance := func(phase string) error {
			return advance(mailProfilePhase(profile.ID, serviceID, phase))
		}
		_, failure := p.runServiceInstall(ctx, serviceInstallRequest{
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
	if err := p.callAgentContext(ctx, "Agent.ConfigureMailStack", &mutationRequest, &mailStack); err != nil {
		return result, mailProfileInstallFailure(fmt.Errorf("final mail stack configuration: %w", err))
	}
	if mailStack.Error != "" {
		return result, mailProfileInstallFailure(fmt.Errorf("final mail stack configuration: %s", mailStack.Error))
	}
	if !mailStack.Configured {
		return result, mailProfileInstallFailure(errors.New("agent did not confirm final mail stack configuration"))
	}

	if err := advance(mailProfilePhase(profile.ID, "submission")); err != nil {
		return result, operationAdvanceFailure(err)
	}
	var submission transport.ConfigureMailSubmissionResponse
	if err := p.callAgentContext(ctx, "Agent.ConfigureMailSubmission", &mutationRequest, &submission); err != nil {
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
	if err := p.requireMailProfileDNSIdentity(ctx); err != nil {
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
		if service.Installed() {
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
	if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncMailTLSV2"); err != nil {
		return err
	}
	if err := p.requireMailTLSSyncV2Agent(ctx); err != nil {
		return err
	}
	if _, err := p.resolveMailProfileHostname(ctx); err != nil {
		return err
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

// resolveMailProfileHostname is the single answer to "what will the mail
// server be called". Everything downstream — the preflight, the catalogue and
// the install itself — asks this one function, so the screen can never promise
// a name the install would not use.
// resolveMailProfileHostname, "posta sunucusunun adı ne olacak" sorusunun tek
// yanıtıdır. Aşağıdaki her şey — ön kontrol, katalog ve kurulumun kendisi — bu
// tek işleve sorar; böylece ekran, kurulumun kullanmayacağı bir adı asla vaat
// edemez.
func (p *Panel) resolveMailProfileHostname(ctx context.Context) (string, error) {
	if _, err := readMailProfileHostname(); err != nil {
		return "", fmt.Errorf("read mail profile server hostname: %w", err)
	}
	identity := p.mailHostnameIdentity(ctx)
	if identity.Hostname == "" {
		return "", errMailProfileServerHostnameRequired
	}
	return identity.Hostname, nil
}

// mailProfileHostBlockedReason exposes the safe, actionable hostname gate in
// the read-only catalogue. A server without a fully qualified name is no
// longer blocked: the install screen asks for one and the install sets it. The
// catalogue only fails closed when this server's own hostname cannot be read
// at all, because then nothing about the name can be decided.
// mailProfileHostBlockedReason, salt-okunur katalogdaki güvenli ve eyleme
// dönük ana bilgisayar adı kapısını gösterir. Tam nitelikli adı olmayan bir
// sunucu artık engellenmez: kurulum ekranı bir ad ister, kurulum onu koyar.
// Katalog yalnız bu sunucunun kendi adı hiç okunamadığında kapanır; çünkü o
// zaman ad hakkında hiçbir şeye karar verilemez.
func mailProfileHostBlockedReason() string {
	if _, err := readMailProfileHostname(); err != nil {
		return mailProfileServerHostnameUnknown
	}
	return ""
}

func verifyMailProfileReady(profile mailProfileDefinition, services []ManagedServiceResponse) error {
	for _, serviceID := range profile.Services {
		managed := core.GetManagedServiceByID(serviceID)
		if managed == nil {
			return fmt.Errorf("final scan contains unknown profile service %s", serviceID)
		}
		ready := false
		for _, service := range services {
			if service.ID != serviceID || !service.Installed() {
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
	hostBlockedReason string,
	webmailReady bool,
	webmailProven bool,
	profileAttemptProofs map[string]mailProfileAttemptProof,
) []MailProfileResponse {
	byID := make(map[string]ManagedServiceResponse, len(services))
	installed := make(map[string]bool, len(services))
	for _, service := range services {
		byID[service.ID] = service
		if service.Installed() {
			installed[service.ID] = true
		}
	}

	profiles := make([]MailProfileResponse, 0, len(mailProfileDefinitions))
	for _, definition := range allMailProfiles() {
		proof, attempted := profileAttemptProofs[definition.ID]
		if !attempted {
			proof.Status = mailProfileAttemptNone
		}
		view := MailProfileResponse{
			ID:                  definition.ID,
			Name:                definition.Name,
			Description:         definition.Description,
			Services:            append([]string(nil), definition.Services...),
			LatestAttemptStatus: proof.Status,
			LatestAttemptError:  proof.Error,
			Status:              mailProfileStatusUnknown,
			Available:           false,
		}
		if !verified {
			view.BlockedReason = "Service state is unverified; run a fresh scan."
			profiles = append(profiles, view)
			continue
		}
		if hostBlockedReason != "" {
			view.Status = mailProfileStatusBlocked
			view.BlockedReason = hostBlockedReason
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
			if service.Installed() {
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
			view.Verified = proof.Verified
			if !view.Verified {
				// A service scan can prove component membership and runtime state, but
				// only the bound profile operation proves its final MailStack, TLS and
				// submission reconciliations. Keep that distinction visible instead of
				// over-claiming a complete mail setup from component state alone.
				view.Warning = mailProfileReconciliationWarning
			}
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
