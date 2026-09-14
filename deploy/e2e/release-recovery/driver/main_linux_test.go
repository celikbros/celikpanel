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

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestMarkerRejectsAnotherHostOrNonce(t *testing.T) {
	nonce := strings.Repeat("a", 64)
	uuid := "d24e58ea-3de4-49aa-a151-2beb4c32a29e"
	marker := labMarker{Schema: markerSchema, Nonce: nonce, VMUUID: uuid, CellID: "release-recovery-0123456789abcdef", Node: "debian13"}
	raw, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseMarker(raw, nonce, strings.ToUpper(uuid)+"\n", "QEMU\n"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, nonce, uuid, vendor string
		raw                       []byte
	}{
		{"nonce", strings.Repeat("b", 64), uuid, "QEMU", raw},
		{"uuid", nonce, "00000000-0000-0000-0000-000000000000", "QEMU", raw},
		{"hardware", nonce, uuid, "Dell Inc.", raw},
		{"trailing-json", nonce, uuid, "QEMU", append(append([]byte{}, raw...), []byte(" {}")...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseMarker(test.raw, test.nonce, test.uuid, test.vendor); err == nil {
				t.Fatal("unbound guest accepted")
			}
		})
	}
}

func TestMarkerProtectedReadRejectsSymlinkAndWritableFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root metadata test")
	}
	path := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(path, []byte("{}"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path, 4096, true); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(link, 4096, true); err == nil {
		t.Fatal("symlink marker accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtected(path, 4096, true); err == nil {
		t.Fatal("writable marker accepted")
	}
}

func testIdentity() transport.ServiceMutationBeginRequest {
	return transport.ServiceMutationBeginRequest{RequestID: strings.Repeat("1", 32), OwnerID: strings.Repeat("2", 32), Kind: "dns_zone_sync", Target: "fixture.test", PackageName: "dns-zone-sync/v3:sha256:" + strings.Repeat("a", 64)}
}

func liveJob(begin transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	now := time.Now()
	return &transport.ServiceMutationJob{RequestID: begin.RequestID, OwnerID: begin.OwnerID, Kind: begin.Kind, Target: begin.Target, PackageName: begin.PackageName, Status: "running", Phase: "leased", Attempt: 1, StartedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(15 * time.Minute)}
}

func succeededJob(begin transport.ServiceMutationBeginRequest, phase string) *transport.ServiceMutationJob {
	job := liveJob(begin)
	job.Status, job.Phase = "succeeded", phase
	job.FinishedAt, job.LeaseExpiresAt = time.Now(), time.Time{}
	return job
}

func TestOperationUnknownReplyNeverFinishesOrRetriesMutation(t *testing.T) {
	begin := testIdentity()
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unconfirmed", true: "committed-proof"}[committed], func(t *testing.T) {
			calls := []string{}
			call := func(_ context.Context, method string, request, output any) error {
				calls = append(calls, method)
				response := output.(*transport.ServiceMutationResponse)
				switch method {
				case "Agent.BeginServiceMutation":
					response.Job = liveJob(begin)
				case "Agent.ServiceMutationStatus":
					if committed {
						response.Job = succeededJob(begin, "expected-proof")
					} else {
						response.Job = liveJob(begin)
					}
				default:
					t.Fatalf("unsafe control call after unknown result: %s", method)
				}
				return nil
			}
			invocations := 0
			job, err := operate(context.Background(), call, begin, "expected-proof", time.Hour, func(_ context.Context, binding transport.ServiceMutationBinding) (bool, error) {
				invocations++
				if binding.MutationRequestID != begin.RequestID || binding.MutationOwnerID != begin.OwnerID {
					t.Fatal("wrong mutation binding")
				}
				return false, errors.New("lost RPC response")
			})
			if invocations != 1 || len(calls) != 2 {
				t.Fatalf("unexpected execution/control sequence: %d %v", invocations, calls)
			}
			if committed && (err != nil || job == nil) {
				t.Fatalf("exact committed proof did not reconcile: %v", err)
			}
			if !committed && err == nil {
				t.Fatal("unknown mutation reported successful")
			}
		})
	}
}

func TestOperationSuccessRequiresIndependentExactTerminalRead(t *testing.T) {
	begin := testIdentity()
	for _, wrongStatus := range []bool{false, true} {
		call := func(_ context.Context, method string, request, output any) error {
			response := output.(*transport.ServiceMutationResponse)
			switch method {
			case "Agent.BeginServiceMutation":
				response.Job = liveJob(begin)
			case "Agent.FinishServiceMutation":
				finish := request.(*transport.ServiceMutationFinishRequest)
				if !finish.Success || finish.RequestID != begin.RequestID || finish.OwnerID != begin.OwnerID {
					t.Fatal("wrong finish identity")
				}
				response.Job = succeededJob(begin, "expected-proof")
			case "Agent.ServiceMutationStatus":
				response.Job = succeededJob(begin, "expected-proof")
				if wrongStatus {
					response.Job.OwnerID = strings.Repeat("3", 32)
				}
			default:
				t.Fatalf("unexpected RPC %s", method)
			}
			return nil
		}
		_, err := operate(context.Background(), call, begin, "expected-proof", time.Hour, func(context.Context, transport.ServiceMutationBinding) (bool, error) { return true, nil })
		if wrongStatus == (err == nil) {
			t.Fatalf("wrongStatus=%v err=%v", wrongStatus, err)
		}
	}
}

func TestOperationRejectsWrongBeginWithoutMutation(t *testing.T) {
	begin := testIdentity()
	call := func(_ context.Context, _ string, _, output any) error {
		response := output.(*transport.ServiceMutationResponse)
		response.Job = liveJob(begin)
		response.Job.PackageName = "different-commitment"
		return nil
	}
	_, err := operate(context.Background(), call, begin, "expected-proof", time.Hour, func(context.Context, transport.ServiceMutationBinding) (bool, error) {
		t.Fatal("wrong begin proof authorized a mutation")
		return true, nil
	})
	if err == nil {
		t.Fatal("wrong lease accepted")
	}
}

func TestHeartbeatAllowsOnlyExactRegisteredPackageWorker(t *testing.T) {
	begin := testIdentity()
	job := liveJob(begin)
	job.Phase, job.WorkerPID, job.WorkerStarted, job.WorkerCommand = "package/install", 123, "456789", "apt-get"
	if err := runningJob(job, begin, false); err != nil {
		t.Fatal(err)
	}
	if err := runningJob(job, begin, true); err == nil {
		t.Fatal("package worker accepted as fresh lease")
	}
	job.WorkerStarted = ""
	if err := runningJob(job, begin, false); err == nil {
		t.Fatal("partial worker identity accepted")
	}
}
