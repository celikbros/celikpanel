//go:build !linux

package bindpeerinspector

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

type Command func(context.Context, string, ...string) ([]byte, error)
type NativeReader struct{ Run Command }

func (NativeReader) Read(context.Context, dnspeerproof.RequestV1) (Snapshot, error) {
	return Snapshot{}, errors.New("native BIND peer inspection is available only on Linux")
}
