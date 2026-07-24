package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/alicelik/celikpanel/internal/core"
)

// handleServiceInstall installs a managed service on demand (admin-only via
// isAdminOnlyPath). The panel installs nothing at setup; the admin adds the
// services they actually want, and the agent installs exactly the whitelisted
// packages for this host's distro.
//
// handleServiceInstall, yönetilen bir servisi talep üzerine kurar
// (isAdminOnlyPath ile yalnız admin). Panel kurulumda hiçbir şey kurmaz;
// yönetici gerçekten istediği servisleri ekler ve agent bu makinenin
// dağıtımı için whitelist'teki paketleri kurar.
func (p *Panel) handleServiceInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServiceID string `json:"service_id"`
		Package   string `json:"package,omitempty"` // optional version pick from a managed repo
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServiceID == "" {
		writeClientError(w, http.StatusBadRequest, "service_id is required")
		return
	}

	// Roundcube is not a distro package (D-004: one path on every Linux) — it
	// installs from its own verified tarball via a dedicated RPC, then the
	// loopback webmail server is (re)generated. Handled here and returned so
	// the package path below never sees it.
	// Roundcube dağıtım paketi değildir (D-004: her Linux'ta tek yol) — kendi
	// doğrulanmış tarball'ından adanmış bir RPC ile kurulur, sonra loopback
	// webmail sunucusu yeniden üretilir. Burada ele alınıp döndürülür; aşağıdaki
	// paket yolu onu hiç görmez.
	if req.ServiceID == "roundcube" {
		var rcResp struct {
			Installed bool   `json:"installed"`
			Version   string `json:"version"`
			Error     string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.InstallRoundcube", &struct{}{}, &rcResp); err != nil {
			writeServerError(w, err)
			return
		}
		if rcResp.Error != "" {
			writeClientError(w, http.StatusConflict, rcResp.Error)
			return
		}
		var wmResp struct {
			Configured bool   `json:"configured"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureWebmail", &struct{}{}, &wmResp); err != nil || wmResp.Error != "" {
			log.Printf("webmail configure after install: %v %s", err, wmResp.Error)
		}
		if _, err := p.scanManagedServices(r.Context()); err != nil {
			log.Printf("rescan after roundcube install: %v", err)
		}
		p.audit(r, "service.install:roundcube", "service", 0)
		json.NewEncoder(w).Encode(map[string]any{"installed": rcResp.Installed, "detail": "Roundcube " + rcResp.Version, "success": true})
		return
	}

	// Package installs can take a while (apt fetches + configures); the
	// agent runs it synchronously and reports the real outcome.
	// Paket kurulumları sürebilir (apt indirir + yapılandırır); agent bunu
	// senkron çalıştırır ve gerçek sonucu bildirir.
	var resp struct {
		Installed bool   `json:"installed"`
		Detail    string `json:"detail,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.InstallService", &struct {
		ID      string `json:"id"`
		Package string `json:"package,omitempty"`
	}{ID: req.ServiceID, Package: req.Package}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}

	// Installing the DNS server is only half the job: point it at our
	// dedicated database and push the existing zones so it answers
	// immediately.
	// DNS sunucusunu kurmak işin yarısıdır: onu bize ayrılmış veritabanına
	// yönlendir ve hemen cevap versin diye mevcut zone'ları it.
	if req.ServiceID == "pdns" {
		var dnsResp struct {
			Synced bool   `json:"synced"`
			Error  string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigurePowerDNSSQLite", &struct{}{}, &dnsResp); err != nil || dnsResp.Error != "" {
			log.Printf("pdns configure after install: %v %s", err, dnsResp.Error)
		} else {
			p.syncAllZones(r.Context())
		}
	}

	// Installing the mail stack is likewise only half done until postfix and
	// dovecot are wired to the panel's virtual mailboxes.
	// Mail yığınını kurmak da, postfix ve dovecot panelin sanal posta
	// kutularına bağlanana dek yarım kalır.
	if req.ServiceID == "postfix" || req.ServiceID == "dovecot" {
		var mailResp struct {
			Configured bool   `json:"configured"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureMailStack", &struct{}{}, &mailResp); err != nil || mailResp.Error != "" {
			log.Printf("mail stack configure after install: %v %s", err, mailResp.Error)
		}
	}

	// Database web tools are files, not daemons: after (un)install the agent
	// must (re)generate the loopback nginx server that actually serves them.
	// Veritabanı web araçları daemon değil dosyadır: kur/kaldır sonrası agent,
	// onları fiilen sunan yalnız-loopback nginx sunucusunu yeniden üretmeli.
	if req.ServiceID == "phpmyadmin" || req.ServiceID == "phppgadmin" {
		var dbtResp struct {
			Configured bool   `json:"configured"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureDBTools", &struct{}{}, &dbtResp); err != nil || dbtResp.Error != "" {
			log.Printf("db tools configure after install: %v %s", err, dbtResp.Error)
		}
	}

	// (Roundcube's install is handled earlier and returns before this point.)
	// (Roundcube kurulumu yukarıda ele alınır ve buraya gelmeden döner.)

	// A new service exists now; refresh the cached scan so every page keeps
	// reading from cache instead of probing.
	// Artık yeni bir servis var; önbellekteki taramayı tazele ki sayfalar
	// yoklama yapmak yerine önbellekten okumaya devam etsin.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("service scan after install %s: %v", req.ServiceID, err)
	}

	// New service may expose new ports; if the firewall is on, open them.
	// Yeni servis yeni port açabilir; güvenlik duvarı açıksa onları aç.
	p.syncFirewall()
	p.audit(r, "service.install:"+req.ServiceID, "service", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "installed": resp.Installed, "detail": resp.Detail})
}

// handleServiceCandidate returns the version apt would install for a service
// (admin-only), so the install modal can show "what will land" honestly.
// handleServiceCandidate, apt'ın bir servis için kuracağı sürümü döndürür
// (yalnız admin); kurulum modalı "ne inecek"i dürüstçe gösterebilsin diye.
func (p *Panel) handleServiceCandidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	if id == "" {
		writeClientError(w, http.StatusBadRequest, "id is required")
		return
	}
	var version string
	_ = p.agentClient.Call("Agent.ServiceCandidateVersion",
		&struct {
			ID string `json:"id"`
		}{ID: id}, &version)
	json.NewEncoder(w).Encode(map[string]string{"version": version})
}

// handleServiceUninstall removes a managed service on demand (admin-only via
// isAdminOnlyPath) — the mirror of install, for shrinking the attack surface.
// Every installed service is exploitable code; taking one back off is a
// first-class action, not a manual SSH chore.
// handleServiceUninstall, yönetilen bir servisi talep üzerine kaldırır
// (isAdminOnlyPath ile yalnız admin) — kurulumun aynası, saldırı yüzeyini
// küçültmek için.
func (p *Panel) handleServiceUninstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ServiceID string `json:"service_id"`
		// Package: remove ONE version of a runtime (php8.3-fpm) instead of
		// the whole component — the drawer's per-version Kaldır (B3d).
		// Package: bileşenin bütünü yerine bir runtime'ın TEK sürümünü
		// kaldır (php8.3-fpm) — çekmecenin sürüm başına Kaldır'ı (B3d).
		Package string `json:"package,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServiceID == "" {
		writeClientError(w, http.StatusBadRequest, "service_id is required")
		return
	}

	// Deletion protection (B3d): nothing is removed while something the
	// panel knows about depends on it. The refusal names WHO blocks (D-014)
	// — an admin must see what a click would break before deciding.
	// Silme koruması (B3d): panelin bildiği bir şey ona muhtaçken hiçbir şey
	// kaldırılmaz. Ret, KİMİN engellediğini adlandırır (D-014) — admin,
	// tıklamanın neyi kıracağını görmeden karar vermemeli.
	if req.Package != "" {
		version := versionFromPackage(core.GetManagedServiceByID(req.ServiceID), req.Package)
		if version == "" {
			writeClientError(w, http.StatusBadRequest, "package does not name a removable version of this service")
			return
		}
		count, blockers, err := runtimeVersionBlockers(r.Context(), p.db.GetDB(), req.ServiceID, version)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if count > 0 {
			writeCodedErrorDetails(w, http.StatusConflict, errCodeRuntimeInUse,
				fmt.Sprintf("%d site(s) run on version %s — switch them to another version first.", count, version),
				"", blockers)
			return
		}
	} else {
		count, blockers, err := serviceDependents(r.Context(), p.db.GetDB(), req.ServiceID)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if count > 0 {
			writeCodedErrorDetails(w, http.StatusConflict, errCodeServiceHasDependents,
				fmt.Sprintf("%d thing(s) on this server depend on %s — remove or move them first.", count, req.ServiceID),
				"", blockers)
			return
		}
	}

	// Roundcube removal is its own RPC (it was never a distro package): delete
	// the tarball tree, then regenerate the loopback webmail server (which,
	// finding nothing, removes its config). Handled and returned here.
	// Roundcube kaldırma kendi RPC'sidir (hiç dağıtım paketi olmadı): tarball
	// ağacını sil, sonra loopback webmail sunucusunu yeniden üret (hiçbir şey
	// bulamayınca config'ini kaldırır). Burada ele alınıp döndürülür.
	if req.ServiceID == "roundcube" {
		var rmResp struct {
			Removed bool   `json:"removed"`
			Error   string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.RemoveRoundcube", &struct{}{}, &rmResp); err != nil {
			writeAgentError(w, err, "roundcube remove")
			return
		}
		if rmResp.Error != "" {
			writeClientError(w, http.StatusConflict, rmResp.Error)
			return
		}
		var wmResp struct {
			Configured bool   `json:"configured"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureWebmail", &struct{}{}, &wmResp); err != nil || wmResp.Error != "" {
			log.Printf("webmail configure after uninstall: %v %s", err, wmResp.Error)
		}
		if _, err := p.scanManagedServices(r.Context()); err != nil {
			log.Printf("rescan after roundcube uninstall: %v", err)
		}
		p.audit(r, "service.uninstall:roundcube", "service", 0)
		json.NewEncoder(w).Encode(map[string]any{"removed": rmResp.Removed, "success": true})
		return
	}

	var resp struct {
		Removed bool   `json:"removed"`
		Detail  string `json:"detail,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.UninstallService",
		&struct {
			ID      string `json:"id"`
			Package string `json:"package,omitempty"`
		}{ID: req.ServiceID, Package: req.Package}, &resp); err != nil {
		writeAgentError(w, err, "service uninstall")
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	if req.ServiceID == "phpmyadmin" || req.ServiceID == "phppgadmin" {
		var dbtResp struct {
			Configured bool   `json:"configured"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureDBTools", &struct{}{}, &dbtResp); err != nil || dbtResp.Error != "" {
			log.Printf("db tools configure after uninstall: %v %s", err, dbtResp.Error)
		}
	}
	// Removed service's ports should close; re-sync the firewall.
	// Kaldırılan servisin portları kapanmalı; güvenlik duvarını yeniden senkronla.
	p.syncFirewall()
	// Refresh the scan cache so the page tells the truth immediately — the
	// install path always did this; uninstall silently skipped it and the
	// removed service kept its old row until someone pressed Scan.
	// Tarama önbelleğini tazele ki sayfa hemen doğruyu söylesin — kurulum
	// yolu bunu hep yapıyordu; kaldırma sessizce atlıyordu ve kaldırılan
	// servis biri Tara'ya basana dek eski satırıyla kalıyordu.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("rescan after uninstall: %v", err)
	}
	p.audit(r, "service.uninstall:"+req.ServiceID+pkgSuffix(req.Package), "service", 0)
	json.NewEncoder(w).Encode(map[string]any{"removed": resp.Removed, "detail": resp.Detail, "success": true})
}

// versionFromPackage maps a version-pick package back to the version sites
// record: "php8.3-fpm" → "8.3". The service's own PackagePattern is the
// gatekeeper — an arbitrary package name never reaches the agent.
// versionFromPackage, sürüm-seçimli paketi sitelerin kaydettiği sürüme geri
// eşler: "php8.3-fpm" → "8.3". Kapı bekçisi servisin kendi PackagePattern'i —
// keyfi paket adı agent'a hiç ulaşmaz.
func versionFromPackage(svc *core.ManagedService, pkg string) string {
	if svc == nil || svc.Repo == nil || svc.Repo.PackagePattern == "" {
		return ""
	}
	re, err := regexp.Compile(svc.Repo.PackagePattern)
	if err != nil {
		return ""
	}
	m := re.FindStringSubmatch(pkg)
	if len(m) < 2 {
		return ""
	}
	// Capture 1 is the version-bearing prefix ("php8.3"); the version is its
	// digit tail. / 1. yakalama sürüm taşıyan önektir ("php8.3"); sürüm onun
	// rakamla başlayan kuyruğudur.
	prefix := m[1]
	for i, r := range prefix {
		if r >= '0' && r <= '9' {
			return prefix[i:]
		}
	}
	return ""
}

func pkgSuffix(pkg string) string {
	if pkg == "" {
		return ""
	}
	return ":" + pkg
}
