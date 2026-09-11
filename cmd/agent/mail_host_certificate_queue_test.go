//go:build linux

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailHostRenewalQueueUsesServiceGroupForWriteReadAndRemoval(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to prove a different service group")
	}
	previousUID, previousGID := serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, 1234
	t.Cleanup(func() { serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = previousUID, previousGID })
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(t.TempDir(), "state"))
	pending := mailHostRenewal{Lineage: "celikpanel-mail-" + strings.Repeat("a", 24), LeafSHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(pending)
	path := mailHostRenewalPendingPath()
	if err := writeMailHostRenewalPending(path, raw); err != nil {
		t.Fatal(err)
	}
	read, found, err := readSecureServiceMutationLedger(path, 512)
	if err != nil || !found || string(read) != string(raw) {
		t.Fatalf("queue writer disagrees with protected reader: found=%v error=%v", found, err)
	}
	fd, err := openMailHostRenewalStateDirectory(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	os.NewFile(uintptr(fd), filepath.Dir(path)).Close()
	// clearMailHostCertificateRenewal includes the exact-content compare and
	// the cross-process certificate publication lock. Its directory proof
	// must permit precisely the service group used above.
	oldLock := panelCertWithPublishLock
	panelCertWithPublishLock = func(action func() error) error { return action() }
	t.Cleanup(func() { panelCertWithPublishLock = oldLock })
	if err := clearMailHostCertificateRenewal(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("completed queue remains: %v", err)
	}
}
