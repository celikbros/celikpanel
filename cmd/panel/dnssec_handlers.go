package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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

type dnssecAgentResponse struct {
	Secured bool     `json:"secured"`
	DS      []string `json:"ds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

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
		if err := p.agentClient.Call("Agent.DNSSECStatus", &struct {
			Zone string `json:"zone"`
		}{Zone: domain}, &resp); err != nil {
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
		if err := p.agentClient.Call("Agent.SecureDNSZone", &struct {
			Zone string `json:"zone"`
		}{Zone: domain}, &resp); err != nil {
			writeAgentError(w, err, "DNSSEC")
			return
		}
		if problem := dnssecResultError(resp, true); problem != "" {
			writeClientError(w, http.StatusConflict, problem)
			return
		}
		// Bump the ledger SOA serial, republish the now-signed primary, rectify
		// it, and only then send NOTIFY. This forces a full signed transfer path
		// before the registrar-facing DS is handed back as a successful result.
		if err := p.syncZoneToDNS(r.Context(), domain, false); err != nil {
			log.Printf("dnssec publish %s: %v", domain, err)
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
