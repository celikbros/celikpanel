package main

import (
	"context"
	"errors"
	"fmt"
	"net/rpc"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	agentRPCQuickReadTimeout    = 20 * time.Second
	agentRPCStandardReadTimeout = 60 * time.Second
	agentRPCNetworkReadTimeout  = 2 * time.Minute
	agentRPCMutationTimeout     = 3 * time.Minute
	agentRPCDatabaseTimeout     = 10 * time.Minute
	agentRPCDeploymentTimeout   = 20 * time.Minute
	agentRPCBulkImportTimeout   = 30 * time.Minute
)

type agentRPCEffect uint8

const (
	agentRPCEffectInvalid agentRPCEffect = iota
	agentRPCEffectRead
	agentRPCEffectControl
	agentRPCEffectHostMutation
	agentRPCEffectHostRepairMutation
)

type agentRPCCapability string

const (
	agentRPCCapabilityPackageLifecycle agentRPCCapability = "package.lifecycle"
	agentRPCCapabilityServiceLifecycle agentRPCCapability = "service.lifecycle"
	agentRPCCapabilityHostConfig       agentRPCCapability = "host.config"
	agentRPCCapabilityHostingSite      agentRPCCapability = "hosting.site"
	agentRPCCapabilityHostingVHost     agentRPCCapability = "hosting.vhost"
	agentRPCCapabilityHostingApp       agentRPCCapability = "hosting.app"
	agentRPCCapabilityCertificate      agentRPCCapability = "certificate"
	agentRPCCapabilityFirewall         agentRPCCapability = "firewall"
	agentRPCCapabilityDNS              agentRPCCapability = "dns"
	agentRPCCapabilityMail             agentRPCCapability = "mail"
	agentRPCCapabilityFilesystem       agentRPCCapability = "filesystem"
	agentRPCCapabilityCron             agentRPCCapability = "cron"
	agentRPCCapabilityBackup           agentRPCCapability = "backup"
	agentRPCCapabilityRuntimeNode      agentRPCCapability = "runtime.node"
	agentRPCCapabilityVPN              agentRPCCapability = "vpn"
	agentRPCCapabilityDatabase         agentRPCCapability = "database"
	agentRPCCapabilitySystemSQLite     agentRPCCapability = "system-sqlite"
	agentRPCCapabilitySystemUpdate     agentRPCCapability = "system-update"
)

var validAgentRPCCapabilities = map[agentRPCCapability]struct{}{
	agentRPCCapabilityPackageLifecycle: {},
	agentRPCCapabilityServiceLifecycle: {},
	agentRPCCapabilityHostConfig:       {},
	agentRPCCapabilityHostingSite:      {},
	agentRPCCapabilityHostingVHost:     {},
	agentRPCCapabilityHostingApp:       {},
	agentRPCCapabilityCertificate:      {},
	agentRPCCapabilityFirewall:         {},
	agentRPCCapabilityDNS:              {},
	agentRPCCapabilityMail:             {},
	agentRPCCapabilityFilesystem:       {},
	agentRPCCapabilityCron:             {},
	agentRPCCapabilityBackup:           {},
	agentRPCCapabilityRuntimeNode:      {},
	agentRPCCapabilityVPN:              {},
	agentRPCCapabilityDatabase:         {},
	agentRPCCapabilitySystemSQLite:     {},
	agentRPCCapabilitySystemUpdate:     {},
}

type agentRPCPolicy struct {
	timeout    time.Duration
	effect     agentRPCEffect
	capability agentRPCCapability
}

func (p agentRPCPolicy) validate() error {
	if p.timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	switch p.effect {
	case agentRPCEffectRead, agentRPCEffectControl:
		if p.capability != "" {
			return errors.New("read/control policy carries a host capability")
		}
		return nil
	case agentRPCEffectHostMutation, agentRPCEffectHostRepairMutation:
		if _, ok := validAgentRPCCapabilities[p.capability]; !ok {
			return errors.New("host mutation policy lacks a known capability")
		}
		return nil
	default:
		return errors.New("effect is invalid")
	}
}

type agentRPCAuthorizationGroup struct {
	effect     agentRPCEffect
	capability agentRPCCapability
	methods    []string
}

func agentRPCAuthGroup(effect agentRPCEffect, capability agentRPCCapability, methods string) agentRPCAuthorizationGroup {
	return agentRPCAuthorizationGroup{effect, capability, strings.Fields(methods)}
}

var agentRPCAuthorizationGroups = []agentRPCAuthorizationGroup{
	agentRPCAuthGroup(agentRPCEffectRead, "", `
		Agent.AppUnitLogs Agent.AppUnitStatus Agent.CheckInstalledServices Agent.CheckRBL
		Agent.CheckSystemSQLiteDatabase Agent.DNSBackendReadiness Agent.DNSClusterReadiness
		Agent.DNSEngineRollbackEvidenceV1
		Agent.DNSSECStatus Agent.DovecotStats Agent.Fail2banConfig Agent.Fail2banStatus
		Agent.FirewallStatus Agent.GetAccessLogs Agent.GetCertificateInfo Agent.GetConfig
		Agent.GetDKIMStatus Agent.GetErrorLogs Agent.GetExtendedPHPConfig Agent.GetMailPolicy
		Agent.GetMailQuotaStatus Agent.GetMySQLConfig Agent.GetPHPConfig Agent.GetPHPConfiguration
		Agent.GetPHPExtensions Agent.GetPHPLogs Agent.GetPHPPoolConfig Agent.GetPHPPools
		Agent.GetServices Agent.HostPlatform Agent.InspectBackup Agent.InspectCpmove
		Agent.InspectInstalledCertificate Agent.InstalledRepoPackages Agent.InstalledServiceIDs
		Agent.InstalledServiceIDsStrict Agent.ListBackups Agent.ListCronJobs Agent.ListFiles
		Agent.ListNodeLTS Agent.ListNodeVersions Agent.ListServiceInstances
		Agent.ListSystemSQLiteDatabases Agent.MailHealth Agent.NginxInspect Agent.PkgFamily
		Agent.PostfixQueue Agent.ReadBackupChunk Agent.ReadFile
		Agent.ReadSystemSQLiteSnapshotChunk Agent.RepoPackages Agent.ServiceCandidateVersion
		Agent.ServiceJournal Agent.ServiceMutationReadiness Agent.SiteUsage Agent.Version Agent.VPNStatus
		Agent.CheckSystemUpdate Agent.SecurityAudit Agent.SystemUpdateStatus
	`),
	agentRPCAuthGroup(agentRPCEffectControl, "", `
		Agent.BeginServiceMutation Agent.CancelServiceMutation Agent.FinishServiceMutation
		Agent.HeartbeatServiceMutation Agent.ServiceMutationStatus
	`),
	agentRPCAuthGroup(agentRPCEffectHostRepairMutation, agentRPCCapabilityPackageLifecycle, `
		Agent.RepoStatus
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityPackageLifecycle, `
		Agent.ConfigureDBTools Agent.ConfigureWebmail Agent.DisableRepo Agent.EnableRepo
		Agent.InstallRoundcube Agent.InstallService Agent.RemoveRoundcube Agent.UninstallService
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityServiceLifecycle, `
		Agent.EnsureNginxReady Agent.ResetFailedUnitMutation Agent.ServiceMutationAction
		Agent.StartServiceMutation
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityHostConfig, `
		Agent.DeletePHPPool Agent.MigratePHPPool Agent.TogglePHPExtension Agent.UpdateConfig
		Agent.UpdateExtendedPHPConfig Agent.UpdateMySQLConfig Agent.UpdatePHPConfig
		Agent.UpdatePHPConfiguration Agent.UpdatePHPPoolConfig
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityHostingSite, `
		Agent.CreateSite Agent.DeleteSite Agent.InstallWordPress
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityHostingVHost, `
		Agent.ApplyVhost Agent.ApplyVhosts
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityHostingApp, `
		Agent.ApplyAppUnit Agent.ControlAppUnit Agent.RemoveAppUnit
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityCertificate, `
		Agent.DeleteCertLineage Agent.InstallCustomCertificate Agent.IssueLetsEncryptCertificate
		Agent.IssuePanelCertificateV2 Agent.ReconcileSiteCertLineages
		Agent.RenewLetsEncryptCertificate Agent.ValidateCertificate
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityFirewall, `
		Agent.ApplyFirewallV2 Agent.Fail2banToggleJail Agent.Fail2banUnban
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityDNS, `
		Agent.ConfigureDNSClusterV2 Agent.ConfigurePowerDNSSQLite Agent.SecureDNSZoneV2
		Agent.RecoverDNSZoneV3 Agent.SwitchDNSEngineV1 Agent.SyncDNSZoneV2 Agent.SyncDNSZoneV3
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityMail, `
		Agent.AddMailAccount Agent.ConfigureDKIMSigning Agent.ConfigureMailStack
		Agent.ConfigureMailSubmission Agent.DeleteMailAccount Agent.DeleteMailDomain
		Agent.EnsureDKIMKey Agent.ImportMailAccount Agent.PostfixQueueAction
		Agent.SetMailPolicy Agent.SyncMailTLSV2
		Agent.UpdateMailForwarding Agent.UpdateMailPassword Agent.UpdateMailQuota
		Agent.WireMailFilters
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityFilesystem, `
		Agent.ChmodFile Agent.ClearLogs Agent.CreateFileOrDir Agent.DeleteFileOrDir
		Agent.RenameFile Agent.UploadFile Agent.WriteFile
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityCron, `
		Agent.AddCronJob Agent.DeleteCronJob Agent.UpdateCronJob
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityBackup, `
		Agent.CreateBackup Agent.DeleteBackup Agent.ExtractCpmoveFiles Agent.RestoreBackup
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityRuntimeNode, `
		Agent.InstallNodeVersion Agent.RemoveNodeVersion
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityVPN, `
		Agent.GenerateVPNKeys Agent.SetupVPN Agent.SyncVPNPeersV2
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilityDatabase, `
		Agent.CreateDatabase Agent.DeleteDatabase Agent.ImportCpmoveDatabase
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilitySystemSQLite, `
		Agent.CreateSystemSQLiteSnapshot Agent.OptimizeSystemSQLiteDatabase
		Agent.ReleaseSystemSQLiteSnapshot
	`),
	agentRPCAuthGroup(agentRPCEffectHostMutation, agentRPCCapabilitySystemUpdate, `
		Agent.AbandonSystemUpdate Agent.StartSystemUpdate
	`),
}

var (
	errAgentRPCPolicyMissing               = errors.New("agent RPC policy is missing")
	errAgentRPCPolicyInvalid               = errors.New("agent RPC policy is invalid")
	errAgentRPCPlatformCapabilityDenied    = errors.New("agent RPC platform capability is denied")
	errAgentRPCPlatformIdentityUnavailable = errors.New("agent RPC platform identity is unavailable")
	errAgentRPCClientUnavailable           = errors.New("agent RPC client is unavailable")
	errAgentRPCTimeoutPolicyMissing        = errAgentRPCPolicyMissing
)

// agentRPCTimeouts is the timeout facet of the typed, fail-closed registry. A new
// privileged RPC cannot accidentally inherit an arbitrary global timeout: its
// expected work class must be reviewed and added here before the panel can call
// it through callAgent. Long package/import operations therefore do not share
// the deadline used by small status reads.
var agentRPCTimeouts = map[string]time.Duration{
	// Small local status/configuration reads.
	"Agent.AppUnitStatus":               agentRPCQuickReadTimeout,
	"Agent.CheckInstalledServices":      agentRPCQuickReadTimeout,
	"Agent.CheckSystemUpdate":           agentRPCNetworkReadTimeout,
	"Agent.DNSSECStatus":                agentRPCQuickReadTimeout,
	"Agent.DNSBackendReadiness":         agentRPCQuickReadTimeout,
	"Agent.DNSEngineRollbackEvidenceV1": agentRPCQuickReadTimeout,
	"Agent.DovecotStats":                agentRPCQuickReadTimeout,
	"Agent.Fail2banConfig":              agentRPCQuickReadTimeout,
	"Agent.Fail2banStatus":              agentRPCQuickReadTimeout,
	"Agent.FirewallStatus":              agentRPCQuickReadTimeout,
	"Agent.GetCertificateInfo":          agentRPCQuickReadTimeout,
	"Agent.GetConfig":                   agentRPCQuickReadTimeout,
	"Agent.GetDKIMStatus":               agentRPCQuickReadTimeout,
	"Agent.GetExtendedPHPConfig":        agentRPCQuickReadTimeout,
	"Agent.GetMailPolicy":               agentRPCQuickReadTimeout,
	"Agent.GetMailQuotaStatus":          agentRPCQuickReadTimeout,
	"Agent.GetMySQLConfig":              agentRPCQuickReadTimeout,
	"Agent.GetPHPConfig":                agentRPCQuickReadTimeout,
	"Agent.GetPHPConfiguration":         agentRPCQuickReadTimeout,
	"Agent.GetPHPExtensions":            agentRPCQuickReadTimeout,
	"Agent.GetPHPPoolConfig":            agentRPCQuickReadTimeout,
	"Agent.GetPHPPools":                 agentRPCQuickReadTimeout,
	"Agent.GetServices":                 agentRPCQuickReadTimeout,
	"Agent.HostPlatform":                agentRPCQuickReadTimeout,
	"Agent.InstalledServiceIDsStrict":   agentRPCQuickReadTimeout,
	"Agent.ListServiceInstances":        agentRPCQuickReadTimeout,
	"Agent.ListCronJobs":                agentRPCQuickReadTimeout,
	"Agent.NginxInspect":                agentRPCQuickReadTimeout,
	"Agent.PkgFamily":                   agentRPCQuickReadTimeout,
	"Agent.PostfixQueue":                agentRPCQuickReadTimeout,
	"Agent.RepoStatus":                  agentRPCDeploymentTimeout,
	"Agent.SiteUsage":                   agentRPCQuickReadTimeout,
	"Agent.Version":                     agentRPCQuickReadTimeout,
	"Agent.SystemUpdateStatus":          agentRPCQuickReadTimeout,
	"Agent.SecurityAudit":               agentRPCStandardReadTimeout,
	"Agent.VPNStatus":                   agentRPCQuickReadTimeout,
	"Agent.CheckSystemSQLiteDatabase":   agentRPCQuickReadTimeout,
	"Agent.InspectInstalledCertificate": agentRPCQuickReadTimeout,
	"Agent.InstalledRepoPackages":       agentRPCQuickReadTimeout,
	"Agent.InstalledServiceIDs":         agentRPCQuickReadTimeout,
	"Agent.ListNodeVersions":            agentRPCQuickReadTimeout,
	"Agent.ListSystemSQLiteDatabases":   agentRPCQuickReadTimeout,
	"Agent.ServiceMutationReadiness":    agentRPCQuickReadTimeout,

	// Bounded disk/log reads.
	"Agent.AppUnitLogs":                   agentRPCStandardReadTimeout,
	"Agent.GetAccessLogs":                 agentRPCStandardReadTimeout,
	"Agent.GetErrorLogs":                  agentRPCStandardReadTimeout,
	"Agent.GetPHPLogs":                    agentRPCStandardReadTimeout,
	"Agent.InspectCpmove":                 agentRPCStandardReadTimeout,
	"Agent.ListFiles":                     agentRPCStandardReadTimeout,
	"Agent.MailHealth":                    agentRPCStandardReadTimeout,
	"Agent.ReadFile":                      agentRPCStandardReadTimeout,
	"Agent.InspectBackup":                 agentRPCStandardReadTimeout,
	"Agent.ListBackups":                   agentRPCStandardReadTimeout,
	"Agent.ReadBackupChunk":               agentRPCStandardReadTimeout,
	"Agent.ReadSystemSQLiteSnapshotChunk": agentRPCStandardReadTimeout,
	"Agent.ServiceJournal":                agentRPCStandardReadTimeout,

	// Reads that may contact repositories or public DNS.
	"Agent.CheckRBL":                agentRPCNetworkReadTimeout,
	"Agent.DNSClusterReadiness":     agentRPCNetworkReadTimeout,
	"Agent.RepoPackages":            agentRPCNetworkReadTimeout,
	"Agent.ServiceCandidateVersion": agentRPCNetworkReadTimeout,
	"Agent.ListNodeLTS":             agentRPCNetworkReadTimeout,

	// Short, bounded host mutations.
	"Agent.BeginServiceMutation":        agentRPCMutationTimeout,
	"Agent.CancelServiceMutation":       agentRPCMutationTimeout,
	"Agent.FinishServiceMutation":       agentRPCMutationTimeout,
	"Agent.HeartbeatServiceMutation":    agentRPCMutationTimeout,
	"Agent.ServiceMutationStatus":       agentRPCMutationTimeout,
	"Agent.AddCronJob":                  agentRPCMutationTimeout,
	"Agent.AddMailAccount":              agentRPCMutationTimeout,
	"Agent.ApplyFirewallV2":             agentRPCMutationTimeout,
	"Agent.ApplyVhost":                  agentRPCMutationTimeout,
	"Agent.ChmodFile":                   agentRPCMutationTimeout,
	"Agent.ClearLogs":                   agentRPCMutationTimeout,
	"Agent.ConfigureDNSClusterV2":       agentRPCMutationTimeout,
	"Agent.ConfigureDKIMSigning":        agentRPCMutationTimeout,
	"Agent.ConfigurePowerDNSSQLite":     agentRPCMutationTimeout,
	"Agent.CreateFileOrDir":             agentRPCMutationTimeout,
	"Agent.DeleteCertLineage":           agentRPCMutationTimeout,
	"Agent.DeleteBackup":                agentRPCMutationTimeout,
	"Agent.DeleteCronJob":               agentRPCMutationTimeout,
	"Agent.DeleteFileOrDir":             agentRPCMutationTimeout,
	"Agent.DeleteMailAccount":           agentRPCMutationTimeout,
	"Agent.DeleteMailDomain":            agentRPCMutationTimeout,
	"Agent.DeletePHPPool":               agentRPCMutationTimeout,
	"Agent.Fail2banToggleJail":          agentRPCMutationTimeout,
	"Agent.Fail2banUnban":               agentRPCMutationTimeout,
	"Agent.GenerateVPNKeys":             agentRPCMutationTimeout,
	"Agent.ReleaseSystemSQLiteSnapshot": agentRPCMutationTimeout,
	"Agent.ResetFailedUnitMutation":     agentRPCMutationTimeout,
	"Agent.StartServiceMutation":        agentRPCMutationTimeout,
	"Agent.SyncVPNPeersV2":              agentRPCMutationTimeout,
	"Agent.ValidateCertificate":         agentRPCMutationTimeout,
	"Agent.PostfixQueueAction":          agentRPCMutationTimeout,
	"Agent.ReconcileSiteCertLineages":   agentRPCDeploymentTimeout,
	"Agent.RemoveAppUnit":               agentRPCMutationTimeout,
	"Agent.RenameFile":                  agentRPCMutationTimeout,
	"Agent.SetMailPolicy":               agentRPCMutationTimeout,
	"Agent.TogglePHPExtension":          agentRPCMutationTimeout,
	"Agent.UpdateConfig":                agentRPCMutationTimeout,
	"Agent.UpdateCronJob":               agentRPCMutationTimeout,
	"Agent.UpdateExtendedPHPConfig":     agentRPCMutationTimeout,
	"Agent.UpdateMailForwarding":        agentRPCMutationTimeout,
	"Agent.UpdateMailPassword":          agentRPCMutationTimeout,
	"Agent.UpdateMailQuota":             agentRPCMutationTimeout,
	"Agent.UpdateMySQLConfig":           agentRPCMutationTimeout,
	"Agent.UpdatePHPConfig":             agentRPCMutationTimeout,
	"Agent.UpdatePHPConfiguration":      agentRPCMutationTimeout,
	"Agent.UpdatePHPPoolConfig":         agentRPCMutationTimeout,
	"Agent.UploadFile":                  agentRPCMutationTimeout,
	"Agent.WriteFile":                   agentRPCMutationTimeout,

	// Repository publication/removal refreshes package metadata and can be
	// bounded by network and package-manager latency.
	"Agent.DisableRepo": agentRPCDeploymentTimeout,
	"Agent.EnableRepo":  agentRPCDeploymentTimeout,

	// Database and site lifecycle operations.
	"Agent.ApplyAppUnit":                 agentRPCDatabaseTimeout,
	"Agent.ControlAppUnit":               agentRPCDatabaseTimeout,
	"Agent.CreateDatabase":               agentRPCDatabaseTimeout,
	"Agent.CreateSite":                   agentRPCDatabaseTimeout,
	"Agent.DeleteDatabase":               agentRPCDatabaseTimeout,
	"Agent.DeleteSite":                   agentRPCDatabaseTimeout,
	"Agent.EnsureDKIMKey":                agentRPCDatabaseTimeout,
	"Agent.ImportMailAccount":            agentRPCDatabaseTimeout,
	"Agent.MigratePHPPool":               agentRPCDatabaseTimeout,
	"Agent.SecureDNSZoneV2":              agentRPCDatabaseTimeout,
	"Agent.SyncDNSZoneV2":                agentRPCDatabaseTimeout,
	"Agent.SyncDNSZoneV3":                agentRPCDatabaseTimeout,
	"Agent.RecoverDNSZoneV3":             agentRPCDatabaseTimeout,
	"Agent.SyncMailTLSV2":                agentRPCMutationTimeout,
	"Agent.CreateSystemSQLiteSnapshot":   agentRPCDatabaseTimeout,
	"Agent.OptimizeSystemSQLiteDatabase": agentRPCDatabaseTimeout,

	// Operations that legitimately run package/application installers.
	"Agent.ApplyVhosts":                 agentRPCDeploymentTimeout,
	"Agent.ConfigureDBTools":            agentRPCDeploymentTimeout,
	"Agent.ConfigureMailStack":          agentRPCDeploymentTimeout,
	"Agent.ConfigureMailSubmission":     agentRPCDeploymentTimeout,
	"Agent.ConfigureWebmail":            agentRPCDeploymentTimeout,
	"Agent.EnsureNginxReady":            agentRPCDeploymentTimeout,
	"Agent.InstallCustomCertificate":    agentRPCDeploymentTimeout,
	"Agent.InstallNodeVersion":          agentRPCDeploymentTimeout,
	"Agent.InstallRoundcube":            agentRPCDeploymentTimeout,
	"Agent.InstallService":              agentRPCDeploymentTimeout,
	"Agent.StartSystemUpdate":           agentRPCDeploymentTimeout,
	"Agent.AbandonSystemUpdate":         agentRPCMutationTimeout,
	"Agent.InstallWordPress":            agentRPCDeploymentTimeout,
	"Agent.IssueLetsEncryptCertificate": agentRPCDeploymentTimeout,
	"Agent.IssuePanelCertificateV2":     agentRPCDeploymentTimeout,
	"Agent.RemoveNodeVersion":           agentRPCDeploymentTimeout,
	"Agent.RemoveRoundcube":             agentRPCDeploymentTimeout,
	"Agent.RenewLetsEncryptCertificate": agentRPCDeploymentTimeout,
	"Agent.ServiceMutationAction":       agentRPCDeploymentTimeout,
	"Agent.SetupVPN":                    agentRPCDeploymentTimeout,
	"Agent.UninstallService":            agentRPCDeploymentTimeout,
	"Agent.WireMailFilters":             agentRPCDeploymentTimeout,
	"Agent.SwitchDNSEngineV1":           agentRPCDeploymentTimeout,

	// Large archive/database imports may process customer-sized data sets.
	"Agent.ExtractCpmoveFiles":   agentRPCBulkImportTimeout,
	"Agent.ImportCpmoveDatabase": agentRPCBulkImportTimeout,
	"Agent.CreateBackup":         agentRPCBulkImportTimeout,
	"Agent.RestoreBackup":        agentRPCBulkImportTimeout,
}

var agentRPCPolicies = buildAgentRPCPolicies()

func buildAgentRPCPolicies() map[string]agentRPCPolicy {
	authorizations := make(map[string]agentRPCPolicy)
	for _, group := range agentRPCAuthorizationGroups {
		for _, method := range group.methods {
			if _, duplicate := authorizations[method]; duplicate {
				panic("duplicate agent RPC authorization: " + method)
			}
			authorizations[method] = agentRPCPolicy{
				effect: group.effect, capability: group.capability,
			}
		}
	}
	policies := make(map[string]agentRPCPolicy, len(agentRPCTimeouts))
	for method, timeout := range agentRPCTimeouts {
		policy, ok := authorizations[method]
		if !ok {
			panic("agent RPC timeout lacks authorization: " + method)
		}
		policy.timeout = timeout
		if err := policy.validate(); err != nil {
			panic(fmt.Sprintf("invalid agent RPC policy %s: %v", method, err))
		}
		policies[method] = policy
	}
	for method := range authorizations {
		if _, ok := agentRPCTimeouts[method]; !ok {
			panic("agent RPC authorization lacks timeout: " + method)
		}
	}
	return policies
}

func agentRPCPolicyForMethod(method string) (agentRPCPolicy, error) {
	policy, ok := agentRPCPolicies[method]
	if !ok {
		return agentRPCPolicy{}, fmt.Errorf("%w: %s", errAgentRPCPolicyMissing, method)
	}
	if err := policy.validate(); err != nil {
		return agentRPCPolicy{}, fmt.Errorf("%w: %s: %v", errAgentRPCPolicyInvalid, method, err)
	}
	return policy, nil
}

func agentRPCTimeout(method string) (time.Duration, error) {
	policy, err := agentRPCPolicyForMethod(method)
	if err != nil {
		return 0, err
	}
	return policy.timeout, nil
}

type agentRPCHostIdentity struct {
	host     core.ManagedServiceHostProfile
	verified bool
}

type agentRPCHostIdentityResolution struct {
	done     chan struct{}
	identity agentRPCHostIdentity
	err      error
}

// rhelPreviewAgentRPCMethodGrants is a dormant exact-method prefilter, not an
// activation surface or a broad capability-family switch. Its empty production
// value keeps all RHEL/DNF mutations closed. An entry remains forbidden until
// parameterized RPC arguments and the durable lease kind/target/package are
// bound and the lifecycle has passed live certification.
var rhelPreviewAgentRPCMethodGrants = map[string]agentRPCCapability{}

func verifiedAgentRPCHostCapability(identity agentRPCHostIdentity) bool {
	if !identity.verified {
		return false
	}
	host := identity.host
	return validHostPlatformCapability(transport.HostPlatformResponse{
		DistroFamily:   host.DistroFamily,
		PackageManager: host.PackageFamily,
		ServiceManager: host.ServiceManager,
		DistroID:       host.DistroID,
		VersionID:      host.VersionID,
		Architecture:   host.Architecture,
	})
}

// authorizeAgentRPCPolicyForHost is the pure platform firewall. Host mutations
// require the complete HostPlatform capability identity produced by the
// agent trusted manager/systemd proof. The legacy family-only identity is
// catalogue/read compatibility data and can never authorize a mutation. DNF
// additionally requires a narrowly qualified candidate and an exact
// method/capability prefilter entry. A prefilter entry alone is deliberately
// insufficient for a parameterized method until its request and durable target
// are bound.
func authorizeAgentRPCPolicyForHost(method string, policy agentRPCPolicy, identity agentRPCHostIdentity) error {
	if err := policy.validate(); err != nil {
		return fmt.Errorf("%w: %v", errAgentRPCPolicyInvalid, err)
	}
	switch policy.effect {
	case agentRPCEffectRead, agentRPCEffectControl:
		return nil
	case agentRPCEffectHostMutation, agentRPCEffectHostRepairMutation:
		packageFamily := strings.TrimSpace(identity.host.PackageFamily)
		switch packageFamily {
		case "apt", "pacman":
			if verifiedAgentRPCHostCapability(identity) {
				return nil
			}
			return fmt.Errorf(
				"%w: package_family=%s requires a verified HostPlatform capability identity",
				errAgentRPCPlatformCapabilityDenied,
				packageFamily,
			)
		case "":
			return errAgentRPCPlatformIdentityUnavailable
		case "dnf":
			if verifiedAgentRPCHostCapability(identity) &&
				core.IsRHELPreviewNginxCandidate(identity.host) {
				grantedCapability, granted := rhelPreviewAgentRPCMethodGrants[method]
				if granted && grantedCapability == policy.capability {
					return nil
				}
			}
			return fmt.Errorf(
				"%w: package_family=%s capability=%s",
				errAgentRPCPlatformCapabilityDenied,
				packageFamily,
				policy.capability,
			)
		default:
			return fmt.Errorf(
				"%w: package_family=%s capability=%s",
				errAgentRPCPlatformCapabilityDenied,
				packageFamily,
				policy.capability,
			)
		}
	default:
		return errAgentRPCPolicyInvalid
	}
}

// authorizeAgentRPCContext performs the exact pre-dispatch decision used by
// callAgentContext. Callers that must authorize before writing local metadata
// can reuse it without dispatching the privileged RPC.
func (p *Panel) authorizeAgentRPCContext(ctx context.Context, method string) error {
	policy, err := agentRPCPolicyForMethod(method)
	if err != nil {
		return err
	}
	if policy.effect == agentRPCEffectRead || policy.effect == agentRPCEffectControl {
		return authorizeAgentRPCPolicyForHost(method, policy, agentRPCHostIdentity{})
	}
	if ctx == nil {
		ctx = context.Background()
	}
	identity, err := p.agentRPCHostIdentity(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: %s: %w", errAgentRPCPlatformIdentityUnavailable, method, err)
	}
	if err := authorizeAgentRPCPolicyForHost(method, policy, identity); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

func agentRPCMethodUnavailable(err error, method string) bool {
	var serverErr rpc.ServerError
	if !errors.As(err, &serverErr) {
		return false
	}
	return strings.TrimSpace(string(serverErr)) == "rpc: can't find method "+method
}

func (p *Panel) rawAgentCallContext(ctx context.Context, method string, args, reply any) error {
	if p == nil || p.agentClient == nil {
		return errAgentRPCClientUnavailable
	}
	return p.agentClient.CallContext(ctx, method, args, reply)
}

// agentRPCHostIdentity resolves identity through the sole raw dispatcher so
// policy evaluation cannot recurse. A shared flight coalesces the first
// HostPlatform lookup without holding a mutex across the RPC: each waiter can
// still honor its own context, while waiters that remain receive the leader's
// exact result. Any cached family is intentionally enriched through
// HostPlatform when an agent is available. Without an agent, a cached family
// that is already mutation-ineligible may still produce a deterministic
// capability denial; supported APT/pacman mutation paths require the live
// verified identity and report the missing client instead.
func (p *Panel) agentRPCHostIdentity(ctx context.Context) (agentRPCHostIdentity, error) {
	if p == nil {
		return agentRPCHostIdentity{}, errAgentRPCClientUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentRPCHostIdentity{}, err
	}

	// A fully published identity is immutable for the panel lifetime and can
	// take the fast path without consulting the flight registry.
	p.pkgFamilyMu.Lock()
	if p.hostPlatformKnown {
		response := p.hostPlatformVal
		family := strings.TrimSpace(p.pkgFamilyVal)
		p.pkgFamilyMu.Unlock()
		host, ok := managedServiceHostProfileFromResponse(response)
		if !ok {
			return agentRPCHostIdentity{}, errors.New("cached HostPlatform identity is invalid")
		}
		if family != "" && family != host.PackageFamily {
			return agentRPCHostIdentity{}, errors.New("cached HostPlatform identity conflicts with cached PkgFamily")
		}
		return agentRPCHostIdentity{host: host, verified: true}, nil
	}
	p.pkgFamilyMu.Unlock()

	p.hostPlatformResolutionMu.Lock()
	if err := ctx.Err(); err != nil {
		p.hostPlatformResolutionMu.Unlock()
		return agentRPCHostIdentity{}, err
	}
	if resolution := p.hostPlatformResolution; resolution != nil {
		p.hostPlatformResolutionMu.Unlock()
		select {
		case <-resolution.done:
			if err := ctx.Err(); err != nil {
				return agentRPCHostIdentity{}, err
			}
			return resolution.identity, resolution.err
		case <-ctx.Done():
			return agentRPCHostIdentity{}, ctx.Err()
		}
	}

	// The cache must be checked again after joining the flight registry: a
	// previous leader may have published and cleared its completed flight
	// between the fast-path read and this critical section.
	p.pkgFamilyMu.Lock()
	family := strings.TrimSpace(p.pkgFamilyVal)
	if p.hostPlatformKnown {
		response := p.hostPlatformVal
		p.pkgFamilyMu.Unlock()
		p.hostPlatformResolutionMu.Unlock()
		host, ok := managedServiceHostProfileFromResponse(response)
		if !ok {
			return agentRPCHostIdentity{}, errors.New("cached HostPlatform identity is invalid")
		}
		if family != "" && family != host.PackageFamily {
			return agentRPCHostIdentity{}, errors.New("cached HostPlatform identity conflicts with cached PkgFamily")
		}
		return agentRPCHostIdentity{host: host, verified: true}, nil
	}
	hasAgent := p.agentClient != nil
	if family != "" && !hasAgent && family != "apt" && family != "pacman" {
		p.pkgFamilyMu.Unlock()
		p.hostPlatformResolutionMu.Unlock()
		return agentRPCHostIdentity{
			host: core.ManagedServiceHostProfile{PackageFamily: family},
		}, nil
	}
	p.pkgFamilyMu.Unlock()
	if !hasAgent {
		p.hostPlatformResolutionMu.Unlock()
		return agentRPCHostIdentity{}, errAgentRPCClientUnavailable
	}

	resolution := &agentRPCHostIdentityResolution{done: make(chan struct{})}
	p.hostPlatformResolution = resolution
	p.hostPlatformResolutionMu.Unlock()

	go func(resolution *agentRPCHostIdentityResolution, family string) {
		identityCtx, cancel := context.WithTimeout(
			context.Background(),
			agentRPCQuickReadTimeout,
		)
		defer cancel()
		identity, resolveErr := func() (agentRPCHostIdentity, error) {
			var response transport.HostPlatformResponse
			err := p.rawAgentCallContext(
				identityCtx,
				"Agent.HostPlatform",
				&transport.Empty{},
				&response,
			)
			if err == nil {
				host, ok := managedServiceHostProfileFromResponse(response)
				if !ok {
					return agentRPCHostIdentity{}, errors.New("HostPlatform returned an invalid identity")
				}
				p.pkgFamilyMu.Lock()
				if p.hostPlatformKnown {
					publishedResponse := p.hostPlatformVal
					publishedFamily := strings.TrimSpace(p.pkgFamilyVal)
					p.pkgFamilyMu.Unlock()
					publishedHost, ok := managedServiceHostProfileFromResponse(publishedResponse)
					if !ok {
						return agentRPCHostIdentity{}, errors.New("published HostPlatform identity is invalid")
					}
					if publishedFamily != "" && publishedFamily != publishedHost.PackageFamily {
						return agentRPCHostIdentity{}, errors.New("published HostPlatform identity conflicts with cached PkgFamily")
					}
					return agentRPCHostIdentity{host: publishedHost, verified: true}, nil
				}
				publishedFamily := strings.TrimSpace(p.pkgFamilyVal)
				if publishedFamily != "" && publishedFamily != host.PackageFamily {
					p.pkgFamilyMu.Unlock()
					return agentRPCHostIdentity{}, errors.New("HostPlatform identity conflicts with cached PkgFamily")
				}
				p.hostPlatformVal = response
				p.hostPlatformKnown = true
				p.pkgFamilyVal = host.PackageFamily
				p.pkgFamilyMu.Unlock()
				return agentRPCHostIdentity{host: host, verified: true}, nil
			}
			if identityCtx.Err() != nil {
				return agentRPCHostIdentity{}, identityCtx.Err()
			}
			if !agentRPCMethodUnavailable(err, "Agent.HostPlatform") {
				return agentRPCHostIdentity{}, fmt.Errorf("read HostPlatform: %w", err)
			}

			// Only an agent that truly predates HostPlatform may use this
			// compatibility fallback. Family-only dnf remains blocked by the
			// platform firewall.
			if family == "" {
				p.pkgFamilyMu.Lock()
				family = strings.TrimSpace(p.pkgFamilyVal)
				p.pkgFamilyMu.Unlock()
			}
			if family == "" {
				if err := p.rawAgentCallContext(
					identityCtx,
					"Agent.PkgFamily",
					&transport.Empty{},
					&family,
				); err != nil {
					if identityCtx.Err() != nil {
						return agentRPCHostIdentity{}, identityCtx.Err()
					}
					return agentRPCHostIdentity{}, fmt.Errorf("read legacy PkgFamily: %w", err)
				}
			}
			family = strings.TrimSpace(family)
			if family == "" {
				return agentRPCHostIdentity{}, errors.New("legacy PkgFamily returned an empty identity")
			}
			p.pkgFamilyMu.Lock()
			if p.pkgFamilyVal == "" {
				p.pkgFamilyVal = family
			}
			publishedFamily := strings.TrimSpace(p.pkgFamilyVal)
			p.pkgFamilyMu.Unlock()
			if publishedFamily != family {
				return agentRPCHostIdentity{}, errors.New("legacy PkgFamily conflicts with cached PkgFamily")
			}
			return agentRPCHostIdentity{
				host: core.ManagedServiceHostProfile{PackageFamily: publishedFamily},
			}, nil
		}()

		p.hostPlatformResolutionMu.Lock()
		resolution.identity = identity
		resolution.err = resolveErr
		if p.hostPlatformResolution == resolution {
			p.hostPlatformResolution = nil
		}
		close(resolution.done)
		p.hostPlatformResolutionMu.Unlock()
	}(resolution, family)

	select {
	case <-resolution.done:
		if err := ctx.Err(); err != nil {
			return agentRPCHostIdentity{}, err
		}
		return resolution.identity, resolution.err
	case <-ctx.Done():
		return agentRPCHostIdentity{}, ctx.Err()
	}
}

// callAgent is the compatibility entry point for handlers that do not yet
// propagate an HTTP/request context. It still uses CallContext and a reviewed,
// method-specific hard deadline. New code should prefer callAgentContext.
func (p *Panel) callAgent(method string, args, reply any) error {
	return p.callAgentContext(context.Background(), method, args, reply)
}

func (p *Panel) callAgentContext(parent context.Context, method string, args, reply any) error {
	policy, err := agentRPCPolicyForMethod(method)
	if err != nil {
		return err
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, policy.timeout)
	defer cancel()
	if err := p.authorizeAgentRPCContext(ctx, method); err != nil {
		return err
	}
	return p.rawAgentCallContext(ctx, method, args, reply)
}
