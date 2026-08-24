package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func mutationPolicyJob(kind, target, packageName string) *ServiceMutationJob {
	return &ServiceMutationJob{
		Kind:        kind,
		Target:      target,
		PackageName: packageName,
	}
}

func mutationPolicyClaim(
	method serviceMutationStepMethod,
	target, packageName, action string,
) serviceMutationStepClaim {
	return newServiceMutationStepClaim(method, target, packageName, action)
}

func TestServiceMutationStepPolicyAllowsEveryDeclaredWorkflowRow(t *testing.T) {
	if serviceMutationStepSyncVPNPeers != "Agent.SyncVPNPeersV2" {
		t.Fatalf("VPN peer mutation method=%q", serviceMutationStepSyncVPNPeers)
	}
	if serviceMutationStepApplyFirewall != "Agent.ApplyFirewallV2" {
		t.Fatalf("firewall mutation method=%q", serviceMutationStepApplyFirewall)
	}
	if serviceMutationStepSyncDNSZone != "Agent.SyncDNSZoneV2" {
		t.Fatalf("DNS zone mutation method=%q", serviceMutationStepSyncDNSZone)
	}
	if serviceMutationStepSyncDNSZoneV3 != "Agent.SyncDNSZoneV3" {
		t.Fatalf("DNS zone V3 mutation method=%q", serviceMutationStepSyncDNSZoneV3)
	}
	if serviceMutationStepRecoverDNSZoneV3 != "Agent.RecoverDNSZoneV3" {
		t.Fatalf("DNS zone V3 recovery method=%q", serviceMutationStepRecoverDNSZoneV3)
	}
	if serviceMutationStepSwitchDNSEngine != "Agent.SwitchDNSEngineV1" {
		t.Fatalf("DNS engine switch mutation method=%q", serviceMutationStepSwitchDNSEngine)
	}
	if serviceMutationStepIssuePanelCertificate != "Agent.IssuePanelCertificateV2" {
		t.Fatalf("panel certificate mutation method=%q", serviceMutationStepIssuePanelCertificate)
	}
	vpnQualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("0", 64)
	firewallQualifier := "firewall-apply/v1:sha256:" + strings.Repeat("0", 64)
	certificateQualifier := "panel-certificate-issue/v1:sha256:" + strings.Repeat("0", 64)
	mailTLSQualifier := "mail-tls-sync/v1:sha256:" + strings.Repeat("0", 64)
	dnsQualifier := "dns-zone-sync/v1:sha256:" + strings.Repeat("0", 64)
	dnsV3Qualifier := "dns-zone-sync/v3:sha256:" + strings.Repeat("0", 64)
	dnsSwitchQualifier := "dns-engine-switch/v1:sha256:" + strings.Repeat("0", 64)
	tests := []struct {
		name  string
		job   *ServiceMutationJob
		claim serviceMutationStepClaim
	}{
		{"install service", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"install versioned managed service", mutationPolicyJob("service_install", "php-fpm", "php8.3-fpm"), mutationPolicyClaim(serviceMutationStepInstallService, "php-fpm", "php8.3-fpm", "install")},
		{"uninstall service", mutationPolicyJob("service_uninstall", "nginx", ""), mutationPolicyClaim(serviceMutationStepUninstallService, "nginx", "", "uninstall")},
		{"install roundcube standalone", mutationPolicyJob("service_install", "roundcube", ""), mutationPolicyClaim(serviceMutationStepInstallRoundcube, "roundcube", "", "install")},
		{"install roundcube profile", mutationPolicyJob("mail_profile_install", core.MailProfileWebmail, ""), mutationPolicyClaim(serviceMutationStepInstallRoundcube, "roundcube", "", "install")},
		{"remove roundcube", mutationPolicyJob("service_uninstall", "roundcube", ""), mutationPolicyClaim(serviceMutationStepRemoveRoundcube, "roundcube", "", "remove")},
		{"configure webmail after install", mutationPolicyJob("service_install", "roundcube", ""), mutationPolicyClaim(serviceMutationStepConfigureWebmail, "roundcube", "", "configure")},
		{"configure webmail before uninstall", mutationPolicyJob("service_uninstall", "roundcube", ""), mutationPolicyClaim(serviceMutationStepConfigureWebmail, "roundcube", "", "configure")},
		{"configure webmail profile", mutationPolicyJob("mail_profile_install", core.MailProfileWebmail, ""), mutationPolicyClaim(serviceMutationStepConfigureWebmail, "roundcube", "", "configure")},
		{"install node", mutationPolicyJob("runtime_install", "node", "22.14.0"), mutationPolicyClaim(serviceMutationStepInstallNodeVersion, "node", "22.14.0", "install")},
		{"remove node", mutationPolicyJob("runtime_remove", "node:22.14.0", ""), mutationPolicyClaim(serviceMutationStepRemoveNodeVersion, "node", "22.14.0", "remove")},
		{"enable repository", mutationPolicyJob("repo_enable", "docker", ""), mutationPolicyClaim(serviceMutationStepEnableRepo, "docker", "", "enable")},
		{"disable repository", mutationPolicyJob("repo_disable", "docker", ""), mutationPolicyClaim(serviceMutationStepDisableRepo, "docker", "", "disable")},
		{"service start", mutationPolicyJob("service_start", "nginx", ""), mutationPolicyClaim(serviceMutationStepServiceAction, "nginx", "", "start")},
		{"service stop", mutationPolicyJob("service_stop", "nginx", ""), mutationPolicyClaim(serviceMutationStepServiceAction, "nginx", "", "stop")},
		{"service restart", mutationPolicyJob("service_restart", "nginx", ""), mutationPolicyClaim(serviceMutationStepServiceAction, "nginx", "", "restart")},
		{"service reload", mutationPolicyJob("service_reload", "nginx", ""), mutationPolicyClaim(serviceMutationStepServiceAction, "nginx", "", "reload")},
		{"start postfix after install", mutationPolicyJob("service_install", "postfix", ""), mutationPolicyClaim(serviceMutationStepStartService, "postfix", "", "start")},
		{"start dovecot in profile", mutationPolicyJob("mail_profile_install", core.MailProfileCore, ""), mutationPolicyClaim(serviceMutationStepStartService, "dovecot", "", "start")},
		{"reset dovecot after install", mutationPolicyJob("service_install", "dovecot", ""), mutationPolicyClaim(serviceMutationStepResetFailedUnit, "dovecot", "", "reset-failed")},
		{"reset postfix in profile", mutationPolicyJob("mail_profile_install", core.MailProfileCore, ""), mutationPolicyClaim(serviceMutationStepResetFailedUnit, "postfix", "", "reset-failed")},
		{"configure pdns standalone", mutationPolicyJob("pdns_configure", "pdns", ""), mutationPolicyClaim(serviceMutationStepConfigurePowerDNSSQLite, "pdns", "", "configure")},
		{"sync dns zone", mutationPolicyJob("dns_zone_sync", "example.com", dnsQualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", dnsQualifier, "sync")},
		{"delete dns zone", mutationPolicyJob("dns_zone_sync", "example.com", dnsQualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", dnsQualifier, "delete")},
		{"sync BIND zone V3", mutationPolicyJob("dns_zone_sync", "example.com", dnsV3Qualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZoneV3, "example.com", dnsV3Qualifier, "sync")},
		{"delete BIND zone V3", mutationPolicyJob("dns_zone_sync", "example.com", dnsV3Qualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZoneV3, "example.com", dnsV3Qualifier, "delete")},
		{"recover DNS zone V3", mutationPolicyJob("dns_zone_sync", "example.com", dnsV3Qualifier), mutationPolicyClaim(serviceMutationStepRecoverDNSZoneV3, "example.com", dnsV3Qualifier, "recover")},
		{"switch DNS engine to BIND", mutationPolicyJob("dns_engine_switch", "bind", dnsSwitchQualifier), mutationPolicyClaim(serviceMutationStepSwitchDNSEngine, "bind", dnsSwitchQualifier, "switch")},
		{"switch DNS engine to PowerDNS", mutationPolicyJob("dns_engine_switch", "pdns", dnsSwitchQualifier), mutationPolicyClaim(serviceMutationStepSwitchDNSEngine, "pdns", dnsSwitchQualifier, "switch")},
		{"adopt managed PowerDNS", mutationPolicyJob("dns_engine_switch", "pdns", dnsSwitchQualifier), mutationPolicyClaim(serviceMutationStepSwitchDNSEngine, "pdns", dnsSwitchQualifier, "adopt")},
		{"nginx ready after install", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim(serviceMutationStepEnsureNginxReady, "nginx", "", "ready")},
		{"nginx ready in webmail profile", mutationPolicyJob("mail_profile_install", core.MailProfileWebmail, ""), mutationPolicyClaim(serviceMutationStepEnsureNginxReady, "nginx", "", "ready")},
		{"configure mail standalone", mutationPolicyJob("mail_configure", "mail-stack", ""), mutationPolicyClaim(serviceMutationStepConfigureMailStack, "mail-stack", "", "configure")},
		{"configure mail after postfix install", mutationPolicyJob("service_install", "postfix", ""), mutationPolicyClaim(serviceMutationStepConfigureMailStack, "mail-stack", "", "configure")},
		{"configure mail after dovecot install", mutationPolicyJob("service_install", "dovecot", ""), mutationPolicyClaim(serviceMutationStepConfigureMailStack, "mail-stack", "", "configure")},
		{"configure mail profile", mutationPolicyJob("mail_profile_install", core.MailProfileCore, ""), mutationPolicyClaim(serviceMutationStepConfigureMailStack, "mail-stack", "", "configure")},
		{"wire filters at startup", mutationPolicyJob("mail_filter_wire", "startup", ""), mutationPolicyClaim(serviceMutationStepWireMailFilters, "mail-filters", "", "wire")},
		{"wire selected filter", mutationPolicyJob("mail_filter_wire", "rspamd", ""), mutationPolicyClaim(serviceMutationStepWireMailFilters, "mail-filters", "", "wire")},
		{"wire installed filter", mutationPolicyJob("service_install", "rspamd", ""), mutationPolicyClaim(serviceMutationStepWireMailFilters, "mail-filters", "", "wire")},
		{"wire protected profile", mutationPolicyJob("mail_profile_install", core.MailProfileProtected, ""), mutationPolicyClaim(serviceMutationStepWireMailFilters, "mail-filters", "", "wire")},
		{"configure mail submission", mutationPolicyJob("mail_submission_configure", "postfix", ""), mutationPolicyClaim(serviceMutationStepConfigureMailSubmission, "postfix", "", "configure")},
		{"configure profile submission", mutationPolicyJob("mail_profile_install", core.MailProfileCore, ""), mutationPolicyClaim(serviceMutationStepConfigureMailSubmission, "postfix", "", "configure")},
		{"sync mail tls", mutationPolicyJob("mail_tls_sync", "mail-tls", mailTLSQualifier), mutationPolicyClaim(serviceMutationStepSyncMailTLS, "mail-tls", mailTLSQualifier, "sync")},
		{"configure dkim", mutationPolicyJob("dkim_signing_configure", "opendkim", ""), mutationPolicyClaim(serviceMutationStepConfigureDKIMSigning, "opendkim", "", "configure")},
		{"configure phpmyadmin after install", mutationPolicyJob("service_install", "phpmyadmin", ""), mutationPolicyClaim(serviceMutationStepConfigureDBTools, "dbtools", "", "configure")},
		{"configure phppgadmin after install", mutationPolicyJob("service_install", "phppgadmin", ""), mutationPolicyClaim(serviceMutationStepConfigureDBTools, "dbtools", "", "configure")},
		{"configure phpmyadmin standalone", mutationPolicyJob("dbtools_configure", "phpmyadmin", ""), mutationPolicyClaim(serviceMutationStepConfigureDBTools, "dbtools", "", "configure")},
		{"configure phppgadmin standalone", mutationPolicyJob("dbtools_configure", "phppgadmin", ""), mutationPolicyClaim(serviceMutationStepConfigureDBTools, "dbtools", "", "configure")},
		{"setup vpn", mutationPolicyJob("vpn_setup", "wireguard", ""), mutationPolicyClaim(serviceMutationStepSetupVPN, "wireguard", "", "setup")},
		{"setup vpn after install", mutationPolicyJob("service_install", "wireguard", ""), mutationPolicyClaim(serviceMutationStepSetupVPN, "wireguard", "", "setup")},
		{"sync vpn peers", mutationPolicyJob("vpn_peer_sync", "wireguard", vpnQualifier), mutationPolicyClaim(serviceMutationStepSyncVPNPeers, "wireguard", vpnQualifier, "sync")},
		{"firewall live apply", mutationPolicyJob("firewall_apply", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnableLive)},
		{"firewall persisted apply", mutationPolicyJob("firewall_apply", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnablePersisted)},
		{"firewall persisted disable", mutationPolicyJob("firewall_apply", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallDisablePersisted)},
		{"firewall sync", mutationPolicyJob("firewall_sync", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnableLive)},
		{"issue panel certificate", mutationPolicyJob("panel_certificate_issue", "panel.example.com", certificateQualifier), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "panel.example.com", certificateQualifier, "issue")},
		{"activate panel certificate", mutationPolicyJob(panelCertificateActivationKind, "panel.example.com", ""), mutationPolicyClaim(serviceMutationStepActivatePanelCertificate, "panel.example.com", "", "activate")},
	}

	seenMethods := make(map[serviceMutationStepMethod]bool)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := authorizeServiceMutationStep(tt.job, tt.claim); err != nil {
				t.Fatalf("authorizeServiceMutationStep(%+v, %+v): %v", tt.job, tt.claim, err)
			}
		})
		seenMethods[tt.claim.method] = true
	}

	expectedMethods := []serviceMutationStepMethod{
		serviceMutationStepConfigureDBTools,
		serviceMutationStepConfigureDKIMSigning,
		serviceMutationStepSyncDNSZone,
		serviceMutationStepSyncDNSZoneV3,
		serviceMutationStepRecoverDNSZoneV3,
		serviceMutationStepSwitchDNSEngine,
		serviceMutationStepConfigurePowerDNSSQLite,
		serviceMutationStepApplyFirewall,
		serviceMutationStepInstallService,
		serviceMutationStepUninstallService,
		serviceMutationStepConfigureMailStack,
		serviceMutationStepWireMailFilters,
		serviceMutationStepConfigureMailSubmission,
		serviceMutationStepSyncMailTLS,
		serviceMutationStepServiceAction,
		serviceMutationStepStartService,
		serviceMutationStepResetFailedUnit,
		serviceMutationStepEnsureNginxReady,
		serviceMutationStepIssuePanelCertificate,
		serviceMutationStepEnableRepo,
		serviceMutationStepDisableRepo,
		serviceMutationStepInstallNodeVersion,
		serviceMutationStepRemoveNodeVersion,
		serviceMutationStepSetupVPN,
		serviceMutationStepSyncVPNPeers,
		serviceMutationStepInstallRoundcube,
		serviceMutationStepRemoveRoundcube,
		serviceMutationStepConfigureWebmail,
		serviceMutationStepActivatePanelCertificate,
	}
	for _, method := range expectedMethods {
		if !seenMethods[method] {
			t.Errorf("declared privileged method %q has no positive policy row", method)
		}
	}
	if len(seenMethods) != len(expectedMethods) {
		t.Fatalf("positive policy methods=%d want=%d", len(seenMethods), len(expectedMethods))
	}
}

func TestServiceMutationStepPolicyRejectsEveryGenericDNSEngineClaim(t *testing.T) {
	targets := []string{
		"bind",
		"bind9",
		"named",
		"bind9.service",
		"named.service",
		"pdns",
		"pdns.service",
	}
	tests := []struct {
		name    string
		jobKind string
		method  serviceMutationStepMethod
		action  string
	}{
		{"install", "service_install", serviceMutationStepInstallService, "install"},
		{"uninstall", "service_uninstall", serviceMutationStepUninstallService, "uninstall"},
		{"action start", "service_start", serviceMutationStepServiceAction, "start"},
		{"action stop", "service_stop", serviceMutationStepServiceAction, "stop"},
		{"action restart", "service_restart", serviceMutationStepServiceAction, "restart"},
		{"action reload", "service_reload", serviceMutationStepServiceAction, "reload"},
		{"start after install", "service_install", serviceMutationStepStartService, "start"},
		{"reset after install", "service_install", serviceMutationStepResetFailedUnit, "reset-failed"},
	}

	for _, target := range targets {
		for _, tt := range tests {
			t.Run(target+"/"+tt.name, func(t *testing.T) {
				err := authorizeServiceMutationStep(
					mutationPolicyJob(tt.jobKind, target, ""),
					mutationPolicyClaim(tt.method, target, "", tt.action),
				)
				if !errors.Is(err, errServiceMutationStepUnauthorized) {
					t.Fatalf("error=%v want stable unauthorized sentinel", err)
				}
			})
		}
	}

	pdnsInstallClaims := []struct {
		name  string
		claim serviceMutationStepClaim
	}{
		{"restart", mutationPolicyClaim(serviceMutationStepServiceAction, "pdns", "", "restart")},
		{"configure", mutationPolicyClaim(serviceMutationStepConfigurePowerDNSSQLite, "pdns", "", "configure")},
	}
	for _, tt := range pdnsInstallClaims {
		t.Run("pdns install cannot "+tt.name, func(t *testing.T) {
			err := authorizeServiceMutationStep(
				mutationPolicyJob("service_install", "pdns", ""),
				tt.claim,
			)
			if !errors.Is(err, errServiceMutationStepUnauthorized) {
				t.Fatalf("error=%v want stable unauthorized sentinel", err)
			}
		})
	}
}

func TestServiceMutationMailProfilesAllowOnlyExactCompiledMembership(t *testing.T) {
	profiles := []struct {
		id      string
		members map[string]bool
	}{
		{core.MailProfileCore, map[string]bool{"postfix": true, "dovecot": true}},
		{core.MailProfileWebmail, map[string]bool{"postfix": true, "dovecot": true, "nginx": true, "php-fpm": true, "roundcube": true}},
		{core.MailProfileProtected, map[string]bool{"postfix": true, "dovecot": true, "rspamd": true}},
	}
	universe := []string{"postfix", "dovecot", "nginx", "php-fpm", "roundcube", "rspamd"}

	for _, profile := range profiles {
		profile := profile
		t.Run(profile.id, func(t *testing.T) {
			job := mutationPolicyJob("mail_profile_install", profile.id, "")
			for _, serviceID := range universe {
				method := serviceMutationStepInstallService
				if serviceID == "roundcube" {
					method = serviceMutationStepInstallRoundcube
				}
				claim := mutationPolicyClaim(method, serviceID, "", "install")
				err := authorizeServiceMutationStep(job, claim)
				if profile.members[serviceID] && err != nil {
					t.Errorf("member %q rejected: %v", serviceID, err)
				}
				if !profile.members[serviceID] && !errors.Is(err, errServiceMutationStepUnauthorized) {
					t.Errorf("non-member %q err=%v want unauthorized sentinel", serviceID, err)
				}
			}

			firstMember := universe[0]
			for _, candidate := range universe {
				if profile.members[candidate] {
					firstMember = candidate
					break
				}
			}
			claim := mutationPolicyClaim(serviceMutationStepInstallService, firstMember, "", "install")
			if err := authorizeServiceMutationStep(
				mutationPolicyJob("mail_profile_install", profile.id, "unexpected-package"),
				claim,
			); !errors.Is(err, errServiceMutationStepUnauthorized) {
				t.Errorf("profile ledger package err=%v want unauthorized sentinel", err)
			}
			if err := authorizeServiceMutationStep(
				job,
				mutationPolicyClaim(serviceMutationStepInstallService, firstMember, "unexpected-package", "install"),
			); !errors.Is(err, errServiceMutationStepUnauthorized) {
				t.Errorf("profile claim package err=%v want unauthorized sentinel", err)
			}
		})
	}

	unknown := mutationPolicyJob("mail_profile_install", "unknown-profile", "")
	if err := authorizeServiceMutationStep(
		unknown,
		mutationPolicyClaim(serviceMutationStepInstallService, "postfix", "", "install"),
	); !errors.Is(err, errServiceMutationStepUnauthorized) {
		t.Fatalf("unknown profile err=%v want unauthorized sentinel", err)
	}
}

func TestServiceMutationStepPolicyRejectsMismatchesAndConfusedDeputies(t *testing.T) {
	vpnQualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("0", 64)
	otherVPNQualifier := "vpn-peer-sync/v1:sha256:" + strings.Repeat("1", 64)
	firewallQualifier := "firewall-apply/v1:sha256:" + strings.Repeat("0", 64)
	otherFirewallQualifier := "firewall-apply/v1:sha256:" + strings.Repeat("1", 64)
	certificateQualifier := "panel-certificate-issue/v1:sha256:" + strings.Repeat("0", 64)
	otherCertificateQualifier := "panel-certificate-issue/v1:sha256:" + strings.Repeat("1", 64)
	dnsQualifier := "dns-zone-sync/v1:sha256:" + strings.Repeat("0", 64)
	otherDNSQualifier := "dns-zone-sync/v1:sha256:" + strings.Repeat("1", 64)
	tests := []struct {
		name  string
		job   *ServiceMutationJob
		claim serviceMutationStepClaim
	}{
		{"nil job", nil, mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"wrong kind", mutationPolicyJob("service_uninstall", "nginx", ""), mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"wrong target", mutationPolicyJob("service_install", "apache", ""), mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"wrong package", mutationPolicyJob("runtime_install", "node", "22.14.0"), mutationPolicyClaim(serviceMutationStepInstallNodeVersion, "node", "20.19.0", "install")},
		{"wrong action", mutationPolicyJob("repo_enable", "docker", ""), mutationPolicyClaim(serviceMutationStepEnableRepo, "docker", "", "disable")},
		{"wrong known method", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim(serviceMutationStepUninstallService, "nginx", "", "uninstall")},
		{"unknown method", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim(serviceMutationStepMethod("Agent.Unknown"), "nginx", "", "install")},
		{"empty method", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim("", "nginx", "", "install")},
		{"unknown job", mutationPolicyJob("unknown_kind", "nginx", ""), mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"webmail profile cannot install rspamd", mutationPolicyJob("mail_profile_install", core.MailProfileWebmail, ""), mutationPolicyClaim(serviceMutationStepInstallService, "rspamd", "", "install")},
		{"protected profile cannot prepare nginx", mutationPolicyJob("mail_profile_install", core.MailProfileProtected, ""), mutationPolicyClaim(serviceMutationStepEnsureNginxReady, "nginx", "", "ready")},
		{"repo enable cannot disable", mutationPolicyJob("repo_enable", "docker", ""), mutationPolicyClaim(serviceMutationStepDisableRepo, "docker", "", "disable")},
		{"nginx install cannot install node", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim(serviceMutationStepInstallNodeVersion, "node", "22.14.0", "install")},
		{"vpn setup cannot sync peers", mutationPolicyJob("vpn_setup", "wireguard", ""), mutationPolicyClaim(serviceMutationStepSyncVPNPeers, "wireguard", vpnQualifier, "sync")},
		{"legacy direct vpn sync has no commitment", mutationPolicyJob("vpn_peer_sync", "wireguard", ""), mutationPolicyClaim(serviceMutationStepSyncVPNPeers, "wireguard", vpnQualifier, "sync")},
		{"direct vpn sync digest mismatch", mutationPolicyJob("vpn_peer_sync", "wireguard", vpnQualifier), mutationPolicyClaim(serviceMutationStepSyncVPNPeers, "wireguard", otherVPNQualifier, "sync")},
		{"direct vpn sync malformed digest", mutationPolicyJob("vpn_peer_sync", "wireguard", vpnQualifier), mutationPolicyClaim(serviceMutationStepSyncVPNPeers, "wireguard", "not-a-qualifier", "sync")},
		{"legacy certificate job cannot authorize V2", mutationPolicyJob("panel_certificate_issue", "panel.example.com", "certbot"), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "panel.example.com", certificateQualifier, "issue")},
		{"certificate qualifier mismatch", mutationPolicyJob("panel_certificate_issue", "panel.example.com", certificateQualifier), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "panel.example.com", otherCertificateQualifier, "issue")},
		{"certificate malformed qualifier", mutationPolicyJob("panel_certificate_issue", "panel.example.com", certificateQualifier), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "panel.example.com", "certbot", "issue")},
		{"certificate domain mismatch", mutationPolicyJob("panel_certificate_issue", "panel.example.com", certificateQualifier), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "other.example.com", certificateQualifier, "issue")},
		{"noncanonical certificate issue target", mutationPolicyJob("panel_certificate_issue", "panel.example.com.", certificateQualifier), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "panel.example.com.", certificateQualifier, "issue")},
		{"invalid certificate issue target", mutationPolicyJob("panel_certificate_issue", "not-a-domain", certificateQualifier), mutationPolicyClaim(serviceMutationStepIssuePanelCertificate, "not-a-domain", certificateQualifier, "issue")},
		{"noncanonical certificate activation target", mutationPolicyJob(panelCertificateActivationKind, "panel.example.com.", ""), mutationPolicyClaim(serviceMutationStepActivatePanelCertificate, "panel.example.com.", "", "activate")},
		{"invalid certificate activation target", mutationPolicyJob(panelCertificateActivationKind, "not-a-domain", ""), mutationPolicyClaim(serviceMutationStepActivatePanelCertificate, "not-a-domain", "", "activate")},
		{"pdns install cannot sync zone", mutationPolicyJob("service_install", "pdns", ""), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", dnsQualifier, "sync")},
		{"pdns install cannot delete zone", mutationPolicyJob("service_install", "pdns", ""), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", dnsQualifier, "delete")},
		{"legacy direct DNS sync has no commitment", mutationPolicyJob("dns_zone_sync", "example.com", ""), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", dnsQualifier, "sync")},
		{"direct DNS sync digest mismatch", mutationPolicyJob("dns_zone_sync", "example.com", dnsQualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", otherDNSQualifier, "sync")},
		{"direct DNS sync malformed digest", mutationPolicyJob("dns_zone_sync", "example.com", dnsQualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "example.com", "not-a-qualifier", "sync")},
		{"direct DNS sync domain mismatch", mutationPolicyJob("dns_zone_sync", "example.com", dnsQualifier), mutationPolicyClaim(serviceMutationStepSyncDNSZone, "other.example.com", dnsQualifier, "sync")},
		{"legacy direct firewall has no commitment", mutationPolicyJob("firewall_apply", "nftables", ""), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnableLive)},
		{"direct firewall digest mismatch", mutationPolicyJob("firewall_apply", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", otherFirewallQualifier, serviceMutationFirewallEnableLive)},
		{"direct firewall malformed digest", mutationPolicyJob("firewall_apply", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", "not-a-qualifier", serviceMutationFirewallEnableLive)},
		{"direct firewall live disable forbidden", mutationPolicyJob("firewall_apply", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallDisableLive)},
		{"firewall sync cannot persist enable", mutationPolicyJob("firewall_sync", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnablePersisted)},
		{"firewall sync cannot persist disable", mutationPolicyJob("firewall_sync", "nftables", firewallQualifier), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallDisablePersisted)},
		{"service install cannot borrow firewall", mutationPolicyJob("service_install", "nginx", ""), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnableLive)},
		{"profile cannot borrow firewall", mutationPolicyJob("mail_profile_install", core.MailProfileCore, ""), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnableLive)},
		{"certificate issue cannot borrow firewall", mutationPolicyJob("panel_certificate_issue", "panel.example.com", "certbot"), mutationPolicyClaim(serviceMutationStepApplyFirewall, "nftables", firewallQualifier, serviceMutationFirewallEnableLive)},
		{"noncanonical job kind", mutationPolicyJob("Service_Install", "nginx", ""), mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"noncanonical job target", mutationPolicyJob("service_install", "Nginx", ""), mutationPolicyClaim(serviceMutationStepInstallService, "nginx", "", "install")},
		{"noncanonical job package", mutationPolicyJob("runtime_install", "node", " 22.14.0 "), mutationPolicyClaim(serviceMutationStepInstallNodeVersion, "node", "22.14.0", "install")},
		{"noncanonical claim target", mutationPolicyJob("service_install", "nginx", ""), serviceMutationStepClaim{method: serviceMutationStepInstallService, target: "Nginx", action: "install"}},
		{"noncanonical claim package", mutationPolicyJob("runtime_install", "node", "22.14.0"), serviceMutationStepClaim{method: serviceMutationStepInstallNodeVersion, target: "node", packageName: " 22.14.0 ", action: "install"}},
		{"noncanonical claim action", mutationPolicyJob("service_install", "nginx", ""), serviceMutationStepClaim{method: serviceMutationStepInstallService, target: "nginx", action: "Install"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authorizeServiceMutationStep(tt.job, tt.claim)
			if !errors.Is(err, errServiceMutationStepUnauthorized) {
				t.Fatalf("error=%v want stable unauthorized sentinel", err)
			}
		})
	}
}

func TestServiceMutationPrivilegedCallsitesCarryTypedClaims(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	expectedMethods := map[string]bool{
		"serviceMutationStepConfigureDBTools":        true,
		"serviceMutationStepConfigureDKIMSigning":    true,
		"serviceMutationStepSyncDNSZone":             true,
		"serviceMutationStepSyncDNSZoneV3":           true,
		"serviceMutationStepRecoverDNSZoneV3":        true,
		"serviceMutationStepSwitchDNSEngine":         true,
		"serviceMutationStepSecureDNSZone":           true,
		"serviceMutationStepConfigureDNSCluster":     true,
		"serviceMutationStepConfigurePowerDNSSQLite": true,
		"serviceMutationStepApplyFirewall":           true,
		"serviceMutationStepInstallService":          true,
		"serviceMutationStepUninstallService":        true,
		"serviceMutationStepConfigureMailStack":      true,
		"serviceMutationStepWireMailFilters":         true,
		"serviceMutationStepConfigureMailSubmission": true,
		"serviceMutationStepSyncMailTLS":             true,
		"serviceMutationStepServiceAction":           true,
		"serviceMutationStepStartService":            true,
		"serviceMutationStepResetFailedUnit":         true,
		"serviceMutationStepEnsureNginxReady":        true,
		"serviceMutationStepIssuePanelCertificate":   true,
		"serviceMutationStepEnableRepo":              true,
		"serviceMutationStepDisableRepo":             true,
		"serviceMutationStepInstallNodeVersion":      true,
		"serviceMutationStepRemoveNodeVersion":       true,
		"serviceMutationStepSetupVPN":                true,
		"serviceMutationStepSyncVPNPeers":            true,
		"serviceMutationStepInstallRoundcube":        true,
		"serviceMutationStepRemoveRoundcube":         true,
		"serviceMutationStepConfigureWebmail":        true,
	}
	seenMethods := make(map[string]string)
	directAcquire := make(map[string][]string)
	fset := token.NewFileSet()

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			rejectServiceMutationStepFunctionAliases(t, fset, name, function)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch selector.Sel.Name {
				case "requiredServiceMutationStep":
					position := fset.Position(call.Pos())
					if len(call.Args) != 2 {
						t.Errorf("%s:%d requiredServiceMutationStep args=%d want=2", name, position.Line, len(call.Args))
						return true
					}
					claimCall, ok := call.Args[1].(*ast.CallExpr)
					if !ok || len(claimCall.Args) != 4 {
						t.Errorf("%s:%d second argument is not a four-field claim constructor", name, position.Line)
						return true
					}
					constructor, ok := claimCall.Fun.(*ast.Ident)
					method, methodOK := claimCall.Args[0].(*ast.Ident)
					if !ok || constructor.Name != "newServiceMutationStepClaim" || !methodOK {
						t.Errorf("%s:%d claim is not built by newServiceMutationStepClaim with a declared method", name, position.Line)
						return true
					}
					if previous := seenMethods[method.Name]; previous != "" {
						t.Errorf("method claim %s appears at both %s and %s:%d", method.Name, previous, name, position.Line)
					}
					seenMethods[method.Name] = name
				case "acquireStep":
					directAcquire[name] = append(directAcquire[name], function.Name.Name)
				}
				return true
			})
		}
	}

	if len(seenMethods) != 30 {
		t.Errorf("production requiredServiceMutationStep claim count=%d want=30", len(seenMethods))
	}
	for method := range expectedMethods {
		if seenMethods[method] == "" {
			t.Errorf("production RPC is missing typed claim %s", method)
		}
	}
	for method, file := range seenMethods {
		if !expectedMethods[method] {
			t.Errorf("unexpected production typed claim %s in %s", method, file)
		}
	}

	wantDirect := map[string][]string{
		"panel_cert_reconcile.go": {"reconcilePanelCertificateActivationOnce"},
		"service_mutation_rpc.go": {"requiredServiceMutationStep"},
	}
	for file := range directAcquire {
		sort.Strings(directAcquire[file])
	}
	for file := range wantDirect {
		sort.Strings(wantDirect[file])
	}
	if len(directAcquire) != len(wantDirect) {
		t.Fatalf("direct acquireStep files=%v want=%v", directAcquire, wantDirect)
	}
	for file, wantFunctions := range wantDirect {
		gotFunctions := directAcquire[file]
		if strings.Join(gotFunctions, ",") != strings.Join(wantFunctions, ",") {
			t.Errorf("direct acquireStep callers in %s=%v want=%v", file, gotFunctions, wantFunctions)
		}
	}
}

func rejectServiceMutationStepFunctionAliases(
	t *testing.T,
	fset *token.FileSet,
	filename string,
	function *ast.FuncDecl,
) {
	t.Helper()
	var ancestors []ast.Node
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if node == nil {
			ancestors = ancestors[:len(ancestors)-1]
			return true
		}
		var parent ast.Node
		if len(ancestors) != 0 {
			parent = ancestors[len(ancestors)-1]
		}
		if selector, ok := node.(*ast.SelectorExpr); ok &&
			(selector.Sel.Name == "requiredServiceMutationStep" || selector.Sel.Name == "acquireStep") {
			call, directCall := parent.(*ast.CallExpr)
			if !directCall || call.Fun != selector {
				position := fset.Position(selector.Pos())
				t.Errorf(
					"%s:%d %s takes %s as a function value; privileged step boundaries must be direct calls",
					filename,
					position.Line,
					function.Name.Name,
					selector.Sel.Name,
				)
			}
		}
		ancestors = append(ancestors, node)
		return true
	})
}
