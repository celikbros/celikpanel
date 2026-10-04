//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreLedgerEmptyResidueCannotBecomeAlternateOwnerLedger(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires distinct root/service group identities")
	}
	root := mutationTestRoot(t)
	serviceMutationRequiredOwnerGID = 1234
	state := filepath.Join(root, "state")
	runtime := filepath.Join(root, "runtime")
	for _, p := range []string{state, runtime} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chown(runtime, 0, 1234); err != nil {
		t.Fatal(err)
	}
	original := packageManagerMutationBusyProbe
	t.Cleanup(func() { packageManagerMutationBusyProbe = original })
	raw, err := canonicalInitialServiceMutationLedger()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, serviceMutationLedgerFileName)
	packageManagerMutationBusyProbe = func() (bool, error) {
		// The owner creates evidence after the first empty-directory observation.
		return false, os.WriteFile(path, raw, 0600)
	}
	err = checkPreLedgerServiceMutationIdle(state, filepath.Join(runtime, "service-mutation.lock"))
	if !errors.Is(err, errServiceMutationNotIdle) {
		t.Fatalf("alternate owner evidence admitted: %v", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(raw, after) {
		t.Fatalf("owner-created evidence changed: %v", readErr)
	}
	if _, found, err := readSecureServiceMutationLedger(path, serviceMutationLedgerMaxSize); err == nil || found {
		t.Fatal("strict reader accepted alternate owner evidence")
	}
}
