package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

var errDNSEngineWorkflowRequired = errors.New("DNS engine changes require the reviewed DNS infrastructure workflow")

func managedDNSEngineService(service *core.ManagedService) bool {
	return service != nil && service.ConflictGroup == "dns-server"
}

func managedDNSEngineServiceID(serviceID string) bool {
	return managedDNSEngineService(core.GetManagedServiceByID(serviceID))
}

func writeDNSEngineWorkflowRequired(w http.ResponseWriter) {
	writeCodedError(
		w,
		http.StatusConflict,
		errCodeDNSEngineWorkflowRequired,
		"BIND and PowerDNS must be installed, activated, switched, or removed from DNS infrastructure settings",
		"/settings?section=dns",
	)
}

func (p *Panel) requireActiveDNSPublisherForMutation(
	w http.ResponseWriter,
	ctx context.Context,
) (dnsPublisherIdentity, bool) {
	identity, ready, err := p.activeDNSPublisher(ctx)
	if err != nil {
		writeServerError(w, err)
		return dnsPublisherIdentity{}, false
	}
	if !ready {
		writeCodedError(
			w,
			http.StatusConflict,
			errCodeDNSServerRequired,
			"no single managed authoritative DNS engine is active and verified",
			"/settings?section=dns",
		)
		return dnsPublisherIdentity{}, false
	}
	return identity, true
}
