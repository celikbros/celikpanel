//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
)

func TestSelectedMailRenewalRecoveryPreservesUnverifiedEvidence(t *testing.T) {
	for _, scenario := range []string{"preflight-refusal", "completed", "failed", "missing-job", "foreign-active", "wrong-phase", "other-host", "other-qualifier", "other-leaf", "unknown-selection", "newer-queue", "stage", "dns-journal", "held-lock", "ledger-changed-during-preflight", "selection-changed-during-preflight", "queue-changed-during-preflight", "live-worker"} {
		t.Run(scenario, func(t *testing.T) {
			m, _ := newMutationTestManager(t)
			dir := filepath.Dir(m.ledgerPath)
			t.Setenv("CELIKPANEL_AGENT_STATE_DIR", dir)
			raw := putRenewalScopeFixture(t, m, "alpha81-published-mail-certificate.json")
			ledger, err := decodeServiceMutationLedger(raw)
			if err != nil {
				t.Fatal(err)
			}
			job := ledger.Jobs[testMutationRequestID]
			receipt := mailHostCertificateReceipt{Schema: mailhostartifact.ReceiptSchema, RequestID: job.RequestID, Qualifier: job.PackageName, Domain: job.Target, LeafSHA256: strings.Repeat("a", 64)}
			expected := mailHostRenewal{Lineage: mailHostCertLineageName(receipt.Domain), LeafSHA256: receipt.LeafSHA256}
			pending := expected
			if scenario != "completed" && scenario != "failed" {
				job.Status = serviceMutationStatusRunning
				job.FinishedAt = time.Time{}
				job.LeaseExpiresAt = job.DeadlineAt
				job.Phase, err = formatMailHostCertificateCommitPhase(mailHostCertificateCommitIntent, job.RequestID, job.Target, job.PackageName)
				if err != nil {
					t.Fatal(err)
				}
				ledger.ActiveRequestID = job.RequestID
			}
			switch scenario {
			case "failed":
				job.Status = serviceMutationStatusFailed
			case "missing-job":
				delete(ledger.Jobs, job.RequestID)
			case "foreign-active":
				ledger.ActiveRequestID = testMutationSecondRequestID
			case "wrong-phase":
				job.Phase = "preparing"
			case "other-host":
				receipt.Domain = "other.example.com"
			case "other-qualifier":
				receipt.Qualifier = "mhc1:" + strings.Repeat("b", 64)
			case "other-leaf":
				receipt.LeafSHA256 = strings.Repeat("b", 64)
			case "newer-queue":
				pending.LeafSHA256 = strings.Repeat("b", 64)
			case "live-worker":
				job.WorkerPID = os.Getpid()
				job.WorkerStarted, err = serviceMutationProcessStartIdentity(os.Getpid())
				if err != nil {
					t.Fatal(err)
				}
				job.WorkerCommand = "fixture"
			}
			raw, err = json.Marshal(ledger)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			queue, err := mailhostartifact.CanonicalPending(pending)
			if err != nil {
				t.Fatal(err)
			}
			queuePath := filepath.Join(dir, "mail-host-certificate-renewal.pending")
			if err = writeMailHostRenewalPending(queuePath, queue); err != nil {
				t.Fatal(err)
			}
			var extra string
			if scenario == "stage" {
				extra = ".service-mutations-retained.json"
			}
			if scenario == "dns-journal" {
				extra = dnsEngineSwitchJournalFile
			}
			if extra != "" {
				if err = os.WriteFile(filepath.Join(dir, extra), []byte("owner evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			old := panelCertWithPublishLock
			inPublication := false
			panelCertWithPublishLock = func(action func() error) error {
				inPublication = true
				defer func() { inPublication = false }()
				return action()
			}
			t.Cleanup(func() { panelCertWithPublishLock = old })
			if scenario == "held-lock" {
				lock, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}
			selectedCalls, preflightCalls := 0, 0
			retained, err := recoverSelectedMailRenewalAt(expected, dir, m.lockPath, func() (mailHostCertificateReceipt, error) {
				selectedCalls++
				if !inPublication {
					t.Fatal("selection observed outside publication lock")
				}
				if lock, e := acquireServiceMutationHostAndPublicationLocks(m.lockPath); e == nil {
					lock.Close()
					t.Fatal("lost common host lock")
				}
				if scenario == "unknown-selection" {
					return receipt, os.ErrPermission
				}
				return receipt, nil
			}, func(ctx context.Context, domain string) error {
				preflightCalls++
				if domain != job.Target {
					t.Fatal("wrong preflight scope")
				}
				switch scenario {
				case "ledger-changed-during-preflight":
					job.UpdatedAt = job.UpdatedAt.Add(time.Second)
					raw, err = json.Marshal(ledger)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
						t.Fatal(err)
					}
					return nil
				case "selection-changed-during-preflight":
					receipt.LeafSHA256 = strings.Repeat("b", 64)
					return nil
				case "queue-changed-during-preflight":
					pending.LeafSHA256 = strings.Repeat("b", 64)
					queue, err = mailhostartifact.CanonicalPending(pending)
					if err != nil {
						t.Fatal(err)
					}
					if err = writeMailHostRenewalPending(queuePath, queue); err != nil {
						t.Fatal(err)
					}
					return nil
				}
				return &mailHostReloadUnverified{cause: errors.New("owner stopped native mail")}
			})
			if retained != nil {
				t.Fatal("read-only refusal retained a manager")
			}
			if scenario == "completed" {
				if err != nil || preflightCalls != 0 {
					t.Fatalf("completed receipt: %v", err)
				}
			} else if err == nil {
				t.Fatal("unverified recovery accepted")
			}
			if scenario == "preflight-refusal" || strings.HasSuffix(scenario, "during-preflight") {
				if preflightCalls != 1 {
					t.Fatalf("preflight not reached: %d, %v", preflightCalls, err)
				}
			} else if preflightCalls != 0 {
				t.Fatal("invalid evidence reached host preflight")
			}
			if scenario == "preflight-refusal" && selectedCalls != 1 {
				t.Fatal("unexpected selection count")
			}
			assertRenewalScopeBytes(t, m.ledgerPath, raw)
			assertRenewalScopeBytes(t, queuePath, queue)
			if extra != "" {
				assertRenewalScopeBytes(t, filepath.Join(dir, extra), []byte("owner evidence"))
			}
			if scenario != "held-lock" {
				lock, e := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
				if e != nil {
					t.Fatalf("refusal retained exclusion: %v", e)
				}
				if e = lock.Close(); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestMailRenewalRecoveryRequiresProvenWorkerAbsence(t *testing.T) {
	for _, tc := range []struct {
		name, actual string
		err          error
		gone         bool
	}{
		{"same", "123", nil, false}, {"different", "124", nil, true}, {"missing", "", os.ErrNotExist, true}, {"permission", "", os.ErrPermission, false}, {"malformed", "", errors.New("invalid proc stat"), false}, {"empty", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := &ServiceMutationJob{WorkerPID: 123, WorkerStarted: "123"}
			err := mailRenewalWorkerGone(job, func(pid int) (string, error) {
				if pid != 123 {
					t.Fatal("wrong worker")
				}
				return tc.actual, tc.err
			})
			if (err == nil) != tc.gone {
				t.Fatalf("unknown worker promoted to absent: %v", err)
			}
		})
	}
	for _, job := range []*ServiceMutationJob{{WorkerPID: 1}, {WorkerStarted: "123"}, {WorkerPID: -1, WorkerStarted: "123"}} {
		if mailRenewalWorkerGone(job, func(int) (string, error) { t.Fatal("malformed identity read"); return "", nil }) == nil {
			t.Fatal("invalid worker identity accepted")
		}
	}
	if err := mailRenewalWorkerGone(&ServiceMutationJob{}, func(int) (string, error) { t.Fatal("no worker to read"); return "", nil }); err != nil {
		t.Fatal(err)
	}
}
