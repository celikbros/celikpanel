package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

type serviceMutationStepMethod string

const (
	serviceMutationStepConfigureDBTools         serviceMutationStepMethod = "Agent.ConfigureDBTools"
	serviceMutationStepConfigureDKIMSigning     serviceMutationStepMethod = "Agent.ConfigureDKIMSigning"
	serviceMutationStepSyncDNSZone              serviceMutationStepMethod = "Agent.SyncDNSZone"
	serviceMutationStepConfigurePowerDNSSQLite  serviceMutationStepMethod = "Agent.ConfigurePowerDNSSQLite"
	serviceMutationStepApplyFirewall            serviceMutationStepMethod = "Agent.ApplyFirewall"
	serviceMutationStepInstallService           serviceMutationStepMethod = "Agent.InstallService"
	serviceMutationStepUninstallService         serviceMutationStepMethod = "Agent.UninstallService"
	serviceMutationStepConfigureMailStack       serviceMutationStepMethod = "Agent.ConfigureMailStack"
	serviceMutationStepWireMailFilters          serviceMutationStepMethod = "Agent.WireMailFilters"
	serviceMutationStepConfigureMailSubmission  serviceMutationStepMethod = "Agent.ConfigureMailSubmission"
	serviceMutationStepReconcileMailTLS         serviceMutationStepMethod = "Agent.ReconcileMailTLSMutation"
	serviceMutationStepServiceAction            serviceMutationStepMethod = "Agent.ServiceMutationAction"
	serviceMutationStepStartService             serviceMutationStepMethod = "Agent.StartServiceMutation"
	serviceMutationStepResetFailedUnit          serviceMutationStepMethod = "Agent.ResetFailedUnitMutation"
	serviceMutationStepEnsureNginxReady         serviceMutationStepMethod = "Agent.EnsureNginxReady"
	serviceMutationStepIssuePanelCertificate    serviceMutationStepMethod = "Agent.IssuePanelCertificate"
	serviceMutationStepEnableRepo               serviceMutationStepMethod = "Agent.EnableRepo"
	serviceMutationStepDisableRepo              serviceMutationStepMethod = "Agent.DisableRepo"
	serviceMutationStepInstallNodeVersion       serviceMutationStepMethod = "Agent.InstallNodeVersion"
	serviceMutationStepRemoveNodeVersion        serviceMutationStepMethod = "Agent.RemoveNodeVersion"
	serviceMutationStepSetupVPN                 serviceMutationStepMethod = "Agent.SetupVPN"
	serviceMutationStepSyncVPNPeers             serviceMutationStepMethod = "Agent.SyncVPNPeersV2"
	serviceMutationStepInstallRoundcube         serviceMutationStepMethod = "Agent.InstallRoundcube"
	serviceMutationStepRemoveRoundcube          serviceMutationStepMethod = "Agent.RemoveRoundcube"
	serviceMutationStepConfigureWebmail         serviceMutationStepMethod = "Agent.ConfigureWebmail"
	serviceMutationStepActivatePanelCertificate serviceMutationStepMethod = "agent.panel-certificate-activation"
)

const (
	serviceMutationFirewallEnableLive       = "enable-live"
	serviceMutationFirewallEnablePersisted  = "enable-persisted"
	serviceMutationFirewallDisableLive      = "disable-live"
	serviceMutationFirewallDisablePersisted = "disable-persisted"
)

var errServiceMutationStepUnauthorized = errors.New(
	"active service mutation lease does not authorize this privileged step",
)

type serviceMutationStepClaim struct {
	method      serviceMutationStepMethod
	target      string
	packageName string
	action      string
}

func newServiceMutationStepClaim(
	method serviceMutationStepMethod,
	target, packageName, action string,
) serviceMutationStepClaim {
	return serviceMutationStepClaim{
		method:      method,
		target:      strings.ToLower(strings.TrimSpace(target)),
		packageName: strings.TrimSpace(packageName),
		action:      strings.ToLower(strings.TrimSpace(action)),
	}
}

func serviceMutationFirewallAction(enabled, persist bool) string {
	switch {
	case enabled && persist:
		return serviceMutationFirewallEnablePersisted
	case enabled:
		return serviceMutationFirewallEnableLive
	case persist:
		return serviceMutationFirewallDisablePersisted
	default:
		return serviceMutationFirewallDisableLive
	}
}

func authorizeServiceMutationStep(
	job *ServiceMutationJob,
	claim serviceMutationStepClaim,
) error {
	if job == nil || claim.method == "" ||
		claim.target != strings.ToLower(strings.TrimSpace(claim.target)) ||
		claim.packageName != strings.TrimSpace(claim.packageName) ||
		claim.action != strings.ToLower(strings.TrimSpace(claim.action)) ||
		job.Kind != strings.ToLower(strings.TrimSpace(job.Kind)) ||
		job.Target != strings.ToLower(strings.TrimSpace(job.Target)) ||
		job.PackageName != strings.TrimSpace(job.PackageName) ||
		!serviceMutationStepAllowed(job, claim) {
		return errServiceMutationStepUnauthorized
	}
	return nil
}

func serviceMutationJobMatches(job *ServiceMutationJob, kind, target, packageName string) bool {
	return job != nil && job.Kind == kind && job.Target == target && job.PackageName == packageName
}

func serviceMutationMailProfileKnown(job *ServiceMutationJob) bool {
	if job == nil || job.Kind != "mail_profile_install" || job.PackageName != "" {
		return false
	}
	_, ok := core.MailProfileServiceIDs(job.Target)
	return ok
}

func serviceMutationMailProfileContains(job *ServiceMutationJob, serviceID string) bool {
	return serviceMutationMailProfileKnown(job) &&
		core.MailProfileContainsService(job.Target, serviceID)
}

func serviceMutationSpamFilter(serviceID string) bool {
	service := core.GetManagedServiceByID(serviceID)
	return service != nil && service.ConflictGroup == "spam-filter"
}

func serviceMutationCanonicalFQDN(value string) bool {
	canonical, err := hostname.CanonicalFQDN(value)
	return err == nil && canonical == value
}

func serviceMutationValidInstallJob(job *ServiceMutationJob) bool {
	if job == nil || job.Kind != "service_install" {
		return false
	}
	service := core.GetManagedServiceByID(job.Target)
	if service == nil {
		return false
	}
	if job.PackageName == "" {
		return true
	}
	_, err := validateRepoPackageSelection(service, job.PackageName)
	return err == nil
}

func serviceMutationStepAllowed(job *ServiceMutationJob, claim serviceMutationStepClaim) bool {
	switch claim.method {
	case serviceMutationStepInstallService:
		return claim.action == "install" && claim.target != "roundcube" &&
			(serviceMutationJobMatches(job, "service_install", claim.target, claim.packageName) ||
				(claim.packageName == "" && serviceMutationMailProfileContains(job, claim.target)))

	case serviceMutationStepUninstallService:
		return claim.action == "uninstall" && claim.target != "roundcube" &&
			serviceMutationJobMatches(job, "service_uninstall", claim.target, claim.packageName)

	case serviceMutationStepInstallRoundcube:
		return claim.target == "roundcube" && claim.packageName == "" && claim.action == "install" &&
			(serviceMutationJobMatches(job, "service_install", "roundcube", "") ||
				serviceMutationMailProfileContains(job, "roundcube"))

	case serviceMutationStepRemoveRoundcube:
		return claim.target == "roundcube" && claim.packageName == "" && claim.action == "remove" &&
			serviceMutationJobMatches(job, "service_uninstall", "roundcube", "")

	case serviceMutationStepConfigureWebmail:
		return claim.target == "roundcube" && claim.packageName == "" && claim.action == "configure" &&
			(serviceMutationJobMatches(job, "service_install", "roundcube", "") ||
				serviceMutationJobMatches(job, "service_uninstall", "roundcube", "") ||
				serviceMutationMailProfileContains(job, "roundcube"))

	case serviceMutationStepInstallNodeVersion:
		return claim.target == "node" && claim.action == "install" &&
			serviceMutationJobMatches(job, "runtime_install", "node", claim.packageName)

	case serviceMutationStepRemoveNodeVersion:
		return claim.target == "node" && claim.packageName != "" && claim.action == "remove" &&
			serviceMutationJobMatches(job, "runtime_remove", "node:"+claim.packageName, "")

	case serviceMutationStepEnableRepo:
		return claim.packageName == "" && claim.action == "enable" &&
			serviceMutationJobMatches(job, "repo_enable", claim.target, "")

	case serviceMutationStepDisableRepo:
		return claim.packageName == "" && claim.action == "disable" &&
			serviceMutationJobMatches(job, "repo_disable", claim.target, "")

	case serviceMutationStepServiceAction:
		if claim.packageName != "" ||
			(claim.action != "start" && claim.action != "stop" &&
				claim.action != "restart" && claim.action != "reload") {
			return false
		}
		return serviceMutationJobMatches(job, "service_"+claim.action, claim.target, "") ||
			(claim.target == "pdns" && claim.action == "restart" &&
				serviceMutationJobMatches(job, "service_install", "pdns", ""))

	case serviceMutationStepStartService, serviceMutationStepResetFailedUnit:
		expectedAction := "start"
		if claim.method == serviceMutationStepResetFailedUnit {
			expectedAction = "reset-failed"
		}
		if claim.packageName != "" || claim.action != expectedAction ||
			(claim.target != "postfix" && claim.target != "dovecot") {
			return false
		}
		return serviceMutationJobMatches(job, "service_install", claim.target, "") ||
			serviceMutationMailProfileContains(job, claim.target)

	case serviceMutationStepConfigurePowerDNSSQLite:
		return claim.target == "pdns" && claim.packageName == "" && claim.action == "configure" &&
			(serviceMutationJobMatches(job, "pdns_configure", "pdns", "") ||
				serviceMutationJobMatches(job, "service_install", "pdns", ""))

	case serviceMutationStepSyncDNSZone:
		if claim.target == "" || claim.packageName != "" ||
			(claim.action != "sync" && claim.action != "delete") {
			return false
		}
		return serviceMutationJobMatches(job, "dns_zone_sync", claim.target, "") ||
			(claim.action == "sync" &&
				serviceMutationJobMatches(job, "service_install", "pdns", ""))

	case serviceMutationStepEnsureNginxReady:
		return claim.target == "nginx" && claim.packageName == "" && claim.action == "ready" &&
			(serviceMutationJobMatches(job, "service_install", "nginx", "") ||
				serviceMutationMailProfileContains(job, "nginx"))

	case serviceMutationStepConfigureMailStack:
		if claim.target != "mail-stack" || claim.packageName != "" || claim.action != "configure" {
			return false
		}
		return serviceMutationJobMatches(job, "mail_configure", "mail-stack", "") ||
			serviceMutationJobMatches(job, "service_install", "postfix", "") ||
			serviceMutationJobMatches(job, "service_install", "dovecot", "") ||
			(serviceMutationMailProfileContains(job, "postfix") &&
				serviceMutationMailProfileContains(job, "dovecot"))

	case serviceMutationStepWireMailFilters:
		if claim.target != "mail-filters" || claim.packageName != "" || claim.action != "wire" {
			return false
		}
		return (job.Kind == "mail_filter_wire" && job.PackageName == "" &&
			(job.Target == "startup" || serviceMutationSpamFilter(job.Target))) ||
			(job.Kind == "service_install" && job.PackageName == "" &&
				serviceMutationSpamFilter(job.Target)) ||
			serviceMutationJobMatches(job, "mail_profile_install", core.MailProfileProtected, "")

	case serviceMutationStepConfigureMailSubmission:
		return claim.target == "postfix" && claim.packageName == "" && claim.action == "configure" &&
			(serviceMutationJobMatches(job, "mail_submission_configure", "postfix", "") ||
				(serviceMutationMailProfileContains(job, "postfix") &&
					serviceMutationMailProfileContains(job, "dovecot")))

	case serviceMutationStepReconcileMailTLS:
		return claim.target == "mail-tls" && claim.packageName == "" && claim.action == "reconcile" &&
			serviceMutationMailProfileContains(job, "postfix") &&
			serviceMutationMailProfileContains(job, "dovecot")

	case serviceMutationStepConfigureDKIMSigning:
		return claim.target == "opendkim" && claim.packageName == "" && claim.action == "configure" &&
			serviceMutationJobMatches(job, "dkim_signing_configure", "opendkim", "")

	case serviceMutationStepConfigureDBTools:
		if claim.target != "dbtools" || claim.packageName != "" || claim.action != "configure" ||
			(job.Target != "phpmyadmin" && job.Target != "phppgadmin") {
			return false
		}
		return job.PackageName == "" &&
			(job.Kind == "service_install" || job.Kind == "dbtools_configure")

	case serviceMutationStepSetupVPN:
		return claim.target == "wireguard" && claim.packageName == "" && claim.action == "setup" &&
			(serviceMutationJobMatches(job, "vpn_setup", "wireguard", "") ||
				serviceMutationJobMatches(job, "service_install", "wireguard", ""))

	case serviceMutationStepSyncVPNPeers:
		if claim.target != "wireguard" || claim.action != "sync" ||
			!mutationpayload.ValidVPNPeerSyncQualifier(claim.packageName) {
			return false
		}
		return serviceMutationJobMatches(job, "vpn_peer_sync", "wireguard", claim.packageName)

	case serviceMutationStepApplyFirewall:
		if claim.target != "nftables" || claim.packageName != "" {
			return false
		}
		if serviceMutationJobMatches(job, "firewall_apply", "nftables", "") {
			return claim.action == serviceMutationFirewallEnableLive ||
				claim.action == serviceMutationFirewallEnablePersisted ||
				claim.action == serviceMutationFirewallDisablePersisted
		}
		if claim.action != serviceMutationFirewallEnableLive {
			return false
		}
		return serviceMutationJobMatches(job, "firewall_sync", "nftables", "") ||
			serviceMutationValidInstallJob(job) ||
			serviceMutationMailProfileKnown(job) ||
			(job.Kind == "panel_certificate_issue" &&
				serviceMutationCanonicalFQDN(job.Target) &&
				job.PackageName == "certbot")

	case serviceMutationStepIssuePanelCertificate:
		return serviceMutationCanonicalFQDN(claim.target) &&
			claim.packageName == "certbot" && claim.action == "issue" &&
			serviceMutationJobMatches(job, "panel_certificate_issue", claim.target, "certbot")

	case serviceMutationStepActivatePanelCertificate:
		return serviceMutationCanonicalFQDN(claim.target) &&
			claim.packageName == "" && claim.action == "activate" &&
			serviceMutationJobMatches(job, panelCertificateActivationKind, claim.target, "")
	}
	return false
}
