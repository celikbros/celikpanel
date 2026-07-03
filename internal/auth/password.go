// Package auth provides password hashing and session management for the
// panel. It is the single gate every request passes before reaching a
// handler.
//
// auth paketi, panel için parola özetleme ve oturum yönetimi sağlar. Her
// isteğin bir işleyiciye ulaşmadan önce geçtiği tek kapıdır.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters. These are deliberate: 64 MiB memory and 3 passes
// put a meaningful cost on offline cracking while staying fast enough for
// an interactive login.
// argon2id parametreleri. Bilinçlidir: 64 MiB bellek ve 3 geçiş,
// çevrimdışı kırmaya anlamlı bir maliyet yüklerken etkileşimli bir giriş
// için yeterince hızlı kalır.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrInvalidHash is returned when a stored hash cannot be parsed.
// ErrInvalidHash, saklanan bir özet çözümlenemediğinde döndürülür.
var ErrInvalidHash = errors.New("invalid password hash format")

// HashPassword returns a PHC-formatted argon2id hash of the password.
// HashPassword, parolanın PHC biçimli bir argon2id özetini döndürür.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<key>
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded hash. The
// comparison is constant-time.
// VerifyPassword, parolanın kodlanmış özetle eşleşip eşleşmediğini bildirir.
// Karşılaştırma sabit zamanlıdır.
func VerifyPassword(password, encodedHash string) (bool, error) {
	memory, time, threads, salt, key, err := decodeHash(encodedHash)
	if err != nil {
		return false, err
	}

	other := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(key)))
	if subtle.ConstantTimeCompare(key, other) == 1 {
		return true, nil
	}
	return false, nil
}

func decodeHash(encodedHash string) (memory, time uint32, threads uint8, salt, key []byte, err error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	return memory, time, threads, salt, key, nil
}
