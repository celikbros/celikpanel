//go:build !linux

package hostplatform

import (
	"fmt"
	"runtime"
)

func Detect() (Profile, error) {
	return Profile{}, fmt.Errorf("managed-server platform detection requires Linux; running on %s", runtime.GOOS)
}
