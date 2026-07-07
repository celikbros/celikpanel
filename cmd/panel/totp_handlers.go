package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
)

// Two-factor sign-in. When a user has 2FA enabled, a correct password is only
// step one: the login returns a short-lived pending token instead of a
// session, and the session is issued only after a valid TOTP code. The
// pending tokens live in memory with a tight expiry — losing them on restart
// just means re-entering the password, never a lockout.
//
// İki faktörlü giriş. Bir kullanıcının 2FA'sı açıksa, doğru parola yalnız
// birinci adımdır: giriş, oturum yerine kısa ömürlü bir bekleme jetonu
// döndürür ve oturum ancak geçerli bir TOTP kodundan sonra verilir. Bekleme
// jetonları bellekte sıkı bir süreyle yaşar — yeniden başlatmada kaybolmaları
// yalnız parolayı yeniden girmek demektir, asla kilitlenme değil.

type pendingLogin struct {
	userID  int
	expires time.Time
}

var (
	pendingMu    sync.Mutex
	pendingStore = map[string]pendingLogin{}
)

const pendingTOTPTTL = 5 * time.Minute

func newPendingToken(userID int) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	pendingMu.Lock()
	// Opportunistically drop expired entries so the map cannot grow forever.
	// Süresi dolmuşları fırsatçı düşür ki harita sonsuza dek büyümesin.
	now := time.Now()
	for k, v := range pendingStore {
		if now.After(v.expires) {
			delete(pendingStore, k)
		}
	}
	pendingStore[tok] = pendingLogin{userID: userID, expires: now.Add(pendingTOTPTTL)}
	pendingMu.Unlock()
	return tok
}

func consumePendingToken(tok string) (int, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	p, ok := pendingStore[tok]
	if !ok || time.Now().After(p.expires) {
		delete(pendingStore, tok)
		return 0, false
	}
	delete(pendingStore, tok)
	return p.userID, true
}

// userTOTP reads a user's 2FA secret and enabled flag.
// userTOTP, bir kullanıcının 2FA anahtarını ve etkin bayrağını okur.
func (p *Panel) userTOTP(ctx context.Context, userID int) (secret string, enabled bool) {
	var s *string
	var en int
	_ = p.db.GetDB().QueryRowContext(ctx,
		`SELECT totp_secret, totp_enabled FROM users WHERE id = ?`, userID).Scan(&s, &en)
	if s != nil {
		secret = *s
	}
	return secret, en == 1
}

// handleLoginTOTP completes a two-step sign-in: a valid pending token plus a
// valid current code yields a session.
// handleLoginTOTP, iki adımlı girişi tamamlar: geçerli bir bekleme jetonu artı
// geçerli güncel bir kod bir oturum verir.
func (p *Panel) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !p.loginLimiter.allow(clientIP(r)) {
		writeClientError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	var req struct {
		PendingToken string `json:"pending_token"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, ok := consumePendingToken(req.PendingToken)
	if !ok {
		writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
		return
	}
	secret, enabled := p.userTOTP(r.Context(), userID)
	if !enabled || !auth.ValidateTOTP(secret, req.Code) {
		writeClientError(w, http.StatusUnauthorized, "invalid code")
		return
	}
	user, err := p.users.GetByID(r.Context(), userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	token, err := p.sessions.Create(r.Context(), userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	http.SetCookie(w, p.sessionCookie(token, time.Now().Add(auth.SessionDuration)))
	p.auditAs(r, userID, "auth.login.2fa", "", 0)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"username": user.Username, "role": user.Role})
}

// handle2FA routes the self-service 2FA endpoints for the signed-in user.
// handle2FA, giriş yapmış kullanıcı için self-servis 2FA uçlarını yönlendirir.
func (p *Panel) handle2FA(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := currentCaller(r)
	if c == nil {
		writeClientError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	switch r.URL.Path {
	case "/api/v1/auth/2fa/status":
		_, enabled := p.userTOTP(r.Context(), c.ID)
		json.NewEncoder(w).Encode(map[string]any{"enabled": enabled})
	case "/api/v1/auth/2fa/setup":
		p.handle2FASetup(w, r, c.ID)
	case "/api/v1/auth/2fa/enable":
		p.handle2FAEnable(w, r, c.ID)
	case "/api/v1/auth/2fa/disable":
		p.handle2FADisable(w, r, c.ID)
	default:
		http.NotFound(w, r)
	}
}

// handle2FASetup generates a fresh secret (stored but not yet enforced) and
// returns the otpauth URI plus the secret for manual entry.
// handle2FASetup, taze bir anahtar üretir (saklanır ama henüz zorlanmaz) ve
// otpauth URI'siyle elle giriş için anahtarı döndürür.
func (p *Panel) handle2FASetup(w http.ResponseWriter, r *http.Request, userID int) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if _, err := p.db.GetDB().ExecContext(r.Context(),
		`UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE id = ?`, secret, userID); err != nil {
		writeServerError(w, err)
		return
	}
	user, _ := p.users.GetByID(r.Context(), userID)
	account := "user"
	if user != nil {
		account = user.Username
	}
	json.NewEncoder(w).Encode(map[string]any{
		"secret": secret,
		"uri":    auth.TOTPURI(secret, account, "CelikPanel"),
	})
}

// handle2FAEnable verifies the first code against the pending secret and, on
// success, turns enforcement on.
// handle2FAEnable, ilk kodu bekleyen anahtara karşı doğrular ve başarıda
// zorlamayı açar.
func (p *Panel) handle2FAEnable(w http.ResponseWriter, r *http.Request, userID int) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	secret, _ := p.userTOTP(r.Context(), userID)
	if secret == "" || !auth.ValidateTOTP(secret, req.Code) {
		writeClientError(w, http.StatusBadRequest, "invalid code — check your authenticator and try again")
		return
	}
	if _, err := p.db.GetDB().ExecContext(r.Context(),
		`UPDATE users SET totp_enabled = 1 WHERE id = ?`, userID); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "2fa.enable", "user", userID)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handle2FADisable turns 2FA off, requiring the account password and a current
// code so a hijacked session alone cannot remove the second factor.
// handle2FADisable, 2FA'yı kapatır; kaçırılmış bir oturumun tek başına ikinci
// faktörü kaldıramaması için hesap parolası ve güncel bir kod ister.
func (p *Panel) handle2FADisable(w http.ResponseWriter, r *http.Request, userID int) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := p.users.GetByID(r.Context(), userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	ok, _ := auth.VerifyPassword(req.Password, user.PasswordHash)
	secret, _ := p.userTOTP(r.Context(), userID)
	if !ok || !auth.ValidateTOTP(secret, req.Code) {
		writeClientError(w, http.StatusUnauthorized, "password or code incorrect")
		return
	}
	if _, err := p.db.GetDB().ExecContext(r.Context(),
		`UPDATE users SET totp_enabled = 0, totp_secret = NULL WHERE id = ?`, userID); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "2fa.disable", "user", userID)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}
