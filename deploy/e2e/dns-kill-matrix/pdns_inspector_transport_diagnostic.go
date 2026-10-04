//go:build ignore

// Standalone Linux diagnostic, run with `go run <file>`; excluded from ./... builds.

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/pdnspeerenrollment"
	"github.com/alicelik/celikpanel/internal/pdnspeerjournal"
	"github.com/alicelik/celikpanel/internal/pdnspeertransport"
)

// Fixture only: fresh non-journal nonce, read-only forced command, no retirement.
func main() {
	record, err := pdnspeerjournal.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal read:", err)
		os.Exit(1)
	}
	enrollment, err := pdnspeerenrollment.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "enrollment read:", err)
		os.Exit(1)
	}
	r := record.Request
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		fmt.Fprintln(os.Stderr, "random unavailable")
		os.Exit(1)
	}
	r.Nonce = hex.EncodeToString(nonce[:])
	r.Attempt++
	r.IssuedAtUnix = time.Now().Unix()
	r.ExpiresAtUnix = r.IssuedAtUnix + 90
	response, auth, err := pdnspeertransport.Inspect(context.Background(), enrollment.Transport, r, pdnspeertransport.SSH{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "transport:", err)
		os.Exit(1)
	}
	fmt.Printf("transport: response=%s/%s/%s authenticated=%t\n", response.CatalogState, response.MemberState, response.NativeState, auth.Established)
}
