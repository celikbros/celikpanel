package transport

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
	"time"
)

func TestServiceMutationResponseGobRoundTripPreservesEveryJobField(t *testing.T) {
	now := time.Date(2026, time.August, 2, 9, 10, 11, 12, time.UTC)
	want := ServiceMutationResponse{Job: &ServiceMutationJob{
		RequestID:      "request-1",
		OwnerID:        "owner-1",
		Kind:           "service_install",
		Target:         "nginx",
		PackageName:    "nginx=1.2.3",
		Status:         "running",
		Phase:          "installing",
		Attempt:        2,
		StartedAt:      now,
		UpdatedAt:      now.Add(time.Second),
		LeaseExpiresAt: now.Add(20 * time.Second),
		DeadlineAt:     now.Add(45 * time.Minute),
		FinishedAt:     now.Add(time.Minute),
		ErrorCode:      "package_failed",
		ErrorMessage:   "apt exited 100",
		WorkerPID:      1234,
		WorkerStarted:  "987654",
		WorkerCommand:  "/usr/bin/apt-get install nginx",
	}, ErrorCode: HostMutationBusy, Error: "operation still running"}

	var wire bytes.Buffer
	if err := gob.NewEncoder(&wire).Encode(want); err != nil {
		t.Fatalf("encode ServiceMutationResponse: %v", err)
	}
	var got ServiceMutationResponse
	if err := gob.NewDecoder(&wire).Decode(&got); err != nil {
		t.Fatalf("decode ServiceMutationResponse: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip dropped or changed fields:\n got: %#v\nwant: %#v", got, want)
	}
}
