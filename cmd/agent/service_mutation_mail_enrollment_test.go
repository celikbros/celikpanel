//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func TestMailEnrollmentReservationSurvivesAgentAndGenericRPC(t *testing.T) {
	for _, direction := range []string{servicemutationledger.MailEnrollmentForward, servicemutationledger.MailEnrollmentRollback} {
		t.Run(direction, func(t *testing.T) {
			manager, _ := newMutationTestManager(t)
			id := servicemutationledger.MailEnrollmentIdentity{RequestID: testMutationRequestID, OwnerID: testMutationOwnerID, ScopeSHA256: strings.Repeat("a", 64)}
			now := time.Now().UTC().Add(-48 * time.Hour)
			ledger, err := servicemutationledger.AdmitMailEnrollment(&manager.ledger, id, now)
			if err != nil {
				t.Fatal(err)
			}
			if direction == servicemutationledger.MailEnrollmentRollback {
				ledger, err = servicemutationledger.AdvanceMailEnrollment(&ledger, id, direction, now.Add(time.Minute))
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := servicemutationledger.Encode(&ledger)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(manager.ledgerPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			// Three fresh managers prove startup and polling cannot expire the durable
			// reservation, clear the pointer, replay work or overwrite known guidance.
			for i := 0; i < 3; i++ {
				fresh, err := newServiceMutationManager(filepath.Dir(manager.ledgerPath), manager.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				if fresh.healthErrorLocked() != nil || fresh.active != nil {
					t.Fatal("reservation poisoned healthy manager or created worker")
				}
				job := fresh.status(id.RequestID)
				if job == nil || job.Status != serviceMutationStatusOrphaned {
					t.Fatal(job)
				}
				ordinary := &ServiceMutationBeginRequest{RequestID: testMutationSecondRequestID, OwnerID: testMutationOwnerID, Kind: "service_install", Target: "nginx"}
				if _, err := fresh.begin(ordinary); !errors.Is(err, errServiceMutationBusy) {
					t.Fatal("unrelated begin", err)
				}
				reserved := &ServiceMutationBeginRequest{RequestID: id.RequestID, OwnerID: id.OwnerID, Kind: servicemutationledger.MailEnrollmentKind, Target: servicemutationledger.MailEnrollmentTarget, PackageName: job.PackageName, Resume: true}
				if _, err := fresh.begin(reserved); !errors.Is(err, servicemutationledger.ErrMailEnrollment) {
					t.Fatal("generic enrollment begin", err)
				}
				if _, err := fresh.heartbeat(&ServiceMutationHeartbeatRequest{RequestID: id.RequestID, OwnerID: id.OwnerID, Phase: "done"}); err == nil {
					t.Fatal("heartbeat changed enrollment")
				}
				if _, err := fresh.cancelJob(&ServiceMutationCancelRequest{RequestID: id.RequestID, ExpectedOwner: id.OwnerID}); err == nil {
					t.Fatal("generic cancel released enrollment")
				}
				if _, err := fresh.finish(&ServiceMutationFinishRequest{RequestID: id.RequestID, OwnerID: id.OwnerID, Success: true}); err == nil {
					t.Fatal("generic finish released enrollment")
				}
				if err := fresh.finishPersistedOrphanLocked(fresh.ledger.Jobs[id.RequestID], "interrupted", "dead worker"); err == nil {
					t.Fatal("generic orphan finalizer released enrollment")
				}
				if err := checkServiceMutationIdle(filepath.Dir(manager.ledgerPath), manager.lockPath); !errors.Is(err, errServiceMutationNotIdle) {
					t.Fatal("update accepted nonidle enrollment", err)
				}
				after, err := os.ReadFile(manager.ledgerPath)
				if err != nil || !bytes.Equal(raw, after) {
					t.Fatal("observer rewrote durable evidence", err)
				}
			}
		})
	}
}
func TestMailEnrollmentGenericAdmissionNeverCreatesReservation(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	before, _ := os.ReadFile(manager.ledgerPath)
	request := &ServiceMutationBeginRequest{RequestID: testMutationRequestID, OwnerID: testMutationOwnerID, Kind: servicemutationledger.MailEnrollmentKind, Target: servicemutationledger.MailEnrollmentTarget, PackageName: servicemutationledger.MailEnrollmentQualifierPrefix + strings.Repeat("a", 64)}
	if _, err := manager.begin(request); !errors.Is(err, servicemutationledger.ErrMailEnrollment) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(manager.ledgerPath)
	if !bytes.Equal(before, after) || manager.active != nil {
		t.Fatal("RPC admitted enrollment")
	}
}
