//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailRenewalAcknowledgementRequiresFreshCompletion(t *testing.T) {
	for _, scenario := range []string{"complete", "active", "missing-ledger", "missing-job", "failed", "wrong-phase", "other-kind", "other-host", "other-qualifier", "different-selected-leaf", "different-lineage", "unknown-selection", "retained-stage", "retained-dns", "newer-queue", "held-host-lock"} {
		t.Run(scenario, func(t *testing.T) {
			m, _ := newMutationTestManager(t)
			t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Dir(m.ledgerPath))
			t.Setenv("CELIKPANEL_MUTATION_LOCK", m.lockPath)
			original := putRenewalScopeFixture(t, m, "alpha81-published-mail-certificate.json")
			ledger, err := decodeServiceMutationLedger(original)
			if err != nil {
				t.Fatal(err)
			}
			job := ledger.Jobs[testMutationRequestID]
			receipt := mailHostCertificateReceipt{Schema: mailhostartifact.ReceiptSchema, RequestID: job.RequestID, Qualifier: job.PackageName, Domain: job.Target, LeafSHA256: strings.Repeat("a", 64)}
			expected := mailHostRenewal{Lineage: mailHostCertLineageName(receipt.Domain), LeafSHA256: receipt.LeafSHA256}
			pending := expected
			switch scenario {
			case "missing-job":
				delete(ledger.Jobs, job.RequestID)
			case "failed":
				job.Status = serviceMutationStatusFailed
			case "wrong-phase":
				job.Phase = "pending"
			case "other-kind":
				job.Kind = "mail_tls_sync"
			case "other-host":
				job.Target = "other.example.com"
			case "other-qualifier":
				job.PackageName = "mhc1:" + strings.Repeat("b", 64)
			case "different-selected-leaf":
				receipt.LeafSHA256 = strings.Repeat("b", 64)
			case "different-lineage":
				expected.Lineage = mailHostCertLineageName("other.example.com")
				pending = expected
			case "newer-queue":
				pending.LeafSHA256 = strings.Repeat("b", 64)
			}
			// Keep malformed/cross-identity evidence as actual bytes too: the strict
			// shared decoder must refuse it before queue removal.
			raw, err := json.Marshal(ledger)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "active" {
				raw = putRenewalScopeFixture(t, m, "alpha81-running.json")
			}
			if scenario == "missing-ledger" {
				if err = os.Remove(m.ledgerPath); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "retained-stage" || scenario == "retained-dns" {
				name := ".service-mutations-retained.json"
				if scenario == "retained-dns" {
					name = dnsEngineSwitchJournalFile
				}
				if err = os.WriteFile(filepath.Join(filepath.Dir(m.ledgerPath), name), []byte("retained unknown evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			queue, err := mailhostartifact.CanonicalPending(pending)
			if err != nil {
				t.Fatal(err)
			}
			if err = writeMailHostRenewalPending(mailHostRenewalPendingPath(), queue); err != nil {
				t.Fatal(err)
			}
			old := panelCertWithPublishLock
			inPublication := false
			panelCertWithPublishLock = func(action func() error) error {
				inPublication = true
				defer func() { inPublication = false }()
				return action()
			}
			t.Cleanup(func() { panelCertWithPublishLock = old })
			if scenario == "held-host-lock" {
				held, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
				if err != nil {
					t.Fatal(err)
				}
				defer held.Close()
			}
			removed := false
			err = acknowledgeMailHostRenewal(expected, func() (mailHostCertificateReceipt, error) {
				if !inPublication {
					t.Fatal("selection read outside certificate publication lock")
				}
				if held, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath); err == nil {
					held.Close()
					t.Fatal("acknowledgement did not retain common lock")
				}
				if scenario == "unknown-selection" {
					return receipt, errors.New("unknown selection")
				}
				return receipt, nil
			}, func(value mailHostRenewal) error {
				removed = true
				return removeMailHostRenewalUnderPublicationLock(value)
			})
			if scenario == "complete" {
				if err != nil || !removed {
					t.Fatalf("verified acknowledgement failed: %v", err)
				}
				if _, err = os.Stat(mailHostRenewalPendingPath()); !os.IsNotExist(err) {
					t.Fatal("completed queue retained")
				}
			} else {
				if err == nil {
					t.Fatal("unverified acknowledgement accepted")
				}
				assertRenewalScopeBytes(t, mailHostRenewalPendingPath(), queue)
				if scenario != "newer-queue" && removed {
					t.Fatal("removal reached without completion proof")
				}
			}
			if scenario != "missing-ledger" {
				assertRenewalScopeBytes(t, m.ledgerPath, raw)
			}
		})
	}
}
