package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsEnginePreviewTTL        = 5 * time.Minute
	dnsEngineSwitchTimeout     = 30 * time.Minute
	dnsEngineEstimatedOutage   = 15
	dnsEngineSwitchKind        = "dns_engine_switch"
	dnsEngineStateUnconfigured = "unconfigured"
	dnsEngineStateReady        = "ready"
	dnsEngineStateUnmanaged    = "unmanaged"
	dnsEngineStateConflict     = "conflict"
	dnsEngineStateSwitching    = "switching"
	dnsEngineStateDegraded     = "degraded"
)

type nullableDNSEngine struct {
	Set   bool
	Valid bool
	Value transport.DNSEngine
}

func (value *nullableDNSEngine) UnmarshalJSON(encoded []byte) error {
	value.Set = true
	if string(encoded) == "null" {
		value.Valid = false
		value.Value = ""
		return nil
	}
	var engine transport.DNSEngine
	if err := json.Unmarshal(encoded, &engine); err != nil {
		return err
	}
	if !transport.ValidDNSEngine(engine) {
		return errors.New("DNS engine must be pdns or bind")
	}
	value.Valid = true
	value.Value = engine
	return nil
}

func (value nullableDNSEngine) engine() transport.DNSEngine {
	if !value.Valid {
		return ""
	}
	return value.Value
}

type dnsEngineDBState struct {
	ActiveEngine    transport.DNSEngine
	EngineEpoch     int64
	Revision        int64
	Topology        string
	PairRole        string
	LocalIP         string
	LocalNS         string
	PeerIP          string
	PeerNS          string
	CurrentSwitchID string
}

type dnsEngineEntry struct {
	ID         transport.DNSEngine `json:"id"`
	Installed  bool                `json:"installed"`
	Running    bool                `json:"running"`
	Managed    bool                `json:"managed"`
	Status     string              `json:"status"`
	DetailCode string              `json:"detail_code,omitempty"`
}

type dnsEngineSnapshot struct {
	Revision         int64                `json:"revision"`
	EngineEpoch      int64                `json:"engine_epoch"`
	ActiveEngine     *transport.DNSEngine `json:"active_engine"`
	State            string               `json:"state"`
	Topology         string               `json:"topology"`
	PairRole         string               `json:"pair_role,omitempty"`
	PairReady        *bool                `json:"pair_ready,omitempty"`
	DNSSECZoneCount  int                  `json:"dnssec_zone_count"`
	ZoneCount        int                  `json:"zone_count"`
	PendingZoneCount int                  `json:"pending_zone_count"`
	OperationID      string               `json:"operation_id,omitempty"`
	Engines          []dnsEngineEntry     `json:"engines"`
	runtime          map[transport.DNSEngine]transport.DNSBackendRuntimeState
	port53Conflict   bool
	runtimeErr       error
	dnssecErr        error
}

type dnsEnginePreviewBlocker struct {
	Code string `json:"code"`
}

type dnsEngineSwitchPreview struct {
	PreviewToken                    string                    `json:"preview_token"`
	SourceEngine                    *transport.DNSEngine      `json:"source_engine"`
	TargetEngine                    transport.DNSEngine       `json:"target_engine"`
	ExpectedRevision                int64                     `json:"expected_revision"`
	Action                          string                    `json:"action"`
	Topology                        string                    `json:"topology"`
	ZoneCount                       int                       `json:"zone_count"`
	PendingZoneCount                int                       `json:"pending_zone_count"`
	DNSSECZoneCount                 int                       `json:"dnssec_zone_count"`
	EstimatedDowntimeSeconds        int                       `json:"estimated_downtime_seconds"`
	RequiresDowntimeAcknowledgement bool                      `json:"requires_downtime_acknowledgement"`
	Blockers                        []dnsEnginePreviewBlocker `json:"blockers"`
	Impacts                         []string                  `json:"impacts"`
}

type dnsEnginePreviewRequest struct {
	TargetEngine     transport.DNSEngine `json:"target_engine"`
	ExpectedSource   nullableDNSEngine   `json:"expected_source"`
	ExpectedRevision int64               `json:"expected_revision"`
}

type dnsEngineSwitchRequest struct {
	RequestID            string              `json:"request_id"`
	TargetEngine         transport.DNSEngine `json:"target_engine"`
	ExpectedSource       nullableDNSEngine   `json:"expected_source"`
	ExpectedRevision     int64               `json:"expected_revision"`
	PreviewToken         string              `json:"preview_token"`
	DowntimeAcknowledged bool                `json:"downtime_acknowledged"`
}

type dnsEnginePreviewAuthority struct {
	Target            transport.DNSEngine
	Source            transport.DNSEngine
	Action            string
	Revision          int64
	ManifestQualifier string
	SnapshotBytes     int64
	ExpiresAt         time.Time
}

type dnsEnginePreviewCache struct {
	mu      sync.Mutex
	entries map[string]dnsEnginePreviewAuthority
}

func enginePointer(engine transport.DNSEngine) *transport.DNSEngine {
	if engine == "" {
		return nil
	}
	copy := engine
	return &copy
}

func requireExactRows(result sql.Result, want int64, message string) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != want {
		return errors.New(message)
	}
	return nil
}

func readDNSEngineDBState(ctx context.Context, query dnsZoneStateQuery) (dnsEngineDBState, error) {
	var active, current sql.NullString
	var state dnsEngineDBState
	err := query.QueryRowContext(ctx, `
		SELECT active_engine, active_epoch, revision, topology, current_switch_id
		FROM dns_engine_state WHERE singleton_id = 1`).Scan(
		&active, &state.EngineEpoch, &state.Revision, &state.Topology, &current,
	)
	if err != nil {
		return dnsEngineDBState{}, err
	}
	if active.Valid {
		state.ActiveEngine = transport.DNSEngine(active.String)
	}
	if current.Valid {
		state.CurrentSwitchID = current.String
	}
	var pairRole, localIP, localNS, peerIP, peerNS sql.NullString
	var pairEpoch int64
	pairErr := query.QueryRowContext(ctx, `
		SELECT active_epoch, pair_role, local_ip, local_ns, peer_ip, peer_ns
		FROM dns_bind_pair_state WHERE singleton_id = 1`).Scan(
		&pairEpoch, &pairRole, &localIP, &localNS, &peerIP, &peerNS,
	)
	if pairErr != nil && !errors.Is(pairErr, sql.ErrNoRows) {
		return dnsEngineDBState{}, pairErr
	}
	if pairErr == nil {
		if !transport.ValidDNSEngine(state.ActiveEngine) ||
			state.Topology != transport.DNSTopologyStandalone ||
			pairEpoch != state.EngineEpoch || !pairRole.Valid || !localIP.Valid ||
			!localNS.Valid || !peerIP.Valid || !peerNS.Valid {
			return dnsEngineDBState{}, errors.New("persisted BIND pair state is inconsistent")
		}
		state.Topology = transport.DNSTopologyPaired
		state.PairRole = pairRole.String
		state.LocalIP, state.LocalNS = localIP.String, localNS.String
		state.PeerIP, state.PeerNS = peerIP.String, peerNS.String
	}
	if (state.ActiveEngine != "" && !transport.ValidDNSEngine(state.ActiveEngine)) ||
		state.EngineEpoch < 0 || state.Revision < 0 ||
		(state.Topology != transport.DNSTopologyStandalone &&
			state.Topology != transport.DNSTopologyPaired) ||
		(state.Topology == transport.DNSTopologyPaired &&
			state.ActiveEngine != transport.DNSEnginePowerDNS &&
			(state.ActiveEngine != transport.DNSEngineBIND ||
				(state.PairRole != transport.DNSPairRolePrimary &&
					state.PairRole != transport.DNSPairRoleSecondary))) ||
		(state.CurrentSwitchID != "" && !validServiceOperationID(state.CurrentSwitchID)) {
		return dnsEngineDBState{}, errors.New("persisted DNS engine state is invalid")
	}
	return state, nil
}

func validateDNSBackendReadiness(
	response transport.DNSBackendReadinessResponse,
) (map[transport.DNSEngine]transport.DNSBackendRuntimeState, bool, error) {
	if response.Error != "" || len(response.Engines) != 2 {
		return nil, false, errors.New("DNS backend readiness is unavailable")
	}
	result := make(map[transport.DNSEngine]transport.DNSBackendRuntimeState, 2)
	for _, runtime := range response.Engines {
		if !transport.ValidDNSEngine(runtime.Engine) {
			return nil, false, errors.New("DNS backend readiness contains an unknown engine")
		}
		if _, duplicate := result[runtime.Engine]; duplicate {
			return nil, false, errors.New("DNS backend readiness contains a duplicate engine")
		}
		if runtime.Running && !runtime.Installed ||
			runtime.Managed && !runtime.Installed ||
			runtime.PairReady && (!runtime.Installed || !runtime.Running || !runtime.Managed) ||
			len(runtime.Unit) > 128 ||
			strings.ContainsAny(runtime.Unit, "\r\n\x00") {
			return nil, false, errors.New("DNS backend readiness is internally inconsistent")
		}
		result[runtime.Engine] = runtime
	}
	if _, ok := result[transport.DNSEnginePowerDNS]; !ok {
		return nil, false, errors.New("PowerDNS readiness is missing")
	}
	if _, ok := result[transport.DNSEngineBIND]; !ok {
		return nil, false, errors.New("BIND readiness is missing")
	}
	return result, response.Port53Conflict, nil
}

func (p *Panel) readDNSBackendRuntime(
	ctx context.Context,
) (map[transport.DNSEngine]transport.DNSBackendRuntimeState, bool, error) {
	var response transport.DNSBackendReadinessResponse
	if err := p.callAgentContext(
		ctx, "Agent.DNSBackendReadiness", &transport.Empty{}, &response,
	); err != nil {
		return nil, false, fmt.Errorf("read DNS backend readiness: %w", err)
	}
	return validateDNSBackendReadiness(response)
}

func (p *Panel) dnsEngineTopology(ctx context.Context) string {
	raw := strings.TrimSpace(p.setting(ctx, settingDNSRole))
	switch normalizeDNSRole(raw) {
	case "standalone":
		return "standalone"
	case "paired":
		return "paired"
	default:
		return "unconfigured"
	}
}

func (p *Panel) dnsEngineZoneCounts(ctx context.Context) (int, int, []string, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT zone_name, desired_action,
		       CASE WHEN desired_generation <> applied_generation
		              OR status <> 'applied'
		              OR lease_request_id IS NOT NULL
		              OR EXISTS (
		                   SELECT 1 FROM dns_zone_engine_leases AS lease
		                   WHERE lease.zone_name = dns_zone_sync_state.zone_name
		              )
		            THEN 1 ELSE 0 END
		FROM dns_zone_sync_state
		ORDER BY zone_name`)
	if err != nil {
		return 0, 0, nil, err
	}
	defer rows.Close()
	var zones []string
	pending := 0
	for rows.Next() {
		var zone, action string
		var isPending int
		if err := rows.Scan(&zone, &action, &isPending); err != nil {
			return 0, 0, nil, err
		}
		if action == "sync" {
			zones = append(zones, zone)
		}
		pending += isPending
	}
	if err := rows.Err(); err != nil {
		return 0, 0, nil, err
	}
	var total int
	if err := p.db.GetDB().QueryRowContext(
		ctx, `SELECT count(*) FROM dns_zone_sync_state`,
	).Scan(&total); err != nil {
		return 0, 0, nil, err
	}
	return total, pending, zones, nil
}

func (p *Panel) dnsEngineDNSSECCount(
	ctx context.Context,
	zones []string,
) (int, error) {
	if len(zones) == 0 {
		return 0, nil
	}
	type result struct {
		secured bool
		err     error
	}
	workers := 8
	if len(zones) < workers {
		workers = len(zones)
	}
	jobs := make(chan string)
	results := make(chan result, len(zones))
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for zone := range jobs {
				var response transport.DNSSECStatusResponse
				err := p.callAgentContext(
					ctx, "Agent.DNSSECStatus",
					&transport.DNSSECRequest{Zone: zone}, &response,
				)
				if err == nil && response.Error != "" {
					err = errors.New("DNSSEC readiness is unavailable")
				}
				results <- result{secured: response.Secured || len(response.DS) > 0, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, zone := range zones {
			select {
			case jobs <- zone:
			case <-ctx.Done():
				return
			}
		}
	}()
	group.Wait()
	close(results)
	count := 0
	var joined error
	for item := range results {
		if item.secured {
			count++
		}
		if item.err != nil {
			joined = errors.Join(joined, item.err)
		}
	}
	if ctx.Err() != nil {
		joined = errors.Join(joined, ctx.Err())
	}
	return count, joined
}

func deriveDNSEnginePresentation(
	state dnsEngineDBState,
	runtimes map[transport.DNSEngine]transport.DNSBackendRuntimeState,
	runtimeErr error,
) (string, []dnsEngineEntry) {
	ids := []transport.DNSEngine{
		transport.DNSEnginePowerDNS,
		transport.DNSEngineBIND,
	}
	entries := make([]dnsEngineEntry, 0, len(ids))
	if runtimeErr != nil {
		for _, id := range ids {
			entries = append(entries, dnsEngineEntry{
				ID: id, Status: "available", DetailCode: "readiness_unavailable",
			})
		}
		if state.CurrentSwitchID != "" {
			return dnsEngineStateSwitching, entries
		}
		return dnsEngineStateDegraded, entries
	}
	running := 0
	for _, runtime := range runtimes {
		if runtime.Running {
			running++
		}
	}
	for _, id := range ids {
		runtime := runtimes[id]
		entry := dnsEngineEntry{
			ID: id, Installed: runtime.Installed,
			Running: runtime.Running, Managed: runtime.Managed,
		}
		switch {
		case running > 1 && runtime.Running:
			entry.Status = "conflict"
			entry.DetailCode = "port_53_conflict"
		case state.ActiveEngine == id && runtime.Installed && runtime.Running && runtime.Managed:
			entry.Status = "active"
		case runtime.Running:
			if state.ActiveEngine == "" || !runtime.Managed {
				entry.Status = "unmanaged"
				entry.DetailCode = "unmanaged_dns_detected"
			} else {
				entry.Status = "conflict"
				entry.DetailCode = "active_engine_mismatch"
			}
		case runtime.Installed:
			if !runtime.Managed {
				entry.Status = "unmanaged"
				entry.DetailCode = "unmanaged_dns_detected"
			} else {
				entry.Status = "installed_standby"
			}
		default:
			entry.Status = "available"
		}
		entries = append(entries, entry)
	}
	if state.CurrentSwitchID != "" {
		return dnsEngineStateSwitching, entries
	}
	if running > 1 {
		return dnsEngineStateConflict, entries
	}
	if state.ActiveEngine == "" {
		if running == 0 {
			return dnsEngineStateUnconfigured, entries
		}
		return dnsEngineStateUnmanaged, entries
	}
	active := runtimes[state.ActiveEngine]
	other := transport.DNSEnginePowerDNS
	if state.ActiveEngine == transport.DNSEnginePowerDNS {
		other = transport.DNSEngineBIND
	}
	if active.Installed && active.Running && active.Managed && !runtimes[other].Running {
		return dnsEngineStateReady, entries
	}
	if runtimes[other].Running {
		return dnsEngineStateConflict, entries
	}
	return dnsEngineStateDegraded, entries
}

// dnsEngineSnapshot is the reusable fail-closed state reader shared by the
// engine UI and lifecycle gates. It never infers durable authority from a
// running process: active_engine comes only from dns_engine_state.
func (p *Panel) dnsEngineSnapshot(ctx context.Context) (dnsEngineSnapshot, error) {
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return dnsEngineSnapshot{}, fmt.Errorf("read DNS engine identity: %w", err)
	}
	zoneCount, pendingCount, zones, err := p.dnsEngineZoneCounts(ctx)
	if err != nil {
		return dnsEngineSnapshot{}, fmt.Errorf("read DNS engine zone counts: %w", err)
	}
	runtimes, port53Conflict, runtimeErr := p.readDNSBackendRuntime(ctx)
	dnssecCount := 0
	var dnssecErr error
	// BIND is currently activated only by an exact unsigned-zone switch
	// snapshot. Do not probe the stopped PowerDNS backend as ongoing BIND
	// health; engine-aware DNSSEC publication will replace this proof when
	// BIND signing support is introduced.
	if state.ActiveEngine != transport.DNSEngineBIND {
		dnssecCount, dnssecErr = p.dnsEngineDNSSECCount(ctx, zones)
	}
	presentationState, entries := deriveDNSEnginePresentation(
		state, runtimes, runtimeErr,
	)
	if dnssecErr != nil && presentationState != dnsEngineStateSwitching {
		presentationState = dnsEngineStateDegraded
	}
	topology := p.dnsEngineTopology(ctx)
	if state.ActiveEngine != "" {
		topology = state.Topology
	}
	var pairReady *bool
	if state.ActiveEngine != "" && state.Topology == transport.DNSTopologyPaired {
		ready := runtimes[state.ActiveEngine].PairReady
		pairReady = &ready
	}
	return dnsEngineSnapshot{
		Revision: state.Revision, EngineEpoch: state.EngineEpoch,
		ActiveEngine: enginePointer(state.ActiveEngine),
		State:        presentationState, Topology: topology, PairRole: state.PairRole,
		PairReady:       pairReady,
		DNSSECZoneCount: dnssecCount, ZoneCount: zoneCount,
		PendingZoneCount: pendingCount, OperationID: state.CurrentSwitchID,
		Engines: entries, runtime: runtimes, port53Conflict: port53Conflict,
		runtimeErr: runtimeErr,
		dnssecErr:  dnssecErr,
	}, nil
}

type dnsPublisherIdentity struct {
	Engine   transport.DNSEngine
	Epoch    int64
	PairRole string
}

// activeDNSPublisher authorizes engine-aware publication only when durable
// identity and strict runtime readiness agree exactly. Epoch is part of the
// authority: callers must bind both fields into SyncDNSZoneV3.
func (p *Panel) activeDNSPublisher(
	ctx context.Context,
) (dnsPublisherIdentity, bool, error) {
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return dnsPublisherIdentity{}, false, err
	}
	if state.ActiveEngine == "" || state.CurrentSwitchID != "" {
		return dnsPublisherIdentity{}, false, nil
	}
	runtimes, port53Conflict, err := p.readDNSBackendRuntime(ctx)
	if err != nil {
		return dnsPublisherIdentity{}, false, err
	}
	if port53Conflict {
		return dnsPublisherIdentity{}, false, nil
	}
	runtime := runtimes[state.ActiveEngine]
	if !runtime.Installed || !runtime.Running || !runtime.Managed {
		return dnsPublisherIdentity{}, false, nil
	}
	for engine, candidate := range runtimes {
		if engine != state.ActiveEngine && candidate.Running {
			return dnsPublisherIdentity{}, false, nil
		}
	}
	identity := dnsPublisherIdentity{
		Engine: state.ActiveEngine,
		Epoch:  state.EngineEpoch, PairRole: state.PairRole,
	}
	// A directional secondary serves transferred zones but must never
	// accept panel-local domain or record mutations.  Returning the exact
	// identity with ready=false keeps read-only engine truth visible while all
	// hosting publication paths fail closed before acquiring a V3 lease.
	if identity.PairRole == transport.DNSPairRoleSecondary {
		return identity, false, nil
	}
	// A directional primary becomes a panel-local publisher only after the
	// agent proves that the peer serves the exact primary catalog and every
	// catalog member. Engine ownership remains managed while a peer is absent.
	if identity.PairRole == transport.DNSPairRolePrimary && !runtime.PairReady {
		return identity, false, nil
	}
	return identity, true, nil
}

func (p *Panel) requireActivePowerDNSPublisher(ctx context.Context) error {
	publisher, ready, err := p.activeDNSPublisher(ctx)
	if err != nil {
		return fmt.Errorf("verify active PowerDNS publisher: %w", err)
	}
	if !ready || publisher.Engine != transport.DNSEnginePowerDNS ||
		publisher.Epoch < 1 {
		return errors.New("PowerDNS is not the exact active DNS publisher")
	}
	return nil
}

// callSyncDNSZoneV3 is the reviewed engine-bound publication boundary. The
// surrounding durable mutation owns request/owner binding and recovery; this
// helper rejects a response for any authority other than the exact request.
func (p *Panel) callSyncDNSZoneV3(
	ctx context.Context,
	request *transport.SyncDNSZoneV3Request,
	response *transport.SyncDNSZoneV3Response,
) error {
	if request == nil || response == nil ||
		!transport.ValidDNSEngine(request.Engine) || request.EngineEpoch < 1 {
		return errors.New("invalid engine-bound DNS publication")
	}
	if err := p.callAgentContext(
		ctx, "Agent.SyncDNSZoneV3", request, response,
	); err != nil {
		return err
	}
	if response.Error != "" || !response.Synced ||
		response.Engine != request.Engine ||
		response.EngineEpoch != request.EngineEpoch ||
		response.AppliedGeneration != request.DesiredGeneration {
		return errors.New("agent did not confirm the exact DNS publication")
	}
	return nil
}

func dnsEngineAction(
	snapshot dnsEngineSnapshot,
	target transport.DNSEngine,
) string {
	runtime := snapshot.runtime[target]
	if !runtime.Installed {
		return "install"
	}
	if snapshot.ActiveEngine == nil &&
		target == transport.DNSEnginePowerDNS &&
		runtime.Running && runtime.Managed {
		for engine, candidate := range snapshot.runtime {
			if engine != target && candidate.Running {
				return "switch"
			}
		}
		return "adopt"
	}
	return "switch"
}

func dnsEngineImpacts(action string, hasSource bool) []string {
	if action == "adopt" {
		return []string{"validate_target", "adopt_existing"}
	}
	impacts := make([]string, 0, 8)
	if action == "install" {
		impacts = append(impacts, "install_target")
	}
	impacts = append(impacts, "validate_target", "publish_zones")
	if hasSource {
		impacts = append(impacts, "stop_source")
	}
	impacts = append(impacts, "start_target")
	if hasSource {
		impacts = append(impacts, "brief_dns_interruption")
	}
	if hasSource {
		impacts = append(impacts, "keep_source_standby")
	}
	return impacts
}

func addDNSEngineBlocker(
	blockers []dnsEnginePreviewBlocker,
	code string,
) []dnsEnginePreviewBlocker {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return blockers
		}
	}
	return append(blockers, dnsEnginePreviewBlocker{Code: code})
}

func dnsEnginePreviewBlockers(
	snapshot dnsEngineSnapshot,
	target, expectedSource transport.DNSEngine,
	expectedRevision int64,
) []dnsEnginePreviewBlocker {
	blockers := make([]dnsEnginePreviewBlocker, 0, 8)
	action := dnsEngineAction(snapshot, target)
	actualSource := transport.DNSEngine("")
	if snapshot.ActiveEngine != nil {
		actualSource = *snapshot.ActiveEngine
	}
	if snapshot.Revision != expectedRevision || actualSource != expectedSource {
		blockers = addDNSEngineBlocker(blockers, "stale_revision")
	}
	if snapshot.State == dnsEngineStateSwitching {
		blockers = addDNSEngineBlocker(blockers, "operation_running")
	}
	if snapshot.ActiveEngine == nil &&
		snapshot.Topology == dnsEngineStateUnconfigured {
		blockers = addDNSEngineBlocker(blockers, "dns_identity_required")
	}
	// Registration-only adoption verifies the exact full runtime zone set and
	// may therefore reconcile legacy pending generations without publishing.
	// A live lease is still rejected by buildDNSEngineManifest.
	if action != "adopt" && snapshot.PendingZoneCount > 0 {
		blockers = addDNSEngineBlocker(blockers, "pending_zone_sync")
	}
	if action != "adopt" &&
		(snapshot.DNSSECZoneCount > 0 || snapshot.dnssecErr != nil) {
		blockers = addDNSEngineBlocker(blockers, "dnssec_unsupported")
	}
	if snapshot.runtimeErr != nil {
		blockers = addDNSEngineBlocker(blockers, "target_unavailable")
	}
	if snapshot.port53Conflict {
		blockers = addDNSEngineBlocker(blockers, "port_53_conflict")
	}
	if snapshot.ActiveEngine != nil && *snapshot.ActiveEngine == target {
		blockers = addDNSEngineBlocker(blockers, "target_already_active")
	}
	targetRuntime := snapshot.runtime[target]
	if targetRuntime.Installed && !targetRuntime.Managed {
		blockers = addDNSEngineBlocker(blockers, "unmanaged_dns_detected")
	}
	if action == "adopt" &&
		(target != transport.DNSEnginePowerDNS || snapshot.ActiveEngine != nil ||
			!targetRuntime.Installed || !targetRuntime.Running ||
			!targetRuntime.Managed ||
			(snapshot.Topology != transport.DNSTopologyStandalone &&
				snapshot.Topology != transport.DNSTopologyPaired)) {
		blockers = addDNSEngineBlocker(blockers, "target_unavailable")
	}
	switch snapshot.State {
	case dnsEngineStateConflict:
		blockers = addDNSEngineBlocker(blockers, "port_53_conflict")
	case dnsEngineStateDegraded:
		blockers = addDNSEngineBlocker(blockers, "source_degraded")
	case dnsEngineStateUnmanaged:
		runningOther := false
		for id, runtime := range snapshot.runtime {
			if id != target && runtime.Running {
				runningOther = true
			}
		}
		if !targetRuntime.Running || !targetRuntime.Managed || runningOther {
			blockers = addDNSEngineBlocker(blockers, "unmanaged_dns_detected")
		}
	}
	return blockers
}

func (cache *dnsEnginePreviewCache) put(
	token string,
	authority dnsEnginePreviewAuthority,
) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[string]dnsEnginePreviewAuthority)
	}
	now := time.Now()
	for key, entry := range cache.entries {
		if !entry.ExpiresAt.After(now) {
			delete(cache.entries, key)
		}
	}
	cache.entries[token] = authority
}

func (cache *dnsEnginePreviewCache) consume(
	token string,
) (dnsEnginePreviewAuthority, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[token]
	if ok {
		delete(cache.entries, token)
	}
	if !ok || !entry.ExpiresAt.After(time.Now()) {
		return dnsEnginePreviewAuthority{}, false
	}
	return entry, true
}

func canonicalDNSEnginePeerSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	topology string,
) (string, string, error) {
	rawRole, err := settingTx(ctx, tx, settingDNSRole)
	if err != nil {
		return "", "", err
	}
	storedRole := normalizeDNSRole(strings.TrimSpace(rawRole))
	if strings.TrimSpace(rawRole) == "" {
		storedRole = transport.DNSTopologyStandalone
	}
	if storedRole != topology {
		return "", "", errors.New(
			"stored DNS peer topology differs from the observed authority",
		)
	}
	if topology == transport.DNSTopologyStandalone {
		return "", "", nil
	}
	peerIP, err := settingTx(ctx, tx, settingDNSPeerIP)
	if err != nil {
		return "", "", err
	}
	peerNS, err := settingTx(ctx, tx, settingDNSPeerNS)
	if err != nil {
		return "", "", err
	}
	peer, err := mutationpayload.CanonicalDNSClusterConfig(
		topology,
		strings.TrimSpace(peerIP),
		strings.ToLower(strings.TrimSpace(strings.TrimSuffix(peerNS, "."))),
	)
	if err != nil {
		return "", "", err
	}
	return peer.PeerIP, peer.PeerNS, nil
}

func canonicalBINDEnginePairIdentityTx(
	ctx context.Context,
	tx *sql.Tx,
	peerNS string,
) (string, string, string, error) {
	ns1Raw, err := settingTx(ctx, tx, settingNS1)
	if err != nil {
		return "", "", "", err
	}
	ns2Raw, err := settingTx(ctx, tx, settingNS2)
	if err != nil {
		return "", "", "", err
	}
	ns1 := canonicalDNSName(ns1Raw)
	ns2 := canonicalDNSName(ns2Raw)
	if ns1 == "" || ns2 == "" || ns1 == ns2 {
		return "", "", "", errors.New("BIND pairing requires two distinct saved nameservers")
	}
	localNS := ""
	role := ""
	switch peerNS {
	case ns2:
		localNS, role = ns1, transport.DNSPairRolePrimary
	case ns1:
		localNS, role = ns2, transport.DNSPairRoleSecondary
	default:
		return "", "", "", errors.New("BIND peer nameserver does not match the saved identity")
	}
	localIP := strings.TrimSpace(serverPrimaryIP())
	if localIP == "" {
		return "", "", "", errors.New("BIND pairing requires a verified local IPv4 address")
	}
	return role, localIP, localNS, nil
}

func (p *Panel) buildDNSEngineManifest(
	ctx context.Context,
	state dnsEngineDBState,
	target transport.DNSEngine,
	action, observedTopology string,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	mode := dnsEngineMutationMode(action)
	topology := state.Topology
	if mode == transport.DNSEngineSwitchModeSwitch &&
		state.ActiveEngine == "" &&
		observedTopology == transport.DNSTopologyPaired {
		topology = transport.DNSTopologyPaired
	}
	switch mode {
	case transport.DNSEngineSwitchModeSwitch:
		if topology != transport.DNSTopologyStandalone &&
			topology != transport.DNSTopologyPaired {
			return mutationpayload.DNSEngineSwitchManifestCommitment{},
				errors.New("durable DNS engine topology is unsupported for the target")
		}
	case transport.DNSEngineSwitchModeAdopt:
		topology = observedTopology
		if state.ActiveEngine != "" || state.EngineEpoch != 0 ||
			target != transport.DNSEnginePowerDNS ||
			(topology != transport.DNSTopologyStandalone &&
				topology != transport.DNSTopologyPaired) {
			return mutationpayload.DNSEngineSwitchManifestCommitment{},
				errors.New("legacy PowerDNS adoption identity is invalid")
		}
	default:
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine operation mode is invalid")
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	defer tx.Rollback()
	peerIP, peerNS, err := canonicalDNSEnginePeerSnapshotTx(ctx, tx, topology)
	if err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	pairRole, localIP, localNS := "", "", ""
	if mode == transport.DNSEngineSwitchModeSwitch &&
		topology == transport.DNSTopologyPaired {
		pairRole, localIP, localNS, err = canonicalBINDEnginePairIdentityTx(ctx, tx, peerNS)
		if err != nil {
			return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
		}
	}
	var leases int
	if err := tx.QueryRowContext(ctx, `
		SELECT
		  (SELECT count(*) FROM dns_zone_sync_state WHERE lease_request_id IS NOT NULL)
		  + (SELECT count(*) FROM dns_zone_engine_leases)`).Scan(&leases); err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	if leases != 0 {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("a DNS publication operation is still active")
	}
	type desiredZone struct {
		name       string
		domainID   sql.NullInt64
		generation int64
		action     string
		zoneType   string
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT zone_name, source_domain_id, desired_generation,
		       desired_action, desired_zone_type
		FROM dns_zone_sync_state ORDER BY zone_name`)
	if err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	var desired []desiredZone
	for rows.Next() {
		var zone desiredZone
		if err := rows.Scan(
			&zone.name, &zone.domainID, &zone.generation,
			&zone.action, &zone.zoneType,
		); err != nil {
			rows.Close()
			return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
		}
		desired = append(desired, zone)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	if err := rows.Close(); err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	zones := make([]transport.DNSEngineSwitchZoneSnapshot, 0, len(desired))
	for _, desiredZone := range desired {
		var records []transport.ZoneRecord
		deleteZone := desiredZone.action == "delete"
		if deleteZone {
			if desiredZone.domainID.Valid {
				return mutationpayload.DNSEngineSwitchManifestCommitment{},
					errors.New("DNS deletion retains a source domain")
			}
		} else {
			if desiredZone.action != "sync" || !desiredZone.domainID.Valid {
				return mutationpayload.DNSEngineSwitchManifestCommitment{},
					errors.New("DNS desired zone identity is inconsistent")
			}
			recordRows, err := tx.QueryContext(ctx, `
				SELECT name, type, content, COALESCE(ttl, 3600),
				       COALESCE(prio, 0), disabled
				FROM pdns_records WHERE domain_id = ?`,
				desiredZone.domainID.Int64,
			)
			if err != nil {
				return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
			}
			for recordRows.Next() {
				var record transport.ZoneRecord
				if err := recordRows.Scan(
					&record.Name, &record.Type, &record.Content,
					&record.TTL, &record.Prio, &record.Disabled,
				); err != nil {
					recordRows.Close()
					return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
				}
				records = append(records, record)
			}
			if err := recordRows.Err(); err != nil {
				recordRows.Close()
				return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
			}
			if err := recordRows.Close(); err != nil {
				return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
			}
		}
		zones = append(zones, transport.DNSEngineSwitchZoneSnapshot{
			Domain: desiredZone.name, DesiredGeneration: desiredZone.generation,
			Delete: deleteZone, ZoneType: desiredZone.zoneType, Records: records,
		})
	}
	if pairRole == transport.DNSPairRoleSecondary {
		for _, zone := range zones {
			if !zone.Delete {
				return mutationpayload.DNSEngineSwitchManifestCommitment{},
					errors.New("a BIND secondary cannot retain locally owned live zones")
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	return mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		mode,
		state.ActiveEngine, target, state.EngineEpoch, state.EngineEpoch+1,
		state.Revision, topology, pairRole, localIP, localNS, peerIP, peerNS, zones,
	)
}

func (p *Panel) makeDNSEnginePreview(
	ctx context.Context,
	request dnsEnginePreviewRequest,
	operationBusy bool,
) (dnsEngineSwitchPreview, error) {
	snapshot, err := p.dnsEngineSnapshot(ctx)
	if err != nil {
		return dnsEngineSwitchPreview{}, err
	}
	source := request.ExpectedSource.engine()
	action := dnsEngineAction(snapshot, request.TargetEngine)
	blockers := dnsEnginePreviewBlockers(
		snapshot, request.TargetEngine, source, request.ExpectedRevision,
	)
	if operationBusy {
		blockers = addDNSEngineBlocker(blockers, "operation_running")
	}
	if !operationBusy {
		if err := p.requireNoPendingDNSClusterSaga(ctx); err != nil {
			blockers = addDNSEngineBlocker(blockers, "operation_running")
		}
		if err := p.requireDNSEngineSwitchV1Agent(ctx); err != nil {
			blockers = addDNSEngineBlocker(blockers, "agent_incompatible")
		}
	}
	token, err := newServiceOperationID()
	if err != nil {
		return dnsEngineSwitchPreview{}, err
	}
	hasSource := source != ""
	requiresAck := hasSource
	preview := dnsEngineSwitchPreview{
		PreviewToken: token, SourceEngine: enginePointer(source),
		TargetEngine:     request.TargetEngine,
		ExpectedRevision: request.ExpectedRevision,
		Action:           action, Topology: snapshot.Topology,
		ZoneCount:                       snapshot.ZoneCount,
		PendingZoneCount:                snapshot.PendingZoneCount,
		DNSSECZoneCount:                 snapshot.DNSSECZoneCount,
		RequiresDowntimeAcknowledgement: requiresAck,
		Blockers:                        blockers, Impacts: dnsEngineImpacts(action, hasSource),
	}
	if requiresAck {
		preview.EstimatedDowntimeSeconds = dnsEngineEstimatedOutage
	}
	if len(blockers) != 0 {
		return preview, nil
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return dnsEngineSwitchPreview{}, err
	}
	manifest, err := p.buildDNSEngineManifest(
		ctx, state, request.TargetEngine, action, snapshot.Topology,
	)
	if err != nil {
		preview.Blockers = addDNSEngineBlocker(
			preview.Blockers, "operation_running",
		)
		return preview, nil
	}
	p.dnsEnginePreviews.put(token, dnsEnginePreviewAuthority{
		Target: request.TargetEngine, Source: source,
		Action:            action,
		Revision:          request.ExpectedRevision,
		ManifestQualifier: manifest.Qualifier,
		SnapshotBytes:     manifest.SnapshotBytes,
		ExpiresAt:         time.Now().Add(dnsEnginePreviewTTL),
	})
	return preview, nil
}

func (p *Panel) handleDNSEngine(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	snapshot, err := p.dnsEngineSnapshot(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(snapshot)
}

func (p *Panel) handleDNSEngineSwitchPreview(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request dnsEnginePreviewRequest
	if err := decodeServiceOperationJSON(w, r, &request); err != nil ||
		!transport.ValidDNSEngine(request.TargetEngine) ||
		!request.ExpectedSource.Set || request.ExpectedRevision < 0 {
		writeClientError(w, http.StatusBadRequest, "invalid DNS engine preview request")
		return
	}
	locked := p.serviceMutationMu.TryLock()
	if locked {
		defer p.serviceMutationMu.Unlock()
		p.dnsTopologyMu.Lock()
		defer p.dnsTopologyMu.Unlock()
		dnsPublicationMu.Lock()
		defer dnsPublicationMu.Unlock()
	}
	preview, err := p.makeDNSEnginePreview(r.Context(), request, !locked)
	if err != nil {
		writeServerError(w, fmt.Errorf("prepare DNS engine preview: %w", err))
		return
	}
	_ = json.NewEncoder(w).Encode(preview)
}

type persistedDNSEngineSwitch struct {
	SwitchID       string
	RequestID      string
	OwnerID        string
	SourceEngine   transport.DNSEngine
	TargetEngine   transport.DNSEngine
	SourceEpoch    int64
	TargetEpoch    int64
	SourceRevision int64
	Action         string
	Mode           string
	Topology       string
	PeerIP         string
	PeerNS         string
	PairRole       string
	LocalIP        string
	LocalNS        string
	Phase          string
	Qualifier      string
	ZoneCount      int
	SnapshotBytes  int64
}

func readDNSEngineSwitchByRequest(
	ctx context.Context,
	query dnsZoneStateQuery,
	requestID string,
) (persistedDNSEngineSwitch, error) {
	var result persistedDNSEngineSwitch
	var source, pairRole, localIP, localNS, pairPeerIP, pairPeerNS sql.NullString
	err := query.QueryRowContext(ctx, `
		SELECT snapshot.switch_id, snapshot.request_id, snapshot.owner_id,
		       snapshot.mode, snapshot.source_engine, snapshot.target_engine,
		       snapshot.source_epoch, snapshot.target_epoch,
		       snapshot.source_state_revision, snapshot.phase,
		       snapshot.topology, snapshot.peer_ip, snapshot.peer_ns,
		       snapshot.manifest_qualifier, snapshot.zone_count, snapshot.snapshot_bytes,
		       pairing.pair_role, pairing.local_ip, pairing.local_ns,
		       pairing.peer_ip, pairing.peer_ns
		FROM dns_engine_switch_snapshots AS snapshot
		LEFT JOIN dns_bind_pair_switches AS pairing
		  ON pairing.switch_id = snapshot.switch_id
		WHERE snapshot.request_id = ?`,
		requestID,
	).Scan(
		&result.SwitchID, &result.RequestID, &result.OwnerID, &result.Mode, &source,
		&result.TargetEngine, &result.SourceEpoch, &result.TargetEpoch,
		&result.SourceRevision, &result.Phase, &result.Topology,
		&result.PeerIP, &result.PeerNS, &result.Qualifier,
		&result.ZoneCount, &result.SnapshotBytes,
		&pairRole, &localIP, &localNS, &pairPeerIP, &pairPeerNS,
	)
	if source.Valid {
		result.SourceEngine = transport.DNSEngine(source.String)
	}
	if pairRole.Valid {
		if !localIP.Valid || !localNS.Valid || !pairPeerIP.Valid || !pairPeerNS.Valid ||
			!transport.ValidDNSEngine(result.TargetEngine) ||
			result.Mode != transport.DNSEngineSwitchModeSwitch ||
			result.Topology != transport.DNSTopologyStandalone {
			return persistedDNSEngineSwitch{}, errors.New("persisted DNS pair switch is invalid")
		}
		result.Topology = transport.DNSTopologyPaired
		result.PairRole, result.LocalIP, result.LocalNS = pairRole.String, localIP.String, localNS.String
		result.PeerIP, result.PeerNS = pairPeerIP.String, pairPeerNS.String
	}
	if err == nil &&
		(result.Mode != transport.DNSEngineSwitchModeSwitch &&
			result.Mode != transport.DNSEngineSwitchModeAdopt ||
			(result.Topology != transport.DNSTopologyStandalone &&
				result.Topology != transport.DNSTopologyPaired)) {
		return persistedDNSEngineSwitch{}, errors.New(
			"persisted DNS engine switch identity is invalid",
		)
	}
	if err == nil {
		peer, peerErr := mutationpayload.CanonicalDNSClusterConfig(
			result.Topology, result.PeerIP, result.PeerNS,
		)
		if peerErr != nil || peer.PeerIP != result.PeerIP || peer.PeerNS != result.PeerNS {
			return persistedDNSEngineSwitch{}, errors.New(
				"persisted DNS engine peer identity is invalid",
			)
		}
	}
	return result, err
}

func readDNSEngineSwitchByID(
	ctx context.Context,
	query dnsZoneStateQuery,
	switchID string,
) (persistedDNSEngineSwitch, error) {
	var requestID string
	if err := query.QueryRowContext(ctx, `
		SELECT request_id FROM dns_engine_switch_snapshots
		WHERE switch_id = ?`, switchID,
	).Scan(&requestID); err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	return readDNSEngineSwitchByRequest(ctx, query, requestID)
}

func (p *Panel) persistDNSEngineSwitch(
	ctx context.Context,
	request dnsEngineSwitchRequest,
	ownerID, switchID string,
	action string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (persistedDNSEngineSwitch, error) {
	if !validDNSEngineSwitchAction(action) {
		return persistedDNSEngineSwitch{}, errors.New(
			"DNS engine switch action is invalid",
		)
	}
	mode := dnsEngineMutationMode(action)
	if mode != manifest.Mode {
		return persistedDNSEngineSwitch{}, errors.New(
			"DNS engine action does not match its durable mode",
		)
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	defer tx.Rollback()
	state, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	if state.CurrentSwitchID != "" ||
		state.ActiveEngine != manifest.SourceEngine ||
		state.EngineEpoch != manifest.SourceEpoch ||
		state.Revision != manifest.SourceRevision {
		return persistedDNSEngineSwitch{}, errors.New("DNS engine state changed before switch persistence")
	}
	peerIP, peerNS, err := canonicalDNSEnginePeerSnapshotTx(
		ctx, tx, manifest.Topology,
	)
	if err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	if peerIP != manifest.PeerIP || peerNS != manifest.PeerNS {
		return persistedDNSEngineSwitch{}, errors.New(
			"DNS peer identity changed before switch persistence",
		)
	}
	var source any
	if manifest.SourceEngine != "" {
		source = string(manifest.SourceEngine)
	}
	storageTopology := manifest.Topology
	storagePeerIP, storagePeerNS := manifest.PeerIP, manifest.PeerNS
	if manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.Topology == transport.DNSTopologyPaired {
		storageTopology = transport.DNSTopologyStandalone
		storagePeerIP, storagePeerNS = "", ""
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO dns_engine_switch_snapshots (
		  switch_id, request_id, owner_id, mode, source_engine, target_engine,
		  source_epoch, target_epoch, source_state_revision, topology,
		  peer_ip, peer_ns, phase,
		  manifest_qualifier, zone_count, snapshot_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'planned', ?, ?, ?)`,
		switchID, request.RequestID, ownerID, manifest.Mode, source,
		manifest.TargetEngine, manifest.SourceEpoch, manifest.TargetEpoch,
		manifest.SourceRevision, storageTopology, storagePeerIP, storagePeerNS,
		manifest.Qualifier,
		len(manifest.Zones), manifest.SnapshotBytes,
	); err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	if manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.Topology == transport.DNSTopologyPaired {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO dns_bind_pair_switches (
			  switch_id, pair_role, local_ip, local_ns, peer_ip, peer_ns
			) VALUES (?, ?, ?, ?, ?, ?)`,
			switchID, manifest.PairRole, manifest.LocalIP, manifest.LocalNS,
			manifest.PeerIP, manifest.PeerNS,
		); err != nil {
			return persistedDNSEngineSwitch{}, err
		}
	}
	for _, zone := range manifest.Zones {
		recordsJSON, err := mutationpayload.MarshalDNSZoneSnapshotRecords(zone.Records)
		if err != nil {
			return persistedDNSEngineSwitch{}, err
		}
		action := "sync"
		if zone.Delete {
			action = "delete"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO dns_engine_switch_zones (
			  switch_id, ordinal, zone_name, desired_generation,
			  desired_action, desired_zone_type, zone_qualifier,
			  records_json, records_bytes, phase
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
			switchID, zone.Ordinal, zone.Domain, zone.DesiredGeneration,
			action, zone.ZoneType, zone.ZoneQualifier,
			string(recordsJSON), len(recordsJSON),
		); err != nil {
			return persistedDNSEngineSwitch{}, err
		}
	}
	if err := persistDNSEngineOperationMarkerTx(
		ctx, tx, dnsEngineOperationMarker{
			Version:   dnsEngineOperationVersion,
			RequestID: request.RequestID, SwitchID: switchID,
			SourceEngine: manifest.SourceEngine,
			TargetEngine: manifest.TargetEngine,
			Action:       action,
			Phase:        dnsEngineOperationAccepted,
		},
	); err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	attached, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_state
		SET current_switch_id = ?, revision = revision + 1,
		    updated_at = datetime('now')
		WHERE singleton_id = 1 AND current_switch_id IS NULL
		  AND revision = ?`,
		switchID, manifest.SourceRevision,
	)
	if err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	if changed, err := attached.RowsAffected(); err != nil || changed != 1 {
		return persistedDNSEngineSwitch{}, errors.New(
			"DNS engine state changed before switch attachment",
		)
	}
	for _, transition := range []struct {
		from string
		to   string
	}{
		{from: "planned", to: "staging"},
		{from: "staging", to: "staged"},
		{from: "staged", to: "activating"},
	} {
		if transition.to == "staged" {
			staged, err := tx.ExecContext(ctx, `
				UPDATE dns_engine_switch_zones
				SET phase = 'staged', updated_at = datetime('now')
				WHERE switch_id = ? AND phase = 'pending'`, switchID)
			if err != nil {
				return persistedDNSEngineSwitch{}, err
			}
			if err := requireExactRows(
				staged, int64(len(manifest.Zones)),
				"DNS engine switch zone staging was not exact",
			); err != nil {
				return persistedDNSEngineSwitch{}, err
			}
		}
		advanced, err := tx.ExecContext(ctx, `
			UPDATE dns_engine_switch_snapshots
			SET phase = ?, updated_at = datetime('now')
			WHERE switch_id = ? AND phase = ?`,
			transition.to, switchID, transition.from,
		)
		if err != nil {
			return persistedDNSEngineSwitch{}, err
		}
		if err := requireExactRows(
			advanced, 1, "DNS engine switch phase transition was not exact",
		); err != nil {
			return persistedDNSEngineSwitch{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return persistedDNSEngineSwitch{}, err
	}
	return persistedDNSEngineSwitch{
		SwitchID: switchID, RequestID: request.RequestID, OwnerID: ownerID,
		SourceEngine: manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision,
		Action:         action, Mode: mode, Topology: manifest.Topology,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		Phase:     "activating",
		Qualifier: manifest.Qualifier, ZoneCount: len(manifest.Zones),
		SnapshotBytes: manifest.SnapshotBytes,
	}, nil
}

func (p *Panel) verifyDNSEngineRuntimeTarget(
	ctx context.Context,
	target transport.DNSEngine,
) error {
	runtimes, port53Conflict, err := p.readDNSBackendRuntime(ctx)
	if err != nil {
		return err
	}
	if port53Conflict {
		return errors.New("another process owns public port 53")
	}
	for engine, runtime := range runtimes {
		if engine == target {
			if !runtime.Installed || !runtime.Running || !runtime.Managed {
				return errors.New("target DNS engine is not active and managed")
			}
		} else if runtime.Running {
			return errors.New("source DNS engine still owns port 53")
		}
	}
	return nil
}

func (p *Panel) finalizeDNSEngineSwitchSuccess(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return err
	}
	if state.CurrentSwitchID != persisted.SwitchID {
		return errors.New("DNS engine switch is not attached to singleton state")
	}
	verifying, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_switch_snapshots
		SET phase = 'verifying', updated_at = datetime('now')
		WHERE switch_id = ? AND phase = 'activating'`,
		persisted.SwitchID,
	)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		verifying, 1, "DNS engine switch verification transition was not exact",
	); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT zone_name, desired_generation, desired_action,
		       desired_zone_type, zone_qualifier
		FROM dns_engine_switch_zones
		WHERE switch_id = ? ORDER BY ordinal`,
		persisted.SwitchID,
	)
	if err != nil {
		return err
	}
	type appliedZone struct {
		name, action, zoneType, qualifier string
		generation                        int64
	}
	var zones []appliedZone
	for rows.Next() {
		var zone appliedZone
		if err := rows.Scan(
			&zone.name, &zone.generation, &zone.action,
			&zone.zoneType, &zone.qualifier,
		); err != nil {
			rows.Close()
			return err
		}
		zones = append(zones, zone)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(zones) != persisted.ZoneCount {
		return errors.New("DNS engine switch zone snapshot count changed")
	}
	for _, zone := range zones {
		application, err := tx.ExecContext(ctx, `
			INSERT INTO dns_zone_engine_applications (
			  zone_name, engine, engine_epoch, applied_generation,
			  applied_action, applied_zone_type, qualifier,
			  mutation_request_id, mutation_owner_id, switch_id, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(zone_name, engine) DO UPDATE SET
			  engine_epoch = excluded.engine_epoch,
			  applied_generation = excluded.applied_generation,
			  applied_action = excluded.applied_action,
			  applied_zone_type = excluded.applied_zone_type,
			  qualifier = excluded.qualifier,
			  mutation_request_id = excluded.mutation_request_id,
			  mutation_owner_id = excluded.mutation_owner_id,
			  switch_id = excluded.switch_id,
			  revision = dns_zone_engine_applications.revision + 1,
			  applied_at = datetime('now'), updated_at = datetime('now')`,
			zone.name, persisted.TargetEngine, persisted.TargetEpoch,
			zone.generation, zone.action, zone.zoneType, zone.qualifier,
			persisted.RequestID, persisted.OwnerID, persisted.SwitchID,
		)
		if err != nil {
			return err
		}
		if err := requireExactRows(
			application, 1,
			"DNS engine zone application finalization was not exact",
		); err != nil {
			return err
		}
		applied, err := tx.ExecContext(ctx, `
			UPDATE dns_zone_sync_state
			SET applied_generation = desired_generation, status = 'applied',
			    last_error = NULL, updated_at = datetime('now')
			WHERE zone_name = ? AND desired_generation = ?
			  AND desired_action = ? AND desired_zone_type = ?`,
			zone.name, zone.generation, zone.action, zone.zoneType,
		)
		if err != nil {
			return err
		}
		if changed, err := applied.RowsAffected(); err != nil || changed != 1 {
			return errors.New(
				"frozen DNS zone state changed before switch finalization",
			)
		}
		if zone.action == "delete" {
			retired, err := tx.ExecContext(ctx, `
				DELETE FROM dns_zone_deletion_markers WHERE zone_name = ?`,
				zone.name,
			)
			if err != nil {
				return fmt.Errorf(
					"retire applied DNS engine deletion marker: %w", err,
				)
			}
			if err := requireExactRows(
				retired, 1,
				"applied DNS engine deletion marker was not retired exactly once",
			); err != nil {
				return err
			}
		}
	}
	verified, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_switch_zones
		SET phase = 'verified', last_error = NULL, updated_at = datetime('now')
		WHERE switch_id = ? AND phase = 'staged'`, persisted.SwitchID)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		verified, int64(persisted.ZoneCount),
		"DNS engine switch zone verification was not exact",
	); err != nil {
		return err
	}
	if err := advanceDNSEngineOperationMarkerTx(
		ctx, tx, persisted,
		dnsEngineOperationAccepted, dnsEngineOperationPostCommit,
	); err != nil {
		return err
	}
	committed, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_switch_snapshots
		SET phase = 'committed', updated_at = datetime('now')
		WHERE switch_id = ? AND phase = 'verifying'`,
		persisted.SwitchID,
	)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		committed, 1, "DNS engine switch commit transition was not exact",
	); err != nil {
		return err
	}
	storageTopology := persisted.Topology
	if persisted.Mode == transport.DNSEngineSwitchModeSwitch &&
		persisted.Topology == transport.DNSTopologyPaired {
		storageTopology = transport.DNSTopologyStandalone
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO dns_bind_pair_state (
			  singleton_id, active_epoch, pair_role, local_ip, local_ns,
			  peer_ip, peer_ns, source_switch_id
			) VALUES (1, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(singleton_id) DO UPDATE SET
			  active_epoch = excluded.active_epoch,
			  pair_role = excluded.pair_role,
			  local_ip = excluded.local_ip,
			  local_ns = excluded.local_ns,
			  peer_ip = excluded.peer_ip,
			  peer_ns = excluded.peer_ns,
			  source_switch_id = excluded.source_switch_id,
			  updated_at = datetime('now')`,
			persisted.TargetEpoch, persisted.PairRole, persisted.LocalIP,
			persisted.LocalNS, persisted.PeerIP, persisted.PeerNS,
			persisted.SwitchID,
		); err != nil {
			return err
		}
	}
	detached, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_state
		SET active_engine = ?, active_epoch = ?, topology = ?,
		    current_switch_id = NULL,
		    revision = revision + 1, updated_at = datetime('now')
		WHERE singleton_id = 1 AND current_switch_id = ?`,
		persisted.TargetEngine, persisted.TargetEpoch, storageTopology,
		persisted.SwitchID,
	)
	if err != nil {
		return err
	}
	if changed, err := detached.RowsAffected(); err != nil || changed != 1 {
		return errors.New("DNS engine switch singleton finalization was not exact")
	}
	return tx.Commit()
}

func (p *Panel) rollbackDNSEngineSwitch(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := readDNSEngineSwitchByRequest(ctx, tx, persisted.RequestID)
	if err != nil {
		return err
	}
	if err := attachDNSEngineOperationAction(ctx, tx, &current); err != nil {
		return err
	}
	const safeFailure = "DNS engine switch did not complete"
	switch current.Phase {
	case "planned":
		failed, err := tx.ExecContext(ctx, `
			UPDATE dns_engine_switch_snapshots
			SET phase = 'failed', last_error = ?, updated_at = datetime('now')
			WHERE switch_id = ? AND phase = 'planned'`,
			safeFailure, current.SwitchID,
		)
		if err != nil {
			return err
		}
		if err := requireExactRows(
			failed, 1, "DNS engine switch failure transition was not exact",
		); err != nil {
			return err
		}
	case "staging", "staged", "activating", "verifying":
		rollingBack, err := tx.ExecContext(ctx, `
			UPDATE dns_engine_switch_snapshots
			SET phase = 'rolling_back', last_error = ?,
			    updated_at = datetime('now')
			WHERE switch_id = ? AND phase = ?`,
			safeFailure, current.SwitchID, current.Phase,
		)
		if err != nil {
			return err
		}
		if err := requireExactRows(
			rollingBack, 1,
			"DNS engine switch rollback transition was not exact",
		); err != nil {
			return err
		}
		rolledBack, err := tx.ExecContext(ctx, `
			UPDATE dns_engine_switch_snapshots
			SET phase = 'rolled_back', updated_at = datetime('now')
			WHERE switch_id = ? AND phase = 'rolling_back'`,
			current.SwitchID,
		)
		if err != nil {
			return err
		}
		if err := requireExactRows(
			rolledBack, 1,
			"DNS engine switch rollback completion was not exact",
		); err != nil {
			return err
		}
	case "failed", "rolled_back":
		// A previous recovery attempt already made the host outcome terminal.
	case "committed":
		return errors.New("committed DNS engine switch cannot be rolled back in the panel")
	default:
		return errors.New("DNS engine switch has an unknown phase")
	}
	detached, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_state
		SET current_switch_id = NULL, revision = revision + 1,
		    updated_at = datetime('now')
		WHERE singleton_id = 1 AND current_switch_id = ?`,
		current.SwitchID,
	)
	if err != nil {
		return err
	}
	if changed, err := detached.RowsAffected(); err != nil || changed != 1 {
		return errors.New("DNS engine rollback singleton finalization was not exact")
	}
	if err := clearDNSEngineOperationMarkerTx(
		ctx, tx, current, dnsEngineOperationAccepted,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Panel) verifyDNSEngineRollbackRuntime(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	runtimes, port53Conflict, err := p.readDNSBackendRuntime(ctx)
	if err != nil {
		return err
	}
	if port53Conflict {
		return errors.New("another process owns public port 53")
	}
	if persisted.Mode == transport.DNSEngineSwitchModeAdopt {
		target := runtimes[persisted.TargetEngine]
		if persisted.SourceEngine != "" ||
			persisted.TargetEngine != transport.DNSEnginePowerDNS ||
			!target.Installed || !target.Running || !target.Managed {
			return errors.New("registration-only PowerDNS adoption rollback is not proven")
		}
		for engine, runtime := range runtimes {
			if engine != persisted.TargetEngine && runtime.Running {
				return errors.New("another DNS engine is running after adoption failure")
			}
		}
		return nil
	}
	if persisted.SourceEngine == "" {
		for _, runtime := range runtimes {
			if runtime.Running {
				return errors.New(
					"an authoritative DNS runtime remains active after initial switch failure",
				)
			}
		}
		return nil
	}
	source := runtimes[persisted.SourceEngine]
	if !source.Installed || !source.Running || !source.Managed {
		return errors.New("source DNS engine restoration is not proven")
	}
	for engine, runtime := range runtimes {
		if engine != persisted.SourceEngine && runtime.Running {
			return errors.New("target DNS engine remains active after rollback")
		}
	}
	return nil
}

func (p *Panel) reconstructPersistedDNSEngineManifest(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT ordinal, zone_name, desired_generation, desired_action,
		       desired_zone_type, zone_qualifier, records_json, records_bytes
		FROM dns_engine_switch_zones
		WHERE switch_id = ? ORDER BY ordinal`, persisted.SwitchID)
	if err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	defer rows.Close()
	zones := make([]transport.DNSEngineSwitchZoneSnapshot, 0, persisted.ZoneCount)
	var totalBytes int64
	for rows.Next() {
		var zone transport.DNSEngineSwitchZoneSnapshot
		var action, recordsJSON string
		var recordsBytes int64
		if err := rows.Scan(
			&zone.Ordinal, &zone.Domain, &zone.DesiredGeneration,
			&action, &zone.ZoneType, &zone.ZoneQualifier,
			&recordsJSON, &recordsBytes,
		); err != nil {
			return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
		}
		if recordsBytes != int64(len([]byte(recordsJSON))) ||
			recordsBytes > mutationpayload.DNSEngineSwitchMaxSnapshotBytes-totalBytes {
			return mutationpayload.DNSEngineSwitchManifestCommitment{},
				errors.New("persisted DNS engine records size is invalid")
		}
		totalBytes += recordsBytes
		if err := json.Unmarshal([]byte(recordsJSON), &zone.Records); err != nil {
			return mutationpayload.DNSEngineSwitchManifestCommitment{},
				errors.New("persisted DNS engine records are invalid")
		}
		switch action {
		case "sync":
			zone.Delete = false
		case "delete":
			zone.Delete = true
		default:
			return mutationpayload.DNSEngineSwitchManifestCommitment{},
				errors.New("persisted DNS engine zone action is invalid")
		}
		zones = append(zones, zone)
	}
	if err := rows.Err(); err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	if len(zones) != persisted.ZoneCount || totalBytes != persisted.SnapshotBytes {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("persisted DNS engine snapshot size or count mismatch")
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		persisted.Mode,
		persisted.SourceEngine, persisted.TargetEngine,
		persisted.SourceEpoch, persisted.TargetEpoch,
		persisted.SourceRevision, persisted.Topology,
		persisted.PairRole, persisted.LocalIP, persisted.LocalNS,
		persisted.PeerIP, persisted.PeerNS, zones,
	)
	if err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	if manifest.Qualifier != persisted.Qualifier ||
		manifest.SnapshotBytes != persisted.SnapshotBytes {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("persisted DNS engine manifest qualifier mismatch")
	}
	return manifest, nil
}

func dnsEngineSwitchRequestForManifest(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) transport.SwitchDNSEngineV1Request {
	return transport.SwitchDNSEngineV1Request{
		Mode:         manifest.Mode,
		SourceEngine: manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		Zones: manifest.Zones, SnapshotBytes: manifest.SnapshotBytes,
		ManifestQualifier: manifest.Qualifier,
	}
}

func (p *Panel) executeDNSEngineSwitch(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	request := dnsEngineSwitchRequestForManifest(manifest)
	var response transport.SwitchDNSEngineV1Response
	op := serviceOperation{
		RequestID: persisted.RequestID, Kind: dnsEngineSwitchKind,
		ServiceID:   string(persisted.TargetEngine),
		PackageName: persisted.Qualifier,
	}
	err := p.withStandaloneAgentMutationIdentity(
		ctx, op, persisted.OwnerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			request.ServiceMutationBinding = binding
			if err := p.callAgentContext(
				callCtx, "Agent.SwitchDNSEngineV1", &request, &response,
			); err != nil {
				return err
			}
			if response.Error != "" {
				return errors.New("agent rejected DNS engine switch")
			}
			if !response.Applied ||
				response.ActiveEngine != manifest.TargetEngine ||
				response.ActiveEpoch != manifest.TargetEpoch ||
				response.AppliedZones != len(manifest.Zones) {
				return errors.New("agent did not confirm the exact DNS engine switch")
			}
			return nil
		},
	)
	if err != nil {
		return err
	}
	if err := p.verifyDNSEngineRuntimeTarget(ctx, persisted.TargetEngine); err != nil {
		return &dnsEngineFinalizeUncertainError{err: err}
	}
	if err := p.finalizeDNSEngineSwitchSuccess(ctx, persisted); err != nil {
		return &dnsEngineFinalizeUncertainError{err: err}
	}
	return nil
}

type dnsEngineFinalizeUncertainError struct {
	err error
}

func (err *dnsEngineFinalizeUncertainError) Error() string {
	return "DNS engine host switch succeeded but panel finalization is pending"
}

func (err *dnsEngineFinalizeUncertainError) Unwrap() error {
	return err.err
}

func (p *Panel) matchingDNSEngineSwitchReplay(
	ctx context.Context,
	request dnsEngineSwitchRequest,
) (bool, bool, error) {
	persisted, err := readDNSEngineSwitchByRequest(
		ctx, p.db.GetDB(), request.RequestID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true,
		persisted.TargetEngine == request.TargetEngine &&
			persisted.SourceEngine == request.ExpectedSource.engine() &&
			persisted.SourceRevision == request.ExpectedRevision,
		nil
}

func (p *Panel) handleDNSEngineSwitch(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request dnsEngineSwitchRequest
	if err := decodeServiceOperationJSON(w, r, &request); err != nil ||
		!validServiceOperationID(request.RequestID) ||
		!transport.ValidDNSEngine(request.TargetEngine) ||
		!request.ExpectedSource.Set || request.ExpectedRevision < 0 ||
		!validServiceOperationID(request.PreviewToken) {
		writeClientError(w, http.StatusBadRequest, "invalid DNS engine switch request")
		return
	}
	actor := dnsEngineActorFromRequest(r)
	found, matching, err := p.matchingDNSEngineSwitchReplay(r.Context(), request)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if found {
		if !matching {
			writeClientError(w, http.StatusConflict,
				"request_id was already used for a different DNS engine change")
			return
		}
		p.serviceMutationMu.Lock()
		defer p.serviceMutationMu.Unlock()
		p.dnsTopologyMu.Lock()
		defer p.dnsTopologyMu.Unlock()
		dnsPublicationMu.Lock()
		defer dnsPublicationMu.Unlock()

		persisted, err := readDNSEngineSwitchByRequest(
			r.Context(), p.db.GetDB(), request.RequestID,
		)
		if err != nil {
			writeServerError(w, err)
			return
		}
		marker, err := readDNSEngineOperationMarker(
			r.Context(), p.db.GetDB(),
		)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if marker != nil && marker.RequestID == persisted.RequestID &&
			marker.SwitchID == persisted.SwitchID &&
			marker.Phase == dnsEngineOperationPostCommit {
			if err := attachDNSEngineOperationAction(
				r.Context(), p.db.GetDB(), &persisted,
			); err != nil {
				writeServerError(w, err)
				return
			}
			result := p.reconcileDNSEnginePostCommitLocked(
				r.Context(), persisted,
			)
			if result.failed() {
				log.Printf(
					"DNS engine replay post-commit %s: firewall=%v scan=%v",
					persisted.SwitchID, result.FirewallErr, result.ScanErr,
				)
				p.auditDNSEngineBounded(actor, "post_commit.pending", persisted)
				writeDNSEnginePostCommitFailed(w, result)
				return
			}
			p.auditDNSEngineBounded(actor, "post_commit.recovered", persisted)
		}
		snapshot, err := p.dnsEngineSnapshot(r.Context())
		if err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(snapshot)
		return
	}
	authority, ok := p.dnsEnginePreviews.consume(request.PreviewToken)
	if !ok ||
		authority.Target != request.TargetEngine ||
		authority.Source != request.ExpectedSource.engine() ||
		authority.Revision != request.ExpectedRevision {
		writeClientError(w, http.StatusConflict,
			"DNS engine preview expired or no longer matches this request")
		return
	}

	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	p.dnsTopologyMu.Lock()
	defer p.dnsTopologyMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()

	if err := p.requireNoPendingDNSClusterSaga(r.Context()); err != nil {
		writeClientError(w, http.StatusConflict,
			"another DNS topology operation must finish first")
		return
	}
	if err := p.requireDNSEngineSwitchV1Agent(r.Context()); err != nil {
		writeClientError(w, http.StatusConflict,
			"the paired agent is not ready for DNS engine switching")
		return
	}
	snapshot, err := p.dnsEngineSnapshot(r.Context())
	if err != nil {
		writeServerError(w, err)
		return
	}
	blockers := dnsEnginePreviewBlockers(
		snapshot, request.TargetEngine, request.ExpectedSource.engine(),
		request.ExpectedRevision,
	)
	if len(blockers) != 0 {
		writeClientError(w, http.StatusConflict,
			"DNS engine state changed or is not safe to switch")
		return
	}
	action := dnsEngineAction(snapshot, request.TargetEngine)
	if action != authority.Action {
		writeClientError(w, http.StatusConflict,
			"DNS engine state changed after preview; review the change again")
		return
	}
	if request.ExpectedSource.Valid &&
		!request.DowntimeAcknowledged {
		writeClientError(w, http.StatusBadRequest,
			"downtime acknowledgement is required")
		return
	}
	state, err := readDNSEngineDBState(r.Context(), p.db.GetDB())
	if err != nil {
		writeServerError(w, err)
		return
	}
	manifest, err := p.buildDNSEngineManifest(
		r.Context(), state, request.TargetEngine, action, snapshot.Topology,
	)
	if err != nil {
		writeClientError(w, http.StatusConflict,
			"DNS zones changed after preview; review the change again")
		return
	}
	if manifest.Qualifier != authority.ManifestQualifier ||
		manifest.SnapshotBytes != authority.SnapshotBytes {
		writeClientError(w, http.StatusConflict,
			"DNS zones changed after preview; review the change again")
		return
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		writeServerError(w, err)
		return
	}
	switchID, err := newServiceOperationID()
	if err != nil {
		writeServerError(w, err)
		return
	}
	persisted, err := p.persistDNSEngineSwitch(
		r.Context(), request, ownerID, switchID, action, manifest,
	)
	if err != nil {
		writeServerError(w, fmt.Errorf("persist DNS engine switch: %w", err))
		return
	}
	p.auditDNSEngineBounded(actor, "accepted", persisted)
	workerCtx, cancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchTimeout,
	)
	defer cancel()
	err = p.executeDNSEngineSwitch(workerCtx, persisted, manifest)
	if err != nil {
		p.auditDNSEngineBounded(actor, "failed", persisted)
		var uncertain *dnsEngineFinalizeUncertainError
		if !mutationTerminalUncertain(err) && !errors.As(err, &uncertain) {
			proofErr := p.verifyDNSEngineRollbackRuntime(workerCtx, persisted)
			if proofErr == nil {
				proofErr = p.rollbackDNSEngineSwitch(workerCtx, persisted)
			}
			if proofErr != nil {
				p.auditDNSEngineBounded(actor, "uncertain", persisted)
				log.Printf(
					"DNS engine switch %s failed and rollback finalization failed: %v / %v",
					persisted.SwitchID, err, proofErr,
				)
			} else {
				p.auditDNSEngineBounded(actor, "rolled_back", persisted)
			}
		} else {
			p.auditDNSEngineBounded(actor, "uncertain", persisted)
		}
		log.Printf("DNS engine switch %s did not finalize: %v", persisted.SwitchID, err)
		writeClientError(w, http.StatusConflict,
			"DNS engine change did not complete; refresh to see its verified state")
		return
	}
	p.auditDNSEngineBounded(actor, "succeeded", persisted)
	postCommit := p.reconcileDNSEnginePostCommitLocked(workerCtx, persisted)
	if postCommit.failed() {
		log.Printf(
			"DNS engine switch %s committed with pending follow-up: firewall=%v scan=%v",
			persisted.SwitchID, postCommit.FirewallErr, postCommit.ScanErr,
		)
		p.auditDNSEngineBounded(actor, "post_commit.pending", persisted)
		writeDNSEnginePostCommitFailed(w, postCommit)
		return
	}
	p.auditDNSEngineBounded(actor, "post_commit.completed", persisted)
	finalSnapshot, err := p.dnsEngineSnapshot(workerCtx)
	if err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(finalSnapshot)
}

func validDirectDNSEngineSwitch(job *agentMutationJob) bool {
	return job != nil &&
		validServiceOperationID(job.RequestID) &&
		validServiceOperationID(job.OwnerID) &&
		job.Kind == dnsEngineSwitchKind &&
		transport.ValidDNSEngine(transport.DNSEngine(job.Target)) &&
		mutationpayload.ValidDNSEngineSwitchQualifier(job.PackageName)
}

// recoverDNSEngineSwitchLocked reconciles the snapshot committed before
// BeginServiceMutation. The caller holds serviceMutationMu during startup.
func (p *Panel) recoverDNSEngineSwitchLocked(
	ctx context.Context,
	globalJob *agentMutationJob,
) (bool, error) {
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return false, err
	}
	marker, err := readDNSEngineOperationMarker(ctx, p.db.GetDB())
	if err != nil {
		return false, fmt.Errorf("read DNS engine recovery marker: %w", err)
	}
	if state.CurrentSwitchID == "" {
		if marker != nil {
			if marker.Phase != dnsEngineOperationPostCommit {
				return true, errors.New(
					"detached DNS engine operation has not reached post-commit",
				)
			}
			persisted, readErr := readDNSEngineSwitchByID(
				ctx, p.db.GetDB(), marker.SwitchID,
			)
			if readErr != nil {
				return true, readErr
			}
			if err := attachDNSEngineOperationAction(
				ctx, p.db.GetDB(), &persisted,
			); err != nil {
				return true, err
			}
			if persisted.RequestID != marker.RequestID ||
				persisted.SourceEngine != marker.SourceEngine ||
				persisted.TargetEngine != marker.TargetEngine ||
				persisted.Phase != "committed" ||
				state.ActiveEngine != persisted.TargetEngine ||
				state.EngineEpoch != persisted.TargetEpoch ||
				state.Topology != persisted.Topology {
				return true, errors.New(
					"DNS engine post-commit marker does not match active authority",
				)
			}
			if globalJob != nil && agentMutationActive(globalJob.Status) {
				if !validDirectFirewallMutation(globalJob) {
					p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
					return true, errors.New(
						"active mutation does not match DNS engine post-commit recovery",
					)
				}
				if err := p.terminalizeInterruptedFirewallMutation(
					ctx, globalJob,
				); err != nil {
					p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
					return true, err
				}
			}
			result := p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
			if result.failed() {
				log.Printf(
					"DNS engine startup post-commit %s remains pending: firewall=%v scan=%v",
					persisted.SwitchID, result.FirewallErr, result.ScanErr,
				)
				p.auditDNSEngineSystem(
					ctx, "recovered.post_commit.pending", persisted,
				)
				// The engine is already committed and serving. Keep the durable
				// marker for the next replay/startup without preventing the
				// panel from starting.
				return true, nil
			}
			p.auditDNSEngineSystem(
				ctx, "recovered.post_commit.completed", persisted,
			)
			return true, nil
		}
		if globalJob != nil && agentMutationActive(globalJob.Status) &&
			validDirectDNSEngineSwitch(globalJob) {
			return true, errors.New(
				"active DNS engine mutation has no attached panel snapshot",
			)
		}
		return false, nil
	}
	persisted, err := readDNSEngineSwitchByID(
		ctx, p.db.GetDB(), state.CurrentSwitchID,
	)
	if err != nil {
		return true, err
	}
	if marker == nil || marker.Phase != dnsEngineOperationAccepted ||
		marker.SwitchID != persisted.SwitchID ||
		marker.RequestID != persisted.RequestID {
		return true, errors.New(
			"active DNS engine switch has no exact accepted marker",
		)
	}
	if err := attachDNSEngineOperationAction(
		ctx, p.db.GetDB(), &persisted,
	); err != nil {
		return true, err
	}
	if _, err := p.reconstructPersistedDNSEngineManifest(ctx, persisted); err != nil {
		return true, fmt.Errorf("verify persisted DNS engine manifest: %w", err)
	}
	identity := agentMutationIdentity{
		RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
		Kind: dnsEngineSwitchKind, Target: string(persisted.TargetEngine),
		PackageName: persisted.Qualifier,
	}
	job := globalJob
	if job != nil && agentMutationActive(job.Status) && !identity.matches(job) {
		return true, errors.New(
			"active agent mutation does not match the attached DNS engine switch",
		)
	}
	if job == nil || !identity.matches(job) {
		job, err = p.statusAgentMutation(ctx, persisted.RequestID)
		if err != nil {
			return true, fmt.Errorf("read DNS engine mutation during recovery: %w", err)
		}
	}
	if job != nil && !identity.matches(job) {
		return true, errAgentMutationIdentityMismatch
	}
	if job != nil && agentMutationActive(job.Status) {
		job, err = p.waitExpectedAgentMutationTerminal(ctx, identity)
		if err != nil {
			return true, fmt.Errorf("wait for DNS engine switch recovery: %w", err)
		}
	}
	if job != nil && job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return true, err
		}
		if err := p.verifyDNSEngineRuntimeTarget(ctx, persisted.TargetEngine); err != nil {
			return true, fmt.Errorf("verify recovered DNS engine runtime: %w", err)
		}
		if err := p.finalizeDNSEngineSwitchSuccess(ctx, persisted); err != nil {
			p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
			return true, fmt.Errorf("finalize recovered DNS engine switch: %w", err)
		}
		p.auditDNSEngineSystem(ctx, "recovered.succeeded", persisted)
		result := p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
		if result.failed() {
			log.Printf(
				"recovered DNS engine switch %s has pending follow-up: firewall=%v scan=%v",
				persisted.SwitchID, result.FirewallErr, result.ScanErr,
			)
			p.auditDNSEngineSystem(
				ctx, "recovered.post_commit.pending", persisted,
			)
			return true, nil
		}
		p.auditDNSEngineSystem(
			ctx, "recovered.post_commit.completed", persisted,
		)
		return true, nil
	}
	if job != nil && agentMutationActive(job.Status) {
		return true, errors.New("DNS engine mutation remained active after recovery wait")
	}
	if err := p.verifyDNSEngineRollbackRuntime(ctx, persisted); err != nil {
		p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
		return true, fmt.Errorf("verify DNS engine rollback during recovery: %w", err)
	}
	p.auditDNSEngineSystem(ctx, "recovered.failed", persisted)
	if err := p.rollbackDNSEngineSwitch(ctx, persisted); err != nil {
		p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
		return true, fmt.Errorf("finalize DNS engine rollback during recovery: %w", err)
	}
	p.auditDNSEngineSystem(ctx, "recovered.rolled_back", persisted)
	return true, nil
}
