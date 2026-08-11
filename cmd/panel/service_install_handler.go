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
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	roundcubeUninstallMutationTimeout = 15 * time.Minute
	roundcubeUninstallScanTimeout     = 2 * time.Minute
	roundcubeUninstallAuditTimeout    = 5 * time.Second
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
	binding, err := panelMutationBinding(ctx)
	if err != nil {
		return result, serviceInstallFailure(err)
	}
	if err := p.preflightManagedServiceInstall(ctx, req.ServiceID, req.Package); err != nil {
		return result, serviceInstallFailure(err)
	}
	mutationRequest := transport.ServiceMutationRequest{ServiceMutationBinding: binding}

	// Roundcube is not a distro package (D-004: one path on every Linux) — it
	// installs from its own verified tarball via a dedicated RPC, then the
	// loopback webmail server is (re)generated. Handled here and returned so
	// the package path below never sees it.
	// Roundcube dağıtım paketi değildir (D-004: her Linux'ta tek yol) — kendi
	// doğrulanmış tarball'ından adanmış bir RPC ile kurulur, sonra loopback
	// webmail sunucusu yeniden üretilir. Burada ele alınıp döndürülür; aşağıdaki
	// paket yolu onu hiç görmez.
	if req.ServiceID == "roundcube" {
		var rcResp transport.InstallRoundcubeResponse
		webmailRequest := transport.WebmailMutationRequest{ServiceMutationBinding: binding}
		if err := p.agentClient.CallContext(ctx, "Agent.InstallRoundcube", &webmailRequest, &rcResp); err != nil {
			return result, serviceInstallFailure(err)
		}
		if rcResp.Error != "" {
			return result, serviceInstallFailure(fmt.Errorf("roundcube install: %s", rcResp.Error))
		}
		result["installed"] = rcResp.Installed
		if err := advance("configuring"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		var wmResp transport.ConfigureWebmailResponse
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigureWebmail", &webmailRequest, &wmResp); err != nil {
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
		if err := p.syncFirewall(ctx); err != nil {
			return result, firewallSyncFailure(err)
		}
		result["success"] = true
		return result, nil
	}

	// Package installs can take a while (apt fetches + configures); the
	// agent runs it synchronously and reports the real outcome.
	// Paket kurulumları sürebilir (apt indirir + yapılandırır); agent bunu
	// senkron çalıştırır ve gerçek sonucu bildirir.
	var resp transport.InstallServiceResponse
	if err := p.agentClient.CallContext(ctx, "Agent.InstallService", &transport.InstallServiceRequest{
		ServiceMutationBinding: binding,
		ID:                     req.ServiceID,
		Package:                req.Package,
	}, &resp); err != nil {
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
	if req.ServiceID == "postgresql" && req.Package != "" {
		// A selected major is complete only when the agent confirms the exact
		// per-cluster unit it started and verified. The later catalogue scan is
		// intentionally aggregate and may also see an older running major, so it
		// cannot replace this exact-target proof.
		// Seçilen major yalnızca agent başlattığı ve doğruladığı tam cluster
		// unit'ini onayladığında tamamlanır. Sonraki katalog taraması topludur ve
		// çalışan eski bir major'ı da görebilir; bu yüzden tam hedef kanıtının
		// yerini alamaz.
		expectedUnit, ok := core.PostgreSQLClusterUnitForPackage(req.Package)
		if !ok || resp.Unit != expectedUnit {
			return result, serviceInstallFailure(fmt.Errorf(
				"agent did not confirm the selected PostgreSQL cluster unit %s", expectedUnit))
		}
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
		var dnsResp transport.SyncDNSZoneResponse
		if err := p.callAgentContext(ctx, "Agent.ConfigurePowerDNSSQLite", &mutationRequest, &dnsResp); err != nil {
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
		if err := p.agentClient.CallContext(ctx, "Agent.ServiceMutationAction", &transport.ServiceMutationActionRequest{
			ServiceMutationBinding: binding,
			ServiceName:            "pdns",
			Action:                 "restart",
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
		var nrResp transport.EnsureNginxReadyResponse
		if err := p.agentClient.CallContext(ctx, "Agent.EnsureNginxReady", &mutationRequest, &nrResp); err != nil {
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
		var mailResp transport.ConfigureMailStackResponse
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigureMailStack", &mutationRequest, &mailResp); err != nil {
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
		serviceRequest := transport.ServiceMutationServiceRequest{
			ServiceMutationBinding: binding,
			ServiceName:            unit,
		}
		if err := p.agentClient.CallContext(ctx, "Agent.ResetFailedUnitMutation", &serviceRequest, &ok); err != nil {
			return result, serviceInstallFailure(fmt.Errorf("reset failed mail service: %w", err))
		}
		if !ok {
			return result, serviceInstallFailure(errors.New("agent did not confirm resetting the mail service"))
		}
		ok = false
		if err := p.agentClient.CallContext(ctx, "Agent.StartServiceMutation", &serviceRequest, &ok); err != nil {
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
		var wireResp transport.WireMailFiltersResponse
		if err := p.agentClient.CallContext(ctx, "Agent.WireMailFilters", &mutationRequest, &wireResp); err != nil {
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
		var vpnResp transport.SetupVPNResponse
		if err := advance("starting"); err != nil {
			return result, operationAdvanceFailure(err)
		}
		if err := p.agentClient.CallContext(ctx, "Agent.SetupVPN", &transport.SetupVPNRequest{
			ServiceMutationBinding: transport.ServiceMutationBinding{
				MutationRequestID: binding.MutationRequestID,
				MutationOwnerID:   binding.MutationOwnerID,
			},
		}, &vpnResp); err != nil {
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
		var dbtResp transport.ConfigureDBToolsResponse
		if err := p.agentClient.CallContext(ctx, "Agent.ConfigureDBTools", &mutationRequest, &dbtResp); err != nil {
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
	if req.ServiceID == "postgresql" && req.Package != "" {
		expectedUnit, ok := core.PostgreSQLClusterUnitForPackage(req.Package)
		if !ok {
			return result, serviceInstallFailure(fmt.Errorf("invalid PostgreSQL package %q", req.Package))
		}
		// The aggregate catalogue scan can be green because an older major is
		// still active. Re-read the agent's final unit state and require the
		// selected cluster itself; the InstallService response is not a durable
		// proof if that unit stops before the operation completes.
		//
		// Toplu katalog taraması eski bir major hâlâ çalıştığı için yeşil olabilir.
		// Agent'ın son unit durumunu yeniden oku ve seçilen cluster'ın kendisini
		// zorunlu tut; işlem bitmeden unit durursa InstallService yanıtı kalıcı
		// kanıt değildir.
		exactReady, err := p.exactServiceUnitActive(ctx, expectedUnit)
		if err != nil {
			return result, serviceInstallFailure(fmt.Errorf("verify selected PostgreSQL cluster unit %s: %w", expectedUnit, err))
		}
		if !exactReady {
			return result, serviceInstallFailure(fmt.Errorf(
				"post-install scan did not find selected PostgreSQL cluster unit %s active", expectedUnit))
		}
	}
	result["installed"] = true

	// New service may expose new ports; if the firewall is on, open them.
	// Yeni servis yeni port açabilir; güvenlik duvarı açıksa onları aç.
	if err := advance("firewall"); err != nil {
		return result, operationAdvanceFailure(err)
	}
	if err := p.syncFirewall(ctx); err != nil {
		return result, firewallSyncFailure(err)
	}
	result["success"] = true
	return result, nil
}

func (p *Panel) exactServiceUnitActive(ctx context.Context, expectedUnit string) (bool, error) {
	var services []core.Service
	if err := p.agentClient.CallContext(ctx, "Agent.GetServices", &transport.Empty{}, &services); err != nil {
		return false, err
	}
	expectedUnit = strings.TrimSuffix(strings.TrimSpace(expectedUnit), ".service")
	for _, service := range services {
		unit := strings.TrimSuffix(strings.TrimSpace(service.Name), ".service")
		if unit == expectedUnit && strings.HasPrefix(strings.ToLower(strings.TrimSpace(service.Status)), "active") {
			return true, nil
		}
	}
	return false, nil
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
	if err := p.callAgentContext(
		r.Context(),
		"Agent.ServiceCandidateVersion",
		&transport.InstallServiceRequest{ID: id},
		&version,
	); err != nil {
		writeServerError(w, err)
		return
	}
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
		var rmResp transport.RemoveRoundcubeResponse
		var wmResp transport.ConfigureWebmailResponse
		var mutationWorkErr error
		mutationApplied := false
		configureAttempted := false

		// A browser going away must not strand the host half-mutated. The durable
		// lease, both idempotent host steps and lease finalization therefore run
		// under their own hard deadline while retaining caller values for
		// attribution.
		mutationCtx, cancelMutation := context.WithTimeout(
			context.WithoutCancel(r.Context()),
			roundcubeUninstallMutationTimeout,
		)
		mutationErr := p.withStandaloneAgentMutation(mutationCtx, "service_uninstall", "roundcube", "", func(callCtx context.Context, binding agentMutationBinding) error {
			request := transport.WebmailMutationRequest{ServiceMutationBinding: binding}
			var removeCallErr error
			for attempt := 0; attempt < 2; attempt++ {
				rmResp = transport.RemoveRoundcubeResponse{}
				removeCallErr = p.agentClient.CallContext(callCtx, "Agent.RemoveRoundcube", &request, &rmResp)
				if removeCallErr == nil {
					break
				}
			}

			var workErrors []error
			if removeCallErr != nil {
				workErrors = append(workErrors, fmt.Errorf("remove Roundcube after retry: %w", removeCallErr))
			} else {
				mutationApplied = rmResp.MutationApplied
				if rmResp.Error != "" {
					workErrors = append(workErrors, fmt.Errorf("remove Roundcube: %s", rmResp.Error))
				}
				if !rmResp.Removed {
					workErrors = append(workErrors, errors.New("agent did not confirm Roundcube removal"))
				}
			}

			// A lost Remove response is ambiguous, and an applied Remove may carry a
			// post-mutation error. Configure is idempotent and serialized by the
			// same agent step lock, so it is the safe reconciliation in both cases.
			if removeCallErr != nil || rmResp.Removed || rmResp.MutationApplied {
				configureAttempted = true
				var configureCallErr error
				for attempt := 0; attempt < 2; attempt++ {
					wmResp = transport.ConfigureWebmailResponse{}
					configureCallErr = p.agentClient.CallContext(callCtx, "Agent.ConfigureWebmail", &request, &wmResp)
					if configureCallErr == nil {
						break
					}
				}
				if configureCallErr != nil {
					workErrors = append(workErrors, fmt.Errorf("clean up Roundcube webmail configuration after retry: %w", configureCallErr))
				} else {
					if wmResp.Error != "" {
						workErrors = append(workErrors, fmt.Errorf("clean up Roundcube webmail configuration: %s", wmResp.Error))
					}
					if !wmResp.Configured {
						workErrors = append(workErrors, errors.New("agent did not confirm Roundcube webmail cleanup"))
					}
					if wmResp.Present {
						workErrors = append(workErrors, errors.New("agent still reports Roundcube present after webmail cleanup"))
					}
				}
			}

			mutationWorkErr = errors.Join(workErrors...)
			return mutationWorkErr
		})
		cancelMutation()

		// The final host observation and every audit write get new detached,
		// bounded contexts. Clone retains the authenticated caller and request
		// metadata even when the original HTTP context has been canceled.
		auditDetached := func(action string) {
			auditCtx, cancelAudit := context.WithTimeout(
				context.WithoutCancel(r.Context()),
				roundcubeUninstallAuditTimeout,
			)
			defer cancelAudit()
			p.audit(r.Clone(auditCtx), action, "service", 0)
		}
		scanCtx, cancelScan := context.WithTimeout(
			context.WithoutCancel(r.Context()),
			roundcubeUninstallScanTimeout,
		)
		services, scanErr := p.scanManagedServices(scanCtx)
		cancelScan()

		cleanMutation := mutationErr == nil && mutationWorkErr == nil &&
			rmResp.Removed && rmResp.Error == "" &&
			configureAttempted && wmResp.Configured && !wmResp.Present && wmResp.Error == ""
		failureErr := mutationErr
		if failureErr == nil {
			failureErr = mutationWorkErr
		} else if mutationWorkErr != nil && !errors.Is(failureErr, mutationWorkErr) {
			failureErr = errors.Join(mutationWorkErr, failureErr)
		}

		if scanErr != nil {
			log.Printf("rescan after roundcube uninstall: %v", scanErr)
			if cleanMutation {
				auditDetached("service.uninstall:roundcube")
			} else {
				reason := failureErr
				if reason == nil {
					reason = errors.New("Roundcube removal outcome is uncertain")
				}
				auditDetached("service.uninstall.partial:roundcube — " + auditReason(reason.Error()))
			}
			auditDetached("service.uninstall.refresh.failed:roundcube — " + auditReason(scanErr.Error()))
			writeRoundcubeStateRefreshFailed(w, mutationApplied)
			return
		}

		// The fresh scan is authoritative. Even a positive RPC acknowledgement
		// cannot be reported as applied when the host still exposes Roundcube.
		if verifyManagedServiceInstalled(services, "roundcube") {
			installedErr := errors.New("fresh service scan still reports Roundcube installed")
			if failureErr != nil {
				installedErr = errors.Join(failureErr, installedErr)
			}
			auditDetached("service.uninstall.failed:roundcube — " + auditReason(installedErr.Error()))
			writeAgentError(w, installedErr, "roundcube remains installed after removal attempt")
			return
		}

		if !cleanMutation {
			if failureErr == nil {
				failureErr = errors.New("Roundcube removal could not be fully confirmed")
			}
			log.Printf("roundcube uninstall partial: %v", failureErr)
			auditDetached("service.uninstall.partial:roundcube — " + auditReason(failureErr.Error()))
			writeWebmailUninstallPartial(w, mutationApplied)
			return
		}

		auditDetached("service.uninstall:roundcube")
		json.NewEncoder(w).Encode(map[string]any{"removed": rmResp.Removed, "success": true})
		return
	}

	var resp transport.UninstallServiceResponse
	err := p.withStandaloneAgentMutation(r.Context(), "service_uninstall", req.ServiceID, req.Package, func(callCtx context.Context, binding agentMutationBinding) error {
		request := transport.InstallServiceRequest{
			ServiceMutationBinding: binding,
			ID:                     req.ServiceID,
			Package:                req.Package,
		}
		if err := p.agentClient.CallContext(callCtx, "Agent.UninstallService", &request, &resp); err != nil {
			return err
		}
		if resp.Error != "" && !resp.MutationApplied {
			return errors.New(resp.Error)
		}
		return nil
	})
	if err != nil && resp.Error == "" {
		p.audit(r, "service.uninstall.failed:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(err.Error()), "service", 0)
		writeAgentError(w, err, "service uninstall")
		return
	}
	if resp.Error != "" && !resp.MutationApplied {
		p.audit(r, "service.uninstall.failed:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(resp.Error), "service", 0)
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	packageRemovalPartial := resp.Error != "" && resp.MutationApplied
	// The package mutation itself succeeded. Follow-up wiring, firewall and
	// scan failures are separate outcomes and must not hide this audit entry.
	// Paket değişikliğinin kendisi başarılı oldu. Sonraki bağlantı, güvenlik
	// duvarı ve tarama hataları bu denetim kaydını gizlememelidir.
	if packageRemovalPartial {
		p.audit(r, "service.uninstall.partial:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(resp.Error), "service", 0)
	} else {
		p.audit(r, "service.uninstall:"+req.ServiceID+pkgSuffix(req.Package), "service", 0)
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
	var mailFilterSyncErr error
	if svc := core.GetManagedServiceByID(req.ServiceID); svc != nil && svc.ConflictGroup == "spam-filter" {
		var wireResp transport.WireMailFiltersResponse
		err := p.withStandaloneAgentMutation(r.Context(), "mail_filter_wire", req.ServiceID, "", func(callCtx context.Context, binding agentMutationBinding) error {
			request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
			if err := p.agentClient.CallContext(callCtx, "Agent.WireMailFilters", &request, &wireResp); err != nil {
				return err
			}
			if wireResp.Error != "" {
				return errors.New(wireResp.Error)
			}
			return nil
		})
		if err != nil {
			mailFilterSyncErr = err
			log.Printf("milter wiring after %s %s: %v", "uninstall", req.ServiceID, err)
		} else if wireResp.Error != "" {
			mailFilterSyncErr = errors.New(wireResp.Error)
			log.Printf("milter wiring after %s %s: %s", "uninstall", req.ServiceID, wireResp.Error)
		} else {
			log.Printf("milter chain now: %q", wireResp.Detail)
		}
	}

	if req.ServiceID == "phpmyadmin" || req.ServiceID == "phppgadmin" {
		var dbtResp transport.ConfigureDBToolsResponse
		err := p.withStandaloneAgentMutation(r.Context(), "dbtools_configure", req.ServiceID, "", func(callCtx context.Context, binding agentMutationBinding) error {
			request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
			if err := p.agentClient.CallContext(callCtx, "Agent.ConfigureDBTools", &request, &dbtResp); err != nil {
				return err
			}
			if dbtResp.Error != "" {
				return errors.New(dbtResp.Error)
			}
			return nil
		})
		if err != nil || dbtResp.Error != "" {
			log.Printf("db tools configure after uninstall: %v %s", err, dbtResp.Error)
		}
	}
	// Removed service's ports should close; re-sync the firewall.
	// Kaldırılan servisin portları kapanmalı; güvenlik duvarını yeniden senkronla.
	firewallErr := p.syncFirewall(r.Context())
	// Refresh the scan cache so the page tells the truth immediately — the
	// install path always did this; uninstall silently skipped it and the
	// removed service kept its old row until someone pressed Scan.
	// Tarama önbelleğini tazele ki sayfa hemen doğruyu söylesin — kurulum
	// yolu bunu hep yapıyordu; kaldırma sessizce atlıyordu ve kaldırılan
	// servis biri Tara'ya basana dek eski satırıyla kalıyordu.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("rescan after uninstall: %v", err)
		if firewallErr != nil {
			p.audit(r, "service.uninstall.partial:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(firewallErr.Error()), "service", 0)
		}
		p.audit(r, "service.uninstall.refresh.failed:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(err.Error()), "service", 0)
		writeServiceStateRefreshFailed(w)
		return
	}
	if packageRemovalPartial {
		if mailFilterSyncErr != nil {
			p.audit(r, "service.uninstall.mail-filter.failed:"+req.ServiceID+" — "+auditReason(mailFilterSyncErr.Error()), "service", 0)
		}
		if firewallErr != nil {
			p.audit(r, "service.uninstall.firewall.failed:"+req.ServiceID+" — "+auditReason(firewallErr.Error()), "service", 0)
		}
		writeServiceUninstallPartial(w)
		return
	}
	if mailFilterSyncErr != nil {
		p.audit(r, "service.uninstall.mail-filter.failed:"+req.ServiceID+" — "+auditReason(mailFilterSyncErr.Error()), "service", 0)
		if firewallErr != nil {
			p.audit(r, "service.uninstall.firewall.failed:"+req.ServiceID+" — "+auditReason(firewallErr.Error()), "service", 0)
		}
		writeServiceMailFilterSyncFailed(w)
		return
	}
	if firewallErr != nil {
		log.Printf("service removed but firewall synchronization failed: %v", firewallErr)
		p.audit(r, "service.uninstall.partial:"+req.ServiceID+pkgSuffix(req.Package)+" — "+auditReason(firewallErr.Error()), "service", 0)
		writeServiceFirewallSyncFailed(w)
		return
	}
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
		var resp transport.WireMailFiltersResponse
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := p.withStandaloneAgentMutation(ctx, "mail_filter_wire", "startup", "", func(callCtx context.Context, binding agentMutationBinding) error {
			request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
			if err := p.agentClient.CallContext(callCtx, "Agent.WireMailFilters", &request, &resp); err != nil {
				return err
			}
			if resp.Error != "" {
				return errors.New(resp.Error)
			}
			return nil
		})
		if err != nil {
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
	var resp transport.ServiceJournalResponse
	callCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	request := transport.ServiceJournalRequest{Unit: unit, Lines: lines}
	if err := p.agentClient.CallContext(callCtx, "Agent.ServiceJournal", &request, &resp); err != nil {
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
