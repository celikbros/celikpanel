package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// The control-plane backup key seals the disaster archive and nothing else. It
// is deliberately NOT secret.key: it is generated on demand, shown to the
// operator once, never written to the host and never stored in the database
// (docs/DISASTER-RECOVERY.md §4). Without it the archive cannot be opened by
// anyone, including us.
//
// Kontrol düzlemi yedek anahtarı yalnızca felaket arşivini mühürler. Bilerek
// secret.key DEĞİLDİR: istendiğinde üretilir, operatöre bir kez gösterilir,
// makineye hiç yazılmaz ve veritabanında saklanmaz. Anahtar olmadan arşivi
// kimse açamaz, biz de açamayız.

const (
	// controlPlaneKeyPrefix versions the printed key so a future key shape can
	// be told apart by looking at it.
	controlPlaneKeyPrefix = "cpk1"
	// controlPlaneKeyBytes is the raw entropy behind one key.
	controlPlaneKeyBytes = 32
	// controlPlaneKeyPayloadChars is ceil(32*8/5): 52 base32 characters carry
	// 256 bits with four trailing padding bits that must be zero.
	controlPlaneKeyPayloadChars = 52
	// controlPlaneKeyGroupSize is the printed grouping, for reading aloud and
	// for typing into a fresh host.
	controlPlaneKeyGroupSize = 4
	// crockfordBase32Alphabet is Crockford's alphabet in lower case: the
	// digits plus the letters with i, l, o and u removed so nothing printed can
	// be confused with 0 or 1.
	crockfordBase32Alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
)

var errControlPlaneKeyMalformed = errors.New(
	"control-plane backup key is not a valid cpk1 key",
)

// generateControlPlaneKey returns one fresh printable key. The caller prints it
// and forgets it; nothing in the product keeps a copy.
func generateControlPlaneKey() (string, error) {
	raw := make([]byte, controlPlaneKeyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate control-plane backup key: %w", err)
	}
	defer zeroControlPlaneKey(raw)
	return formatControlPlaneKey(raw)
}

// formatControlPlaneKey renders 32 raw bytes as the printed key form.
func formatControlPlaneKey(raw []byte) (string, error) {
	if len(raw) != controlPlaneKeyBytes {
		return "", errControlPlaneKeyMalformed
	}
	payload := encodeCrockfordBase32(raw)
	groups := make([]string, 0, 1+len(payload)/controlPlaneKeyGroupSize)
	groups = append(groups, controlPlaneKeyPrefix)
	for start := 0; start < len(payload); start += controlPlaneKeyGroupSize {
		end := start + controlPlaneKeyGroupSize
		if end > len(payload) {
			end = len(payload)
		}
		groups = append(groups, payload[start:end])
	}
	return strings.Join(groups, "-"), nil
}

// parseControlPlaneKey is the deterministic counterpart of
// formatControlPlaneKey. It accepts the printed form with or without the
// grouping dashes and in any letter case, and rejects everything else —
// including the Crockford o/i/l substitutions, so exactly one text maps to one
// key and a typo is reported rather than silently accepted.
func parseControlPlaneKey(text string) ([]byte, error) {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.ReplaceAll(normalized, "-", "")
	if !strings.HasPrefix(normalized, controlPlaneKeyPrefix) {
		return nil, errControlPlaneKeyMalformed
	}
	payload := normalized[len(controlPlaneKeyPrefix):]
	if len(payload) != controlPlaneKeyPayloadChars {
		return nil, errControlPlaneKeyMalformed
	}
	raw, err := decodeCrockfordBase32(payload, controlPlaneKeyBytes)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeCrockfordBase32(data []byte) string {
	var builder strings.Builder
	builder.Grow((len(data)*8 + 4) / 5)
	var accumulator uint32
	var bits uint
	for _, value := range data {
		accumulator = accumulator<<8 | uint32(value)
		bits += 8
		for bits >= 5 {
			bits -= 5
			builder.WriteByte(crockfordBase32Alphabet[(accumulator>>bits)&0x1f])
		}
	}
	if bits > 0 {
		builder.WriteByte(crockfordBase32Alphabet[(accumulator<<(5-bits))&0x1f])
	}
	return builder.String()
}

func decodeCrockfordBase32(text string, expectedBytes int) ([]byte, error) {
	decoded := make([]byte, 0, expectedBytes)
	var accumulator uint32
	var bits uint
	for index := 0; index < len(text); index++ {
		symbol := strings.IndexByte(crockfordBase32Alphabet, text[index])
		if symbol < 0 {
			return nil, errControlPlaneKeyMalformed
		}
		accumulator = accumulator<<5 | uint32(symbol)
		bits += 5
		if bits >= 8 {
			bits -= 8
			decoded = append(decoded, byte((accumulator>>bits)&0xff))
		}
	}
	// The remaining bits are padding. A canonical key pads with zeros only, so
	// two different texts can never decode to the same key.
	if bits >= 5 || accumulator&((1<<bits)-1) != 0 {
		return nil, errControlPlaneKeyMalformed
	}
	if len(decoded) != expectedBytes {
		return nil, errControlPlaneKeyMalformed
	}
	return decoded, nil
}

// zeroControlPlaneKey wipes key material the moment it stops being needed.
func zeroControlPlaneKey(key []byte) {
	for index := range key {
		key[index] = 0
	}
}
