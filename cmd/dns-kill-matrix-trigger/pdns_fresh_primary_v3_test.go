package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The probe classifies the Agent's exact refusal texts; a drift in either
// constant must fail here, not silently turn "closed" into "unknown".
func TestGateProbeReasonsMatchAgentSource(t *testing.T) {
	switchSource, err := os.ReadFile("../agent/dns_engine_pdns_switch.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(switchSource),
		"const pdnsPairedPrimarySwitchPausedReason = \""+agentPDNSPrimaryPausedReason+"\"") {
		t.Fatal("the Agent's paired-primary pause reason differs from the probe's copy")
	}
	leaseSource, err := os.ReadFile("../agent/service_mutation_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(leaseSource), "errors.New(\""+agentLeaseRequiredReason+"\")") {
		t.Fatal("the Agent's lease-required refusal differs from the probe's copy")
	}
}

func TestClassifyGateProbe(t *testing.T) {
	for _, test := range []struct {
		response transport.SwitchDNSEngineV1Response
		want     string
	}{
		{transport.SwitchDNSEngineV1Response{Error: agentPDNSPrimaryPausedReason}, gateClosed},
		{transport.SwitchDNSEngineV1Response{Error: agentLeaseRequiredReason}, gateOpen},
		{transport.SwitchDNSEngineV1Response{Error: "switching a serving BIND source to PowerDNS is unsupported"}, gateUnknown},
		{transport.SwitchDNSEngineV1Response{}, gateUnknown},
		{transport.SwitchDNSEngineV1Response{Applied: true, Error: agentLeaseRequiredReason}, gateUnknown},
	} {
		if got, detail := classifyGateProbe(test.response); got != test.want || detail == "" {
			t.Fatalf("classify %+v = %q (%q), want %q", test.response, got, detail, test.want)
		}
	}
}

func freshPrimaryV3TestScenario() scenario {
	return scenario{
		Schema: scenarioSchema, Driver: "pdns-switch", SourceFixture: "uninitialized",
		Mode: transport.DNSEngineSwitchModeSwitch, TargetEngine: transport.DNSEnginePowerDNS,
		TargetEpoch: 1, Topology: transport.DNSTopologyPaired, PairRole: "primary",
		LocalIP: "192.0.2.10", LocalNS: "ns1.s1-kill.test",
		PeerIP: "192.0.2.11", PeerNS: "ns2.s1-kill.test",
		Zones: []transport.DNSEngineSwitchZoneSnapshot{{
			Domain: "s1-kill.test", DesiredGeneration: 1, ZoneType: "MASTER",
			Records: []transport.ZoneRecord{
				{Name: "s1-kill.test", Type: "SOA", Content: "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600", TTL: 3600},
				{Name: "s1-kill.test", Type: "NS", Content: "ns1.s1-kill.test", TTL: 3600},
				{Name: "ns1.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300},
				{Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300},
				{Name: "s1-kill.test", Type: "NS", Content: "ns2.s1-kill.test", TTL: 3600},
				{Name: "ns2.s1-kill.test", Type: "A", Content: "192.0.2.11", TTL: 300},
			},
		}},
	}
}

func TestFreshPairedPrimaryScenarioIsAnAcceptedPDNSSwitch(t *testing.T) {
	value := freshPrimaryV3TestScenario()
	if !freshPairedPrimaryScenario(value) {
		t.Fatal("the prepared fresh paired primary scenario was refused")
	}
	request, err := requestForScenario(value, "pdns-switch")
	if err != nil {
		t.Fatal(err)
	}
	if request.PairRole != transport.DNSPairRolePrimary || request.SourceEngine != "" ||
		request.ServiceMutationBinding != (transport.ServiceMutationBinding{}) {
		t.Fatalf("unexpected canonical request: %+v", request)
	}
	for _, mutate := range []func(*scenario){
		func(s *scenario) { s.SourceFixture = "managed-bind" },
		func(s *scenario) { s.PairRole = "secondary" },
		func(s *scenario) { s.Zones[0].ZoneType = "NATIVE" },
		func(s *scenario) { s.LocalIP = "192.0.2.11" },
	} {
		changed := freshPrimaryV3TestScenario()
		mutate(&changed)
		if freshPairedPrimaryScenario(changed) {
			t.Fatalf("a non-fresh-primary scenario was accepted: %+v", changed)
		}
	}
}

func TestFreshPairedPrimaryCellAdmitsOnlyV3JournalWrites(t *testing.T) {
	for _, phase := range []string{"intent", "target-staged", "source-stopped", "target-started", "target-verified", "committed"} {
		for _, edge := range []string{"before-write", "after-write"} {
			id := "pdns-switch__" + phase + "__" + edge + "__paired-primary__peer-reachable"
			if !freshPairedPrimaryCell(id) {
				t.Fatalf("%s refused", id)
			}
		}
	}
	for _, id := range []string{
		"pdns-switch__intent__after-write__paired-primary__peer-unreachable",
		"pdns-switch__pre-intent__paired-primary__peer-reachable",
		"pdns-switch__rolling-back__after-write__paired-primary__peer-reachable",
		"pdns-switch__rolled-back__before-write__paired-primary__peer-reachable",
		"pdns-switch__intent__after-write__standalone__peer-reachable",
		"bind__intent__after-write__paired-primary__peer-reachable",
	} {
		if freshPairedPrimaryCell(id) {
			t.Fatalf("%s accepted", id)
		}
	}
}

type fakeAgent struct {
	calls    []string
	handlers map[string]func(request, response any) error
}

func (f *fakeAgent) call(_ context.Context, method string, request, response any) error {
	f.calls = append(f.calls, method)
	handler := f.handlers[method]
	if handler == nil {
		return errors.New("unexpected RPC " + method)
	}
	return handler(request, response)
}

func (f *fakeAgent) count(method string) int {
	total := 0
	for _, call := range f.calls {
		if call == method {
			total++
		}
	}
	return total
}

func TestGateProbeSendsNoLeaseAndMutatesNothing(t *testing.T) {
	request, err := requestForScenario(freshPrimaryV3TestScenario(), "pdns-switch")
	if err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.SwitchDNSEngineV1": func(req, resp any) error {
			if req.(*transport.SwitchDNSEngineV1Request).ServiceMutationBinding != (transport.ServiceMutationBinding{}) {
				return errors.New("probe carried a lease")
			}
			resp.(*transport.SwitchDNSEngineV1Response).Error = agentPDNSPrimaryPausedReason
			return nil
		},
	}}
	cell := "pdns-switch__intent__after-write__paired-primary__peer-reachable"
	result, err := runGateProbe(context.Background(), cell, request, agent.call)
	if err != nil || result.Gate != gateClosed || len(agent.calls) != 1 {
		t.Fatalf("closed gate probe: result=%+v err=%v calls=%v", result, err, agent.calls)
	}
	bound := request
	bound.ServiceMutationBinding = transport.ServiceMutationBinding{
		MutationRequestID: testRequestID, MutationOwnerID: testOwnerID,
	}
	agent.calls = nil
	if _, err := runGateProbe(context.Background(), cell, bound, agent.call); err == nil || len(agent.calls) != 0 {
		t.Fatal("a probe carrying a lease was sent")
	}
}

func unrelatedTestIdentity(t *testing.T) transport.ServiceMutationBeginRequest {
	t.Helper()
	begin, err := unrelatedProbeIdentity("pdns-switch__target-started__after-write__paired-primary__peer-reachable", testRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if begin.RequestID == testRequestID || begin.Kind != unrelatedProbeKind || begin.Target != unrelatedProbeTarget {
		t.Fatalf("unrelated identity is not distinct: %+v", begin)
	}
	return begin
}

func runningLease(begin transport.ServiceMutationBeginRequest) *transport.ServiceMutationJob {
	now := time.Now().UTC()
	return &transport.ServiceMutationJob{
		RequestID: begin.RequestID, OwnerID: begin.OwnerID, Kind: begin.Kind,
		Target: begin.Target, PackageName: begin.PackageName, Status: mutationRunning,
		Phase: mutationLeasedPhase, Attempt: 1, StartedAt: now, UpdatedAt: now,
		LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
	}
}

func TestUnrelatedBeginFinishesItsOwnLeaseFailed(t *testing.T) {
	begin := unrelatedTestIdentity(t)
	var finish *transport.ServiceMutationFinishRequest
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.ServiceMutationStatus": func(any, any) error { return nil },
		"Agent.BeginServiceMutation": func(req, resp any) error {
			resp.(*transport.ServiceMutationResponse).Job = runningLease(*req.(*transport.ServiceMutationBeginRequest))
			return nil
		},
		"Agent.FinishServiceMutation": func(req, resp any) error {
			finish = req.(*transport.ServiceMutationFinishRequest)
			job := runningLease(begin)
			job.Status = mutationFailed
			resp.(*transport.ServiceMutationResponse).Job = job
			return nil
		},
	}}
	result, err := runUnrelatedBegin(context.Background(), begin, agent.call)
	if err != nil || !result.Begun || !result.Finished || finish == nil ||
		finish.Success || finish.FailureCode != unrelatedProbeCode {
		t.Fatalf("result=%+v err=%v finish=%+v", result, err, finish)
	}
}

func TestUnrelatedBeginRefusalIsVerifiedAndNothingIsFinished(t *testing.T) {
	begin := unrelatedTestIdentity(t)
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.ServiceMutationStatus": func(any, any) error { return nil },
		"Agent.BeginServiceMutation": func(_, resp any) error {
			resp.(*transport.ServiceMutationResponse).Error = "another host mutation is active"
			return nil
		},
	}}
	result, err := runUnrelatedBegin(context.Background(), begin, agent.call)
	if err != nil || result.Begun || result.BeginError == "" || agent.count("Agent.FinishServiceMutation") != 0 {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, agent.calls)
	}
	agent.handlers["Agent.ServiceMutationStatus"] = func(_, resp any) error {
		resp.(*transport.ServiceMutationResponse).Job = runningLease(begin)
		return nil
	}
	agent.calls = nil
	if _, err := runUnrelatedBegin(context.Background(), begin, agent.call); err == nil ||
		agent.count("Agent.BeginServiceMutation") != 0 {
		t.Fatal("an existing unrelated identity was begun again")
	}
}

func freshPrimaryV3TestReceipt(t *testing.T) (identityReceipt, transport.SwitchDNSEngineV1Request) {
	t.Helper()
	request, err := requestForScenario(freshPrimaryV3TestScenario(), "pdns-switch")
	if err != nil {
		t.Fatal(err)
	}
	cell := "pdns-switch__committed__after-write__paired-primary__peer-reachable"
	owner, err := deterministicOwnerIdentity(cell, testRequestID)
	if err != nil {
		t.Fatal(err)
	}
	return identityReceipt{
		Schema: identityReceiptSchema, CellID: cell, Driver: "pdns-switch",
		SourceFixture: "uninitialized", RequestID: testRequestID, OwnerID: owner,
		ManifestQualifier: request.ManifestQualifier,
	}, request
}

func freshPrimaryV3TestState(receipt identityReceipt) freshPrimaryStateEnvelope {
	var state freshPrimaryStateEnvelope
	state.Schema = freshPrimaryStateSchema
	state.NativeCatalog = freshPrimaryNativeMarker
	state.Acquisition.Schema = "celikpanel-dns-engine-acquisition/v1"
	state.Acquisition.Mode = "switch"
	state.Acquisition.Engine = "pdns"
	state.Acquisition.EngineEpoch = 1
	state.Acquisition.PairRole = "primary"
	state.Acquisition.LocalIP = "192.0.2.10"
	state.Acquisition.PeerIP = "192.0.2.11"
	state.Acquisition.ManifestQualifier = receipt.ManifestQualifier
	state.Acquisition.RequestID = receipt.RequestID
	state.Acquisition.OwnerID = receipt.OwnerID
	state.Publication.Schema = "celikpanel-dns-engine-publication/v1"
	state.Publication.CatalogSerial = 1790588837
	return state
}

func TestFreshPrimaryZoneSourceIsBoundToTheMeasuredSwitch(t *testing.T) {
	receipt, request := freshPrimaryV3TestReceipt(t)
	source := freshPrimaryV3TestScenario()
	state := freshPrimaryV3TestState(receipt)
	if err := validateFreshPrimaryZoneSource(source, request, receipt, state); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*freshPrimaryStateEnvelope){
		func(s *freshPrimaryStateEnvelope) { s.Schema = "celikpanel-dns-engine-state/v2" },
		func(s *freshPrimaryStateEnvelope) { s.NativeCatalog = "" },
		func(s *freshPrimaryStateEnvelope) { s.Acquisition.RequestID = strings.Repeat("9", 32) },
		func(s *freshPrimaryStateEnvelope) { s.Publication.CatalogSerial = 1 },
	} {
		changed := freshPrimaryV3TestState(receipt)
		mutate(&changed)
		if err := validateFreshPrimaryZoneSource(source, request, receipt, changed); err == nil {
			t.Fatalf("a foreign state was accepted: %+v", changed)
		}
	}
}

func TestFreshPrimaryZoneStepsAreTheFourDistinctRequests(t *testing.T) {
	receipt, _ := freshPrimaryV3TestReceipt(t)
	seen := map[string]bool{}
	for index, step := range freshPrimaryZoneSteps {
		request, begin, err := freshPrimaryZoneIdentity(step.name, receipt)
		if err != nil {
			t.Fatal(err)
		}
		if request.DesiredGeneration != int64(index+1) || request.Delete != (step.name == "delete") ||
			request.Engine != transport.DNSEnginePowerDNS || request.ZoneType != "MASTER" ||
			begin.Kind != mutationKindDNSZoneSync || begin.Target != freshPrimaryZoneDomain ||
			seen[begin.RequestID] || begin.RequestID == receipt.RequestID {
			t.Fatalf("%s identity is not exact: %+v %+v", step.name, request, begin)
		}
		seen[begin.RequestID] = true
		changed := false
		for _, record := range request.Records {
			changed = changed || record.Name == "changed."+freshPrimaryZoneDomain
		}
		if changed != (step.name == "edit") {
			t.Fatalf("%s edited record presence is wrong", step.name)
		}
	}
	if _, _, err := freshPrimaryZoneIdentity("rename", receipt); err == nil {
		t.Fatal("an unknown step was accepted")
	}
}

func TestFreshPrimaryZoneStepRefusesWithoutPublishedPredecessor(t *testing.T) {
	receipt, _ := freshPrimaryV3TestReceipt(t)
	request, begin, err := freshPrimaryZoneIdentity("edit", receipt)
	if err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.ServiceMutationStatus": func(any, any) error { return nil },
	}}
	result := freshPrimaryZoneResult{}
	if err := runFreshPrimaryZoneStep(context.Background(), request, begin, nil, agent.call, &result); err == nil ||
		len(agent.calls) != 0 {
		t.Fatal("edit ran without its predecessor identity")
	}
	_, prior, _ := freshPrimaryZoneIdentity("add", receipt)
	if err := runFreshPrimaryZoneStep(context.Background(), request, begin, &prior, agent.call, &result); err == nil ||
		result.Outcome != "refused_predecessor" || agent.count("Agent.BeginServiceMutation") != 0 {
		t.Fatalf("edit began without a published add: %+v calls=%v", result, agent.calls)
	}
}

func TestFreshPrimaryZonePendingDeletionIsNotFinished(t *testing.T) {
	receipt, _ := freshPrimaryV3TestReceipt(t)
	request, begin, _ := freshPrimaryZoneIdentity("delete", receipt)
	_, prior, _ := freshPrimaryZoneIdentity("edit", receipt)
	now := time.Now().UTC()
	pending := &transport.ServiceMutationJob{
		RequestID: begin.RequestID, OwnerID: begin.OwnerID, Kind: begin.Kind,
		Target: begin.Target, PackageName: begin.PackageName, Status: "pending",
		Phase: freshPrimaryZonePhase("propagation-pending", begin), Attempt: 1,
		StartedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Hour), FinishedAt: now,
	}
	statusCalls := 0
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.ServiceMutationStatus": func(req, resp any) error {
			statusCalls++
			id := req.(*transport.ServiceMutationStatusRequest).RequestID
			switch {
			case id == prior.RequestID:
				job := runningLease(prior)
				job.Status, job.Phase = mutationSucceeded, freshPrimaryZonePhase("published", prior)
				job.LeaseExpiresAt, job.FinishedAt = time.Time{}, now
				resp.(*transport.ServiceMutationResponse).Job = job
			case id == begin.RequestID && statusCalls > 2:
				resp.(*transport.ServiceMutationResponse).Job = pending
			}
			return nil
		},
		"Agent.BeginServiceMutation": func(_, resp any) error {
			resp.(*transport.ServiceMutationResponse).Job = runningLease(begin)
			return nil
		},
		"Agent.HeartbeatServiceMutation": func(any, any) error { return nil },
		"Agent.SyncDNSZoneV3": func(_, resp any) error {
			*resp.(*transport.SyncDNSZoneV3Response) = transport.SyncDNSZoneV3Response{
				RecoveryPending: true, Engine: transport.DNSEnginePowerDNS, EngineEpoch: 1,
				AppliedGeneration: 3,
			}
			return nil
		},
	}}
	result := freshPrimaryZoneResult{}
	if err := runFreshPrimaryZoneStep(context.Background(), request, begin, &prior, agent.call, &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "pending_exact_operation" || agent.count("Agent.FinishServiceMutation") != 0 {
		t.Fatalf("pending deletion: %+v calls=%v", result, agent.calls)
	}
}

func TestFreshPrimaryZoneRecoveryResumesOnlyAPendingDeletion(t *testing.T) {
	receipt, _ := freshPrimaryV3TestReceipt(t)
	request, begin, _ := freshPrimaryZoneIdentity("delete", receipt)
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.ServiceMutationStatus": func(any, any) error { return nil },
	}}
	result := freshPrimaryZoneResult{}
	if err := recoverFreshPrimaryZoneDeletion(context.Background(), request, begin, agent.call, &result); err == nil ||
		result.Outcome != "refused_not_pending" || agent.count("Agent.BeginServiceMutation") != 0 {
		t.Fatalf("a non-pending deletion was resumed: %+v calls=%v", result, agent.calls)
	}
	addRequest, addBegin, _ := freshPrimaryZoneIdentity("add", receipt)
	if err := recoverFreshPrimaryZoneDeletion(context.Background(), addRequest, addBegin, agent.call, &result); err == nil {
		t.Fatal("recovery accepted a non-deletion step")
	}
}
