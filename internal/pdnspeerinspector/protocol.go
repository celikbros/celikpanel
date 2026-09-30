package pdnspeerinspector

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
)

// Serve handles one bounded canonical request and emits only one minimal proof
// document. An SSH forced-command wrapper and authenticated client are separate
// enrollment prerequisites; this package exposes no shell or mutation API.
func Serve(ctx context.Context, input io.Reader, output io.Writer, policy PolicyReader, native Reader, now func() time.Time) error {
	if input == nil || output == nil {
		return errors.New("PowerDNS inspector stream unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(input, 4098))
	if err != nil || len(raw) > 4097 {
		return errors.New("PowerDNS inspector request exceeds safe bound")
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	request, err := pdnspeerproof.DecodeRequest(raw)
	if err != nil {
		return err
	}
	digest, err := pdnspeerproof.RequestSHA256(request)
	if err != nil {
		return err
	}
	response, err := Inspect(ctx, request, native, policy, now)
	if err != nil {
		return &InspectionFailure{RequestSHA256: digest, Err: err}
	}
	encoded, err := pdnspeerproof.EncodeResponse(response)
	if err != nil {
		return err
	}
	_, err = output.Write(append(encoded, '\n'))
	return err
}

// InspectionFailure keeps the decoded request's digest with an inspection
// error, so the only detail the primary can receive is a reviewed reason
// token bound to that exact challenge.
type InspectionFailure struct {
	RequestSHA256 string
	Err           error
}

func (f *InspectionFailure) Error() string { return f.Err.Error() }
func (f *InspectionFailure) Unwrap() error { return f.Err }

// ReasonLine returns the single reviewed stderr line
// (dnspeerproof.InspectorReasonPrefixV1) for a failed inspection that is bound
// to a decoded request and classified with a reviewed reason. Anything else
// yields false and the caller prints its fixed generic sentence; raw error
// text is never printed.
func ReasonLine(err error) (string, bool) {
	var failure *InspectionFailure
	if !errors.As(err, &failure) {
		return "", false
	}
	return dnspeerproof.FormatInspectorReason(failure.RequestSHA256, Reason(failure.Err))
}
