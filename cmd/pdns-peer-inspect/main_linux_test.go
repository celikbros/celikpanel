//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnspeerinspector"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
)

type missingPolicy struct{ reads int }

func (p *missingPolicy) Read(context.Context) (pdnspeerinspector.OwnerPolicyV1, string, error) {
	p.reads++
	return pdnspeerinspector.OwnerPolicyV1{}, "", errors.New("missing")
}

type nativeProbe struct{ reads int }

func (n *nativeProbe) Read(context.Context, pdnspeerproof.RequestV1, pdnspeerinspector.OwnerPolicyV1) (pdnspeerinspector.Snapshot, error) {
	n.reads++
	return pdnspeerinspector.Snapshot{}, errors.New("must not run")
}
func requestDocument(t *testing.T) []byte {
	t.Helper()
	digest, err := pdnspeerproof.CatalogMembersSHA256("192.0.2.10", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := pdnspeerproof.RequestV1{
		Schema:            pdnspeerproof.RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 3, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64),
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogSerial: 2,
		CatalogMembersSHA256: digest, DeletedZone: "gone.example.test", View: dnspeerproof.DefaultView,
		Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060,
	}
	raw, err := pdnspeerproof.EncodeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestMissingOwnerPolicyStopsBeforeNativeRead(t *testing.T) {
	policy := &missingPolicy{}
	native := &nativeProbe{}
	var output bytes.Buffer
	err := runWith(context.Background(), bytes.NewReader(append(requestDocument(t), '\n')), &output, policy, native, func() time.Time { return time.Unix(1800000002, 0) })
	if err == nil || policy.reads != 1 || native.reads != 0 || output.Len() != 0 {
		t.Fatalf("unsafe command result: %v policy=%d native=%d output=%q", err, policy.reads, native.reads, output.String())
	}
}
func TestBINDDocumentAndOversizeNeverReadPolicy(t *testing.T) {
	raw := requestDocument(t)
	raw = bytes.Replace(raw, []byte(pdnspeerproof.RequestSchemaV1), []byte(dnspeerproof.RequestSchemaV1), 1)
	for _, input := range [][]byte{raw, bytes.Repeat([]byte{'x'}, 4098)} {
		policy := &missingPolicy{}
		native := &nativeProbe{}
		var output bytes.Buffer
		if err := runWith(context.Background(), bytes.NewReader(input), &output, policy, native, time.Now); err == nil || policy.reads != 0 || native.reads != 0 || output.Len() != 0 {
			t.Fatalf("unsafe command input accepted: err=%v policy=%d native=%d", err, policy.reads, native.reads)
		}
	}
}

type unreviewedConfig struct{}

func (unreviewedConfig) Read(context.Context) (pdnspeerinspector.OwnerPolicyV1, string, error) {
	return pdnspeerinspector.OwnerPolicyV1{
		Schema: pdnspeerinspector.PolicySchemaV1, PrimaryIP: "192.0.2.10", PeerIP: "192.0.2.11",
		CatalogName: "catalog-c000020a.celikpanel.invalid", CatalogAccount: "fixture-pdns-peer",
	}, "policy", nil
}

type configReader struct{}

func (configReader) Read(context.Context, pdnspeerproof.RequestV1, pdnspeerinspector.OwnerPolicyV1) (pdnspeerinspector.Snapshot, error) {
	return pdnspeerinspector.Snapshot{}, &pdnspeerinspector.ReasonError{Reason: "config_unreviewed", Message: "/etc/powerdns/pdns.d/zz.conf"}
}

func TestFailureLineIsTheReviewedReasonOrTheGenericSentence(t *testing.T) {
	raw := requestDocument(t)
	request, err := pdnspeerproof.DecodeRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := pdnspeerproof.RequestSHA256(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runWith(context.Background(), bytes.NewReader(append(raw, '\n')), &output, unreviewedConfig{}, configReader{}, func() time.Time { return time.Unix(1800000002, 0) })
	if got := failureLine(err); got != "celikpanel-peer-inspect-reason/v1 "+digest+" config_unreviewed" || output.Len() != 0 {
		t.Fatalf("reason line: %q (output %q)", got, output.String())
	}
	if strings.Contains(failureLine(err), "/etc/") {
		t.Fatal("local error text crossed to stderr")
	}
	err = runWith(context.Background(), bytes.NewReader(append(raw, '\n')), &output, &missingPolicy{}, &nativeProbe{}, time.Now)
	if got := failureLine(err); got != "native PowerDNS peer observation unavailable" {
		t.Fatalf("generic line: %q", got)
	}
	if got := failureLine(errors.New("inspector accepts no command")); got != "native PowerDNS peer observation unavailable" {
		t.Fatalf("generic line: %q", got)
	}
}
