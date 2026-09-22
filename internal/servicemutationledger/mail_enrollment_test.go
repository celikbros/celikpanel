package servicemutationledger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func enrollmentFixture(t *testing.T) (Ledger, MailEnrollmentIdentity, time.Time) {
	t.Helper()
	_, ledger := fixture(t, "alpha81-empty")
	id := MailEnrollmentIdentity{strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 64)}
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	admitted, err := AdmitMailEnrollment(&ledger, id, now)
	if err != nil {
		t.Fatal(err)
	}
	return admitted, id, now
}
func TestMailEnrollmentMonotonicReservation(t *testing.T) {
	for _, inverse := range []bool{false, true} {
		ledger, id, now := enrollmentFixture(t)
		before, _ := Encode(&ledger)
		state, err := MailEnrollmentState(&ledger, id)
		if err != nil || state != MailEnrollmentForward || ledger.ActiveRequestID != id.RequestID {
			t.Fatal(state, err)
		}
		if _, err := AdmitMailEnrollment(&ledger, id, now.Add(time.Hour)); err == nil {
			t.Fatal("re-admitted reserved identity")
		}
		if inverse {
			ledger, err = AdvanceMailEnrollment(&ledger, id, MailEnrollmentRollback, now.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := AdvanceMailEnrollment(&ledger, id, MailEnrollmentPublished, now.Add(2*time.Hour)); err == nil {
				t.Fatal("reversed inverse intent")
			}
		}
		next := MailEnrollmentPublished
		if inverse {
			next = MailEnrollmentRestored
		}
		result, err := AdvanceMailEnrollment(&ledger, id, next, now.Add(48*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if result.ActiveRequestID != "" {
			t.Fatal("verified result retained reservation")
		}
		raw, err := Encode(&result)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := MailEnrollmentState(&decoded, id); err != nil || got != next {
			t.Fatal(got, err)
		}
		for _, transition := range []string{MailEnrollmentForward, MailEnrollmentRollback, MailEnrollmentPublished, MailEnrollmentRestored} {
			if _, err := AdvanceMailEnrollment(&decoded, id, transition, now.Add(72*time.Hour)); err == nil {
				t.Fatal("reopened terminal", transition)
			}
		}
		if _, err := AdmitMailEnrollment(&decoded, id, now.Add(72*time.Hour)); err == nil {
			t.Fatal("reused terminal request")
		}
		if !inverse {
			unchanged, _ := Encode(&ledger)
			if !bytes.Equal(before, unchanged) {
				t.Fatal("candidate mutated source ledger")
			}
		}
	}
}
func TestMailEnrollmentRejectsIdentityStatusAndForgedRelease(t *testing.T) {
	cases := map[string]func(*Ledger, *ServiceMutationJob){
		"kind":   func(l *Ledger, j *ServiceMutationJob) { j.Kind = "service_install" },
		"target": func(l *Ledger, j *ServiceMutationJob) { j.Target = "nginx" },
		"scope": func(l *Ledger, j *ServiceMutationJob) {
			j.PackageName = MailEnrollmentQualifierPrefix + strings.Repeat("d", 64)
		},
		"phase request": func(l *Ledger, j *ServiceMutationJob) {
			j.Phase = strings.Replace(j.Phase, j.RequestID, strings.Repeat("e", 32), 1)
		},
		"running RPC": func(l *Ledger, j *ServiceMutationJob) { j.Status = StatusRunning },
		"cancel RPC":  func(l *Ledger, j *ServiceMutationJob) { j.Status = StatusCancelling },
		"failed dead worker": func(l *Ledger, j *ServiceMutationJob) {
			j.Status = StatusFailed
			j.Phase = "interrupted"
			j.LeaseExpiresAt = time.Time{}
			j.FinishedAt = j.UpdatedAt
			l.ActiveRequestID = ""
		},
		"forged success": func(l *Ledger, j *ServiceMutationJob) {
			j.Status = StatusSucceeded
			j.LeaseExpiresAt = time.Time{}
			j.FinishedAt = j.UpdatedAt
			l.ActiveRequestID = ""
		},
		"pointer": func(l *Ledger, j *ServiceMutationJob) { l.ActiveRequestID = "" },
		"worker": func(l *Ledger, j *ServiceMutationJob) {
			j.WorkerPID = 17
			j.WorkerStarted = "12"
			j.WorkerCommand = "worker"
		},
		"new attempt": func(l *Ledger, j *ServiceMutationJob) { j.Attempt++ },
		"pending":     func(l *Ledger, j *ServiceMutationJob) { j.Status = StatusPending },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			ledger, id, _ := enrollmentFixture(t)
			edit(&ledger, ledger.Jobs[id.RequestID])
			raw, _ := json.Marshal(&ledger)
			if _, err := Decode(raw); err == nil {
				t.Fatal("malformed enrollment decoded")
			}
			if _, err := Encode(&ledger); err == nil {
				t.Fatal("malformed enrollment encoded")
			}
		})
	}
	ledger, id, now := enrollmentFixture(t)
	for _, field := range []string{"request", "owner", "scope"} {
		wrong := id
		switch field {
		case "request":
			wrong.RequestID = strings.Repeat("d", 32)
		case "owner":
			wrong.OwnerID = strings.Repeat("d", 32)
		case "scope":
			wrong.ScopeSHA256 = strings.Repeat("d", 64)
		}
		if _, err := MailEnrollmentState(&ledger, wrong); err == nil {
			t.Fatal(field)
		}
		if _, err := AdvanceMailEnrollment(&ledger, wrong, MailEnrollmentPublished, now); err == nil {
			t.Fatal(field)
		}
	}
	if _, err := AdvanceMailEnrollment(&ledger, id, MailEnrollmentRestored, now); err == nil {
		t.Fatal("inverse without intent")
	}
	if _, err := AdvanceMailEnrollment(&ledger, id, MailEnrollmentRollback, now.Add(-time.Hour)); err == nil {
		t.Fatal("backdated transition")
	}
	if _, err := AdvanceMailEnrollment(nil, id, MailEnrollmentPublished, now); err == nil {
		t.Fatal("nil ledger")
	}
}

func TestRecordedMailEnrollmentSelectsOnlyExactRequest(t *testing.T) {
	ledger, id, now := enrollmentFixture(t)
	before, _ := Encode(&ledger)
	for _, request := range []string{"", strings.Repeat("f", 32), "../active"} {
		if _, _, err := RecordedMailEnrollment(&ledger, request); err == nil {
			t.Fatal("unrecorded request accepted")
		}
	}
	got, state, err := RecordedMailEnrollment(&ledger, id.RequestID)
	if err != nil || got != id || state != "forward" {
		t.Fatal(got, state, err)
	}
	after, _ := Encode(&ledger)
	if !bytes.Equal(before, after) {
		t.Fatal("selector modified ledger")
	}
	ledger, err = AdvanceMailEnrollment(&ledger, id, MailEnrollmentPublished, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, state, err = RecordedMailEnrollment(&ledger, id.RequestID); err != nil || state != "published" {
		t.Fatal(state, err)
	}
	ledger.ActiveRequestID = id.RequestID
	if _, _, err = RecordedMailEnrollment(&ledger, id.RequestID); err == nil {
		t.Fatal("inconsistent whole ledger accepted")
	}
}
