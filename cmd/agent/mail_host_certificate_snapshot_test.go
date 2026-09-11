//go:build linux

package main

import (
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// A clean rollback makes the machine mutable again, but its failed desired
// journal must never become the configuration used by unattended TLS renewal.
func TestMailHostCertificateSnapshotSurvivesFailedMailTLSChange(t *testing.T) {
	for _, priorSuccess := range []bool{false, true} {
		name := "failed_initial_configuration"
		if priorSuccess {
			name = "failed_change_after_success"
		}
		t.Run(name, func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
			t.Setenv("CELIKPANEL_MUTATION_LOCK", filepath.Join(root, "service-mutation.lock"))
			original := mailTLSSyncTestCommitment(t)
			var committed *mailTLSSyncJournal
			if priorSuccess {
				ctx, finish := acquireMailTLSSyncTestStep(t, manager, original)
				var err error
				committed, err = commitStandaloneMailTLSSyncIntent(ctx, mailTLSSyncPreparedJournal(original))
				if err == nil {
					err = publishStandaloneMailTLSSync(ctx, committed)
				}
				finish()
				if err != nil {
					t.Fatal(err)
				}
			}

			// The proposed change has both another hostname and another SNI
			// snapshot. Neither part is safe to replay after a clean rollback.
			proposed, err := mutationpayload.CanonicalMailTLSSync(original.ManagedRoot, "changed.panel.test", nil)
			if err != nil {
				t.Fatal(err)
			}
			binding := ServiceMutationBinding{MutationRequestID: testMutationSecondRequestID, MutationOwnerID: testMutationOwnerID}
			if _, err = manager.begin(&ServiceMutationBeginRequest{RequestID: binding.MutationRequestID, OwnerID: binding.MutationOwnerID, Kind: "mail_tls_sync", Target: "mail-tls", PackageName: proposed.Qualifier}); err != nil {
				t.Fatal(err)
			}
			ctx, finish, err := manager.acquireStep(binding, newServiceMutationStepClaim(serviceMutationStepSyncMailTLS, "mail-tls", proposed.Qualifier, "sync"))
			if err != nil {
				t.Fatal(err)
			}
			journal, err := commitStandaloneMailTLSSyncIntent(ctx, mailTLSSyncPreparedJournal(proposed))
			if err == nil {
				err = failStandaloneMailTLSSync(ctx, journal, mailTLSSyncFailedUntouchedCode, "The proposed configuration was not applied.")
			}
			finish()
			if err != nil {
				t.Fatal(err)
			}
			if failed := manager.status(binding.MutationRequestID); failed == nil || failed.Status != serviceMutationStatusFailed {
				t.Fatalf("failed change retained mutation ownership: %+v", failed)
			}

			assertSnapshot := func(stage string) {
				t.Helper()
				got, err := loadMailHostCertificatePlan()
				if !priorSuccess {
					if err == nil {
						t.Errorf("%s: failed initial mail TLS journal became a renewal source: %+v", stage, got)
					}
					return
				}
				if err != nil || !equalMailTLSSyncJournals(got, committed) {
					t.Errorf("%s: renewal lost the successful hostname/SNI snapshot: got=%+v error=%v want=%+v", stage, got, err, committed)
				}
			}
			assertSnapshot("before restart")
			reloadMailTLSSyncTestManager(t, root)
			assertSnapshot("after restart")
		})
	}
}
