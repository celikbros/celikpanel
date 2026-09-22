//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func renewalScopeTestRequest(t *testing.T) *ServiceMutationBeginRequest {
	t.Helper()
	c, err := mutationpayload.CanonicalMailHostCertificate("mail.example.com", "renewal@celikpanel.invalid", "")
	if err != nil {
		t.Fatal(err)
	}
	return &ServiceMutationBeginRequest{RequestID: testMutationSecondRequestID, OwnerID: testMutationOwnerID, Kind: "mail_host_certificate", Target: c.Domain, PackageName: c.Qualifier}
}

func putRenewalScopeFixture(t *testing.T, m *serviceMutationManager, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../internal/servicemutationledger/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertRenewalScopeBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("evidence changed at %s: %v", path, err)
	}
}

func TestMailRenewalScopePreservesForeignEvidence(t *testing.T) {
	cases := []string{"active", "own-active", "writer-stage", "firewall-stage", "mail-stage", "cluster-stage", "dns-journal", "invalid-mail-journal"}
	for _, scenario := range cases {
		t.Run(scenario, func(t *testing.T) {
			m, _ := newMutationTestManager(t)
			request := renewalScopeTestRequest(t)
			var extraPath string
			extra := []byte("owner evidence; do not clean or interpret as permission")
			switch scenario {
			case "active", "own-active":
				raw := putRenewalScopeFixture(t, m, "alpha81-running.json")
				if scenario == "own-active" {
					ledger, err := decodeServiceMutationLedger(raw)
					if err != nil {
						t.Fatal(err)
					}
					job := ledger.Jobs[ledger.ActiveRequestID]
					delete(ledger.Jobs, ledger.ActiveRequestID)
					job.RequestID, job.OwnerID, job.Kind, job.Target, job.PackageName = request.RequestID, request.OwnerID, request.Kind, request.Target, request.PackageName
					ledger.ActiveRequestID = request.RequestID
					ledger.Jobs[request.RequestID] = job
					raw, err = encodeServiceMutationLedger(&ledger)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "writer-stage":
				extraPath = ".service-mutations-retained.json"
			case "firewall-stage":
				extraPath = ".firewall-apply-journal-retained.json"
			case "mail-stage":
				extraPath = ".mail-tls-sync-journal-retained.json"
			case "cluster-stage":
				extraPath = ".dns-cluster-config-journal-retained.json"
			case "dns-journal":
				extraPath = dnsEngineSwitchJournalFile
			case "invalid-mail-journal":
				extraPath = mailTLSSyncJournalFileName
			}
			if extraPath != "" {
				extraPath = filepath.Join(filepath.Dir(m.ledgerPath), extraPath)
				if err := os.WriteFile(extraPath, extra, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(m.ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			scoped, err := newLedgerOnlyMailRenewalTestManager(filepath.Dir(m.ledgerPath), m.lockPath, request)
			if scoped != nil || !errors.Is(err, errMailRenewalRecoveryRequired) {
				t.Fatalf("scoped=%v err=%v", scoped, err)
			}
			assertRenewalScopeBytes(t, m.ledgerPath, before)
			if extraPath != "" {
				assertRenewalScopeBytes(t, extraPath, extra)
			}
			lock, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
			if err != nil {
				t.Fatalf("read-only refusal retained lock: %v", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMailRenewalScopeRechecksFreshEvidenceAtBeginAndStatus(t *testing.T) {
	m, _ := newMutationTestManager(t)
	request := renewalScopeTestRequest(t)
	scoped, err := newLedgerOnlyMailRenewalTestManager(filepath.Dir(m.ledgerPath), m.lockPath, request)
	if err != nil {
		t.Fatal(err)
	}
	before := putRenewalScopeFixture(t, m, "alpha81-running.json")
	_ = scoped.status(request.RequestID)
	if _, err := scoped.begin(request); !errors.Is(err, errMailRenewalRecoveryRequired) {
		t.Fatalf("stale admission: %v", err)
	}
	assertRenewalScopeBytes(t, m.ledgerPath, before)
	if scoped.active != nil || scoped.hostBootWait != nil {
		t.Fatal("renewal dispatched recovery")
	}
}

func TestMailRenewalScopeRejectsBroaderIdentityBeforeRecovery(t *testing.T) {
	m, _ := newMutationTestManager(t)
	original := renewalScopeTestRequest(t)
	scoped, err := newLedgerOnlyMailRenewalTestManager(filepath.Dir(m.ledgerPath), m.lockPath, original)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the constructor argument must not change retained scope.
	original.Kind = "service_install"
	original.Target = "nginx"
	original.PackageName = ""
	before := putRenewalScopeFixture(t, m, "alpha81-running.json")
	if _, err := scoped.begin(original); err == nil {
		t.Fatal("broadened authority")
	}
	assertRenewalScopeBytes(t, m.ledgerPath, before)
	for _, field := range []string{"request", "owner", "domain", "qualifier"} {
		request := renewalScopeTestRequest(t)
		switch field {
		case "request":
			request.RequestID = testMutationRequestID
		case "owner":
			request.OwnerID = testMutationRequestID
		case "domain":
			request.Target = "other.example.com"
		case "qualifier":
			request.PackageName = "mhc1:" + fmt.Sprintf("%064x", 1)
		}
		if _, err := scoped.begin(request); err == nil || errors.Is(err, errMailRenewalRecoveryRequired) {
			t.Fatalf("%s not rejected before recovery: %v", field, err)
		}
		assertRenewalScopeBytes(t, m.ledgerPath, before)
	}
}

func TestMailRenewalScopeOwnLifecyclePreservesHistory(t *testing.T) {
	m, _ := newMutationTestManager(t)
	raw := putRenewalScopeFixture(t, m, "alpha81-failed.json")
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	seed := *ledger.Jobs[testMutationRequestID]
	for i := 1; i <= serviceMutationHistoryLimit; i++ {
		job := seed
		job.RequestID = fmt.Sprintf("%032x", i)
		ledger.Jobs[job.RequestID] = &job
	}
	raw, err = encodeServiceMutationLedger(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request := renewalScopeTestRequest(t)
	scoped, err := newLedgerOnlyMailRenewalTestManager(filepath.Dir(m.ledgerPath), m.lockPath, request)
	if err != nil {
		t.Fatal(err)
	}
	job, err := scoped.begin(request)
	if err != nil || job.Status != serviceMutationStatusRunning {
		t.Fatalf("begin: %+v %v", job, err)
	}
	_, err = scoped.finish(&ServiceMutationFinishRequest{RequestID: request.RequestID, OwnerID: request.OwnerID, FailureCode: "test_refused", Message: "fixture stopped before publication"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(m.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeServiceMutationLedger(after)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActiveRequestID != "" || len(result.Jobs) != len(ledger.Jobs)+1 {
		t.Fatal("renewal discarded history or retained active pointer")
	}
	delete(result.Jobs, request.RequestID)
	preserved, err := encodeServiceMutationLedger(&result)
	if err != nil || !bytes.Equal(preserved, raw) {
		t.Fatalf("foreign history changed: %v", err)
	}
}

func TestMailRenewalScopeSharesExclusionWithOrdinaryAgent(t *testing.T) {
	ordinary, _ := newMutationTestManager(t)
	request := renewalScopeTestRequest(t)
	scoped, err := newLedgerOnlyMailRenewalTestManager(filepath.Dir(ordinary.ledgerPath), ordinary.lockPath, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.begin(request); err != nil {
		t.Fatal(err)
	}
	other := &ServiceMutationBeginRequest{RequestID: testMutationRequestID, OwnerID: testMutationOwnerID, Kind: "service_install", Target: "nginx"}
	if _, err := ordinary.begin(other); !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("ordinary Agent entered renewal lease: %v", err)
	}
	if _, err := scoped.finish(&ServiceMutationFinishRequest{RequestID: request.RequestID, OwnerID: request.OwnerID, FailureCode: "fixture", Message: "stopped before publication"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.begin(other); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinary.finish(&ServiceMutationFinishRequest{RequestID: other.RequestID, OwnerID: other.OwnerID, FailureCode: "fixture", Message: "stopped before publication"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ordinary.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Jobs) != 2 || ledger.Jobs[request.RequestID].Status != serviceMutationStatusFailed || ledger.Jobs[other.RequestID].Status != serviceMutationStatusFailed {
		t.Fatal("cross-manager publication lost an operation")
	}
}

func TestMailRenewalScopeDoesNotTakeOverHistoricalOwner(t *testing.T) {
	m, _ := newMutationTestManager(t)
	request := renewalScopeTestRequest(t)
	raw := putRenewalScopeFixture(t, m, "alpha81-failed.json")
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	job := ledger.Jobs[testMutationRequestID]
	delete(ledger.Jobs, testMutationRequestID)
	job.RequestID, job.Kind, job.Target, job.PackageName = request.RequestID, request.Kind, request.Target, request.PackageName
	job.OwnerID = testMutationRequestID
	ledger.Jobs[request.RequestID] = job
	raw, err = encodeServiceMutationLedger(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(m.ledgerPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newLedgerOnlyMailRenewalTestManager(filepath.Dir(m.ledgerPath), m.lockPath, request); !errors.Is(err, errMailRenewalRecoveryRequired) {
		t.Fatalf("owner takeover: %v", err)
	}
	assertRenewalScopeBytes(t, m.ledgerPath, raw)
}

// These ledger/selected-publication fixtures deliberately start below the new
// source/selection admission adapter. They do not establish before-selection
// recovery coverage; that adapter has separate material and interruption tests.
func newLedgerOnlyMailRenewalTestManager(stateDir, lockPath string, request *ServiceMutationBeginRequest) (*serviceMutationManager, error) {
	m, err := newMailRenewalMutationManager(stateDir, lockPath, request)
	if m != nil {
		m.mailRenewalBeforeAdmission = func(*ServiceMutationBeginRequest) error { return nil }
	}
	return m, err
}
