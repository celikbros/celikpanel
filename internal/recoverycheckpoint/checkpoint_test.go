package recoverycheckpoint

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func validRecord() Record {
	return Record{Schema: Schema, Snapshot: "20260914T120000Z-from-unknown-to-" + strings.Repeat("a", 40) + "-" + strings.Repeat("b", 32), TokenHash: strings.Repeat("c", 64), Operation: "rollback", Phase: "active", Checkpoint: "payload_restored", Sequence: 2, ObservedAt: "2026-09-14T12:00:00Z", Unit: Unit, InvocationID: strings.Repeat("d", 32), MainPID: 44, MainStartTicks: "55", BootID: "67139a2c-7b23-4387-95ad-45f9e2b831ea", RuntimeManifest: strings.Repeat("e", 64)}
}
func TestCanonicalRecordAndUnexpectedFields(t *testing.T) {
	r := validRecord()
	raw, err := encode(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decode(raw)
	if err != nil || got != r {
		t.Fatal(got, err)
	}
	for _, bad := range [][]byte{append([]byte(" "), raw...), bytes.Replace(raw, []byte(`"schema":`), []byte(`"extra":1,"schema":`), 1), bytes.Replace(raw, []byte(`"sequence":2`), []byte(`"sequence":1,"sequence":2`), 1)} {
		if _, err := decode(bad); err == nil {
			t.Fatal("ambiguous record accepted")
		}
	}
}
func TestMonotonicSequenceAndFreshRestart(t *testing.T) {
	old := validRecord()
	now := old
	now.Checkpoint = "units_reloaded"
	now.ObservedAt = time.Date(2026, 9, 14, 12, 0, 1, 0, time.UTC).Format(time.RFC3339Nano)
	next, err := next(&old, now)
	if err != nil || next.Sequence != 3 {
		t.Fatal(next, err)
	}
}
func TestNoSameInvocationRegressionButRestartCanReobserveEarlierCheckpoint(t *testing.T) {
	old := validRecord()
	now := old
	now.Checkpoint = "restore_admitted"
	if _, err := next(&old, now); err == nil {
		t.Fatal("same invocation regressed")
	}
	now.InvocationID = strings.Repeat("f", 32)
	now.MainPID = 60
	now.MainStartTicks = "70"
	got, err := next(&old, now)
	if err != nil || got.Sequence != 3 {
		t.Fatal(got, err)
	}
	for _, change := range []func(*Record){func(r *Record) { r.Snapshot = "other" }, func(r *Record) { r.RuntimeManifest = strings.Repeat("0", 64) }, func(r *Record) { r.TokenHash = strings.Repeat("0", 64) }} {
		bad := now
		change(&bad)
		if _, err := next(&old, bad); err == nil {
			t.Fatal("identity changed")
		}
	}
}
func TestTimestampDoesNotRegressWithFractionalOrdering(t *testing.T) {
	old := validRecord()
	old.ObservedAt = "2026-09-14T12:00:00.9Z"
	now := old
	now.ObservedAt = "2026-09-14T12:00:00Z"
	if _, err := next(&old, now); err == nil {
		t.Fatal("fractional timestamp regression accepted")
	}
}
func TestMarkerDoesNotExposeRawToken(t *testing.T) {
	token := strings.Repeat("9", 64)
	raw := []byte("version=1\ntoken=" + token + "\noperation=rollback\nsnapshot=" + validRecord().Snapshot + "\n")
	got, err := parseMarker(raw, "active")
	if err != nil || got.TokenHash != digest([]byte(token)) {
		t.Fatal(got, err)
	}
	for _, bad := range [][]byte{append(raw, []byte("extra=1\n")...), bytes.Replace(raw, []byte("version=1"), []byte("version=2"), 1)} {
		if _, err := parseMarker(bad, "active"); err == nil {
			t.Fatal("invalid marker accepted")
		}
	}
}
