//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

type missingOwnerPolicy struct{ reads int }

func (p *missingOwnerPolicy) Read(context.Context) (bindpeerinspector.OwnerPolicyV1, string, error) {
	p.reads++
	return bindpeerinspector.OwnerPolicyV1{}, "", errors.New("missing")
}

type nativeSpy struct{ reads int }

func (n *nativeSpy) Read(context.Context, dnspeerproof.RequestV1) (bindpeerinspector.Snapshot, error) {
	n.reads++
	return bindpeerinspector.Snapshot{}, errors.New("must not run")
}

func TestCommandStartupRejectsMissingPolicyBeforeNative(t *testing.T) {
	digest, err := dnspeerproof.CatalogMembersSHA256("192.0.2.10", 12, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := binddns.CatalogDomain("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	req := dnspeerproof.RequestV1{
		Schema: dnspeerproof.RequestSchemaV1, MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID: strings.Repeat("b", 32), DeletionGeneration: 1,
		DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP:         "192.0.2.10", PeerIP: "192.0.2.11", PeerIdentitySHA256: strings.Repeat("d", 64),
		CatalogName: catalog, CatalogSerial: 12, CatalogMembersSHA256: digest,
		DeletedZone: "gone.example.test", View: dnspeerproof.DefaultView,
		Nonce: strings.Repeat("e", 64), Attempt: 1, IssuedAtUnix: 1800000000, ExpiresAtUnix: 1800000060,
	}
	raw, err := dnspeerproof.EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	policy := &missingOwnerPolicy{}
	native := &nativeSpy{}
	var output bytes.Buffer
	err = runWith(context.Background(), bytes.NewReader(append(raw, '\n')), &output, policy, native, func() time.Time { return time.Unix(1800000002, 0) })
	if err == nil || policy.reads != 1 || native.reads != 0 || output.Len() != 0 {
		t.Fatalf("missing policy did not stop command startup: err=%v policy=%d native=%d output=%q", err, policy.reads, native.reads, output.String())
	}
}
