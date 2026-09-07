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
	// The same isolation, for the other thing these tests read off the machine:
	// whether the host has finished starting. Startup recovery asks, and when
	// the answer is "still starting" it correctly defers the decision to a
	// goroutine that waits for the host. A test that reloads a manager and
	// asserts the decision was made then returns before the goroutine runs,
	// t.TempDir() removes the state directory underneath it, and the run fails
	// with a missing lock directory - at random, because the answer depends on
	// the machine running the tests rather than on anything the test set up.
	//
	// Ten test files reload a manager that way. Pinning them one at a time
	// would be the same mistake this codebase keeps finding: fixing where it
	// was found and leaving the sibling. So the default is here, once, beside
	// the probe that was already pinned for exactly this reason. The one file
	// that is ABOUT readiness - host_boot_recovery_test.go - overrides it per
	// test to drive all three answers, and always did.
	//
	// Ayni yalitim, bu testlerin makineden okudugu diger sey icin: makinenin
	// acilisinin bitip bitmedigi. On test dosyasi yoneticiyi bu sekilde yeniden
	// yukluyor; onlari tek tek sabitlemek, bu kod tabaninin surekli buldugu
	// hatanin ayni olurdu. Varsayilan burada, bir kez.
	hostRecoveryProbe = func() (hostRecoveryReadiness, error) {
		return hostRecoveryDecideNow, nil
	}
	// Unit tests exercise the mutation coordinator on portability builds, where
	// production deliberately refuses every host mutation.
	if runtime.GOOS != "linux" {
		verifyServiceMutationSecurityPolicy = func() error { return nil }
	}
	os.Exit(m.Run())
}
