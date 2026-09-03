package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestControlPlaneKeyRoundTrip(t *testing.T) {
	for attempt := 0; attempt < 32; attempt++ {
		printed, err := generateControlPlaneKey()
		if err != nil {
			t.Fatalf("generate control-plane key: %v", err)
		}
		if !strings.HasPrefix(printed, controlPlaneKeyPrefix+"-") {
			t.Fatalf("key %q does not carry its version prefix", printed)
		}
		groups := strings.Split(printed, "-")
		if len(groups) != 1+(controlPlaneKeyPayloadChars+controlPlaneKeyGroupSize-1)/controlPlaneKeyGroupSize {
			t.Fatalf("key %q has %d groups", printed, len(groups))
		}
		for _, group := range groups[1:] {
			if len(group) != controlPlaneKeyGroupSize {
				t.Fatalf("key %q has a group of %d characters", printed, len(group))
			}
		}
		raw, err := parseControlPlaneKey(printed)
		if err != nil {
			t.Fatalf("parse %q: %v", printed, err)
		}
		if len(raw) != controlPlaneKeyBytes {
			t.Fatalf("parsed %d bytes, want %d", len(raw), controlPlaneKeyBytes)
		}
		reprinted, err := formatControlPlaneKey(raw)
		if err != nil {
			t.Fatalf("reformat: %v", err)
		}
		if reprinted != printed {
			t.Fatalf("reformatted %q, want %q", reprinted, printed)
		}
		// The printed form, the undashed form and any letter case must all
		// decode to exactly the same key.
		for _, variant := range []string{
			strings.ReplaceAll(printed, "-", ""),
			strings.ToUpper(printed),
			strings.ToUpper(strings.ReplaceAll(printed, "-", "")),
			"  " + printed + "\n",
		} {
			decoded, err := parseControlPlaneKey(variant)
			if err != nil {
				t.Fatalf("parse variant %q: %v", variant, err)
			}
			if !bytes.Equal(decoded, raw) {
				t.Fatalf("variant %q decoded to a different key", variant)
			}
		}
	}
}

func TestControlPlaneKeyGeneratesDistinctKeys(t *testing.T) {
	seen := map[string]struct{}{}
	for attempt := 0; attempt < 64; attempt++ {
		key, err := generateControlPlaneKey()
		if err != nil {
			t.Fatalf("generate control-plane key: %v", err)
		}
		if _, repeated := seen[key]; repeated {
			t.Fatalf("generated the same key twice: %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestControlPlaneKeyRejections(t *testing.T) {
	valid, err := generateControlPlaneKey()
	if err != nil {
		t.Fatalf("generate control-plane key: %v", err)
	}
	undashed := strings.ReplaceAll(valid, "-", "")
	payload := undashed[len(controlPlaneKeyPrefix):]

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty"},
		{name: "prefix only", input: controlPlaneKeyPrefix},
		{name: "no prefix", input: payload},
		{name: "wrong prefix", input: "cpk2" + payload},
		{name: "too short", input: controlPlaneKeyPrefix + payload[:len(payload)-1]},
		{name: "too long", input: controlPlaneKeyPrefix + payload + "0"},
		{
			name:  "ambiguous letter i",
			input: controlPlaneKeyPrefix + "i" + payload[1:],
		},
		{
			name:  "ambiguous letter l",
			input: controlPlaneKeyPrefix + "l" + payload[1:],
		},
		{
			name:  "ambiguous letter o",
			input: controlPlaneKeyPrefix + "o" + payload[1:],
		},
		{
			name:  "ambiguous letter u",
			input: controlPlaneKeyPrefix + "u" + payload[1:],
		},
		{
			name:  "non alphabet character",
			input: controlPlaneKeyPrefix + "!" + payload[1:],
		},
		{
			// The last character carries four padding bits that must be zero.
			name:  "non canonical padding",
			input: controlPlaneKeyPrefix + payload[:len(payload)-1] + "1",
		},
		{name: "hex instead of base32", input: "cpk1-0123-4567-89ab-cdef"},
		{name: "words", input: "correct horse battery staple"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseControlPlaneKey(test.input); err == nil {
				t.Fatalf("parse %q was accepted", test.input)
			}
		})
	}

	// The canonical-padding case is only a rejection if the generated key
	// really did end on a zero-padded symbol, which every 32-byte key does.
	if _, err := parseControlPlaneKey(valid); err != nil {
		t.Fatalf("the control key of the table was itself rejected: %v", err)
	}
}

func TestControlPlaneKeyIsZeroed(t *testing.T) {
	key := []byte{1, 2, 3, 4}
	zeroControlPlaneKey(key)
	for index, value := range key {
		if value != 0 {
			t.Fatalf("byte %d is %d after wiping", index, value)
		}
	}
}
