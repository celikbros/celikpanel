package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
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
		// API preflights never need application data. Terminate them here so an
		// unauthenticated OPTIONS request cannot reach a handler that happens to
		// omit its own method check and disclose the protected GET response.
		if r.Method == http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api/") {
			w.WriteHeader(http.StatusNoContent)
			return
		}

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
		if err != nil || u == nil {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}
		if u.Status == "suspended" {
			writeCodedError(w, http.StatusForbidden, errCodeAccountSuspended, "account suspended", "")
			return
		}
		if u.Status != "active" {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}

		identity, ok := u.EffectiveIdentity()
		if !ok || identity.UserID != userID {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}

		accountType := u.AccountType
		if accountType == "" {
			accountType = core.AccountTypeAccount
		}

		// An additional-user marker is only meaningful while it points to a
		// live, real customer account. Validate the parent through the
		// repository on every authenticated request so a deleted, suspended or
		// retyped parent immediately revokes the member's effective identity.
		if accountType == core.AccountTypeAdditionalUser {
			parent, parentErr := p.users.GetByID(r.Context(), identity.CustomerID)
			if parentErr != nil || parent == nil {
				writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
				return
			}
			if parent.Status == "suspended" {
				writeCodedError(w, http.StatusForbidden, errCodeAccountSuspended, "account suspended", "")
				return
			}
			parentIdentity, parentOK := parent.EffectiveIdentity()
			parentType := parent.AccountType
			if parentType == "" {
				parentType = core.AccountTypeAccount
			}
			if parent.Status != "active" || !parentOK ||
				parentType != core.AccountTypeAccount ||
				parentIdentity.UserID != identity.CustomerID ||
				parentIdentity.CustomerID != identity.CustomerID ||
				parentIdentity.Role != roleCustomer {
				writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
				return
			}
		}

		c := &Caller{
			ID:          identity.UserID,
			Role:        identity.Role,
			AccountType: accountType,
			CustomerID:  identity.CustomerID,
		}
		if !c.validAuthorizationIdentity() {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}

		// Additional users are tenant-scoped team members. Server-global and
		// account-management surfaces are not inherited from a parent customer.
		if c.isAdditionalUser() && isAdditionalUserRestrictedPath(r.URL.Path) {
			writeCodedError(w, http.StatusForbidden, errCodeAdditionalUserScope, "resource is unavailable to additional users", "")
			return
		}

		// Server/OS-layer endpoints are administrator-only (ROLES.md: only the
		// admin touches services, config files, and infrastructure). Tenant
		// data (domains) is instead ownership-filtered inside its handlers.
		// Sunucu/OS-katmanı uç noktaları yalnızca yöneticiye açıktır (ROLES.md:
		// servisler, config dosyaları ve altyapıya yalnızca yönetici dokunur).
		// Kiracı verisi (domain'ler) ise kendi işleyicilerinde sahiplik
		// süzgecinden geçer.
		if isAdminOnlyPath(r.URL.Path) && !c.hasAccountRole(roleAdmin) {
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
	// apps and demands an authenticated, authorized session.
	// Veritabanı-aracı vekili herkese açık DEĞİLDİR: yalnız-loopback web
	// uygulamalarının önündedir ve kimliği doğrulanmış, yetkili oturum ister.
	if r.URL.Path == "/dbtool" || strings.HasPrefix(r.URL.Path, "/dbtool/") {
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
	return false
}

// isAdminOnlyPath matches the server/OS-layer and infrastructure endpoints
// that only administrators may call. Read-only dashboard health, auth, and
// the ownership-filtered domain routes are intentionally not listed here.
// Additional-user restrictions for server-global read-only data are enforced
// separately so reseller and customer account behavior remains unchanged.
// isAdminOnlyPath, yalnızca yöneticilerin çağırabileceği sunucu/OS-katmanı ve
// altyapı uç noktalarını eşler. Salt-okunur panel sağlığı
// (/api/v1/system/stats), kimlik doğrulama ve sahiplik-süzgeçli domain
// rotaları bilerek listelenmemiştir.
func isAdminOnlyPath(path string) bool {
	if path == "/api/v1/system-databases" || strings.HasPrefix(path, "/api/v1/system-databases/") {
		return true
	}
	if path == storeCatalogAdminPath || strings.HasPrefix(path, storeCatalogAdminPath+"/") {
		return true
	}
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
		"/api/v1/metrics/",
		"/api/v1/nginx/",
		"/api/v1/panel/",
		"/api/v1/pdns/",
		"/api/v1/php/",
		"/api/v1/postfix/",
		"/api/v1/repo",
		"/api/v1/service/",
		"/api/v1/security/",
		"/api/v1/system/check",
	}
	for _, prefix := range adminPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// isAdditionalUserRestrictedPath centralizes routes that tenant-scoped team
// members must never inherit from their parent customer account. It covers
// server-global inventory and account, billing, VPN, and database-tool
// management surfaces. Real admin, reseller, and customer accounts bypass
// this additional-user-only guard and retain their existing handler rules.
func isAdditionalUserRestrictedPath(path string) bool {
	switch path {
	case "/api/v1/system/stats",
		"/api/v1/hosting/capabilities",
		"/api/v1/products",
		"/api/v1/domains/create":
		return true
	}

	for _, root := range []string{
		"/api/v1/store",
		"/api/v1/users",
		"/api/v1/team-members",
		"/api/v1/plans",
		"/api/v1/subscriptions",
		"/api/v1/vpn",
		"/api/v1/runtimes",
		"/dbtool",
	} {
		if path == root || strings.HasPrefix(path, root+"/") {
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
