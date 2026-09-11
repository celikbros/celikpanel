//go:build !linux

package main

import "github.com/alicelik/celikpanel/internal/transport"

func (a *Agent) PanelRenewalReadiness(req *transport.PanelRenewalReadinessRequest, resp *transport.PanelRenewalReadinessResponse) error {
	resp.Ready = false
	resp.Code = "panel_renewal_unavailable"
	return nil
}
