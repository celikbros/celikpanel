package main

import (
	"errors"
	"testing"
	"time"
)

func TestAgentRPCTimeoutPoliciesAreExplicitAndClassified(t *testing.T) {
	methods := []string{
		"Agent.AddCronJob", "Agent.AddMailAccount", "Agent.ApplyAppUnit",
		"Agent.ApplyFirewall", "Agent.AppUnitLogs", "Agent.AppUnitStatus",
		"Agent.CheckInstalledServices", "Agent.CheckRBL", "Agent.ChmodFile",
		"Agent.ClearLogs", "Agent.ComputeTLSA", "Agent.ConfigureDNSCluster",
		"Agent.ConfigurePowerDNSSQLite", "Agent.ControlAppUnit",
		"Agent.CreateDatabase", "Agent.CreateFileOrDir", "Agent.DeleteCertLineage",
		"Agent.DeleteCronJob", "Agent.DeleteDatabase", "Agent.DeleteFileOrDir",
		"Agent.DeleteMailAccount", "Agent.DeleteMailDomain", "Agent.DeletePHPPool", "Agent.DeleteSite",
		"Agent.DisableRepo",
		"Agent.DNSClusterReadiness", "Agent.DNSSECStatus", "Agent.DovecotStats",
		"Agent.EnableRepo", "Agent.EnsureDKIMKey", "Agent.ExtractCpmoveFiles", "Agent.Fail2banConfig",
		"Agent.Fail2banStatus", "Agent.Fail2banToggleJail", "Agent.Fail2banUnban",
		"Agent.FirewallStatus", "Agent.GenerateVPNKeys", "Agent.GetAccessLogs",
		"Agent.GetCertificateInfo", "Agent.GetConfig", "Agent.GetDKIMStatus",
		"Agent.GetErrorLogs", "Agent.GetExtendedPHPConfig", "Agent.GetMailPolicy",
		"Agent.GetMailQuotaStatus", "Agent.GetMySQLConfig", "Agent.GetPHPConfig",
		"Agent.GetPHPConfiguration", "Agent.GetPHPExtensions", "Agent.GetPHPLogs",
		"Agent.GetPHPPoolConfig", "Agent.GetPHPPools", "Agent.GetServices",
		"Agent.HostPlatform",
		"Agent.ImportCpmoveDatabase", "Agent.ImportMailAccount", "Agent.InspectCpmove",
		"Agent.InstalledServiceIDsStrict", "Agent.InstallWordPress", "Agent.ListCronJobs",
		"Agent.ListServiceInstances",
		"Agent.ListFiles", "Agent.MailHealth", "Agent.MigratePHPPool",
		"Agent.NginxInspect", "Agent.PkgFamily", "Agent.PostfixQueue",
		"Agent.PostfixQueueAction", "Agent.ReadFile", "Agent.RemoveAppUnit",
		"Agent.ReconcileMailTLSMutation", "Agent.ReconcileSiteCertLineages", "Agent.RenameFile", "Agent.RepoPackages", "Agent.RepoStatus",
		"Agent.SecureDNSZone", "Agent.SecureMailTLS", "Agent.ServiceCandidateVersion", "Agent.SetMailPolicy",
		"Agent.SiteUsage", "Agent.SyncDNSZone", "Agent.TogglePHPExtension", "Agent.UpdateConfig",
		"Agent.UpdateCronJob", "Agent.UpdateExtendedPHPConfig", "Agent.UpdateMailForwarding",
		"Agent.UpdateMailPassword", "Agent.UpdateMailQuota", "Agent.UpdateMySQLConfig", "Agent.UpdatePHPConfig",
		"Agent.UpdatePHPConfiguration", "Agent.UpdatePHPPoolConfig", "Agent.UploadFile",
		"Agent.Version", "Agent.VPNStatus", "Agent.WriteFile",
	}

	classes := map[time.Duration]bool{}
	for _, method := range methods {
		timeout, err := agentRPCTimeout(method)
		if err != nil {
			t.Errorf("%s: %v", method, err)
			continue
		}
		classes[timeout] = true
	}
	if len(classes) < 5 {
		t.Fatalf("timeout policy collapsed into %d work classes; want at least 5", len(classes))
	}
	if len(agentRPCTimeoutPolicies) != len(methods) {
		t.Fatalf("policy table has %d methods, inventory has %d", len(agentRPCTimeoutPolicies), len(methods))
	}

	if _, err := agentRPCTimeout("Agent.UnreviewedFutureMethod"); !errors.Is(err, errAgentRPCTimeoutPolicyMissing) {
		t.Fatalf("unreviewed method error = %v, want fail-closed policy error", err)
	}
}
