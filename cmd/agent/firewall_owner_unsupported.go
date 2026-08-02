//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package main

import (
	"fmt"
	"os"
	"runtime"
)

// requireRootOwner fails closed on platforms that cannot prove Unix root
// ownership. Privileged firewall state and executable trust checks must never
// treat missing ownership metadata as authorization.
func requireRootOwner(_ os.FileInfo, label string) error {
	return fmt.Errorf("%s owner UID validation is unsupported on %s", label, runtime.GOOS)
}
