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

const userIDKey contextKey = "userID"

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

		ctx := context.WithValue(r.Context(), userIDKey, userID)
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
	// The login endpoint is the only public API route.
	// Giriş uç noktası, herkese açık tek API rotasıdır.
	if r.URL.Path == "/api/v1/auth/login" {
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

// currentUserID returns the authenticated user's ID, or 0 if none.
// currentUserID, kimliği doğrulanmış kullanıcının kimliğini, yoksa 0
// döndürür.
func currentUserID(r *http.Request) int {
	if id, ok := r.Context().Value(userIDKey).(int); ok {
		return id
	}
	return 0
}
