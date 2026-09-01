package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	testRequestID = "00112233445566778899aabbccddeeff"
	testOwnerID   = "ffeeddccbbaa99887766554433221100"
)

func TestRequestForScenarioDriverRoutes(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		scenario scenario
		target   transport.DNSEngine
		mode     string
	}{
		{
			name: "bind", driver: "bind", target: transport.DNSEngineBIND,
			mode: transport.DNSEngineSwitchModeSwitch,
			scenario: standaloneScenario(
				"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
			),
		},
		{
			name: "bind-uninitialized", driver: "bind", target: transport.DNSEngineBIND,
			mode: transport.DNSEngineSwitchModeSwitch,
			scenario: scenario{
				Schema: scenarioSchema, Driver: "bind", SourceFixture: "uninitialized",
				Mode:         transport.DNSEngineSwitchModeSwitch,
				TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1,
				Topology: transport.DNSTopologyStandalone,
				Zones:    []transport.DNSEngineSwitchZoneSnapshot{},
			},
		},
		{
			name: "pdns-switch", driver: "pdns-switch", target: transport.DNSEnginePowerDNS,
			mode: transport.DNSEngineSwitchModeSwitch,
			scenario: standaloneScenario(
				"pdns-switch", transport.DNSEngineBIND, transport.DNSEnginePowerDNS, 1,
			),
		},
		{
			name: "pdns-adopt", driver: "pdns-adopt", target: transport.DNSEnginePowerDNS,
			mode: transport.DNSEngineSwitchModeAdopt,
			scenario: standaloneScenario(
				"pdns-adopt", "", transport.DNSEnginePowerDNS, 0,
			),
		},
		{
			name: "pdns-secondary-reconfigure", driver: "pdns-secondary-reconfigure",
			target: transport.DNSEnginePowerDNS, mode: transport.DNSEngineSwitchModeSwitch,
			scenario: scenario{
				Schema: scenarioSchema, Driver: "pdns-secondary-reconfigure",
				SourceFixture: "legacy-pdns-secondary",
				Mode:          transport.DNSEngineSwitchModeSwitch,
				TargetEngine:  transport.DNSEnginePowerDNS, TargetEpoch: 1,
				Topology: transport.DNSTopologyPaired,
				PairRole: transport.DNSPairRoleSecondary,
				LocalIP:  "192.0.2.10", LocalNS: "ns1.example.test",
				PeerIP: "192.0.2.11", PeerNS: "ns2.example.test",
				Zones: []transport.DNSEngineSwitchZoneSnapshot{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := requestForScenario(test.scenario, test.driver)
			if err != nil {
				t.Fatalf("requestForScenario: %v", err)
			}
			if request.TargetEngine != test.target || request.Mode != test.mode {
				t.Fatalf("request route=(%q,%q), want (%q,%q)", request.TargetEngine, request.Mode, test.target, test.mode)
			}
			if request.ManifestQualifier == "" {
				t.Fatal("canonical request lacks a manifest qualifier")
			}
			if request.MutationRequestID != "" || request.MutationOwnerID != "" {
				t.Fatal("scenario was allowed to select the durable mutation identity")
			}
		})
	}
}

func TestRequestForScenarioRejectsDriverConfusion(t *testing.T) {
	secondary := scenario{
		Schema: scenarioSchema, Driver: "pdns-switch",
		SourceFixture: "legacy-pdns-secondary",
		Mode:          transport.DNSEngineSwitchModeSwitch,
		TargetEngine:  transport.DNSEnginePowerDNS, TargetEpoch: 1,
		Topology: transport.DNSTopologyPaired,
		PairRole: transport.DNSPairRoleSecondary,
		LocalIP:  "192.0.2.10", LocalNS: "ns1.example.test",
		PeerIP: "192.0.2.11", PeerNS: "ns2.example.test",
		Zones: []transport.DNSEngineSwitchZoneSnapshot{},
	}
	if _, err := requestForScenario(secondary, "pdns-switch"); err == nil {
		t.Fatal("secondary reconfiguration manifest was accepted as pdns-switch")
	}
	signed := standaloneScenario(
		"signed-update-finalize", transport.DNSEnginePowerDNS,
		transport.DNSEngineBIND, 1,
	)
	if _, err := requestForScenario(signed, signed.Driver); err == nil {
		t.Fatal("signed-update startup recovery was accepted as an RPC switch")
	}
}

func TestLoadScenarioRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.json")
	raw := []byte(`{"schema":"celikpanel-dns-kill-matrix-trigger/v1","driver":"bind","unknown":true}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadScenario(path, "bind"); err == nil {
		t.Fatal("scenario with an unknown field was accepted")
	}
}

func TestRunRPCSwitchHeartbeatsAndLeavesKilledOutcomeForRecovery(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	))
	heartbeatSeen := make(chan struct{})
	var heartbeatOnce sync.Once
	var mu sync.Mutex
	finishCalls := 0
	var switchBinding transport.ServiceMutationBinding
	call := func(_ context.Context, method string, input, output any) error {
		switch method {
		case "Agent.BeginServiceMutation":
			begin := input.(*transport.ServiceMutationBeginRequest)
			response := output.(*transport.ServiceMutationResponse)
			response.Job = runningJob(*begin)
			return nil
		case "Agent.HeartbeatServiceMutation":
			heartbeat := input.(*transport.ServiceMutationHeartbeatRequest)
			response := output.(*transport.ServiceMutationResponse)
			response.Job = runningJob(transport.ServiceMutationBeginRequest{
				RequestID: heartbeat.RequestID, OwnerID: heartbeat.OwnerID,
				Kind: mutationKind, Target: string(request.TargetEngine),
				PackageName: request.ManifestQualifier,
			})
			heartbeatOnce.Do(func() { close(heartbeatSeen) })
			return nil
		case "Agent.SwitchDNSEngineV1":
			switchRequest := input.(*transport.SwitchDNSEngineV1Request)
			switchBinding = switchRequest.ServiceMutationBinding
			<-heartbeatSeen
			return io.EOF
		case "Agent.FinishServiceMutation":
			mu.Lock()
			finishCalls++
			mu.Unlock()
			return nil
		default:
			return errors.New("unexpected RPC method")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := runRPCSwitch(
		ctx, testRequestID, testOwnerID, request, false, time.Millisecond, call,
	)
	if !errors.Is(err, errSwitchOutcomeUncertain) {
		t.Fatalf("runRPCSwitch error=%v, want uncertain outcome", err)
	}
	if result.heartbeatCount < 1 {
		t.Fatal("long-running switch received no heartbeat")
	}
	if switchBinding.MutationRequestID != testRequestID ||
		switchBinding.MutationOwnerID != testOwnerID {
		t.Fatalf("switch binding=%+v", switchBinding)
	}
	mu.Lock()
	defer mu.Unlock()
	if finishCalls != 0 {
		t.Fatalf("ambiguous killed switch was prematurely finished %d times", finishCalls)
	}
}

func TestRunRPCSwitchCompletesWithExactFinalizedReceipt(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"pdns-switch", transport.DNSEngineBIND, transport.DNSEnginePowerDNS, 1,
	))
	methods := make([]string, 0, 3)
	call := func(_ context.Context, method string, input, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.BeginServiceMutation":
			begin := input.(*transport.ServiceMutationBeginRequest)
			output.(*transport.ServiceMutationResponse).Job = runningJob(*begin)
		case "Agent.SwitchDNSEngineV1":
			switchRequest := input.(*transport.SwitchDNSEngineV1Request)
			if switchRequest.MutationRequestID != testRequestID ||
				switchRequest.MutationOwnerID != testOwnerID {
				return errors.New("switch binding differs")
			}
			*output.(*transport.SwitchDNSEngineV1Response) = transport.SwitchDNSEngineV1Response{
				Applied: true, ActiveEngine: request.TargetEngine,
				ActiveEpoch: request.TargetEpoch, AppliedZones: len(request.Zones),
			}
		case "Agent.FinishServiceMutation":
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
		default:
			return errors.New("unexpected RPC method")
		}
		return nil
	}
	result, err := runRPCSwitch(
		context.Background(), testRequestID, testOwnerID, request,
		false, time.Hour, call,
	)
	if err != nil {
		t.Fatalf("runRPCSwitch: %v", err)
	}
	if result.terminal == nil || result.terminal.Status != mutationSucceeded {
		t.Fatalf("terminal=%+v", result.terminal)
	}
	want := []string{
		"Agent.BeginServiceMutation", "Agent.SwitchDNSEngineV1",
		"Agent.FinishServiceMutation",
	}
	if len(methods) != len(want) {
		t.Fatalf("methods=%v, want %v", methods, want)
	}
	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("methods=%v, want %v", methods, want)
		}
	}
}

func TestBuildPDNSNormalizationIdentityBindsExactProductionOperations(t *testing.T) {
	value := standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	)
	value.SourceFixture = "managed-pdns"
	value.Zones = []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: "s1-kill.test", DesiredGeneration: 7, ZoneType: "NATIVE",
		Records: []transport.ZoneRecord{{
			Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300,
		}},
	}}
	request := mustScenarioRequest(t, value)
	receipt, err := buildPDNSNormalizationIdentity(
		"bind-source-stopped-standalone-reachable-before", "bind",
		testRequestID, value, request,
	)
	if err != nil {
		t.Fatalf("buildPDNSNormalizationIdentity: %v", err)
	}
	if receipt.Configure.Method != "Agent.ConfigurePowerDNSSQLite" ||
		receipt.Configure.Kind != mutationKindPDNSConfigure ||
		receipt.Configure.Target != "pdns" || receipt.Configure.PackageName != "" ||
		receipt.Configure.TerminalPhase != "completed" {
		t.Fatalf("configure identity=%+v", receipt.Configure)
	}
	if len(receipt.ZoneSyncs) != 1 {
		t.Fatalf("zone sync identities=%+v", receipt.ZoneSyncs)
	}
	zone := receipt.ZoneSyncs[0]
	sourceCommitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, 7, "s1-kill.test", false, "NATIVE",
		value.Zones[0].Records,
	)
	if err != nil {
		t.Fatal(err)
	}
	if zone.Method != "Agent.SyncDNSZoneV3" || zone.Kind != mutationKindDNSZoneSync ||
		zone.Target != "s1-kill.test" || zone.Domain != "s1-kill.test" ||
		zone.Engine != "pdns" || zone.EngineEpoch != 1 ||
		zone.DesiredGeneration != 7 || zone.Qualifier != sourceCommitment.Qualifier ||
		zone.PackageName != sourceCommitment.Qualifier ||
		zone.Qualifier == request.Zones[0].ZoneQualifier ||
		zone.RequestID == receipt.Configure.RequestID || zone.OwnerID == receipt.Configure.OwnerID {
		t.Fatalf("zone sync identity=%+v", zone)
	}
	encoded, _ := canonicalNormalizationIdentityReceipt(receipt)
	var observed normalizationIdentityReceipt
	if err := json.Unmarshal(encoded, &observed); err != nil {
		t.Fatalf("decode normalization identity: %v", err)
	}
	observedEncoded, _ := canonicalNormalizationIdentityReceipt(observed)
	if string(encoded) != string(observedEncoded) {
		t.Fatalf("normalization receipt changed:\n%s\n%s", encoded, observedEncoded)
	}
}

func TestRunPDNSNormalizationUsesExactConfigureAndV3Bindings(t *testing.T) {
	value := standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	)
	value.SourceFixture = "managed-pdns"
	value.Zones = []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: "s1-kill.test", DesiredGeneration: 7, ZoneType: "NATIVE",
		Records: []transport.ZoneRecord{{
			Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300,
		}},
	}}
	request := mustScenarioRequest(t, value)
	receipt, err := buildPDNSNormalizationIdentity(
		"bind-source-stopped-standalone-reachable-before", "bind",
		testRequestID, value, request,
	)
	if err != nil {
		t.Fatal(err)
	}
	methods := make([]string, 0, 6)
	var active transport.ServiceMutationBeginRequest
	call := func(_ context.Context, method string, input, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.BeginServiceMutation":
			active = *input.(*transport.ServiceMutationBeginRequest)
			output.(*transport.ServiceMutationResponse).Job = runningJob(active)
		case "Agent.ConfigurePowerDNSSQLite":
			binding := input.(*transport.ServiceMutationRequest).ServiceMutationBinding
			if binding.MutationRequestID != receipt.Configure.RequestID ||
				binding.MutationOwnerID != receipt.Configure.OwnerID {
				return errors.New("configure binding differs")
			}
			*output.(*transport.SyncDNSZoneResponse) = transport.SyncDNSZoneResponse{Synced: true}
		case "Agent.SyncDNSZoneV3":
			zone := input.(*transport.SyncDNSZoneV3Request)
			identity := receipt.ZoneSyncs[0]
			if zone.MutationRequestID != identity.RequestID ||
				zone.MutationOwnerID != identity.OwnerID || zone.Engine != transport.DNSEnginePowerDNS ||
				zone.EngineEpoch != 1 || zone.Domain != "s1-kill.test" ||
				zone.DesiredGeneration != 7 || len(zone.Records) != 1 {
				return errors.New("V3 sync binding or snapshot differs")
			}
			*output.(*transport.SyncDNSZoneV3Response) = transport.SyncDNSZoneV3Response{
				Synced: true, Engine: transport.DNSEnginePowerDNS,
				EngineEpoch: 1, AppliedGeneration: 7,
			}
		case "Agent.FinishServiceMutation":
			finish := input.(*transport.ServiceMutationFinishRequest)
			if !finish.Success || finish.RequestID != active.RequestID ||
				finish.OwnerID != active.OwnerID {
				return errors.New("finish identity differs")
			}
			phase := receipt.Configure.TerminalPhase
			if active.RequestID == receipt.ZoneSyncs[0].RequestID {
				phase = receipt.ZoneSyncs[0].TerminalPhase
			}
			output.(*transport.ServiceMutationResponse).Job = succeededJobAtPhase(active, phase)
		default:
			return errors.New("unexpected method " + method)
		}
		return nil
	}
	if _, err := runPDNSNormalization(
		context.Background(), receipt, request, time.Hour, call,
	); err != nil {
		t.Fatalf("runPDNSNormalization: %v", err)
	}
	want := []string{
		"Agent.BeginServiceMutation", "Agent.ConfigurePowerDNSSQLite",
		"Agent.FinishServiceMutation", "Agent.BeginServiceMutation",
		"Agent.SyncDNSZoneV3", "Agent.FinishServiceMutation",
	}
	if len(methods) != len(want) {
		t.Fatalf("methods=%v, want %v", methods, want)
	}
	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("methods=%v, want %v", methods, want)
		}
	}
}

func TestRunRPCSwitchRetryResumesExactFailedJob(t *testing.T) {
	request := mustScenarioRequest(t, scenario{
		Schema: scenarioSchema, Driver: "bind", SourceFixture: "uninitialized",
		Mode:         transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1,
		Topology: transport.DNSTopologyStandalone,
		Zones:    []transport.DNSEngineSwitchZoneSnapshot{},
	})
	identity := transport.ServiceMutationBeginRequest{
		RequestID: testRequestID, OwnerID: testOwnerID,
		Kind: mutationKind, Target: string(request.TargetEngine),
		PackageName: request.ManifestQualifier,
	}
	methods := make([]string, 0, 4)
	call := func(_ context.Context, method string, input, output any) error {
		methods = append(methods, method)
		switch method {
		case "Agent.ServiceMutationStatus":
			output.(*transport.ServiceMutationResponse).Job = failedJob(
				identity,
				mutationInterruptedPhase,
				restartBeforeSwitchCommitCode,
				restartBeforeSwitchCommitMessage,
			)
		case "Agent.BeginServiceMutation":
			begin := input.(*transport.ServiceMutationBeginRequest)
			if !begin.Resume {
				return errors.New("retry did not set Begin.Resume")
			}
			output.(*transport.ServiceMutationResponse).Job = runningJob(*begin)
		case "Agent.SwitchDNSEngineV1":
			*output.(*transport.SwitchDNSEngineV1Response) = transport.SwitchDNSEngineV1Response{
				Applied: true, ActiveEngine: request.TargetEngine,
				ActiveEpoch: request.TargetEpoch, AppliedZones: len(request.Zones),
			}
		case "Agent.FinishServiceMutation":
			output.(*transport.ServiceMutationResponse).Job = succeededJob(identity)
		default:
			return errors.New("unexpected RPC method")
		}
		return nil
	}
	result, err := runRPCSwitch(
		context.Background(), testRequestID, testOwnerID, request,
		true, time.Hour, call,
	)
	if err != nil {
		t.Fatalf("runRPCSwitch retry: %v", err)
	}
	if result.terminal == nil || result.terminal.Status != mutationSucceeded {
		t.Fatalf("terminal=%+v", result.terminal)
	}
	want := []string{
		"Agent.ServiceMutationStatus", "Agent.BeginServiceMutation",
		"Agent.SwitchDNSEngineV1", "Agent.FinishServiceMutation",
	}
	if len(methods) != len(want) {
		t.Fatalf("methods=%v, want %v", methods, want)
	}
	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("methods=%v, want %v", methods, want)
		}
	}
}

func TestValidateRetryableFailedJobAcceptsExactProductionReceipts(t *testing.T) {
	identity := transport.ServiceMutationBeginRequest{
		RequestID: testRequestID, OwnerID: testOwnerID,
		Kind: mutationKind, Target: string(transport.DNSEngineBIND),
		PackageName: "manifest-qualifier",
	}
	tests := []struct {
		name    string
		phase   string
		code    string
		message string
	}{
		{
			name:  "restart-before-switch-commit",
			phase: mutationInterruptedPhase,
			code:  restartBeforeSwitchCommitCode, message: restartBeforeSwitchCommitMessage,
		},
		{
			name:  "verified-rollback-after-restart",
			phase: mutationInterruptedPhase,
			code:  switchRolledBackAfterRestartCode, message: switchRolledBackAfterRestartMessage,
		},
		{
			name:  "explicit-trigger-rejection",
			phase: mutationFailed,
			code:  triggerFailureCode, message: triggerFailureMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := failedJob(identity, test.phase, test.code, test.message)
			if err := validateRetryableFailedJob(job, identity); err != nil {
				t.Fatalf("validateRetryableFailedJob: %v", err)
			}
			finishErr := validateFailedJob(job, identity)
			if test.phase == mutationFailed && finishErr != nil {
				t.Fatalf("validateFailedJob: %v", finishErr)
			}
			if test.phase != mutationFailed && finishErr == nil {
				t.Fatal("post-finish validator accepted a startup-recovery receipt")
			}
		})
	}
}

func TestValidateRetryableFailedJobRejectsInexactReceipts(t *testing.T) {
	identity := transport.ServiceMutationBeginRequest{
		RequestID: testRequestID, OwnerID: testOwnerID,
		Kind: mutationKind, Target: string(transport.DNSEngineBIND),
		PackageName: "manifest-qualifier",
	}
	valid := func() *transport.ServiceMutationJob {
		return failedJob(
			identity,
			mutationInterruptedPhase,
			restartBeforeSwitchCommitCode,
			restartBeforeSwitchCommitMessage,
		)
	}
	tests := []struct {
		name   string
		mutate func(*transport.ServiceMutationJob)
	}{
		{name: "request-id", mutate: func(job *transport.ServiceMutationJob) {
			job.RequestID = testOwnerID
		}},
		{name: "owner-id", mutate: func(job *transport.ServiceMutationJob) {
			job.OwnerID = testRequestID
		}},
		{name: "kind", mutate: func(job *transport.ServiceMutationJob) {
			job.Kind = "dns_zone_sync"
		}},
		{name: "target", mutate: func(job *transport.ServiceMutationJob) {
			job.Target = string(transport.DNSEnginePowerDNS)
		}},
		{name: "package-name", mutate: func(job *transport.ServiceMutationJob) {
			job.PackageName += "-different"
		}},
		{name: "status", mutate: func(job *transport.ServiceMutationJob) {
			job.Status = mutationSucceeded
		}},
		{name: "phase", mutate: func(job *transport.ServiceMutationJob) {
			job.Phase = "orphaned"
		}},
		{name: "unknown-interrupted-code", mutate: func(job *transport.ServiceMutationJob) {
			job.ErrorCode = "agent_restarted_before_completion"
		}},
		{name: "inexact-interrupted-message", mutate: func(job *transport.ServiceMutationJob) {
			job.ErrorMessage += " "
		}},
		{name: "crossed-interrupted-reason", mutate: func(job *transport.ServiceMutationJob) {
			job.ErrorMessage = switchRolledBackAfterRestartMessage
		}},
		{name: "trigger-reason-in-interrupted-phase", mutate: func(job *transport.ServiceMutationJob) {
			job.ErrorCode = triggerFailureCode
			job.ErrorMessage = triggerFailureMessage
		}},
		{name: "restart-reason-in-failed-phase", mutate: func(job *transport.ServiceMutationJob) {
			job.Phase = mutationFailed
		}},
		{name: "inexact-trigger-rejection", mutate: func(job *transport.ServiceMutationJob) {
			job.Phase = mutationFailed
			job.ErrorCode = triggerFailureCode
			job.ErrorMessage = triggerFailureMessage + " "
		}},
		{name: "attempt", mutate: func(job *transport.ServiceMutationJob) {
			job.Attempt = 0
		}},
		{name: "started-at", mutate: func(job *transport.ServiceMutationJob) {
			job.StartedAt = time.Time{}
		}},
		{name: "updated-at", mutate: func(job *transport.ServiceMutationJob) {
			job.UpdatedAt = time.Time{}
		}},
		{name: "deadline-at", mutate: func(job *transport.ServiceMutationJob) {
			job.DeadlineAt = time.Time{}
		}},
		{name: "finished-at", mutate: func(job *transport.ServiceMutationJob) {
			job.FinishedAt = time.Time{}
		}},
		{name: "updated-before-start", mutate: func(job *transport.ServiceMutationJob) {
			job.UpdatedAt = job.StartedAt.Add(-time.Nanosecond)
			job.FinishedAt = job.UpdatedAt
		}},
		{name: "deadline-before-start", mutate: func(job *transport.ServiceMutationJob) {
			job.DeadlineAt = job.StartedAt.Add(-time.Nanosecond)
		}},
		{name: "updated-differs-from-finished", mutate: func(job *transport.ServiceMutationJob) {
			job.UpdatedAt = job.FinishedAt.Add(-time.Nanosecond)
		}},
		{name: "lease", mutate: func(job *transport.ServiceMutationJob) {
			job.LeaseExpiresAt = job.FinishedAt.Add(time.Second)
		}},
		{name: "worker-pid", mutate: func(job *transport.ServiceMutationJob) {
			job.WorkerPID = 123
		}},
		{name: "worker-started", mutate: func(job *transport.ServiceMutationJob) {
			job.WorkerStarted = "456"
		}},
		{name: "worker-command", mutate: func(job *transport.ServiceMutationJob) {
			job.WorkerCommand = "/bin/false"
		}},
	}
	if err := validateRetryableFailedJob(nil, identity); err == nil {
		t.Fatal("nil failed receipt was accepted")
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := valid()
			test.mutate(job)
			if err := validateRetryableFailedJob(job, identity); err == nil {
				t.Fatalf("inexact receipt was accepted: %+v", job)
			}
		})
	}
}

func TestRunRPCSwitchRetryAcceptsExactFinalizedReceiptIdempotently(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"pdns-switch", transport.DNSEngineBIND, transport.DNSEnginePowerDNS, 1,
	))
	identity := transport.ServiceMutationBeginRequest{
		RequestID: testRequestID, OwnerID: testOwnerID,
		Kind: mutationKind, Target: string(request.TargetEngine),
		PackageName: request.ManifestQualifier,
	}
	methods := 0
	call := func(_ context.Context, method string, _, output any) error {
		methods++
		if method != "Agent.ServiceMutationStatus" {
			return errors.New("idempotent retry made another RPC")
		}
		output.(*transport.ServiceMutationResponse).Job = succeededJob(identity)
		return nil
	}
	result, err := runRPCSwitch(
		context.Background(), testRequestID, testOwnerID, request,
		true, time.Hour, call,
	)
	if err != nil {
		t.Fatalf("runRPCSwitch idempotent retry: %v", err)
	}
	if methods != 1 || result.terminal == nil || result.terminal.Status != mutationSucceeded {
		t.Fatalf("methods=%d terminal=%+v", methods, result.terminal)
	}
}

func TestRunRPCSwitchInitialRejectsPreexistingTerminalRequest(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	))
	identity := transport.ServiceMutationBeginRequest{
		RequestID: testRequestID, OwnerID: testOwnerID,
		Kind: mutationKind, Target: string(request.TargetEngine),
		PackageName: request.ManifestQualifier,
	}
	call := func(_ context.Context, method string, _, output any) error {
		if method != "Agent.BeginServiceMutation" {
			return errors.New("initial trigger proceeded past reused request ID")
		}
		output.(*transport.ServiceMutationResponse).Job = succeededJob(identity)
		return nil
	}
	if _, err := runRPCSwitch(
		context.Background(), testRequestID, testOwnerID, request,
		false, time.Hour, call,
	); err == nil {
		t.Fatal("initial trigger accepted a preexisting terminal request ID")
	}
}

func TestRunRPCSwitchRejectsWorkerBackedDNSLease(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	))
	switchCalls := 0
	call := func(_ context.Context, method string, input, output any) error {
		if method != "Agent.BeginServiceMutation" {
			switchCalls++
			return nil
		}
		begin := input.(*transport.ServiceMutationBeginRequest)
		job := runningJob(*begin)
		job.WorkerPID = 123
		job.WorkerStarted = "99"
		job.WorkerCommand = "unexpected-worker"
		output.(*transport.ServiceMutationResponse).Job = job
		return nil
	}
	if _, err := runRPCSwitch(
		context.Background(), testRequestID, testOwnerID, request,
		false, time.Hour, call,
	); err == nil {
		t.Fatal("worker-backed DNS lease was accepted")
	}
	if switchCalls != 0 {
		t.Fatalf("switch was invoked %d times after invalid lease", switchCalls)
	}
}

func standaloneScenario(
	driver string,
	source, target transport.DNSEngine,
	sourceEpoch int64,
) scenario {
	mode := transport.DNSEngineSwitchModeSwitch
	if driver == "pdns-adopt" {
		mode = transport.DNSEngineSwitchModeAdopt
	}
	sourceFixture := "uninitialized"
	if source == transport.DNSEnginePowerDNS {
		sourceFixture = "managed-pdns"
	} else if source == transport.DNSEngineBIND {
		sourceFixture = "managed-bind"
	}
	if driver == "pdns-adopt" {
		sourceFixture = "external-pdns-adoption"
	}
	return scenario{
		Schema: scenarioSchema, Driver: driver,
		SourceFixture: sourceFixture, Mode: mode,
		SourceEngine: source, TargetEngine: target,
		SourceEpoch: sourceEpoch, TargetEpoch: sourceEpoch + 1,
		Topology: transport.DNSTopologyStandalone,
		Zones:    []transport.DNSEngineSwitchZoneSnapshot{},
	}
}

func mustScenarioRequest(
	t *testing.T,
	value scenario,
) transport.SwitchDNSEngineV1Request {
	t.Helper()
	request, err := requestForScenario(value, value.Driver)
	if err != nil {
		t.Fatalf("requestForScenario: %v", err)
	}
	return request
}

func runningJob(identity transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	now := time.Now().UTC()
	return &transport.ServiceMutationJob{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: identity.Kind, Target: identity.Target,
		PackageName: identity.PackageName,
		Status:      mutationRunning, Phase: mutationLeasedPhase, Attempt: 1,
		StartedAt: now, UpdatedAt: now,
		LeaseExpiresAt: now.Add(20 * time.Second),
		DeadlineAt:     now.Add(time.Hour),
	}
}

func succeededJob(identity transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	return succeededJobAtPhase(
		identity,
		"commit/dns-engine-switch/v2/finalized/"+identity.RequestID+"/"+identity.PackageName,
	)
}

func succeededJobAtPhase(
	identity transport.ServiceMutationBeginRequest,
	phase string,
) *transport.ServiceMutationJob {
	now := time.Now().UTC()
	return &transport.ServiceMutationJob{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: identity.Kind, Target: identity.Target,
		PackageName: identity.PackageName,
		Status:      mutationSucceeded,
		Phase:       phase,
		Attempt:     1, StartedAt: now.Add(-time.Second), UpdatedAt: now,
		DeadlineAt: now.Add(time.Hour), FinishedAt: now,
	}
}

func failedJob(
	identity transport.ServiceMutationBeginRequest,
	phase, code, message string,
) *transport.ServiceMutationJob {
	now := time.Now().UTC()
	return &transport.ServiceMutationJob{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: identity.Kind, Target: identity.Target,
		PackageName: identity.PackageName,
		Status:      mutationFailed, Phase: phase,
		Attempt: 1, StartedAt: now.Add(-time.Second), UpdatedAt: now,
		DeadlineAt: now.Add(time.Hour), FinishedAt: now,
		ErrorCode: code, ErrorMessage: message,
	}
}
