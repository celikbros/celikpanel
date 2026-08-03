package main

import (
	"testing"
	"time"
)

func TestAppUnitMutationLockSerializesSameSite(t *testing.T) {
	unlockFirst := lockAppUnitMutation(41)
	acquiredSecond := make(chan struct{})
	releaseSecond := make(chan struct{})

	go func() {
		unlockSecond := lockAppUnitMutation(41)
		close(acquiredSecond)
		<-releaseSecond
		unlockSecond()
	}()

	select {
	case <-acquiredSecond:
		t.Fatal("second mutation acquired the same site lock too early")
	case <-time.After(50 * time.Millisecond):
	}

	unlockFirst()
	select {
	case <-acquiredSecond:
	case <-time.After(time.Second):
		t.Fatal("second mutation did not acquire the released site lock")
	}
	close(releaseSecond)
}

func TestAppUnitMutationLockAllowsDifferentSites(t *testing.T) {
	unlockFirst := lockAppUnitMutation(41)
	defer unlockFirst()

	acquiredSecond := make(chan struct{})
	go func() {
		unlockSecond := lockAppUnitMutation(42)
		close(acquiredSecond)
		unlockSecond()
	}()

	select {
	case <-acquiredSecond:
	case <-time.After(time.Second):
		t.Fatal("different site mutation was unnecessarily blocked")
	}
}
