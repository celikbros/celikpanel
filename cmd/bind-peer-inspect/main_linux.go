//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, failureLine(err))
		os.Exit(1)
	}
}

// inspectionFailure keeps the decoded request's digest with an inspection
// error, so the only detail the primary can receive is a reviewed reason token
// bound to that exact challenge.
type inspectionFailure struct {
	requestSHA256 string
	err           error
}

func (f *inspectionFailure) Error() string { return f.err.Error() }
func (f *inspectionFailure) Unwrap() error { return f.err }

// failureLine is the single stderr line of a failed inspection: the reviewed
// reason line when the error is classified and bound to a decoded request,
// otherwise the fixed generic sentence. Raw error text is never printed.
func failureLine(err error) string {
	var failure *inspectionFailure
	if errors.As(err, &failure) {
		if line, ok := dnspeerproof.FormatInspectorReason(
			failure.requestSHA256, bindpeerinspector.Reason(failure.err),
		); ok {
			return line
		}
	}
	return "native BIND peer observation unavailable"
}

func run() error {
	if len(os.Args) != 1 || (os.Getenv("SSH_ORIGINAL_COMMAND") != "" && os.Getenv("SSH_ORIGINAL_COMMAND") != "celikpanel-bind-peer-inspect-v1") {
		return errors.New("inspector accepts no command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return runWith(ctx, os.Stdin, os.Stdout, bindpeerinspector.OwnerPolicyReader{}, bindpeerinspector.NativeReader{}, time.Now)
}

func runWith(ctx context.Context, input io.Reader, output io.Writer, policy bindpeerinspector.PolicyReader, native bindpeerinspector.Reader, now func() time.Time) error {
	raw, err := io.ReadAll(io.LimitReader(input, 4098))
	if err != nil || len(raw) > 4097 {
		return errors.New("request exceeds safe bound")
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	request, err := dnspeerproof.DecodeRequest(raw)
	if err != nil {
		return err
	}
	digest, err := dnspeerproof.RequestSHA256(request)
	if err != nil {
		return err
	}
	response, err := bindpeerinspector.Inspect(ctx, request, native, policy, now)
	if err != nil {
		return &inspectionFailure{requestSHA256: digest, err: err}
	}
	encoded, err := dnspeerproof.EncodeResponse(response)
	if err != nil {
		return err
	}
	_, err = output.Write(append(encoded, '\n'))
	return err
}
