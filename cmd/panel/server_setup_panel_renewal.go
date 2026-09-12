package main

import (
	"slices"
	"time"
)

func serverSetupPanelCertificateChangesRequired(cert panelCertInfo, domain string, steps []serverSetupPlanStep) bool {
	if !cert.HTTPSEnabled || cert.SelfSigned || !slices.Contains(cert.DNSNames, domain) || !cert.ExpiresAt.After(time.Now().Add(24*time.Hour)) {
		return true
	}
	// Installing nginx changes ownership of port 80. A certificate issued while
	// nginx was absent may still be valid but renew through standalone mode.
	// Review a managed reissue after nginx to establish its durable webroot route.
	return slices.ContainsFunc(steps, func(step serverSetupPlanStep) bool { return step.Kind == "service" && step.Target == "nginx" })
}
