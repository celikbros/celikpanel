package main

import (
	"testing"
	"time"
)

func TestServerSetupNginxBootstrapPrecedesPanelCertificateAndPublisher(t *testing.T) {
	for _, role := range []string{"primary", "secondary"} {
		draft := secondaryHostingDraft()
		draft.DNSRole = role
		steps := serverSetupDNSBootstrapSteps(draft, []serverSetupPlanStep{
			{Kind: "dns", Target: "local"}, {Kind: "service", Target: "nginx"},
			{Kind: "service", Target: "php-fpm"}, {Kind: "mail_profile", Target: "webmail"},
			{Kind: "service", Target: "certbot"}, {Kind: "firewall", Target: "enable_and_persist"},
			{Kind: "panel_certificate", Target: draft.PanelDomain}, {Kind: "verify", Target: draft.Purpose},
		})
		nginx, cert, gate, mail := -1, -1, -1, -1
		for index, step := range steps {
			if step.Kind == "service" && step.Target == "nginx" {
				nginx = index
			}
			if step.Kind == "panel_certificate" {
				cert = index
			}
			if step.Kind == "dns_publisher" || step.Kind == "dns_readiness" {
				gate = index
			}
			if step.Kind == "mail_profile" {
				mail = index
			}
		}
		if nginx < 0 || cert <= nginx || gate <= cert || mail <= gate {
			t.Fatalf("%s unsafe bootstrap order nginx=%d cert=%d gate=%d mail=%d", role, nginx, cert, gate, mail)
		}
	}
}

func TestServerSetupValidCertificateDoesNotHideNginxRenewalMigration(t *testing.T) {
	cert := panelCertInfo{HTTPSEnabled: true, DNSNames: []string{"panel.example.test"}, ExpiresAt: time.Now().Add(60 * 24 * time.Hour)}
	if serverSetupPanelCertificateChangesRequired(cert, "panel.example.test", nil) {
		t.Fatal("unchanged healthy certificate forced a reissue")
	}
	if !serverSetupPanelCertificateChangesRequired(cert, "panel.example.test", []serverSetupPlanStep{{Kind: "service", Target: "nginx"}}) {
		t.Fatal("valid standalone certificate hid the future port 80 conflict")
	}
	if serverSetupPanelCertificateChangesRequired(cert, "panel.example.test", []serverSetupPlanStep{{Kind: "service", Target: "mariadb"}}) {
		t.Fatal("unrelated service forced a certificate reissue")
	}
}
