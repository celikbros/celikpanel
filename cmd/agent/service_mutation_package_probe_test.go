package main

import (
	"os"
	"runtime"
	"testing"
)

// TestMain isolates ledger and supervisor tests from package-manager locks on
// the machine running the test binary. Production keeps the real host probe.
// TestMain, ledger ve supervisor testlerini test ikilisini çalıştıran makinenin
// paket yöneticisi kilitlerinden yalıtır. Üretim gerçek host probunu korur.
func TestMain(m *testing.M) {
	packageManagerMutationBusyProbe = func() (bool, error) {
		return false, nil
	}
	// Unit tests exercise the mutation coordinator on portability builds, where
	// production deliberately refuses every host mutation.
	if runtime.GOOS != "linux" {
		verifyServiceMutationSecurityPolicy = func() error { return nil }
	}
	os.Exit(m.Run())
}
