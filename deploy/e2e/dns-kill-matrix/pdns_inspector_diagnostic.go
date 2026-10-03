//go:build ignore

// Standalone Linux diagnostic, run with `go run <file>`; excluded from ./... builds.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alicelik/celikpanel/internal/pdnspeerinspector"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
)

// Fixture-only read-only diagnostic. The request is intentionally synthetic;
// it cannot retire a production challenge or mutate DNS state.
func main() {
	policy, _, err := (pdnspeerinspector.OwnerPolicyReader{}).Read(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "policy:", err)
		os.Exit(1)
	}
	r := pdnspeerproof.RequestV1{
		Schema:               pdnspeerproof.RequestSchemaV1,
		MutationRequestID:    "e103302b8168e6fc677312675b4304b4",
		MutationOwnerID:      "3901316504b1219db00b863d0e45ae48",
		DeletionGeneration:   1,
		DeletionQualifier:    "dns-zone-sync/v3:sha256:5507b380b4a0488ac478ae2183c2594e247e8e588a20b5b6c4fc3d5c40098c45",
		PrimaryIP:            policy.PrimaryIP,
		PeerIP:               policy.PeerIP,
		PeerIdentitySHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CatalogName:          policy.CatalogName,
		CatalogSerial:        2,
		CatalogMembersSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DeletedZone:          "s1-kill.test",
		View:                 "_default",
		Nonce:                "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Attempt:              1,
		IssuedAtUnix:         1,
		ExpiresAtUnix:        2,
	}
	if _, err := (pdnspeerinspector.NativeReader{}).Read(context.Background(), r, policy); err != nil {
		fmt.Fprintln(os.Stderr, "native:", err)
		os.Exit(1)
	}
	fmt.Println("native: read succeeded")
}
