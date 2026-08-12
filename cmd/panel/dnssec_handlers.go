package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

// DNSSEC + DANE, panel side. Signing happens in pdns through the agent; the
// panel's job is the DS handoff (shown to the operator for the registrar)
// and keeping TLSA records in the zone ledger in step with the mail
// certificate — publish on secure-mail, refresh on renewal, remove on
// disable. Without a registrar DS the TLSA records are treated as insecure
// by validators (exactly what Plesk warns about), so the UI shows both
// together.
//
// DNSSEC + DANE, panel tarafı. İmzalama agent üzerinden pdns'te olur;
// panelin işi DS teslimi (operatöre registrar için gösterilir) ve TLSA
// kayıtlarını zone defterinde posta sertifikasıyla adımda tutmaktır —
// posta-koruma açılınca yayımla, yenilemede tazele, kapanınca kaldır.
// Registrar'da DS olmadan doğrulayıcılar TLSA kayıtlarını güvensiz sayar
// (tam Plesk'in uyardığı şey); bu yüzden arayüz ikisini birlikte gösterir.

// Release safety gate: the automatic publish/refresh/remove behavior
// described above is disabled until both TTL-aware durable rollover and
// explicit panel-record ownership exist.

const (
	automaticDANEMutationEnabled = false
	automaticDANEMutationReason  = "automatic DANE/TLSA updates are disabled: safe TTL-aware certificate rollover and explicit panel record ownership are not available in this release"
)

type daneAutomationState struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

func currentDANEAutomationState() daneAutomationState {
	return daneAutomationState{
		Enabled: automaticDANEMutationEnabled,
		Reason:  automaticDANEMutationReason,
	}
}

func daneMutationSafetyPrerequisitesAvailable() bool {
	// There is no durable rollover job and pdns_records has no ownership
	// column in this release. Keep this separate barrier even if a future
	// build changes the feature flag by mistake.
	return false
}

type dnssecAgentResponse = transport.DNSSECStatusResponse

// dnssecResultError keeps failures returned inside an otherwise successful
// RPC response visible to the operator. It also protects a newer panel from an
// older agent that used to report signing success even when pdnsutil produced
// no DS record to publish at the registrar.
func dnssecResultError(resp dnssecAgentResponse, signing bool) string {
	if resp.Error != "" {
		return resp.Error
	}
	if resp.Secured && len(resp.DS) == 0 {
		return "DNSSEC is not ready: the signed zone produced no DS records"
	}
	if signing && !resp.Secured {
		return "DNSSEC signing did not complete"
	}
	return ""
}

// handleDomainDNSSEC: GET returns signing status + DS records; POST signs
// the zone (admins and the domain's managers — same dispatcher authz as the
// other domain endpoints).
// handleDomainDNSSEC: GET imza durumu + DS kayıtlarını döndürür; POST zone'u
// imzalar.
func (p *Panel) handleDomainDNSSEC(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	var domain string
	if p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&domain) != nil {
		writeClientError(w, http.StatusNotFound, "domain not found")
		return
	}

	var resp dnssecAgentResponse
	switch r.Method {
	case http.MethodGet:
		if err := p.callAgent("Agent.DNSSECStatus", &transport.DNSSECRequest{Zone: domain}, &resp); err != nil {
			writeAgentError(w, err, "DNSSEC")
			return
		}
		if problem := dnssecResultError(resp, false); problem != "" {
			writeClientError(w, http.StatusConflict, problem)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"secured":         resp.Secured,
			"ds":              resp.DS,
			"dane_automation": currentDANEAutomationState(),
		})

	case http.MethodPost:
		// Lock order is the global host mutation gate followed by DNS
		// publication. The exact SOA/snapshot lease is committed before signing;
		// a crash or response loss therefore leaves startup enough authority to
		// republish/rectify the signed host state.
		p.serviceMutationMu.Lock()
		defer p.serviceMutationMu.Unlock()
		dnsPublicationMu.Lock()
		defer dnsPublicationMu.Unlock()
		if err := p.requireNoPendingDNSClusterSaga(r.Context()); err != nil {
			writeClientError(w, http.StatusConflict,
				"DNS topology recovery must finish before DNSSEC signing")
			return
		}
		if err := p.requireDNSSECSecureV2Agent(r.Context()); err != nil {
			writeServerError(w, fmt.Errorf("verify DNSSEC publisher: %w", err))
			return
		}
		plan, err := p.prepareDNSZoneSyncPlanReconciledLocked(
			r.Context(), domain, false,
		)
		if err != nil {
			var publicationErr *dnsAgentPublicationError
			if errors.As(err, &publicationErr) {
				writeClientError(w, http.StatusConflict,
					"a previous DNS publication is still being reconciled; retry shortly")
				return
			}
			writeServerError(w, fmt.Errorf("prepare DNSSEC publication: %w", err))
			return
		}
		secureReq := transport.SecureDNSZoneV2Request{Zone: plan.Commitment.Domain}
		secureRequestID, err := newServiceOperationID()
		if err != nil {
			writeServerError(w, fmt.Errorf("create DNSSEC request identity: %w", err))
			return
		}
		secureOwnerID, err := newServiceOperationID()
		if err != nil {
			writeServerError(w, fmt.Errorf("create DNSSEC owner identity: %w", err))
			return
		}
		secureOp := serviceOperation{
			RequestID: secureRequestID, Kind: "dnssec_secure",
			ServiceID: plan.Commitment.Domain,
		}
		secureIdentity := agentMutationIdentityForOperation(secureOp, secureOwnerID)
		secureErr := p.withStandaloneAgentMutationIdentity(
			r.Context(), secureOp, secureOwnerID,
			func(callCtx context.Context, binding agentMutationBinding) error {
				secureReq.ServiceMutationBinding = binding
				if err := p.callAgentContext(
					callCtx, "Agent.SecureDNSZoneV2", &secureReq, &resp,
				); err != nil {
					return err
				}
				if problem := dnssecResultError(resp, true); problem != "" {
					return errors.New(problem)
				}
				return nil
			},
		)
		terminalKnown, settleErr := p.dnssecSecureTerminalKnown(
			r.Context(), secureIdentity, secureErr,
		)
		if settleErr != nil {
			// An exact active or unqueryable signing job may already have changed
			// the PowerDNS database. The pre-sign DNS publication lease is the
			// crash bridge and must remain completely untouched for startup.
			log.Printf("dnssec secure %s remains pending: %v", domain, settleErr)
			writeClientError(w, http.StatusConflict,
				"DNSSEC signing is still being reconciled; retry after recovery completes")
			return
		}
		if !terminalKnown {
			// A successful exact status lookup returning no job proves Begin did
			// not create host authority. Release only this exact publication lease;
			// the desired generation remains pending for ordinary startup repair.
			finalizeCtx, cancel := dnsZoneFinalizeContext(r.Context())
			recordErr := p.recordDNSZoneSyncFailure(finalizeCtx, plan.State, secureErr)
			cancel()
			if recordErr != nil {
				writeServerError(w, fmt.Errorf("release unstarted DNSSEC publication: %w", recordErr))
				return
			}
			writeAgentError(w, secureErr, "DNSSEC")
			return
		}

		// Even a known terminal signing failure may have created keys before
		// stopping. Consume the pre-sign publication lease while both global
		// locks remain held so the served zone converges to the exact snapshot.
		_, publishErr := p.publishPreparedDNSZoneSyncPlanLocked(r.Context(), plan)
		if secureErr != nil {
			if publishErr != nil {
				log.Printf("dnssec secure %s: %v; repair publication: %v", domain, secureErr, publishErr)
			}
			writeAgentError(w, secureErr, "DNSSEC")
			return
		}
		if problem := dnssecResultError(resp, true); problem != "" {
			// A terminal success can outlive the original SecureDNSZoneV2 RPC
			// response. Recover the registrar-facing DS from a fresh read-only
			// status query before deciding that the result is incomplete.
			var recovered dnssecAgentResponse
			if err := p.callAgent(
				"Agent.DNSSECStatus",
				&transport.DNSSECRequest{Zone: plan.Commitment.Domain},
				&recovered,
			); err == nil && dnssecResultError(recovered, true) == "" {
				resp = recovered
				problem = ""
			}
			if problem != "" {
				writeClientError(w, http.StatusConflict, problem)
				return
			}
		}
		// Bump the ledger SOA serial, republish the now-signed primary, rectify
		// it, and only then send NOTIFY. This forces a full signed transfer path
		// before the registrar-facing DS is handed back as a successful result.
		if publishErr != nil {
			log.Printf("dnssec publish %s: %v", domain, publishErr)
			writeClientError(w, http.StatusConflict,
				"the zone was signed locally but its updated SOA could not be published; check the PowerDNS pair and retry")
			return
		}
		p.audit(r, "dnssec.sign", "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{
			"secured":         resp.Secured,
			"ds":              resp.DS,
			"dane_automation": currentDANEAutomationState(),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// dnssecSecureTerminalKnown distinguishes an exact terminal durable result
// from a proven pre-Begin failure and from terminal uncertainty. Only the
// first case permits the caller to consume its pre-sign DNS publication lease.
func (p *Panel) dnssecSecureTerminalKnown(
	ctx context.Context,
	identity agentMutationIdentity,
	callErr error,
) (bool, error) {
	if callErr == nil {
		return true, nil
	}
	statusCtx, cancel := dnsZoneFinalizeContext(ctx)
	job, statusErr := p.statusAgentMutation(statusCtx, identity.RequestID)
	cancel()
	if statusErr != nil {
		return false, errors.Join(callErr,
			fmt.Errorf("read exact DNSSEC terminal status: %w", statusErr))
	}
	if job == nil {
		return false, nil
	}
	if err := validateAgentMutationIdentity(job, identity); err != nil {
		return false, err
	}
	if agentMutationActive(job.Status) {
		return false, errors.Join(callErr,
			errors.New("exact DNSSEC signing job remains active"))
	}
	if job.Status != agentMutationSucceeded && job.Status != agentMutationFailed {
		return false, fmt.Errorf("exact DNSSEC signing job has invalid terminal status %q", job.Status)
	}
	return true, nil
}

// refreshTLSARecords intentionally performs no mutation in this release.
// A safe DANE certificate rollover needs durable state and TTL waits:
// publish old+new, wait, switch the mail certificate, wait again, then remove
// old. The current schema also cannot distinguish panel-owned TLSA records
// from user-created records. Until both capabilities exist, certificate and
// mail operations leave every TLSA record untouched.
func (p *Panel) refreshTLSARecords(ctx context.Context, domainID int) error {
	_ = p
	_ = ctx
	_ = domainID

	if !automaticDANEMutationEnabled {
		// Keep core certificate and mail TLS operations usable while the
		// DNSSEC endpoint reports the disabled DANE automation state.
		return nil
	}
	if !daneMutationSafetyPrerequisitesAvailable() {
		return errors.New(automaticDANEMutationReason)
	}
	return errors.New("automatic DANE/TLSA mutation has no implementation in this release")
}
