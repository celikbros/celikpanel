package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// HTTP handlers must never hand internal error text to the client: those
// strings carry SQL fragments, filesystem paths and agent internals that
// help an attacker and help no one else. The full error goes to the log;
// the client gets a status-appropriate generic message.
//
// HTTP işleyicileri iç hata metnini asla istemciye vermemelidir: bu
// metinler SQL parçaları, dosya sistemi yolları ve agent iç detayları
// taşır; bunlar yalnızca saldırganın işine yarar. Tam hata log'a gider;
// istemci, duruma uygun genel bir mesaj alır.

// The API error contract (B1, Jul 18): every error body is ONE JSON shape.
// `error` is the developer-authored human message (always present); `code`
// is a STABLE machine-readable constant for deliberate refusals — the
// frontend translates it via i18n (`err.<CODE>`) and, when `action` names an
// in-panel path, renders a "go fix it" button. A refusal without a code is
// legacy; new deliberate refusals must ship with one.
//
// API hata sözleşmesi (B1, 18 Tem): her hata gövdesi TEK JSON biçimidir.
// `error` geliştirici yazımı insan-okur mesajdır (her zaman var); `code`
// bilinçli retler için SABİT makine-okur sabittir — ön yüz i18n ile çevirir
// (`err.<CODE>`) ve `action` panel-içi bir yol adlandırıyorsa "düzeltmeye
// git" düğmesi çizer. Kodsuz ret eskidir; yeni bilinçli ret kodla doğar.

type apiErrorBody struct {
	Error           string `json:"error"`
	Code            string `json:"code,omitempty"`
	Action          string `json:"action,omitempty"`
	PartialSuccess  bool   `json:"partial_success,omitempty"`
	MutationApplied bool   `json:"mutation_applied,omitempty"`
	// Details: the refusal's evidence, one line per item — for
	// RUNTIME_IN_USE the blocking sites ("example.com (ali)"), capped and
	// suffixed with "+N more" by the producer. Optional and additive: old
	// clients ignore it, so it is not a contract break (B3d).
	// Details: retin kanıtı, kalem başına bir satır — RUNTIME_IN_USE'ta
	// engelleyen siteler ("example.com (ali)"); üretici sınırlar ve "+N
	// tane daha" ekler. İsteğe bağlı ve eklemelidir: eski istemci yok
	// sayar, sözleşme kırılmaz (B3d).
	Details []string `json:"details,omitempty"`
}

// Stable refusal codes. Renaming one is an API break — don't.
// Sabit ret kodları. Birini yeniden adlandırmak API kırılmasıdır — yapma.
const (
	errCodeInternal                      = "INTERNAL"
	errCodeSubsystemDegraded             = "SUBSYSTEM_DEGRADED"
	errCodeStartupRecoveryFailed         = "STARTUP_RECOVERY_FAILED"
	errCodeSealedSecretUnreadable        = "SEALED_SECRET_UNREADABLE"
	errCodeMutationsHeld                 = "MUTATIONS_HELD"
	errCodeHostMutationBusy              = transport.HostMutationBusy
	errCodePlatformCapabilityUnavailable = "PLATFORM_CAPABILITY_UNAVAILABLE"
	errCodePlatformIdentityUnavailable   = "PLATFORM_IDENTITY_UNAVAILABLE"
	errCodeAuthRequired                  = "AUTH_REQUIRED"
	errCodeAdminOnly                     = "ADMIN_ONLY"
	errCodeAdditionalUserScope           = "ADDITIONAL_USER_SCOPE"
	errCodeAccountSuspended              = "ACCOUNT_SUSPENDED"
	errCodeDomainDeletionPending         = "DOMAIN_DELETION_PENDING"
	errCodeDNSServerRequired             = "DNS_SERVER_REQUIRED"
	errCodeDNSSettingsRequired           = "DNS_SETTINGS_REQUIRED"
	errCodeDNSSetupRequired              = "DNS_SETUP_REQUIRED"
	errCodeDNSEngineWorkflowRequired     = "DNS_ENGINE_WORKFLOW_REQUIRED"
	errCodeDNSTopologyUnsupported        = "DNS_TOPOLOGY_UNSUPPORTED"
	errCodeDNSPairIdentityLocked         = "DNS_PAIR_IDENTITY_LOCKED"
	errCodeDNSClusterPeerIsLocal         = "DNS_CLUSTER_PEER_IS_LOCAL"
	errCodeDNSPublicationFailed          = "DNS_PUBLICATION_FAILED"
	errCodeDNSEnginePlanRejected         = "DNS_ENGINE_PLAN_REJECTED"
	errCodeDNSEngineChangeNotCommitted   = "DNS_ENGINE_CHANGE_NOT_COMMITTED"
	errCodeDNSEngineStateUnverified      = "DNS_ENGINE_STATE_UNVERIFIED"
	errCodeDNSEngineMutationsHeld        = "DNS_ENGINE_MUTATIONS_HELD"
	errCodeDNSEngineChangeAppliedRefresh = "DNS_ENGINE_CHANGE_APPLIED_REFRESH_REQUIRED"
	errCodeDNSSECEngineUnsupported       = "DNSSEC_ENGINE_UNSUPPORTED"
	errCodeDNSSECStatusUnavailable       = "DNSSEC_STATUS_UNAVAILABLE"
	errCodeWebServerRequired             = "WEB_SERVER_REQUIRED"
	errCodePHPRequired                   = "PHP_REQUIRED"
	errCodeNoSubscription                = "NO_SUBSCRIPTION"
	errCodeQuotaDomains                  = "QUOTA_DOMAINS_EXCEEDED"
	errCodeQuotaDisk                     = "QUOTA_DISK_EXCEEDED"
	errCodeEntitlement                   = "ENTITLEMENT_REQUIRED"
	errCodeFirewallNoEngine              = "FIREWALL_ENGINE_MISSING"
	// R-055. This server is running a kernel whose module tree is gone, so it
	// can load no kernel module - and therefore no WireGuard - until it is
	// restarted. The agent proves that structurally; the panel names the one
	// action that fixes it instead of answering with an opaque 500.
	// R-055. Bu sunucu, modul agaci artik diskte olmayan bir cekirdekle
	// calisiyor; yeniden baslatilana kadar hicbir cekirdek modulu yukleyemez.
	errCodeVPNHostRestartRequired = "VPN_HOST_RESTART_REQUIRED"
	// The three SSH escape-hatch outcomes, told apart so the screen can offer
	// a way forward on the only one that has one.
	// Üç SSH kaçış-yolu sonucu; yalnız birinin bir çıkışı olduğu için ekran
	// onu sunabilsin diye birbirinden ayrılmıştır.
	errCodeFirewallNoSSHService    = "FIREWALL_NO_SSH_SERVICE"
	errCodeFirewallSSHNotListening = "FIREWALL_SSH_NOT_LISTENING"
	errCodeFirewallSSHUnprovable   = "FIREWALL_SSH_DISCOVERY_FAILED"
	// POOL_IDENTITY_FIXED: the caller tried to set who an FPM pool runs as or
	// which socket it answers on. Those are the panel's to decide — they are
	// the boundary between tenants, not a setting — so the attempt is refused
	// by name rather than quietly dropped.
	// POOL_IDENTITY_FIXED: çağıran, bir FPM havuzunun kim olarak koşacağını ya
	// da hangi sokete cevap vereceğini ayarlamaya çalıştı. Buna panel karar
	// verir — bunlar bir ayar değil, kiracılar arasındaki sınırdır — bu yüzden
	// deneme sessizce düşürülmek yerine adıyla reddedilir.
	errCodePoolIdentityFixed = "POOL_IDENTITY_FIXED"
	// RUNTIME_IN_USE: removing ONE version of a runtime while sites run on
	// it. SERVICE_HAS_DEPENDENTS: uninstalling a whole component while
	// things that need it exist (PHP sites, domains for DNS, mailboxes for
	// mail, databases for a DB engine). Both are D-014's rule made code —
	// removal is an event along the whole chain, and the refusal must say
	// WHO blocks, not just "in use"; Details carries the list.
	// RUNTIME_IN_USE: siteler üstünde koşarken bir runtime'ın TEK sürümünü
	// kaldırmak. SERVICE_HAS_DEPENDENTS: ona muhtaç şeyler varken (PHP
	// siteleri, DNS için domain'ler, posta için kutular, motor için
	// veritabanları) bileşeni bütünüyle kaldırmak. İkisi de D-014 kuralının
	// kod hâli — kaldırma zincir boyu bir olaydır ve ret yalnız
	// "kullanımda" değil KİMİN engellediğini söylemelidir; liste Details'ta.
	errCodeRuntimeInUse         = "RUNTIME_IN_USE"
	errCodeServiceHasDependents = "SERVICE_HAS_DEPENDENTS"
	// FIREWALL_SYNC_FAILED means the requested service mutation and its fresh
	// state scan completed, but the active firewall policy did not follow.
	// FIREWALL_SYNC_FAILED, istenen servis değişikliği ile taze durum taramasının
	// tamamlandığını ancak etkin güvenlik duvarı politikasının bunu izleyemediğini belirtir.
	errCodeFirewallSyncFailed = "FIREWALL_SYNC_FAILED"
	// MAIL_FILTER_SYNC_FAILED means package removal and fresh state verification
	// completed, but Postfix's filter chain could not be recomposed safely.
	// MAIL_FILTER_SYNC_FAILED, paket kaldırma ve taze durum doğrulamasının
	// tamamlandığını ancak Postfix filtre zincirinin güvenle yeniden
	// bestelenemediğini belirtir.
	errCodeMailFilterSyncFailed = "MAIL_FILTER_SYNC_FAILED"
	// SERVICE_UNINSTALL_PARTIAL means the service stop/disable mutation reached
	// the host, but package purge failed. A fresh scan is already available.
	// SERVICE_UNINSTALL_PARTIAL, servis durdurma/devre dışı bırakma
	// değişikliğinin makineye ulaştığını ancak paket kaldırmanın başarısız
	// olduğunu belirtir. Taze tarama zaten kullanılabilir.
	errCodeServiceUninstallPartial = "SERVICE_UNINSTALL_PARTIAL"
	errCodeWebmailUninstallPartial = "WEBMAIL_UNINSTALL_PARTIAL"
	// SERVICE_STATE_REFRESH_FAILED means the mandatory follow-up probe failed
	// after a service action. The response flags and message say whether the
	// mutation itself was positively confirmed; callers must never infer that
	// from this code or mistake stale cache data for the result of the action.
	// SERVICE_STATE_REFRESH_FAILED, bir servis işleminden sonraki zorunlu takip
	// yoklamasının başarısız olduğunu belirtir. Değişikliğin pozitif olarak
	// doğrulanıp doğrulanmadığını yanıt bayrakları ve mesaj söyler; çağıran bunu
	// hata kodundan çıkarmamalı veya bayat önbelleği işlemin sonucu sanmamalıdır.
	errCodeServiceStateRefreshFailed = "SERVICE_STATE_REFRESH_FAILED"
	// SERVICE_STATE_UNVERIFIED means persisted scan bytes could not be decoded
	// as a verified snapshot; fabricated not-installed rows must not be served.
	// SERVICE_STATE_UNVERIFIED, kalıcı tarama baytlarının doğrulanmış snapshot
	// olarak çözülemediğini belirtir; uydurma kurulu-değil satırları sunulamaz.
	errCodeServiceStateUnverified = "SERVICE_STATE_UNVERIFIED"
	// RUNTIME_VERSION_REQUIRED: a node site must name a panel-installed
	// version — the "system interpreter" escape is closed (B3d): the panel
	// only operates what it installed.
	// RUNTIME_VERSION_REQUIRED: node sitesi panelin kurduğu bir sürümü
	// adlandırmalı — "sistem yorumlayıcısı" kaçağı kapandı (B3d): panel
	// yalnız kendi kurduğunu işletir.
	errCodeRuntimeVersionRequired = "RUNTIME_VERSION_REQUIRED"
	// The config editor refused a path: not a component's discovered config
	// file, or one of the machine's own protected files. Showing the reason is
	// deliberate — an operator editing a legitimate file must learn WHY nothing
	// happened, and the message only names a path they supplied themselves.
	// Yapılandırma editörü bir yolu reddetti: ya bir bileşenin keşfedilmiş ayar
	// dosyası değil ya da makinenin kendi korunan dosyalarından biri. Gerekçeyi
	// göstermek bilinçlidir — meşru bir dosyayı düzenleyen operatör hiçbir şeyin
	// NEDEN olmadığını öğrenmeli ve mesaj yalnız kendi verdiği yolu adlandırır.
	errCodeConfigPathRefused = "CONFIG_PATH_REFUSED"
	// The written config did not pass its own syntax check, so it was rolled
	// back. The checker's message (nginx names the offending line) is the most
	// useful thing the panel can possibly say here — hiding it behind a
	// generic 500 leaves the operator staring at an editor that silently
	// refuses to save.
	// Yazılan yapılandırma kendi sözdizim denetimini geçmedi ve geri alındı.
	// Denetleyicinin mesajı (nginx hatalı satırı adıyla söyler), panelin
	// burada söyleyebileceği en yararlı şeydir — onu genel bir 500'ün arkasına
	// gizlemek, operatörü sessizce kaydetmeyi reddeden bir editöre bakar
	// hâlde bırakır.
	errCodeConfigInvalid = "CONFIG_INVALID"
)

type agentRPCPlatformErrorClassification struct {
	Status  int
	Code    string
	Message string
}

type singleErrorUnwrapper interface {
	Unwrap() error
}

type multiErrorUnwrapper interface {
	Unwrap() []error
}

// isPureWrappedError accepts only one linear wrapper chain ending at target.
// Joined/aggregate errors may contain rollback, compensation or partial-state
// failures, so they must retain the generic or caller-specific failure path.
func isPureWrappedError(err, target error) bool {
	if err == nil || target == nil {
		return false
	}
	for err != nil {
		if err == target {
			return true
		}
		if _, joined := err.(multiErrorUnwrapper); joined {
			return false
		}
		wrapped, ok := err.(singleErrorUnwrapper)
		if !ok {
			return false
		}
		err = wrapped.Unwrap()
	}
	return false
}

// classifyAgentRPCPlatformError exposes only the two local, policy-authored
// platform failures when they are the sole cause. Transport, context, remote
// RPC and aggregate/compensation errors deliberately retain INTERNAL or their
// caller-specific partial-state contract.
func classifyAgentRPCPlatformError(err error) (agentRPCPlatformErrorClassification, bool) {
	switch {
	case isPureWrappedError(err, errAgentRPCPlatformCapabilityDenied):
		return agentRPCPlatformErrorClassification{
			Status:  http.StatusConflict,
			Code:    errCodePlatformCapabilityUnavailable,
			Message: "this operation is unavailable on the connected server platform",
		}, true
	case isPureWrappedError(err, errAgentRPCPlatformIdentityUnavailable):
		return agentRPCPlatformErrorClassification{
			Status:  http.StatusBadGateway,
			Code:    errCodePlatformIdentityUnavailable,
			Message: "the connected server platform identity could not be verified",
		}, true
	default:
		return agentRPCPlatformErrorClassification{}, false
	}
}

func classifyHostMutationError(err error) (agentRPCPlatformErrorClassification, bool) {
	if !isPureWrappedError(err, errHostMutationBusy) {
		return agentRPCPlatformErrorClassification{}, false
	}
	return agentRPCPlatformErrorClassification{
		Status:  http.StatusConflict,
		Code:    errCodeHostMutationBusy,
		Message: "another server change or package-manager task is still running; wait and try again",
	}, true
}

func classifyStableAgentError(err error) (agentRPCPlatformErrorClassification, bool) {
	if classification, ok := classifyHostMutationError(err); ok {
		return classification, true
	}
	return classifyAgentRPCPlatformError(err)
}

// writeCodedError is the single writer of the contract. action, when
// non-empty, is an in-panel path that fixes the refusal (e.g. "/services").
// writeCodedError, sözleşmenin tek yazıcısıdır. action boş değilse reti
// düzelten panel-içi yoldur (örn. "/services").
func writeCodedError(w http.ResponseWriter, status int, code, message, action string) {
	writeCodedErrorDetails(w, status, code, message, action, nil)
}

// writeCodedErrorDetails: the same contract plus the refusal's evidence list.
// writeCodedErrorDetails: aynı sözleşme + retin kanıt listesi.
func writeCodedErrorDetails(w http.ResponseWriter, status int, code, message, action string, details []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Action: action, Details: details})
}

// writeServiceStateRefreshFailure centralizes the refresh-failure code while
// emitting outcome flags only when the host mutation was positively confirmed.
func writeServiceStateRefreshFailure(w http.ResponseWriter, message string, mutationApplied bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:           message,
		Code:            errCodeServiceStateRefreshFailed,
		PartialSuccess:  mutationApplied,
		MutationApplied: mutationApplied,
	})
}

// writeServiceStateRefreshFailed reports the deliberately asymmetric result
// used by ordinary service operations: the host mutation is complete, but the
// cache still represents the previous verified scan.
func writeServiceStateRefreshFailed(w http.ResponseWriter) {
	writeServiceStateRefreshFailure(
		w,
		"service action completed, but the refreshed service state could not be verified",
		true,
	)
}

// writeRoundcubeStateRefreshFailed does not turn an idempotent already-absent
// result or a lost RPC response into proof that this request changed the host.
func writeRoundcubeStateRefreshFailed(w http.ResponseWriter, mutationApplied bool) {
	writeServiceStateRefreshFailure(
		w,
		"the Roundcube removal outcome and current service state could not be verified",
		mutationApplied,
	)
}

// writeServiceFirewallSyncFailed reports a verified partial success: the
// service state is fresh, while the firewall still needs operator attention.
// writeServiceFirewallSyncFailed, doğrulanmış kısmi başarıyı bildirir: servis
// durumu tazedir ancak güvenlik duvarı hâlâ operatör müdahalesi gerektirir.
func writeServiceFirewallSyncFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:           "service action completed, but the active firewall policy could not be synchronized",
		Code:            errCodeFirewallSyncFailed,
		Action:          "/services",
		PartialSuccess:  true,
		MutationApplied: true,
	})
}

// writeServiceMailFilterSyncFailed reports verified package state while making
// the unsafe mail-flow follow-up impossible to overlook.
// writeServiceMailFilterSyncFailed, doğrulanmış paket durumunu bildirirken
// güvenli olmayan posta akışı takibini gözden kaçırılamaz hâle getirir.
func writeServiceMailFilterSyncFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:           "service removal completed, but the Postfix mail-filter chain could not be synchronized",
		Code:            errCodeMailFilterSyncFailed,
		Action:          "/services",
		PartialSuccess:  true,
		MutationApplied: true,
	})
}

// writeServiceUninstallPartial reports a verified host mutation whose package
// purge did not finish. It never exposes package-manager output to the browser.
// writeServiceUninstallPartial, paket kaldırması tamamlanmayan doğrulanmış
// makine değişikliğini bildirir. Paket yöneticisi çıktısını tarayıcıya açmaz.
func writeServiceUninstallPartial(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:           "the service was stopped or disabled, but its packages could not be fully removed",
		Code:            errCodeServiceUninstallPartial,
		Action:          "/services",
		PartialSuccess:  true,
		MutationApplied: true,
	})
}

// writeWebmailUninstallPartial reports a fresh scan where Roundcube is no
// longer detected, while serving cleanup or durable lease finalization still
// needs operator attention. An already-absent tree is not an applied mutation.
func writeWebmailUninstallPartial(w http.ResponseWriter, mutationApplied bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:           "Roundcube removal is no longer detected, but webmail cleanup or durable finalization could not be fully verified",
		Code:            errCodeWebmailUninstallPartial,
		Action:          "/services",
		PartialSuccess:  true,
		MutationApplied: mutationApplied,
	})
}

// writeServerError logs the internal cause. Local platform-policy sentinels
// receive stable operator-facing codes; every other error remains a generic
// INTERNAL response.
func writeServerError(w http.ResponseWriter, err error) {
	if classification, ok := classifyStableAgentError(err); ok {
		log.Printf("[%d] %v", classification.Status, err)
		writeCodedError(
			w,
			classification.Status,
			classification.Code,
			classification.Message,
			"",
		)
		return
	}
	if err != nil {
		log.Printf("[500] %v", err)
	}
	writeCodedError(w, http.StatusInternalServerError, errCodeInternal, "internal server error", "")
}

// writeClientError returns a 4xx with a safe, explicit message. The
// message must be developer-authored, never derived from an internal
// error value.
// writeClientError güvenli, açık bir mesajla bir 4xx döndürür. Mesaj
// geliştirici tarafından yazılmalı, asla bir iç hata değerinden
// türetilmemelidir.
func writeClientError(w http.ResponseWriter, status int, message string) {
	writeCodedError(w, status, "", message, "")
}

// writeAgentError logs untrusted agent detail without returning it to the
// client, then delegates stable local-policy classification to writeServerError.
func writeAgentError(w http.ResponseWriter, err error, agentDetail string) {
	if agentDetail != "" {
		status := http.StatusInternalServerError
		if classification, ok := classifyStableAgentError(err); ok {
			status = classification.Status
		}
		log.Printf("[%d][agent] %s", status, boundedAgentDiagnostic(agentDetail))
	}
	writeServerError(w, err)
}

// boundedAgentDiagnostic is for server logs only. Agent strings can contain
// subprocess stderr, paths and terminal control characters; keep enough text
// for diagnosis without allowing one response to forge or flood log lines.
func boundedAgentDiagnostic(detail string) string {
	detail = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, detail)
	detail = strings.TrimSpace(detail)
	const maximum = 512
	runes := []rune(detail)
	if len(runes) > maximum {
		detail = string(runes[:maximum]) + "…"
	}
	return detail
}
