package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
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
func (p *Panel) runServiceInstall(
	ctx context.Context,
	req serviceInstallRequest,
	advance func(string) error,
) (serviceOperationResult, *serviceOperationFailure) {
	result := serviceOperationResult{"success": false, "installed": false}
	if err := p.preflightManagedServiceInstall(ctx, req.ServiceID); err != nil {
		return result, serviceInstallFailure(err)
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
		if err := p.agentClient.CallContext(ctx, "Agent.InstallRoundcube", &struct{}{}, &rcResp); err != nil {
			return result, serviceInstallFailure(err)
		}
		if rcResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("roundcube install: %s", rcResp.Error))
		}
		result["installed"] = rcResp.Installed
		if err := advance("configuring"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		var wmResp struct {
			Configured bool   `json:"configured"`
			Present    bool   `json:"present"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigureWebmail", &struct{}{}, &wmResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("roundcube webmail configuration: %w", err))
		}
		if wmResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("roundcube webmail configuration: %s", wmResp.Error))
		}
		if !wmResp.Configured {
			return result, serviceInstallFailure(errors.New("agent did not confirm Roundcube webmail configuration"))
		}
		if !wmResp.Present {
			return result, serviceInstallFailure(errors.New("agent did not confirm that Roundcube is present in the webmail configuration"))
		}
		if err := advance("scanning"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		services, err := p.scanManagedServices(ctx)
		if err != nil {
			return result, serviceInstallFailure(fmt.Errorf("roundcube post-install scan: %w", err))
		}
		if !verifyManagedServiceInstalled(services, req.ServiceID) {
			return result, serviceInstallFailure(errors.New("post-install scan did not find Roundcube"))
		}
		result["installed"] = true
		if err := advance("firewall"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		p.syncFirewall()
		result["success"] = true
		return result, nil
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
	if err := p.agentClient.CallContext(ctx, "Agent.InstallService", &struct {
		ID      string `json:"id"`
		Package string `json:"package,omitempty"`
	}{ID: req.ServiceID, Package: req.Package}, &resp); err != nil {
		// A failed attempt must leave a trace. Netdata could not be installed
		// on Debian (apt has no such package) and the audit log stayed SILENT —
		// so from the log it looked as if the operator never tried. "log yok"
		// (operator, 25 Jul) was really "failures are not logged": every entry
		// in the ledger was a success, which makes the ledger a highlight reel
		// instead of a record.
		// Başarısız deneme de iz bırakmalı. Netdata Debian'da kurulamadı
		// (apt'ta böyle paket yok) ve denetim kaydı SESSİZ kaldı — kayda
		// bakınca operatör hiç denememiş gibi görünüyordu. "log yok"
		// (operatör, 25 Tem) aslında "başarısızlıklar kaydedilmiyor" demekti:
		// defterdeki her satır bir başarıydı, bu da defteri kayıt değil
		// vitrin yapar.
		return result, serviceInstallFailure(err)
	}
	if resp.Error != "" {
		return result, serviceInstallFailure(fmt.Errorf("service install: %s", resp.Error))
	}
	result["installed"] = resp.Installed
	if err := advance("configuring"); err != nil {
		return result, operationAdvanceFailure(err)
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
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigurePowerDNSSQLite", &struct{}{}, &dnsResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("PowerDNS configuration: %w", err))
		}
		if dnsResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("PowerDNS configuration: %s", dnsResp.Error))
		}
		if !dnsResp.Synced {
			return result, serviceInstallFailure(errors.New("agent did not confirm PowerDNS configuration"))
		}
		if err := advance("starting"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		var lifecycle transport.ServiceActionResult
		if err := p.agentClient.CallContext(ctx, "Agent.ServiceAction", &transport.ServiceActionArgs{
			ServiceName: "pdns",
			Action:      "restart",
		}, &lifecycle); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("PowerDNS restart: %w", err))
		}
		if lifecycle.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("PowerDNS restart: %s", lifecycle.Error))
		}
		if !lifecycle.Success {
			return result, serviceInstallFailure(errors.New("agent did not confirm PowerDNS restart"))
		}
		if err := advance("syncing"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		if _, err := p.syncAllZonesStrict(ctx); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("PowerDNS zone synchronization: %w", err))
		}
	}

	// Installing a web server is only half done until it can actually serve
	// the panel's vhosts, db-tools and webmail. Debian's nginx already
	// includes the drop-in dirs; Arch's does not (its minimal nginx.conf) —
	// so "installed" would be a lie there until the includes are wired. A web
	// server that cannot serve is not installed (operator, 24 Jul).
	// Bir web sunucusu kurmak da, panelin vhost'larını, db-araçlarını ve
	// webmail'ini fiilen sunana dek yarım kalır. Debian'ın nginx'i drop-in
	// dizinlerini zaten dahil eder; Arch'ınki (minimal nginx.conf) etmez —
	// yani include'lar bağlanana dek "kurulu" orada yalan olurdu. Sunamayan
	// bir web sunucusu kurulu değildir (operatör, 24 Tem).
	if req.ServiceID == "nginx" {
		var nrResp struct {
			Ready bool   `json:"ready"`
			Error string `json:"error,omitempty"`
		}
		if err := p.agentClient.CallContext(ctx, "Agent.EnsureNginxReady", &struct{}{}, &nrResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("nginx readiness configuration: %w", err))
		}
		if nrResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("nginx readiness configuration: %s", nrResp.Error))
		}
		if !nrResp.Ready {
			return result, serviceInstallFailure(errors.New("agent did not confirm nginx readiness"))
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
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigureMailStack", &struct{}{}, &mailResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("mail stack configuration: %w", err))
		}
		if mailResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("mail stack configuration: %s", mailResp.Error))
		}
		if !mailResp.Configured {
			return result, serviceInstallFailure(errors.New("agent did not confirm mail stack configuration"))
		}
		// The agent starts a unit the moment its package lands, which is BEFORE
		// this configuration ran — on Arch that first start failed (no TLS cert
		// yet) and systemd left the service in "failed", so a correct install
		// still looked broken. Configuration is done now, so start it again;
		// systemd needs the failed state cleared first.
		// Agent, paketi düşer düşmez unit'i başlatır; bu, bu yapılandırmadan
		// ÖNCEdir — Arch'ta o ilk başlatma başarısız oluyordu (henüz TLS
		// sertifikası yok) ve systemd servisi "failed"de bırakıyordu; doğru bir
		// kurulum yine de bozuk görünüyordu. Yapılandırma bitti, tekrar başlat;
		// systemd önce failed durumunun temizlenmesini ister.
		unit := req.ServiceID
		if err := advance("starting"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		var ok bool
		if err := p.agentClient.CallContext(ctx, "Agent.ResetFailedUnit", &transport.ServiceArgs{ServiceName: unit}, &ok); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("reset failed mail service: %w", err))
		}
		if !ok {
			return result, serviceInstallFailure(errors.New("agent did not confirm resetting the mail service"))
		}
		ok = false
		if err := p.agentClient.CallContext(ctx, "Agent.StartService", &transport.ServiceArgs{ServiceName: unit}, &ok); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("start configured mail service: %w", err))
		}
		if !ok {
			return result, serviceInstallFailure(errors.New("agent did not confirm starting the configured mail service"))
		}
	}

	// A spam filter that is installed but not wired into Postfix is theatre:
	// the daemon runs, the row says "Running", and not one message passes
	// through it — exactly what a live check found on 24 Jul (smtpd_milters was
	// EMPTY next to a running Rspamd). Removing one is just as dangerous the
	// other way: Postfix would keep pointing at a socket that no longer
	// answers. Both directions therefore re-compose the chain. The trigger is
	// the SEAT, not a product list, so a spam filter added to the catalogue
	// tomorrow is wired without touching this file.
	// Kurulu ama Postfix'e bağlanmamış bir spam filtresi tiyatrodur: daemon
	// koşar, satır "Çalışıyor" der ve içinden tek bir ileti geçmez — 24 Tem'de
	// canlı denetimin bulduğu tam olarak budur (çalışan bir Rspamd'nin yanında
	// smtpd_milters BOŞtu). Kaldırmak da öbür yönden aynı ölçüde tehlikelidir:
	// Postfix artık cevap vermeyen bir sokete bakmayı sürdürürdü. Bu yüzden iki
	// yön de zinciri yeniden besteler. Tetikleyici ürün listesi değil KOLTUK'tur;
	// böylece yarın kataloğa eklenen bir spam filtresi bu dosyaya dokunmadan
	// bağlanır.
	if svc := core.GetManagedServiceByID(req.ServiceID); svc != nil && svc.ConflictGroup == "spam-filter" {
		var wireResp struct {
			Wired  bool   `json:"wired"`
			Detail string `json:"detail,omitempty"`
			Error  string `json:"error,omitempty"`
		}
		if err := p.agentClient.CallContext(ctx, "Agent.WireMailFilters", &struct{}{}, &wireResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("mail filter wiring: %w", err))
		}
		if wireResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("mail filter wiring: %s", wireResp.Error))
		}
		if !wireResp.Wired {
			return result, serviceInstallFailure(errors.New("agent did not confirm mail filter wiring"))
		}
	}

	// Installing WireGuard used to stop at the package: no server key, no
	// wg0.conf, so the unit was guaranteed to FAIL on start — the operator
	// pressed play on a fresh install and got a dead "Stopped" row (25 Jul:
	// "WireGuard VPN server çalışmıyor"). The same lesson as nginx includes
	// and Dovecot's TLS cert: install must leave the component WORKING. Setup
	// is idempotent and syncs any peers already in the ledger, so a reinstall
	// comes back with its clients intact.
	// WireGuard kurulumu pakette kalıyordu: sunucu anahtarı yok, wg0.conf yok;
	// unit başlatılınca DÜŞMEYE mahkûmdu — operatör taze kurulumda oynat'a
	// bastı ve ölü bir "Durdu" satırı gördü (25 Tem: "WireGuard VPN server
	// çalışmıyor"). Ders, nginx include'ları ve Dovecot TLS sertifikasıyla
	// aynı: kurulum bileşeni ÇALIŞIR bırakmalı. Kurulum adımı değişmez etkili
	// ve defterdeki peer'ları da senkronlar; yeniden kurulum istemcileriyle
	// birlikte döner.
	if req.ServiceID == "wireguard" {
		var vpnResp struct {
			Created bool   `json:"created"`
			Detail  string `json:"detail,omitempty"`
			Error   string `json:"error,omitempty"`
		}
		if err := advance("starting"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		if err := p.agentClient.CallContext(ctx, "Agent.SetupVPN", &struct {
			Port int `json:"port"`
		}{}, &vpnResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("WireGuard setup: %w", err))
		}
		if vpnResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("WireGuard setup: %s", vpnResp.Error))
		}
		if err := advance("syncing"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		if err := p.syncVPNPeers(ctx); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("WireGuard peer synchronization: %w", err))
		}
	}

	// Database web tools are files, not daemons: after (un)install the agent
	// must (re)generate the loopback nginx server that actually serves them.
	// Veritabanı web araçları daemon değil dosyadır: kur/kaldır sonrası agent,
	// onları fiilen sunan yalnız-loopback nginx sunucusunu yeniden üretmeli.
	if req.ServiceID == "phpmyadmin" || req.ServiceID == "phppgadmin" {
		var dbtResp struct {
			Configured bool     `json:"configured"`
			Tools      []string `json:"tools"`
			Error      string   `json:"error,omitempty"`
		}
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigureDBTools", &struct{}{}, &dbtResp); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("database tools configuration: %w", err))
		}
		if dbtResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("database tools configuration: %s", dbtResp.Error))
		}
		if !dbtResp.Configured {
			return result, serviceInstallFailure(errors.New("agent did not confirm database tools configuration"))
		}
		if !contains(dbtResp.Tools, req.ServiceID) {
			return result, serviceInstallFailure(fmt.Errorf("agent did not confirm %s in the database tools configuration", req.ServiceID))
		}
	}

	// (Roundcube's install is handled earlier and returns before this point.)
	// (Roundcube kurulumu yukarıda ele alınır ve buraya gelmeden döner.)

	// A new service exists now; refresh the cached scan so every page keeps
	// reading from cache instead of probing.
	// Artık yeni bir servis var; önbellekteki taramayı tazele ki sayfalar
	// yoklama yapmak yerine önbellekten okumaya devam etsin.
	if err := advance("scanning"); err != nil {
		return result, operationAdvanceFailure(err)
	}
	services, err := p.scanManagedServices(ctx)
	if err != nil {
		return result, serviceInstallFailure(fmt.Errorf("post-install scan: %w", err))
	}
	if !verifyManagedServiceReady(services, req.ServiceID) {
		return result, serviceInstallFailure(errors.New("post-install scan did not find the requested service in its required state"))
	}
	result["installed"] = true

	// New service may expose new ports; if the firewall is on, open them.
	// Yeni servis yeni port açabilir; güvenlik duvarı açıksa onları aç.
	if err := advance("firewall"); err != nil {
		return result, operationAdvanceFailure(err)
	}
	p.syncFirewall()
	result["success"] = true
	return result, nil
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
	release, busy := p.beginServiceMutation(w, r)
	if busy {
		return
	}
	defer release()
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
		p.audit(r, "service.uninstall.failed:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(err.Error()), "service", 0)
		writeAgentError(w, err, "service uninstall")
		return
	}
	if resp.Error != "" {
		p.audit(r, "service.uninstall.failed:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(resp.Error), "service", 0)
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	// A spam filter that is installed but not wired into Postfix is theatre:
	// the daemon runs, the row says "Running", and not one message passes
	// through it — exactly what a live check found on 24 Jul (smtpd_milters was
	// EMPTY next to a running Rspamd). Removing one is just as dangerous the
	// other way: Postfix would keep pointing at a socket that no longer
	// answers. Both directions therefore re-compose the chain. The trigger is
	// the SEAT, not a product list, so a spam filter added to the catalogue
	// tomorrow is wired without touching this file.
	// Kurulu ama Postfix'e bağlanmamış bir spam filtresi tiyatrodur: daemon
	// koşar, satır "Çalışıyor" der ve içinden tek bir ileti geçmez — 24 Tem'de
	// canlı denetimin bulduğu tam olarak budur (çalışan bir Rspamd'nin yanında
	// smtpd_milters BOŞtu). Kaldırmak da öbür yönden aynı ölçüde tehlikelidir:
	// Postfix artık cevap vermeyen bir sokete bakmayı sürdürürdü. Bu yüzden iki
	// yön de zinciri yeniden besteler. Tetikleyici ürün listesi değil KOLTUK'tur;
	// böylece yarın kataloğa eklenen bir spam filtresi bu dosyaya dokunmadan
	// bağlanır.
	if svc := core.GetManagedServiceByID(req.ServiceID); svc != nil && svc.ConflictGroup == "spam-filter" {
		var wireResp struct {
			Wired  bool   `json:"wired"`
			Detail string `json:"detail,omitempty"`
			Error  string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.WireMailFilters", &struct{}{}, &wireResp); err != nil || wireResp.Error != "" {
			log.Printf("milter wiring after %s %s: %v %s", "uninstall", req.ServiceID, err, wireResp.Error)
		} else {
			log.Printf("milter chain now: %q", wireResp.Detail)
		}
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

// wireMailFiltersAtStartup repairs servers whose mail filters were installed
// before the panel knew how to wire them. It runs once, in the background, so a
// slow or absent agent never delays the listener.
// wireMailFiltersAtStartup, posta filtreleri panel onları bağlamayı bilmeden
// önce kurulmuş sunucuları onarır. Bir kez, arka planda koşar; böylece yavaş ya
// da yok olan bir agent dinleyiciyi hiç geciktirmez.
func (p *Panel) wireMailFiltersAtStartup() {
	go func() {
		var resp struct {
			Wired  bool   `json:"wired"`
			Detail string `json:"detail,omitempty"`
			Error  string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.WireMailFilters", &struct{}{}, &resp); err != nil {
			return // no agent yet, or no postfix — nothing to repair
		}
		if resp.Error != "" {
			log.Printf("milter wiring at startup: %s", resp.Error)
			return
		}
		if resp.Detail != "" {
			log.Printf("milter chain: %s", resp.Detail)
		}
	}()
}

// auditReason trims a failure message to something an audit row can carry
// without becoming a wall of text. The reason is the whole point of logging a
// failure — "install failed" alone tells the operator nothing they did not
// already see on screen.
// auditReason, bir başarısızlık mesajını denetim satırının duvara dönüşmeden
// taşıyabileceği boya kırpar. Sebep, başarısızlığı kaydetmenin bütün
// anlamıdır — yalnız "kurulum başarısız", operatöre ekranda zaten gördüğünden
// fazlasını söylemez.
func auditReason(msg string) string {
	msg = strings.TrimSpace(strings.ReplaceAll(msg, "\n", " "))
	const max = 180
	if len(msg) > max {
		msg = msg[:max] + "…"
	}
	return msg
}

// handleServiceLogs serves a unit's recent journal so the generic component
// page can show what a daemon is actually saying. Admin-only via the
// /api/v1/service/ prefix. Read-only: nothing here can change the server.
// handleServiceLogs, bir unit'in son günlüğünü sunar ki genel bileşen sayfası
// bir daemon'ın gerçekte ne dediğini gösterebilsin. /api/v1/service/ öneki
// sayesinde yalnız admin. Salt-okunur: burada sunucuyu değiştirebilecek hiçbir
// şey yok.
func (p *Panel) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	unit := strings.TrimSpace(r.URL.Query().Get("unit"))
	if unit == "" {
		writeClientError(w, http.StatusBadRequest, "unit is required")
		return
	}
	lines := 200
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			lines = n
		}
	}
	var resp struct {
		Unit  string   `json:"unit"`
		Lines []string `json:"lines"`
		Error string   `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.ServiceJournal", struct {
		Unit  string `json:"unit"`
		Lines int    `json:"lines"`
	}{Unit: unit, Lines: lines}, &resp); err != nil {
		writeAgentError(w, err, "service logs")
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusBadRequest, resp.Error)
		return
	}
	if resp.Lines == nil {
		resp.Lines = []string{}
	}
	json.NewEncoder(w).Encode(resp)
}
