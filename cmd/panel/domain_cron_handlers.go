package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Cron Jobs API Handlers

func (p *Panel) handleDomainCronJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract domain ID from URL
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain name for username
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		http.Error(w, "Failed to get domains", http.StatusInternalServerError)
		return
	}

	var domain *core.Domain
	for i := range domains {
		if domains[i].ID == domainID {
			domain = domains[i]
			break
		}
	}

	if domain == nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// The tenant identity travels, not a username. Cron runs as the site's own
	// system user — the identity that owns the site files, so a job can read and
	// write them (Plesk/cPanel do the same) — but WHICH user that is must be
	// re-derived and proven by the agent, not asserted here. SiteUsername is not
	// injective, so a username alone cannot say which tenant it means.
	// Kullanıcı adı değil kiracı kimliği yolculuk eder. Cron, sitenin kendi
	// sistem kullanıcısı olarak çalışır — site dosyalarının sahibi olan kimlik,
	// böylece bir iş onları okuyup yazabilir — ama bunun HANGİ kullanıcı olduğu
	// burada iddia edilmez, agent tarafından yeniden türetilip kanıtlanır.
	// SiteUsername tek yönlü değildir; tek başına bir kullanıcı adı hangi
	// kiracıyı kastettiğini söyleyemez.
	tenant := transport.CronTenant{
		SubscriptionID: domain.SubscriptionID,
		DomainID:       domain.ID,
		Domain:         domain.Name,
	}

	switch r.Method {
	case "GET":
		p.handleListCronJobs(w, tenant)
	case "POST":
		p.handleAddCronJob(w, r, tenant)
	case "PUT":
		p.handleUpdateCronJob(w, r, tenant)
	case "DELETE":
		p.handleDeleteCronJob(w, r, tenant)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListCronJobs(w http.ResponseWriter, tenant transport.CronTenant) {
	var resp transport.ListCronJobsResponse

	err := p.callAgent("Agent.ListCronJobs", &transport.ListCronJobsRequest{CronTenant: tenant}, &resp)
	if err != nil {
		writeCronAgentError(w, err, cronReasonRead)
		return
	}
	// The list and the version of the crontab it was read from travel
	// together; every change carries that version back.
	// Liste ile okunduğu crontab'ın sürümü birlikte gider.
	if resp.Jobs == nil {
		resp.Jobs = []transport.CronJob{}
	}

	json.NewEncoder(w).Encode(resp)
}

// requireCronVersion refuses a change that does not say which crontab it was
// built from, before the Agent is asked. The Agent refuses it too.
// requireCronVersion, hangi crontab'dan kurulduğunu söylemeyen değişikliği
// Agent'a sormadan reddeder. Agent da reddeder.
func requireCronVersion(w http.ResponseWriter, version string) bool {
	if version == "" {
		writeSettingsVersionRequired(w, settingsResourceScheduledTasks)
		return false
	}
	return true
}

func (p *Panel) handleAddCronJob(w http.ResponseWriter, r *http.Request, tenant transport.CronTenant) {
	var req struct {
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Comment  string `json:"comment,omitempty"`
		Version  string `json:"version"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !requireCronVersion(w, req.Version) {
		return
	}

	var success bool
	err := p.callAgentContext(r.Context(), "Agent.AddCronJob", &transport.AddCronJobRequest{
		CronTenant: tenant,
		Schedule:   req.Schedule,
		Command:    req.Command,
		Comment:    req.Comment,
		Version:    req.Version,
	}, &success)

	if err != nil {
		writeCronAgentError(w, err, cronReasonWrite)
		return
	}
	if !success {
		writeAgentError(w, nil, "cron job creation was not completed")
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleUpdateCronJob(w http.ResponseWriter, r *http.Request, tenant transport.CronTenant) {
	var req struct {
		ID       string `json:"id"`
		Schedule string `json:"schedule"`
		Command  string `json:"command"`
		Enabled  bool   `json:"enabled"`
		Comment  string `json:"comment,omitempty"`
		Version  string `json:"version"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !requireCronVersion(w, req.Version) {
		return
	}

	var success bool
	err := p.callAgentContext(r.Context(), "Agent.UpdateCronJob", &transport.UpdateCronJobRequest{
		CronTenant: tenant,
		ID:         req.ID,
		Schedule:   req.Schedule,
		Command:    req.Command,
		Enabled:    req.Enabled,
		Comment:    req.Comment,
		Version:    req.Version,
	}, &success)

	if err != nil {
		writeCronAgentError(w, err, cronReasonWrite)
		return
	}
	if !success {
		writeAgentError(w, nil, "cron job update was not completed")
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleDeleteCronJob(w http.ResponseWriter, r *http.Request, tenant transport.CronTenant) {
	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}
	version := r.URL.Query().Get("version")
	if !requireCronVersion(w, version) {
		return
	}

	var success bool
	err := p.callAgentContext(r.Context(), "Agent.DeleteCronJob", &transport.DeleteCronJobRequest{
		CronTenant: tenant,
		ID:         jobID,
		Version:    version,
	}, &success)

	if err != nil {
		writeCronAgentError(w, err, cronReasonWrite)
		return
	}
	if !success {
		writeAgentError(w, nil, "cron job deletion was not completed")
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
