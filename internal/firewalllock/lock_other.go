//go:build !linux

package firewalllock

import (
	"errors"
	"os"
)

func Acquire() (*os.File, error) {
	return nil, errors.New("native firewall exclusion is supported only on Linux")
}
