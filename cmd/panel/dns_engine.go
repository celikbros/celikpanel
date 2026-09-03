package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
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

type dnsEngineOperationSnapshot struct {
	RequestID    string              `json:"request_id"`
	ID           string              `json:"id"`
	TargetEngine transport.DNSEngine `json:"target_engine"`
	Phase        string              `json:"phase"`
	Status       string              `json:"status"`
	StartedAt    string              `json:"started_at"`
	UpdatedAt    string              `json:"updated_at"`
	LastError    string              `json:"last_error,omitempty"`
}

type dnsEngineSnapshot struct {
	Revision         int64                       `json:"revision"`
	EngineEpoch      int64                       `json:"engine_epoch"`
	ActiveEngine     *transport.DNSEngine        `json:"active_engine"`
	State            string                      `json:"state"`
	Topology         string                      `json:"topology"`
	PairRole         string                      `json:"pair_role,omitempty"`
	PairReady        *bool                       `json:"pair_ready,omitempty"`
	DNSSECZoneCount  int                         `json:"dnssec_zone_count"`
	ZoneCount        int                         `json:"zone_count"`
	PendingZoneCount int                         `json:"pending_zone_count"`
	OperationID      string                      `json:"operation_id,omitempty"`
	Operation        *dnsEngineOperationSnapshot `json:"operation,omitempty"`
	Engines          []dnsEngineEntry            `json:"engines"`
	runtime          map[transport.DNSEngine]transport.DNSBackendRuntimeState
	port53Conflict   bool
	runtimeErr       error
	dnssecErr        error
	pairIdentityErr  error
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

// The mutation hold travels with readiness rather than through a second probe:
// two round trips can disagree, and a presentation built from a disagreeing pair
// is exactly the class of bug this file keeps producing.
// Mutasyon tutması ikinci bir yoklamayla değil hazırlıkla birlikte gelir: iki
// tur birbiriyle çelişebilir ve çelişen bir çiftten kurulan bir sunum, tam da bu
// dosyanın üretmeye devam ettiği hata sınıfıdır.
func validateDNSBackendReadiness(
	response transport.DNSBackendReadinessResponse,
) (map[transport.DNSEngine]transport.DNSBackendRuntimeState, bool, string, error) {
	if response.Error != "" || len(response.Engines) != 2 {
		return nil, false, "", errors.New("DNS backend readiness is unavailable")
	}
	result := make(map[transport.DNSEngine]transport.DNSBackendRuntimeState, 2)
	for _, runtime := range response.Engines {
		if !transport.ValidDNSEngine(runtime.Engine) {
			return nil, false, "", errors.New("DNS backend readiness contains an unknown engine")
		}
		if _, duplicate := result[runtime.Engine]; duplicate {
			return nil, false, "", errors.New("DNS backend readiness contains a duplicate engine")
		}
		if runtime.Running && !runtime.Installed ||
			runtime.Managed && !runtime.Installed ||
			runtime.PairReady && (!runtime.Installed || !runtime.Running || !runtime.Managed) ||
			len(runtime.Unit) > 128 ||
			strings.ContainsAny(runtime.Unit, "\r\n\x00") {
			return nil, false, "", errors.New("DNS backend readiness is internally inconsistent")
		}
		result[runtime.Engine] = runtime
	}
	if _, ok := result[transport.DNSEnginePowerDNS]; !ok {
		return nil, false, "", errors.New("PowerDNS readiness is missing")
	}
	if _, ok := result[transport.DNSEngineBIND]; !ok {
		return nil, false, "", errors.New("BIND readiness is missing")
	}
	return result, response.Port53Conflict, response.MutationHold, nil
}

func (p *Panel) readDNSBackendRuntime(
	ctx context.Context,
) (map[transport.DNSEngine]transport.DNSBackendRuntimeState, bool, string, error) {
	var response transport.DNSBackendReadinessResponse
	if err := p.callAgentContext(
		ctx, "Agent.DNSBackendReadiness", &transport.Empty{}, &response,
	); err != nil {
		return nil, false, "", fmt.Errorf("read DNS backend readiness: %w", err)
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

// mutationHold carries the agent's reason for refusing durable mutations, or ""
// when it accepts them. It changes one thing here and it is the thing that
// matters: an engine the panel installed reads as Managed=false while the
// transaction that would have claimed it is stuck, and without this the screen
// reports a foreign DNS server. "Our own change system is held" and "someone
// else installed a DNS server" are opposite diagnoses with opposite fixes, and
// sending an operator after the second when the first is true is how an
// afternoon disappears.
//
// The status stays inside the existing closed set; only the detail code, which
// is free-form by contract, carries the correction. Inventing a status here
// would be rejected by the frontend validator.
//
// mutationHold, agent'ın kalıcı mutasyonları reddetme sebebini taşır; kabul
// ediyorsa "" olur. Burada tek bir şeyi değiştirir ve önemli olan da odur:
// panelin kurduğu bir motor, onu sahiplenecek işlem takılıyken Managed=false
// görünür ve bu olmadan ekran yabancı bir DNS sunucusu bildirir. "Kendi
// değişiklik sistemimiz tutuluyor" ile "başkası bir DNS sunucusu kurmuş" zıt
// teşhislerdir; birincisi doğruyken operatörü ikincisinin peşine göndermek bir
// öğleden sonrayı yok eder.
//
// Durum mevcut kapalı kümenin içinde kalır; düzeltmeyi, sözleşme gereği serbest
// biçimli olan detay kodu taşır. Burada yeni bir durum uydurmak, arayüz
// doğrulayıcısı tarafından reddedilirdi.
func deriveDNSEnginePresentation(
	state dnsEngineDBState,
	runtimes map[transport.DNSEngine]transport.DNSBackendRuntimeState,
	runtimeErr error,
	mutationHold string,
) (string, []dnsEngineEntry) {
	unmanagedCode := "unmanaged_dns_detected"
	if mutationHold != "" {
		unmanagedCode = "mutations_held"
	}
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
				entry.DetailCode = unmanagedCode
			} else {
				entry.Status = "conflict"
				entry.DetailCode = "active_engine_mismatch"
			}
		case runtime.Installed:
			if !runtime.Managed {
				entry.Status = "unmanaged"
				entry.DetailCode = unmanagedCode
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
	state, operation, err := p.readDNSEngineStateAndOperation(ctx)
	if err != nil {
		return dnsEngineSnapshot{}, err
	}
	zoneCount, pendingCount, zones, err := p.dnsEngineZoneCounts(ctx)
	if err != nil {
		return dnsEngineSnapshot{}, fmt.Errorf("read DNS engine zone counts: %w", err)
	}
	runtimes, port53Conflict, mutationHold, runtimeErr := p.readDNSBackendRuntime(ctx)
	dnssecCount := 0
	var dnssecErr error
	// BIND is currently activated only by an exact unsigned-zone switch
	// snapshot. Do not probe the stopped PowerDNS backend as ongoing BIND
	// health; engine-aware DNSSEC publication will replace this proof when
	// BIND signing support is introduced.
	//
	// A host with no active engine and no PowerDNS installed has nothing that
	// could have signed a zone, and no backend to ask. Asking anyway turned
	// every pre-existing zone into "DNSSEC readiness is unavailable", which
	// the presentation then reported as degraded, and the first-install
	// preview refused with dnssec_unsupported, target_unavailable and
	// source_degraded at once (S-8 T1 on Arch, reproduced on the same shape
	// on Debian; register R-029, third layer). A legacy PowerDNS that is
	// installed but not yet adopted is still probed: its zones may well be
	// signed, and that is exactly what the blocker exists for.
	//
	// Etkin motoru ve kurulu PowerDNS'i olmayan sunucuda bir bölgeyi
	// imzalamış olabilecek hiçbir şey ve sorulacak bir arka uç yoktur. Yine
	// de sormak, önceden var olan her bölgeyi "DNSSEC hazırlığı
	// kullanılamıyor"a çeviriyordu; sunum bunu "degraded" bildiriyor ve ilk
	// kurulum önizlemesi dnssec_unsupported, target_unavailable ve
	// source_degraded ile aynı anda reddediyordu (S-8 T1 Arch'ta, aynı
	// biçimde Debian'da yeniden üretildi; defter R-029, üçüncü kat).
	// Kurulu ama henüz devralınmamış eski bir PowerDNS yine sorgulanır:
	// bölgeleri pekâlâ imzalı olabilir; engelleyici tam bunun için vardır.
	probeDNSSEC := state.ActiveEngine == transport.DNSEnginePowerDNS ||
		(state.ActiveEngine == "" && runtimeErr == nil &&
			runtimes[transport.DNSEnginePowerDNS].Installed)
	if probeDNSSEC {
		dnssecCount, dnssecErr = p.dnsEngineDNSSECCount(ctx, zones)
	}
	presentationState, entries := deriveDNSEnginePresentation(
		state, runtimes, runtimeErr, mutationHold,
	)
	if dnssecErr != nil && presentationState != dnsEngineStateSwitching {
		presentationState = dnsEngineStateDegraded
	}
	topology := p.dnsEngineTopology(ctx)
	if state.ActiveEngine != "" {
		topology = state.Topology
	}
	pairRole := state.PairRole
	var pairIdentityErr error
	if state.ActiveEngine == "" && topology == transport.DNSTopologyPaired {
		pairRole, pairIdentityErr = p.unresolvedDNSPairRole(ctx)
	}
	var pairReady *bool
	if state.ActiveEngine != "" && state.Topology == transport.DNSTopologyPaired {
		ready := runtimes[state.ActiveEngine].PairReady
		pairReady = &ready
	}
	if operation != nil && state.CurrentSwitchID != "" {
		p.enrichAttachedDNSEngineOperation(ctx, operation, state.CurrentSwitchID)
	}
	current, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return dnsEngineSnapshot{}, fmt.Errorf("recheck DNS engine identity: %w", err)
	}
	if current != state {
		return dnsEngineSnapshot{}, errors.New(
			"DNS engine state changed while its snapshot was being prepared",
		)
	}
	return dnsEngineSnapshot{
		Revision: state.Revision, EngineEpoch: state.EngineEpoch,
		ActiveEngine: enginePointer(state.ActiveEngine),
		State:        presentationState, Topology: topology, PairRole: pairRole,
		PairReady:       pairReady,
		DNSSECZoneCount: dnssecCount, ZoneCount: zoneCount,
		PendingZoneCount: pendingCount, OperationID: state.CurrentSwitchID,
		Operation: operation,
		Engines:   entries, runtime: runtimes, port53Conflict: port53Conflict,
		runtimeErr:      runtimeErr,
		dnssecErr:       dnssecErr,
		pairIdentityErr: pairIdentityErr,
	}, nil
}

func (p *Panel) readDNSEngineStateAndOperation(
	ctx context.Context,
) (dnsEngineDBState, *dnsEngineOperationSnapshot, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return dnsEngineDBState{}, nil, err
	}
	defer tx.Rollback()
	state, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return dnsEngineDBState{}, nil, fmt.Errorf("read DNS engine identity: %w", err)
	}
	operation, err := readPresentedDNSEngineOperation(
		ctx, tx, state.CurrentSwitchID,
	)
	if err != nil {
		return dnsEngineDBState{}, nil, fmt.Errorf("read DNS engine operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return dnsEngineDBState{}, nil, err
	}
	return state, operation, nil
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
	runtimes, port53Conflict, _, err := p.readDNSBackendRuntime(ctx)
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
	if response.Error != "" {
		if response.Synced || response.RecoveryPending ||
			response.Engine != "" || response.EngineEpoch != 0 ||
			response.AppliedGeneration != 0 {
			return errors.New("agent returned a mixed DNS publication failure response")
		}
		return errors.New("agent did not confirm the exact DNS publication")
	}
	if response.RecoveryPending {
		if response.Synced || response.Engine != request.Engine ||
			response.EngineEpoch != request.EngineEpoch ||
			response.AppliedGeneration != request.DesiredGeneration {
			return errors.New("agent returned an invalid pending DNS publication receipt")
		}
		return &dnsZoneV3PropagationPendingError{}
	}
	if !response.Synced ||
		response.Engine != request.Engine ||
		response.EngineEpoch != request.EngineEpoch ||
		response.AppliedGeneration != request.DesiredGeneration {
		return errors.New("agent did not confirm the exact DNS publication")
	}
	return nil
}

func (p *Panel) callRecoverDNSZoneV3(
	ctx context.Context,
	lease dnsZoneEngineLease,
	binding agentMutationBinding,
	response *transport.RecoverDNSZoneV3Response,
) error {
	if !lease.valid() || response == nil ||
		binding.MutationRequestID != lease.RequestID ||
		binding.MutationOwnerID != lease.OwnerID {
		return errors.New("invalid exact DNS zone V3 recovery binding")
	}
	request := transport.RecoverDNSZoneV3Request{
		ServiceMutationBinding: binding,
		Domain:                 lease.ZoneName,
		Qualifier:              lease.Qualifier,
	}
	if err := p.callAgentContext(
		ctx, "Agent.RecoverDNSZoneV3", &request, response,
	); err != nil {
		return err
	}
	if response.Error != "" {
		if response.Recovered || response.RecoveryPending {
			return errors.New("agent returned a mixed DNS zone recovery failure response")
		}
		return errors.New("agent could not verify the exact DNS zone recovery")
	}
	if response.RecoveryPending {
		if response.Recovered {
			return errors.New("agent returned a mixed DNS zone recovery response")
		}
		return &dnsZoneV3PropagationPendingError{}
	}
	if !response.Recovered {
		return errors.New("agent did not confirm the exact DNS zone recovery")
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
	// A failed initial BIND install may leave an exact panel-managed package
	// stopped as a rollback standby. With no durable source and no running DNS
	// backend, retrying is still the initial install/activation operation.
	if snapshot.ActiveEngine == nil &&
		snapshot.State == dnsEngineStateUnconfigured &&
		snapshot.EngineEpoch == 0 &&
		target == transport.DNSEngineBIND &&
		!runtime.Running && runtime.Managed {
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
		// Revision zero is the released legacy adoption contract: the running
		// PowerDNS may already implement a signed paired topology and must be
		// registered without replacement. A DB-staged plan advances revision
		// first; only that explicit authority selects destructive reconfigure.
		if snapshot.Topology == transport.DNSTopologyPaired &&
			snapshot.Revision > 0 {
			return "reconfigure"
		}
		return "adopt"
	}
	return "switch"
}

func dnsEngineImpacts(action string, hasSource bool) []string {
	if action == "adopt" {
		return []string{"validate_target", "adopt_existing"}
	}
	if action == "reconfigure" {
		return []string{
			"validate_target",
			"replace_existing",
			"restart_target",
			"configure_secondary",
			"brief_dns_interruption",
		}
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
	//
	// A first install has no source engine, so its zones are pending by
	// construction: nothing exists that could have applied them, and the
	// install itself publishes every zone at its desired generation and marks
	// it applied on commit. Treating that as a blocker made the first engine
	// install unreachable on any host where a domain existed first (S-7 T1,
	// register R-029). With a source engine active the blocker stays: pending
	// there means the source has not caught up, and a switch must not copy an
	// unsettled zone set.
	//
	// İlk kurulumun kaynak motoru yoktur; bölgeleri yapısı gereği bekler:
	// onları uygulamış olabilecek hiçbir şey yoktur ve kurulumun kendisi her
	// bölgeyi istenen neslinde yayımlayıp commit'te uygulandı işaretler. Bunu
	// engelleyici saymak, önce alan adı eklenmiş her sunucuda ilk motor
	// kurulumunu ulaşılamaz kılıyordu (S-7 T1, defter R-029). Kaynak motor
	// etkinken engelleyici kalır: orada bekleme, kaynağın yetişmediği
	// anlamına gelir ve geçiş oturmamış bir bölge kümesini kopyalamamalıdır.
	if action != "adopt" && snapshot.ActiveEngine != nil &&
		snapshot.PendingZoneCount > 0 {
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
	// A durable switch always has a source. Registration-only PowerDNS paths use
	// adopt or reconfigure. Every other source-free switch, and an install while
	// runtime state proves the server is not unconfigured, must stop at preview.
	if snapshot.ActiveEngine == nil &&
		action != "adopt" && action != "reconfigure" &&
		(action == "switch" || snapshot.State != dnsEngineStateUnconfigured) {
		blockers = addDNSEngineBlocker(blockers, "target_unavailable")
	}
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
	if action == "reconfigure" &&
		(target != transport.DNSEnginePowerDNS ||
			snapshot.ActiveEngine != nil ||
			snapshot.Topology != transport.DNSTopologyPaired ||
			snapshot.PairRole != transport.DNSPairRoleSecondary ||
			snapshot.pairIdentityErr != nil ||
			snapshot.ZoneCount != 0 ||
			!targetRuntime.Installed || !targetRuntime.Running ||
			!targetRuntime.Managed) {
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

func (p *Panel) unresolvedDNSPairRole(ctx context.Context) (string, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, peerNS, err := canonicalDNSEnginePeerSnapshotTx(
		ctx, tx, transport.DNSTopologyPaired,
	)
	if err != nil {
		return "", err
	}
	role, _, _, err := canonicalBINDEnginePairIdentityTx(ctx, tx, peerNS)
	if err != nil {
		return "", err
	}
	return role, nil
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
	requiresAck := hasSource || action == "reconfigure"
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

func validDNSEngineReceiptCommitment(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (p *Panel) verifyDNSEngineRollbackEvidence(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (string, error) {
	switchRequest := dnsEngineSwitchRequestForManifest(manifest)
	switchRequest.ServiceMutationBinding = agentMutationBinding{
		MutationRequestID: persisted.RequestID,
		MutationOwnerID:   persisted.OwnerID,
	}
	request := transport.DNSEngineRollbackEvidenceRequest(switchRequest)
	var response transport.DNSEngineRollbackEvidenceResponse
	if err := p.callAgentContext(
		ctx, "Agent.DNSEngineRollbackEvidenceV1", &request, &response,
	); err != nil {
		return "", errors.New("DNS engine rollback evidence is unavailable")
	}
	if response.Outcome != transport.DNSEngineRollbackSafe ||
		!validDNSEngineReceiptCommitment(response.ReceiptCommitment) {
		return "", errors.New("DNS engine rollback evidence is not safe")
	}
	return response.ReceiptCommitment, nil
}

func validateInitialBINDInstallReconcileScope(
	persisted persistedDNSEngineSwitch,
) error {
	frozenTopology := persisted.Topology == transport.DNSTopologyStandalone &&
		persisted.PairRole == "" && persisted.LocalIP == "" &&
		persisted.LocalNS == "" && persisted.PeerIP == "" &&
		persisted.PeerNS == ""
	if persisted.Topology == transport.DNSTopologyPaired {
		frozenTopology = persisted.PairRole == transport.DNSPairRolePrimary &&
			persisted.LocalIP != "" && persisted.LocalNS != "" &&
			persisted.PeerIP != "" && persisted.PeerNS != ""
	}
	if persisted.Mode != transport.DNSEngineSwitchModeSwitch ||
		persisted.Action != "install" ||
		persisted.SourceEngine != "" ||
		persisted.SourceEpoch != 0 ||
		persisted.TargetEngine != transport.DNSEngineBIND ||
		persisted.TargetEpoch != 1 || !frozenTopology {
		return errors.New(
			"DNS engine reconciliation is limited to an initial failed BIND install",
		)
	}
	return nil
}

// This predicate cannot turn a fresh source-empty install into an
// authority-bearing rollback. Agent evidence separately binds the exact
// active-unit, database, and configuration preimage.
func validateLegacyPDNSPairSecondaryReconfigureScope(
	persisted persistedDNSEngineSwitch,
) error {
	if persisted.Mode != transport.DNSEngineSwitchModeSwitch ||
		persisted.Action != "reconfigure" ||
		persisted.SourceEngine != "" || persisted.SourceEpoch != 0 ||
		persisted.TargetEngine != transport.DNSEnginePowerDNS ||
		persisted.TargetEpoch != 1 || persisted.SourceRevision < 1 ||
		persisted.Topology != transport.DNSTopologyPaired ||
		persisted.PairRole != transport.DNSPairRoleSecondary ||
		persisted.LocalIP == "" || persisted.LocalNS == "" ||
		persisted.PeerIP == "" || persisted.PeerNS == "" ||
		persisted.ZoneCount != 0 || persisted.SnapshotBytes != 0 {
		return errors.New("not an exact legacy PowerDNS paired-secondary reconfiguration")
	}
	return nil
}

func validateSourceEmptyDNSEngineReconcileScope(
	persisted persistedDNSEngineSwitch,
) error {
	if err := validateInitialBINDInstallReconcileScope(persisted); err == nil {
		return nil
	}
	if err := validateLegacyPDNSPairSecondaryReconfigureScope(persisted); err == nil {
		return nil
	}
	return errors.New("DNS engine switch is outside exact source-empty rollback scopes")
}

func (p *Panel) verifyDNSEngineRollbackWithStableEvidence(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	first, err := p.verifyDNSEngineRollbackEvidence(ctx, persisted, manifest)
	if err != nil {
		return err
	}
	if err := p.verifyDNSEngineRollbackRuntime(ctx, persisted); err != nil {
		return fmt.Errorf("verify DNS engine rollback runtime: %w", err)
	}
	second, err := p.verifyDNSEngineRollbackEvidence(ctx, persisted, manifest)
	if err != nil {
		return err
	}
	if first != second {
		return errors.New("DNS engine mutation terminal receipt changed during reconciliation")
	}
	return nil
}

// A source-empty reconfigure may restore a running PowerDNS only while two
// stable agent proofs bind its exact preimage around the runtime observation.
func (p *Panel) verifyDNSEngineRollbackOutcome(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) error {
	if err := validateLegacyPDNSPairSecondaryReconfigureScope(persisted); err != nil {
		return p.verifyDNSEngineRollbackRuntime(ctx, persisted)
	}
	manifest, err := p.reconstructPersistedDNSEngineManifest(ctx, persisted)
	if err != nil {
		return fmt.Errorf("verify persisted DNS engine manifest: %w", err)
	}
	return p.verifyDNSEngineRollbackWithStableEvidence(ctx, persisted, manifest)
}

// reconcileFailedDNSEngineSwitchLocked clears only an attached switch whose
// exact agent identity is durably terminal-failed and whose pre-operation
// runtime is independently proven. It never mutates host state or treats a
// missing receipt as failure. The caller holds serviceMutationMu,
// dnsTopologyMu, and dnsPublicationMu in that order.
func (p *Panel) reconcileFailedDNSEngineSwitchLocked(
	ctx context.Context,
) (persistedDNSEngineSwitch, bool, error) {
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return persistedDNSEngineSwitch{}, false, err
	}
	if state.CurrentSwitchID == "" {
		return persistedDNSEngineSwitch{}, false, nil
	}
	persisted, err := readDNSEngineSwitchByID(
		ctx, p.db.GetDB(), state.CurrentSwitchID,
	)
	if err != nil {
		return persistedDNSEngineSwitch{}, false, err
	}
	marker, err := readDNSEngineOperationMarker(ctx, p.db.GetDB())
	if err != nil {
		return persisted, false, err
	}
	if marker == nil || marker.Phase != dnsEngineOperationAccepted ||
		marker.SwitchID != persisted.SwitchID ||
		marker.RequestID != persisted.RequestID {
		return persisted, false, errors.New(
			"active DNS engine switch has no exact accepted marker",
		)
	}
	if err := attachDNSEngineOperationAction(
		ctx, p.db.GetDB(), &persisted,
	); err != nil {
		return persisted, false, err
	}
	if err := validateSourceEmptyDNSEngineReconcileScope(persisted); err != nil {
		return persisted, false, err
	}
	if persisted.Phase != "activating" ||
		!exactSourceEmptyDNSEngineSwitchAttachedState(state, persisted) {
		return persisted, false, errors.New(
			"attached DNS engine switch no longer matches its source authority",
		)
	}
	manifest, err := p.reconstructPersistedDNSEngineManifest(
		ctx, persisted,
	)
	if err != nil {
		return persisted, false, fmt.Errorf(
			"verify persisted DNS engine manifest: %w", err,
		)
	}
	if err := p.verifyDNSEngineRollbackWithStableEvidence(
		ctx, persisted, manifest,
	); err != nil {
		return persisted, false, err
	}
	if err := p.rollbackVerifiedSourceEmptyDNSEngineSwitch(
		ctx, persisted, manifest,
	); err != nil {
		return persisted, false, fmt.Errorf(
			"finalize DNS engine rollback: %w", err,
		)
	}
	return persisted, true, nil
}

// reconcileDNSEngineSwitchLocked observes the durable agent receipt without
// waiting for an active package operation. It finalizes a proven successful
// mutation immediately and otherwise delegates terminal failure recovery to
// the exact source-empty rollback proof.
type dnsEngineReconcilePostCommitError struct {
	Result     dnsEnginePostCommitResult
	Unverified bool
}

func (err *dnsEngineReconcilePostCommitError) Error() string {
	return "DNS engine reconciliation has pending post-commit follow-up"
}

func (p *Panel) reconcileDNSEngineSwitchLocked(
	ctx context.Context,
) (persistedDNSEngineSwitch, bool, error) {
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return persistedDNSEngineSwitch{}, false, err
	}
	if state.CurrentSwitchID == "" {
		marker, markerErr := readDNSEngineOperationMarker(ctx, p.db.GetDB())
		if markerErr != nil {
			return persistedDNSEngineSwitch{}, false, markerErr
		}
		var detached persistedDNSEngineSwitch
		if marker != nil && marker.Phase == dnsEngineOperationPostCommit {
			detached, markerErr = readDNSEngineSwitchByID(
				ctx, p.db.GetDB(), marker.SwitchID,
			)
			if markerErr == nil {
				markerErr = attachDNSEngineOperationAction(
					ctx, p.db.GetDB(), &detached,
				)
			}
			if markerErr != nil {
				return detached, false, markerErr
			}
		}
		recovered, recoverErr := p.recoverDNSEngineSwitchWithPostCommitLocksLocked(
			ctx, nil,
		)
		if recoverErr != nil {
			return detached, recovered, recoverErr
		}
		if marker != nil && marker.Phase == dnsEngineOperationPostCommit {
			remaining, markerErr := readDNSEngineOperationMarker(
				ctx, p.db.GetDB(),
			)
			if markerErr != nil {
				return detached, recovered, markerErr
			}
			if remaining != nil && remaining.Phase == dnsEngineOperationPostCommit &&
				remaining.SwitchID == marker.SwitchID &&
				remaining.RequestID == marker.RequestID {
				return detached, recovered, &dnsEngineReconcilePostCommitError{
					Unverified: true,
				}
			}
		}
		return detached, recovered, nil
	}
	persisted, err := readDNSEngineSwitchByID(
		ctx, p.db.GetDB(), state.CurrentSwitchID,
	)
	if err != nil {
		return persistedDNSEngineSwitch{}, false, err
	}
	marker, err := readDNSEngineOperationMarker(ctx, p.db.GetDB())
	if err != nil {
		return persisted, false, err
	}
	if marker == nil || marker.Phase != dnsEngineOperationAccepted ||
		marker.SwitchID != persisted.SwitchID ||
		marker.RequestID != persisted.RequestID {
		return persisted, false, errors.New(
			"active DNS engine switch has no exact accepted marker",
		)
	}
	if err := attachDNSEngineOperationAction(
		ctx, p.db.GetDB(), &persisted,
	); err != nil {
		return persisted, false, err
	}
	if _, err := p.reconstructPersistedDNSEngineManifest(ctx, persisted); err != nil {
		return persisted, false, fmt.Errorf(
			"verify persisted DNS engine manifest: %w", err,
		)
	}
	identity := agentMutationIdentity{
		RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
		Kind: dnsEngineSwitchKind, Target: string(persisted.TargetEngine),
		PackageName: persisted.Qualifier,
	}
	job, err := p.statusAgentMutation(ctx, persisted.RequestID)
	if err != nil {
		return persisted, false, fmt.Errorf(
			"read DNS engine mutation during reconciliation: %w", err,
		)
	}
	if job != nil && !identity.matches(job) {
		return persisted, false, errAgentMutationIdentityMismatch
	}
	if job != nil && agentMutationActive(job.Status) {
		return persisted, false, nil
	}
	if job != nil && job.Status == agentMutationSucceeded {
		if err := validateAgentMutationSucceededReceipt(job, identity); err != nil {
			return persisted, false, err
		}
		if err := p.verifyDNSEngineRuntimeTarget(
			ctx, persisted.TargetEngine,
		); err != nil {
			return persisted, false, fmt.Errorf(
				"verify reconciled DNS engine runtime: %w", err,
			)
		}
		if err := p.finalizeDNSEngineSwitchSuccess(ctx, persisted); err != nil {
			return persisted, false, fmt.Errorf(
				"finalize reconciled DNS engine switch: %w", err,
			)
		}
		result := p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
		if result.failed() {
			log.Printf(
				"reconciled DNS engine switch %s has pending follow-up: normalization=%v firewall=%v scan=%v",
				persisted.SwitchID, result.NormalizationErr,
				result.FirewallErr, result.ScanErr,
			)
			return persisted, true, &dnsEngineReconcilePostCommitError{Result: result}
		}
		return persisted, true, nil
	}
	return p.reconcileFailedDNSEngineSwitchLocked(ctx)
}

func (p *Panel) handleDNSEngineReconcile(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !p.serviceMutationMu.TryLock() {
		writeDNSEngineStateUnverified(w)
		return
	}
	var (
		persisted     persistedDNSEngineSwitch
		reconciled    bool
		postCommitErr *dnsEngineReconcilePostCommitError
		err           error
	)
	func() {
		defer p.serviceMutationMu.Unlock()
		p.dnsTopologyMu.Lock()
		defer p.dnsTopologyMu.Unlock()
		dnsPublicationMu.Lock()
		defer dnsPublicationMu.Unlock()

		actor := dnsEngineActorFromRequest(r)
		persisted, reconciled, err = p.reconcileDNSEngineSwitchLocked(
			r.Context(),
		)
		if errors.As(err, &postCommitErr) {
			if persisted.SwitchID != "" {
				p.auditDNSEngineBounded(actor, "post_commit.pending", persisted)
			}
		} else if err != nil {
			if persisted.SwitchID != "" {
				p.auditDNSEngineBounded(actor, "reconciled_operation.uncertain", persisted)
				log.Printf(
					"DNS engine reconcile %s target=%s retained the attached state",
					persisted.SwitchID, persisted.TargetEngine,
				)
			}
		} else if reconciled && persisted.SwitchID != "" {
			p.auditDNSEngineBounded(actor, "reconciled_operation", persisted)
		}
	}()

	if postCommitErr != nil {
		if postCommitErr.Unverified {
			writeDNSEngineChangeAppliedRefreshRequired(w)
		} else {
			writeDNSEnginePostCommitFailed(w, postCommitErr.Result)
		}
		return
	}
	if err != nil {
		writeDNSEngineStateUnverified(w)
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Reconciled bool `json:"reconciled"`
	}{Reconciled: reconciled})
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
	LastError      string
	CreatedAt      string
	UpdatedAt      string
}

func readDNSEngineSwitchByRequest(
	ctx context.Context,
	query dnsZoneStateQuery,
	requestID string,
) (persistedDNSEngineSwitch, error) {
	var result persistedDNSEngineSwitch
	var source, lastError, pairRole, localIP, localNS, pairPeerIP, pairPeerNS sql.NullString
	err := query.QueryRowContext(ctx, `
		SELECT snapshot.switch_id, snapshot.request_id, snapshot.owner_id,
		       snapshot.mode, snapshot.source_engine, snapshot.target_engine,
		       snapshot.source_epoch, snapshot.target_epoch,
		       snapshot.source_state_revision, snapshot.phase,
		       snapshot.topology, snapshot.peer_ip, snapshot.peer_ns,
		       snapshot.manifest_qualifier, snapshot.zone_count, snapshot.snapshot_bytes,
		       snapshot.last_error, snapshot.created_at, snapshot.updated_at,
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
		&lastError, &result.CreatedAt, &result.UpdatedAt,
		&pairRole, &localIP, &localNS, &pairPeerIP, &pairPeerNS,
	)
	if source.Valid {
		result.SourceEngine = transport.DNSEngine(source.String)
	}
	if lastError.Valid {
		result.LastError = lastError.String
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

func normalizeDNSEngineOperationTime(raw string) (string, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC().Format(time.RFC3339), nil
		}
	}
	return "", errors.New("DNS engine operation timestamp is invalid")
}

func dnsEngineOperationStatus(phase string) (string, error) {
	switch phase {
	case "planned", "staging", "staged", "activating", "verifying":
		return "running", nil
	case "rolling_back":
		return "rolling_back", nil
	case "committed":
		return "succeeded", nil
	case "rolled_back":
		return "rolled_back", nil
	case "failed":
		return "failed", nil
	default:
		return "", errors.New("DNS engine operation phase is invalid")
	}
}

func presentDNSEngineOperation(
	persisted persistedDNSEngineSwitch,
) (*dnsEngineOperationSnapshot, error) {
	status, err := dnsEngineOperationStatus(persisted.Phase)
	if err != nil {
		return nil, err
	}
	startedAt, err := normalizeDNSEngineOperationTime(persisted.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedAt, err := normalizeDNSEngineOperationTime(persisted.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &dnsEngineOperationSnapshot{
		ID: persisted.SwitchID, RequestID: persisted.RequestID,
		TargetEngine: persisted.TargetEngine,
		Phase:        persisted.Phase, Status: status,
		StartedAt: startedAt, UpdatedAt: updatedAt,
		LastError: persisted.LastError,
	}, nil
}

func (p *Panel) dnsEngineReplaySnapshot(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) (dnsEngineSnapshot, error) {
	snapshot, err := p.dnsEngineSnapshot(ctx)
	if err != nil {
		return dnsEngineSnapshot{}, err
	}
	exactOperation, err := presentDNSEngineOperation(persisted)
	if err != nil {
		return dnsEngineSnapshot{}, err
	}
	if snapshot.State == dnsEngineStateSwitching {
		if snapshot.OperationID != persisted.SwitchID ||
			snapshot.Operation == nil ||
			snapshot.Operation.RequestID != persisted.RequestID {
			return dnsEngineSnapshot{}, errors.New(
				"DNS engine replay is not the active operation",
			)
		}
		return snapshot, nil
	}
	// A later completed DNS change may now be the globally latest operation.
	// Idempotent replay still answers for the request that was replayed; the
	// remaining snapshot fields continue to describe current DNS authority.
	snapshot.Operation = exactOperation
	return snapshot, nil
}

func readPresentedDNSEngineOperation(
	ctx context.Context,
	query dnsZoneStateQuery,
	currentSwitchID string,
) (*dnsEngineOperationSnapshot, error) {
	var (
		persisted persistedDNSEngineSwitch
		err       error
	)
	if currentSwitchID != "" {
		persisted, err = readDNSEngineSwitchByID(ctx, query, currentSwitchID)
	} else {
		var requestID string
		err = query.QueryRowContext(ctx, `
			SELECT request_id
			FROM dns_engine_switch_snapshots
			ORDER BY julianday(updated_at) DESC, rowid DESC
			LIMIT 1`,
		).Scan(&requestID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err == nil {
			persisted, err = readDNSEngineSwitchByRequest(ctx, query, requestID)
		}
	}
	if err != nil {
		return nil, err
	}
	return presentDNSEngineOperation(persisted)
}

// enrichAttachedDNSEngineOperation adds only a proven terminal agent receipt
// to the durable panel phase. A failed host mutation can leave the panel saga
// attached while exact rollback verification is still pending; presenting
// that state as merely "running" would hide the reason operator attention is
// required. Agent lookup failures do not erase the last durable panel truth.
func (p *Panel) enrichAttachedDNSEngineOperation(
	ctx context.Context,
	operation *dnsEngineOperationSnapshot,
	switchID string,
) {
	persisted, err := readDNSEngineSwitchByID(ctx, p.db.GetDB(), switchID)
	if err != nil {
		return
	}
	identity := agentMutationIdentity{
		RequestID: persisted.RequestID, OwnerID: persisted.OwnerID,
		Kind: dnsEngineSwitchKind, Target: string(persisted.TargetEngine),
		PackageName: persisted.Qualifier,
	}
	job, err := p.statusAgentMutation(ctx, persisted.RequestID)
	if err != nil || job == nil || !identity.matches(job) ||
		(operation.Status != "running" && operation.Status != "rolling_back") {
		return
	}
	switch job.Status {
	case agentMutationFailed:
		operation.Status = "recovery_required"
		operation.LastError = safeDNSEngineOperationReceiptMessage(job.ErrorMessage)
	case agentMutationSucceeded:
		if errors.Is(
			validateAgentMutationSucceededReceipt(job, identity),
			errAgentMutationRecoveryRequired,
		) {
			operation.Status = "recovery_required"
			operation.LastError = "The DNS engine switch is waiting for privileged recovery finalization."
		}
	}
}

func safeDNSEngineOperationReceiptMessage(message string) string {
	const fallback = "The privileged DNS operation failed before the panel could finalize it."
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 512 {
		return fallback
	}
	for _, character := range message {
		if character < 0x20 || character == 0x7f {
			return fallback
		}
	}
	return message
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
	runtimes, port53Conflict, _, err := p.readDNSBackendRuntime(ctx)
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

func exactSourceEmptyDNSEngineSwitchAttachedState(
	state dnsEngineDBState,
	persisted persistedDNSEngineSwitch,
) bool {
	return state.ActiveEngine == "" &&
		state.EngineEpoch == persisted.SourceEpoch &&
		state.Revision == persisted.SourceRevision+1 &&
		state.Topology == transport.DNSTopologyStandalone &&
		state.PairRole == "" && state.LocalIP == "" &&
		state.LocalNS == "" && state.PeerIP == "" &&
		state.PeerNS == "" &&
		state.CurrentSwitchID == persisted.SwitchID
}

// rollbackVerifiedSourceEmptyDNSEngineSwitch is deliberately narrower than the
// ordinary rollback path. It consumes the exact snapshot and canonical zones
// that the agent verified twice, then binds the detach to the still-attached
// source-empty authority in the same transaction.
func (p *Panel) rollbackVerifiedSourceEmptyDNSEngineSwitch(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
	verifiedManifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := validateSourceEmptyDNSEngineReconcileScope(persisted); err != nil {
		return err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := readDNSEngineSwitchByRequest(
		ctx, tx, persisted.RequestID,
	)
	if err != nil {
		return err
	}
	if err := attachDNSEngineOperationAction(ctx, tx, &current); err != nil {
		return err
	}
	if current != persisted || current.Phase != "activating" {
		return errors.New(
			"verified DNS engine switch snapshot changed before reconciliation",
		)
	}
	if err := validateSourceEmptyDNSEngineReconcileScope(current); err != nil {
		return err
	}
	currentManifest, err := reconstructPersistedDNSEngineManifestFromQuery(
		ctx, tx, current,
	)
	if err != nil {
		return fmt.Errorf("reconstruct verified DNS engine manifest: %w", err)
	}
	if !reflect.DeepEqual(currentManifest, verifiedManifest) {
		return errors.New(
			"verified DNS engine zone snapshot changed before reconciliation",
		)
	}
	var stagedZones int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM dns_engine_switch_zones
		WHERE switch_id = ? AND phase = 'staged'`,
		current.SwitchID,
	).Scan(&stagedZones); err != nil {
		return err
	}
	if stagedZones != current.ZoneCount {
		return errors.New(
			"verified DNS engine zone phases changed before reconciliation",
		)
	}
	state, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return err
	}
	if !exactSourceEmptyDNSEngineSwitchAttachedState(state, current) {
		return errors.New(
			"verified DNS engine source authority changed before reconciliation",
		)
	}

	const safeFailure = "DNS engine switch did not complete"
	rollingBack, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_switch_snapshots
		SET phase = 'rolling_back', last_error = ?,
		    updated_at = datetime('now')
		WHERE switch_id = ? AND phase = 'activating'`,
		safeFailure, current.SwitchID,
	)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		rollingBack, 1,
		"verified DNS engine rollback transition was not exact",
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
		"verified DNS engine rollback completion was not exact",
	); err != nil {
		return err
	}
	detached, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_state
		SET current_switch_id = NULL, revision = revision + 1,
		    updated_at = datetime('now')
		WHERE singleton_id = 1
		  AND current_switch_id = ?
		  AND active_engine IS NULL
		  AND active_epoch = ?
		  AND revision = ?
		  AND topology = ?
		  AND NOT EXISTS (
		    SELECT 1 FROM dns_bind_pair_state WHERE singleton_id = 1
		  )`,
		current.SwitchID, current.SourceEpoch,
		current.SourceRevision+1, transport.DNSTopologyStandalone,
	)
	if err != nil {
		return err
	}
	if err := requireExactRows(
		detached, 1,
		"verified DNS engine singleton detach was not exact",
	); err != nil {
		return err
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
	runtimes, port53Conflict, _, err := p.readDNSBackendRuntime(ctx)
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
		if err := validateLegacyPDNSPairSecondaryReconfigureScope(persisted); err == nil {
			target := runtimes[transport.DNSEnginePowerDNS]
			if !target.Installed || !target.Running || !target.Managed {
				return errors.New("restored legacy PowerDNS is not active and managed")
			}
			for engine, runtime := range runtimes {
				if engine != transport.DNSEnginePowerDNS && runtime.Running {
					return errors.New("another DNS engine remains active after PowerDNS rollback")
				}
			}
			return nil
		}
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

type dnsEngineManifestQuery interface {
	dnsZoneStateQuery
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (p *Panel) reconstructPersistedDNSEngineManifest(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	return reconstructPersistedDNSEngineManifestFromQuery(
		ctx, p.db.GetDB(), persisted,
	)
}

func reconstructPersistedDNSEngineManifestFromQuery(
	ctx context.Context,
	query dnsEngineManifestQuery,
	persisted persistedDNSEngineSwitch,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	rows, err := query.QueryContext(ctx, `
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
				return newDNSEngineAgentRejectedError(response.Error)
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
		return &dnsEngineMutationAppliedFollowupError{err: err}
	}
	if err := p.finalizeDNSEngineSwitchSuccess(ctx, persisted); err != nil {
		return &dnsEngineMutationAppliedFollowupError{err: err}
	}
	return nil
}

type dnsEngineMutationAppliedFollowupError struct {
	err error
}

func (err *dnsEngineMutationAppliedFollowupError) Error() string {
	return "DNS engine host switch was applied but panel follow-up is pending"
}

func (err *dnsEngineMutationAppliedFollowupError) Unwrap() error {
	return err.err
}

// dnsEngineAgentRejectedError keeps only an allowlisted classification. Raw
// agent text can contain host paths or command output and never crosses into
// either the HTTP response or the panel log.
type dnsEngineAgentRejectedError struct {
	diagnosticCode string
	clientCode     string
}

func (err *dnsEngineAgentRejectedError) Error() string {
	return "agent rejected DNS engine switch"
}

func logDNSEngineAgentRejection(switchID string, err error) {
	var rejected *dnsEngineAgentRejectedError
	if !errors.As(err, &rejected) {
		return
	}
	log.Printf(
		"DNS engine switch %s agent rejection code=%s",
		switchID, rejected.diagnosticCode,
	)
}

func newDNSEngineAgentRejectedError(detail string) *dnsEngineAgentRejectedError {
	rejected := &dnsEngineAgentRejectedError{
		diagnosticCode: "unclassified_detail_omitted",
	}
	switch detail {
	case "DNS engine switch request is required":
		rejected.diagnosticCode = "invalid_request"
	case "DNS engine switch request is not the exact canonical manifest":
		rejected.diagnosticCode = "canonical_manifest_mismatch"
		rejected.clientCode = errCodeDNSEnginePlanRejected
	case "DNS engine switch did not complete; inspect the agent log":
		rejected.diagnosticCode = "backend_switch_failed"
	case "DNS engine switch did not return the exact verified target receipt":
		rejected.diagnosticCode = "target_receipt_mismatch"
	case "DNS engine switch finished but its durable receipt could not be verified":
		rejected.diagnosticCode = "terminal_receipt_unverified"
	}
	return rejected
}

func writeDNSEngineChangeNotCommitted(w http.ResponseWriter, switchErr error) {
	var rejected *dnsEngineAgentRejectedError
	if errors.As(switchErr, &rejected) &&
		rejected.clientCode == errCodeDNSEnginePlanRejected {
		writeCodedError(
			w,
			http.StatusConflict,
			errCodeDNSEnginePlanRejected,
			"The DNS agent rejected the reviewed plan. The DNS engine change was not committed. Refresh state before creating a new review.",
			"",
		)
		return
	}
	writeCodedError(
		w,
		http.StatusConflict,
		errCodeDNSEngineChangeNotCommitted,
		"The DNS engine change was not committed. The pre-operation serving state was verified; packages or setup files may still have changed. Refresh state before creating a new review.",
		"",
	)
}

// writeDNSEngineMutationsHeld names the hold. Everything else about a held
// agent is already true of an unverified outcome — the change did not complete
// and state must be refreshed — but the operator's next action is different:
// nothing will retry on its own, the agent's health is the problem, and the
// hold code says which health problem. The message is fixed English and the
// code is one of the stable MutationHold* values; no internal error text is
// forwarded.
// writeDNSEngineMutationsHeld tutulmayı adlandırır. Tutulan bir agent hakkında
// geri kalan her şey doğrulanmamış bir sonuç için zaten geçerlidir — değişiklik
// tamamlanmadı ve durum yenilenmeli — ama operatörün bir sonraki adımı
// farklıdır: hiçbir şey kendiliğinden yeniden denemeyecek, sorun agent'ın
// sağlığıdır ve tutulma kodu hangi sağlık sorunu olduğunu söyler. Mesaj sabit
// İngilizcedir, kod kararlı MutationHold* değerlerinden biridir; hiçbir iç hata
// metni iletilmez.
func writeDNSEngineMutationsHeld(w http.ResponseWriter, hold string) {
	writeCodedErrorDetails(
		w,
		http.StatusServiceUnavailable,
		errCodeDNSEngineMutationsHeld,
		"The agent is refusing durable mutations, so this DNS engine change did not complete and will not retry on its own. Refresh state and review the agent's health before requesting another change",
		"",
		[]string{hold},
	)
}

func writeDNSEngineStateUnverified(w http.ResponseWriter) {
	writeCodedError(
		w,
		http.StatusBadGateway,
		errCodeDNSEngineStateUnverified,
		"DNS engine change outcome could not be verified. Refresh state before reviewing another change",
		"",
	)
}

func writeDNSEngineChangeAppliedRefreshRequired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(apiErrorBody{
		Error:          "DNS engine change was applied, but panel finalization is incomplete. Refresh state before taking another action",
		Code:           errCodeDNSEngineChangeAppliedRefresh,
		PartialSuccess: true, MutationApplied: true,
	})
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
	// If startup could not reconcile interrupted service operations, the durable
	// state this switch would build on is unknown. Refuse with the stored cause
	// rather than starting a second transaction on top of an unresolved one.
	// Açılış yarım kalmış servis işlemlerini uzlaştıramadıysa, bu geçişin
	// üzerine kuracağı kalıcı durum bilinmiyor. Çözülmemiş bir işlemin üstüne
	// ikincisini başlatmak yerine saklanan sebeple reddet.
	if !p.requireSubsystemOperational(w, degradedSubsystemServiceOperations) {
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
					"DNS engine replay post-commit %s: normalization=%v firewall=%v scan=%v",
					persisted.SwitchID, result.NormalizationErr,
					result.FirewallErr, result.ScanErr,
				)
				p.auditDNSEngineBounded(actor, "post_commit.pending", persisted)
				writeDNSEnginePostCommitFailed(w, result)
				return
			}
			p.auditDNSEngineBounded(actor, "post_commit.recovered", persisted)
		}
		snapshot, err := p.dnsEngineReplaySnapshot(r.Context(), persisted)
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
	if (request.ExpectedSource.Valid || action == "reconfigure") &&
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
		var appliedFollowup *dnsEngineMutationAppliedFollowupError
		mutationApplied := errors.As(err, &appliedFollowup)
		changeNotCommitted := false
		if !mutationTerminalUncertain(err) && !mutationApplied {
			proofErr := p.verifyDNSEngineRollbackOutcome(workerCtx, persisted)
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
				changeNotCommitted = true
				p.auditDNSEngineBounded(actor, "change_not_committed", persisted)
			}
		} else {
			p.auditDNSEngineBounded(actor, "uncertain", persisted)
		}
		logDNSEngineAgentRejection(persisted.SwitchID, err)
		log.Printf("DNS engine switch %s did not finalize: %v", persisted.SwitchID, err)
		var held *agentMutationHeldError
		switch {
		case errors.As(err, &held):
			writeDNSEngineMutationsHeld(w, held.Hold)
		case changeNotCommitted:
			writeDNSEngineChangeNotCommitted(w, err)
		case mutationApplied:
			writeDNSEngineChangeAppliedRefreshRequired(w)
		default:
			writeDNSEngineStateUnverified(w)
		}
		return
	}
	p.auditDNSEngineBounded(actor, "succeeded", persisted)
	postCommit := p.reconcileDNSEnginePostCommitLocked(workerCtx, persisted)
	if postCommit.failed() {
		log.Printf(
			"DNS engine switch %s committed with pending follow-up: normalization=%v firewall=%v scan=%v",
			persisted.SwitchID, postCommit.NormalizationErr,
			postCommit.FirewallErr, postCommit.ScanErr,
		)
		p.auditDNSEngineBounded(actor, "post_commit.pending", persisted)
		writeDNSEnginePostCommitFailed(w, postCommit)
		return
	}
	p.auditDNSEngineBounded(actor, "post_commit.completed", persisted)
	finalSnapshot, err := p.dnsEngineSnapshot(workerCtx)
	if err != nil {
		log.Printf("DNS engine change completed but final state response failed: %v", err)
		writeDNSEngineChangeAppliedRefreshRequired(w)
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

func adoptionConfigureIdentityFromMarker(
	marker *dnsEngineOperationMarker,
) (agentMutationIdentity, bool) {
	if marker == nil || marker.Action != "adopt" ||
		marker.Phase != dnsEngineOperationPostCommit ||
		marker.TargetEngine != transport.DNSEnginePowerDNS ||
		!validServiceOperationID(marker.ConfigurePDNSRequestID) ||
		!validServiceOperationID(marker.ConfigurePDNSOwnerID) {
		return agentMutationIdentity{}, false
	}
	return agentMutationIdentity{
		RequestID: marker.ConfigurePDNSRequestID,
		OwnerID:   marker.ConfigurePDNSOwnerID,
		Kind:      "pdns_configure",
		Target:    "pdns",
	}, true
}

// reconcileDNSEnginePostCommitAfterRestartLocked enters the same process-lock
// order as the HTTP switch path. Startup already owns serviceMutationMu.
func (p *Panel) reconcileDNSEnginePostCommitAfterRestartLocked(
	ctx context.Context,
	persisted persistedDNSEngineSwitch,
) dnsEnginePostCommitResult {
	p.dnsTopologyMu.Lock()
	defer p.dnsTopologyMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	return p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
}

// recoverDNSEngineSwitchLocked reconciles the snapshot committed before
// BeginServiceMutation. The caller holds serviceMutationMu during startup.
func (p *Panel) recoverDNSEngineSwitchLocked(
	ctx context.Context,
	globalJob *agentMutationJob,
) (bool, error) {
	return p.recoverDNSEngineSwitchLockedMode(ctx, globalJob, false)
}

// recoverDNSEngineSwitchWithPostCommitLocksLocked is the manual-reconcile
// entry point. Its caller already owns serviceMutationMu, dnsTopologyMu, and
// dnsPublicationMu in that order.
func (p *Panel) recoverDNSEngineSwitchWithPostCommitLocksLocked(
	ctx context.Context,
	globalJob *agentMutationJob,
) (bool, error) {
	return p.recoverDNSEngineSwitchLockedMode(ctx, globalJob, true)
}

func (p *Panel) recoverDNSEngineSwitchLockedMode(
	ctx context.Context,
	globalJob *agentMutationJob,
	postCommitLocksHeld bool,
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
				configureIdentity, hasConfigureIdentity :=
					adoptionConfigureIdentityFromMarker(marker)
				switch {
				case hasConfigureIdentity &&
					configureIdentity.matches(globalJob):
					// The post-commit reconciler waits for this exact child and
					// either validates its success receipt or resumes its failed
					// identity. The marker was durable before BeginServiceMutation.
				case marker.Action == "adopt" &&
					marker.ConfigurePDNSComplete &&
					validDirectDNSZoneSyncV3(globalJob):
					p.dnsTopologyMu.Lock()
					recoverErr := p.recoverDirectDNSZoneSyncV3Locked(
						ctx, globalJob,
					)
					p.dnsTopologyMu.Unlock()
					if recoverErr != nil {
						p.auditDNSEngineSystem(
							ctx, "recovered.uncertain", persisted,
						)
						return true, recoverErr
					}
				case validDirectFirewallMutation(globalJob):
					if err := p.terminalizeInterruptedFirewallMutation(
						ctx, globalJob,
					); err != nil {
						p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
						return true, err
					}
				default:
					p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
					return true, errors.New(
						"active mutation does not match DNS engine post-commit recovery",
					)
				}
			}
			var result dnsEnginePostCommitResult
			if postCommitLocksHeld {
				result = p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
			} else {
				result = p.reconcileDNSEnginePostCommitAfterRestartLocked(
					ctx, persisted,
				)
			}
			if result.failed() {
				log.Printf(
					"DNS engine startup post-commit %s remains pending: normalization=%v firewall=%v scan=%v",
					persisted.SwitchID, result.NormalizationErr,
					result.FirewallErr, result.ScanErr,
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
		var result dnsEnginePostCommitResult
		if postCommitLocksHeld {
			result = p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
		} else {
			result = p.reconcileDNSEnginePostCommitAfterRestartLocked(
				ctx, persisted,
			)
		}
		if result.failed() {
			log.Printf(
				"recovered DNS engine switch %s has pending follow-up: normalization=%v firewall=%v scan=%v",
				persisted.SwitchID, result.NormalizationErr,
				result.FirewallErr, result.ScanErr,
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
	if err := p.verifyDNSEngineRollbackOutcome(ctx, persisted); err != nil {
		p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
		return true, fmt.Errorf("verify DNS engine rollback during recovery: %w", err)
	}
	p.auditDNSEngineSystem(ctx, "recovered.failed", persisted)
	if err := p.rollbackDNSEngineSwitch(ctx, persisted); err != nil {
		p.auditDNSEngineSystem(ctx, "recovered.uncertain", persisted)
		return true, fmt.Errorf("finalize DNS engine rollback during recovery: %w", err)
	}
	p.auditDNSEngineSystem(ctx, "recovered.change_not_committed", persisted)
	return true, nil
}
