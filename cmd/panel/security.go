package main

import (
	"net/http"
	"net/url"
)

// securityHeaders sets defensive response headers on every request. HSTS
// is only sent when the panel is served over TLS (tracked by
// secureCookies), since sending it over plain HTTP would be meaningless
// or harmful during local development.
//
// securityHeaders her istekte savunmacı yanıt başlıkları ayarlar. HSTS
// yalnızca panel TLS üzerinden sunulduğunda (secureCookies ile izlenir)
// gönderilir; düz HTTP üzerinde göndermek yerel geliştirmede anlamsız ya
// da zararlı olurdu.
func securityHeaders(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// The SPA is fully same-origin: scripts and XHR only from 'self'.
		// Tailwind and React set inline styles, so style-src needs
		// 'unsafe-inline'; script-src deliberately does not.
		// SPA tümüyle aynı köken: script ve XHR yalnızca 'self'. Tailwind
		// ve React satır içi stil koyduğundan style-src 'unsafe-inline'
		// gerektirir; script-src bilinçli olarak gerektirmez.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; "+
				"base-uri 'self'; form-action 'self'")
		if secure {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// csrfProtect blocks cross-origin state-changing requests. Combined with
// the SameSite=Lax session cookie it gives two independent CSRF defenses.
// Safe methods (GET/HEAD/OPTIONS) pass through; unsafe methods must carry
// an Origin (or Referer) whose host matches this server.
//
// csrfProtect, köken-dışı durum değiştiren istekleri engeller. SameSite=Lax
// oturum çereziyle birlikte iki bağımsız CSRF savunması sağlar. Güvenli
// metotlar (GET/HEAD/OPTIONS) geçer; güvensiz metotlar, host'u bu sunucuyla
// eşleşen bir Origin (ya da Referer) taşımalıdır.
func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(r) {
			writeClientError(w, http.StatusForbidden, "cross-origin request blocked")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the request's Origin/Referer host matches the
// server's host. A request with neither header is treated as cross-origin
// and rejected for unsafe methods — a browser cannot be tricked into
// omitting both, so this only affects non-browser clients, which should
// send an explicit Origin.
//
// sameOrigin, isteğin Origin/Referer host'unun sunucunun host'uyla eşleşip
// eşleşmediğini bildirir. İki başlığı da olmayan bir istek köken-dışı
// sayılır ve güvensiz metotlar için reddedilir.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}
