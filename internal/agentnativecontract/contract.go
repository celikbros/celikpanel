// Package agentnativecontract describes a trusted release's native service
// compatibility claim, bound to its exact Agent bytes. Parsing is not signing,
// owner authority, proof of behavior, or permission to enroll a native service.
package agentnativecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

const Schema = "celikpanel-agent-native-contract/v1"
const FileName = "agent-native-contract.json"
const MailHookPolicy = "preserve-independent-v1"
const DNSEvidencePolicy = "separated-acquisition-publication-v2"
const MailEnrollmentPolicy = "retain-enrollment-ledger-v1"
const MaxSize = 2048
const MaxAgentSize = 128 << 20

var ErrContract = errors.New("Agent native service compatibility is unverified; preserve native service evidence and use a release with a matching supported Agent contract")
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Contract struct {
	Schema                string `json:"schema"`
	SourceCommit          string `json:"source_commit"`
	AgentSHA256           string `json:"agent_sha256"`
	MailHookPolicy        string `json:"mail_hook_policy"`
	MailEnrollmentPolicy  string `json:"mail_enrollment_policy,omitempty"`
	MailRenewalGeneration string `json:"mail_renewal_generation,omitempty"`
	DNSEvidencePolicy     string `json:"dns_evidence_policy,omitempty"`
}

// New is only the reviewed current-source build producer. It must never be used
// to retroactively certify a historical executable. The outer release signature
// and verified snapshot provenance establish the claim's authority; this package
// only binds the declaration to bytes and checks its supported semantics.
func New(agent []byte, commit string) (Contract, error) {
	if len(agent) == 0 || len(agent) > MaxAgentSize || !commitPattern.MatchString(commit) {
		return Contract{}, ErrContract
	}
	digest := sha256.Sum256(agent)
	return Contract{Schema: Schema, SourceCommit: commit, AgentSHA256: hex.EncodeToString(digest[:]), MailHookPolicy: MailHookPolicy, MailEnrollmentPolicy: MailEnrollmentPolicy, DNSEvidencePolicy: DNSEvidencePolicy}, nil
}
func Encode(c Contract) ([]byte, error) {
	if c.Schema != Schema || !commitPattern.MatchString(c.SourceCommit) || !digestPattern.MatchString(c.AgentSHA256) || c.MailHookPolicy != MailHookPolicy ||
		(c.MailEnrollmentPolicy != "" && c.MailEnrollmentPolicy != MailEnrollmentPolicy) ||
		(c.DNSEvidencePolicy != "" && c.DNSEvidencePolicy != DNSEvidencePolicy) ||
		(c.MailRenewalGeneration != "" && (!digestPattern.MatchString(c.MailRenewalGeneration) || c.MailEnrollmentPolicy != MailEnrollmentPolicy)) {
		return nil, ErrContract
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw)+1 > MaxSize {
		return nil, ErrContract
	}
	return append(raw, '\n'), nil
}
func Parse(raw []byte) (Contract, error) {
	var c Contract
	if len(raw) == 0 || len(raw) > MaxSize {
		return c, ErrContract
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return Contract{}, ErrContract
	}
	canonical, err := Encode(c)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Contract{}, ErrContract
	}
	return c, nil
}

// Verify never starts the target Agent, including historical versions whose
// unknown CLI flags could start a management daemon instead of an inspection.
func Verify(raw, agent []byte) (Contract, error) {
	c, err := Parse(raw)
	if err != nil {
		return Contract{}, err
	}
	if len(agent) == 0 || len(agent) > MaxAgentSize {
		return Contract{}, ErrContract
	}
	digest := sha256.Sum256(agent)
	if c.AgentSHA256 != hex.EncodeToString(digest[:]) {
		return Contract{}, ErrContract
	}
	return c, nil
}

// BindMailRenewal binds the current release producer's exact verified kit. It
// does not choose among installed generations or authorize enrollment. Existing
// bindings cannot be redirected, and historical declarations gain no capability.
func BindMailRenewal(c Contract, manifest []byte, files map[string][]byte) (Contract, error) {
	if _, err := Encode(c); err != nil || c.MailEnrollmentPolicy != MailEnrollmentPolicy {
		return Contract{}, ErrContract
	}
	kit, err := mailrenewalkit.Verify(manifest, files)
	if err != nil || (c.MailRenewalGeneration != "" && c.MailRenewalGeneration != kit.Generation) {
		return Contract{}, ErrContract
	}
	c.MailRenewalGeneration = kit.Generation
	return c, nil
}
