package main

import (
	"context"
	"log"
)

// Execution context is presentation only. It is derived from the accepted plan
// without rewriting the durable execution or probing the host during polling.
// Sunum bilgisi kabul edilen plandan gelir; sorgu kayit yazmaz ve sunucuyu taramaz.
type serverSetupExecutionContext struct {
	DNSMode              string `json:"dns_mode"`
	DNSRole              string `json:"dns_role"`
	DNSEngine            string `json:"dns_engine"`
	LocalNameserver      string `json:"local_nameserver"`
	LocalIP              string `json:"local_ip"`
	PeerNameserver       string `json:"peer_nameserver"`
	PeerIP               string `json:"peer_ip"`
	PanelDomain          string `json:"panel_domain"`
	MailHostname         string `json:"mail_hostname"`
	DNSHostingManagement string `json:"dns_hosting_management"`
}

type serverSetupOperationResponse struct {
	*serverSetupExecution
	Context *serverSetupExecutionContext `json:"context,omitempty"`
}

func (p *Panel) serverSetupOperationResponse(ctx context.Context, execution *serverSetupExecution) *serverSetupOperationResponse {
	if execution == nil {
		return nil
	}
	response := &serverSetupOperationResponse{serverSetupExecution: execution}
	plan, err := p.loadServerSetupPlan(ctx, execution.PlanID)
	if err == nil {
		err = validateServerSetupExecution(plan, *execution)
	}
	if err != nil {
		// The existing operation remains available when optional context cannot
		// be proven. Never substitute an editable or unrelated plan.
		// Istege bagli bilgi kanitlanamazsa islem korunur; duzenlenebilir
		// veya ilgisiz plan yerine kullanilmaz.
		log.Printf("server setup progress context unavailable: %v", err)
		return response
	}
	draft := plan.Draft
	response.Context = &serverSetupExecutionContext{
		DNSMode:      draft.DNSMode,
		PanelDomain:  draft.PanelDomain,
		MailHostname: draft.MailHostname,
	}
	if draft.DNSMode == setupDNSModeLocal {
		response.Context.DNSRole = draft.DNSRole
		response.Context.DNSEngine = draft.DNSEngine
		response.Context.LocalNameserver = draft.NS1
		if draft.DNSRole == "secondary" {
			response.Context.LocalNameserver = draft.NS2
		}
		response.Context.LocalIP = draft.LocalIP
		response.Context.PeerNameserver = draft.PeerNS
		response.Context.PeerIP = draft.PeerIP
		if serverSetupManualSecondaryHosting(draft) {
			response.Context.DNSHostingManagement = "manual"
		} else if serverSetupSecondaryHosting(draft) {
			// Empty in legacy plans means the original panel connector flow.
			// Eski plandaki bos alan ilk panel baglanti akisini belirtir.
			response.Context.DNSHostingManagement = "panel"
		}
	}
	// External/existing DNS drafts do not prove a local address or DNS role.
	// Harici/mevcut DNS taslagi yerel adres veya DNS rolunu kanitlamaz.
	return response
}
