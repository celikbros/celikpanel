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

// The pending deletion exactly as batch 9 z04 recorded it after the owner
// enrollment: the lifecycle's child, not the parent, in the phase.
func batch9PendingChildDeletion(begin transport.ServiceMutationBeginRequest, domain string) *transport.ServiceMutationJob {
	now := time.Now().UTC()
	return &transport.ServiceMutationJob{
		RequestID: begin.RequestID, OwnerID: begin.OwnerID, Kind: begin.Kind,
		Target: begin.Target, PackageName: begin.PackageName, Status: "pending",
		Phase: "commit/dns-zone-sync/v3/propagation-pending/" + begin.RequestID + "/" +
			domain + "/" + begin.PackageName,
		ErrorCode:    "dns_peer_enrollment_required",
		ErrorMessage: "The exact local DNS publication is waiting for paired propagation recovery.",
		Attempt:      1, StartedAt: now.Add(-time.Minute), UpdatedAt: now,
		DeadlineAt: now.Add(time.Hour), FinishedAt: now,
	}
}

type recoverFake struct {
	agent    *fakeAgent
	statuses int
	resumed  []transport.ServiceMutationBeginRequest
	recovery []transport.RecoverDNSZoneV3Request
}

// newRecoverFake answers the first status with before, resumes into the
// child's recovering phase, and answers the status after RecoverDNSZoneV3
// with after (nil: the same pending job).
func newRecoverFake(
	begin transport.ServiceMutationBeginRequest, before, after *transport.ServiceMutationJob,
	response transport.RecoverDNSZoneV3Response,
) *recoverFake {
	fake := &recoverFake{}
	fake.agent = &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.ServiceMutationStatus": func(req, resp any) error {
			if req.(*transport.ServiceMutationStatusRequest).RequestID != begin.RequestID {
				return errors.New("status of another request")
			}
			fake.statuses++
			job := before
			if fake.statuses > 1 && after != nil {
				job = after
			}
			resp.(*transport.ServiceMutationResponse).Job = job
			return nil
		},
		"Agent.BeginServiceMutation": func(req, resp any) error {
			resume := *req.(*transport.ServiceMutationBeginRequest)
			fake.resumed = append(fake.resumed, resume)
			job := runningLease(begin)
			job.Phase = freshPrimaryZonePhase("recovering", begin)
			resp.(*transport.ServiceMutationResponse).Job = job
			return nil
		},
		"Agent.HeartbeatServiceMutation": func(any, any) error { return nil },
		"Agent.RecoverDNSZoneV3": func(req, resp any) error {
			fake.recovery = append(fake.recovery, *req.(*transport.RecoverDNSZoneV3Request))
			*resp.(*transport.RecoverDNSZoneV3Response) = response
			return nil
		},
	}}
	return fake
}

func TestFreshPrimaryZoneRecoveryResumesThePendingChildDeletion(t *testing.T) {
	receipt, _ := freshPrimaryV3TestReceipt(t)
	request, begin, err := freshPrimaryZoneIdentity("delete", receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(begin.PackageName, "dns-zone-sync/v3:sha256:") {
		t.Fatalf("the delete qualifier is not the V3 package shape: %q", begin.PackageName)
	}
	pending := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
	if pending.Phase != freshPrimaryZonePhase("propagation-pending", begin) {
		t.Fatalf("the lifecycle's own pending phase differs from batch 9's: %q", pending.Phase)
	}
	published := runningLease(begin)
	published.Status, published.Phase = mutationSucceeded, freshPrimaryZonePhase("published", begin)
	published.LeaseExpiresAt, published.FinishedAt = time.Time{}, time.Now().UTC()
	fake := newRecoverFake(begin, pending, published, transport.RecoverDNSZoneV3Response{Recovered: true})
	result := freshPrimaryZoneResult{}
	if err := recoverFreshPrimaryZoneDeletion(context.Background(), request, begin, fake.agent.call, &result); err != nil {
		t.Fatalf("the pending child deletion was not resumed: %v (%+v)", err, result)
	}
	if result.Outcome != "verified_published" || result.JobPhase != published.Phase {
		t.Fatalf("resumed child deletion: %+v", result)
	}
	if len(fake.resumed) != 1 || !fake.resumed[0].Resume ||
		fake.resumed[0].RequestID != begin.RequestID || fake.resumed[0].PackageName != begin.PackageName {
		t.Fatalf("the exact identity was not resumed once: %+v", fake.resumed)
	}
	if len(fake.recovery) != 1 || fake.recovery[0].Domain != "s2.s1-kill.test" ||
		fake.recovery[0].Qualifier != begin.PackageName ||
		fake.recovery[0].MutationRequestID != begin.RequestID ||
		fake.recovery[0].MutationOwnerID != begin.OwnerID {
		t.Fatalf("recovery did not name the exact child operation: %+v", fake.recovery)
	}
	for _, method := range []string{"Agent.SyncDNSZoneV3", "Agent.FinishServiceMutation"} {
		if fake.agent.count(method) != 0 {
			t.Fatalf("recovery issued %s: %v", method, fake.agent.calls)
		}
	}

	// Still pending after the Agent's reconciliation: the same job is kept.
	again := newRecoverFake(begin, pending, nil, transport.RecoverDNSZoneV3Response{RecoveryPending: true})
	result = freshPrimaryZoneResult{}
	if err := recoverFreshPrimaryZoneDeletion(context.Background(), request, begin, again.agent.call, &result); err != nil ||
		result.Outcome != "pending_exact_operation" || again.agent.count("Agent.FinishServiceMutation") != 0 {
		t.Fatalf("a still-pending child deletion: %v %+v calls=%v", err, result, again.agent.calls)
	}
}

func TestFreshPrimaryZoneRecoveryRefusesTheParentNameAndOtherShapes(t *testing.T) {
	receipt, _ := freshPrimaryV3TestReceipt(t)
	request, begin, _ := freshPrimaryZoneIdentity("delete", receipt)
	_, otherBegin, _ := freshPrimaryZoneIdentity("edit", receipt)
	for name, job := range map[string]*transport.ServiceMutationJob{
		// The name the old helper built: the served parent, not the deleted child.
		"parent name": batch9PendingChildDeletion(begin, "s1-kill.test"),
		"running": func() *transport.ServiceMutationJob {
			job := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
			job.Status = mutationRunning
			return job
		}(),
		"published": func() *transport.ServiceMutationJob {
			job := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
			job.Phase = freshPrimaryZonePhase("published", begin)
			return job
		}(),
		"another package": func() *transport.ServiceMutationJob {
			job := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
			job.Phase = "commit/dns-zone-sync/v3/propagation-pending/" + begin.RequestID +
				"/s2.s1-kill.test/" + otherBegin.PackageName
			return job
		}(),
		"another request": func() *transport.ServiceMutationJob {
			job := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
			job.Phase = "commit/dns-zone-sync/v3/propagation-pending/" + otherBegin.RequestID +
				"/s2.s1-kill.test/" + begin.PackageName
			return job
		}(),
		"leased": func() *transport.ServiceMutationJob {
			job := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
			job.LeaseExpiresAt = time.Now().Add(time.Minute)
			return job
		}(),
	} {
		fake := newRecoverFake(begin, job, nil, transport.RecoverDNSZoneV3Response{Recovered: true})
		result := freshPrimaryZoneResult{}
		err := recoverFreshPrimaryZoneDeletion(context.Background(), request, begin, fake.agent.call, &result)
		if err == nil || result.Outcome != "refused_not_pending" ||
			fake.agent.count("Agent.BeginServiceMutation") != 0 || fake.agent.count("Agent.RecoverDNSZoneV3") != 0 {
			t.Fatalf("%s: resumed or not refused: %v %+v calls=%v", name, err, result, fake.agent.calls)
		}
		if result.JobPhase != job.Phase || result.JobCode != job.ErrorCode {
			t.Fatalf("%s: the refused job's state was not recorded: %+v", name, result)
		}
	}
	// The domain must be the operation's own target.
	child := batch9PendingChildDeletion(begin, "s2.s1-kill.test")
	if !exactPendingZoneDeletionJob(child, begin, freshPrimaryZoneDomain) ||
		exactPendingZoneDeletionJob(child, begin, deletionTrialDomain) ||
		exactPendingDeletionJob(child, begin) {
		t.Fatal("the pending-deletion match is not bound to the operation's own zone")
	}
}

func freshPrimaryV3ZeroZoneTestScenario() scenario {
	value := freshPrimaryV3TestScenario()
	value.Zones = []transport.DNSEngineSwitchZoneSnapshot{}
	return value
}

// The zero-zone variant (guest_bootstrap.py --zero-zones): the scenario a new
// server installs through the wizard. It must pass the same public-RPC path as
// the one-member scenario: canonical request, gate probe, zone lifecycle source.
func TestFreshPairedPrimaryZeroZoneScenarioIsAccepted(t *testing.T) {
	value := freshPrimaryV3ZeroZoneTestScenario()
	if !freshPairedPrimaryScenario(value) || !freshPairedPrimaryZeroZoneScenario(value) {
		t.Fatal("the zero-zone fresh paired primary scenario was refused")
	}
	request, err := requestForScenario(value, "pdns-switch")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Zones) != 0 || request.PairRole != transport.DNSPairRolePrimary ||
		request.ServiceMutationBinding != (transport.ServiceMutationBinding{}) {
		t.Fatalf("unexpected zero-zone request: %+v", request)
	}
	member, err := requestForScenario(freshPrimaryV3TestScenario(), "pdns-switch")
	if err != nil || member.ManifestQualifier == request.ManifestQualifier {
		t.Fatalf("the zero-zone manifest must have its own qualifier: %v", err)
	}
	for _, mutate := range []func(*scenario){
		func(s *scenario) { s.Zones = nil },
		func(s *scenario) { s.PairRole = "secondary" },
		func(s *scenario) { s.SourceFixture = "managed-bind" },
	} {
		changed := freshPrimaryV3ZeroZoneTestScenario()
		mutate(&changed)
		if freshPairedPrimaryScenario(changed) {
			t.Fatalf("a non-fresh-primary scenario was accepted: %+v", changed)
		}
	}
	other := freshPrimaryV3TestScenario()
	other.Zones[0].Domain = "other.test"
	if freshPairedPrimaryScenario(other) {
		t.Fatal("a one-zone scenario other than s1-kill.test was accepted")
	}
}

func TestFreshPairedPrimaryZeroZoneScenarioLoadsFromTheDocument(t *testing.T) {
	encoded, err := json.Marshal(freshPrimaryV3ZeroZoneTestScenario())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"zones":[]`) {
		t.Fatalf("the zero-zone document must carry an explicit empty list: %s", encoded)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, request, err := loadScenario(path, "pdns-switch")
	if err != nil {
		t.Fatal(err)
	}
	if !freshPairedPrimaryScenario(loaded) || len(request.Zones) != 0 {
		t.Fatalf("the loaded zero-zone scenario is not the fresh paired primary: %+v", loaded)
	}
	agent := &fakeAgent{handlers: map[string]func(any, any) error{
		"Agent.SwitchDNSEngineV1": func(req, resp any) error {
			if len(req.(*transport.SwitchDNSEngineV1Request).Zones) != 0 {
				return errors.New("the probe sent zones")
			}
			resp.(*transport.SwitchDNSEngineV1Response).Error = agentLeaseRequiredReason
			return nil
		},
	}}
	cell := "pdns-switch__target-started__after-write__paired-primary__peer-reachable"
	result, err := runGateProbe(context.Background(), cell, request, agent.call)
	if err != nil || result.Gate != gateOpen || len(agent.calls) != 1 {
		t.Fatalf("zero-zone gate probe: result=%+v err=%v calls=%v", result, err, agent.calls)
	}
}

func TestFreshPrimaryZeroZoneLifecycleSourceAdmitsAnyPositiveSerial(t *testing.T) {
	request, err := requestForScenario(freshPrimaryV3ZeroZoneTestScenario(), "pdns-switch")
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := freshPrimaryV3TestReceipt(t)
	receipt.ManifestQualifier = request.ManifestQualifier
	source := freshPrimaryV3ZeroZoneTestScenario()
	for _, serial := range []uint32{1, 1790588837} {
		state := freshPrimaryV3TestState(receipt)
		state.Publication.CatalogSerial = serial
		if err := validateFreshPrimaryZoneSource(source, request, receipt, state); err != nil {
			t.Fatalf("zero-zone state with catalog serial %d refused: %v", serial, err)
		}
	}
	state := freshPrimaryV3TestState(receipt)
	state.Publication.CatalogSerial = 0
	if validateFreshPrimaryZoneSource(source, request, receipt, state) == nil {
		t.Fatal("a zero catalog serial was accepted")
	}
	// The one-member scenario keeps its re-stamp floor.
	memberReceipt, memberRequest := freshPrimaryV3TestReceipt(t)
	memberState := freshPrimaryV3TestState(memberReceipt)
	memberState.Publication.CatalogSerial = 1
	if validateFreshPrimaryZoneSource(freshPrimaryV3TestScenario(), memberRequest, memberReceipt, memberState) == nil {
		t.Fatal("the one-member scenario accepted the staged catalog serial")
	}
	// The lifecycle's first step is still the child's add at generation 1.
	sync, begin, err := freshPrimaryZoneIdentity("add", receipt)
	if err != nil || sync.DesiredGeneration != 1 || sync.Delete || begin.Target != freshPrimaryZoneDomain {
		t.Fatalf("zero-zone add identity: %+v %+v %v", sync, begin, err)
	}
}
