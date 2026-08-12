package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
)

// loginRequest is the JSON body of a login attempt.
// loginRequest, bir giriş denemesinin JSON gövdesidir.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleLogin verifies credentials and, on success, sets the session
// cookie. Failures return an identical generic message and status so the
// endpoint does not reveal whether a username exists.
//
// handleLogin, kimlik bilgilerini doğrular ve başarılı olursa oturum
// çerezini ayarlar. Başarısızlıklar aynı genel mesaj ve durumu döndürür;
// böylece uç nokta bir kullanıcı adının var olup olmadığını açığa vurmaz.
func (p *Panel) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Throttle credential guessing before doing any work.
	// Herhangi bir iş yapmadan önce kimlik bilgisi tahminini kısıtla.
	if !p.loginLimiter.allow(clientIP(r)) {
		writeClientError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := p.users.GetByUsername(r.Context(), req.Username)
	if err != nil {
		// Run a verify against a dummy hash anyway to keep timing uniform,
		// then fail. Kullanıcı yoksa da zamanlamayı eşit tutmak için sahte
		// bir özete karşı doğrulama çalıştır, sonra başarısız ol.
		_, _ = auth.VerifyPassword(req.Password, dummyHash)
		writeClientError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if !ok {
		writeClientError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	// Second factor: if the account has 2FA on, the password alone does not
	// grant a session. Hand back a short-lived pending token; the session is
	// issued by /auth/login/totp once a valid code arrives.
	// İkinci faktör: hesabın 2FA'sı açıksa parola tek başına oturum vermez.
	// Kısa ömürlü bir bekleme jetonu döndür; oturum, geçerli bir kod gelince
	// /auth/login/totp tarafından verilir.
	state, err := p.userAuthState(r.Context(), user.ID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.passwordHash != user.PasswordHash {
		writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
		return
	}
	if state.status == "suspended" {
		writeClientError(w, http.StatusForbidden, "account suspended")
		return
	}
	identity, err := p.canonicalAuthIdentity(r.Context(), user.ID)
	if err != nil || !state.matchesCanonical(identity) {
		writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
		return
	}
	if state.totpEnabled {
		pendingToken, err := newPendingToken(state)
		if err != nil {
			writeServerError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totp_required": true,
			"pending_token": pendingToken,
		})
		return
	}

	token, err := p.sessions.CreateForAuthEpoch(r.Context(), user.ID, state.authEpoch, false)
	if err != nil {
		if errors.Is(err, auth.ErrAuthStateChanged) {
			writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
			return
		}
		writeServerError(w, err)
		return
	}

	http.SetCookie(w, p.sessionCookie(token, time.Now().Add(auth.SessionDuration)))

	// Record the sign-in against the user (the session is fresh, so the
	// caller-based audit cannot resolve them yet).
	// Girişi kullanıcıya kaydet (oturum yeni; çağıran-tabanlı denetim onu
	// henüz çözemez).
	p.auditAs(r, user.ID, "auth.login", "", 0)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(identity.response(false))
}

// handleLogout deletes the current session and clears the cookie.
// handleLogout, mevcut oturumu siler ve çerezi temizler.
func (p *Panel) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var deleteErr error
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		deleteErr = p.sessions.Delete(r.Context(), cookie.Value)
	}
	// An already-expired cookie tells the browser to drop it.
	// Süresi geçmiş bir çerez, tarayıcıya onu bırakmasını söyler.
	http.SetCookie(w, p.sessionCookie("", time.Unix(0, 0)))
	if deleteErr != nil {
		// The browser token is still cleared, but do not claim a complete
		// logout when the server-side credential could not be revoked.
		// Tarayıcı jetonu yine temizlenir; fakat sunucu tarafındaki kimlik
		// bilgisi iptal edilemediyse çıkış tamamlandı diye davranma.
		writeServerError(w, deleteErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the current user, used by the SPA to decide whether to
// show the login screen.
// handleMe, mevcut kullanıcıyı döndürür; SPA'nın giriş ekranını gösterip
// göstermeyeceğine karar vermek için kullanılır.
func (p *Panel) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	caller := currentCaller(r)
	if caller == nil || !caller.validAuthorizationIdentity() {
		writeClientError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	identity, err := p.canonicalAuthIdentity(r.Context(), caller.ID)
	if err != nil {
		writeClientError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if identity.identity.UserID != caller.ID || identity.identity.Role != caller.Role ||
		identity.accountType != caller.normalizedAccountType() ||
		identity.identity.CustomerID != caller.CustomerID {
		writeClientError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// Tell the SPA when this session is an impersonation so it can show the
	// "return to my account" banner.
	// Bu oturum bir taklit oturumuysa SPA'ya söyle; "hesabıma dön" şeridini
	// gösterebilsin.
	impersonating := false
	if _, err := r.Cookie(impersonatorCookieName); err == nil {
		impersonating = true
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(identity.response(impersonating))
}

// sessionCookie builds the session cookie with security attributes. Secure
// is set when the request/response is served over TLS in production; we
// default it on and rely on SameSite for the HTTP dev case.
// sessionCookie, güvenlik nitelikleriyle oturum çerezini oluşturur.
func (p *Panel) sessionCookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   p.secureCookies,
		Expires:  expires,
	}
}

// dummyHash is a valid argon2id hash of a random password nobody knows.
// Verifying against it when the username does not exist keeps login timing
// uniform, so the endpoint does not leak account existence via response
// time. It is computed once at startup.
//
// dummyHash, kimsenin bilmediği rastgele bir parolanın geçerli bir
// argon2id özetidir. Kullanıcı adı yokken buna karşı doğrulama, giriş
// zamanlamasını eşit tutar; böylece uç nokta yanıt süresiyle hesabın
// varlığını sızdırmaz. Başlangıçta bir kez hesaplanır.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		panic("failed to generate dummy password: " + err.Error())
	}
	h, err := auth.HashPassword(hex.EncodeToString(raw))
	if err != nil {
		panic("failed to compute dummy password hash: " + err.Error())
	}
	return h
}
