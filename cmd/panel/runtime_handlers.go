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
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Node runtime management endpoints (admin-only via isAdminOnlyPath).
// Node runtime yönetim uçları (isAdminOnlyPath ile yalnızca admin).

func (p *Panel) handleNodeRuntimes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		var resp transport.NodeVersionsResponse
		callCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := p.callAgentContext(callCtx, "Agent.ListNodeVersions", &transport.Empty{}, &resp); err != nil {
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
			Version   string `json:"version"`
			RequestID string `json:"request_id"`
		}
		if err := decodeServiceOperationJSON(w, r, &req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if !validServiceOperationID(req.RequestID) {
			writeClientError(w, http.StatusBadRequest, "invalid request_id")
			return
		}
		req.Version = strings.TrimSpace(req.Version)
		if !nodeSemverRe.MatchString(req.Version) {
			writeClientError(w, http.StatusBadRequest, "not a valid node version")
			return
		}
		existing, found, err := p.idempotentServiceOperation(
			r.Context(), req.RequestID, serviceOperationKindRuntimeInstall, "node", req.Version,
		)
		if err != nil {
			if errors.Is(err, errServiceOperationRequestConflict) {
				writeServiceOperationRequestConflict(w)
				return
			}
			writeServerError(w, err)
			return
		}
		if found {
			writeAcceptedServiceOperation(w, existing)
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
		op, err := p.createServiceOperationRequest(
			r.Context(), serviceOperationKindRuntimeInstall, "node", req.Version, req.RequestID, actor,
		)
		if errors.Is(err, errServiceOperationBusy) {
			writeServiceOperationBusy(w)
			return
		}
		if errors.Is(err, errServiceOperationReplay) {
			writeAcceptedServiceOperation(w, op)
			return
		}
		if errors.Is(err, errServiceOperationRequestConflict) {
			writeServiceOperationRequestConflict(w)
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
		var resp transport.NodeLTSResponse
		callCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if err := p.callAgentContext(callCtx, "Agent.ListNodeLTS", &transport.Empty{}, &resp); err != nil {
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
			resp.Releases = []transport.NodeLTSRelease{}
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
		var resp transport.NodeRemoveResponse
		err = p.withStandaloneAgentMutation(r.Context(), "runtime_remove", "node:"+version, "", func(callCtx context.Context, binding agentMutationBinding) error {
			if err := p.callAgentContext(callCtx, "Agent.RemoveNodeVersion", &transport.NodeRemoveRequest{
				ServiceMutationBinding: transport.ServiceMutationBinding{
					MutationRequestID: binding.MutationRequestID,
					MutationOwnerID:   binding.MutationOwnerID,
				},
				Version: version,
			}, &resp); err != nil {
				return err
			}
			if resp.Error != "" {
				return errors.New(resp.Error)
			}
			return nil
		})
		if err != nil && resp.Error == "" {
			writeServerError(w, err)
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		// The runtime tree is already removed at this point. Preserve the
		// successful host mutation even if its mandatory state refresh fails.
		// Runtime ağacı bu noktada kaldırılmıştır. Zorunlu durum tazelemesi
		// başarısız olsa bile başarılı makine değişikliğini kaybetme.
		p.audit(r, "runtime.node.remove:"+version, "service", 0)
		// Keep the components page truthful without a manual rescan.
		// Bileşenler sayfası elle taramasız da doğru kalsın.
		if _, err := p.scanManagedServices(r.Context()); err != nil {
			log.Printf("rescan after node version removal: %v", err)
			p.audit(r, "runtime.node.remove.refresh.failed:"+version+" — "+auditReason(err.Error()), "service", 0)
			writeServiceStateRefreshFailed(w)
			return
		}
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
