package agentnativecontract

import (
	"bytes"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"strings"
	"testing"
)

func TestContractBindsExactAgentWithoutExecutingIt(t *testing.T) {
	agent := []byte("fixture Agent bytes; this is data, not an executable")
	c, err := New(agent, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Verify(raw, agent); err != nil || got != c {
		t.Fatal(got, err)
	}
	if _, err := Verify(raw, append(bytes.Clone(agent), '!')); err == nil {
		t.Fatal("different binary accepted")
	}
	if _, err := Verify(nil, agent); err == nil {
		t.Fatal("historical absence invented compatibility")
	}
}
func TestContractRefusesAmbiguousOrUnsupportedEvidence(t *testing.T) {
	c, _ := New([]byte("agent"), strings.Repeat("a", 40))
	raw, _ := Encode(c)
	cases := [][]byte{nil, []byte("null\n"), raw[:len(raw)-1], append(bytes.Clone(raw), ' '), append(bytes.Clone(raw), raw...), bytes.Replace(raw, []byte("/v1"), []byte("/v2"), 1), bytes.Replace(raw, []byte(MailHookPolicy), []byte("legacy-overwrite"), 1), bytes.Replace(raw, []byte(`{"schema":`), []byte(`{"unknown":true,"schema":`), 1), bytes.Replace(raw, []byte(`{"schema":`), []byte(`{"schema":"ignored","schema":`), 1), bytes.Replace(raw, []byte(strings.Repeat("a", 40)), []byte("unknown"), 1), bytes.Repeat([]byte(" "), MaxSize+1)}
	for n, raw := range cases {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("case %d accepted", n)
		}
	}
}
func TestProducerRequiresBoundSourceAndNonemptyBinary(t *testing.T) {
	for _, commit := range []string{"", "unknown", strings.Repeat("A", 40), strings.Repeat("a", 39)} {
		if _, err := New([]byte("agent"), commit); err == nil {
			t.Fatal(commit)
		}
	}
	if _, err := New(nil, strings.Repeat("a", 40)); err == nil {
		t.Fatal("empty binary")
	}
}

func TestEnrollmentCapabilityDoesNotCertifyHistoricalAgents(t *testing.T) {
	c, _ := New([]byte("agent"), strings.Repeat("a", 40))
	if c.MailEnrollmentPolicy != MailEnrollmentPolicy {
		t.Fatal("current producer lacks retention capability")
	}
	c.MailEnrollmentPolicy = ""
	raw, err := Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("mail_enrollment_policy")) {
		t.Fatal("changed historical envelope")
	}
	old, err := Verify(raw, []byte("agent"))
	if err != nil || old.MailEnrollmentPolicy != "" {
		t.Fatal(old, err)
	}
	c.MailEnrollmentPolicy = "unknown"
	if _, err := Encode(c); err == nil {
		t.Fatal("unknown capability")
	}
}

func TestExactMailRuntimeBinding(t *testing.T) {
	c, _ := New([]byte("agent"), strings.Repeat("a", 40))
	old, _ := Encode(c)
	parsed, err := Parse(old)
	if err != nil || parsed.MailRenewalGeneration != "" {
		t.Fatal("old retention-only declaration gained a target", err)
	}
	again, _ := Encode(parsed)
	if !bytes.Equal(old, again) {
		t.Fatal("historical bytes changed")
	}
	m, files, _ := mailrenewalkit.Payload([]byte("reviewed runtime A"))
	manifest, _ := mailrenewalkit.Encode(m)
	bound, err := BindMailRenewal(c, manifest, files)
	if err != nil || bound.MailRenewalGeneration != m.Generation {
		t.Fatal(bound, err)
	}
	raw, _ := Encode(bound)
	if got, err := Verify(raw, []byte("agent")); err != nil || got != bound {
		t.Fatal(got, err)
	}
	if got, err := BindMailRenewal(bound, manifest, files); err != nil || got != bound {
		t.Fatal("exact retry", err)
	}
	other, otherFiles, _ := mailrenewalkit.Payload([]byte("reviewed runtime B"))
	otherRaw, _ := mailrenewalkit.Encode(other)
	if _, err := BindMailRenewal(bound, otherRaw, otherFiles); err == nil {
		t.Fatal("existing binding redirected")
	}
	if _, err := BindMailRenewal(c, manifest, otherFiles); err == nil {
		t.Fatal("mixed release files accepted")
	}
	files[mailrenewalkit.TimerName] = []byte("owner timer")
	if _, err := BindMailRenewal(c, manifest, files); err == nil {
		t.Fatal("modified native template accepted")
	}
	c.MailEnrollmentPolicy = ""
	if _, err := BindMailRenewal(c, otherRaw, otherFiles); err == nil {
		t.Fatal("historical Agent certified")
	}
	for _, bad := range []string{"x", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		bound.MailRenewalGeneration = bad
		if _, err := Encode(bound); err == nil {
			t.Fatal("invalid target", bad)
		}
	}
	bound.MailRenewalGeneration = m.Generation
	bound.MailEnrollmentPolicy = ""
	if _, err := Encode(bound); err == nil {
		t.Fatal("target without retention capability")
	}
}
