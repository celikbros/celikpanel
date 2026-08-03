package main

import (
	"context"
	"errors"
	"fmt"
	"time"
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

var errAgentRPCTimeoutPolicyMissing = errors.New("agent RPC timeout policy is missing")

// agentRPCTimeoutPolicies is intentionally explicit and fail-closed. A new
// privileged RPC cannot accidentally inherit an arbitrary global timeout: its
// expected work class must be reviewed and added here before the panel can call
// it through callAgent. Long package/import operations therefore do not share
// the deadline used by small status reads.
var agentRPCTimeoutPolicies = map[string]time.Duration{
	// Small local status/configuration reads.
	"Agent.AppUnitStatus":             agentRPCQuickReadTimeout,
	"Agent.CheckInstalledServices":    agentRPCQuickReadTimeout,
	"Agent.DNSSECStatus":              agentRPCQuickReadTimeout,
	"Agent.ComputeTLSA":               agentRPCQuickReadTimeout,
	"Agent.DovecotStats":              agentRPCQuickReadTimeout,
	"Agent.Fail2banConfig":            agentRPCQuickReadTimeout,
	"Agent.Fail2banStatus":            agentRPCQuickReadTimeout,
	"Agent.FirewallStatus":            agentRPCQuickReadTimeout,
	"Agent.GetCertificateInfo":        agentRPCQuickReadTimeout,
	"Agent.GetConfig":                 agentRPCQuickReadTimeout,
	"Agent.GetDKIMStatus":             agentRPCQuickReadTimeout,
	"Agent.GetExtendedPHPConfig":      agentRPCQuickReadTimeout,
	"Agent.GetMailPolicy":             agentRPCQuickReadTimeout,
	"Agent.GetMailQuotaStatus":        agentRPCQuickReadTimeout,
	"Agent.GetMySQLConfig":            agentRPCQuickReadTimeout,
	"Agent.GetPHPConfig":              agentRPCQuickReadTimeout,
	"Agent.GetPHPConfiguration":       agentRPCQuickReadTimeout,
	"Agent.GetPHPExtensions":          agentRPCQuickReadTimeout,
	"Agent.GetPHPPoolConfig":          agentRPCQuickReadTimeout,
	"Agent.GetPHPPools":               agentRPCQuickReadTimeout,
	"Agent.GetServices":               agentRPCQuickReadTimeout,
	"Agent.InstalledServiceIDsStrict": agentRPCQuickReadTimeout,
	"Agent.ListServiceInstances":      agentRPCQuickReadTimeout,
	"Agent.ListCronJobs":              agentRPCQuickReadTimeout,
	"Agent.NginxInspect":              agentRPCQuickReadTimeout,
	"Agent.PkgFamily":                 agentRPCQuickReadTimeout,
	"Agent.PostfixQueue":              agentRPCQuickReadTimeout,
	"Agent.RepoStatus":                agentRPCQuickReadTimeout,
	"Agent.SiteUsage":                 agentRPCQuickReadTimeout,
	"Agent.Version":                   agentRPCQuickReadTimeout,
	"Agent.VPNStatus":                 agentRPCQuickReadTimeout,

	// Bounded disk/log reads.
	"Agent.AppUnitLogs":   agentRPCStandardReadTimeout,
	"Agent.GetAccessLogs": agentRPCStandardReadTimeout,
	"Agent.GetErrorLogs":  agentRPCStandardReadTimeout,
	"Agent.GetPHPLogs":    agentRPCStandardReadTimeout,
	"Agent.InspectCpmove": agentRPCStandardReadTimeout,
	"Agent.ListFiles":     agentRPCStandardReadTimeout,
	"Agent.MailHealth":    agentRPCStandardReadTimeout,
	"Agent.ReadFile":      agentRPCStandardReadTimeout,

	// Reads that may contact repositories or public DNS.
	"Agent.CheckRBL":                agentRPCNetworkReadTimeout,
	"Agent.DNSClusterReadiness":     agentRPCNetworkReadTimeout,
	"Agent.RepoPackages":            agentRPCNetworkReadTimeout,
	"Agent.ServiceCandidateVersion": agentRPCNetworkReadTimeout,

	// Short, bounded host mutations.
	"Agent.AddCronJob":                agentRPCMutationTimeout,
	"Agent.AddMailAccount":            agentRPCMutationTimeout,
	"Agent.ApplyFirewall":             agentRPCMutationTimeout,
	"Agent.ChmodFile":                 agentRPCMutationTimeout,
	"Agent.ClearLogs":                 agentRPCMutationTimeout,
	"Agent.ConfigureDNSCluster":       agentRPCMutationTimeout,
	"Agent.ConfigurePowerDNSSQLite":   agentRPCMutationTimeout,
	"Agent.CreateFileOrDir":           agentRPCMutationTimeout,
	"Agent.DeleteCertLineage":         agentRPCMutationTimeout,
	"Agent.DeleteCronJob":             agentRPCMutationTimeout,
	"Agent.DeleteFileOrDir":           agentRPCMutationTimeout,
	"Agent.DeleteMailAccount":         agentRPCMutationTimeout,
	"Agent.DeletePHPPool":             agentRPCMutationTimeout,
	"Agent.Fail2banToggleJail":        agentRPCMutationTimeout,
	"Agent.Fail2banUnban":             agentRPCMutationTimeout,
	"Agent.GenerateVPNKeys":           agentRPCMutationTimeout,
	"Agent.PostfixQueueAction":        agentRPCMutationTimeout,
	"Agent.ReconcileSiteCertLineages": agentRPCDeploymentTimeout,
	"Agent.RemoveAppUnit":             agentRPCMutationTimeout,
	"Agent.RenameFile":                agentRPCMutationTimeout,
	"Agent.SetMailPolicy":             agentRPCMutationTimeout,
	"Agent.TogglePHPExtension":        agentRPCMutationTimeout,
	"Agent.UpdateConfig":              agentRPCMutationTimeout,
	"Agent.UpdateCronJob":             agentRPCMutationTimeout,
	"Agent.UpdateExtendedPHPConfig":   agentRPCMutationTimeout,
	"Agent.UpdateMailForwarding":      agentRPCMutationTimeout,
	"Agent.UpdateMailQuota":           agentRPCMutationTimeout,
	"Agent.UpdateMySQLConfig":         agentRPCMutationTimeout,
	"Agent.UpdatePHPConfig":           agentRPCMutationTimeout,
	"Agent.UpdatePHPConfiguration":    agentRPCMutationTimeout,
	"Agent.UpdatePHPPoolConfig":       agentRPCMutationTimeout,
	"Agent.UploadFile":                agentRPCMutationTimeout,
	"Agent.WriteFile":                 agentRPCMutationTimeout,

	// Repository publication/removal refreshes package metadata and can be
	// bounded by network and package-manager latency.
	"Agent.DisableRepo": agentRPCDeploymentTimeout,
	"Agent.EnableRepo":  agentRPCDeploymentTimeout,

	// Database and site lifecycle operations.
	"Agent.ApplyAppUnit":      agentRPCDatabaseTimeout,
	"Agent.ControlAppUnit":    agentRPCDatabaseTimeout,
	"Agent.CreateDatabase":    agentRPCDatabaseTimeout,
	"Agent.DeleteDatabase":    agentRPCDatabaseTimeout,
	"Agent.DeleteSite":        agentRPCDatabaseTimeout,
	"Agent.EnsureDKIMKey":     agentRPCDatabaseTimeout,
	"Agent.ImportMailAccount": agentRPCDatabaseTimeout,
	"Agent.MigratePHPPool":    agentRPCDatabaseTimeout,
	"Agent.SecureDNSZone":     agentRPCDatabaseTimeout,
	"Agent.SecureMailTLS":     agentRPCDatabaseTimeout,
	"Agent.SyncDNSZone":       agentRPCDatabaseTimeout,

	// Operations that legitimately run package/application installers.
	"Agent.InstallWordPress": agentRPCDeploymentTimeout,

	// Large archive/database imports may process customer-sized data sets.
	"Agent.ExtractCpmoveFiles":   agentRPCBulkImportTimeout,
	"Agent.ImportCpmoveDatabase": agentRPCBulkImportTimeout,
}

func agentRPCTimeout(method string) (time.Duration, error) {
	timeout, ok := agentRPCTimeoutPolicies[method]
	if !ok || timeout <= 0 {
		return 0, fmt.Errorf("%w: %s", errAgentRPCTimeoutPolicyMissing, method)
	}
	return timeout, nil
}

// callAgent is the compatibility entry point for handlers that do not yet
// propagate an HTTP/request context. It still uses CallContext and a reviewed,
// method-specific hard deadline. New code should prefer callAgentContext.
func (p *Panel) callAgent(method string, args, reply any) error {
	return p.callAgentContext(context.Background(), method, args, reply)
}

func (p *Panel) callAgentContext(parent context.Context, method string, args, reply any) error {
	timeout, err := agentRPCTimeout(method)
	if err != nil {
		return err
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return p.agentClient.CallContext(ctx, method, args, reply)
}
