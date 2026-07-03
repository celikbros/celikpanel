package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

	token, err := p.sessions.Create(r.Context(), user.ID)
	if err != nil {
		writeServerError(w, err)
		return
	}

	http.SetCookie(w, p.sessionCookie(token, time.Now().Add(auth.SessionDuration)))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": user.Username,
		"role":     user.Role,
	})
}

// handleLogout deletes the current session and clears the cookie.
// handleLogout, mevcut oturumu siler ve çerezi temizler.
func (p *Panel) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		_ = p.sessions.Delete(r.Context(), cookie.Value)
	}
	// An already-expired cookie tells the browser to drop it.
	// Süresi geçmiş bir çerez, tarayıcıya onu bırakmasını söyler.
	http.SetCookie(w, p.sessionCookie("", time.Unix(0, 0)))
	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns the current user, used by the SPA to decide whether to
// show the login screen.
// handleMe, mevcut kullanıcıyı döndürür; SPA'nın giriş ekranını gösterip
// göstermeyeceğine karar vermek için kullanılır.
func (p *Panel) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := p.users.GetByID(r.Context(), currentUserID(r))
	if err != nil {
		writeClientError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": user.Username,
		"role":     user.Role,
		"email":    user.Email,
	})
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
	_, _ = rand.Read(raw)
	h, err := auth.HashPassword(hex.EncodeToString(raw))
	if err != nil {
		panic("failed to compute dummy password hash: " + err.Error())
	}
	return h
}
