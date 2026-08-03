package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Managed vendor repositories, panel side. A distro pins one major version of a
// database/runtime; the vendor's own repo (PGDG for PostgreSQL) carries every
// current major, so enabling it unlocks version choice at install time.
//
// The allowlist lives here: the panel resolves the repo from the catalog by
// service ID and passes only those declared values to the agent. The UI never
// sends a URL, so it can never point the agent at an arbitrary repository.
// Admin-only, and every enable/disable is audited — turning on a repo widens
// the trust boundary and should be traceable.
//
// Yönetilen vendor depoları, panel tarafı. Dağıtım bir veritabanı/çalışma
// zamanının tek major'unu sabitler; vendor'ın kendi deposu (PostgreSQL için
// PGDG) tüm güncel major'ları taşır, böylece açmak kurulumda sürüm seçimini açar.
//
// İzin listesi burada: panel depoyu katalogdan servis ID'siyle çözer ve agent'a
// yalnız bu tanımlı değerleri geçirir. UI asla URL göndermez; agent'ı asla keyfi
// bir depoya yönlendiremez. Yalnız admin ve her aç/kapat audit'lenir.

type repoInfoResp struct {
	Available  bool     `json:"available"`            // service has a managed repo at all
	Enabled    bool     `json:"enabled"`              // repo currently active on this host
	Repairable bool     `json:"repairable,omitempty"` // drift can be repaired by idempotent re-enable
	ID         string   `json:"id,omitempty"`         // repo id, e.g. "pgdg"
	Name       string   `json:"name,omitempty"`       // human name
	Detail     string   `json:"detail,omitempty"`     // one-line description
	Required   bool     `json:"required,omitempty"`   // without this repo the package does not exist here
	Packages   []string `json:"packages,omitempty"`   // available version packages (newest first)
	ErrorCode  string   `json:"error_code,omitempty"`
}

// Repository failures cross two trust boundaries: agent -> panel and panel ->
// browser. Only this allowlist may cross the second boundary. Raw diagnostics
// may contain commands, paths or package-manager output, so they stay in logs.
//
// Depo hatalari iki guven sinirini gecer: agent -> panel ve panel -> tarayici.
// Ikinci siniri yalnizca bu izin listesindeki sabit kodlar gecebilir. Ham tani
// komut, yol veya paket yoneticisi ciktisi icerebildigi icin gunlukte kalir.
const (
	errCodeRepoStatusUnavailable    = "REPO_STATUS_UNAVAILABLE"
	errCodeRepoPackagesUnavailable  = "REPO_PACKAGES_UNAVAILABLE"
	errCodeRepoAgentUnavailable     = "REPO_AGENT_UNAVAILABLE"
	errCodeRepoInvalidRequest       = "REPO_INVALID_REQUEST"
	errCodeRepoUnsupportedSystem    = "REPO_UNSUPPORTED_SYSTEM"
	errCodeRepoUnsupportedDistro    = "REPO_UNSUPPORTED_DISTRIBUTION"
	errCodeRepoKeyUntrusted         = "REPO_KEY_UNTRUSTED"
	errCodeRepoEnableFailed         = "REPO_ENABLE_FAILED"
	errCodeRepoDisableFailed        = "REPO_DISABLE_FAILED"
	errCodeRepoConfigurationInvalid = "REPO_CONFIGURATION_INVALID"
	errCodeRepoStatusFailed         = "REPO_STATUS_FAILED"
	errCodeRepoPackagesFailed       = "REPO_PACKAGES_FAILED"
)

func normalizeRepoErrorCode(code, fallback string) string {
	trimmed := strings.TrimSpace(code)
	switch trimmed {
	case errCodeRepoStatusUnavailable,
		errCodeRepoPackagesUnavailable,
		errCodeRepoAgentUnavailable,
		errCodeRepoInvalidRequest,
		errCodeRepoUnsupportedSystem,
		errCodeRepoUnsupportedDistro,
		errCodeRepoKeyUntrusted,
		errCodeRepoEnableFailed,
		errCodeRepoDisableFailed,
		errCodeRepoConfigurationInvalid,
		errCodeRepoStatusFailed,
		errCodeRepoPackagesFailed:
		return trimmed
	default:
		return fallback
	}
}

func safeRepoErrorMessage(code string) string {
	switch code {
	case errCodeRepoStatusUnavailable, errCodeRepoStatusFailed:
		return "repository status could not be verified"
	case errCodeRepoPackagesUnavailable, errCodeRepoPackagesFailed:
		return "repository package list could not be loaded"
	case errCodeRepoUnsupportedSystem, errCodeRepoUnsupportedDistro:
		return "this repository is not supported on this system"
	case errCodeRepoKeyUntrusted:
		return "repository signing key could not be verified"
	case errCodeRepoDisableFailed:
		return "repository could not be disabled"
	case errCodeRepoConfigurationInvalid:
		return "repository configuration needs repair"
	case errCodeRepoInvalidRequest:
		return "invalid repository request"
	case errCodeRepoAgentUnavailable:
		return "repository service is temporarily unavailable"
	default:
		return "repository could not be enabled"
	}
}

// handleRepo: GET ?service_id= reports the managed repo for a service and, when
// enabled, the version packages available; POST {service_id, action} enables or
// disables it. Admin-only.
// handleRepo: GET ?service_id= bir servisin yönetilen deposunu ve etkinse mevcut
// sürüm paketlerini bildirir; POST {service_id, action} açar ya da kapatır.
func (p *Panel) handleRepo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		svc := core.GetManagedServiceByID(r.URL.Query().Get("service_id"))
		if svc == nil || svc.Repo == nil {
			json.NewEncoder(w).Encode(repoInfoResp{Available: false})
			return
		}
		// Managed vendor repositories are apt-only today. On Arch the same
		// component may come from the native pacman repository, so presenting
		// an apt-only required-repo button would falsely block its install.
		// Yonetilen vendor depolari simdilik yalniz apt icindir. Arch'ta ayni
		// bilesen yerel pacman deposundan gelebilir; apt-only zorunlu-depo
		// dugmesi gostermek kurulumu yanlislikla engellerdi.
		if p.packageFamily() != "apt" {
			json.NewEncoder(w).Encode(repoInfoResp{Available: false})
			return
		}
		json.NewEncoder(w).Encode(p.repoInfo(svc.Repo))

	case http.MethodPost:
		release, busy := p.beginServiceMutation(w, r)
		if busy {
			return
		}
		defer release()
		var req struct {
			ServiceID string `json:"service_id"`
			Action    string `json:"action"` // "enable" | "disable"
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		svc := core.GetManagedServiceByID(req.ServiceID)
		if svc == nil || svc.Repo == nil {
			writeClientError(w, http.StatusBadRequest, "service has no managed repository")
			return
		}
		repo := svc.Repo

		var st RepoStatusResp
		method := ""
		switch req.Action {
		case "enable":
			// Only durable mutation identity plus the repository id travel: the
			// agent looks the URL and source line up in its OWN catalogue.
			// Yalnız kalıcı değişiklik kimliği ile depo id'si yolculuk eder: URL'i
			// ve kaynak satırını agent KENDİ kataloğunda arar.
			method = "Agent.EnableRepo"
		case "disable":
			method = "Agent.DisableRepo"
		default:
			writeClientError(w, http.StatusBadRequest, "action must be enable or disable")
			return
		}
		err := p.withStandaloneAgentMutation(r.Context(), "repo_"+req.Action, repo.ID, "", func(callCtx context.Context, binding agentMutationBinding) error {
			request := &enableRepoReq{
				ServiceMutationBinding: binding,
				RepoID:                 repo.ID,
			}
			if err := p.callAgentContext(callCtx, method, request, &st); err != nil {
				return err
			}
			if st.Error != "" {
				return errors.New(st.Error)
			}
			return nil
		})
		if err != nil && st.Error == "" {
			log.Printf("[repo][%s][%s][transport] %v", repo.ID, req.Action, err)
			p.audit(r, "repo."+req.Action+".failed:"+repo.ID+" — "+errCodeRepoAgentUnavailable, "repo", 0)
			writeCodedError(w, http.StatusBadGateway, errCodeRepoAgentUnavailable, safeRepoErrorMessage(errCodeRepoAgentUnavailable), "/services")
			return
		}
		if st.Error != "" {
			fallback := errCodeRepoEnableFailed
			if req.Action == "disable" {
				fallback = errCodeRepoDisableFailed
			}
			code := normalizeRepoErrorCode(st.ErrorCode, fallback)
			log.Printf("[repo][%s][%s][agent][%s] %s", repo.ID, req.Action, code, st.Error)
			partial := st.PartialSuccess || st.MutationApplied
			// A rollback failure is not an ordinary refusal: disk state changed and
			// the audit/API contract must tell the operator that repair is required.
			// Rollback başarısızlığı sıradan bir ret değildir: disk durumu değişmiştir
			// ve audit/API sözleşmesi operatöre onarım gerektiğini söylemelidir.
			if partial {
				p.audit(r, "repo."+req.Action+".partial:"+repo.ID+" — "+code, "repo", 0)
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(apiErrorBody{
					Error:           safeRepoErrorMessage(code),
					Code:            code,
					Action:          "/services",
					PartialSuccess:  true,
					MutationApplied: true,
				})
				return
			}
			p.audit(r, "repo."+req.Action+".failed:"+repo.ID+" — "+code, "repo", 0)
			writeCodedError(w, http.StatusConflict, code, safeRepoErrorMessage(code), "/services")
			return
		}
		p.audit(r, "repo."+req.Action+":"+repo.ID, "repo", 0)
		json.NewEncoder(w).Encode(p.repoInfo(repo))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// repoInfo asks the agent for the repo's current state and, when it is enabled,
// the versioned packages available right now.
// repoInfo, agent'tan deponun mevcut durumunu ve etkinse şu an mevcut sürümlü
// paketleri ister.
func (p *Panel) repoInfo(repo *core.ManagedRepo) repoInfoResp {
	info := repoInfoResp{Available: true, ID: repo.ID, Name: repo.Name, Detail: repo.Description, Required: repo.Required}
	var st RepoStatusResp
	if err := p.callAgent("Agent.RepoStatus", &enableRepoReq{RepoID: repo.ID}, &st); err != nil {
		log.Printf("[repo][%s][status][transport] %v", repo.ID, err)
		info.ErrorCode = errCodeRepoStatusUnavailable
		return info
	}
	info.Enabled = st.Enabled
	info.Repairable = st.Repairable
	if st.Error != "" {
		fallback := errCodeRepoStatusFailed
		if st.Repairable {
			fallback = errCodeRepoConfigurationInvalid
		}
		info.ErrorCode = normalizeRepoErrorCode(st.ErrorCode, fallback)
		log.Printf("[repo][%s][status][agent][%s] %s", repo.ID, info.ErrorCode, st.Error)
	}
	// A repo without PackagePattern (for example Netdata) offers no version
	// menu, so never ask the agent to enumerate it with an empty search.
	// PackagePattern olmayan depo (örneğin Netdata) sürüm menüsü sunmaz; bu
	// yüzden agent'tan boş aramayla paket listelemesini asla isteme.
	if info.Enabled && strings.TrimSpace(repo.PackagePattern) != "" {
		var pkgs RepoPackagesResp
		if err := p.callAgent("Agent.RepoPackages", &repoPackagesReq{RepoID: repo.ID}, &pkgs); err != nil {
			log.Printf("[repo][%s][packages][transport] %v", repo.ID, err)
			info.ErrorCode = errCodeRepoPackagesUnavailable
			return info
		}
		info.Packages = pkgs.Packages
		if pkgs.Error != "" {
			info.ErrorCode = normalizeRepoErrorCode(pkgs.ErrorCode, errCodeRepoPackagesFailed)
			log.Printf("[repo][%s][packages][agent][%s] %s", repo.ID, info.ErrorCode, pkgs.Error)
		}
	}
	return info
}

// enableRepoReq carries an ID and nothing else. The key URL and source line
// deliberately do NOT travel over the RPC any more — the agent reads them from
// its own compiled catalogue, so a compromised panel cannot choose what apt
// trusts. Do not add fields here.
// enableRepoReq yalnız bir ID taşır. Anahtar URL'i ve kaynak satırı artık
// bilerek RPC üzerinden GİTMEZ — agent onları kendi derlenmiş kataloğundan
// okur; böylece ele geçirilmiş bir panel apt'ın neye güveneceğini seçemez.
// Buraya alan eklemeyin.
type enableRepoReq = transport.EnableRepoRequest
type RepoStatusResp = transport.RepoStatusResponse
type repoPackagesReq = transport.RepoPackagesRequest
type RepoPackagesResp = transport.RepoPackagesResponse
