package main

import (
	"context"
	"net/http"
	"strings"
)

// sessionCookieName is the cookie carrying the raw session token.
// sessionCookieName, ham oturum jetonunu taşıyan çerezdir.
const sessionCookieName = "celikpanel_session"

type contextKey string

const callerKey contextKey = "caller"

// requireAuth wraps the whole mux. Everything under /api requires a valid
// session except the login endpoint; static assets and SPA routes are
// public so the login screen itself can load.
//
// requireAuth tüm mux'ı sarar. /api altındaki her şey, giriş uç noktası
// dışında geçerli bir oturum gerektirir; statik dosyalar ve SPA rotaları
// herkese açıktır, böylece giriş ekranının kendisi yüklenebilir.
func (p *Panel) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r) {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}

		userID, err := p.sessions.Validate(r.Context(), cookie.Value)
		if err != nil {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}

		// Attach the caller (id + role) so handlers can enforce ownership.
		// Suspended accounts are cut off immediately, whatever session they
		// still hold.
		// Çağıranı (kimlik + rol) iliştir; böylece işleyiciler sahipliği
		// uygulayabilir. Askıya alınmış hesaplar, ellerinde hangi oturum
		// olursa olsun anında kesilir.
		// Fail closed: a request whose user record cannot be read never
		// proceeds with an empty role — an unreadable user is treated as an
		// invalid session, not as a roleless one.
		// Kapalı-varsayılan: kullanıcı kaydı okunamayan istek boş rolle asla
		// ilerlemez — okunamayan kullanıcı, rolsüz değil geçersiz oturumdur.
		u, err := p.users.GetByID(r.Context(), userID)
		if err != nil {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}
		if u.Status == "suspended" {
			writeCodedError(w, http.StatusForbidden, errCodeAccountSuspended, "account suspended", "")
			return
		}
		c := &Caller{ID: userID, Role: u.Role}

		// Server/OS-layer endpoints are administrator-only (ROLES.md: only the
		// admin touches services, config files, and infrastructure). Tenant
		// data (domains) is instead ownership-filtered inside its handlers.
		// Sunucu/OS-katmanı uç noktaları yalnızca yöneticiye açıktır (ROLES.md:
		// servisler, config dosyaları ve altyapıya yalnızca yönetici dokunur).
		// Kiracı verisi (domain'ler) ise kendi işleyicilerinde sahiplik
		// süzgecinden geçer.
		if isAdminOnlyPath(r.URL.Path) && c.Role != roleAdmin {
			writeCodedError(w, http.StatusForbidden, errCodeAdminOnly, "administrator access required", "")
			return
		}

		ctx := context.WithValue(r.Context(), callerKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath decides which requests skip authentication.
// isPublicPath, hangi isteklerin kimlik doğrulamayı atlayacağına karar verir.
func isPublicPath(r *http.Request) bool {
	// The database-tool proxy is NOT public: it fronts loopback-only web
	// apps and demands an authenticated admin session.
	// Veritabanı-aracı vekili herkese açık DEĞİLDİR: yalnız-loopback web
	// uygulamalarının önündedir ve kimlik doğrulamalı yönetici oturumu ister.
	if strings.HasPrefix(r.URL.Path, "/dbtool/") {
		return false
	}
	// Non-API paths are static files or SPA routes and must stay public.
	// API dışı yollar statik dosyalar ya da SPA rotalarıdır ve herkese
	// açık kalmalıdır.
	if !strings.HasPrefix(r.URL.Path, "/api") {
		return true
	}
	// The login and demo-credentials endpoints are public. Demo returns
	// nothing unless the server was started with --demo, so this is safe.
	// Giriş ve demo kimlik bilgileri uç noktaları herkese açıktır. Demo,
	// sunucu --demo ile başlatılmadıkça hiçbir şey döndürmez; bu yüzden
	// güvenlidir.
	// login/totp is the second step of an unauthenticated sign-in — it carries
	// the pending token, not a session, so it too must be public.
	// login/totp, kimlik-doğrulamasız girişin ikinci adımıdır — oturum değil
	// bekleme jetonu taşır; o da herkese açık olmalıdır.
	if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/demo" || r.URL.Path == "/api/v1/auth/login/totp" {
		return true
	}
	// Same-origin CORS preflight carries no credentials; let it through so
	// the handler's own OPTIONS branch can answer.
	// Aynı köken CORS ön kontrolü kimlik bilgisi taşımaz; işleyicinin kendi
	// OPTIONS dalının yanıtlayabilmesi için geçmesine izin ver.
	if r.Method == http.MethodOptions {
		return true
	}
	return false
}

// isAdminOnlyPath matches the server/OS-layer and infrastructure endpoints
// that only administrators may call. Read-only dashboard health
// (/api/v1/system/stats), auth, and the ownership-filtered domain routes are
// intentionally not listed.
// isAdminOnlyPath, yalnızca yöneticilerin çağırabileceği sunucu/OS-katmanı ve
// altyapı uç noktalarını eşler. Salt-okunur panel sağlığı
// (/api/v1/system/stats), kimlik doğrulama ve sahiplik-süzgeçli domain
// rotaları bilerek listelenmemiştir.
func isAdminOnlyPath(path string) bool {
	// /api/v2/ is deliberately NOT here anymore (B1 role split, Jul 17):
	// its DB/user/grant endpoints are tenant-scoped inside their handlers
	// (callerSubscriptionID + canAccessDBServer); only SERVER REGISTRATION
	// stays admin, enforced in its own handlers — registering an arbitrary
	// host/port/root-password is infrastructure, not tenant self-service.
	// /dbtool/ likewise: any authenticated session may reach the proxy; the
	// tools' own database-credential login is the real authorization layer.
	// /api/v2/ artık bilerek burada DEĞİL (B1 rol ayrımı, 17 Tem): DB/
	// kullanıcı/grant uçları kendi handler'larında kiracı-kapsamlı
	// (callerSubscriptionID + canAccessDBServer); yalnız SUNUCU KAYDI admin
	// kalır ve kendi handler'ında uygulanır — keyfi host/port/root-parola
	// kaydı kiracı self-servisi değil altyapıdır. /dbtool/ de öyle: vekile
	// kimlikli her oturum ulaşabilir; gerçek yetki katmanı araçların kendi
	// veritabanı-kimlik girişidir.
	adminPrefixes := []string{
		"/api/v1/config",
		"/api/v1/dovecot/",
		"/api/v1/fail2ban/",
		"/api/v1/firewall",
		"/api/v1/import/",
		"/api/v1/mail/",
		"/api/v1/audit-logs",
		"/api/v1/dashboard",
		"/api/v1/managed-services",
		"/api/v1/nginx/",
		"/api/v1/panel/",
		"/api/v1/pdns/",
		"/api/v1/php/",
		"/api/v1/postfix/",
		"/api/v1/repo",
		"/api/v1/service/",
		"/api/v1/system/check",
	}
	for _, prefix := range adminPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// currentCaller returns the authenticated caller, or nil if none.
// currentCaller, kimliği doğrulanmış çağıranı, yoksa nil döndürür.
func currentCaller(r *http.Request) *Caller {
	if c, ok := r.Context().Value(callerKey).(*Caller); ok {
		return c
	}
	return nil
}

// currentUserID returns the authenticated user's ID, or 0 if none.
// currentUserID, kimliği doğrulanmış kullanıcının kimliğini, yoksa 0
// döndürür.
func currentUserID(r *http.Request) int {
	if c := currentCaller(r); c != nil {
		return c.ID
	}
	return 0
}
