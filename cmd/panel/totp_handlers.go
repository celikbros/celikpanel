package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	secretstore "github.com/alicelik/celikpanel/internal/secrets"
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

type userAuthState struct {
	userID       int
	username     string
	role         string
	accountType  core.AccountType
	parentID     sql.NullInt64
	passwordHash string
	status       string
	totpSecret   string
	totpEnabled  bool
	authEpoch    int64
}

func (s userAuthState) binding() [sha256.Size]byte {
	return sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%q:%q:%q:%t:%d:%q:%q:%q:%t",
		s.userID, s.authEpoch, s.username, s.role, s.accountType, s.parentID.Valid, s.parentID.Int64,
		s.passwordHash, s.status, s.totpSecret, s.totpEnabled)))
}

func (s userAuthState) matchesCanonical(identity canonicalAuthIdentity) bool {
	if identity.user == nil ||
		s.userID != identity.user.ID ||
		s.username != identity.user.Username ||
		s.role != identity.user.Role ||
		s.accountType != normalizedStoredAccountType(identity.user) {
		return false
	}
	if identity.user.ParentID == nil {
		return !s.parentID.Valid
	}
	return s.parentID.Valid && s.parentID.Int64 == int64(*identity.user.ParentID)
}

type pendingLogin struct {
	userID    int
	authEpoch int64
	binding   [sha256.Size]byte
	expires   time.Time
}

var (
	pendingMu    sync.Mutex
	pendingStore = map[string]pendingLogin{}
)

const pendingTOTPTTL = 5 * time.Minute

func newPendingToken(state userAuthState) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate pending login token: %w", err)
	}
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
	pendingStore[tok] = pendingLogin{
		userID: state.userID, authEpoch: state.authEpoch,
		binding: state.binding(), expires: now.Add(pendingTOTPTTL),
	}
	pendingMu.Unlock()
	return tok, nil
}

func consumePendingToken(tok string) (pendingLogin, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	p, ok := pendingStore[tok]
	if !ok || time.Now().After(p.expires) {
		delete(pendingStore, tok)
		return pendingLogin{}, false
	}
	delete(pendingStore, tok)
	return p, true
}

func revokePendingLogins(userID int) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	for token, pending := range pendingStore {
		if pending.userID == userID {
			delete(pendingStore, token)
		}
	}
}

// userTOTP reads a user's 2FA secret and enabled flag.
// userTOTP, bir kullanıcının 2FA anahtarını ve etkin bayrağını okur.
func (p *Panel) userAuthState(ctx context.Context, userID int) (userAuthState, error) {
	state := userAuthState{userID: userID}
	var stored *string
	var enabled int
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT username, role, COALESCE(NULLIF(account_type, ''), 'account'), parent_id,
		       password_hash, COALESCE(status, 'active'),
		       totp_secret, totp_enabled, auth_epoch
		FROM users WHERE id = ?`, userID).Scan(
		&state.username, &state.role, &state.accountType, &state.parentID,
		&state.passwordHash, &state.status,
		&stored, &enabled, &state.authEpoch,
	); err != nil {
		return userAuthState{}, fmt.Errorf("read authentication state: %w", err)
	}
	state.totpEnabled = enabled == 1
	if stored == nil || *stored == "" {
		if state.totpEnabled {
			return userAuthState{}, fmt.Errorf("read TOTP state: enabled account has no secret")
		}
		return state, nil
	}
	if p.secrets == nil {
		return userAuthState{}, fmt.Errorf("read TOTP state: secret box unavailable")
	}

	secret, err := p.secrets.Decrypt(*stored)
	if err != nil {
		return userAuthState{}, fmt.Errorf("decrypt TOTP secret: %w", err)
	}
	if !auth.ValidateTOTPSecret(secret) {
		return userAuthState{}, fmt.Errorf("read TOTP state: invalid secret")
	}
	state.totpSecret = secret

	// Startup migrates all rows. This compare-and-swap remains as defense in
	// depth for a legacy row introduced while the process is already running.
	if !secretstore.IsEncrypted(*stored) {
		sealed, err := p.secrets.Encrypt(secret)
		if err != nil {
			return userAuthState{}, fmt.Errorf("encrypt legacy TOTP secret: %w", err)
		}
		result, err := p.db.GetDB().ExecContext(ctx,
			`UPDATE users SET totp_secret = ? WHERE id = ? AND totp_secret = ?`, sealed, userID, *stored)
		if err != nil {
			return userAuthState{}, fmt.Errorf("seal legacy TOTP secret: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return userAuthState{}, fmt.Errorf("seal legacy TOTP secret: rows affected: %w", err)
		}
		if affected != 1 {
			return userAuthState{}, fmt.Errorf("seal legacy TOTP secret: concurrent update")
		}
	}
	return state, nil
}

func (p *Panel) userTOTP(ctx context.Context, userID int) (secret string, enabled bool, err error) {
	state, err := p.userAuthState(ctx, userID)
	if err != nil {
		return "", false, err
	}
	return state.totpSecret, state.totpEnabled, nil
}

type totpStateChange uint8

const (
	totpStateSetup totpStateChange = iota
	totpStateEnable
	totpStateDisable
)

var errTOTPStateChanged = errors.New("two-factor state changed")

// changeTOTPStateAndRevokeSessions makes the credential change and revokes
// every pre-change session in one SQLite transaction. A failed revocation must
// never leave a half-applied TOTP state behind.
func (p *Panel) changeTOTPStateAndRevokeSessions(
	ctx context.Context,
	userID int,
	expectedEpoch int64,
	change totpStateChange,
	storedSecret string,
) (int64, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin TOTP state change: %w", err)
	}
	defer tx.Rollback()

	var result sql.Result
	switch change {
	case totpStateSetup:
		result, err = tx.ExecContext(ctx, `
			UPDATE users
			SET totp_secret = ?, totp_enabled = 0, auth_epoch = auth_epoch + 1
			WHERE id = ? AND totp_enabled = 0 AND auth_epoch = ?`, storedSecret, userID, expectedEpoch)
	case totpStateEnable:
		result, err = tx.ExecContext(ctx, `
			UPDATE users
			SET totp_enabled = 1, auth_epoch = auth_epoch + 1
			WHERE id = ? AND totp_enabled = 0 AND auth_epoch = ?`, userID, expectedEpoch)
	case totpStateDisable:
		result, err = tx.ExecContext(ctx, `
			UPDATE users
			SET totp_enabled = 0, totp_secret = NULL, auth_epoch = auth_epoch + 1
			WHERE id = ? AND totp_enabled = 1 AND auth_epoch = ?`, userID, expectedEpoch)
	default:
		return 0, fmt.Errorf("unsupported TOTP state change")
	}
	if err != nil {
		return 0, fmt.Errorf("update TOTP state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirm TOTP state change: %w", err)
	}
	if affected != 1 {
		return 0, errTOTPStateChanged
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return 0, fmt.Errorf("revoke sessions after TOTP state change: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit TOTP state change: %w", err)
	}
	return expectedEpoch + 1, nil
}

// replaceSessionAfterTOTPChange rotates the authenticated browser onto the
// new credential epoch. All old sessions were deleted by the transaction;
// only this freshly reauthenticated browser receives a replacement.
func (p *Panel) replaceSessionAfterTOTPChange(
	ctx context.Context,
	w http.ResponseWriter,
	userID int,
	authEpoch int64,
	requireTOTP bool,
) error {
	token, err := p.sessions.CreateForAuthEpoch(ctx, userID, authEpoch, requireTOTP)
	if err != nil {
		return fmt.Errorf("replace session after TOTP state change: %w", err)
	}
	http.SetCookie(w, p.sessionCookie(token, time.Now().Add(auth.SessionDuration)))
	return nil
}

func (p *Panel) allowSensitiveAuthAttempt(r *http.Request, userID int, action string) bool {
	if p.loginLimiter == nil {
		return true
	}
	return p.loginLimiter.allow(fmt.Sprintf("%s:%d:%s", action, userID, clientIP(r)))
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
	pending, ok := consumePendingToken(req.PendingToken)
	if !ok {
		writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
		return
	}
	state, err := p.userAuthState(r.Context(), pending.userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.status == "suspended" {
		writeClientError(w, http.StatusForbidden, "account suspended")
		return
	}
	currentBinding := state.binding()
	if state.authEpoch != pending.authEpoch ||
		subtle.ConstantTimeCompare(currentBinding[:], pending.binding[:]) != 1 {
		writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
		return
	}
	identity, err := p.canonicalAuthIdentity(r.Context(), pending.userID)
	if err != nil || !state.matchesCanonical(identity) {
		writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
		return
	}
	if !state.totpEnabled || !auth.ValidateTOTP(state.totpSecret, req.Code) {
		writeClientError(w, http.StatusUnauthorized, "invalid code")
		return
	}
	token, err := p.sessions.CreateForAuthEpoch(r.Context(), pending.userID, pending.authEpoch, true)
	if err != nil {
		if errors.Is(err, auth.ErrAuthStateChanged) {
			writeClientError(w, http.StatusUnauthorized, "sign-in expired, start again")
			return
		}
		writeServerError(w, err)
		return
	}
	http.SetCookie(w, p.sessionCookie(token, time.Now().Add(auth.SessionDuration)))
	p.auditAs(r, pending.userID, "auth.login.2fa", "", 0)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(identity.response(false))
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
		_, enabled, err := p.userTOTP(r.Context(), c.ID)
		if err != nil {
			writeServerError(w, err)
			return
		}
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
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	state, err := p.userAuthState(r.Context(), userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.totpEnabled {
		writeClientError(w, http.StatusConflict, "disable two-factor authentication before creating a new setup")
		return
	}
	if !p.allowSensitiveAuthAttempt(r, userID, "2fa-setup") {
		writeClientError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	passwordOK, err := auth.VerifyPassword(req.Password, state.passwordHash)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if !passwordOK {
		writeClientError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if p.secrets == nil {
		writeServerError(w, fmt.Errorf("store TOTP secret: secret box unavailable"))
		return
	}
	storedSecret, err := p.secrets.Encrypt(secret)
	if err != nil {
		writeServerError(w, fmt.Errorf("store TOTP secret: %w", err))
		return
	}
	newEpoch, err := p.changeTOTPStateAndRevokeSessions(
		r.Context(), userID, state.authEpoch, totpStateSetup, storedSecret,
	)
	if errors.Is(err, errTOTPStateChanged) {
		writeClientError(w, http.StatusConflict, "two-factor state changed; reload and try again")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	revokePendingLogins(userID)
	if err := p.replaceSessionAfterTOTPChange(r.Context(), w, userID, newEpoch, false); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "2fa.setup", "user", userID)
	json.NewEncoder(w).Encode(map[string]any{
		"secret": secret,
		"uri":    auth.TOTPURI(secret, state.username, "CelikPanel"),
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
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	state, err := p.userAuthState(r.Context(), userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if state.totpEnabled {
		writeClientError(w, http.StatusConflict, "two-factor authentication is already enabled")
		return
	}
	if !p.allowSensitiveAuthAttempt(r, userID, "2fa-enable") {
		writeClientError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	passwordOK, err := auth.VerifyPassword(req.Password, state.passwordHash)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if !passwordOK || state.totpSecret == "" || !auth.ValidateTOTP(state.totpSecret, req.Code) {
		writeClientError(w, http.StatusUnauthorized, "password or code incorrect")
		return
	}
	newEpoch, err := p.changeTOTPStateAndRevokeSessions(
		r.Context(), userID, state.authEpoch, totpStateEnable, "",
	)
	if errors.Is(err, errTOTPStateChanged) {
		writeClientError(w, http.StatusConflict, "two-factor state changed; reload and try again")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	revokePendingLogins(userID)
	if err := p.replaceSessionAfterTOTPChange(r.Context(), w, userID, newEpoch, true); err != nil {
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
	state, err := p.userAuthState(r.Context(), userID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if !state.totpEnabled {
		writeClientError(w, http.StatusConflict, "two-factor authentication is not enabled")
		return
	}
	if !p.allowSensitiveAuthAttempt(r, userID, "2fa-disable") {
		writeClientError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, state.passwordHash)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if !ok || !auth.ValidateTOTP(state.totpSecret, req.Code) {
		writeClientError(w, http.StatusUnauthorized, "password or code incorrect")
		return
	}
	newEpoch, err := p.changeTOTPStateAndRevokeSessions(
		r.Context(), userID, state.authEpoch, totpStateDisable, "",
	)
	if errors.Is(err, errTOTPStateChanged) {
		writeClientError(w, http.StatusConflict, "two-factor state changed; reload and try again")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	revokePendingLogins(userID)
	if err := p.replaceSessionAfterTOTPChange(r.Context(), w, userID, newEpoch, false); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "2fa.disable", "user", userID)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}
