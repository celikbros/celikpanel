package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

// dnsSetupRequest is the complete operator-owned DNS identity. Keeping the
// nameserver pair and the cluster tuple in one request prevents a paired
// nameserver rename from being validated against stale stored peer settings.
type dnsSetupRequest struct {
	NS1    string `json:"ns1"`
	NS2    string `json:"ns2"`
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

// handleDNSSetup applies the complete DNS identity idempotently. The agent is
// changed first, then the pair, role, peer tuple and every ledger-zone rewrite
// commit together. If the ledger transaction fails, the previous agent role is
// restored. Zone publication happens after commit and is intentionally
// retryable by sending this same request again.
func (p *Panel) handleDNSSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	if r.Method != http.MethodPut {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p.dnsTopologyMu.Lock()
	defer p.dnsTopologyMu.Unlock()

	var req dnsSetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.NS1 = canonicalDNSName(req.NS1)
	req.NS2 = canonicalDNSName(req.NS2)
	for _, ns := range []string{req.NS1, req.NS2} {
		if !validDNSHostname(ns) {
			writeClientError(w, http.StatusBadRequest, "a nameserver must be a full host name, for example ns1.example.com")
			return
		}
	}
	if req.NS1 == req.NS2 {
		writeClientError(w, http.StatusBadRequest, "the two nameservers must have different names")
		return
	}

	req.Role = normalizeDNSRole(strings.TrimSpace(req.Role))
	req.PeerIP = strings.TrimSpace(req.PeerIP)
	req.PeerNS = canonicalDNSName(req.PeerNS)
	localIPv4, ok := canonicalIPv4(serverPrimaryIP())
	if !ok {
		writeClientError(w, http.StatusConflict, "this server has no usable IPv4 address; set CELIKPANEL_SERVER_IP and retry")
		return
	}

	switch req.Role {
	case "standalone":
		req.PeerIP, req.PeerNS = "", ""
	case "paired":
		peerIP := net.ParseIP(req.PeerIP)
		peerIPv4 := peerIP.To4()
		if peerIPv4 == nil || !peerIPv4.IsGlobalUnicast() {
			writeClientError(w, http.StatusBadRequest, "enter the other server's IPv4 address")
			return
		}
		req.PeerIP = peerIPv4.String()
		if !validDNSHostname(req.PeerNS) {
			writeClientError(w, http.StatusBadRequest, "enter the other server's nameserver name, for example ns2.example.com")
			return
		}
		if req.PeerIP == localIPv4 {
			writeCodedError(w, http.StatusBadRequest, errCodeDNSClusterPeerIsLocal, "the other server cannot be this server", "")
			return
		}
		if req.PeerNS != req.NS1 && req.PeerNS != req.NS2 {
			writeClientError(w, http.StatusBadRequest, "the other server's name must be one of the two nameservers in this setup")
			return
		}
	default:
		writeClientError(w, http.StatusBadRequest, "role must be standalone or paired")
		return
	}

	previous, err := p.dnsClusterAgentSnapshot(r.Context())
	if err != nil {
		writeServerError(w, fmt.Errorf("read previous DNS cluster settings: %w", err))
		return
	}

	state := dnsClusterAgentState{Role: req.Role, PeerIP: req.PeerIP, PeerNS: req.PeerNS}
	resp, err := p.applyDNSClusterAgent(state)
	if err != nil {
		writeAgentError(w, err, "DNS setup")
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	if !resp.Applied {
		writeClientError(w, http.StatusConflict, "the DNS server did not confirm the setup change")
		return
	}

	if err := p.saveDNSClusterSettingsAndReconcile(
		r.Context(), req.Role, req.PeerIP, req.PeerNS, req.NS1, req.NS2, localIPv4,
	); err != nil {
		rollbackResp, rollbackCallErr := p.applyDNSClusterAgent(previous)
		var rollbackErr error
		switch {
		case rollbackCallErr != nil:
			rollbackErr = rollbackCallErr
		case rollbackResp.Error != "":
			rollbackErr = fmt.Errorf("%s", rollbackResp.Error)
		case !rollbackResp.Applied:
			rollbackErr = fmt.Errorf("agent did not confirm the restored DNS role")
		}
		if rollbackErr != nil {
			log.Printf("[500][dns-setup] save ledger after agent apply: %v; restore previous role %q failed: %v",
				err, previous.Role, rollbackErr)
			writeCodedError(w, http.StatusInternalServerError, errCodeInternal,
				"DNS setup could not be saved, and the previous DNS server role could not be restored; check the DNS service before retrying", "")
			return
		}
		log.Printf("[500][dns-setup] save ledger after agent apply: %v; previous role %q restored", err, previous.Role)
		writeCodedError(w, http.StatusInternalServerError, errCodeInternal,
			"DNS setup could not be saved; the previous DNS server role was restored", "")
		return
	}

	p.audit(r, "settings.dns_setup_saved:"+req.Role+" peer="+req.PeerIP, "settings", 0)
	if _, err := p.syncAllZonesStrict(r.Context()); err != nil {
		err = fmt.Errorf("publish DNS setup: %w", err)
		if writeDNSPublicationConflict(w, err,
			"DNS setup was saved, but one or more zones could not be published; retry the same setup") {
			return
		}
		writeServerError(w, err)
		return
	}

	p.audit(r, "settings.dns_setup_published:"+req.Role+" peer="+req.PeerIP, "settings", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "detail": resp.Detail})
}
