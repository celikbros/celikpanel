package main

import (
	"encoding/json"
	"log"
	"net/http"
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
	Error  string `json:"error"`
	Code   string `json:"code,omitempty"`
	Action string `json:"action,omitempty"`
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
	errCodeInternal          = "INTERNAL"
	errCodeAuthRequired      = "AUTH_REQUIRED"
	errCodeAdminOnly         = "ADMIN_ONLY"
	errCodeAccountSuspended  = "ACCOUNT_SUSPENDED"
	errCodeDNSServerRequired = "DNS_SERVER_REQUIRED"
	errCodeWebServerRequired = "WEB_SERVER_REQUIRED"
	errCodePHPRequired       = "PHP_REQUIRED"
	errCodeNoSubscription    = "NO_SUBSCRIPTION"
	errCodeQuotaDomains      = "QUOTA_DOMAINS_EXCEEDED"
	errCodeQuotaDisk         = "QUOTA_DISK_EXCEEDED"
	errCodeEntitlement       = "ENTITLEMENT_REQUIRED"
	errCodeFirewallNoEngine  = "FIREWALL_ENGINE_MISSING"
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

// writeServerError logs the internal error and returns a generic 500.
// writeServerError iç hatayı log'lar ve genel bir 500 döndürür.
func writeServerError(w http.ResponseWriter, err error) {
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

// writeAgentError handles a failed agent RPC. The agent's error string may
// carry command output and paths, so it is logged, never returned; the
// transport error (if any) is logged too, and the client gets a generic
// 500.
// writeAgentError, başarısız bir agent RPC'sini ele alır. Agent'ın hata
// metni komut çıktısı ve yollar taşıyabilir; bu yüzden log'lanır, asla
// döndürülmez; taşıma hatası (varsa) da log'lanır ve istemci genel bir
// 500 alır.
func writeAgentError(w http.ResponseWriter, err error, agentDetail string) {
	if agentDetail != "" {
		log.Printf("[500][agent] %s", agentDetail)
	}
	writeServerError(w, err)
}
