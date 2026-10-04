package servicemutationledger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) ([]byte, Ledger) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw, ledger
}
func TestHistoricalProducerBytes(t *testing.T) {
	paths, err := filepath.Glob("testdata/alpha81-*.json")
	if err != nil || len(paths) != 11 {
		t.Fatalf("fixtures: %d %v", len(paths), err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(raw)
			ledger, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := Encode(&ledger)
			if err != nil || !bytes.Equal(raw, encoded) {
				t.Fatalf("producer bytes differ: %v", err)
			}
			if !bytes.Equal(raw, original) {
				t.Fatal("reader changed input")
			}
		})
	}
}
func TestRejectUnknownAmbiguousOrOversizeEnvelope(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"version":2,"jobs":{}}`, `{"version":1,"jobs":null}`, `{"version":1,"jobs":{},"unknown":true}`, `{"version":1,"version":1,"jobs":{}}`, `{"jobs":{},"version":1}`, `{"version":1,"jobs":{}} {}`, "{\"version\":1,\"jobs\":{}}\n", strings.Repeat(" ", MaxSize+1)} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatal("unsafe envelope accepted")
		}
	}
	for _, ledger := range []*Ledger{nil, {}, {Version: Version}, {Version: Version + 1, Jobs: map[string]*ServiceMutationJob{}}} {
		if _, err := Encode(ledger); err == nil {
			t.Fatal("invalid writer envelope accepted")
		}
	}
	_, ledger := fixture(t, "alpha81-failed")
	for _, job := range ledger.Jobs {
		job.ErrorMessage = strings.Repeat("x", MaxSize)
	}
	if _, err := Encode(&ledger); err == nil {
		t.Fatal("writer exceeds reader bound")
	}
}
func TestCrossFieldEvidenceCannotBecomeIdle(t *testing.T) {
	for name, edit := range map[string]func(*Ledger, *ServiceMutationJob){
		"lost pointer":    func(l *Ledger, j *ServiceMutationJob) { l.ActiveRequestID = "" },
		"wrong pointer":   func(l *Ledger, j *ServiceMutationJob) { l.ActiveRequestID = strings.Repeat("a", 32) },
		"wrong request":   func(l *Ledger, j *ServiceMutationJob) { j.RequestID = strings.Repeat("a", 32) },
		"missing owner":   func(l *Ledger, j *ServiceMutationJob) { j.OwnerID = "" },
		"worker partial":  func(l *Ledger, j *ServiceMutationJob) { j.WorkerPID = 321 },
		"no lease":        func(l *Ledger, j *ServiceMutationJob) { j.LeaseExpiresAt = time.Time{} },
		"finished active": func(l *Ledger, j *ServiceMutationJob) { j.FinishedAt = j.UpdatedAt },
		"unknown state":   func(l *Ledger, j *ServiceMutationJob) { j.Status = "unknown" },
		"two active": func(l *Ledger, j *ServiceMutationJob) {
			other := *j
			other.RequestID = strings.Repeat("a", 32)
			l.Jobs[other.RequestID] = &other
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, l := fixture(t, "alpha81-running")
			j := l.Jobs[l.ActiveRequestID]
			edit(&l, j)
			raw, err := json.Marshal(l)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Decode(raw); err == nil {
				t.Fatal("invalid persisted evidence accepted")
			}
			if _, err = Encode(&l); err == nil {
				t.Fatal("writer accepted invalid evidence")
			}
		})
	}
	// Expiry does not erase active/orphaned ownership or prove host idleness.
	_, l := fixture(t, "alpha81-running")
	j := l.Jobs[l.ActiveRequestID]
	j.Status = StatusOrphaned
	if err := Validate(&l); err != nil || !StatusActive(j.Status) {
		t.Fatalf("orphan lost: %v", err)
	}
}
func TestPublishedSuccessRequiresExactOperationReceipt(t *testing.T) {
	paths, _ := filepath.Glob("testdata/alpha81-published-*.json")
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for name, edit := range map[string]func(*ServiceMutationJob){
				"different request": func(j *ServiceMutationJob) {
					j.Phase = strings.Replace(j.Phase, j.RequestID, strings.Repeat("b", 32), 1)
				},
				"different payload": func(j *ServiceMutationJob) {
					j.PackageName = strings.ReplaceAll(j.PackageName, strings.Repeat("a", 64), strings.Repeat("b", 64))
				},
				"unproved success": func(j *ServiceMutationJob) { j.Phase = "completed" },
				"worker remains":   func(j *ServiceMutationJob) { j.WorkerPID = 12; j.WorkerStarted = "34"; j.WorkerCommand = "fixture" },
			} {
				t.Run(name, func(t *testing.T) {
					l, err := Decode(raw)
					if err != nil {
						t.Fatal(err)
					}
					for _, j := range l.Jobs {
						edit(j)
					}
					if _, err = Encode(&l); err == nil {
						t.Fatal("forged terminal proof accepted")
					}
				})
			}
		})
	}
}
