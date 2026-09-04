//go:build !linux

package main

import (
	"errors"
	"os"
)

// Taking or restoring a control-plane archive is a root-only Linux operation.
// The archive library itself is portable so the round trip can be exercised on
// the development host, but the inherited-descriptor discipline and the
// root-owned path chain have no meaning here.
//
// Kontrol düzlemi arşivini almak ya da geri yüklemek yalnız-root bir Linux
// işlemidir. Arşiv kitaplığı taşınabilirdir; devralınan descriptor disiplini
// burada anlamsızdır.

func readControlPlaneKeyFile(_ *os.File) (string, error) {
	return "", errors.New("reading the control-plane key from stdin is supported only on Linux")
}

func controlPlaneValidateRootOwnedDirectoryChain(_ string) error {
	return errors.New("control-plane archives require Linux")
}
