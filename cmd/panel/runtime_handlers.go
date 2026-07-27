package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
)

// Node runtime management endpoints (admin-only via isAdminOnlyPath).
// Node runtime yönetim uçları (isAdminOnlyPath ile yalnızca admin).

func (p *Panel) handleNodeRuntimes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		var resp struct {
			Installed     []string `json:"installed"`
			SystemVersion string   `json:"system_version"`
		}
		if err := p.agentClient.Call("Agent.ListNodeVersions", &struct{}{}, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Installed == nil {
			resp.Installed = []string{}
		}
		json.NewEncoder(w).Encode(resp)

	case http.MethodPost:
		// Listing versions is open to any signed-in user (projects pick from
		// them); installing runtimes stays an administrator action.
		// Sürümleri listelemek oturumdaki herkese açıktır (projeler onlardan
		// seçer); runtime kurmak yönetici işidir.
		if c := currentCaller(r); c == nil || c.Role != roleAdmin {
			writeClientError(w, http.StatusForbidden, "administrator access required")
			return
		}
		var req struct {
			Version string `json:"version"`
		}
		if err := decodeServiceOperationJSON(w, r, &req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req.Version = strings.TrimSpace(req.Version)
		if !nodeSemverRe.MatchString(req.Version) {
			writeClientError(w, http.StatusBadRequest, "not a valid node version")
			return
		}
		release, busy := p.beginServiceMutation(w, r)
		if busy {
			return
		}
		releaseInHandler := true
		defer func() {
			if releaseInHandler {
				release()
			}
		}()
		actor := captureServiceOperationActor(r)
		op, err := p.createServiceOperation(
			r.Context(), serviceOperationKindRuntimeInstall, "node", req.Version, actor,
		)
		if errors.Is(err, errServiceOperationBusy) {
			writeServiceOperationBusy(w)
			return
		}
		if err != nil {
			writeServerError(w, err)
			return
		}
		// Downloads can take a while; the agent verifies the official
		// checksum before anything is unpacked.
		// İndirme sürebilir; agent açmadan önce resmi sağlamayı doğrular.
		p.launchServiceOperation(
			op, actor, "installing",
			"runtime.node.install:"+req.Version,
			"runtime.node.install.failed:"+req.Version,
			release,
			func(ctx context.Context, advance func(string) error) (serviceOperationResult, *serviceOperationFailure) {
				return p.runNodeInstall(ctx, req.Version, advance)
			},
		)
		releaseInHandler = false
		writeAcceptedServiceOperation(w, op)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNodeRuntimeSub serves the sub-paths under /api/v1/runtimes/node/:
//
//	GET    /api/v1/runtimes/node/lts        → upstream LTS list (fills the
//	       drawer's install buttons — the free-text semver box is gone, B3d)
//	DELETE /api/v1/runtimes/node/{version}  → remove ONE managed version,
//	       refused with RUNTIME_IN_USE while any site runs on it
//
// handleNodeRuntimeSub, /api/v1/runtimes/node/ altındaki alt yolları sunar:
// lts → kaynak LTS listesi (çekmecenin kurulum düğmelerini doldurur — serbest
// semver kutusu gitti, B3d); DELETE {version} → yönetilen TEK sürümü kaldırır,
// bir site üstünde koşarken RUNTIME_IN_USE ile reddedilir.
func (p *Panel) handleNodeRuntimeSub(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/runtimes/node/")

	if rest == "lts" && r.Method == http.MethodGet {
		var resp struct {
			Releases []struct {
				Version string `json:"version"`
				Name    string `json:"name"`
			} `json:"releases"`
			Error string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ListNodeLTS", &struct{}{}, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Error != "" {
			// The upstream index being unreachable is a fact worth showing,
			// not an internal error: the UI offers retry.
			// Kaynak dizine ulaşılamaması gösterilmeye değer bir gerçektir,
			// iç hata değil: arayüz yeniden dene sunar.
			writeClientError(w, http.StatusBadGateway, "could not fetch the Node.js release list")
			return
		}
		if resp.Releases == nil {
			resp.Releases = []struct {
				Version string `json:"version"`
				Name    string `json:"name"`
			}{}
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodDelete {
		// Same split as install: anyone signed-in may look, only the admin
		// may change the machine.
		// Kurulumla aynı ayrım: bakan herkes olabilir, makineyi yalnız
		// yönetici değiştirir.
		if c := currentCaller(r); c == nil || c.Role != roleAdmin {
			writeClientError(w, http.StatusForbidden, "administrator access required")
			return
		}
		release, busy := p.beginServiceMutation(w, r)
		if busy {
			return
		}
		defer release()
		version := rest
		if !nodeSemverRe.MatchString(version) {
			writeClientError(w, http.StatusBadRequest, "not a valid node version")
			return
		}
		count, blockers, err := runtimeVersionBlockers(r.Context(), p.db.GetDB(), "node", version)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if count > 0 {
			writeCodedErrorDetails(w, http.StatusConflict, errCodeRuntimeInUse,
				fmt.Sprintf("%d site(s) run on Node.js %s — switch them to another version first.", count, version),
				"", blockers)
			return
		}
		var resp struct {
			Removed bool   `json:"removed"`
			Error   string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.RemoveNodeVersion", &struct {
			Version string `json:"version"`
		}{Version: version}, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		// Keep the components page truthful without a manual rescan.
		// Bileşenler sayfası elle taramasız da doğru kalsın.
		if _, err := p.scanManagedServices(r.Context()); err != nil {
			log.Printf("rescan after node version removal: %v", err)
		}
		p.audit(r, "runtime.node.remove:"+version, "service", 0)
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// nodeSemverRe mirrors the agent's own gate (runtime_rpc.go nodeVersionRe):
// full semver, nothing else reaches an RPC argument.
// nodeSemverRe, agent'ın kendi kapısının aynasıdır: tam semver; başka hiçbir
// şey RPC argümanına ulaşmaz.
var nodeSemverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
