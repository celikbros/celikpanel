package recoveryobs

import (
	"encoding/json"
	"strings"
	"testing"
)

func testRecord() Record {
	return Record{RequestID: strings.Repeat("a", 32), TargetCommit: strings.Repeat("b", 40), Phase: "running", TerminalProof: "none", Reason: "update_running", ObservedAt: "2026-09-14T10:00:00Z", PreviousFailure: "none"}
}

func TestRecordStrictSchemaAndRedactedStatus(t *testing.T) {
	r := testRecord()
	raw, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw, r.RequestID)
	if err != nil || decoded != r {
		t.Fatalf("roundtrip: %#v %v", decoded, err)
	}
	status, err := json.Marshal(decoded.Status())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(status), r.TargetCommit) || strings.Contains(string(status), "target_commit") {
		t.Fatal("internal binding leaked through HTTP status")
	}
	for name, value := range map[string]string{
		"extra": string(raw) + "secret=value\n", "missing": strings.TrimSuffix(string(raw), "\n"),
		"duplicate":           strings.Replace(string(raw), "target_commit=", "request_id=", 1),
		"unknown schema":      strings.Replace(string(raw), RecordSchema, "other/v1", 1),
		"mismatched identity": strings.Replace(string(raw), r.RequestID, strings.Repeat("c", 32), 1),
		"raw diagnostic":      strings.Replace(string(raw), "reason=update_running", "reason=secret-token", 1),
		"false terminal":      strings.Replace(string(raw), "terminal_proof=none", "terminal_proof=update_verified", 1),
		"invalid date":        strings.Replace(string(raw), "2026-09-14", "2026-02-31", 1),
		"noncanonical time":   strings.Replace(string(raw), "10:00:00Z", "10:00:00+00:00", 1),
		"carriage return":     strings.ReplaceAll(string(raw), "\n", "\r\n"),
		"oversize":            strings.Repeat("a", MaxRecordSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(value), r.RequestID); err == nil {
				t.Fatal("invalid record accepted")
			}
		})
	}
}

func TestMergePreservesVerifiedFailureAndTerminalProof(t *testing.T) {
	r := testRecord()
	failed := r
	failed.Phase, failed.Reason = "failed", "update_failed"
	failed, err := merge(&r, failed)
	if err != nil || failed.PreviousFailure != "update_failed" {
		t.Fatalf("failure lost: %#v %v", failed, err)
	}
	recovering := r
	recovering.Phase, recovering.Reason = "recovering", "recovery_running"
	recovering, err = merge(&failed, recovering)
	if err != nil || recovering.PreviousFailure != "update_failed" {
		t.Fatalf("reconciliation hid failure: %#v %v", recovering, err)
	}
	recovered := r
	recovered.Phase, recovered.Reason, recovered.TerminalProof = "recovered", "rollback_verified", "rollback_verified"
	recovered, err = merge(&recovering, recovered)
	if err != nil {
		t.Fatal(err)
	}
	late := failed
	late.ObservedAt = "2026-09-14T11:00:00Z"
	got, err := merge(&recovered, late)
	if err != nil || got != recovered {
		t.Fatalf("late worker overwrote proof: %#v %v", got, err)
	}
	late.TargetCommit = strings.Repeat("c", 40)
	if _, err := merge(&recovered, late); err == nil {
		t.Fatal("different release reused request identity")
	}
	late = r
	late.ObservedAt = "2026-09-13T10:00:00Z"
	if _, err := merge(&r, late); err == nil {
		t.Fatal("clock reversal replaced newer observation")
	}
}
