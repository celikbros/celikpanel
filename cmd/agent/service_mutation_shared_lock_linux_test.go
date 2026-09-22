//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
)

func TestServiceMutationSharedExclusionInteroperatesWithActualProducer(t *testing.T) {
	root := mutationTestRoot(t)
	path := filepath.Join(root, "service-mutation.lock")
	producer, err := acquireServiceMutationFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if file, err := hostmutationlock.AcquireExisting(path, serviceMutationLockOwner()); !errors.Is(err, hostmutationlock.ErrBusy) {
		if file != nil {
			file.Close()
		}
		t.Fatalf("shared consumer did not exclude actual producer: %v", err)
	}
	if err = verifyInheritedServiceMutationFileLockFD(path, int(producer.file.Fd())); err != nil {
		t.Fatal(err)
	}
	if err = producer.Close(); err != nil {
		t.Fatal(err)
	}
	consumer, err := hostmutationlock.AcquireExisting(path, serviceMutationLockOwner())
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if writer, err := acquireServiceMutationFileLock(path); !errors.Is(err, errServiceMutationHostBusy) {
		if writer != nil {
			writer.Close()
		}
		t.Fatalf("actual producer did not exclude shared consumer: %v", err)
	}
	if err = probeServiceMutationFileLockIdle(path); !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("probe lost typed busy: %v", err)
	}
	if err = consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = probeServiceMutationFileLockIdle(path); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("owner data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = probeServiceMutationFileLockIdle(path); err == nil {
		t.Fatal("nonempty owner file treated as idle")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "owner data" {
		t.Fatalf("owner data changed: %q %v", raw, err)
	}
}
