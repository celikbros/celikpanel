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
)

// writeCodedError is the single writer of the contract. action, when
// non-empty, is an in-panel path that fixes the refusal (e.g. "/services").
// writeCodedError, sözleşmenin tek yazıcısıdır. action boş değilse reti
// düzelten panel-içi yoldur (örn. "/services").
func writeCodedError(w http.ResponseWriter, status int, code, message, action string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: message, Code: code, Action: action})
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
