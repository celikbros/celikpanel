//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMailRenewalFailedBudgetDurableFreshAdmissionAndOwnerRetry(t *testing.T) {
	base, _ := newMutationTestManager(t)
	request := renewalScopeTestRequest(t)
	newScoped := func() *serviceMutationManager {
		t.Helper()
		m, err := newMailRenewalMutationManager(filepath.Dir(base.ledgerPath), base.lockPath, request)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	fail := func(m *serviceMutationManager) {
		t.Helper()
		if _, err := m.finish(&ServiceMutationFinishRequest{RequestID: request.RequestID, OwnerID: request.OwnerID, FailureCode: "mail_host_renewal_failed", Message: "accepted configuration needs review"}); err != nil {
			t.Fatal(err)
		}
	}
	var stale *serviceMutationManager
	for attempt := 1; attempt <= 3; attempt++ {
		m := newScoped()
		if attempt == 3 {
			stale = newScoped()
		} // Created before another manager consumes the final automatic attempt.
		request.Resume = attempt > 1
		job, err := m.begin(request)
		if err != nil || job.Attempt != attempt {
			t.Fatalf("attempt %d: %+v %v", attempt, job, err)
		}
		fail(m)
	}
	before, err := os.ReadFile(base.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []*serviceMutationManager{stale, newScoped(), newScoped()} {
		_, err = m.begin(request)
		var budget *mailRenewalFailedBudgetError
		if !errors.As(err, &budget) || budget.RequestID != request.RequestID {
			t.Fatalf("unbounded automatic admission: %v", err)
		}
		assertRenewalScopeBytes(t, base.ledgerPath, before)
		if m.active != nil {
			t.Fatal("refusal acquired a worker")
		}
	}
	wrong := newScoped()
	wrong.mailRenewalFailedOwnerRequest = testMutationRequestID
	if _, err = wrong.begin(request); err == nil {
		t.Fatal("other owner retry accepted")
	}
	assertRenewalScopeBytes(t, base.ledgerPath, before)
	owner := newScoped()
	owner.mailRenewalFailedOwnerRequest = request.RequestID
	job, err := owner.begin(request)
	if err != nil || job.Attempt != 4 {
		t.Fatalf("explicit attempt: %+v %v", job, err)
	}
	if owner.mailRenewalFailedOwnerRequest != "" {
		t.Fatal("durable admission did not consume owner grant")
	}
	fail(owner)
	before, err = os.ReadFile(base.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []*serviceMutationManager{owner, newScoped()} {
		_, err = m.begin(request)
		var budget *mailRenewalFailedBudgetError
		if !errors.As(err, &budget) {
			t.Fatalf("owner command reset automatic budget: %v", err)
		}
		assertRenewalScopeBytes(t, base.ledgerPath, before)
	}
	lock, err := acquireServiceMutationHostAndPublicationLocks(base.lockPath)
	if err != nil {
		t.Fatalf("refusal leaked exclusion: %v", err)
	}
	if err = lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMailRenewalFailedOwnerRetryCannotCreateOperation(t *testing.T) {
	m, _ := newMutationTestManager(t)
	request := renewalScopeTestRequest(t)
	scoped, err := newMailRenewalMutationManager(filepath.Dir(m.ledgerPath), m.lockPath, request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(m.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	scoped.mailRenewalFailedOwnerRequest = request.RequestID
	request.Resume = true
	if _, err = scoped.begin(request); err == nil {
		t.Fatal("owner retry created a new operation")
	}
	assertRenewalScopeBytes(t, m.ledgerPath, before)
}

func TestMailRenewalFailedBudgetRejectsInvalidEvidenceAndKeepsSuccess(t *testing.T) {
	request := renewalScopeTestRequest(t)
	request.Resume = true
	job := &ServiceMutationJob{RequestID: request.RequestID, OwnerID: request.OwnerID, Kind: request.Kind, Target: request.Target, PackageName: request.PackageName, Status: serviceMutationStatusFailed, Attempt: 3}
	for _, scenario := range []string{"overflow", "zero", "owner", "identity", "running", "succeeded", "no-resume", "wrong-request"} {
		t.Run(scenario, func(t *testing.T) {
			j, q := *job, *request
			owner := request.RequestID
			switch scenario {
			case "overflow":
				j.Attempt = int(^uint(0) >> 1)
			case "zero":
				j.Attempt = 0
			case "owner":
				j.OwnerID = testMutationRequestID
			case "identity":
				j.PackageName = "different"
			case "running":
				j.Status = serviceMutationStatusRunning
			case "succeeded":
				j.Status = serviceMutationStatusSucceeded
			case "no-resume":
				q.Resume = false
			case "wrong-request":
				owner = testMutationRequestID
			}
			before := j
			if err := admitMailRenewalFailedRetry(&j, &q, owner); err == nil {
				t.Fatal("invalid explicit retry accepted")
			}
			if j != before {
				t.Fatal("observation mutated evidence")
			}
		})
	}
	job.Status = serviceMutationStatusSucceeded
	request.Resume = false
	if err := admitMailRenewalFailedRetry(job, request, ""); err != nil {
		t.Fatalf("historical success was relabelled failed: %v", err)
	}
}
