package main

import (
	"os"
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
	os.Exit(m.Run())
}
