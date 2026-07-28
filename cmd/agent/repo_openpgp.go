package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"strings"
)

const (
	openPGPPublicKeyArmorBegin = "-----BEGIN PGP PUBLIC KEY BLOCK-----"
	openPGPPublicKeyArmorEnd   = "-----END PGP PUBLIC KEY BLOCK-----"
)

// validateRepoPublicKey parses the complete primary OpenPGP key and compares
// its RFC 4880 v4 fingerprint with the catalogue pin. Packet-header lookalikes
// and a valid but different key are both rejected before apt can trust them.
//
// validateRepoPublicKey, birincil OpenPGP anahtarını tüm yapısıyla ayrıştırır ve
// RFC 4880 v4 fingerprint'ini katalogdaki sabit degerle karsilastirir. Yalnizca
// paket basligina benzeyen veri ve gecerli fakat farkli anahtar reddedilir.
func validateRepoPublicKey(key []byte, expectedFingerprint string) (armored bool, err error) {
	binaryKey, armored, err := decodeRepoPublicKey(key)
	if err != nil {
		return false, err
	}
	fingerprint, err := primaryPublicKeyFingerprint(binaryKey)
	if err != nil {
		return false, err
	}
	want := strings.ToUpper(strings.TrimSpace(expectedFingerprint))
	if len(want) != 40 {
		return false, fmt.Errorf("repository catalogue has an invalid primary key fingerprint")
	}
	if _, err := hex.DecodeString(want); err != nil {
		return false, fmt.Errorf("repository catalogue has an invalid primary key fingerprint")
	}
	if fingerprint != want {
		return false, fmt.Errorf("repository signing key fingerprint does not match the catalogue pin")
	}
	return armored, nil
}

func decodeRepoPublicKey(key []byte) ([]byte, bool, error) {
	text := strings.TrimSpace(string(key))
	if !strings.HasPrefix(text, "-----BEGIN") {
		return key, false, nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != openPGPPublicKeyArmorBegin {
		return nil, false, fmt.Errorf("repository key has invalid OpenPGP armor")
	}

	inPayload := false
	ended := false
	var encoded strings.Builder
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !inPayload {
			if line == "" {
				inPayload = true
				continue
			}
			if !strings.Contains(line, ":") {
				return nil, false, fmt.Errorf("repository key has invalid OpenPGP armor headers")
			}
			continue
		}
		if line == openPGPPublicKeyArmorEnd {
			for _, trailing := range lines[i+1:] {
				if strings.TrimSpace(trailing) != "" {
					return nil, false, fmt.Errorf("repository key has data after OpenPGP armor")
				}
			}
			ended = true
			break
		}
		if line == "" || strings.HasPrefix(line, "=") { // optional CRC24 line
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return nil, false, fmt.Errorf("repository key has invalid OpenPGP armor payload")
		}
		encoded.WriteString(line)
	}
	if !inPayload || !ended || encoded.Len() == 0 {
		return nil, false, fmt.Errorf("repository key has incomplete OpenPGP armor")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded.String())
	if err != nil {
		return nil, false, fmt.Errorf("repository key has invalid OpenPGP armor payload")
	}
	return decoded, true, nil
}

// primaryPublicKeyFingerprint accepts a packet stream only when it contains
// exactly one structurally valid v4 primary Public-Key packet (tag 6).
// primaryPublicKeyFingerprint, paket akışını yalnızca yapısal olarak geçerli
// tam bir v4 birincil Public-Key paketi (tag 6) içerdiğinde kabul eder.
// It deliberately does not shell out to gpg; validation stays deterministic.
// Bilerek gpg çalıştırmaz; doğrulama minimal sistemlerde de deterministik kalır.
func primaryPublicKeyFingerprint(packetStream []byte) (string, error) {
	var fingerprint string
	primaryCount := 0
	for offset := 0; offset < len(packetStream); {
		tag, body, next, err := readOpenPGPPacket(packetStream, offset)
		if err != nil {
			return "", err
		}
		if tag == 6 {
			primaryCount++
			if primaryCount > 1 {
				return "", fmt.Errorf("OpenPGP keyring contains multiple primary public key packets")
			}
			if err := validateV4PublicKeyBody(body); err != nil {
				return "", err
			}
			if len(body) > 0xffff {
				return "", fmt.Errorf("OpenPGP primary key packet is too large")
			}
			prefix := []byte{0x99, byte(len(body) >> 8), byte(len(body))}
			h := sha1.New()
			_, _ = h.Write(prefix)
			_, _ = h.Write(body)
			fingerprint = strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
		}
		offset = next
	}
	if primaryCount == 0 {
		return "", fmt.Errorf("OpenPGP keyring has no primary public key packet")
	}
	return fingerprint, nil
}

func readOpenPGPPacket(data []byte, offset int) (tag byte, body []byte, next int, err error) {
	if offset < 0 || offset >= len(data) || data[offset]&0x80 == 0 {
		return 0, nil, 0, fmt.Errorf("invalid OpenPGP packet header")
	}
	header := data[offset]
	offset++
	var length uint64
	if header&0x40 != 0 {
		tag = header & 0x3f
		if offset >= len(data) {
			return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet length")
		}
		first := data[offset]
		offset++
		switch {
		case first < 192:
			length = uint64(first)
		case first <= 223:
			if offset >= len(data) {
				return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet length")
			}
			length = uint64(first-192)<<8 + uint64(data[offset]) + 192
			offset++
		case first == 255:
			if len(data)-offset < 4 {
				return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet length")
			}
			length = uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			offset += 4
		default:
			return 0, nil, 0, fmt.Errorf("partial OpenPGP packet lengths are not accepted for repository keys")
		}
	} else {
		tag = (header >> 2) & 0x0f
		switch header & 0x03 {
		case 0:
			if offset >= len(data) {
				return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet length")
			}
			length = uint64(data[offset])
			offset++
		case 1:
			if len(data)-offset < 2 {
				return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet length")
			}
			length = uint64(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
		case 2:
			if len(data)-offset < 4 {
				return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet length")
			}
			length = uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			offset += 4
		default:
			return 0, nil, 0, fmt.Errorf("indeterminate OpenPGP packet length is not accepted for repository keys")
		}
	}
	if length > uint64(len(data)-offset) {
		return 0, nil, 0, fmt.Errorf("truncated OpenPGP packet body")
	}
	end := offset + int(length)
	return tag, data[offset:end], end, nil
}

func validateV4PublicKeyBody(body []byte) error {
	if len(body) < 6 || body[0] != 4 {
		return fmt.Errorf("repository key primary packet is not a valid OpenPGP v4 public key")
	}
	algorithm := body[5]
	offset := 6
	var err error
	parseMPIs := func(count int) bool {
		for i := 0; i < count; i++ {
			offset, err = readOpenPGPMPI(body, offset)
			if err != nil {
				return false
			}
		}
		return true
	}
	switch algorithm {
	case 1, 2, 3: // RSA
		parseMPIs(2)
	case 16, 20: // ElGamal
		parseMPIs(3)
	case 17: // DSA
		parseMPIs(4)
	case 18, 19, 22: // ECDH, ECDSA, EdDSA
		if offset >= len(body) || body[offset] == 0 {
			return fmt.Errorf("OpenPGP elliptic-curve key has no curve OID")
		}
		oidLength := int(body[offset])
		offset++
		if oidLength > len(body)-offset {
			return fmt.Errorf("truncated OpenPGP elliptic-curve OID")
		}
		offset += oidLength
		if !parseMPIs(1) {
			break
		}
		if algorithm == 18 {
			if offset >= len(body) {
				return fmt.Errorf("OpenPGP ECDH key has no KDF parameters")
			}
			kdfLength := int(body[offset])
			offset++
			if kdfLength == 0 || kdfLength > len(body)-offset {
				return fmt.Errorf("invalid OpenPGP ECDH KDF parameters")
			}
			offset += kdfLength
		}
	default:
		return fmt.Errorf("unsupported OpenPGP public key algorithm %d", algorithm)
	}
	if err != nil {
		return err
	}
	if offset != len(body) {
		return fmt.Errorf("OpenPGP primary key packet has trailing or missing key material")
	}
	return nil
}

func readOpenPGPMPI(data []byte, offset int) (int, error) {
	if offset < 0 || len(data)-offset < 2 {
		return 0, fmt.Errorf("truncated OpenPGP MPI length")
	}
	bitLength := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	byteLength := (bitLength + 7) / 8
	if bitLength == 0 || byteLength > len(data)-offset {
		return 0, fmt.Errorf("invalid or truncated OpenPGP MPI")
	}
	first := data[offset]
	actualBits := (byteLength-1)*8 + (8 - bits.LeadingZeros8(first))
	if first == 0 || actualBits != bitLength {
		return 0, fmt.Errorf("OpenPGP MPI bit length does not match its value")
	}
	return offset + byteLength, nil
}
