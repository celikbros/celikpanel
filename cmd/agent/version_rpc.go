package main

import "github.com/alicelik/celikpanel/internal/transport"

// The agent carries the same two link-time values as the panel, so a
// panel/agent version mismatch is detectable instead of silent. They are
// deployed as a pair; when the pair breaks, the side that ENFORCES a rule may
// be older than the side that believes the rule is in force.
//
// Agent, panelle aynı iki bağlama-anı değerini taşır; böylece panel/agent
// sürüm uyuşmazlığı sessiz kalmaz, saptanabilir olur. İkisi bir çift olarak
// dağıtılır; çift bozulduğunda bir kuralı UYGULAYAN taraf, kuralın yürürlükte
// olduğunu sanan taraftan eski olabilir.
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

type AgentVersionResponse = transport.AgentVersionResponse

func (a *Agent) Version(_ *transport.Empty, resp *AgentVersionResponse) error {
	resp.Version = buildVersion
	resp.Commit = buildCommit
	resp.Capabilities = []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityDNSSECSecureV2,
		transport.AgentCapabilityDNSClusterConfigureV2,
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSZoneRecoverV1,
		transport.AgentCapabilityDNSEngineSwitchV1,
		transport.AgentCapabilityMailTLSSyncV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
		transport.AgentCapabilitySystemUpdateV1,
		transport.AgentCapabilitySystemUpdateAbandonV1,
		transport.AgentCapabilitySecurityAuditV1,
	}
	return nil
}
