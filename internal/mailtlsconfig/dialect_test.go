package mailtlsconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestObservedDovecotDialects(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		modern bool
	}{
		{"2.3.21.1 (d492236fa0)", false}, {"2.3.21-1ubuntu1", false},
		{"2.4.1 (hash)\n", true}, {"2.4.1-4", true}, {"2.4.1+vendor.2", true}, {"2.4", true},
	} {
		got, err := DovecotIs24([]byte(tc.raw))
		if err != nil || got != tc.modern {
			t.Fatalf("%q: %v %v", tc.raw, got, err)
		}
	}
}
func TestUnknownIsNotModernAndDoesNotExposeOutput(t *testing.T) {
	for _, raw := range []string{"", " \n", "private-token=secret", "2", "2.x.1", "2.4.bad", "2.4.1/secret", "2.4.1;secret", strings.Repeat("x", 4097)} {
		modern, err := DovecotIs24([]byte(raw))
		if modern || !errors.Is(err, ErrDovecotVersionUnknown) {
			t.Fatalf("unknown version accepted: %v %v", modern, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("command output exposed")
		}
	}
	for _, raw := range []string{"1.2.3", "2.2.36", "2.5.0", "3.0.0", "02.04.1"} {
		modern, err := DovecotIs24([]byte(raw))
		if modern || !errors.Is(err, ErrDovecotVersionUnsupported) {
			t.Fatalf("unimplemented dialect accepted: %q %v %v", raw, modern, err)
		}
	}
}
