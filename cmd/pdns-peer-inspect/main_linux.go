//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/pdnspeerinspector"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, failureLine(err))
		os.Exit(1)
	}
}

// failureLine is the single stderr line of a failed inspection: the reviewed
// reason line when the error is classified and bound to a decoded request,
// otherwise the fixed generic sentence. Raw error text is never printed.
func failureLine(err error) string {
	if line, ok := pdnspeerinspector.ReasonLine(err); ok {
		return line
	}
	return "native PowerDNS peer observation unavailable"
}

// An owner must install this as a separately reviewed, root-owned forced
// command for a dedicated non-root SSH account. The command itself does not
// enroll accounts or mutate DNS state.
func run() error {
	if len(os.Args) != 1 || (os.Getenv("SSH_ORIGINAL_COMMAND") != "" &&
		os.Getenv("SSH_ORIGINAL_COMMAND") != dnspeertransport.FixedPowerDNSCommand) {
		return errors.New("inspector accepts no command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return runWith(ctx, os.Stdin, os.Stdout, pdnspeerinspector.OwnerPolicyReader{}, pdnspeerinspector.NativeReader{}, time.Now)
}

func runWith(ctx context.Context, input io.Reader, output io.Writer, policy pdnspeerinspector.PolicyReader, native pdnspeerinspector.Reader, now func() time.Time) error {
	return pdnspeerinspector.Serve(ctx, input, output, policy, native, now)
}
