package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// SessionDuration is how long a session stays valid after login.
// SessionDuration, bir oturumun girişten sonra ne kadar geçerli kalacağıdır.
const SessionDuration = 24 * time.Hour

// ErrSessionInvalid covers a missing, expired or malformed session.
// ErrSessionInvalid; eksik, süresi dolmuş ya da bozuk bir oturumu kapsar.
var ErrSessionInvalid = errors.New("session invalid or expired")

// SessionStore persists sessions in SQLite. Only the SHA-256 of each token
// is stored, so the raw token exists solely in the user's cookie.
// SessionStore, oturumları SQLite'da saklar. Her jetonun yalnızca SHA-256
// özeti saklanır; ham jeton yalnızca kullanıcının çerezinde bulunur.
type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(db *sql.DB) *SessionStore {
	return &SessionStore{db: db}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create issues a new session for userID and returns the raw token that
// belongs in the cookie.
// Create, userID için yeni bir oturum açar ve çereze konulacak ham jetonu
// döndürür.
func (s *SessionStore) Create(ctx context.Context, userID int) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	token := hex.EncodeToString(raw)
	expiresAt := time.Now().Add(SessionDuration).UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)",
		hashToken(token), userID, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to store session: %w", err)
	}
	return token, nil
}

// Validate resolves a raw token to its user ID, rejecting expired ones.
// Expired rows are cleaned up opportunistically.
// Validate, ham bir jetonu kullanıcı kimliğine çözer ve süresi dolmuş
// olanları reddeder. Süresi dolmuş satırlar fırsat buldukça temizlenir.
func (s *SessionStore) Validate(ctx context.Context, token string) (int, error) {
	if token == "" {
		return 0, ErrSessionInvalid
	}

	var userID int
	var expiresAt string
	err := s.db.QueryRowContext(ctx,
		"SELECT user_id, expires_at FROM sessions WHERE token_hash = ?",
		hashToken(token),
	).Scan(&userID, &expiresAt)
	if err != nil {
		return 0, ErrSessionInvalid
	}

	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		_ = s.Delete(ctx, token)
		return 0, ErrSessionInvalid
	}
	return userID, nil
}

// Delete removes a single session (logout).
// Delete, tek bir oturumu kaldırır (çıkış).
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashToken(token))
	return err
}

// DeleteExpired purges all sessions past their expiry. Meant to run
// periodically.
// DeleteExpired, süresi geçmiş tüm oturumları siler. Periyodik çalışması
// amaçlanmıştır.
func (s *SessionStore) DeleteExpired(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", now)
	return err
}
