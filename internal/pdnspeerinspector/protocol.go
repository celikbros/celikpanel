package pdnspeerinspector

import (
	"context"
	"errors"
	"io"
	"time"

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
	response, err := Inspect(ctx, request, native, policy, now)
	if err != nil {
		return err
	}
	encoded, err := pdnspeerproof.EncodeResponse(response)
	if err != nil {
		return err
	}
	_, err = output.Write(append(encoded, '\n'))
	return err
}
