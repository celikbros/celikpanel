package mailtlsartifact

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/alpha81-" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestActualAlpha81ProducerRoundTrip(t *testing.T) {
	for _, name := range []string{"empty", "sni"} {
		t.Run(name, func(t *testing.T) {
			raw := fixture(t, name)
			plan, e := Decode(raw)
			if e != nil {
				t.Fatal(e)
			}
			got, e := Encode(plan)
			if e != nil || !bytes.Equal(got, raw) {
				t.Fatalf("historical bytes differ: %v", e)
			}
			if !Equal(plan, plan) || Equal(nil, plan) || !Equal(nil, nil) {
				t.Fatal("equality contract differs")
			}
		})
	}
}

func TestRejectChangedMeaningAndNoncanonicalEvidence(t *testing.T) {
	raw := string(fixture(t, "sni"))
	cases := map[string]string{
		"empty": "", "oversize": strings.Repeat("x", MaxSize+1),
		"unknown":         strings.Replace(raw, `{"version":1`, `{"surprise":true,"version":1`, 1),
		"duplicate":       strings.Replace(raw, `{"version":1`, `{"version":1,"version":1`, 1),
		"schema":          strings.Replace(raw, `"version":1`, `"version":2`, 1),
		"request":         strings.Replace(raw, strings.Repeat("a", 32), strings.Repeat("A", 32), 1),
		"different host":  strings.Replace(raw, "mail.host.example.test", "mail.other.example.test", 1),
		"different root":  strings.Replace(raw, "/etc/ssl/celikpanel", "/etc/ssl/other", 1),
		"escaped path":    strings.Replace(raw, "/fullchain.pem", "/../fullchain.pem", 1),
		"trailing object": raw + `{}`, "newline": raw + "\n", "leading space": " " + raw,
		"null": "null",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if _, e := Decode([]byte(value)); e == nil {
				t.Fatal("invalid accepted-plan evidence accepted")
			}
		})
	}
	empty := string(fixture(t, "empty"))
	for _, tail := range []string{`,"sni":[]}`, `,"sni":null}`} {
		if _, e := Decode([]byte(strings.TrimSuffix(empty, "}") + tail)); e == nil {
			t.Fatal("alternate empty SNI encoding accepted")
		}
	}
}

func TestValidationDoesNotNormalizeOwnerIntent(t *testing.T) {
	plan, e := Decode(fixture(t, "sni"))
	if e != nil {
		t.Fatal(e)
	}
	plan.SNI[0].Names[0], plan.SNI[0].Names[1] = plan.SNI[0].Names[1], plan.SNI[0].Names[0]
	first := plan.SNI[0].Names[0]
	if e = Validate(plan); e == nil {
		t.Fatal("noncanonical ordered intent accepted")
	}
	if plan.SNI[0].Names[0] != first {
		t.Fatal("validation changed caller's intent")
	}
	if _, e = Encode(nil); e == nil {
		t.Fatal("nil plan accepted")
	}
	if Equal(plan, plan) {
		t.Fatal("invalid evidence considered equal")
	}
}
