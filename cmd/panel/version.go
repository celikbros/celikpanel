package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	buildVersion = "dev"
	buildCommit  = "unknown"
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
	var agent struct {
		Commit string `json:"commit"`
	}
	if err := p.agentClient.CallContext(ctx, "Agent.Version", &struct{}{}, &agent); err != nil {
		return fmt.Errorf("verify panel/agent build pair: %w", err)
	}
	agentCommit := strings.TrimSpace(agent.Commit)
	if agentCommit == "" || agentCommit != panelCommit {
		return fmt.Errorf(
			"panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before changing SSL",
			panelCommit, agentCommit,
		)
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
	w.Header().Set("Content-Type", "application/json")

	agentCommit := ""
	var av struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := p.agentClient.Call("Agent.Version", &struct{}{}, &av); err == nil {
		agentCommit = av.Commit
	}

	json.NewEncoder(w).Encode(map[string]any{
		"version":      buildVersion,
		"commit":       buildCommit,
		"agent_commit": agentCommit,
		// A mismatch is reported, never hidden: an agent from a different
		// build may not enforce what this panel believes it enforces.
		// Eşleşmezlik gizlenmez, bildirilir: farklı bir yapıdan gelen agent,
		// bu panelin uyguladığını sandığı şeyi uygulamıyor olabilir.
		"agent_matches": agentCommit != "" && agentCommit == buildCommit,
	})
}
