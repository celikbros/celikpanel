package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

const hostMutationReadinessPath = "/api/v1/host-mutation-readiness"

func hostMutationBusy(reason string) transport.HostMutationReadinessResponse {
	return transport.HostMutationReadinessResponse{
		Code: transport.HostMutationBusy, Reason: reason,
	}
}

func hostMutationUnavailable() transport.HostMutationReadinessResponse {
	return transport.HostMutationReadinessResponse{
		Code:   transport.HostMutationUnavailable,
		Reason: transport.HostMutationReasonStateUnverified,
	}
}

func verifiedAgentMutationReadiness(
	response transport.HostMutationReadinessResponse,
) transport.HostMutationReadinessResponse {
	if response.Ready {
		if response.Code == "" && response.Reason == "" {
			return response
		}
		return hostMutationUnavailable()
	}
	switch response.Code {
	case transport.HostMutationBusy:
		switch response.Reason {
		case transport.HostMutationReasonAgentMutation,
			transport.HostMutationReasonHostLock,
			transport.HostMutationReasonPackageManager:
			return response
		}
	case transport.HostMutationUnavailable:
		if response.Reason == transport.HostMutationReasonStateUnverified {
			return response
		}
	}
	return hostMutationUnavailable()
}

// readHostMutationReadiness is an advisory snapshot, not a lease. The local
// lock is released before the RPC so a slow read cannot block a real mutation;
// every mutating endpoint must still perform authoritative admission.
func (p *Panel) readHostMutationReadiness(ctx context.Context) transport.HostMutationReadinessResponse {
	if !p.serviceMutationMu.TryLock() {
		return hostMutationBusy(transport.HostMutationReasonPanelOperation)
	}
	operation, err := p.activeServiceOperation(ctx)
	p.serviceMutationMu.Unlock()
	if err != nil {
		log.Printf("[host-mutation-readiness] inspect panel operation: %v", err)
		return hostMutationUnavailable()
	}
	if operation != nil {
		return hostMutationBusy(transport.HostMutationReasonPanelOperation)
	}

	var response transport.HostMutationReadinessResponse
	if err := p.callAgentContext(
		ctx,
		"Agent.ServiceMutationReadiness",
		&transport.Empty{},
		&response,
	); err != nil {
		log.Printf("[host-mutation-readiness] inspect agent state: %v", err)
		return hostMutationUnavailable()
	}
	return verifiedAgentMutationReadiness(response)
}

func (p *Panel) handleHostMutationReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if caller := currentCaller(r); caller == nil || caller.Role != roleAdmin {
		writeCodedError(w, http.StatusForbidden, errCodeAdminOnly, "admin only", "")
		return
	}
	if r.Method != http.MethodGet {
		rejectRouteMethod(w, []string{http.MethodGet})
		return
	}
	_ = json.NewEncoder(w).Encode(p.readHostMutationReadiness(r.Context()))
}
