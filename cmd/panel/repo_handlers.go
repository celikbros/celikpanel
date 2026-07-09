package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
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
	Available bool     `json:"available"`          // service has a managed repo at all
	Enabled   bool     `json:"enabled"`            // repo currently active on this host
	ID        string   `json:"id,omitempty"`       // repo id, e.g. "pgdg"
	Name      string   `json:"name,omitempty"`     // human name
	Detail    string   `json:"detail,omitempty"`   // one-line description
	Packages  []string `json:"packages,omitempty"` // available version packages (newest first)
	Error     string   `json:"error,omitempty"`
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
		json.NewEncoder(w).Encode(p.repoInfo(svc.Repo))

	case http.MethodPost:
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
		switch req.Action {
		case "enable":
			err := p.agentClient.Call("Agent.EnableRepo", &enableRepoReq{
				RepoID:         repo.ID,
				KeyURL:         repo.KeyURL,
				SourceTemplate: repo.SourceTemplate,
			}, &st)
			if err != nil {
				writeAgentError(w, err, "repo enable")
				return
			}
		case "disable":
			err := p.agentClient.Call("Agent.DisableRepo", &enableRepoReq{RepoID: repo.ID}, &st)
			if err != nil {
				writeAgentError(w, err, "repo disable")
				return
			}
		default:
			writeClientError(w, http.StatusBadRequest, "action must be enable or disable")
			return
		}
		if st.Error != "" {
			writeClientError(w, http.StatusConflict, st.Error)
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
	info := repoInfoResp{Available: true, ID: repo.ID, Name: repo.Name, Detail: repo.Description}
	var st RepoStatusResp
	if err := p.agentClient.Call("Agent.RepoStatus", &enableRepoReq{RepoID: repo.ID}, &st); err == nil {
		info.Enabled = st.Enabled
	}
	if info.Enabled {
		var pkgs RepoPackagesResp
		if err := p.agentClient.Call("Agent.RepoPackages", &repoPackagesReq{Pattern: repo.PackagePattern}, &pkgs); err == nil {
			info.Packages = pkgs.Packages
		}
	}
	return info
}

type enableRepoReq struct {
	RepoID         string `json:"repo_id"`
	KeyURL         string `json:"key_url,omitempty"`
	SourceTemplate string `json:"source_template,omitempty"`
}

type RepoStatusResp struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source,omitempty"`
	Error   string `json:"error,omitempty"`
}

type repoPackagesReq struct {
	Pattern string `json:"pattern"`
}

type RepoPackagesResp struct {
	Packages []string `json:"packages"`
	Error    string   `json:"error,omitempty"`
}
