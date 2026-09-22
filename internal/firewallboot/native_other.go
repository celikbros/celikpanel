//go:build !linux

package firewallboot

import (
	"context"
	"errors"
)

func Restore(context.Context, bool) (Result, error) {
	return Result{}, errors.New("native nft firewall restore requires Linux")
}
