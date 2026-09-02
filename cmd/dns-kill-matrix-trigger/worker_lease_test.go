package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func workerLeaseIdentity() transport.ServiceMutationBeginRequest {
	return transport.ServiceMutationBeginRequest{
		RequestID: testRequestID, OwnerID: testOwnerID,
		Kind: mutationKind, Target: "bind", PackageName: "qualifier",
	}
}

func workerBackedRunningJob(identity transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	job := runningJob(identity)
	job.WorkerPID = 4242
	job.WorkerStarted = "boot-7:1234567"
	job.WorkerCommand = "apt-get"
	return job
}

// The S-6 exit-75 shape. A package-installing switch legitimately carries a
// registered apt-get worker while it runs; the heartbeat that observes it must
// not call that "not the exact in-process DNS lease".
// S-6'nın çıkış-75 biçimi. Paket kuran bir geçiş koşarken meşru olarak kayıtlı
// bir apt-get işçisi taşır; onu gözleyen kalp atışı buna "tam süreç-içi DNS
// kirası değil" dememelidir.
func TestHeartbeatAcceptsTheOwningJobsRegisteredWorker(t *testing.T) {
	identity := workerLeaseIdentity()
	job := workerBackedRunningJob(identity)

	if err := validateRunningJobDuringSwitch(job, identity); err != nil {
		t.Fatalf("a registered package worker must be accepted mid-switch: %v", err)
	}
	// Begin stays strict: a job that already carries a worker the moment it is
	// created belongs to somebody else.
	// Begin katı kalır: yaratıldığı anda işçi taşıyan bir iş başkasınındır.
	if err := validateRunningJob(job, identity, true); err == nil {
		t.Fatal("begin must still refuse a worker-bearing job")
	}
}

// The relaxation is exactly the canonical worker and nothing else.
// Gevşetme tam olarak kanonik işçidir, başka hiçbir şey değil.
func TestHeartbeatStillRefusesEveryNoncanonicalWorker(t *testing.T) {
	identity := workerLeaseIdentity()
	for name, mutate := range map[string]func(*transport.ServiceMutationJob){
		"pid without token": func(j *transport.ServiceMutationJob) { j.WorkerStarted = "" },
		"padded token":      func(j *transport.ServiceMutationJob) { j.WorkerStarted = " boot-7:1234567" },
		"pid without cmd":   func(j *transport.ServiceMutationJob) { j.WorkerCommand = "" },
		"padded command":    func(j *transport.ServiceMutationJob) { j.WorkerCommand = " apt-get" },
		"path-bearing":      func(j *transport.ServiceMutationJob) { j.WorkerCommand = "/usr/bin/apt-get" },
		"oversized command": func(j *transport.ServiceMutationJob) { j.WorkerCommand = strings.Repeat("x", 65) },
		"wrong status":      func(j *transport.ServiceMutationJob) { j.Status = mutationFailed },
		"foreign request":   func(j *transport.ServiceMutationJob) { j.RequestID = strings.Repeat("f", 32) },
		"finished":          func(j *transport.ServiceMutationJob) { j.FinishedAt = j.UpdatedAt },
		"zero lease":        func(j *transport.ServiceMutationJob) { j.LeaseExpiresAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			job := workerBackedRunningJob(identity)
			mutate(job)
			if err := validateRunningJobDuringSwitch(job, identity); err == nil {
				t.Fatalf("%s must be refused", name)
			}
		})
	}
	if err := validateRunningJobDuringSwitch(nil, identity); err == nil {
		t.Fatal("a nil job must be refused")
	}
}

// End to end: the agent reports a registered worker on the heartbeat while the
// switch runs, then completes it. Before the fix this returned
// errSwitchOutcomeUncertain (exit 75) although the switch had succeeded — the
// exact result S-6 recorded four times.
// Uçtan uca: geçiş koşarken agent kalp atışında kayıtlı bir işçi bildirir,
// sonra geçişi tamamlar. Düzeltmeden önce bu, geçiş başarılı olduğu hâlde
// errSwitchOutcomeUncertain (çıkış 75) döndürüyordu — S-6'nın dört kez
// kaydettiği sonucun ta kendisi.
func TestRunRPCSwitchSurvivesAWorkerBackedHeartbeat(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	))
	heartbeatSeen := make(chan struct{})
	var heartbeatOnce sync.Once
	var mu sync.Mutex
	finishCalls := 0
	call := func(_ context.Context, method string, input, output any) error {
		switch method {
		case "Agent.BeginServiceMutation":
			begin := input.(*transport.ServiceMutationBeginRequest)
			output.(*transport.ServiceMutationResponse).Job = runningJob(*begin)
			return nil
		case "Agent.HeartbeatServiceMutation":
			heartbeat := input.(*transport.ServiceMutationHeartbeatRequest)
			identity := transport.ServiceMutationBeginRequest{
				RequestID: heartbeat.RequestID, OwnerID: heartbeat.OwnerID,
				Kind: mutationKind, Target: string(request.TargetEngine),
				PackageName: request.ManifestQualifier,
			}
			// The package install is in flight: apt-get is the job's worker.
			// Paket kurulumu sürüyor: apt-get işin işçisi.
			output.(*transport.ServiceMutationResponse).Job = workerBackedRunningJob(identity)
			heartbeatOnce.Do(func() { close(heartbeatSeen) })
			return nil
		case "Agent.SwitchDNSEngineV1":
			<-heartbeatSeen
			*output.(*transport.SwitchDNSEngineV1Response) = transport.SwitchDNSEngineV1Response{
				Applied: true, ActiveEngine: request.TargetEngine,
				ActiveEpoch: request.TargetEpoch, AppliedZones: len(request.Zones),
			}
			return nil
		case "Agent.FinishServiceMutation":
			mu.Lock()
			finishCalls++
			mu.Unlock()
			finish := input.(*transport.ServiceMutationFinishRequest)
			if !finish.Success {
				return errors.New("successful switch was finished as failure")
			}
			identity := transport.ServiceMutationBeginRequest{
				RequestID: testRequestID, OwnerID: testOwnerID,
				Kind: mutationKind, Target: string(request.TargetEngine),
				PackageName: request.ManifestQualifier,
			}
			output.(*transport.ServiceMutationResponse).Job = succeededJob(identity)
			return nil
		default:
			return errors.New("unexpected RPC method")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runRPCSwitch(
		ctx, testRequestID, testOwnerID, request, false, time.Millisecond, call,
	)
	if err != nil {
		t.Fatalf("a switch with a registered worker must complete, got %v", err)
	}
	if errors.Is(err, errSwitchOutcomeUncertain) {
		t.Fatal("the outcome must not be reported as uncertain")
	}
	if result.heartbeatCount < 1 {
		t.Fatal("the worker-bearing heartbeat was never observed")
	}
	if result.terminal == nil || result.terminal.Status != mutationSucceeded {
		t.Fatalf("terminal=%+v, want succeeded", result.terminal)
	}
	mu.Lock()
	defer mu.Unlock()
	if finishCalls != 1 {
		t.Fatalf("finish was called %d times, want exactly once", finishCalls)
	}
}
