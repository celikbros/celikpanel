//go:build !linux

package dnsenginerecovery

import (
	"context"
	"errors"
)

func InspectPDNSDatabaseFile(context.Context, string, bool) (bool, int64, string, error) {
	return false, 0, "", errors.New("PowerDNS database inspection requires Linux")
}
