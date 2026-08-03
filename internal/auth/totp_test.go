package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	// A code produced from the same secret at the same instant must verify.
	// Aynı anda aynı anahtardan üretilen kod doğrulanmalı.
	key, err := base32Decode(secret)
	if err != nil {
		t.Fatalf("base32Decode: %v", err)
	}
	counter := currentCounter()
	code := hotp(key, counter)
	if !ValidateTOTP(secret, code) {
		t.Fatalf("valid code %q rejected", code)
	}
	if ValidateTOTP(secret, "000000") && code != "000000" {
		t.Fatalf("wrong code accepted")
	}
	if len(code) != totpDigits {
		t.Fatalf("code length = %d, want %d", len(code), totpDigits)
	}
}

func currentCounter() uint64 {
	// Mirror ValidateTOTP's step so the test is deterministic within a window.
	// ValidateTOTP'nin adımını yansıt ki test bir pencere içinde belirlenimci olsun.
	return uint64(time.Now().Unix() / totpStep)
}

func TestValidateTOTPRejectsEmptyMalformedAndWrongSizedSecrets(t *testing.T) {
	shortKey := []byte(`short`)
	longKey := make([]byte, totpSecretBytes+1)
	tests := []struct {
		name   string
		secret string
		key    []byte
	}{
		{name: `empty`, secret: ``, key: nil},
		{name: `malformed`, secret: `AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA!`, key: nil},
		{name: `short`, secret: base32Encode(shortKey), key: shortKey},
		{name: `long`, secret: base32Encode(longKey), key: longKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code := hotp(test.key, currentCounter())
			if ValidateTOTP(test.secret, code) {
				t.Fatalf(`invalid secret %q accepted`, test.secret)
			}
			if ValidateTOTPSecret(test.secret) {
				t.Fatalf(`invalid secret %q reported valid`, test.secret)
			}
		})
	}
}

func TestValidateTOTPSecretAllowsCaseAndPaddingCompatibility(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	compatible := strings.ToLower(secret) + `========`
	if !ValidateTOTPSecret(compatible) {
		t.Fatalf(`compatible secret %q rejected`, compatible)
	}
	key, err := base32Decode(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateTOTP(compatible, hotp(key, currentCounter())) {
		t.Fatal(`compatible secret did not authenticate`)
	}
}
