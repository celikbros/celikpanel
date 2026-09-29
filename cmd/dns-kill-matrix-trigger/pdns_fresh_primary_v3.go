package main

// Fresh paired PowerDNS primary (V3 journal) through the public Agent RPC.
//
// Three read-or-bounded commands serve the kill-matrix controller and the
// QEMU host for the pdns-switch paired-primary cells:
//
//   - rpc-gate-probe asks the Agent whether its product gate admits the exact
//     scenario manifest, before any mutation: SwitchDNSEngineV1 without a
//     service-mutation lease. A closed gate answers with the pause reason
//     before the lease check; an open gate reaches the lease check and is
//     refused there. Neither answer creates a ledger job or touches the host.
//   - rpc-unrelated-begin proves a DNS-only hold: while a DNS operation is
//     held, an unrelated lease (service_install/nginx, no step, no worker) can
//     still be begun, and is then finished failed without any host effect.
//   - rpc-pdns-primary-zone-v3 and rpc-pdns-primary-zone-v3-recover drive the
//     Panel's zone-sync V3 RPCs (BeginServiceMutation dns_zone_sync,
//     SyncDNSZoneV3, FinishServiceMutation; RecoverDNSZoneV3 for a pending
//     deletion) for one child zone of the accepted primary: add, edit, delete,
//     re-add, each exactly once, each only after its predecessor published.
//
// Eşli PowerDNS birincilinin ilk kurulumu için: ürün kapısını değişiklik
// yapmadan soran, yalnız DNS'in tutulduğunu kanıtlayan ve kabul edilmiş
// birincilde bölge yaşam döngüsünü herkese açık RPC ile süren komutlar.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	// Copied verbatim from cmd/agent (pdnsPairedPrimarySwitchPausedReason and
	// requiredServiceMutationStep); the probe classifies only these exact texts.
	agentPDNSPrimaryPausedReason = "PowerDNS paired-primary switch is paused pending native catalog and rollback support; leave any current DNS engine serving and review another DNS plan"
	agentLeaseRequiredReason     = "a valid durable service mutation lease is required"

	gateProbeSchema = "celikpanel/dns-kill-matrix-gate-probe/v1"
	gateClosed      = "closed"
	gateOpen        = "open"
	gateUnknown     = "unknown"

	unrelatedProbeSchema  = "celikpanel/dns-kill-matrix-unrelated-begin/v1"
	unrelatedProbeKind    = "service_install"
	unrelatedProbeTarget  = "nginx"
	unrelatedProbeCode    = "dns_kill_matrix_unrelated_probe"
	unrelatedProbeMessage = "The DNS kill-matrix probe released an unrelated lease it began only to prove that a held DNS operation does not block other changes; nothing was changed on the host."

	freshPrimaryZoneSchema   = "celikpanel/dns-kill-matrix-pdns-primary-zone-v3/v1"
	freshPrimaryZoneDomain   = "s2.s1-kill.test"
	freshPrimaryZoneMarker   = "/etc/celikpanel-dns-kill-matrix"
	freshPrimaryZoneJournal  = "/var/lib/celikpanel-agent-private/dns-engine-switch-journal.json"
	freshPrimaryZoneState    = "/var/lib/celikpanel-agent-private/dns-engine-state.json"
	freshPrimaryStateSchema  = "celikpanel-dns-engine-state/v3"
	freshPrimaryNativeMarker = "pdns-fresh-paired-primary/debian-4.9/v1"
)

// freshPairedPrimaryScenario is the exact manifest the harness prepares for a
// fresh paired PowerDNS primary on the Debian guest (guest_bootstrap.py
// pdns_switch_scenario(role="paired-primary", source_fixture="uninitialized")).
func freshPairedPrimaryScenario(value scenario) bool {
	return value.Driver == "pdns-switch" && value.SourceFixture == "uninitialized" &&
		value.Mode == transport.DNSEngineSwitchModeSwitch && value.SourceEngine == "" &&
		value.TargetEngine == transport.DNSEnginePowerDNS && value.SourceEpoch == 0 &&
		value.TargetEpoch == 1 && value.SourceRevision == 0 &&
		value.Topology == transport.DNSTopologyPaired && value.PairRole == "primary" &&
		value.LocalIP == "192.0.2.10" && value.PeerIP == "192.0.2.11" &&
		len(value.Zones) == 1 && value.Zones[0].Domain == "s1-kill.test" &&
		value.Zones[0].ZoneType == "MASTER" && !value.Zones[0].Delete
}

// freshPairedPrimaryCell accepts the admitted V3 cell identities: pdns-switch,
// a journal-write boundary, paired-primary, peer-reachable.
func freshPairedPrimaryCell(cellID string) bool {
	const prefix, suffix = "pdns-switch__", "__paired-primary__peer-reachable"
	if !validCellID(cellID) || !strings.HasPrefix(cellID, prefix) || !strings.HasSuffix(cellID, suffix) {
		return false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(cellID, prefix), suffix)
	parts := strings.Split(middle, "__")
	if len(parts) != 2 || (parts[1] != "before-write" && parts[1] != "after-write") {
		return false
	}
	switch parts[0] {
	case "intent", "target-staged", "source-stopped", "target-started", "target-verified", "committed":
		return true
	}
	return false
}

type gateProbeResult struct {
	Schema            string `json:"schema"`
	CellID            string `json:"cell_id"`
	ManifestQualifier string `json:"manifest_qualifier"`
	Gate              string `json:"gate"`
	AgentError        string `json:"agent_error,omitempty"`
	Detail            string `json:"detail"`
}

// classifyGateProbe reads only the Agent's refusal text. Applied must never be
// true without a lease; it is reported as unknown, not as an open gate.
func classifyGateProbe(response transport.SwitchDNSEngineV1Response) (string, string) {
	switch {
	case response.Applied:
		return gateUnknown, "the Agent reported an applied switch without a lease; preserve the evidence"
	case response.Error == agentPDNSPrimaryPausedReason:
		return gateClosed, "gate closed in this build: the Agent refused the fresh paired PowerDNS primary with its pause reason before any lease or host effect"
	case response.Error == agentLeaseRequiredReason:
		return gateOpen, "the Agent admitted the manifest past its product gate and refused only the missing lease; nothing was changed"
	case response.Error == "":
		return gateUnknown, "the Agent returned neither a refusal nor a switch receipt"
	}
	return gateUnknown, "the Agent refused the manifest for another reason; the gate state is not established"
}

func runGateProbe(
	ctx context.Context,
	cellID string,
	request transport.SwitchDNSEngineV1Request,
	call rpcCallFunc,
) (gateProbeResult, error) {
	result := gateProbeResult{
		Schema: gateProbeSchema, CellID: cellID,
		ManifestQualifier: request.ManifestQualifier, Gate: gateUnknown,
	}
	if ctx == nil || call == nil ||
		request.ServiceMutationBinding != (transport.ServiceMutationBinding{}) {
		return result, errors.New("the gate probe must carry no mutation lease")
	}
	var response transport.SwitchDNSEngineV1Response
	if err := call(ctx, "Agent.SwitchDNSEngineV1", &request, &response); err != nil {
		result.Detail = "the gate probe RPC failed"
		return result, err
	}
	result.AgentError = response.Error
	result.Gate, result.Detail = classifyGateProbe(response)
	return result, nil
}

func runRPCGateProbeCommand(arguments []string) {
	flags := flag.NewFlagSet("rpc-gate-probe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scenarioPath := flags.String("scenario", "", "strict JSON scenario manifest")
	timeout := flags.Duration("timeout", controlTimeout, "bounded probe time")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*scenarioPath) == "" || *timeout <= 0 {
		usageError("rpc-gate-probe arguments are invalid")
	}
	cellID, driver, _, err := environmentIdentity()
	if err != nil {
		emitAndExit(triggerEvent{Event: "gate-probe-identity-rejected", Error: err.Error()}, exitUsage)
	}
	loaded, request, err := loadScenario(*scenarioPath, driver)
	if err != nil {
		emitAndExit(triggerEvent{Event: "gate-probe-scenario-rejected", Error: err.Error()}, exitUsage)
	}
	if !freshPairedPrimaryScenario(loaded) || !freshPairedPrimaryCell(cellID) {
		emitAndExit(triggerEvent{
			Event: "gate-probe-scenario-rejected",
			Error: "the gate probe applies only to the fresh paired PowerDNS primary cells",
		}, exitUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := runGateProbe(ctx, cellID, request, callProductionAgent)
	if err != nil {
		result.Detail = result.Detail + ": " + err.Error()
	}
	encoded, _ := json.Marshal(result)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	if err != nil || result.Gate == gateUnknown {
		os.Exit(exitUncertain)
	}
}

type unrelatedProbeResult struct {
	Schema      string `json:"schema"`
	RequestID   string `json:"request_id"`
	OwnerID     string `json:"owner_id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Begun       bool   `json:"begun"`
	BeginError  string `json:"begin_error,omitempty"`
	HoldCode    string `json:"hold_code,omitempty"`
	Finished    bool   `json:"finished"`
	FinalStatus string `json:"final_status,omitempty"`
	Error       string `json:"error,omitempty"`
}

func unrelatedProbeIdentity(cellID, baseRequestID string) (transport.ServiceMutationBeginRequest, error) {
	requestID := deriveNormalizationRequestIdentity(baseRequestID, "unrelated-begin/v1")
	ownerID, err := deterministicOwnerIdentity(cellID, requestID)
	if err != nil {
		return transport.ServiceMutationBeginRequest{}, err
	}
	return transport.ServiceMutationBeginRequest{
		RequestID: requestID, OwnerID: ownerID,
		Kind: unrelatedProbeKind, Target: unrelatedProbeTarget,
	}, nil
}

// runUnrelatedBegin returns (begun, error). begun=false with a nil error is a
// verified refusal of the unrelated lease (the hold is not DNS-only); any
// transport failure is an error (unknown). A begun lease is always finished.
func runUnrelatedBegin(
	ctx context.Context,
	begin transport.ServiceMutationBeginRequest,
	call rpcCallFunc,
) (unrelatedProbeResult, error) {
	result := unrelatedProbeResult{
		Schema: unrelatedProbeSchema, RequestID: begin.RequestID, OwnerID: begin.OwnerID,
		Kind: begin.Kind, Target: begin.Target,
	}
	if ctx == nil || call == nil || begin.Kind != unrelatedProbeKind ||
		begin.Target != unrelatedProbeTarget || begin.PackageName != "" || begin.Resume ||
		!validMutationIdentity(begin.RequestID) || !validMutationIdentity(begin.OwnerID) {
		return result, errors.New("invalid unrelated probe identity")
	}
	var status transport.ServiceMutationResponse
	if err := call(ctx, "Agent.ServiceMutationStatus",
		&transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &status); err != nil {
		return result, fmt.Errorf("inspect unrelated probe identity: %w", err)
	}
	if status.Job != nil {
		return result, errors.New("the unrelated probe identity already exists; it is never begun twice")
	}
	var started transport.ServiceMutationResponse
	beginCtx, cancel := context.WithTimeout(ctx, controlTimeout)
	err := call(beginCtx, "Agent.BeginServiceMutation", &begin, &started)
	cancel()
	if err != nil {
		return result, fmt.Errorf("begin unrelated probe: %w", err)
	}
	result.HoldCode = started.MutationHold
	if responseError(started) != nil {
		result.BeginError = strings.TrimSpace(started.ErrorCode + " " + started.Error)
		return result, nil
	}
	if err := validateRunningJob(started.Job, begin, true); err != nil {
		return result, fmt.Errorf("unrelated probe lease is not exact: %w", err)
	}
	result.Begun = true
	request := transport.ServiceMutationFinishRequest{
		RequestID: begin.RequestID, OwnerID: begin.OwnerID, Success: false,
		FailureCode: unrelatedProbeCode, Message: unrelatedProbeMessage,
	}
	var finished transport.ServiceMutationResponse
	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), controlTimeout)
	defer cancelFinish()
	if err := call(finishCtx, "Agent.FinishServiceMutation", &request, &finished); err != nil {
		return result, fmt.Errorf("finish unrelated probe: %w", err)
	}
	if err := responseError(finished); err != nil {
		return result, fmt.Errorf("finish unrelated probe: %w", err)
	}
	if finished.Job == nil || !jobIdentityMatches(finished.Job, begin) ||
		finished.Job.Status != mutationFailed {
		return result, errors.New("the unrelated probe lease has no exact terminal record")
	}
	result.Finished, result.FinalStatus = true, finished.Job.Status
	return result, nil
}

func runRPCUnrelatedBeginCommand(arguments []string) {
	flags := flag.NewFlagSet("rpc-unrelated-begin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Duration("timeout", 2*controlTimeout, "bounded probe time")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *timeout <= 0 {
		usageError("rpc-unrelated-begin arguments are invalid")
	}
	cellID, _, requestID, err := environmentIdentity()
	if err != nil {
		emitAndExit(triggerEvent{Event: "unrelated-begin-identity-rejected", Error: err.Error()}, exitUsage)
	}
	begin, err := unrelatedProbeIdentity(cellID, requestID)
	if err != nil {
		emitAndExit(triggerEvent{Event: "unrelated-begin-identity-rejected", Error: err.Error()}, exitUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := runUnrelatedBegin(ctx, begin, callProductionAgent)
	if err != nil {
		result.Error = err.Error()
	}
	encoded, _ := json.Marshal(result)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	switch {
	case err != nil:
		os.Exit(exitUncertain)
	case !result.Begun:
		os.Exit(exitDefiniteErr)
	}
}

// Zone lifecycle of the accepted fresh primary (register row 17). The child
// zone sits under the served member, so a deletion can be proved by the
// authoritative parent's negative answer on both servers.
type freshPrimaryZoneStep struct {
	name        string
	generation  int64
	serial      string
	edited      bool
	deleted     bool
	predecessor string
}

var freshPrimaryZoneSteps = []freshPrimaryZoneStep{
	{name: "add", generation: 1, serial: "2026092801"},
	{name: "edit", generation: 2, serial: "2026092802", edited: true, predecessor: "add"},
	{name: "delete", generation: 3, deleted: true, predecessor: "edit"},
	{name: "re-add", generation: 4, serial: "2026092803", predecessor: "delete"},
}

func freshPrimaryZoneStepNamed(name string) (freshPrimaryZoneStep, bool) {
	for _, step := range freshPrimaryZoneSteps {
		if step.name == name {
			return step, true
		}
	}
	return freshPrimaryZoneStep{}, false
}

func freshPrimaryZoneRecords(step freshPrimaryZoneStep) []transport.ZoneRecord {
	if step.deleted {
		return nil
	}
	domain := freshPrimaryZoneDomain
	records := []transport.ZoneRecord{
		{Name: domain, Type: "SOA", Content: "ns1.s1-kill.test hostmaster.s1-kill.test " + step.serial + " 10800 3600 604800 3600", TTL: 300},
		{Name: domain, Type: "NS", Content: "ns1.s1-kill.test", TTL: 300},
		{Name: "www." + domain, Type: "A", Content: "192.0.2.10", TTL: 300},
	}
	if step.edited {
		records = append(records, transport.ZoneRecord{Name: "changed." + domain, Type: "A", Content: "192.0.2.10", TTL: 300})
	}
	return records
}

type freshPrimaryStateEnvelope struct {
	Schema      string `json:"schema"`
	Acquisition struct {
		Schema            string `json:"schema"`
		Mode              string `json:"mode"`
		Engine            string `json:"engine"`
		EngineEpoch       int64  `json:"engine_epoch"`
		PairRole          string `json:"pair_role"`
		LocalIP           string `json:"pair_local_ip"`
		PeerIP            string `json:"pair_peer_ip"`
		SourceRevision    int64  `json:"source_revision"`
		ManifestQualifier string `json:"manifest_qualifier"`
		RequestID         string `json:"mutation_request_id"`
		OwnerID           string `json:"mutation_owner_id"`
	} `json:"acquisition"`
	Publication struct {
		Schema          string `json:"schema"`
		AcquisitionHash string `json:"acquisition_sha256"`
		CatalogSerial   uint32 `json:"primary_catalog_serial"`
	} `json:"publication"`
	NativeCatalog string `json:"native_catalog"`
}

func freshPrimaryZoneIdentity(stepName string, receipt identityReceipt) (
	transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, error,
) {
	step, ok := freshPrimaryZoneStepNamed(stepName)
	if !ok {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{},
			fmt.Errorf("unsupported zone lifecycle step %q", stepName)
	}
	records := freshPrimaryZoneRecords(step)
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, step.generation, freshPrimaryZoneDomain,
		step.deleted, "MASTER", records,
	)
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	requestID := deriveNormalizationRequestIdentity(receipt.RequestID, "pdns-primary-zone-v3/"+step.name)
	ownerID, err := deterministicOwnerIdentity(receipt.CellID, requestID)
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	request := transport.SyncDNSZoneV3Request{
		ServiceMutationBinding: transport.ServiceMutationBinding{
			MutationRequestID: requestID, MutationOwnerID: ownerID,
		},
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: 1,
		DesiredGeneration: step.generation, Domain: freshPrimaryZoneDomain,
		Delete: step.deleted, ZoneType: commitment.ZoneType, Records: records,
	}
	begin := transport.ServiceMutationBeginRequest{
		RequestID: requestID, OwnerID: ownerID,
		Kind: mutationKindDNSZoneSync, Target: freshPrimaryZoneDomain,
		PackageName: commitment.Qualifier,
	}
	return request, begin, nil
}

// validateFreshPrimaryZoneSource binds the lifecycle to the accepted primary:
// the measured switch receipt, its exact scenario and the V3 state document
// that switch published.
func validateFreshPrimaryZoneSource(
	source scenario, switchRequest transport.SwitchDNSEngineV1Request,
	receipt identityReceipt, state freshPrimaryStateEnvelope,
) error {
	if receipt.Schema != identityReceiptSchema || !freshPairedPrimaryCell(receipt.CellID) ||
		receipt.Driver != "pdns-switch" || receipt.SourceFixture != "uninitialized" ||
		!validMutationIdentity(receipt.RequestID) || !validMutationIdentity(receipt.OwnerID) ||
		!freshPairedPrimaryScenario(source) ||
		receipt.ManifestQualifier != switchRequest.ManifestQualifier {
		return errors.New("zone lifecycle source is not the measured fresh paired PowerDNS primary")
	}
	a := state.Acquisition
	if state.Schema != freshPrimaryStateSchema || state.NativeCatalog != freshPrimaryNativeMarker ||
		a.Schema != "celikpanel-dns-engine-acquisition/v1" || a.Mode != "switch" ||
		a.Engine != "pdns" || a.EngineEpoch != 1 || a.PairRole != "primary" ||
		a.LocalIP != "192.0.2.10" || a.PeerIP != "192.0.2.11" || a.SourceRevision != 0 ||
		a.ManifestQualifier != receipt.ManifestQualifier || a.RequestID != receipt.RequestID ||
		a.OwnerID != receipt.OwnerID ||
		state.Publication.Schema != "celikpanel-dns-engine-publication/v1" ||
		state.Publication.CatalogSerial <= 1 {
		return errors.New("the active DNS state is not the V3 fresh paired primary of this request")
	}
	return nil
}

func freshPrimaryZonePreflight(scenarioPath, identityPath, stepName string) (
	transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, identityReceipt, error,
) {
	fail := func(err error) (transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, identityReceipt, error) {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, identityReceipt{}, err
	}
	receipt, err := readIdentityReceipt(identityPath)
	if err != nil {
		return fail(err)
	}
	marker, err := os.ReadFile(freshPrimaryZoneMarker)
	if err != nil || string(marker) != "schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id="+receipt.CellID+"\nnode=debian13\n" {
		return fail(errors.New("not the exact disposable Debian fixture of this cell"))
	}
	if _, err := os.Lstat(freshPrimaryZoneJournal); !errors.Is(err, os.ErrNotExist) {
		return fail(errors.New("a DNS switch journal is present or unreadable; the switch is not retired"))
	}
	source, switchRequest, err := loadScenario(scenarioPath, "pdns-switch")
	if err != nil {
		return fail(err)
	}
	raw, err := os.ReadFile(freshPrimaryZoneState)
	if err != nil {
		return fail(err)
	}
	var state freshPrimaryStateEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return fail(fmt.Errorf("decode DNS state: %w", err))
	}
	if err := validateFreshPrimaryZoneSource(source, switchRequest, receipt, state); err != nil {
		return fail(err)
	}
	request, begin, err := freshPrimaryZoneIdentity(stepName, receipt)
	if err != nil {
		return fail(err)
	}
	return request, begin, receipt, nil
}

func freshPrimaryZonePhase(state string, begin transport.ServiceMutationBeginRequest) string {
	return "commit/dns-zone-sync/v3/" + state + "/" + begin.RequestID + "/" + begin.Target + "/" + begin.PackageName
}

type freshPrimaryZoneResult struct {
	Schema     string `json:"schema"`
	CellID     string `json:"cell_id"`
	Step       string `json:"step"`
	Domain     string `json:"domain"`
	Generation int64  `json:"generation"`
	Delete     bool   `json:"delete"`
	RequestID  string `json:"request_id"`
	OwnerID    string `json:"owner_id"`
	Qualifier  string `json:"qualifier"`
	Outcome    string `json:"outcome"`
	JobStatus  string `json:"job_status,omitempty"`
	JobPhase   string `json:"job_phase,omitempty"`
	JobCode    string `json:"job_error_code,omitempty"`
	JobMessage string `json:"job_error_message,omitempty"`
	Heartbeats int    `json:"heartbeats"`
	Error      string `json:"error,omitempty"`
}

func requirePublishedPredecessor(
	ctx context.Context, predecessor *transport.ServiceMutationBeginRequest, call rpcCallFunc,
) error {
	if predecessor == nil {
		return nil
	}
	var previous transport.ServiceMutationResponse
	if err := call(ctx, "Agent.ServiceMutationStatus",
		&transport.ServiceMutationStatusRequest{RequestID: predecessor.RequestID}, &previous); err != nil {
		return err
	}
	if err := responseError(previous); err != nil {
		return err
	}
	if err := validateSucceededJobAtPhase(previous.Job, *predecessor,
		freshPrimaryZonePhase("published", *predecessor)); err != nil {
		return errors.New("the previous lifecycle step is not terminal published; no new zone request is begun")
	}
	return nil
}

func observeZoneJob(
	ctx context.Context, begin transport.ServiceMutationBeginRequest, call rpcCallFunc,
	result *freshPrimaryZoneResult,
) (*transport.ServiceMutationJob, error) {
	statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlTimeout)
	defer cancel()
	var observed transport.ServiceMutationResponse
	if err := call(statusCtx, "Agent.ServiceMutationStatus",
		&transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &observed); err != nil {
		return nil, err
	}
	if err := responseError(observed); err != nil {
		return nil, err
	}
	if observed.Job == nil || !jobIdentityMatches(observed.Job, begin) {
		return nil, errors.New("ledger status differs from the exact zone operation")
	}
	result.JobStatus, result.JobPhase = observed.Job.Status, observed.Job.Phase
	result.JobCode, result.JobMessage = observed.Job.ErrorCode, observed.Job.ErrorMessage
	return observed.Job, nil
}

func runFreshPrimaryZoneStep(
	ctx context.Context,
	request transport.SyncDNSZoneV3Request,
	begin transport.ServiceMutationBeginRequest,
	predecessor *transport.ServiceMutationBeginRequest,
	call rpcCallFunc,
	result *freshPrimaryZoneResult,
) error {
	if ctx == nil || call == nil || result == nil ||
		!validMutationIdentity(begin.RequestID) || !validMutationIdentity(begin.OwnerID) ||
		request.MutationRequestID != begin.RequestID || request.MutationOwnerID != begin.OwnerID ||
		request.Engine != transport.DNSEnginePowerDNS || request.EngineEpoch != 1 ||
		request.Domain != freshPrimaryZoneDomain || begin.Kind != mutationKindDNSZoneSync ||
		begin.Target != freshPrimaryZoneDomain || begin.PackageName == "" || begin.Resume {
		return errors.New("exact zone lifecycle identity differs")
	}
	if (request.DesiredGeneration == 1) != (predecessor == nil) {
		return errors.New("exact predecessor identity is missing or unexpected")
	}
	if err := requirePublishedPredecessor(ctx, predecessor, call); err != nil {
		result.Outcome = "refused_predecessor"
		return err
	}
	var existing transport.ServiceMutationResponse
	if err := call(ctx, "Agent.ServiceMutationStatus",
		&transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &existing); err != nil {
		return err
	}
	if existing.Job != nil {
		result.Outcome = "refused_existing"
		result.JobStatus, result.JobPhase = existing.Job.Status, existing.Job.Phase
		return errors.New("this lifecycle step already has a durable job; it is never begun twice (use the recover command for a pending deletion)")
	}
	var started transport.ServiceMutationResponse
	if err := call(ctx, "Agent.BeginServiceMutation", &begin, &started); err != nil {
		return err
	}
	if err := responseError(started); err != nil {
		result.Outcome = "refused"
		return err
	}
	if err := validateRunningJob(started.Job, begin, true); err != nil {
		return err
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	done := startHeartbeat(heartbeatCtx, begin, heartbeatIntervalDefault, call)
	var published transport.SyncDNSZoneV3Response
	callErr := call(ctx, "Agent.SyncDNSZoneV3", &request, &published)
	stopHeartbeat()
	heartbeat := <-done
	result.Heartbeats = heartbeat.count
	job, statusErr := observeZoneJob(ctx, begin, call, result)
	if statusErr != nil {
		result.Outcome = "unknown"
		return statusErr
	}
	if callErr != nil || heartbeat.err != nil {
		result.Outcome = "unknown"
		return errors.Join(callErr, heartbeat.err)
	}
	if published.Error != "" {
		result.Outcome = "refused"
		return errors.New(published.Error)
	}
	if published.RecoveryPending && !published.Synced && job.Status == "pending" &&
		job.Phase == freshPrimaryZonePhase("propagation-pending", begin) {
		result.Outcome = "pending_exact_operation"
		return nil
	}
	if !published.Synced || published.RecoveryPending || published.Engine != request.Engine ||
		published.EngineEpoch != request.EngineEpoch ||
		published.AppliedGeneration != request.DesiredGeneration {
		result.Outcome = "mixed"
		return errors.New("zone V3 response is not the exact published generation")
	}
	terminal, err := finishMutationWithResponse(ctx, begin, true, call)
	if err != nil {
		result.Outcome = "unknown"
		return err
	}
	if err := validateSucceededJobAtPhase(terminal, begin, freshPrimaryZonePhase("published", begin)); err != nil {
		result.Outcome = "unknown"
		return err
	}
	result.JobStatus, result.JobPhase = terminal.Status, terminal.Phase
	result.Outcome = "verified_published"
	return nil
}

// recoverFreshPrimaryZoneDeletion resumes only the retained pending deletion.
// It never calls SyncDNSZoneV3 and never creates another identity.
func recoverFreshPrimaryZoneDeletion(
	ctx context.Context,
	request transport.SyncDNSZoneV3Request,
	begin transport.ServiceMutationBeginRequest,
	call rpcCallFunc,
	result *freshPrimaryZoneResult,
) error {
	if ctx == nil || call == nil || result == nil || !request.Delete ||
		request.DesiredGeneration != 3 || request.Domain != freshPrimaryZoneDomain ||
		request.Engine != transport.DNSEnginePowerDNS || begin.Resume ||
		request.MutationRequestID != begin.RequestID || request.MutationOwnerID != begin.OwnerID ||
		begin.Kind != mutationKindDNSZoneSync || begin.Target != freshPrimaryZoneDomain {
		return errors.New("invalid exact zone deletion recovery identity")
	}
	var before transport.ServiceMutationResponse
	if err := call(ctx, "Agent.ServiceMutationStatus",
		&transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &before); err != nil {
		return err
	}
	if err := responseError(before); err != nil {
		return err
	}
	if !exactPendingDeletionJob(before.Job, begin) {
		result.Outcome = "refused_not_pending"
		return errors.New("the exact deletion is not pending at its propagation phase; nothing was resumed")
	}
	resume := begin
	resume.Resume = true
	var started transport.ServiceMutationResponse
	if err := call(ctx, "Agent.BeginServiceMutation", &resume, &started); err != nil {
		return err
	}
	if err := responseError(started); err != nil {
		result.Outcome = "refused"
		return err
	}
	if err := validateRunningJob(started.Job, begin, false); err != nil ||
		started.Job.Phase != freshPrimaryZonePhase("recovering", begin) {
		return errors.New("resumed deletion lacks its exact recovering phase and lease")
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	done := startHeartbeat(heartbeatCtx, begin, heartbeatIntervalDefault, call)
	recovery := transport.RecoverDNSZoneV3Request{
		ServiceMutationBinding: request.ServiceMutationBinding,
		Domain:                 freshPrimaryZoneDomain, Qualifier: begin.PackageName,
	}
	var recovered transport.RecoverDNSZoneV3Response
	callErr := call(ctx, "Agent.RecoverDNSZoneV3", &recovery, &recovered)
	stopHeartbeat()
	heartbeat := <-done
	result.Heartbeats = heartbeat.count
	job, statusErr := observeZoneJob(ctx, begin, call, result)
	if statusErr != nil {
		result.Outcome = "unknown"
		return statusErr
	}
	if callErr != nil {
		result.Outcome = "unknown"
		return callErr
	}
	if recovered.Error != "" {
		result.Outcome = "refused"
		return errors.New("zone V3 recovery was refused; the exact job is preserved")
	}
	if recovered.RecoveryPending && !recovered.Recovered && exactPendingDeletionJob(job, begin) {
		if heartbeat.err != nil {
			result.Outcome = "unknown"
			return errors.New("recovery heartbeat outcome unknown; the exact job is preserved")
		}
		result.Outcome = "pending_exact_operation"
		return nil
	}
	if recovered.Recovered && !recovered.RecoveryPending {
		if err := validateSucceededJobAtPhase(job, begin, freshPrimaryZonePhase("published", begin)); err != nil {
			result.Outcome = "unknown"
			return err
		}
		result.Outcome = "verified_published"
		return nil
	}
	result.Outcome = "unknown"
	return errors.New("zone V3 recovery response or durable status is mixed")
}

func runRPCFreshPrimaryZoneCommand(arguments []string, recover bool) {
	name := "rpc-pdns-primary-zone-v3"
	if recover {
		name += "-recover"
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scenarioPath := flags.String("scenario", "", "exact measured scenario")
	identityPath := flags.String("identity-receipt", "", "measured switch identity receipt")
	stepName := flags.String("step", "", "add, edit, delete or re-add")
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded zone operation")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *timeout <= 0 ||
		*scenarioPath == "" || *identityPath == "" ||
		(recover && *stepName != "delete") {
		usageError(name + " arguments are invalid")
	}
	request, begin, receipt, err := freshPrimaryZonePreflight(*scenarioPath, *identityPath, *stepName)
	if err != nil {
		emitAndExit(triggerEvent{Event: name + "-preflight-refused", Error: err.Error()}, exitUsage)
	}
	step, _ := freshPrimaryZoneStepNamed(*stepName)
	result := freshPrimaryZoneResult{
		Schema: freshPrimaryZoneSchema, CellID: receipt.CellID, Step: step.name,
		Domain: freshPrimaryZoneDomain, Generation: step.generation, Delete: step.deleted,
		RequestID: begin.RequestID, OwnerID: begin.OwnerID, Qualifier: begin.PackageName,
	}
	var predecessor *transport.ServiceMutationBeginRequest
	if step.predecessor != "" {
		_, prior, identityErr := freshPrimaryZoneIdentity(step.predecessor, receipt)
		if identityErr != nil {
			emitAndExit(triggerEvent{Event: name + "-predecessor-refused", Error: identityErr.Error()}, exitUsage)
		}
		predecessor = &prior
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if recover {
		err = recoverFreshPrimaryZoneDeletion(ctx, request, begin, callProductionAgent, &result)
	} else {
		err = runFreshPrimaryZoneStep(ctx, request, begin, predecessor, callProductionAgent, &result)
	}
	if err != nil {
		result.Error = err.Error()
	}
	encoded, _ := json.Marshal(result)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	if err != nil || result.Outcome != "verified_published" {
		os.Exit(exitUncertain)
	}
}
