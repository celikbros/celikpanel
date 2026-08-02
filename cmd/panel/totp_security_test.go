package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	secretstore "github.com/alicelik/celikpanel/internal/secrets"
)

func newTOTPSecurityPanel(t *testing.T) (*Panel, *paneldb.SQLiteDB, *core.User) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), `panel.sqlite`))
	if err != nil {
		t.Fatalf(`open TOTP test database: %v`, err)
	}
	t.Cleanup(database.Close)
	box, err := secretstore.LoadOrCreate(filepath.Join(t.TempDir(), `secrets.key`))
	if err != nil {
		t.Fatalf(`open TOTP secret box: %v`, err)
	}
	hash, err := auth.HashPassword(`correct horse battery staple`)
	if err != nil {
		t.Fatal(err)
	}
	repository := repositories.NewPostgresUserRepository(database.GetDB())
	user := &core.User{
		Username:     `totp-user`,
		PasswordHash: hash,
		Email:        `totp-user@example.test`,
		Role:         `customer`,
		Status:       `active`,
	}
	if err := repository.Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	return &Panel{
		db:           database,
		users:        repository,
		sessions:     auth.NewSessionStore(database.GetDB()),
		secrets:      box,
		loginLimiter: newRateLimiter(20, time.Minute),
	}, database, user
}

func isolatePendingLogins(t *testing.T) {
	t.Helper()
	pendingMu.Lock()
	previous := pendingStore
	pendingStore = map[string]pendingLogin{}
	pendingMu.Unlock()
	t.Cleanup(func() {
		pendingMu.Lock()
		pendingStore = previous
		pendingMu.Unlock()
	})
}

func enableTOTPForSecurityTest(t *testing.T, panel *Panel, database *paneldb.SQLiteDB, userID int) (userAuthState, string) {
	t.Helper()
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := panel.secrets.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(
		`UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE id = ?`, stored, userID,
	); err != nil {
		t.Fatal(err)
	}
	state, err := panel.userAuthState(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return state, secret
}

func totpCodeForSecurityTest(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	return fmt.Sprintf(`%06d`, value%1000000)
}

func completePendingTOTPForSecurityTest(panel *Panel, token, code string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/login/totp`, strings.NewReader(
		fmt.Sprintf(`{"pending_token":%q,"code":%q}`, token, code),
	))
	panel.handleLoginTOTP(recorder, request)
	return recorder
}

func sessionTokenFromRecorder(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie.Value
		}
	}
	t.Fatal(`response did not contain a replacement session cookie`)
	return ""
}

func requireSessionInvalid(t *testing.T, panel *Panel, token string) {
	t.Helper()
	if _, err := panel.sessions.Validate(t.Context(), token); err == nil {
		t.Fatal(`pre-change session remained valid`)
	}
}

func requireSessionValidForUser(t *testing.T, panel *Panel, token string, userID int) {
	t.Helper()
	got, err := panel.sessions.Validate(t.Context(), token)
	if err != nil {
		t.Fatalf(`replacement session is invalid: %v`, err)
	}
	if got != userID {
		t.Fatalf(`replacement session user = %d, want %d`, got, userID)
	}
}

func TestUserTOTPLegacyPlaintextIsReadAndSealed(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(
		`UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE id = ?`, secret, user.ID,
	); err != nil {
		t.Fatal(err)
	}

	got, enabled, err := panel.userTOTP(t.Context(), user.ID)
	if err != nil {
		t.Fatalf(`userTOTP: %v`, err)
	}
	if got != secret || !enabled {
		t.Fatalf(`userTOTP = %q/%v, want original secret/true`, got, enabled)
	}
	var stored string
	if err := database.GetDB().QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !secretstore.IsEncrypted(stored) || stored == secret {
		t.Fatalf(`legacy TOTP secret was not sealed: %q`, stored)
	}
	plain, err := panel.secrets.Decrypt(stored)
	if err != nil || plain != secret {
		t.Fatalf(`sealed TOTP secret decrypt = %q/%v`, plain, err)
	}
}

func TestStartupTOTPSecretMigrationSealsDormantPlaintextAndValidatesCiphertext(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	legacy, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	alreadyPlain, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	alreadySealed, err := panel.secrets.Encrypt(alreadyPlain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(
		`UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE id = ?`, legacy, user.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role, status, totp_secret, totp_enabled)
		VALUES ('sealed-user', 'hash', 'sealed@example.test', 'customer', 'active', ?, 1)`, alreadySealed); err != nil {
		t.Fatal(err)
	}

	if err := panel.encryptLegacyTOTPSecrets(t.Context()); err != nil {
		t.Fatalf(`encryptLegacyTOTPSecrets: %v`, err)
	}
	var migrated string
	if err := database.GetDB().QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, user.ID).Scan(&migrated); err != nil {
		t.Fatal(err)
	}
	if !secretstore.IsEncrypted(migrated) || migrated == legacy {
		t.Fatalf(`dormant plaintext secret was not migrated: %q`, migrated)
	}
	plain, err := panel.secrets.Decrypt(migrated)
	if err != nil || plain != legacy {
		t.Fatalf(`migrated secret decrypt = %q/%v`, plain, err)
	}
	var unchanged string
	if err := database.GetDB().QueryRow(
		`SELECT totp_secret FROM users WHERE username = 'sealed-user'`,
	).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != alreadySealed {
		t.Fatal(`valid sealed TOTP secret was rewritten`)
	}
}

func TestStartupTOTPSecretMigrationRejectsCorruptionAndRollsBack(t *testing.T) {
	tests := []struct {
		name   string
		stored func(*Panel) any
	}{
		{name: `malformed ciphertext`, stored: func(*Panel) any { return `enc:v1:not-base64` }},
		{name: `encrypted invalid secret`, stored: func(panel *Panel) any {
			sealed, err := panel.secrets.Encrypt(`AAAA`)
			if err != nil {
				panic(err)
			}
			return sealed
		}},
		{name: `enabled missing secret`, stored: func(*Panel) any { return nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, database, user := newTOTPSecurityPanel(t)
			legacy, err := auth.GenerateTOTPSecret()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.GetDB().Exec(
				`UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE id = ?`, legacy, user.ID,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := database.GetDB().Exec(`
				INSERT INTO users (username, password_hash, email, role, status, totp_secret, totp_enabled)
				VALUES ('corrupt-user', 'hash', 'corrupt@example.test', 'customer', 'active', ?, 1)`, test.stored(panel)); err != nil {
				t.Fatal(err)
			}

			if err := panel.encryptLegacyTOTPSecrets(t.Context()); err == nil {
				t.Fatal(`corrupt TOTP migration unexpectedly succeeded`)
			}
			var unchanged string
			if err := database.GetDB().QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, user.ID).Scan(&unchanged); err != nil {
				t.Fatal(err)
			}
			if unchanged != legacy {
				t.Fatalf(`failed migration partially committed plaintext row: %q`, unchanged)
			}
		})
	}
}

func TestTOTPSetupStoresOnlySealedSecret(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/setup`, strings.NewReader(
		`{"password":"correct horse battery staple"}`,
	))
	panel.handle2FASetup(recorder, request, user.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`setup status = %d; body=%s`, recorder.Code, recorder.Body.String())
	}
	var response struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := database.GetDB().QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, user.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if response.Secret == `` || !secretstore.IsEncrypted(stored) || stored == response.Secret {
		t.Fatalf(`setup returned/stored insecure secret state: %q/%q`, response.Secret, stored)
	}
	plain, err := panel.secrets.Decrypt(stored)
	if err != nil || plain != response.Secret {
		t.Fatalf(`stored setup secret decrypt = %q/%v`, plain, err)
	}
	loaded, enabled, err := panel.userTOTP(t.Context(), user.ID)
	if err != nil || loaded != response.Secret || enabled {
		t.Fatalf(`userTOTP after setup = %q/%v/%v`, loaded, enabled, err)
	}
}

func TestTOTPSetupRejectsEnabledAccountWithoutMutation(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := panel.secrets.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(
		`UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE id = ?`, stored, user.ID,
	); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/setup`, strings.NewReader(
		`{"password":"correct horse battery staple"}`,
	))
	panel.handle2FASetup(recorder, request, user.ID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf(`setup status = %d, want 409; body=%s`, recorder.Code, recorder.Body.String())
	}
	var got string
	var enabled int
	var epoch int64
	if err := database.GetDB().QueryRow(
		`SELECT totp_secret, totp_enabled, auth_epoch FROM users WHERE id = ?`, user.ID,
	).Scan(&got, &enabled, &epoch); err != nil {
		t.Fatal(err)
	}
	if got != stored || enabled != 1 || epoch != 0 {
		t.Fatalf(`enabled setup mutated state: stored=%q enabled=%d epoch=%d`, got, enabled, epoch)
	}
}

func TestLoginFailsClosedWhenTOTPReadFails(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	if _, err := database.GetDB().Exec(
		`ALTER TABLE users RENAME COLUMN totp_secret TO unavailable_totp_secret`,
	); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/login`, strings.NewReader(
		`{"username":"totp-user","password":"correct horse battery staple"}`,
	))
	panel.handleLogin(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`login status = %d, want 500; body=%s`, recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get(`Set-Cookie`) != `` {
		t.Fatalf(`login issued a cookie on TOTP read failure: %q`, recorder.Header().Get(`Set-Cookie`))
	}
	var sessions int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf(`login created %d sessions on TOTP read failure`, sessions)
	}
}

func TestLoginFailsClosedForInvalidOrUnopenableTOTPSecret(t *testing.T) {
	tests := []struct {
		name   string
		stored string
	}{
		{name: `empty enabled secret`, stored: ``},
		{name: `invalid plaintext secret`, stored: `AAAA`},
		{name: `malformed sealed secret`, stored: `enc:v1:not-base64`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			panel, database, user := newTOTPSecurityPanel(t)
			if _, err := database.GetDB().Exec(
				`UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE id = ?`, test.stored, user.ID,
			); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/login`, strings.NewReader(
				`{"username":"totp-user","password":"correct horse battery staple"}`,
			))
			panel.handleLogin(recorder, request)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf(`login status = %d, want 500; body=%s`, recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get(`Set-Cookie`) != `` {
				t.Fatal(`login issued a cookie for an invalid TOTP secret`)
			}
			var sessions int
			if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
				t.Fatal(err)
			}
			if sessions != 0 {
				t.Fatalf(`login created %d sessions for an invalid TOTP secret`, sessions)
			}
		})
	}
}

func TestPendingTOTPLoginCompletesOnlyForUnchangedAuthEpoch(t *testing.T) {
	t.Run(`unchanged state succeeds`, func(t *testing.T) {
		isolatePendingLogins(t)
		panel, database, user := newTOTPSecurityPanel(t)
		state, secret := enableTOTPForSecurityTest(t, panel, database, user.ID)
		token, err := newPendingToken(state)
		if err != nil {
			t.Fatal(err)
		}
		recorder := completePendingTOTPForSecurityTest(panel, token, totpCodeForSecurityTest(t, secret))
		if recorder.Code != http.StatusOK {
			t.Fatalf(`TOTP completion status = %d; body=%s`, recorder.Code, recorder.Body.String())
		}
		var sessions int
		if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 1 {
			t.Fatalf(`TOTP completion sessions = %d, want 1`, sessions)
		}
	})

	t.Run(`password epoch change rejects`, func(t *testing.T) {
		isolatePendingLogins(t)
		panel, database, user := newTOTPSecurityPanel(t)
		state, secret := enableTOTPForSecurityTest(t, panel, database, user.ID)
		token, err := newPendingToken(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(
			`UPDATE users SET password_hash = 'changed-hash', auth_epoch = auth_epoch + 1 WHERE id = ?`, user.ID,
		); err != nil {
			t.Fatal(err)
		}
		recorder := completePendingTOTPForSecurityTest(panel, token, totpCodeForSecurityTest(t, secret))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf(`changed-epoch completion status = %d, want 401; body=%s`, recorder.Code, recorder.Body.String())
		}
		var sessions int
		if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 0 {
			t.Fatalf(`changed-epoch completion created %d sessions`, sessions)
		}
	})

	t.Run(`suspension is rechecked`, func(t *testing.T) {
		isolatePendingLogins(t)
		panel, database, user := newTOTPSecurityPanel(t)
		state, secret := enableTOTPForSecurityTest(t, panel, database, user.ID)
		token, err := newPendingToken(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(`UPDATE users SET status = 'suspended' WHERE id = ?`, user.ID); err != nil {
			t.Fatal(err)
		}
		recorder := completePendingTOTPForSecurityTest(panel, token, totpCodeForSecurityTest(t, secret))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf(`suspended completion status = %d, want 403; body=%s`, recorder.Code, recorder.Body.String())
		}
		var sessions int
		if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if sessions != 0 {
			t.Fatalf(`suspended completion created %d sessions`, sessions)
		}
	})
}

func TestTOTPSetupIncrementsEpochAndRevokesPendingLogin(t *testing.T) {
	isolatePendingLogins(t)
	panel, database, user := newTOTPSecurityPanel(t)
	state, err := panel.userAuthState(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := newPendingToken(state)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/setup`, strings.NewReader(
		`{"password":"correct horse battery staple"}`,
	))
	panel.handle2FASetup(recorder, request, user.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf(`setup status = %d; body=%s`, recorder.Code, recorder.Body.String())
	}
	var setupResponse struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&setupResponse); err != nil {
		t.Fatal(err)
	}
	if _, ok := consumePendingToken(token); ok {
		t.Fatal(`2FA setup left a pre-change pending login valid`)
	}
	var epoch int64
	if err := database.GetDB().QueryRow(`SELECT auth_epoch FROM users WHERE id = ?`, user.ID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if epoch != state.authEpoch+1 {
		t.Fatalf(`setup auth_epoch = %d, want %d`, epoch, state.authEpoch+1)
	}

	setupState, err := panel.userAuthState(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	enablePending, err := newPendingToken(setupState)
	if err != nil {
		t.Fatal(err)
	}
	enableRecorder := httptest.NewRecorder()
	enableRequest := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/enable`, strings.NewReader(
		fmt.Sprintf(`{"password":"correct horse battery staple","code":%q}`,
			totpCodeForSecurityTest(t, setupResponse.Secret)),
	))
	panel.handle2FAEnable(enableRecorder, enableRequest, user.ID)
	if enableRecorder.Code != http.StatusOK {
		t.Fatalf(`enable status = %d; body=%s`, enableRecorder.Code, enableRecorder.Body.String())
	}
	if _, ok := consumePendingToken(enablePending); ok {
		t.Fatal(`2FA enable left a pre-change pending login valid`)
	}
	enabledState, err := panel.userAuthState(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !enabledState.totpEnabled || enabledState.authEpoch != setupState.authEpoch+1 {
		t.Fatalf(`enable state = enabled:%v epoch:%d`, enabledState.totpEnabled, enabledState.authEpoch)
	}

	disablePending, err := newPendingToken(enabledState)
	if err != nil {
		t.Fatal(err)
	}
	disableRecorder := httptest.NewRecorder()
	disableRequest := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/disable`, strings.NewReader(
		fmt.Sprintf(`{"password":"correct horse battery staple","code":%q}`,
			totpCodeForSecurityTest(t, setupResponse.Secret)),
	))
	panel.handle2FADisable(disableRecorder, disableRequest, user.ID)
	if disableRecorder.Code != http.StatusOK {
		t.Fatalf(`disable status = %d; body=%s`, disableRecorder.Code, disableRecorder.Body.String())
	}
	if _, ok := consumePendingToken(disablePending); ok {
		t.Fatal(`2FA disable left a pre-change pending login valid`)
	}
	disabledState, err := panel.userAuthState(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if disabledState.totpEnabled || disabledState.totpSecret != `` || disabledState.authEpoch != enabledState.authEpoch+1 {
		t.Fatalf(`disable state = enabled:%v secret:%q epoch:%d`,
			disabledState.totpEnabled, disabledState.totpSecret, disabledState.authEpoch)
	}
}

func TestTOTPSetupAndEnableRequireCurrentPassword(t *testing.T) {
	t.Run(`setup rejects a wrong password without mutation`, func(t *testing.T) {
		panel, database, user := newTOTPSecurityPanel(t)
		existing, err := panel.sessions.Create(t.Context(), user.ID)
		if err != nil {
			t.Fatal(err)
		}

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/setup`, strings.NewReader(
			`{"password":"wrong password"}`,
		))
		panel.handle2FASetup(recorder, request, user.ID)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf(`setup status = %d, want 401; body=%s`, recorder.Code, recorder.Body.String())
		}

		var stored *string
		var enabled int
		var epoch int64
		if err := database.GetDB().QueryRow(
			`SELECT totp_secret, totp_enabled, auth_epoch FROM users WHERE id = ?`, user.ID,
		).Scan(&stored, &enabled, &epoch); err != nil {
			t.Fatal(err)
		}
		if stored != nil || enabled != 0 || epoch != 0 {
			t.Fatalf(`rejected setup mutated state: secret=%v enabled=%d epoch=%d`, stored, enabled, epoch)
		}
		requireSessionValidForUser(t, panel, existing, user.ID)
	})

	t.Run(`enable rejects a wrong password without mutation`, func(t *testing.T) {
		panel, database, user := newTOTPSecurityPanel(t)
		secret, err := auth.GenerateTOTPSecret()
		if err != nil {
			t.Fatal(err)
		}
		stored, err := panel.secrets.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.GetDB().Exec(
			`UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE id = ?`, stored, user.ID,
		); err != nil {
			t.Fatal(err)
		}
		existing, err := panel.sessions.Create(t.Context(), user.ID)
		if err != nil {
			t.Fatal(err)
		}

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/enable`, strings.NewReader(
			fmt.Sprintf(`{"password":"wrong password","code":%q}`, totpCodeForSecurityTest(t, secret)),
		))
		panel.handle2FAEnable(recorder, request, user.ID)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf(`enable status = %d, want 401; body=%s`, recorder.Code, recorder.Body.String())
		}

		state, err := panel.userAuthState(t.Context(), user.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.totpEnabled || state.authEpoch != 0 || state.totpSecret != secret {
			t.Fatalf(`rejected enable mutated state: enabled=%v epoch=%d secret=%q`,
				state.totpEnabled, state.authEpoch, state.totpSecret)
		}
		requireSessionValidForUser(t, panel, existing, user.ID)
	})
}

func TestTOTPStateChangesRevokeOldSessionsAndRotateCurrentBrowser(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	first, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}

	setupRecorder := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/setup`, strings.NewReader(
		`{"password":"correct horse battery staple"}`,
	))
	panel.handle2FASetup(setupRecorder, setupRequest, user.ID)
	if setupRecorder.Code != http.StatusOK {
		t.Fatalf(`setup status = %d; body=%s`, setupRecorder.Code, setupRecorder.Body.String())
	}
	var setupResponse struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(setupRecorder.Body).Decode(&setupResponse); err != nil {
		t.Fatal(err)
	}
	requireSessionInvalid(t, panel, first)
	requireSessionInvalid(t, panel, second)
	setupSession := sessionTokenFromRecorder(t, setupRecorder)
	requireSessionValidForUser(t, panel, setupSession, user.ID)

	extraBeforeEnable, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	enableRecorder := httptest.NewRecorder()
	enableRequest := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/enable`, strings.NewReader(
		fmt.Sprintf(`{"password":"correct horse battery staple","code":%q}`,
			totpCodeForSecurityTest(t, setupResponse.Secret)),
	))
	panel.handle2FAEnable(enableRecorder, enableRequest, user.ID)
	if enableRecorder.Code != http.StatusOK {
		t.Fatalf(`enable status = %d; body=%s`, enableRecorder.Code, enableRecorder.Body.String())
	}
	requireSessionInvalid(t, panel, setupSession)
	requireSessionInvalid(t, panel, extraBeforeEnable)
	enabledSession := sessionTokenFromRecorder(t, enableRecorder)
	requireSessionValidForUser(t, panel, enabledSession, user.ID)

	extraBeforeDisable, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	disableRecorder := httptest.NewRecorder()
	disableRequest := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/disable`, strings.NewReader(
		fmt.Sprintf(`{"password":"correct horse battery staple","code":%q}`,
			totpCodeForSecurityTest(t, setupResponse.Secret)),
	))
	panel.handle2FADisable(disableRecorder, disableRequest, user.ID)
	if disableRecorder.Code != http.StatusOK {
		t.Fatalf(`disable status = %d; body=%s`, disableRecorder.Code, disableRecorder.Body.String())
	}
	requireSessionInvalid(t, panel, enabledSession)
	requireSessionInvalid(t, panel, extraBeforeDisable)
	disabledSession := sessionTokenFromRecorder(t, disableRecorder)
	requireSessionValidForUser(t, panel, disabledSession, user.ID)

	var sessions int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID,
	).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf(`sessions after TOTP rotations = %d, want only the replacement`, sessions)
	}
}

func TestTOTPStateAndSessionRevocationRollbackTogether(t *testing.T) {
	panel, database, user := newTOTPSecurityPanel(t)
	existing, err := panel.sessions.Create(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(fmt.Sprintf(`
		CREATE TRIGGER reject_totp_session_revoke
		BEFORE DELETE ON sessions
		WHEN OLD.user_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'session revoke blocked');
		END`, user.ID)); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, `/api/v1/auth/2fa/setup`, strings.NewReader(
		`{"password":"correct horse battery staple"}`,
	))
	panel.handle2FASetup(recorder, request, user.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`setup status = %d, want 500; body=%s`, recorder.Code, recorder.Body.String())
	}

	var stored *string
	var enabled int
	var epoch int64
	if err := database.GetDB().QueryRow(
		`SELECT totp_secret, totp_enabled, auth_epoch FROM users WHERE id = ?`, user.ID,
	).Scan(&stored, &enabled, &epoch); err != nil {
		t.Fatal(err)
	}
	if stored != nil || enabled != 0 || epoch != 0 {
		t.Fatalf(`failed session revocation partially committed TOTP state: secret=%v enabled=%d epoch=%d`,
			stored, enabled, epoch)
	}
	requireSessionValidForUser(t, panel, existing, user.ID)
	if recorder.Header().Get(`Set-Cookie`) != "" {
		t.Fatalf(`failed state change issued a replacement cookie: %q`, recorder.Header().Get(`Set-Cookie`))
	}
}
