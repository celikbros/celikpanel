//go:build linux

// release-recovery-seed is a disposable-guest fixture, not an installed-panel
// administration command. It exercises the old Agent's authenticated producers;
// it never writes DNS state, ownership receipts, configuration or license data.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/rpc"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

const (
	markerPath     = "/etc/celikpanel-release-recovery-lab"
	markerSchema   = "celikpanel-release-recovery-lab/v1"
	stateRoot      = "/var/lib/celikpanel-agent-private"
	controlTimeout = 10 * time.Second
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,95}$`)

type labMarker struct {
	Schema string `json:"schema"`
	Nonce  string `json:"nonce"`
	VMUUID string `json:"vm_uuid"`
	CellID string `json:"cell_id"`
	Node   string `json:"node"`
}

type seedEvent struct {
	Schema             string `json:"schema"`
	Event              string `json:"event"`
	CellID             string `json:"cell_id,omitempty"`
	Node               string `json:"node,omitempty"`
	Zone               string `json:"zone,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	OwnerID            string `json:"owner_id,omitempty"`
	Phase              string `json:"phase,omitempty"`
	Generation         string `json:"generation,omitempty"`
	CatalogSerial      uint64 `json:"catalog_serial"`
	OwnershipSHA256    string `json:"ownership_sha256,omitempty"`
	OwnershipUnchanged bool   `json:"ownership_unchanged"`
	Detail             string `json:"detail,omitempty"`
}

func emit(event seedEvent) {
	event.Schema = "celikpanel-release-recovery-seed/v1"
	_ = json.NewEncoder(os.Stdout).Encode(event)
}

func main() {
	nonce := flag.String("nonce", "", "exact disposable guest nonce (64 lowercase hex characters)")
	zone := flag.String("zone", "recovery-fixture.test", "canonical fixture zone below .test")
	timeout := flag.Duration("timeout", 15*time.Minute, "maximum seeding duration (1m to 30m)")
	flag.Parse()
	if flag.NArg() != 0 || *timeout < time.Minute || *timeout > 30*time.Minute {
		fmt.Fprintln(os.Stderr, "invalid fixture arguments")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := seed(ctx, *nonce, *zone); err != nil {
		emit(seedEvent{Event: "stopped", Detail: err.Error()})
		os.Exit(1)
	}
}

// readProtected opens the final object without following a symlink and rejects
// changes during the read. The fixture's root-only gate runs before these reads.
func readProtected(path string, limit int64, marker bool) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var before, after unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Nlink != 1 ||
		before.Mode&0o022 != 0 || before.Size <= 0 || before.Size > limit {
		return nil, errors.New("fixture input has unsafe type, ownership, links, permissions or size")
	}
	if marker && (before.Gid != 0 || before.Mode&0o7777 != 0o444) {
		return nil, errors.New("guest marker must be root:root mode 0444")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size ||
		before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid ||
		before.Nlink != after.Nlink || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		int64(len(raw)) != before.Size {
		return nil, errors.New("fixture input changed while reading")
	}
	return raw, nil
}

func parseMarker(raw []byte, nonce, uuid, vendor string) (labMarker, error) {
	var marker labMarker
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return marker, errors.New("invalid guest marker JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return marker, errors.New("trailing guest marker content")
	}
	if marker.Schema != markerSchema || !hex64.MatchString(nonce) || marker.Nonce != nonce ||
		!uuidPattern.MatchString(marker.VMUUID) || marker.VMUUID != strings.ToLower(strings.TrimSpace(uuid)) ||
		strings.TrimSpace(vendor) != "QEMU" || !labelPattern.MatchString(marker.CellID) || !labelPattern.MatchString(marker.Node) {
		return marker, errors.New("guest marker, nonce, QEMU identity or DMI UUID does not match")
	}
	return marker, nil
}

func guard(nonce string) (labMarker, error) {
	if os.Geteuid() != 0 {
		return labMarker{}, errors.New("fixture requires root inside its marked QEMU guest")
	}
	if os.Getenv("CELIKPANEL_AGENT_SOCKET") != "" || os.Getenv("CELIKPANEL_AGENT_TOKEN_FILE") != "" ||
		transport.AgentSocketPath() != "/run/celikpanel/agent.sock" || transport.AgentTokenPath() != "/etc/celikpanel/agent.token" {
		return labMarker{}, errors.New("agent endpoint and token overrides are forbidden in this fixture")
	}
	raw, err := readProtected(markerPath, 4096, true)
	if err != nil {
		return labMarker{}, fmt.Errorf("disposable guest marker: %w", err)
	}
	uuid, err := os.ReadFile("/sys/class/dmi/id/product_uuid")
	if err != nil {
		return labMarker{}, errors.New("cannot verify guest DMI UUID")
	}
	vendor, err := os.ReadFile("/sys/class/dmi/id/sys_vendor")
	if err != nil {
		return labMarker{}, errors.New("cannot verify QEMU guest vendor")
	}
	return parseMarker(raw, nonce, string(uuid), string(vendor))
}

type rpcCall func(context.Context, string, any, any) error

func callAgent(ctx context.Context, method string, request, response any) error {
	client, err := transport.ConnectAgentContext(ctx)
	if err != nil {
		return errors.New("authenticated local Agent connection failed")
	}
	defer client.Close()
	done := client.Go(method, request, response, make(chan *rpc.Call, 1)).Done
	select {
	case result := <-done:
		if result.Error != nil {
			return fmt.Errorf("Agent RPC %s failed; outcome may be unknown", method)
		}
		return nil
	case <-ctx.Done():
		_ = client.Close()
		<-done
		return fmt.Errorf("Agent RPC %s interrupted; outcome is unknown", method)
	}
}

func identity(nonce, cell, purpose, role string) string {
	digest := sha256.Sum256([]byte("celikpanel/release-recovery-seed/v1\x00" + nonce + "\x00" + cell + "\x00" + purpose + "\x00" + role))
	return hex.EncodeToString(digest[:16])
}

func exactJob(job *transport.ServiceMutationJob, begin transport.ServiceMutationBeginRequest) bool {
	return job != nil && job.RequestID == begin.RequestID && job.OwnerID == begin.OwnerID &&
		job.Kind == begin.Kind && job.Target == begin.Target && job.PackageName == begin.PackageName
}

func responseOK(response transport.ServiceMutationResponse) error {
	if response.ErrorCode != "" || response.Error != "" || response.MutationHold != "" {
		return errors.New("Agent refused mutation control; inspect its exact operation and logs")
	}
	return nil
}

func runningJob(job *transport.ServiceMutationJob, begin transport.ServiceMutationBeginRequest, initial bool) error {
	if !exactJob(job, begin) || job.Status != "running" || job.Attempt <= 0 ||
		job.StartedAt.IsZero() || job.UpdatedAt.IsZero() || job.DeadlineAt.IsZero() ||
		job.LeaseExpiresAt.IsZero() || !job.FinishedAt.IsZero() {
		return errors.New("Agent did not return the exact live mutation lease")
	}
	workerFree := job.WorkerPID == 0 && job.WorkerStarted == "" && job.WorkerCommand == ""
	workerValid := job.WorkerPID > 0 && strings.TrimSpace(job.WorkerStarted) == job.WorkerStarted && job.WorkerStarted != "" &&
		job.WorkerCommand != "" && strings.TrimSpace(job.WorkerCommand) == job.WorkerCommand &&
		len(job.WorkerCommand) <= 64 && !strings.ContainsAny(job.WorkerCommand, "/\\")
	if initial && (!workerFree || job.Phase != "leased") || !initial && !workerFree && !workerValid {
		return errors.New("Agent mutation lease has an unexpected worker or phase")
	}
	return nil
}

func terminalJob(job *transport.ServiceMutationJob, begin transport.ServiceMutationBeginRequest, phase string, success bool) error {
	status := "failed"
	if success {
		status = "succeeded"
	}
	if !exactJob(job, begin) || job.Status != status || job.Phase != phase ||
		job.WorkerPID != 0 || job.WorkerStarted != "" || job.WorkerCommand != "" ||
		job.FinishedAt.IsZero() || !job.LeaseExpiresAt.IsZero() ||
		(success && (job.ErrorCode != "" || job.ErrorMessage != "")) {
		return errors.New("Agent lacks the exact terminal mutation proof")
	}
	return nil
}

func control(ctx context.Context, call rpcCall, method string, request any) (transport.ServiceMutationResponse, error) {
	bounded, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	var response transport.ServiceMutationResponse
	if err := call(bounded, method, request, &response); err != nil {
		return response, err
	}
	return response, responseOK(response)
}

// known is true only after a completed Agent application response. A transport
// failure never authorizes Finish(false), cancellation, or another mutation.
func operate(ctx context.Context, call rpcCall, begin transport.ServiceMutationBeginRequest, phase string,
	interval time.Duration, invoke func(context.Context, transport.ServiceMutationBinding) (known bool, err error),
) (*transport.ServiceMutationJob, error) {
	response, err := control(ctx, call, "Agent.BeginServiceMutation", &begin)
	if err != nil {
		return nil, err
	}
	if err := runningJob(response.Job, begin, true); err != nil {
		return nil, err
	}
	hbCtx, stopHB := context.WithCancel(ctx)
	hbDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				hbDone <- nil
				return
			case <-ticker.C:
			}
			response, err := control(hbCtx, call, "Agent.HeartbeatServiceMutation", &transport.ServiceMutationHeartbeatRequest{RequestID: begin.RequestID, OwnerID: begin.OwnerID})
			if hbCtx.Err() != nil {
				hbDone <- nil
				return
			}
			if err == nil && response.Job != nil && response.Job.Status == "succeeded" {
				hbDone <- terminalJob(response.Job, begin, phase, true)
				return
			}
			if err == nil {
				err = runningJob(response.Job, begin, false)
			}
			if err != nil {
				hbDone <- err
				return
			}
		}
	}()
	known, operationErr := invoke(ctx, transport.ServiceMutationBinding{MutationRequestID: begin.RequestID, MutationOwnerID: begin.OwnerID})
	stopHB()
	hbErr := <-hbDone
	if !known || hbErr != nil {
		// A read may reconcile a lost RPC response. It never starts a new attempt.
		status, statusErr := control(context.WithoutCancel(ctx), call, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: begin.RequestID})
		if statusErr == nil && terminalJob(status.Job, begin, phase, true) == nil {
			return status.Job, nil
		}
		return nil, fmt.Errorf("operation %s is unconfirmed; preserve the guest and inspect the same operation", begin.RequestID)
	}
	finish := transport.ServiceMutationFinishRequest{RequestID: begin.RequestID, OwnerID: begin.OwnerID, Success: operationErr == nil}
	wantPhase := phase
	if operationErr != nil {
		finish.FailureCode = "RELEASE_RECOVERY_FIXTURE_REJECTED"
		finish.Message = "Agent rejected the disposable fixture seed operation"
		wantPhase = "failed"
	}
	response, err = control(context.WithoutCancel(ctx), call, "Agent.FinishServiceMutation", &finish)
	if err != nil {
		return nil, err
	}
	if err := terminalJob(response.Job, begin, wantPhase, finish.Success); err != nil {
		return nil, err
	}
	// Independent status read proves the terminal record remains authoritative.
	status, err := control(context.WithoutCancel(ctx), call, "Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: begin.RequestID})
	if err != nil {
		return nil, err
	}
	if err := terminalJob(status.Job, begin, wantPhase, finish.Success); err != nil {
		return nil, err
	}
	return status.Job, operationErr
}

type publication struct {
	Generation    string
	CatalogSerial uint64
	Fields        map[string]json.RawMessage
}

func parsePublication(raw []byte) (publication, error) {
	var result publication
	if err := json.Unmarshal(raw, &result.Fields); err != nil {
		return result, err
	}
	var schema, engine string
	var epoch int64
	for name, target := range map[string]any{"schema": &schema, "engine": &engine, "engine_epoch": &epoch, "generation": &result.Generation} {
		if err := json.Unmarshal(result.Fields[name], target); err != nil {
			return result, fmt.Errorf("invalid publication field %s", name)
		}
	}
	if serial, found := result.Fields["primary_catalog_serial"]; found {
		if err := json.Unmarshal(serial, &result.CatalogSerial); err != nil {
			return result, err
		}
	}
	if schema != "celikpanel-dns-engine-state/v1" || engine != "bind" || epoch != 1 || !hex64.MatchString(result.Generation) {
		return result, errors.New("publication is not the seeded BIND epoch")
	}
	delete(result.Fields, "generation")
	delete(result.Fields, "primary_catalog_serial")
	return result, nil
}

func provePublication(ownership []byte, previous publication) (publication, error) {
	currentOwnership, err := readProtected(filepath.Join(stateRoot, "dns-engine-ownership-bind.json"), 16384, false)
	if err != nil {
		return publication{}, err
	}
	if !bytes.Equal(ownership, currentOwnership) {
		return publication{}, errors.New("publication changed acquisition evidence")
	}
	raw, err := readProtected(filepath.Join(stateRoot, "dns-engine-state.json"), 16384, false)
	if err != nil {
		return publication{}, err
	}
	current, err := parsePublication(raw)
	if err != nil {
		return current, err
	}
	if current.Generation == previous.Generation || current.CatalogSerial < previous.CatalogSerial || !reflect.DeepEqual(current.Fields, previous.Fields) {
		return current, errors.New("ordinary publication did not advance within the exact acquisition identity")
	}
	return current, nil
}

func seed(ctx context.Context, nonce, zone string) error {
	marker, err := guard(nonce)
	if err != nil {
		return err
	}
	canonical, err := hostname.CanonicalFQDN(zone)
	if err != nil || canonical != zone || !strings.HasSuffix(zone, ".test") {
		return errors.New("fixture zone must be a canonical FQDN below .test")
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "dns-engine-") {
			return errors.New("fixture requires a fresh DNS state; do not rerun against a partly seeded guest")
		}
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest("switch", "", transport.DNSEngineBIND, 0, 1, 1, transport.DNSTopologyStandalone, nil)
	if err != nil {
		return err
	}
	newIdentity := func(purpose, kind, target, qualifier string) transport.ServiceMutationBeginRequest {
		return transport.ServiceMutationBeginRequest{
			RequestID: identity(nonce, marker.CellID, zone+"/"+purpose, "request"),
			OwnerID:   identity(nonce, marker.CellID, zone+"/"+purpose, "owner"),
			Kind:      kind, Target: target, PackageName: qualifier,
		}
	}
	begin := newIdentity("switch", "dns_engine_switch", "bind", manifest.Qualifier)
	emit(seedEvent{Event: "switch_start", CellID: marker.CellID, Node: marker.Node, Zone: zone, RequestID: begin.RequestID, OwnerID: begin.OwnerID})
	phase := "commit/dns-engine-switch/v2/finalized/" + begin.RequestID + "/" + manifest.Qualifier
	job, err := operate(ctx, callAgent, begin, phase, 5*time.Second, func(ctx context.Context, binding transport.ServiceMutationBinding) (bool, error) {
		request := transport.SwitchDNSEngineV1Request{
			ServiceMutationBinding: binding, Mode: manifest.Mode, TargetEngine: manifest.TargetEngine,
			SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
			Topology: manifest.Topology, Zones: manifest.Zones, SnapshotBytes: manifest.SnapshotBytes, ManifestQualifier: manifest.Qualifier,
		}
		var response transport.SwitchDNSEngineV1Response
		if err := callAgent(ctx, "Agent.SwitchDNSEngineV1", &request, &response); err != nil {
			return false, err
		}
		if response.Error != "" {
			return true, errors.New("Agent rejected the fixture BIND switch; inspect Agent logs")
		}
		if !response.Applied || response.ActiveEngine != transport.DNSEngineBIND || response.ActiveEpoch != 1 || response.AppliedZones != 0 {
			return false, errors.New("BIND switch response does not prove the requested installation")
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	ownership, err := readProtected(filepath.Join(stateRoot, "dns-engine-ownership-bind.json"), 16384, false)
	if err != nil {
		return err
	}
	acquisition, err := parsePublication(ownership)
	if err != nil {
		return err
	}
	state, err := readProtected(filepath.Join(stateRoot, "dns-engine-state.json"), 16384, false)
	if err != nil {
		return err
	}
	if !bytes.Equal(ownership, state) {
		return errors.New("fresh acquisition and state bytes differ before publication")
	}
	digest := sha256.Sum256(ownership)
	ownershipHash := hex.EncodeToString(digest[:])
	emit(seedEvent{Event: "switch_complete", CellID: marker.CellID, Node: marker.Node, Zone: zone, RequestID: job.RequestID, OwnerID: job.OwnerID, Phase: job.Phase, Generation: acquisition.Generation, CatalogSerial: acquisition.CatalogSerial, OwnershipSHA256: ownershipHash, OwnershipUnchanged: true})
	previous := acquisition
	for generation := int64(1); generation <= 2; generation++ {
		address := "192.0.2.10"
		if generation == 2 {
			address = "192.0.2.11"
		}
		records := []transport.ZoneRecord{
			{Name: zone, Type: "SOA", Content: fmt.Sprintf("ns1.%s hostmaster.%s %d 3600 600 604800 300", zone, zone, generation), TTL: 300},
			{Name: zone, Type: "NS", Content: "ns1." + zone, TTL: 300},
			{Name: "ns1." + zone, Type: "A", Content: "192.0.2.53", TTL: 300},
			{Name: zone, Type: "A", Content: address, TTL: 300},
		}
		commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEngineBIND, 1, generation, zone, false, "NATIVE", records)
		if err != nil {
			return err
		}
		begin := newIdentity(fmt.Sprintf("zone/%d", generation), "dns_zone_sync", zone, commitment.Qualifier)
		phase := "commit/dns-zone-sync/v3/published/" + begin.RequestID + "/" + zone + "/" + commitment.Qualifier
		emit(seedEvent{Event: "publication_start", CellID: marker.CellID, Node: marker.Node, Zone: zone, RequestID: begin.RequestID, OwnerID: begin.OwnerID})
		job, err := operate(ctx, callAgent, begin, phase, 5*time.Second, func(ctx context.Context, binding transport.ServiceMutationBinding) (bool, error) {
			request := transport.SyncDNSZoneV3Request{ServiceMutationBinding: binding, Engine: transport.DNSEngineBIND, EngineEpoch: 1, DesiredGeneration: generation, Domain: zone, ZoneType: commitment.ZoneType, Records: commitment.Records}
			var response transport.SyncDNSZoneV3Response
			if err := callAgent(ctx, "Agent.SyncDNSZoneV3", &request, &response); err != nil {
				return false, err
			}
			if response.Error != "" {
				return true, errors.New("Agent rejected fixture DNS publication; inspect Agent logs")
			}
			if !response.Synced || response.RecoveryPending || response.Engine != transport.DNSEngineBIND || response.EngineEpoch != 1 || response.AppliedGeneration != generation {
				return false, errors.New("DNS publication response lacks exact success proof")
			}
			return true, nil
		})
		if err != nil {
			return err
		}
		current, err := provePublication(ownership, previous)
		if err != nil {
			return err
		}
		previous = current
		emit(seedEvent{Event: "publication_complete", CellID: marker.CellID, Node: marker.Node, Zone: zone, RequestID: job.RequestID, OwnerID: job.OwnerID, Phase: job.Phase, Generation: current.Generation, CatalogSerial: current.CatalogSerial, OwnershipSHA256: ownershipHash, OwnershipUnchanged: true})
	}
	emit(seedEvent{Event: "seed_complete", CellID: marker.CellID, Node: marker.Node, Zone: zone, Generation: previous.Generation, CatalogSerial: previous.CatalogSerial, OwnershipSHA256: ownershipHash, OwnershipUnchanged: true, Detail: "standalone BIND only; native DNS answers and update/rollback must be independently verified"})
	return nil
}
