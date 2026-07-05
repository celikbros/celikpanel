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
			writeClientError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		userID, err := p.sessions.Validate(r.Context(), cookie.Value)
		if err != nil {
			writeClientError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		// Attach the caller (id + role) so handlers can enforce ownership.
		// Suspended accounts are cut off immediately, whatever session they
		// still hold.
		// Çağıranı (kimlik + rol) iliştir; böylece işleyiciler sahipliği
		// uygulayabilir. Askıya alınmış hesaplar, ellerinde hangi oturum
		// olursa olsun anında kesilir.
		c := &Caller{ID: userID}
		if u, err := p.users.GetByID(r.Context(), userID); err == nil {
			if u.Status == "suspended" {
				writeClientError(w, http.StatusForbidden, "account suspended")
				return
			}
			c.Role = u.Role
		}

		// Server/OS-layer endpoints are administrator-only (ROLES.md: only the
		// admin touches services, config files, and infrastructure). Tenant
		// data (domains) is instead ownership-filtered inside its handlers.
		// Sunucu/OS-katmanı uç noktaları yalnızca yöneticiye açıktır (ROLES.md:
		// servisler, config dosyaları ve altyapıya yalnızca yönetici dokunur).
		// Kiracı verisi (domain'ler) ise kendi işleyicilerinde sahiplik
		// süzgecinden geçer.
		if isAdminOnlyPath(r.URL.Path) && c.Role != roleAdmin {
			writeClientError(w, http.StatusForbidden, "administrator access required")
			return
		}

		ctx := context.WithValue(r.Context(), callerKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath decides which requests skip authentication.
// isPublicPath, hangi isteklerin kimlik doğrulamayı atlayacağına karar verir.
func isPublicPath(r *http.Request) bool {
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
	if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/demo" {
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
	adminPrefixes := []string{
		"/api/v1/config",
		"/api/v1/dovecot/",
		"/api/v1/fail2ban/",
		"/api/v1/managed-services",
		"/api/v1/nginx/",
		"/api/v1/pdns/",
		"/api/v1/php/",
		"/api/v1/runtimes/",
		"/api/v1/postfix/",
		"/api/v1/service/",
		"/api/v1/system/check",
		"/api/v2/",
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
