package main

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/transport"
)

// ServiceMutationReadiness is deliberately advisory. It shares the exact
// read-only proof used by release tooling, but releases every probe before it
// replies. BeginServiceMutation must therefore repeat authoritative admission
// under the durable host lease.
func (a *Agent) ServiceMutationReadiness(
	_ *transport.Empty,
	response *transport.HostMutationReadinessResponse,
) error {
	err := checkServiceMutationIdle("", "")
	if err == nil {
		*response = transport.HostMutationReadinessResponse{Ready: true}
		return nil
	}
	if reason, ok := serviceMutationReadinessReason(err); ok {
		*response = transport.HostMutationReadinessResponse{
			Code: transport.HostMutationBusy, Reason: reason,
		}
		return nil
	}
	if errors.Is(err, errServiceMutationNotIdle) {
		*response = transport.HostMutationReadinessResponse{
			Code:   transport.HostMutationUnavailable,
			Reason: transport.HostMutationReasonStateUnverified,
		}
		return nil
	}
	return err
}
