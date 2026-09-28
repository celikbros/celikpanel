package main

// A single exact-cell fixture path for native BIND-primary/PowerDNS-secondary
// V3 edit, delete and re-add. Each invocation accepts one operation only.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	pdnsPeerV3Cell     = "pdns-switch__intent__after-write__paired-primary__peer-reachable"
	pdnsPeerV3Zone     = "s1-kill.test"
	pdnsPeerV3Source   = "/var/lib/celikpanel-dns-kill-matrix/source-setup-bind.json"
	pdnsPeerV3Identity = "/var/lib/celikpanel-dns-kill-matrix/source-setup-bind-identity.json"
	pdnsPeerV3State    = "/var/lib/celikpanel-agent-private/dns-engine-state.json"
	pdnsPeerV3Journal  = "/var/lib/celikpanel-agent-private/dns-engine-switch-journal.json"
)

type pdnsPeerV3StateEnvelope struct {
	Schema      string `json:"schema"`
	Acquisition struct {
		Schema            string `json:"schema"`
		Mode              string `json:"mode"`
		Engine            string `json:"engine"`
		EngineEpoch       int64  `json:"engine_epoch"`
		PairRole          string `json:"pair_role"`
		LocalIP           string `json:"pair_local_ip"`
		PeerIP            string `json:"pair_peer_ip"`
		ManifestQualifier string `json:"manifest_qualifier"`
		RequestID         string `json:"mutation_request_id"`
		OwnerID           string `json:"mutation_owner_id"`
	} `json:"acquisition"`
	Publication struct {
		Schema        string `json:"schema"`
		CatalogSerial uint32 `json:"primary_catalog_serial"`
	} `json:"publication"`
}

type pdnsPeerV3Result struct {
	Schema     string `json:"schema"`
	CellID     string `json:"cell_id"`
	Step       string `json:"step"`
	RequestID  string `json:"request_id"`
	OwnerID    string `json:"owner_id"`
	Qualifier  string `json:"qualifier"`
	Outcome    string `json:"outcome"`
	JobStatus  string `json:"job_status"`
	JobPhase   string `json:"job_phase"`
	Heartbeats int    `json:"heartbeats"`
	Error      string `json:"error,omitempty"`
}

func pdnsPeerV3Request(step string, source scenario, receipt identityReceipt, state pdnsPeerV3StateEnvelope) (
	transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, error,
) {
	bad := func() (transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, error) {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, errors.New("fixture source or native BIND state differs from the exact paired cell")
	}
	if receipt.Schema != identityReceiptSchema || receipt.CellID != pdnsPeerV3Cell ||
		receipt.Driver != "bind" || receipt.SourceFixture != "uninitialized" ||
		!validMutationIdentity(receipt.RequestID) || !validMutationIdentity(receipt.OwnerID) ||
		source.Schema != scenarioSchema || source.Driver != "bind" ||
		source.SourceFixture != "uninitialized" || source.Mode != "switch" ||
		source.SourceEngine != "" || source.TargetEngine != transport.DNSEngineBIND ||
		source.SourceEpoch != 0 || source.TargetEpoch != 1 ||
		source.Topology != transport.DNSTopologyPaired || source.PairRole != "primary" ||
		source.LocalIP != "192.0.2.10" || source.PeerIP != "192.0.2.11" ||
		len(source.Zones) != 1 || source.Zones[0].Domain != pdnsPeerV3Zone ||
		source.Zones[0].DesiredGeneration != 1 || source.Zones[0].Delete ||
		source.Zones[0].ZoneType != "NATIVE" ||
		state.Schema != "celikpanel-dns-engine-state/v2" ||
		state.Acquisition.Schema != "celikpanel-dns-engine-acquisition/v1" ||
		state.Acquisition.Mode != "switch" || state.Acquisition.Engine != "bind" ||
		state.Acquisition.EngineEpoch != 1 || state.Acquisition.PairRole != "primary" ||
		state.Acquisition.LocalIP != "192.0.2.10" || state.Acquisition.PeerIP != "192.0.2.11" ||
		state.Acquisition.ManifestQualifier != receipt.ManifestQualifier ||
		state.Acquisition.RequestID != receipt.RequestID ||
		state.Acquisition.OwnerID != receipt.OwnerID ||
		state.Publication.Schema != "celikpanel-dns-engine-publication/v1" ||
		state.Publication.CatalogSerial == 0 {
		return bad()
	}
	expectedRecords := []transport.ZoneRecord{
		{Name: pdnsPeerV3Zone, Type: "SOA", Content: "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600", TTL: 3600},
		{Name: pdnsPeerV3Zone, Type: "NS", Content: "ns1.s1-kill.test", TTL: 3600},
		{Name: "ns1.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300},
		{Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300},
		{Name: pdnsPeerV3Zone, Type: "NS", Content: "ns2.s1-kill.test", TTL: 3600},
		{Name: "ns2.s1-kill.test", Type: "A", Content: "192.0.2.11", TTL: 300},
	}
	if !reflect.DeepEqual(source.Zones[0].Records, expectedRecords) {
		return bad()
	}
	generation := int64(0)
	address := ""
	serial := ""
	deletion := false
	switch step {
	case "edit":
		generation, address, serial = 2, "192.0.2.12", "2026083102"
	case "delete":
		generation, deletion = 3, true
	case "add":
		generation, address, serial = 4, "192.0.2.13", "2026083103"
	default:
		return bad()
	}
	var records []transport.ZoneRecord
	if !deletion {
		records = append([]transport.ZoneRecord(nil), source.Zones[0].Records...)
		soa, www := 0, 0
		for index := range records {
			record := &records[index]
			if record.Name == pdnsPeerV3Zone && record.Type == "SOA" {
				if record.Content != "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600" {
					return bad()
				}
				record.Content = "ns1.s1-kill.test hostmaster.s1-kill.test " + serial + " 10800 3600 604800 3600"
				soa++
			}
			if record.Name == "www.s1-kill.test" && record.Type == "A" {
				if record.Content != "192.0.2.10" {
					return bad()
				}
				record.Content = address
				www++
			}
		}
		if soa != 1 || www != 1 {
			return bad()
		}
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 1, generation, pdnsPeerV3Zone, deletion, "NATIVE", records,
	)
	if err != nil {
		return bad()
	}
	requestID := deriveNormalizationRequestIdentity(receipt.RequestID, "pdns-peer-v3/"+step)
	ownerID, err := deterministicOwnerIdentity(pdnsPeerV3Cell, requestID)
	if err != nil {
		return bad()
	}
	request := transport.SyncDNSZoneV3Request{
		ServiceMutationBinding: transport.ServiceMutationBinding{
			MutationRequestID: requestID, MutationOwnerID: ownerID,
		},
		Engine: transport.DNSEngineBIND, EngineEpoch: 1,
		DesiredGeneration: generation, Domain: pdnsPeerV3Zone,
		Delete: deletion, ZoneType: "NATIVE", Records: records,
	}
	begin := transport.ServiceMutationBeginRequest{
		RequestID: requestID, OwnerID: ownerID,
		Kind: mutationKindDNSZoneSync, Target: pdnsPeerV3Zone,
		PackageName: commitment.Qualifier,
	}
	return request, begin, nil
}

func pdnsPeerV3Preflight(step string) (transport.SyncDNSZoneV3Request, transport.ServiceMutationBeginRequest, error) {
	marker, err := os.ReadFile("/etc/celikpanel-dns-kill-matrix")
	if err != nil || string(marker) != "schema=celikpanel/dns-kill-fixture-plan/v1\ncell_id="+pdnsPeerV3Cell+"\nnode=debian13\n" {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, errors.New("not the exact disposable Debian DNS fixture")
	}
	if _, err := os.Lstat(pdnsPeerV3Journal); !errors.Is(err, os.ErrNotExist) {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, errors.New("DNS switch journal is present or unavailable")
	}
	if exec.Command("systemctl", "is-active", "--quiet", "named.service").Run() != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, errors.New("native BIND is not active")
	}
	source, _, err := loadScenario(pdnsPeerV3Source, "bind")
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	receipt, err := readIdentityReceipt(pdnsPeerV3Identity)
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	raw, err := os.ReadFile(pdnsPeerV3State)
	if err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	var state pdnsPeerV3StateEnvelope
	if err := json.Unmarshal(raw, &state); err != nil {
		return transport.SyncDNSZoneV3Request{}, transport.ServiceMutationBeginRequest{}, err
	}
	return pdnsPeerV3Request(step, source, receipt, state)
}

func runRPCPDNSPeerV3Command(arguments []string) {
	flags := flag.NewFlagSet("rpc-pdns-peer-v3", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	step := flags.String("step", "", "edit, delete, or add exact native member")
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded V3 operation")
	if flags.Parse(arguments) != nil || flags.NArg() != 0 || *timeout <= 0 {
		usageError("rpc-pdns-peer-v3 arguments are invalid")
	}
	request, begin, err := pdnsPeerV3Preflight(*step)
	if err != nil {
		emitAndExit(triggerEvent{Event: "pdns-peer-v3-preflight-refused", Error: err.Error()}, exitUsage)
	}
	result := pdnsPeerV3Result{Schema: "celikpanel/native-pdns-peer-v3/v1", CellID: pdnsPeerV3Cell,
		Step: *step, RequestID: begin.RequestID, OwnerID: begin.OwnerID, Qualifier: begin.PackageName}
	var predecessor *transport.ServiceMutationBeginRequest
	if *step == "delete" || *step == "add" {
		previous := "edit"
		if *step == "add" {
			previous = "delete"
		}
		_, prior, priorErr := pdnsPeerV3Preflight(previous)
		if priorErr != nil {
			emitAndExit(triggerEvent{Event: "pdns-peer-v3-predecessor-refused", Error: priorErr.Error()}, exitUsage)
		}
		predecessor = &prior
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	outcome, job, hb, operationErr := runRPCPDNSPeerV3(ctx, *step, request, begin, predecessor, callProductionAgent)
	result.Outcome, result.Heartbeats = outcome, hb
	if job != nil {
		result.JobStatus, result.JobPhase = job.Status, job.Phase
	}
	if operationErr != nil {
		result.Error = operationErr.Error()
	}
	encoded, _ := json.Marshal(result)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	if operationErr != nil || outcome != "verified_published" {
		os.Exit(exitUncertain)
	}
}

func runRPCPDNSPeerV3(ctx context.Context, step string, request transport.SyncDNSZoneV3Request,
	begin transport.ServiceMutationBeginRequest, predecessor *transport.ServiceMutationBeginRequest,
	call rpcCallFunc) (string, *transport.ServiceMutationJob, int, error) {
	if ctx == nil || call == nil || begin.RequestID == "" || begin.OwnerID == "" ||
		request.MutationRequestID != begin.RequestID || request.MutationOwnerID != begin.OwnerID ||
		request.Engine != transport.DNSEngineBIND || request.EngineEpoch != 1 ||
		request.Domain != pdnsPeerV3Zone || begin.Kind != mutationKindDNSZoneSync ||
		begin.Target != pdnsPeerV3Zone || begin.PackageName == "" {
		return "", nil, 0, errors.New("exact V3 operation identity differs")
	}
	if (step == "edit") != (predecessor == nil) {
		return "", nil, 0, errors.New("exact predecessor identity is missing or unexpected")
	}
	if predecessor != nil {
		var previous transport.ServiceMutationResponse
		if err := call(ctx, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{
			RequestID: predecessor.RequestID}, &previous); err != nil {
			return "", nil, 0, err
		}
		if err := responseError(previous); err != nil {
			return "", previous.Job, 0, err
		}
		phase := "commit/dns-zone-sync/v3/published/" + predecessor.RequestID + "/" + pdnsPeerV3Zone + "/" + predecessor.PackageName
		if err := validateSucceededJobAtPhase(previous.Job, *predecessor, phase); err != nil {
			return "", previous.Job, 0, errors.New("previous exact V3 operation is not terminal published")
		}
	}
	var started transport.ServiceMutationResponse
	if err := call(ctx, "Agent.BeginServiceMutation", &begin, &started); err != nil {
		return "", nil, 0, err
	}
	if err := responseError(started); err != nil {
		return "", started.Job, 0, err
	}
	if err := validateRunningJob(started.Job, begin, true); err != nil {
		return "", started.Job, 0, err
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	done := startHeartbeat(heartbeatCtx, begin, heartbeatIntervalDefault, call)
	var published transport.SyncDNSZoneV3Response
	callErr := call(ctx, "Agent.SyncDNSZoneV3", &request, &published)
	stopHeartbeat()
	heartbeat := <-done
	statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlTimeout)
	defer cancel()
	var observed transport.ServiceMutationResponse
	statusErr := call(statusCtx, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: begin.RequestID}, &observed)
	if statusErr != nil {
		return "unknown", nil, heartbeat.count, statusErr
	}
	if err := responseError(observed); err != nil {
		return "unknown", observed.Job, heartbeat.count, err
	}
	if observed.Job == nil || !jobIdentityMatches(observed.Job, begin) {
		return "unknown", observed.Job, heartbeat.count, errors.New("ledger status differs from exact V3 operation")
	}
	if callErr != nil || heartbeat.err != nil {
		return "unknown", observed.Job, heartbeat.count, errors.Join(callErr, heartbeat.err)
	}
	if published.Error != "" {
		return "refused", observed.Job, heartbeat.count, errors.New(published.Error)
	}
	pending := "commit/dns-zone-sync/v3/propagation-pending/" + begin.RequestID + "/" + pdnsPeerV3Zone + "/" + begin.PackageName
	if published.RecoveryPending && !published.Synced && observed.Job.Status == "pending" && observed.Job.Phase == pending {
		return "pending_exact_operation", observed.Job, heartbeat.count, nil
	}
	if !published.Synced || published.RecoveryPending || published.Engine != request.Engine ||
		published.EngineEpoch != request.EngineEpoch || published.AppliedGeneration != request.DesiredGeneration {
		return "mixed", observed.Job, heartbeat.count, errors.New("V3 response is not exact published generation")
	}
	terminal, err := finishMutationWithResponse(ctx, begin, true, call)
	if err != nil {
		return "unknown", terminal, heartbeat.count, err
	}
	phase := "commit/dns-zone-sync/v3/published/" + begin.RequestID + "/" + pdnsPeerV3Zone + "/" + begin.PackageName
	if err := validateSucceededJobAtPhase(terminal, begin, phase); err != nil {
		return "unknown", terminal, heartbeat.count, err
	}
	return "verified_published", terminal, heartbeat.count, nil
}
