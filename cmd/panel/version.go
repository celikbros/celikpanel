package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The one version. Both values are set at link time (-X main.buildVersion=…,
// -X main.buildCommit=…) by the Makefile and install.sh, from the git
// description of the commit being built.
//
// Before this, the footer showed a hand-typed "v0.1.0" that no build could
// change, next to a commit stamp baked into the FRONTEND bundle only. So an
// upgrade that replaced the backend left the footer identical, while the UI
// told the operator that stamp is how you see a new build landed. The one
// signal they were told to trust was blind to exactly the deploys that carry
// security fixes.
//
// Tek sürüm. İki değer de bağlama anında (-X main.buildVersion=…,
// -X main.buildCommit=…) Makefile ve install.sh tarafından, derlenen commit'in
// git tanımından ayarlanır.
//
// Bundan önce footer, hiçbir derlemenin değiştiremediği elle yazılmış bir
// "v0.1.0" gösteriyordu; yanındaki commit damgası ise yalnız ÖN YÜZ paketine
// gömülüydü. Yani arka ucu değiştiren bir yükseltme footer'ı aynı bırakıyordu,
// üstelik arayüz operatöre yeni yapının indiğini o damgadan anlayacağını
// söylüyordu. Güvenmesi istenen tek işaret, tam da güvenlik düzeltmesi taşıyan
// dağıtımlara kördü.
var (
	buildVersion                      = "dev"
	buildCommit                       = "unknown"
	errDNSZoneSyncV2AgentIncompatible = errors.New(
		"DNS Zone Sync V2 agent is permanently incompatible with this panel",
	)
	errDNSZoneSyncV3AgentIncompatible = errors.New(
		"DNS Zone Sync V3 agent is permanently incompatible with this panel",
	)
)

// requireMatchingAgentBuild fails closed for privileged mutations whenever
// the production panel and agent were not built from the same commit. Dev and
// test binaries keep "unknown", where there is no meaningful release identity
// to compare.
func (p *Panel) requireMatchingAgentBuild(ctx context.Context) error {
	panelCommit := strings.TrimSpace(buildCommit)
	if panelCommit == "" || panelCommit == "unknown" {
		return nil
	}
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return fmt.Errorf("verify panel/agent build pair: %w", err)
	}
	agentCommit := strings.TrimSpace(agent.Commit)
	if agentCommit == "" || agentCommit != panelCommit {
		return fmt.Errorf(
			"panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before privileged changes",
			panelCommit, agentCommit,
		)
	}
	return nil
}

func (p *Panel) requirePanelCertificateSagaAgentCapabilities(ctx context.Context) error {
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return fmt.Errorf("verify panel certificate saga agent capabilities: %w", err)
	}
	want := []string{
		transport.AgentCapabilityFirewallApplyV2,
		transport.AgentCapabilityPanelCertificateIssueV2,
	}
	if err := requireKnownAgentCapabilities(agent.Capabilities, want...); err != nil {
		return fmt.Errorf(
			"panel certificate saga agent capabilities are missing, duplicate, unknown, or noncanonical; finish the paired upgrade: %w",
			err,
		)
	}
	return nil
}

func requireKnownAgentCapabilities(capabilities []string, required ...string) error {
	known := map[string]struct{}{
		transport.AgentCapabilityFirewallApplyV2:         {},
		transport.AgentCapabilityPanelCertificateIssueV2: {},
		transport.AgentCapabilityDNSZoneSyncV2:           {},
		transport.AgentCapabilityDNSSECSecureV2:          {},
		transport.AgentCapabilityDNSClusterConfigureV2:   {},
		transport.AgentCapabilityDNSZoneSyncV3:           {},
		transport.AgentCapabilityDNSZoneRecoverV1:        {},
		transport.AgentCapabilityDNSEngineSwitchV1:       {},
		transport.AgentCapabilityMailTLSSyncV2:           {},
		transport.AgentCapabilitySystemUpdateV1:          {},
		transport.AgentCapabilitySecurityAuditV1:         {},
	}
	seen := make(map[string]struct{}, len(capabilities))
	for _, raw := range capabilities {
		capability := strings.TrimSpace(raw)
		if capability == "" || capability != raw {
			return fmt.Errorf("agent capability is not canonical")
		}
		if _, ok := known[capability]; !ok {
			return fmt.Errorf("agent capability %q is unknown", capability)
		}
		if _, duplicate := seen[capability]; duplicate {
			return fmt.Errorf("agent capability %q is duplicated", capability)
		}
		seen[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := seen[capability]; !ok {
			return fmt.Errorf("agent capability %q is missing", capability)
		}
	}
	return nil
}

// requireMailTLSSyncV2Agent gates the complete snapshot before any database
// read, durable child identity, host lease, or Mail TLS mutation. Even dev
// builds must advertise the exact V2 surface; production also requires the
// paired build commit.
func (p *Panel) requireMailTLSSyncV2Agent(ctx context.Context) error {
	if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncMailTLSV2"); err != nil {
		return fmt.Errorf("authorize Mail TLS V2 host mutation before snapshot preparation: %w", err)
	}
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return fmt.Errorf("verify Mail TLS V2 agent capability: %w", err)
	}
	panelCommit := strings.TrimSpace(buildCommit)
	if panelCommit != "" && panelCommit != "unknown" {
		agentCommit := strings.TrimSpace(agent.Commit)
		if agentCommit == "" || agentCommit != panelCommit {
			return fmt.Errorf(
				"panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before Mail TLS publication",
				panelCommit,
				agentCommit,
			)
		}
	}
	if err := requireKnownAgentCapabilities(
		agent.Capabilities,
		transport.AgentCapabilityMailTLSSyncV2,
	); err != nil {
		return fmt.Errorf("Mail TLS V2 requires the paired agent capability: %w", err)
	}
	return nil
}

// requireDNSZoneSyncV2Agent gates every direct publication before SOA/state
// preparation or lease persistence. Production requires the exact paired
// build; every build, including development, must advertise the V2 method.
func (p *Panel) requireDNSZoneSyncV2Agent(ctx context.Context) error {
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return fmt.Errorf("verify DNS V2 agent capability: %w", err)
	}
	panelCommit := strings.TrimSpace(buildCommit)
	if panelCommit != "" && panelCommit != "unknown" {
		agentCommit := strings.TrimSpace(agent.Commit)
		if agentCommit == "" || agentCommit != panelCommit {
			return fmt.Errorf(
				"%w: panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before DNS publication",
				errDNSZoneSyncV2AgentIncompatible,
				panelCommit,
				agentCommit,
			)
		}
	}
	if err := requireKnownAgentCapabilities(
		agent.Capabilities,
		transport.AgentCapabilityDNSZoneSyncV2,
	); err != nil {
		return fmt.Errorf(
			"%w: DNS V2 requires the paired agent capability: %v",
			errDNSZoneSyncV2AgentIncompatible, err,
		)
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncDNSZoneV2"); err != nil {
		return fmt.Errorf("authorize DNS V2 host mutation before snapshot preparation: %w", err)
	}
	return nil
}

// requireDNSZoneSyncV3Agent verifies the exact paired engine-bound mutation
// before the panel advances SOA, snapshots a zone or persists a V3 lease.
func (p *Panel) requireDNSZoneSyncV3Agent(ctx context.Context) error {
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return fmt.Errorf("verify DNS V3 agent capability: %w", err)
	}
	panelCommit := strings.TrimSpace(buildCommit)
	if panelCommit != "" && panelCommit != "unknown" {
		agentCommit := strings.TrimSpace(agent.Commit)
		if agentCommit == "" || agentCommit != panelCommit {
			return fmt.Errorf(
				"%w: panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before DNS publication",
				errDNSZoneSyncV3AgentIncompatible,
				panelCommit,
				agentCommit,
			)
		}
	}
	if err := requireKnownAgentCapabilities(
		agent.Capabilities,
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSZoneRecoverV1,
	); err != nil {
		return fmt.Errorf(
			"%w: DNS V3 requires the paired agent capability: %v",
			errDNSZoneSyncV3AgentIncompatible, err,
		)
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.SyncDNSZoneV3"); err != nil {
		return fmt.Errorf("authorize DNS V3 host mutation before snapshot preparation: %w", err)
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.RecoverDNSZoneV3"); err != nil {
		return fmt.Errorf("authorize DNS V3 recovery before snapshot preparation: %w", err)
	}
	return nil
}

func (p *Panel) requireDNSEngineSwitchV1Agent(ctx context.Context) error {
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(
		ctx, "Agent.Version", &transport.Empty{}, &agent,
	); err != nil {
		return fmt.Errorf("verify DNS engine switch agent capability: %w", err)
	}
	panelCommit := strings.TrimSpace(buildCommit)
	if panelCommit != "" && panelCommit != "unknown" {
		agentCommit := strings.TrimSpace(agent.Commit)
		if agentCommit == "" || agentCommit != panelCommit {
			return fmt.Errorf(
				"panel/agent build mismatch; finish the paired upgrade before DNS engine switching",
			)
		}
	}
	if err := requireKnownAgentCapabilities(
		agent.Capabilities,
		transport.AgentCapabilityDNSZoneSyncV3,
		transport.AgentCapabilityDNSZoneRecoverV1,
		transport.AgentCapabilityDNSEngineSwitchV1,
	); err != nil {
		return fmt.Errorf("DNS engine switching requires the paired agent capability: %w", err)
	}
	if err := p.authorizeAgentRPCContext(
		ctx, "Agent.SwitchDNSEngineV1",
	); err != nil {
		return fmt.Errorf("authorize DNS engine host mutation before snapshot persistence: %w", err)
	}
	return nil
}

func (p *Panel) requireDNSSECSecureV2Agent(ctx context.Context) error {
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(
		ctx, "Agent.Version", &transport.Empty{}, &agent,
	); err != nil {
		return fmt.Errorf("verify DNSSEC V2 agent capability: %w", err)
	}
	panelCommit := strings.TrimSpace(buildCommit)
	if panelCommit != "" && panelCommit != "unknown" {
		agentCommit := strings.TrimSpace(agent.Commit)
		if agentCommit == "" || agentCommit != panelCommit {
			return fmt.Errorf(
				"%w: panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before DNSSEC signing",
				errDNSZoneSyncV2AgentIncompatible, panelCommit, agentCommit,
			)
		}
	}
	if err := requireKnownAgentCapabilities(
		agent.Capabilities,
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityDNSSECSecureV2,
	); err != nil {
		return fmt.Errorf(
			"%w: DNSSEC V2 requires the paired agent capabilities: %v",
			errDNSZoneSyncV2AgentIncompatible, err,
		)
	}
	if err := p.authorizeAgentRPCContext(
		ctx, "Agent.SecureDNSZoneV2",
	); err != nil {
		return fmt.Errorf("authorize DNSSEC V2 host mutation: %w", err)
	}
	if err := p.authorizeAgentRPCContext(
		ctx, "Agent.SyncDNSZoneV2",
	); err != nil {
		return fmt.Errorf("authorize DNSSEC V2 publication: %w", err)
	}
	return nil
}

// requireDNSClusterConfigureV2Agent gates the desired topology ledger and
// both host-side phases before any mutation is prepared.
func (p *Panel) requireDNSClusterConfigureV2Agent(ctx context.Context) error {
	if err := p.requireDNSZoneSyncV2Agent(ctx); err != nil {
		return fmt.Errorf("verify DNS cluster V2 publication capability: %w", err)
	}
	var agent transport.AgentVersionResponse
	if err := p.callAgentContext(ctx, "Agent.Version", &transport.Empty{}, &agent); err != nil {
		return fmt.Errorf("verify DNS cluster V2 agent capability: %w", err)
	}
	if err := requireKnownAgentCapabilities(
		agent.Capabilities,
		transport.AgentCapabilityDNSZoneSyncV2,
		transport.AgentCapabilityDNSClusterConfigureV2,
	); err != nil {
		return fmt.Errorf("DNS cluster V2 requires the paired agent capability: %w", err)
	}
	if err := p.authorizeAgentRPCContext(ctx, "Agent.ConfigureDNSClusterV2"); err != nil {
		return fmt.Errorf("authorize DNS cluster V2 host mutation: %w", err)
	}
	return nil
}

// handleVersion reports the panel's version and BOTH commits — the panel's own
// and the agent's. They are deployed together and must match; when they do not,
// the enforcing side and the requesting side can silently disagree about what
// is allowed, which is the worst possible blind spot after a privilege fix.
// The UI shows a warning on mismatch instead of a reassuring stamp.
//
// handleVersion, panelin sürümünü ve HER İKİ commit'i bildirir — panelin kendi
// commit'i ve agent'ınki. Birlikte dağıtılırlar ve eşleşmeleri gerekir;
// eşleşmediklerinde uygulayan taraf ile isteyen taraf neye izin verildiği
// konusunda sessizce ayrışabilir — bir yetki düzeltmesinden sonraki en kötü
// kör nokta budur. Arayüz, eşleşmezlikte güven veren bir damga yerine uyarı
// gösterir.
func (p *Panel) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(`Cache-Control`, `no-store`)
	w.Header().Set(`Pragma`, `no-cache`)
	w.Header().Set("Content-Type", "application/json")

	if p.db == nil {
		writeServerError(w, fmt.Errorf("read schema version: database is unavailable"))
		return
	}
	var schemaVersion sql.NullInt64
	if err := p.db.GetDB().QueryRowContext(
		r.Context(),
		`SELECT MAX(version) FROM schema_migrations`,
	).Scan(&schemaVersion); err != nil {
		writeServerError(w, fmt.Errorf("read schema version: %w", err))
		return
	}
	if !schemaVersion.Valid {
		writeServerError(w, fmt.Errorf("read schema version: no applied migrations"))
		return
	}

	agentCommit := ""
	var av transport.AgentVersionResponse
	if err := p.callAgent("Agent.Version", &transport.Empty{}, &av); err == nil {
		agentCommit = av.Commit
	}

	json.NewEncoder(w).Encode(map[string]any{
		"version":        buildVersion,
		"commit":         buildCommit,
		"schema_version": schemaVersion.Int64,
		"agent_commit":   agentCommit,
		// A mismatch is reported, never hidden: an agent from a different
		// build may not enforce what this panel believes it enforces.
		// Eşleşmezlik gizlenmez, bildirilir: farklı bir yapıdan gelen agent,
		// bu panelin uyguladığını sandığı şeyi uygulamıyor olabilir.
		"agent_matches": agentCommit != "" && agentCommit == buildCommit,
	})
}
