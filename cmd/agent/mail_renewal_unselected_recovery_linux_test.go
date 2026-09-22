//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mailrenewalintent"
)

func prepareUnselectedRenewalTest(t *testing.T) (state, tls, lockPath string, request *ServiceMutationBeginRequest, leaf []byte) {
	t.Helper()
	state, tls, request, leaf = mailRenewalBeforeTestMaterial(t)
	base, _ := newMutationTestManager(t)
	lockPath = base.lockPath
	raw, e := os.ReadFile(base.ledgerPath)
	if e != nil {
		t.Fatal(e)
	}
	ledgerPath := filepath.Join(state, "service-mutations.json")
	if e = os.WriteFile(ledgerPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chown(ledgerPath, 0, int(serviceMutationRequiredOwnerGID)); e != nil {
		t.Fatal(e)
	}
	m, e := newMailRenewalMutationManager(state, lockPath, request)
	if e != nil {
		t.Fatal(e)
	}
	m.mailRenewalBeforeAdmission = func(got *ServiceMutationBeginRequest) error {
		return persistMailRenewalBeforeAt(state, tls, got, buildCommit, func(string) ([]byte, error) { return leaf, nil }, func() error { return nil })
	}
	if _, e = m.begin(request); e != nil {
		t.Fatal(e)
	}
	// Unit fixture simulates process loss after the actual admission writer.
	// Native subprocess SIGKILL acceptance is separate.
	m.mu.Lock()
	runtime := m.active
	m.active = nil
	runtime.cancel()
	e = runtime.lock.Close()
	m.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	return
}

func TestUnselectedMailRenewalRecoveryRequiresExactBeforeImageAndPreservesOwner(t *testing.T) {
	for _, scenario := range []string{"leased", "intent", "budget-exhausted", "missing-before", "corrupt-before", "foreign-build", "foreign-owner", "selected-link-replaced", "key-replaced", "new-source", "new-queue", "unknown-source", "native-refusal", "owner-stopped", "live-worker", "unknown-phase", "known-failure", "foreign-stage", "foreign-journal", "held-lock", "selection-during-preflight", "ledger-during-preflight", "queue-during-preflight", "terminal-before-rename", "terminal-after-rename"} {
		t.Run(scenario, func(t *testing.T) {
			state, tls, lockPath, request, leaf := prepareUnselectedRenewalTest(t)
			pending := mailhostartifact.Pending{Lineage: mailhostartifact.LineageName(request.Target), LeafSHA256: mailhostartifact.LeafSHA256(leaf)}
			ledgerPath := filepath.Join(state, "service-mutations.json")
			raw, e := os.ReadFile(ledgerPath)
			if e != nil {
				t.Fatal(e)
			}
			ledger, e := decodeServiceMutationLedger(raw)
			if e != nil {
				t.Fatal(e)
			}
			job := ledger.Jobs[request.RequestID]
			name, _ := mailrenewalintent.FileName(request.RequestID)
			beforePath := filepath.Join(state, name)
			changeLink := func() {
				t.Helper()
				link := filepath.Join(tls, "current")
				target, e := os.Readlink(link)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(link, link+"-owner-kept"); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(target, link); e != nil {
					t.Fatal(e)
				}
			}
			writeLedger := func() {
				t.Helper()
				data, e := encodeServiceMutationLedger(&ledger)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(ledgerPath, data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			changeQueue := func() {
				t.Helper()
				other := pending
				other.LeafSHA256 = strings.Repeat("f", 64)
				data, e := mailhostartifact.CanonicalPending(other)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(state, "mail-host-certificate-renewal.pending"), data, 0600); e != nil {
					t.Fatal(e)
				}
			}
			build := buildCommit
			switch scenario {
			case "intent":
				job.Phase, e = formatMailHostCertificateCommitPhase(mailHostCertificateCommitIntent, job.RequestID, job.Target, job.PackageName)
				if e != nil {
					t.Fatal(e)
				}
			case "budget-exhausted":
				job.Attempt = 3
			case "missing-before":
				if e = os.Remove(beforePath); e != nil {
					t.Fatal(e)
				}
			case "corrupt-before":
				if e = os.WriteFile(beforePath, []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			case "foreign-build":
				build = strings.Repeat("f", 40)
			case "foreign-owner":
				job.OwnerID = strings.Repeat("f", 32)
			case "selected-link-replaced":
				changeLink()
			case "key-replaced":
				link, e := os.Readlink(filepath.Join(tls, "current"))
				if e != nil {
					t.Fatal(e)
				}
				path := filepath.Join(tls, link, "privkey.pem")
				key, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.Rename(path, path+"-owner-kept"); e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path, key, 0600); e != nil {
					t.Fatal(e)
				}
			case "new-queue":
				changeQueue()
			case "live-worker":
				job.WorkerPID = os.Getpid()
				job.WorkerStarted, e = serviceMutationProcessStartIdentity(os.Getpid())
				if e != nil {
					t.Fatal(e)
				}
				job.WorkerCommand = "fixture"
			case "unknown-phase":
				job.Phase = "unrecognized"
			case "known-failure":
				job.ErrorCode = "known_native_failure"
				job.ErrorMessage = "must remain visible"
			case "foreign-stage":
				if e = os.WriteFile(filepath.Join(state, ".service-mutations-owner.json"), []byte("owner"), 0600); e != nil {
					t.Fatal(e)
				}
			case "foreign-journal":
				if e = os.WriteFile(filepath.Join(state, dnsEngineSwitchJournalFile), []byte("owner"), 0600); e != nil {
					t.Fatal(e)
				}
			case "held-lock":
				held, e := acquireServiceMutationHostAndPublicationLocks(lockPath)
				if e != nil {
					t.Fatal(e)
				}
				defer held.Close()
			}
			writeLedger()
			original, e := os.ReadFile(ledgerPath)
			if e != nil {
				t.Fatal(e)
			}
			oldLock := panelCertWithPublishLock
			inPublication := false
			panelCertWithPublishLock = func(action func() error) error {
				inPublication = true
				defer func() { inPublication = false }()
				return action()
			}
			defer func() { panelCertWithPublishLock = oldLock }()
			reads := 0
			read := func(string) ([]byte, error) {
				reads++
				if !inPublication {
					t.Fatal("source outside publication exclusion")
				}
				if scenario == "unknown-source" {
					return nil, os.ErrPermission
				}
				if scenario == "new-source" {
					return []byte("other"), nil
				}
				return leaf, nil
			}
			preflight := func(ctx context.Context, domain string) error {
				if domain != request.Target || !inPublication {
					t.Fatal("wrong native preflight boundary")
				}
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				if held, e := acquireServiceMutationHostAndPublicationLocks(lockPath); e == nil {
					held.Close()
					t.Fatal("recovery lost host exclusion")
				}
				switch scenario {
				case "native-refusal", "owner-stopped":
					return errors.New("native configuration/activity not accepted")
				case "selection-during-preflight":
					changeLink()
				case "ledger-during-preflight":
					job.Attempt++
					writeLedger()
					original, _ = os.ReadFile(ledgerPath)
				case "queue-during-preflight":
					changeQueue()
				}
				return nil
			}
			fault := func(point string) error {
				if scenario == "terminal-before-rename" && point == serviceMutationWriteFaultBeforeRename || scenario == "terminal-after-rename" && point == serviceMutationWriteFaultAfterRename {
					return errors.New("terminal uncertainty")
				}
				return nil
			}
			retained, e := recoverUnselectedMailRenewalAt(pending, state, lockPath, tls, build, read, preflight, fault)
			success := scenario == "leased" || scenario == "intent" || scenario == "budget-exhausted"
			if success {
				if e != nil || retained == nil {
					t.Fatal("proven unselected attempt not settled", e)
				}
				got, e := os.ReadFile(ledgerPath)
				if e != nil {
					t.Fatal(e)
				}
				done, e := decodeServiceMutationLedger(got)
				if e != nil {
					t.Fatal(e)
				}
				final := done.Jobs[request.RequestID]
				if done.ActiveRequestID != "" || final.Status != serviceMutationStatusFailed || final.Attempt != job.Attempt || final.ErrorCode != "mail_renewal_interrupted_before_selection" || reads < 2 {
					t.Fatal("wrong interruption result")
				}
				if _, e = os.Stat(beforePath); e != nil {
					t.Fatal("before-image removed", e)
				}
				if _, e = os.Stat(filepath.Join(state, "mail-host-certificate-renewal.pending")); e != nil {
					t.Fatal("pending source lost", e)
				}
				request.Resume = true
				if e = admitMailRenewalFailedRetry(final, request, ""); (scenario == "budget-exhausted") != (e != nil) {
					t.Fatal("retry budget changed", e)
				}
			} else {
				if e == nil {
					t.Fatal("unverified recovery accepted")
				}
				if strings.HasPrefix(scenario, "terminal-") {
					if retained == nil || retained.poisoned == nil {
						t.Fatal("terminal uncertainty lost owner")
					}
					if held, e := acquireServiceMutationHostAndPublicationLocks(lockPath); e == nil {
						held.Close()
						t.Fatal("uncertain publication released exclusion")
					}
					releasePoisonedVPNPeerSyncTestManager(retained)
				} else {
					if retained != nil {
						t.Fatal("preflight refusal created recovery owner")
					}
					after, e := os.ReadFile(ledgerPath)
					if e != nil || !bytes.Equal(original, after) {
						t.Fatal("unverified evidence changed", e)
					}
				}
			}
		})
	}
}
