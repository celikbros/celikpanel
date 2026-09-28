//go:build linux

// A read-only, disposable-guest probe for the owner-enrolled BIND inspector.
// It never consumes a proof or marks a DNS mutation complete.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerenrollment"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/dnspeertransport"
)

const trialCell = "pdns-switch__intent__after-write__paired-primary__peer-reachable"

func run() error {
	serial := flag.Uint("catalog-serial", 0, "observed native catalog SOA serial")
	member := flag.String("member", "", "observed catalog member")
	zone := flag.String("zone", "", "zone to inspect")
	emitRequest := flag.Bool("emit-request", false, "emit one expiring read-only challenge for SSH diagnosis")
	flag.Parse()
	if flag.NArg() != 0 || *serial == 0 || *serial > 1<<32-1 || *zone == "" {
		return errors.New("provide one observed catalog serial, member and zone")
	}
	marker, err := os.ReadFile("/etc/celikpanel-dns-kill-matrix")
	if err != nil || string(marker) != "schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id="+trialCell+"\nnode=debian13\n" {
		return errors.New("not the exact disposable Debian primary")
	}
	enrollment, err := dnspeerenrollment.Read()
	if err != nil {
		return err
	}
	if enrollment.Record.PrimaryIP != "192.0.2.10" || enrollment.Record.PeerIP != "192.0.2.11" ||
		enrollment.Record.CatalogName != "catalog-c000020a.celikpanel.invalid" {
		return errors.New("owner enrollment is not for this native pair")
	}
	catalogMembers := []string(nil)
	if *member != "" {
		catalogMembers = []string{*member}
	}
	members, err := dnspeerproof.CatalogMembersSHA256(enrollment.Record.PrimaryIP, uint32(*serial), catalogMembers)
	if err != nil {
		return err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	now := time.Now()
	request := dnspeerproof.RequestV1{
		Schema:            dnspeerproof.RequestSchemaV1,
		MutationRequestID: strings.Repeat("a", 32), MutationOwnerID: strings.Repeat("b", 32),
		DeletionGeneration: 2, DeletionQualifier: "dns-zone-sync/v3:sha256:" + strings.Repeat("c", 64),
		PrimaryIP: enrollment.Record.PrimaryIP, PeerIP: enrollment.Record.PeerIP,
		PeerIdentitySHA256: enrollment.Record.HostKeySHA256, CatalogName: enrollment.Record.CatalogName,
		CatalogSerial: uint32(*serial), CatalogMembersSHA256: members,
		DeletedZone: *zone, View: dnspeerproof.DefaultView,
		Nonce: hex.EncodeToString(nonce[:]), Attempt: 1,
		IssuedAtUnix: now.Unix(), ExpiresAtUnix: now.Add(30 * time.Second).Unix(),
	}
	if err := request.Validate(); err != nil {
		return err
	}
	if *emitRequest {
		raw, err := dnspeerproof.EncodeRequest(request)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(raw)
		return err
	}
	response, auth, err := dnspeertransport.Inspect(context.Background(), enrollment.Transport, request, dnspeertransport.SSH{})
	if err != nil {
		return err
	}
	if err := dnspeerenrollment.Recheck(enrollment); err != nil {
		return err
	}
	_, proofErr := dnspeerproof.Verify(request, response, auth, time.Now(), func(string) bool { return false })
	if proofErr == nil {
		return errors.New("read-only probe unexpectedly consumed a proof")
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		CatalogSerial uint32 `json:"catalog_serial"`
		Zone          string `json:"zone"`
		CatalogState  string `json:"catalog_state"`
		MemberState   string `json:"member_state"`
		NativeState   string `json:"native_state"`
		Authenticated bool   `json:"authenticated"`
		ProofGate     string `json:"proof_gate"`
	}{uint32(*serial), *zone, response.CatalogState, response.MemberState,
		response.NativeState, auth.Established, proofErr.Error()})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
