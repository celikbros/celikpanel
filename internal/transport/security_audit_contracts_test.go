package transport

import (
	"strings"
	"testing"
)

func validSecurityAuditAgentFixture() SecurityAuditAgentResponse {
	return SecurityAuditAgentResponse{
		ContractVersion: SecurityAuditContractVersion,
		Capability:      AgentCapabilitySecurityAuditV1,
		BuildVersion:    "v0.1.0-alpha.16",
		BuildCommit:     strings.Repeat("a", 40),
		GeneratedAt:     "2026-08-13T12:00:00Z",
		Firewall: SecurityAuditFirewallResponse{
			Engine:       SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "firewall_engine_available"},
			DefaultDrop:  SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "firewall_policy_drop"},
			Persistence:  SecurityAuditCheck{Status: SecurityAuditStatusUnknown, Code: "firewall_persistence_unverified"},
			TCPAllowlist: []int{22, 443},
			UDPAllowlist: []int{53},
		},
		Listeners: SecurityAuditListenersResponse{
			Check:    SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "listeners_match_allowlist"},
			Findings: []SecurityAuditListenerFinding{},
		},
		SSH: SecurityAuditSSHResponse{
			Check:                             SecurityAuditCheck{Status: SecurityAuditStatusUnknown, Code: "ssh_policy_live_unverified"},
			PasswordAuthentication:            "no",
			KeyboardInteractiveAuthentication: "no",
			PermitRootLogin:                   "prohibit-password",
			PubkeyAuthentication:              "yes",
			HostbasedAuthentication:           "no",
			GSSAPIAuthentication:              "no",
		},
		Reboot: SecurityAuditRebootResponse{
			Check: SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "reboot_not_required"},
		},
		SignedUpdate: SecurityAuditSignedUpdateResponse{
			Check:          SecurityAuditCheck{Status: SecurityAuditStatusWarning, Code: "signed_update_identity_unverified"},
			Enrolled:       true,
			Sequence:       "16",
			Version:        "v0.1.0-alpha.16",
			KeyFingerprint: "sha256:" + strings.Repeat("a", 64),
		},
	}
}

func TestSecurityAuditAgentResponseAcceptsExactBoundedContract(t *testing.T) {
	response := validSecurityAuditAgentFixture()
	if err := ValidateSecurityAuditAgentResponse(response); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
}

func TestSecurityAuditAgentResponseKeepsBoundedLegacyListenerFailureCompatible(t *testing.T) {
	response := validSecurityAuditAgentFixture()
	response.Listeners = SecurityAuditListenersResponse{
		Check: SecurityAuditCheck{Status: SecurityAuditStatusFail, Code: SecurityAuditListenerNotAllowed},
		Findings: []SecurityAuditListenerFinding{{
			Protocol: "tcp",
			Port:     5355,
			Status:   SecurityAuditStatusFail,
			Code:     SecurityAuditListenerNotAllowed,
		}},
	}
	if err := ValidateSecurityAuditAgentResponse(response); err != nil {
		t.Fatalf("bounded v1 listener failure rejected during rolling compatibility: %v", err)
	}
}

func TestSecurityAuditAgentResponseRejectsFalsePassAndUnboundedFindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SecurityAuditAgentResponse)
	}{
		{
			name: "unknown ssh values cannot pass",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.SSH.PasswordAuthentication = "unknown"
			},
		},
		{
			name: "unknown reason code",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Reboot.Check = SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "invented_pass"}
			},
		},
		{
			name: "reason code belongs to another check",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Firewall.Engine = SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "panel_tls_certificate_valid"}
			},
		},
		{
			name: "phase one cannot claim firewall persistence pass",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Firewall.Persistence = SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "firewall_persistence_ready"}
			},
		},
		{
			name: "phase one cannot claim live SSH pass",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.SSH.Check = SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "ssh_key_only"}
			},
		},
		{
			name: "listener failure requires failed aggregate",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Listeners.Findings = []SecurityAuditListenerFinding{{
					Protocol: "tcp", Port: 8080,
					Status: SecurityAuditStatusFail, Code: SecurityAuditListenerNotAllowed,
				}}
			},
		},
		{
			name: "findings are bounded",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Listeners.Check = SecurityAuditCheck{Status: SecurityAuditStatusFail, Code: "listener_not_allowed"}
				response.Listeners.Findings = make([]SecurityAuditListenerFinding, SecurityAuditMaxListenerFindings+1)
			},
		},
		{
			name: "firewall ports are canonical",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Firewall.TCPAllowlist = []int{443, 22}
			},
		},
		{
			name: "unenrolled trust cannot pass",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.SignedUpdate.Enrolled = false
				response.SignedUpdate.Sequence = ""
				response.SignedUpdate.Version = ""
				response.SignedUpdate.KeyFingerprint = ""
			},
		},
		{
			name: "signed update floor must match running build",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.SignedUpdate.Version = "v0.1.0-alpha.15"
			},
		},
		{
			name: "publisher identity cannot claim pass",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.SignedUpdate.Check = SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "signed_update_trust_enrolled"}
			},
		},
		{
			name: "duplicate listener endpoint is rejected",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.Listeners.Check = SecurityAuditCheck{Status: SecurityAuditStatusFail, Code: SecurityAuditListenerNotAllowed}
				response.Listeners.Findings = []SecurityAuditListenerFinding{
					{Protocol: "tcp", Port: 8080, Status: SecurityAuditStatusWarning, Code: SecurityAuditAllowedNoListener},
					{Protocol: "tcp", Port: 8080, Status: SecurityAuditStatusFail, Code: SecurityAuditListenerNotAllowed},
				}
			},
		},
		{
			name: "enrolled trust needs canonical fingerprint",
			mutate: func(response *SecurityAuditAgentResponse) {
				response.SignedUpdate.KeyFingerprint = "sha256:not-a-fingerprint"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := validSecurityAuditAgentFixture()
			test.mutate(&response)
			if err := ValidateSecurityAuditAgentResponse(response); err == nil {
				t.Fatal("invalid response was accepted")
			}
		})
	}
}

func TestSecurityAuditTLSResponseRejectsAuthoritativeChildrenWhenCertificateUnknown(t *testing.T) {
	response := SecurityAuditTLSResponse{
		Certificate: SecurityAuditCheck{Status: SecurityAuditStatusUnknown, Code: "panel_tls_not_managed"},
		SelfSigned:  SecurityAuditCheck{Status: SecurityAuditStatusUnknown, Code: "panel_tls_unknown"},
		Expiry:      SecurityAuditCheck{Status: SecurityAuditStatusUnknown, Code: "panel_tls_unknown"},
		KeyMatch:    SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "panel_tls_key_match"},
	}
	if err := ValidateSecurityAuditTLSResponse(response); err == nil {
		t.Fatal("unknown certificate produced a passing key result")
	}
}

func TestSecurityAuditTLSResponseKeepsNonSelfSignedChainUnverified(t *testing.T) {
	response := SecurityAuditTLSResponse{
		Certificate: SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "panel_tls_certificate_valid"},
		SelfSigned:  SecurityAuditCheck{Status: SecurityAuditStatusUnknown, Code: "panel_tls_chain_unverified"},
		Expiry:      SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "panel_tls_valid"},
		KeyMatch:    SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "panel_tls_key_match"},
		ExpiresAt:   "2027-08-13T12:00:00Z",
	}
	if err := ValidateSecurityAuditTLSResponse(response); err != nil {
		t.Fatalf("unverified non-self-signed chain rejected: %v", err)
	}
	response.SelfSigned = SecurityAuditCheck{Status: SecurityAuditStatusPass, Code: "panel_tls_not_self_signed"}
	if err := ValidateSecurityAuditTLSResponse(response); err == nil {
		t.Fatal("non-self-signed certificate was treated as trusted without chain and hostname proof")
	}
}
