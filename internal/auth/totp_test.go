package auth

import (
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
