package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testRepoPublicKey(t *testing.T, oldFormat bool, created byte) ([]byte, string) {
	t.Helper()
	// A small but structurally complete v4 RSA primary key packet. The MPI
	// values are test-only; repository trust still comes from the exact pin.
	// Yapisal olarak tam, kucuk bir v4 RSA primary key paketi. MPI degerleri
	// yalniz test icindir; depo guveni yine tam fingerprint sabitlemesinden gelir.
	body := []byte{
		4, 0, 0, 0, created, 1,
		0, 9, 0x01, 0x00,
		0, 17, 0x01, 0x00, 0x01,
	}
	var packet []byte
	if oldFormat {
		packet = append([]byte{0x99, 0, byte(len(body))}, body...)
	} else {
		packet = append([]byte{0xC6, byte(len(body))}, body...)
	}
	fingerprint, err := primaryPublicKeyFingerprint(packet)
	if err != nil {
		t.Fatalf("test key fingerprint: %v", err)
	}
	return packet, fingerprint
}

func TestOpenPGPPublicKeyRequiresCompletePacket(t *testing.T) {
	for _, oldFormat := range []bool{false, true} {
		key, _ := testRepoPublicKey(t, oldFormat, 0)
		if !isBinaryPublicKey(key) {
			t.Fatalf("complete key oldFormat=%v was rejected", oldFormat)
		}
	}
	for _, malformed := range [][]byte{
		{0xC6},
		{0xC6, 0x01, 0x04},
		{0x99, 0x01, 0x8d, 0x04},
	} {
		if isBinaryPublicKey(malformed) {
			t.Fatalf("malformed key %x was accepted", malformed)
		}
	}
}

func TestValidateRepoPublicKeyRequiresPinnedPrimaryFingerprint(t *testing.T) {
	key, fingerprint := testRepoPublicKey(t, false, 0)
	armored, err := validateRepoPublicKey(key, fingerprint)
	if err != nil || armored {
		t.Fatalf("binary pinned key: armored=%v err=%v", armored, err)
	}

	differentKey, _ := testRepoPublicKey(t, false, 1)
	if _, err := validateRepoPublicKey(differentKey, fingerprint); err == nil {
		t.Fatal("a structurally valid different primary key matched the catalogue pin")
	}

	armor := strings.Join([]string{
		openPGPPublicKeyArmorBegin,
		"",
		base64.StdEncoding.EncodeToString(key),
		openPGPPublicKeyArmorEnd,
	}, "\n")
	armored, err = validateRepoPublicKey([]byte(armor), fingerprint)
	if err != nil || !armored {
		t.Fatalf("armored pinned key: armored=%v err=%v", armored, err)
	}

	if _, err := validateRepoPublicKey([]byte{0xC6}, fingerprint); err == nil {
		t.Fatal("one-byte packet header was accepted as a trusted key")
	}
}

func TestValidateRepoPublicKeyRejectsMultiplePrimaryPacketsInEitherOrder(t *testing.T) {
	pinned, fingerprint := testRepoPublicKey(t, false, 0)
	attacker, _ := testRepoPublicKey(t, false, 1)
	tests := []struct {
		name string
		key  []byte
	}{
		{name: "pinned then attacker", key: append(append([]byte{}, pinned...), attacker...)},
		{name: "attacker then pinned", key: append(append([]byte{}, attacker...), pinned...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateRepoPublicKey(tt.key, fingerprint); err == nil {
				t.Fatal("keyring with two primary public keys matched the catalogue pin")
			}
		})
	}
}
