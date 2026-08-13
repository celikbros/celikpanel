package transport

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	SecurityAuditContractVersion     = 1
	AgentCapabilitySecurityAuditV1   = "security_audit_v1"
	SecurityAuditMaxFirewallPorts    = 4096
	SecurityAuditMaxListenerFindings = 512
	SecurityAuditStatusPass          = "pass"
	SecurityAuditStatusWarning       = "warning"
	SecurityAuditStatusFail          = "fail"
	SecurityAuditStatusUnknown       = "unknown"
	SecurityAuditListenerNotAllowed  = "listener_not_allowed"
	SecurityAuditAllowedNoListener   = "allowed_no_listener"
)

// SecurityAuditCheck carries only a closed status and a stable, localizable
// reason code. Host command output is deliberately never returned to the
// browser.
type SecurityAuditCheck struct {
	Status string `json:"status"`
	Code   string `json:"code"`
}

type SecurityAuditFirewallResponse struct {
	Engine       SecurityAuditCheck `json:"engine"`
	DefaultDrop  SecurityAuditCheck `json:"default_drop"`
	Persistence  SecurityAuditCheck `json:"persistence"`
	TCPAllowlist []int              `json:"tcp_allowlist"`
	UDPAllowlist []int              `json:"udp_allowlist"`
}

type SecurityAuditListenerFinding struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Status   string `json:"status"`
	Code     string `json:"code"`
}

type SecurityAuditListenersResponse struct {
	Check    SecurityAuditCheck             `json:"check"`
	Findings []SecurityAuditListenerFinding `json:"findings"`
}

type SecurityAuditSSHResponse struct {
	Check                             SecurityAuditCheck `json:"check"`
	PasswordAuthentication            string             `json:"password_authentication"`
	KeyboardInteractiveAuthentication string             `json:"keyboard_interactive_authentication"`
	PermitRootLogin                   string             `json:"permit_root_login"`
	PubkeyAuthentication              string             `json:"pubkey_authentication"`
	HostbasedAuthentication           string             `json:"hostbased_authentication"`
	GSSAPIAuthentication              string             `json:"gssapi_authentication"`
}

type SecurityAuditRebootResponse struct {
	Check    SecurityAuditCheck `json:"check"`
	Required bool               `json:"required"`
}

type SecurityAuditSignedUpdateResponse struct {
	Check          SecurityAuditCheck `json:"check"`
	Enrolled       bool               `json:"enrolled"`
	Sequence       string             `json:"sequence,omitempty"`
	Version        string             `json:"version,omitempty"`
	KeyFingerprint string             `json:"key_fingerprint,omitempty"`
}

// SecurityAuditAgentResponse is the exact no-input Agent.SecurityAudit reply.
// Build identity is repeated here so an agent restart between the capability
// probe and this RPC cannot silently cross a release boundary.
type SecurityAuditAgentResponse struct {
	ContractVersion int                               `json:"contract_version"`
	Capability      string                            `json:"capability"`
	BuildVersion    string                            `json:"build_version"`
	BuildCommit     string                            `json:"build_commit"`
	GeneratedAt     string                            `json:"generated_at"`
	Firewall        SecurityAuditFirewallResponse     `json:"firewall"`
	Listeners       SecurityAuditListenersResponse    `json:"listeners"`
	SSH             SecurityAuditSSHResponse          `json:"ssh"`
	Reboot          SecurityAuditRebootResponse       `json:"reboot"`
	SignedUpdate    SecurityAuditSignedUpdateResponse `json:"signed_update"`
}

type SecurityAuditTLSResponse struct {
	Certificate  SecurityAuditCheck `json:"certificate"`
	SelfSigned   SecurityAuditCheck `json:"self_signed"`
	Expiry       SecurityAuditCheck `json:"expiry"`
	KeyMatch     SecurityAuditCheck `json:"key_match"`
	IsSelfSigned bool               `json:"is_self_signed"`
	ExpiresAt    string             `json:"expires_at,omitempty"`
}

type SecurityAuditHTTPResponse struct {
	ContractVersion int                        `json:"contract_version"`
	GeneratedAt     string                     `json:"generated_at"`
	Agent           SecurityAuditAgentResponse `json:"agent"`
	TLS             SecurityAuditTLSResponse   `json:"tls"`
}

var securityAuditVersionPattern = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func validSecurityAuditCheck(check SecurityAuditCheck) bool {
	want, ok := securityAuditCodeStatuses[check.Code]
	return ok && check.Status == want
}

func validSecurityAuditCheckFor(check SecurityAuditCheck, codes ...string) bool {
	if !validSecurityAuditCheck(check) {
		return false
	}
	for _, code := range codes {
		if check.Code == code {
			return true
		}
	}
	return false
}

var securityAuditCodeStatuses = map[string]string{
	"platform_unsupported":              SecurityAuditStatusUnknown,
	"firewall_engine_available":         SecurityAuditStatusPass,
	"firewall_engine_unavailable":       SecurityAuditStatusFail,
	"firewall_state_unreadable":         SecurityAuditStatusUnknown,
	"firewall_disabled":                 SecurityAuditStatusFail,
	"firewall_policy_drop":              SecurityAuditStatusPass,
	"firewall_policy_not_drop":          SecurityAuditStatusFail,
	"firewall_policy_ambiguous":         SecurityAuditStatusUnknown,
	"firewall_persistence_missing":      SecurityAuditStatusFail,
	"firewall_persistence_stale":        SecurityAuditStatusFail,
	"firewall_persistence_invalid":      SecurityAuditStatusFail,
	"firewall_persistence_unverified":   SecurityAuditStatusUnknown,
	"listeners_match_allowlist":         SecurityAuditStatusPass,
	SecurityAuditListenerNotAllowed:     SecurityAuditStatusFail,
	SecurityAuditAllowedNoListener:      SecurityAuditStatusWarning,
	"listener_state_unreadable":         SecurityAuditStatusUnknown,
	"listener_state_ambiguous":          SecurityAuditStatusUnknown,
	"finding_limit_exceeded":            SecurityAuditStatusUnknown,
	"ssh_password_auth_enabled":         SecurityAuditStatusFail,
	"ssh_non_key_auth_enabled":          SecurityAuditStatusFail,
	"ssh_root_login_unrestricted":       SecurityAuditStatusWarning,
	"ssh_policy_live_unverified":        SecurityAuditStatusUnknown,
	"ssh_policy_unreadable":             SecurityAuditStatusUnknown,
	"ssh_policy_ambiguous":              SecurityAuditStatusUnknown,
	"reboot_not_required":               SecurityAuditStatusPass,
	"reboot_required":                   SecurityAuditStatusWarning,
	"reboot_state_unknown":              SecurityAuditStatusUnknown,
	"signed_update_identity_unverified": SecurityAuditStatusWarning,
	"signed_update_trust_not_enrolled":  SecurityAuditStatusFail,
	"signed_update_trust_unsafe":        SecurityAuditStatusFail,
	"signed_update_trust_unreadable":    SecurityAuditStatusUnknown,
	"panel_tls_certificate_valid":       SecurityAuditStatusPass,
	"panel_tls_not_managed":             SecurityAuditStatusUnknown,
	"panel_tls_incomplete":              SecurityAuditStatusFail,
	"panel_tls_unreadable":              SecurityAuditStatusUnknown,
	"panel_tls_invalid":                 SecurityAuditStatusFail,
	"panel_tls_metadata_unsafe":         SecurityAuditStatusFail,
	"panel_tls_live_unverified":         SecurityAuditStatusUnknown,
	"panel_tls_live_mismatch":           SecurityAuditStatusFail,
	"panel_tls_unknown":                 SecurityAuditStatusUnknown,
	"panel_tls_self_signed":             SecurityAuditStatusWarning,
	"panel_tls_not_self_signed":         SecurityAuditStatusPass,
	"panel_tls_chain_unverified":        SecurityAuditStatusUnknown,
	"panel_tls_valid":                   SecurityAuditStatusPass,
	"panel_tls_expiring":                SecurityAuditStatusWarning,
	"panel_tls_expired":                 SecurityAuditStatusFail,
	"panel_tls_not_yet_valid":           SecurityAuditStatusFail,
	"panel_tls_key_match":               SecurityAuditStatusPass,
	"panel_tls_key_mismatch":            SecurityAuditStatusFail,
}

func validSecurityAuditTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Format(time.RFC3339) == value
}

func validSecurityAuditCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validSecurityAuditPorts(ports []int) bool {
	if len(ports) > SecurityAuditMaxFirewallPorts {
		return false
	}
	previous := 0
	for _, port := range ports {
		if port < 1 || port > 65535 || port <= previous {
			return false
		}
		previous = port
	}
	return true
}

func validSecurityAuditSSHValue(value string, rootPolicy bool) bool {
	if rootPolicy {
		switch value {
		case "yes", "no", "prohibit-password", "without-password", "forced-commands-only", "unknown":
			return true
		}
		return false
	}
	return value == "yes" || value == "no" || value == "unknown"
}

// ValidateSecurityAuditAgentResponse makes the root-agent/browser boundary
// fail closed. Oversized, unsorted, internally inconsistent, or invented
// response shapes are rejected instead of being rendered as a successful
// audit.
func ValidateSecurityAuditAgentResponse(response SecurityAuditAgentResponse) error {
	if response.ContractVersion != SecurityAuditContractVersion ||
		response.Capability != AgentCapabilitySecurityAuditV1 ||
		!securityAuditVersionPattern.MatchString(response.BuildVersion) ||
		!validSecurityAuditCommit(response.BuildCommit) ||
		!validSecurityAuditTimestamp(response.GeneratedAt) {
		return fmt.Errorf("security audit identity is invalid")
	}
	checks := []SecurityAuditCheck{
		response.Firewall.Engine,
		response.Firewall.DefaultDrop,
		response.Firewall.Persistence,
		response.Listeners.Check,
		response.SSH.Check,
		response.Reboot.Check,
		response.SignedUpdate.Check,
	}
	for _, check := range checks {
		if !validSecurityAuditCheck(check) {
			return fmt.Errorf("security audit check is invalid")
		}
	}
	if !validSecurityAuditCheckFor(response.Firewall.Engine,
		"firewall_engine_available", "firewall_engine_unavailable", "firewall_state_unreadable", "platform_unsupported") ||
		!validSecurityAuditCheckFor(response.Firewall.DefaultDrop,
			"firewall_policy_drop", "firewall_policy_not_drop", "firewall_policy_ambiguous", "firewall_disabled", "firewall_state_unreadable", "platform_unsupported") ||
		!validSecurityAuditCheckFor(response.Firewall.Persistence,
			"firewall_persistence_missing", "firewall_persistence_stale", "firewall_persistence_invalid", "firewall_persistence_unverified", "platform_unsupported") ||
		!validSecurityAuditCheckFor(response.Listeners.Check,
			"listeners_match_allowlist", SecurityAuditListenerNotAllowed, SecurityAuditAllowedNoListener,
			"listener_state_unreadable", "listener_state_ambiguous", "finding_limit_exceeded", "platform_unsupported") ||
		!validSecurityAuditCheckFor(response.SSH.Check,
			"ssh_password_auth_enabled", "ssh_non_key_auth_enabled", "ssh_root_login_unrestricted",
			"ssh_policy_unreadable", "ssh_policy_ambiguous", "ssh_policy_live_unverified", "platform_unsupported") ||
		!validSecurityAuditCheckFor(response.Reboot.Check,
			"reboot_not_required", "reboot_required", "reboot_state_unknown", "platform_unsupported") ||
		!validSecurityAuditCheckFor(response.SignedUpdate.Check,
			"signed_update_identity_unverified", "signed_update_trust_not_enrolled", "signed_update_trust_unsafe", "signed_update_trust_unreadable", "platform_unsupported") {
		return fmt.Errorf("security audit reason belongs to another check")
	}
	if !validSecurityAuditPorts(response.Firewall.TCPAllowlist) ||
		!validSecurityAuditPorts(response.Firewall.UDPAllowlist) {
		return fmt.Errorf("security audit firewall allowlist is invalid")
	}
	if response.Firewall.DefaultDrop.Status == SecurityAuditStatusPass &&
		response.Firewall.Engine.Status != SecurityAuditStatusPass {
		return fmt.Errorf("passing firewall policy lacks a passing engine")
	}
	if response.Firewall.Persistence.Status == SecurityAuditStatusPass &&
		(response.Firewall.Engine.Status != SecurityAuditStatusPass || response.Firewall.DefaultDrop.Status != SecurityAuditStatusPass) {
		return fmt.Errorf("passing firewall persistence lacks a verified live policy")
	}
	if response.Firewall.DefaultDrop.Status != SecurityAuditStatusPass &&
		(len(response.Firewall.TCPAllowlist) != 0 || len(response.Firewall.UDPAllowlist) != 0) {
		return fmt.Errorf("unverified firewall policy exposes an authoritative allowlist")
	}
	if response.Listeners.Check.Status != SecurityAuditStatusUnknown &&
		response.Firewall.DefaultDrop.Status != SecurityAuditStatusPass {
		return fmt.Errorf("authoritative listener audit lacks a verified firewall policy")
	}
	if len(response.Listeners.Findings) > SecurityAuditMaxListenerFindings {
		return fmt.Errorf("security audit listener findings exceed the limit")
	}
	previous := ""
	previousEndpoint := ""
	hasFailure, hasWarning := false, false
	for _, finding := range response.Listeners.Findings {
		if (finding.Protocol != "tcp" && finding.Protocol != "udp") || finding.Port < 1 || finding.Port > 65535 {
			return fmt.Errorf("security audit listener finding is invalid")
		}
		endpoint := finding.Protocol + ":" + fmt.Sprintf("%05d", finding.Port)
		key := endpoint + ":" + finding.Code
		if key <= previous {
			return fmt.Errorf("security audit listener findings are not canonical")
		}
		if endpoint == previousEndpoint {
			return fmt.Errorf("security audit listener endpoint is duplicated")
		}
		previous = key
		previousEndpoint = endpoint
		switch finding.Code {
		case SecurityAuditListenerNotAllowed:
			if finding.Status != SecurityAuditStatusFail {
				return fmt.Errorf("security audit listener failure status is invalid")
			}
			hasFailure = true
		case SecurityAuditAllowedNoListener:
			if finding.Status != SecurityAuditStatusWarning {
				return fmt.Errorf("security audit listener warning status is invalid")
			}
			hasWarning = true
		default:
			return fmt.Errorf("security audit listener reason is invalid")
		}
	}
	switch response.Listeners.Check.Status {
	case SecurityAuditStatusPass:
		if len(response.Listeners.Findings) != 0 {
			return fmt.Errorf("passing listener audit contains findings")
		}
	case SecurityAuditStatusFail:
		if !hasFailure {
			return fmt.Errorf("failed listener audit lacks a failure")
		}
	case SecurityAuditStatusWarning:
		if hasFailure || !hasWarning {
			return fmt.Errorf("warning listener audit is inconsistent")
		}
	case SecurityAuditStatusUnknown:
		if len(response.Listeners.Findings) != 0 {
			return fmt.Errorf("unknown listener audit contains findings")
		}
	}
	if !validSecurityAuditSSHValue(response.SSH.PasswordAuthentication, false) ||
		!validSecurityAuditSSHValue(response.SSH.KeyboardInteractiveAuthentication, false) ||
		!validSecurityAuditSSHValue(response.SSH.PermitRootLogin, true) ||
		!validSecurityAuditSSHValue(response.SSH.PubkeyAuthentication, false) ||
		!validSecurityAuditSSHValue(response.SSH.HostbasedAuthentication, false) ||
		!validSecurityAuditSSHValue(response.SSH.GSSAPIAuthentication, false) {
		return fmt.Errorf("security audit SSH policy is invalid")
	}
	if response.SSH.Check.Status == SecurityAuditStatusPass {
		rootSafe := response.SSH.PermitRootLogin == "no" ||
			response.SSH.PermitRootLogin == "prohibit-password" ||
			response.SSH.PermitRootLogin == "without-password"
		if response.SSH.PasswordAuthentication != "no" ||
			response.SSH.KeyboardInteractiveAuthentication != "no" ||
			response.SSH.PubkeyAuthentication != "yes" ||
			response.SSH.HostbasedAuthentication != "no" ||
			response.SSH.GSSAPIAuthentication != "no" || !rootSafe {
			return fmt.Errorf("passing SSH audit is not key-only")
		}
	}
	switch response.SSH.Check.Code {
	case "ssh_password_auth_enabled":
		if response.SSH.PasswordAuthentication != "yes" && response.SSH.KeyboardInteractiveAuthentication != "yes" {
			return fmt.Errorf("SSH password finding lacks an enabled password method")
		}
	case "ssh_non_key_auth_enabled":
		if response.SSH.HostbasedAuthentication != "yes" && response.SSH.GSSAPIAuthentication != "yes" {
			return fmt.Errorf("SSH non-key finding lacks an enabled non-key method")
		}
	case "ssh_root_login_unrestricted":
		if response.SSH.PermitRootLogin != "yes" ||
			response.SSH.PasswordAuthentication != "no" ||
			response.SSH.KeyboardInteractiveAuthentication != "no" ||
			response.SSH.PubkeyAuthentication != "yes" ||
			response.SSH.HostbasedAuthentication != "no" ||
			response.SSH.GSSAPIAuthentication != "no" {
			return fmt.Errorf("SSH root-login warning lacks an unrestricted root policy")
		}
	case "ssh_policy_live_unverified":
		rootSafe := response.SSH.PermitRootLogin == "no" ||
			response.SSH.PermitRootLogin == "prohibit-password" ||
			response.SSH.PermitRootLogin == "without-password" ||
			response.SSH.PermitRootLogin == "forced-commands-only"
		if response.SSH.PasswordAuthentication != "no" ||
			response.SSH.KeyboardInteractiveAuthentication != "no" ||
			response.SSH.PubkeyAuthentication != "yes" ||
			response.SSH.HostbasedAuthentication != "no" ||
			response.SSH.GSSAPIAuthentication != "no" || !rootSafe {
			return fmt.Errorf("unverified live SSH policy is not safe on disk")
		}
	case "ssh_policy_unreadable", "platform_unsupported":
		if response.SSH.PasswordAuthentication != "unknown" ||
			response.SSH.KeyboardInteractiveAuthentication != "unknown" ||
			response.SSH.PermitRootLogin != "unknown" ||
			response.SSH.PubkeyAuthentication != "unknown" ||
			response.SSH.HostbasedAuthentication != "unknown" ||
			response.SSH.GSSAPIAuthentication != "unknown" {
			return fmt.Errorf("unreadable SSH policy exposes authoritative values")
		}
	}
	if response.Reboot.Check.Status == SecurityAuditStatusPass && response.Reboot.Required {
		return fmt.Errorf("passing reboot audit requires a reboot")
	}
	if response.Reboot.Check.Status == SecurityAuditStatusWarning && !response.Reboot.Required {
		return fmt.Errorf("reboot warning does not require a reboot")
	}
	if response.Reboot.Check.Status == SecurityAuditStatusUnknown && response.Reboot.Required {
		return fmt.Errorf("unknown reboot state requires a reboot")
	}
	if response.SignedUpdate.Enrolled {
		sequence, err := strconv.ParseUint(response.SignedUpdate.Sequence, 10, 63)
		if err != nil || sequence == 0 || strconv.FormatUint(sequence, 10) != response.SignedUpdate.Sequence ||
			!securityAuditVersionPattern.MatchString(response.SignedUpdate.Version) ||
			response.SignedUpdate.Version != response.BuildVersion ||
			response.SignedUpdate.Check.Code != "signed_update_identity_unverified" ||
			response.SignedUpdate.Check.Status != SecurityAuditStatusWarning ||
			!validSecurityAuditFingerprint(response.SignedUpdate.KeyFingerprint) {
			return fmt.Errorf("enrolled signed-update trust is invalid")
		}
	} else if response.SignedUpdate.Sequence != "" || response.SignedUpdate.Version != "" || response.SignedUpdate.KeyFingerprint != "" ||
		response.SignedUpdate.Check.Status == SecurityAuditStatusPass || response.SignedUpdate.Check.Status == SecurityAuditStatusWarning {
		return fmt.Errorf("unenrolled signed-update trust is inconsistent")
	}
	return nil
}

func validSecurityAuditFingerprint(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func ValidateSecurityAuditTLSResponse(response SecurityAuditTLSResponse) error {
	for _, check := range []SecurityAuditCheck{response.Certificate, response.SelfSigned, response.Expiry, response.KeyMatch} {
		if !validSecurityAuditCheck(check) {
			return fmt.Errorf("security audit TLS check is invalid")
		}
	}
	if !validSecurityAuditCheckFor(response.Certificate,
		"panel_tls_certificate_valid", "panel_tls_not_managed", "panel_tls_incomplete", "panel_tls_unreadable", "panel_tls_invalid", "panel_tls_metadata_unsafe", "panel_tls_live_unverified", "panel_tls_live_mismatch") ||
		!validSecurityAuditCheckFor(response.SelfSigned,
			"panel_tls_self_signed", "panel_tls_not_self_signed", "panel_tls_chain_unverified", "panel_tls_unknown") ||
		!validSecurityAuditCheckFor(response.Expiry,
			"panel_tls_valid", "panel_tls_expiring", "panel_tls_expired", "panel_tls_not_yet_valid", "panel_tls_unknown") ||
		!validSecurityAuditCheckFor(response.KeyMatch,
			"panel_tls_key_match", "panel_tls_key_mismatch", "panel_tls_unknown") {
		return fmt.Errorf("security audit TLS reason belongs to another check")
	}
	if response.Certificate.Status != SecurityAuditStatusPass {
		if response.ExpiresAt != "" || response.IsSelfSigned ||
			response.SelfSigned.Status != SecurityAuditStatusUnknown ||
			response.Expiry.Status != SecurityAuditStatusUnknown ||
			response.KeyMatch.Status != SecurityAuditStatusUnknown {
			return fmt.Errorf("unreadable TLS certificate has authoritative child results")
		}
		return nil
	}
	if !validSecurityAuditTimestamp(response.ExpiresAt) {
		return fmt.Errorf("security audit TLS expiry is invalid")
	}
	if response.IsSelfSigned {
		if response.SelfSigned.Status != SecurityAuditStatusWarning {
			return fmt.Errorf("self-signed TLS certificate is not a warning")
		}
	} else if response.SelfSigned.Code != "panel_tls_chain_unverified" ||
		response.SelfSigned.Status != SecurityAuditStatusUnknown {
		return fmt.Errorf("non-self-signed TLS certificate trust is not explicitly unverified")
	}
	if response.Expiry.Status == SecurityAuditStatusUnknown || response.KeyMatch.Status == SecurityAuditStatusUnknown {
		return fmt.Errorf("valid TLS certificate has unknown child results")
	}
	if strings.TrimSpace(response.ExpiresAt) != response.ExpiresAt {
		return fmt.Errorf("security audit TLS expiry is not canonical")
	}
	return nil
}
