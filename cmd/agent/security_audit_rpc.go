package main

import (
	"fmt"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func securityAuditCheck(status, code string) transport.SecurityAuditCheck {
	return transport.SecurityAuditCheck{Status: status, Code: code}
}

func unknownSecurityAuditResponse(code string) transport.SecurityAuditAgentResponse {
	unknown := securityAuditCheck(transport.SecurityAuditStatusUnknown, code)
	return transport.SecurityAuditAgentResponse{
		Firewall: transport.SecurityAuditFirewallResponse{
			Engine: unknown, DefaultDrop: unknown, Persistence: unknown,
			TCPAllowlist: []int{}, UDPAllowlist: []int{},
		},
		Listeners: transport.SecurityAuditListenersResponse{
			Check: unknown, Findings: []transport.SecurityAuditListenerFinding{},
		},
		SSH: transport.SecurityAuditSSHResponse{
			Check:                             unknown,
			PasswordAuthentication:            "unknown",
			KeyboardInteractiveAuthentication: "unknown",
			PermitRootLogin:                   "unknown",
			PubkeyAuthentication:              "unknown",
			HostbasedAuthentication:           "unknown",
			GSSAPIAuthentication:              "unknown",
		},
		Reboot:       transport.SecurityAuditRebootResponse{Check: unknown},
		SignedUpdate: transport.SecurityAuditSignedUpdateResponse{Check: unknown},
	}
}

// SecurityAudit is deliberately a no-input, read-only RPC. Every command and
// filesystem path used by the platform collector is fixed in the agent build;
// the panel cannot supply a command, argument, path, URL, or environment value.
func (a *Agent) SecurityAudit(_ *transport.Empty, reply *transport.SecurityAuditAgentResponse) error {
	if reply == nil {
		return fmt.Errorf("security audit response is required")
	}
	now := time.Now().UTC().Truncate(time.Second)
	response := collectHostSecurityAudit(now)
	response.ContractVersion = transport.SecurityAuditContractVersion
	response.Capability = transport.AgentCapabilitySecurityAuditV1
	response.BuildVersion = buildVersion
	response.BuildCommit = buildCommit
	response.GeneratedAt = now.Format(time.RFC3339)
	*reply = response
	return nil
}
