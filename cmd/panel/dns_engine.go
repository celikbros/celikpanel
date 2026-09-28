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
	// mutationHold is the agent's reason for refusing durable mutations, or
	// "" when it accepts them. It is already what turns an engine's detail
	// code into "mutations_held" in the presentation; it is carried on the
	// snapshot so the gates that decide what may be offered can read the fact
	// itself rather than the word derived from it. R-050.
	//
	// mutationHold, agent'Ã„Â±n kalÃ„Â±cÃ„Â± mutasyonlarÃ„Â± reddetme sebebidir; kabul
	// ediyorsa "" olur. Sunumda bir motorun detay kodunu zaten
	// "mutations_held" yapan Ã…Å¸ey odur; neyin ÃƒÂ¶nerilebileceÃ„Å¸ine karar veren
	// kapÃ„Â±lar ondan tÃƒÂ¼retilen kelimeyi deÃ„Å¸il olgunun kendisini okusun diye
	// anlÃ„Â±k gÃƒÂ¶rÃƒÂ¼ntÃƒÂ¼de taÃ…Å¸Ã„Â±nÃ„Â±r. R-050.
	mutationHold string
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
	RequiresAdoptionAcknowledgement bool                      `json:"requires_adoption_acknowledgement"`
	Blockers                        []dnsEnginePreviewBlocker `json:"blockers"`
	Impacts                         []string                  `json:"impacts"`
	// AdoptedDirectives is what the takeover replaces in this server's own
	// options block, one entry per directive, each with the value found and the
	// value CelikPanel will set. It is structured because the browser renders
	// it as a list; a sentence could not be read as one (register R-042).
	//
	// AdoptedDirectives, devralmanÃ„Â±n bu sunucunun kendi seÃƒÂ§enek bloÃ„Å¸unda neyi
	// deÃ„Å¸iÃ…Å¸tirdiÃ„Å¸idir; direktif baÃ…Å¸Ã„Â±na bir kayÃ„Â±t, her birinde bulunan deÃ„Å¸er ve
	// CelikPanel'in koyacaÃ„Å¸Ã„Â± deÃ„Å¸er. YapÃ„Â±landÃ„Â±rÃ„Â±lmÃ„Â±Ã…Å¸tÃ„Â±r, ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ tarayÃ„Â±cÃ„Â± onu bir
	// liste olarak ÃƒÂ§izer; bir cÃƒÂ¼mle liste olarak okunamazdÃ„Â± (defter R-042).
	AdoptedDirectives []dnsEngineAdoptedDirective `json:"adopted_directives,omitempty"`
	// ViewFinding is why a takeover of this server cannot happen at all: its
	// DNS configuration declares views, or a file that configuration includes
	// could not be read. It carries the one place to look, because a refusal
	// the operator cannot act on is the defect the takeover work exists to fix
	// (register R-044).
	//
	// ViewFinding, bu sunucunun devralÃ„Â±nmasÃ„Â±nÃ„Â±n neden hiÃƒÂ§ olamayacaÃ„Å¸Ã„Â±dÃ„Â±r: DNS
	// yapÃ„Â±landÃ„Â±rmasÃ„Â± view bildiriyordur ya da o yapÃ„Â±landÃ„Â±rmanÃ„Â±n dahil ettiÃ„Å¸i bir
	// dosya okunamamÃ„Â±Ã…Å¸tÃ„Â±r. BakÃ„Â±lacak tek yeri taÃ…Å¸Ã„Â±r; ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ operatÃƒÂ¶rÃƒÂ¼n ÃƒÂ¼zerinde
	// iÃ…Å¸lem yapamayacaÃ„Å¸Ã„Â± bir ret, devralma iÃ…Å¸inin dÃƒÂ¼zeltmek iÃƒÂ§in var olduÃ„Å¸u
	// kusurdur (defter R-044).
	ViewFinding *dnsEngineViewFinding `json:"view_finding,omitempty"`
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
	// AdoptionAcknowledged is deliberately not DowntimeAcknowledged under
	// another name. Downtime says "answers may stop for a moment"; this says
	// "a DNS server you did not set up here is about to be reconfigured, and
	// it will stop serving whatever the panel does not know about". Folding
	// the two together would let a click meant for the first stand in for the
	// second.
	//
	// AdoptionAcknowledged, DowntimeAcknowledged'in baÃ…Å¸ka adÃ„Â± deÃ„Å¸ildir. Kesinti
	// "yanÃ„Â±tlar bir an duraklayabilir" der; bu ise "burada sizin kurmadÃ„Â±Ã„Å¸Ã„Â±nÃ„Â±z
	// bir DNS sunucusu yeniden yapÃ„Â±landÃ„Â±rÃ„Â±lacak ve panelin bilmediÃ„Å¸i ne varsa
	// sunmayÃ„Â± bÃ„Â±rakacak" der. Ã„Â°kisini birleÃ…Å¸tirmek, birincisi iÃƒÂ§in yapÃ„Â±lan bir
	// tÃ„Â±klamanÃ„Â±n ikincisinin yerine geÃƒÂ§mesine izin verirdi.
	AdoptionAcknowledged bool `json:"adoption_acknowledged"`
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
// Mutasyon tutmasÃ„Â± ikinci bir yoklamayla deÃ„Å¸il hazÃ„Â±rlÃ„Â±kla birlikte gelir: iki
// tur birbiriyle ÃƒÂ§eliÃ…Å¸ebilir ve ÃƒÂ§eliÃ…Å¸en bir ÃƒÂ§iftten kurulan bir sunum, tam da bu
// dosyanÃ„Â±n ÃƒÂ¼retmeye devam ettiÃ„Å¸i hata sÃ„Â±nÃ„Â±fÃ„Â±dÃ„Â±r.
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
			(runtime.PairReady || runtime.SecondaryReady) && (!runtime.Installed || !runtime.Running || !runtime.Managed) ||
			(runtime.PairReady && runtime.SecondaryReady) ||
			len(runtime.Unit) > 128 ||
			strings.ContainsAny(runtime.Unit, "\r\n\x00") ||
			!validateDNSForeignEngineOptions(runtime) ||
			!validateDNSForeignEngineViews(runtime) {
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
// mutationHold, agent'Ã„Â±n kalÃ„Â±cÃ„Â± mutasyonlarÃ„Â± reddetme sebebini taÃ…Å¸Ã„Â±r; kabul
// ediyorsa "" olur. Burada tek bir Ã…Å¸eyi deÃ„Å¸iÃ…Å¸tirir ve ÃƒÂ¶nemli olan da odur:
// panelin kurduÃ„Å¸u bir motor, onu sahiplenecek iÃ…Å¸lem takÃ„Â±lÃ„Â±yken Managed=false
// gÃƒÂ¶rÃƒÂ¼nÃƒÂ¼r ve bu olmadan ekran yabancÃ„Â± bir DNS sunucusu bildirir. "Kendi
// deÃ„Å¸iÃ…Å¸iklik sistemimiz tutuluyor" ile "baÃ…Å¸kasÃ„Â± bir DNS sunucusu kurmuÃ…Å¸" zÃ„Â±t
// teÃ…Å¸hislerdir; birincisi doÃ„Å¸ruyken operatÃƒÂ¶rÃƒÂ¼ ikincisinin peÃ…Å¸ine gÃƒÂ¶ndermek bir
// ÃƒÂ¶Ã„Å¸leden sonrayÃ„Â± yok eder.
//
// Durum mevcut kapalÃ„Â± kÃƒÂ¼menin iÃƒÂ§inde kalÃ„Â±r; dÃƒÂ¼zeltmeyi, sÃƒÂ¶zleÃ…Å¸me gereÃ„Å¸i serbest
// biÃƒÂ§imli olan detay kodu taÃ…Å¸Ã„Â±r. Burada yeni bir durum uydurmak, arayÃƒÂ¼z
// doÃ„Å¸rulayÃ„Â±cÃ„Â±sÃ„Â± tarafÃ„Â±ndan reddedilirdi.
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
	// Etkin motoru ve kurulu PowerDNS'i olmayan sunucuda bir bÃƒÂ¶lgeyi
	// imzalamÃ„Â±Ã…Å¸ olabilecek hiÃƒÂ§bir Ã…Å¸ey ve sorulacak bir arka uÃƒÂ§ yoktur. Yine
	// de sormak, ÃƒÂ¶nceden var olan her bÃƒÂ¶lgeyi "DNSSEC hazÃ„Â±rlÃ„Â±Ã„Å¸Ã„Â±
	// kullanÃ„Â±lamÃ„Â±yor"a ÃƒÂ§eviriyordu; sunum bunu "degraded" bildiriyor ve ilk
	// kurulum ÃƒÂ¶nizlemesi dnssec_unsupported, target_unavailable ve
	// source_degraded ile aynÃ„Â± anda reddediyordu (S-8 T1 Arch'ta, aynÃ„Â±
	// biÃƒÂ§imde Debian'da yeniden ÃƒÂ¼retildi; defter R-029, ÃƒÂ¼ÃƒÂ§ÃƒÂ¼ncÃƒÂ¼ kat).
	// Kurulu ama henÃƒÂ¼z devralÃ„Â±nmamÃ„Â±Ã…Å¸ eski bir PowerDNS yine sorgulanÃ„Â±r:
	// bÃƒÂ¶lgeleri pekÃƒÂ¢lÃƒÂ¢ imzalÃ„Â± olabilir; engelleyici tam bunun iÃƒÂ§in vardÃ„Â±r.
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
		mutationHold:    mutationHold,
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
			response.PendingCode != "" || response.Engine != "" || response.EngineEpoch != 0 ||
			response.AppliedGeneration != 0 {
			return errors.New("agent returned a mixed DNS publication failure response")
		}
		return errors.New("agent did not confirm the exact DNS publication")
	}
	if response.RecoveryPending {
		if response.Synced || response.Engine != request.Engine ||
			response.EngineEpoch != request.EngineEpoch ||
			response.AppliedGeneration != request.DesiredGeneration ||
			(response.PendingCode != "" && !transport.ValidDNSPeerPendingCode(response.PendingCode)) {
			return errors.New("agent returned an invalid pending DNS publication receipt")
		}
		return &dnsZoneV3PropagationPendingError{Code: response.PendingCode}
	}
	if response.PendingCode != "" || !response.Synced ||
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
		if response.Recovered || response.RecoveryPending || response.PendingCode != "" {
			return errors.New("agent returned a mixed DNS zone recovery failure response")
		}
		return errors.New("agent could not verify the exact DNS zone recovery")
	}
	if response.RecoveryPending {
		if response.Recovered || (response.PendingCode != "" &&
			!transport.ValidDNSPeerPendingCode(response.PendingCode)) {
			return errors.New("agent returned a mixed DNS zone recovery response")
		}
		return &dnsZoneV3PropagationPendingError{Code: response.PendingCode}
	}
	if response.PendingCode != "" || !response.Recovered {
		return errors.New("agent did not confirm the exact DNS zone recovery")
	}
	return nil
}

// dnsEngineActionReinstall names the repair the engine screen had no way to
// offer: the ledger says an engine owns this host, and the host has no copy of
// it. That is what a restored control plane looks like on a fresh server, and
// what a package removal behind the panel's back looks like on an old one.
// Calling it "install" and then refusing it as target_already_active and
// source_degraded told the operator the truth twice and offered nothing.
//
// dnsEngineActionReinstall, motor ekranÃ„Â±nÃ„Â±n sunacak yolu olmayan onarÃ„Â±mÃ„Â±
// adlandÃ„Â±rÃ„Â±r: defter bir motorun bu sunucunun sahibi olduÃ„Å¸unu sÃƒÂ¶yler ve
// sunucuda o motorun kopyasÃ„Â± yoktur. Geri yÃƒÂ¼klenmiÃ…Å¸ bir kontrol dÃƒÂ¼zleminin
// taze sunucudaki gÃƒÂ¶rÃƒÂ¼ntÃƒÂ¼sÃƒÂ¼, eski bir sunucuda ise panelin arkasÃ„Â±ndan paket
// kaldÃ„Â±rmanÃ„Â±n gÃƒÂ¶rÃƒÂ¼ntÃƒÂ¼sÃƒÂ¼ budur. Buna "kurulum" deyip target_already_active ve
// source_degraded ile reddetmek, operatÃƒÂ¶re doÃ„Å¸ruyu iki kez sÃƒÂ¶yleyip hiÃƒÂ§bir yol
// vermiyordu.
const dnsEngineActionReinstall = "reinstall_active"

// reinstallableActiveDNSEngine is the host shape the reinstall repairs: the
// durable ledger names this engine as the authority at a real epoch, the
// topology is standalone, the readiness probe answered, and the engine is not
// serving. Whether its packages are on disk is not part of the question Ã¢â‚¬â€ an
// absent engine and one whose packages a failed attempt already installed need
// the same repair, and the second is what the first leaves behind. Requiring
// the runtime to be wholly absent would offer the retry only until the first
// attempt made progress and then never again: the trap a first install fell
// into on a host where the package survived a failure (register R-028/R-029).
//
// What is NOT relaxed is the proof. The panel only decides what to offer; the
// agent independently refuses unless the engine's ownership receipt names it at
// the active epoch and nothing authoritative is listening, so the reinstall
// cannot run against a server the panel does not own.
//
// reinstallableActiveDNSEngine, yeniden kurulumun onardÃ„Â±Ã„Å¸Ã„Â± sunucu biÃƒÂ§imidir:
// kalÃ„Â±cÃ„Â± defter bu motoru gerÃƒÂ§ek bir ÃƒÂ§aÃ„Å¸da yetki sahibi olarak adlandÃ„Â±rÃ„Â±r,
// topoloji tek sunucudur, hazÃ„Â±rlÃ„Â±k yoklamasÃ„Â± cevap vermiÃ…Å¸tir ve motor hizmet
// vermiyordur. Paketlerinin diskte olup olmamasÃ„Â± sorunun parÃƒÂ§asÃ„Â± deÃ„Å¸ildir Ã¢â‚¬â€
// olmayan bir motor ile paketlerini dÃƒÂ¼Ã…Å¸mÃƒÂ¼Ã…Å¸ bir denemenin kurduÃ„Å¸u motor aynÃ„Â±
// onarÃ„Â±mÃ„Â± ister; ikincisi zaten birincisinin geride bÃ„Â±raktÃ„Â±Ã„Å¸Ã„Â±dÃ„Â±r. Ãƒâ€¡alÃ„Â±Ã…Å¸ma
// zamanÃ„Â±nÃ„Â±n tamamen yok olmasÃ„Â±nÃ„Â± istemek, yeniden denemeyi yalnÃ„Â±z ilk deneme
// ilerleme kaydedene kadar sunardÃ„Â±: paketin bir baÃ…Å¸arÃ„Â±sÃ„Â±zlÃ„Â±ktan saÃ„Å¸ ÃƒÂ§Ã„Â±ktÃ„Â±Ã„Å¸Ã„Â±
// sunucuda ilk kurulumun dÃƒÂ¼Ã…Å¸tÃƒÂ¼Ã„Å¸ÃƒÂ¼ tuzak (defter R-028/R-029).
//
// GevÃ…Å¸etilmeyen Ã…Å¸ey kanÃ„Â±ttÃ„Â±r. Panel yalnÃ„Â±z neyin sunulacaÃ„Å¸Ã„Â±na karar verir;
// agent, motorun sahiplik makbuzu onu etkin ÃƒÂ§aÃ„Å¸da adlandÃ„Â±rmadÃ„Â±kÃƒÂ§a ve yetki
// taÃ…Å¸Ã„Â±yan hiÃƒÂ§bir Ã…Å¸ey dinlemiyor olmadÃ„Â±kÃƒÂ§a baÃ„Å¸Ã„Â±msÃ„Â±z olarak reddeder; dolayÃ„Â±sÃ„Â±yla
// yeniden kurulum, panelin sahibi olmadÃ„Â±Ã„Å¸Ã„Â± bir sunucuda ÃƒÂ§alÃ„Â±Ã…Å¸amaz.
func reinstallableActiveDNSEngine(
	snapshot dnsEngineSnapshot,
	target transport.DNSEngine,
) bool {
	if snapshot.ActiveEngine == nil || *snapshot.ActiveEngine != target ||
		target != transport.DNSEngineBIND {
		return false
	}
	runtime := snapshot.runtime[target]
	return snapshot.runtimeErr == nil && snapshot.EngineEpoch >= 1 &&
		snapshot.Topology == transport.DNSTopologyStandalone &&
		snapshot.PairRole == "" && !runtime.Running
}

// dnsEngineActionAdoptUnmanaged names the explicit, informed takeover of a DNS
// server CelikPanel did not install. The host shape is ordinary and was, until
// now, a dead end: a provider image or an operator put the engine's packages on
// disk, nothing was ever configured, nothing is serving, and the panel has no
// ledger entry naming it. The preview called that "switch" and then refused it
// with target_unavailable and unmanaged_dns_detected, the service screens sent
// the operator back to the screen that refuses, and the only way out was to
// purge the package over SSH Ã¢â‚¬â€ which the product forbids (register R-038).
//
// The refusal had the facts right and the conclusion wrong. What is missing is
// not a proof, it is consent: adopting replaces that server's configuration
// with the panel's, and anything it serves today which the panel does not know
// about stops being served. So the panel names the takeover, says that in plain
// words and asks for its own acknowledgement, separate from the downtime one
// because the operator is agreeing to a different thing. Nothing else moves:
// the commit runs the ordinary first-install transaction, and the agent still
// proves independently that the target is not serving and that it owns every
// byte it writes.
//
// dnsEngineActionAdoptUnmanaged, CelikPanel'in kurmadÃ„Â±Ã„Å¸Ã„Â± bir DNS sunucusunun
// aÃƒÂ§Ã„Â±k ve bilgilendirilmiÃ…Å¸ devralÃ„Â±nmasÃ„Â±nÃ„Â± adlandÃ„Â±rÃ„Â±r. Sunucu biÃƒÂ§imi sÃ„Â±radandÃ„Â±r
// ve bugÃƒÂ¼ne kadar ÃƒÂ§Ã„Â±kmazdÃ„Â±: bir saÃ„Å¸layÃ„Â±cÃ„Â± imajÃ„Â± ya da bir operatÃƒÂ¶r motorun
// paketlerini diske koymuÃ…Å¸tur, hiÃƒÂ§bir Ã…Å¸ey yapÃ„Â±landÃ„Â±rÃ„Â±lmamÃ„Â±Ã…Å¸tÃ„Â±r, hiÃƒÂ§bir Ã…Å¸ey
// hizmet vermemektedir ve panelin defterinde onu adlandÃ„Â±ran bir kayÃ„Â±t yoktur.
// Ãƒâ€“nizleme buna "switch" deyip target_unavailable ve unmanaged_dns_detected ile
// reddediyor, servis ekranlarÃ„Â± operatÃƒÂ¶rÃƒÂ¼ reddeden ekrana geri gÃƒÂ¶nderiyor ve tek
// ÃƒÂ§Ã„Â±kÃ„Â±Ã…Å¸ paketi SSH ile kaldÃ„Â±rmak oluyordu Ã¢â‚¬â€ ÃƒÂ¼rÃƒÂ¼nÃƒÂ¼n yasakladÃ„Â±Ã„Å¸Ã„Â± Ã…Å¸ey (defter
// R-038).
//
// Ret, olgularÃ„Â± doÃ„Å¸ru, sonucu yanlÃ„Â±Ã…Å¸ kuruyordu. Eksik olan bir kanÃ„Â±t deÃ„Å¸il,
// rÃ„Â±zadÃ„Â±r: devralma o sunucunun yapÃ„Â±landÃ„Â±rmasÃ„Â±nÃ„Â± panelinkiyle deÃ„Å¸iÃ…Å¸tirir ve
// bugÃƒÂ¼n sunduÃ„Å¸u, panelin bilmediÃ„Å¸i her Ã…Å¸ey sunulmaz olur. Bu yÃƒÂ¼zden panel
// devralmayÃ„Â± adÃ„Â±yla anar, bunu dÃƒÂ¼z sÃƒÂ¶zlerle sÃƒÂ¶yler ve kesinti onayÃ„Â±ndan ayrÃ„Â±
// kendi onayÃ„Â±nÃ„Â± ister; ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ operatÃƒÂ¶r baÃ…Å¸ka bir Ã…Å¸eye rÃ„Â±za gÃƒÂ¶stermektedir. BaÃ…Å¸ka
// hiÃƒÂ§bir Ã…Å¸ey gevÃ…Å¸emez: commit sÃ„Â±radan ilk kurulum iÃ…Å¸lemini ÃƒÂ§alÃ„Â±Ã…Å¸tÃ„Â±rÃ„Â±r ve agent,
// hedefin hizmet vermediÃ„Å¸ini ve yazdÃ„Â±Ã„Å¸Ã„Â± her baytÃ„Â±n sahibi olduÃ„Å¸unu baÃ„Å¸Ã„Â±msÃ„Â±z
// olarak yine kanÃ„Â±tlar.
const dnsEngineActionAdoptUnmanaged = "adopt_unmanaged"

// adoptableUnmanagedDNSEngine is the exact host shape the takeover answers:
// BIND on disk, no durable authority recorded, the panel owning none of it, and
// a saved standalone identity to publish. It answers both halves of that shape.
//
// The stopped half (R-038) is the shape the agent's first-install transaction
// already accepts unchanged: nothing is listening, so the switch proofs pass as
// written. The running half (R-039) is a server that is answering queries right
// now, and it is a different operation with different evidence Ã¢â‚¬â€ the agent
// adopts it in place, never stopping or starting it, so the switch's
// not-serving proof and its port-53 pre-mutation guard are neither relaxed nor
// reached. Both halves are one action and one acknowledgement, because the
// operator is consenting to the same thing: this server's configuration becomes
// CelikPanel's.
//
// Running is therefore no longer a refusal, but it is still a fact the preview
// must carry, because the two halves cost different things and the impacts say
// so. It is trustworthy because the agent's readiness probe refuses to answer at
// all for a BIND whose unit topology is mixed, masked-but-unsealed or failed; a
// runtime error becomes target_unavailable a few lines below. And a running
// target does not make port 53 contested: the readiness probe allows the
// listener belonging to a running engine, so port53Conflict still means only
// "some other owner holds the port", which is still refused here.
//
// adoptableUnmanagedDNSEngine, devralmanÃ„Â±n yanÃ„Â±tladÃ„Â±Ã„Å¸Ã„Â± kesin sunucu biÃƒÂ§imidir:
// diskte BIND, kayÃ„Â±tlÃ„Â± kalÃ„Â±cÃ„Â± yetki yok, panel hiÃƒÂ§birinin sahibi deÃ„Å¸il ve
// yayÃ„Â±mlanacak kayÃ„Â±tlÃ„Â± tek sunucu kimliÃ„Å¸i var. Bu biÃƒÂ§imin iki yarÃ„Â±sÃ„Â±nÃ„Â± da
// yanÃ„Â±tlar.
//
// DurmuÃ…Å¸ yarÃ„Â± (R-038), agent'Ã„Â±n ilk kurulum iÃ…Å¸leminin deÃ„Å¸iÃ…Å¸meden kabul ettiÃ„Å¸i
// biÃƒÂ§imdir: dinleyen bir Ã…Å¸ey yoktur, dolayÃ„Â±sÃ„Â±yla geÃƒÂ§iÃ…Å¸ kanÃ„Â±tlarÃ„Â± yazÃ„Â±ldÃ„Â±Ã„Å¸Ã„Â± gibi
// geÃƒÂ§er. Ãƒâ€¡alÃ„Â±Ã…Å¸an yarÃ„Â± (R-039), Ã…Å¸u anda sorgu yanÃ„Â±tlayan bir sunucudur ve kanÃ„Â±tÃ„Â±
// baÃ…Å¸ka olan baÃ…Å¸ka bir iÃ…Å¸lemdir Ã¢â‚¬â€ agent onu yerinde devralÃ„Â±r, hiÃƒÂ§ durdurmaz ve
// baÃ…Å¸latmaz; bÃƒÂ¶ylece geÃƒÂ§iÃ…Å¸in hizmet-vermiyor kanÃ„Â±tÃ„Â± ile 53 numaralÃ„Â± baÃ„Å¸lantÃ„Â±
// noktasÃ„Â± ÃƒÂ¶n-mutasyon korumasÃ„Â± ne gevÃ…Å¸etilir ne de o yola girilir. Ã„Â°ki yarÃ„Â± tek
// eylem ve tek onaydÃ„Â±r; ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ operatÃƒÂ¶r aynÃ„Â± Ã…Å¸eye rÃ„Â±za gÃƒÂ¶sterir: bu sunucunun
// yapÃ„Â±landÃ„Â±rmasÃ„Â± CelikPanel'inki olur.
//
// Running artÃ„Â±k bir ret sebebi deÃ„Å¸ildir; ama ÃƒÂ¶nizlemenin taÃ…Å¸Ã„Â±masÃ„Â± gereken bir
// olgu olmayÃ„Â± sÃƒÂ¼rdÃƒÂ¼rÃƒÂ¼r, ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ iki yarÃ„Â±nÃ„Â±n bedeli farklÃ„Â±dÃ„Â±r ve etkiler bunu
// sÃƒÂ¶yler. GÃƒÂ¼venilirdir; ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ agent'Ã„Â±n hazÃ„Â±rlÃ„Â±k yoklamasÃ„Â± birim topolojisi
// karÃ„Â±Ã…Å¸Ã„Â±k, mÃƒÂ¼hÃƒÂ¼rsÃƒÂ¼z maskeli ya da dÃƒÂ¼Ã…Å¸mÃƒÂ¼Ã…Å¸ bir BIND iÃƒÂ§in hiÃƒÂ§ cevap vermez;
// ÃƒÂ§alÃ„Â±Ã…Å¸ma zamanÃ„Â± hatasÃ„Â± birkaÃƒÂ§ satÃ„Â±r aÃ…Å¸aÃ„Å¸Ã„Â±da target_unavailable olur. Ãƒâ€¡alÃ„Â±Ã…Å¸an
// bir hedef 53 numaralÃ„Â± baÃ„Å¸lantÃ„Â± noktasÃ„Â±nÃ„Â± da ÃƒÂ§ekiÃ…Å¸meli yapmaz: hazÃ„Â±rlÃ„Â±k
// yoklamasÃ„Â± ÃƒÂ§alÃ„Â±Ã…Å¸an bir motorun dinleyicisine izin verir, dolayÃ„Â±sÃ„Â±yla
// port53Conflict hÃƒÂ¢lÃƒÂ¢ yalnÃ„Â±z "baÃ„Å¸lantÃ„Â± noktasÃ„Â±nÃ„Â± baÃ…Å¸ka bir sahip tutuyor"
// demektir ve burada hÃƒÂ¢lÃƒÂ¢ reddedilir.
func adoptableUnmanagedDNSEngine(
	snapshot dnsEngineSnapshot,
	target transport.DNSEngine,
) bool {
	if target != transport.DNSEngineBIND {
		return false
	}
	if snapshot.runtimeErr != nil || snapshot.port53Conflict {
		return false
	}
	// R-050. A BIND the panel installed reads Managed=false while the
	// transaction that would have claimed it is held, which is exactly the
	// shape below this line: installed, unmanaged, nothing else serving. The
	// takeover would then be offered for the panel's own unfinished work, and
	// the sentence the operator reads - "a DNS server CelikPanel did not
	// install" - would be false about a server CelikPanel did install. The
	// commit would be refused anyway; the offer is the defect. While the hold
	// stands this host is the panel's and busy, which is what the engine
	// card's mutations_held blocker already says.
	//
	// R-050. Panelin kurduÃ„Å¸u bir BIND, onu sahiplenecek iÃ…Å¸lem tutulurken
	// Managed=false okunur; bu da tam olarak aÃ…Å¸aÃ„Å¸Ã„Â±daki biÃƒÂ§imdir. O hÃƒÂ¢lde
	// devralma, panelin kendi yarÃ„Â±m iÃ…Å¸i iÃƒÂ§in ÃƒÂ¶nerilirdi ve operatÃƒÂ¶rÃƒÂ¼n okuduÃ„Å¸u
	// cÃƒÂ¼mle - "CelikPanel'in kurmadÃ„Â±Ã„Å¸Ã„Â± bir DNS sunucusu" - panelin kurduÃ„Å¸u bir
	// sunucu hakkÃ„Â±nda yanlÃ„Â±Ã…Å¸ olurdu. Commit zaten reddedilirdi; kusur olan
	// tekliftir. Tutma sÃƒÂ¼rdÃƒÂ¼kÃƒÂ§e bu sunucu panelindir ve meÃ…Å¸guldÃƒÂ¼r.
	if snapshot.mutationHold != "" {
		return false
	}
	if snapshot.ActiveEngine != nil || snapshot.EngineEpoch != 0 {
		return false
	}
	if snapshot.Topology != transport.DNSTopologyStandalone ||
		snapshot.PairRole != "" {
		return false
	}
	runtime := snapshot.runtime[target]
	if !runtime.Installed || runtime.Managed {
		return false
	}
	// The presentation state is derived, not authoritative, and it derives a
	// different word for the same host depending on nothing but Running: with
	// no active engine recorded, a stopped unmanaged BIND reads "unconfigured"
	// and a running one reads "unmanaged". Both are the takeover's shape. Only
	// those two are accepted, and "unmanaged" only when it is this target that
	// is running Ã¢â‚¬â€ otherwise the word is describing some other engine.
	//
	// Sunum durumu tÃƒÂ¼retilmiÃ…Å¸tir, yetkili deÃ„Å¸ildir ve aynÃ„Â± sunucu iÃƒÂ§in yalnÃ„Â±z
	// Running'e bakarak farklÃ„Â± bir kelime tÃƒÂ¼retir: kayÃ„Â±tlÃ„Â± etkin motor yokken
	// durmuÃ…Å¸ panel dÃ„Â±Ã…Å¸Ã„Â± bir BIND "unconfigured", ÃƒÂ§alÃ„Â±Ã…Å¸anÃ„Â± "unmanaged" okunur.
	// Ã„Â°kisi de devralmanÃ„Â±n biÃƒÂ§imidir. YalnÃ„Â±z bu ikisi kabul edilir ve
	// "unmanaged" yalnÃ„Â±z ÃƒÂ§alÃ„Â±Ã…Å¸an bu hedefken; aksi hÃƒÂ¢lde kelime baÃ…Å¸ka bir
	// motoru anlatÃ„Â±yordur.
	switch snapshot.State {
	case dnsEngineStateUnconfigured:
	case dnsEngineStateUnmanaged:
		if !runtime.Running {
			return false
		}
	default:
		return false
	}
	// "No engine is active" is the register's own wording and is proven here
	// directly rather than inferred from the presentation string.
	//
	// "HiÃƒÂ§bir motor etkin deÃ„Å¸il", defterin kendi ifadesidir ve burada sunum
	// dizesinden ÃƒÂ§Ã„Â±karsanmak yerine doÃ„Å¸rudan kanÃ„Â±tlanÃ„Â±r.
	for engine, other := range snapshot.runtime {
		if engine != target && other.Running {
			return false
		}
	}
	return true
}

func dnsEngineAction(
	snapshot dnsEngineSnapshot,
	target transport.DNSEngine,
) string {
	runtime := snapshot.runtime[target]
	if reinstallableActiveDNSEngine(snapshot, target) {
		return dnsEngineActionReinstall
	}
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
	if adoptableUnmanagedDNSEngine(snapshot, target) {
		return dnsEngineActionAdoptUnmanaged
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

func dnsEngineImpacts(
	action string,
	hasSource, targetRunning bool,
) []string {
	if action == "adopt" {
		return []string{"validate_target", "adopt_existing"}
	}
	// The takeover installs nothing and stops nothing, so it lists neither.
	// What it does list is the part the operator is actually consenting to.
	//
	// The two halves cost different things and must not claim each other's
	// cost. The stopped half starts a server that is down and its unknown
	// zones are not being answered anyway. The running half never stops the
	// server: BIND re-reads its configuration in place, the process and its
	// sockets survive, so what happens is a reload and not a start, and
	// promising an interruption it does not have would be a false cost.
	//
	// The running half also keeps the foreign zones. CelikPanel's BIND
	// generation is additive - it adds an include and an options block to the
	// server's own files and deletes no zone declaration - so a zone this
	// server answers today keeps being answered, unmanaged, after the
	// takeover. Saying "it stops being served" there would be a false loss,
	// which is worse than a false cost: it invites the operator to refuse a
	// safe change, or to go and rescue zones that were never in danger.
	//
	// Devralma hiÃƒÂ§bir Ã…Å¸ey kurmaz ve hiÃƒÂ§bir Ã…Å¸ey durdurmaz; ikisini de saymaz.
	// SaydÃ„Â±Ã„Å¸Ã„Â± Ã…Å¸ey, operatÃƒÂ¶rÃƒÂ¼n gerÃƒÂ§ekten rÃ„Â±za gÃƒÂ¶sterdiÃ„Å¸i kÃ„Â±sÃ„Â±mdÃ„Â±r.
	//
	// Ã„Â°ki yarÃ„Â±nÃ„Â±n bedeli farklÃ„Â±dÃ„Â±r ve biri diÃ„Å¸erinin bedelini iddia etmemeli.
	// DurmuÃ…Å¸ yarÃ„Â±, kapalÃ„Â± bir sunucuyu baÃ…Å¸latÃ„Â±r ve bilinmeyen bÃƒÂ¶lgeleri zaten
	// yanÃ„Â±tlanmÃ„Â±yordur. Ãƒâ€¡alÃ„Â±Ã…Å¸an yarÃ„Â± sunucuyu hiÃƒÂ§ durdurmaz: BIND
	// yapÃ„Â±landÃ„Â±rmasÃ„Â±nÃ„Â± yerinde yeniden okur, sÃƒÂ¼reÃƒÂ§ ve soketleri yaÃ…Å¸amayÃ„Â±
	// sÃƒÂ¼rdÃƒÂ¼rÃƒÂ¼r; dolayÃ„Â±sÃ„Â±yla olan Ã…Å¸ey baÃ…Å¸latma deÃ„Å¸il yeniden yÃƒÂ¼klemedir ve
	// olmayan bir kesintiyi vaat etmek yanlÃ„Â±Ã…Å¸ bir bedel olurdu.
	//
	// Ãƒâ€¡alÃ„Â±Ã…Å¸an yarÃ„Â±, yabancÃ„Â± bÃƒÂ¶lgeleri de korur. CelikPanel'in BIND nesli
	// eklemelidir - sunucunun kendi dosyalarÃ„Â±na bir include ve bir seÃƒÂ§enek
	// bloÃ„Å¸u ekler, hiÃƒÂ§bir bÃƒÂ¶lge bildirimini silmez - dolayÃ„Â±sÃ„Â±yla bu sunucunun
	// bugÃƒÂ¼n yanÃ„Â±tladÃ„Â±Ã„Å¸Ã„Â± bir bÃƒÂ¶lge, devralmadan sonra da yÃƒÂ¶netilmeden
	// yanÃ„Â±tlanmayÃ„Â± sÃƒÂ¼rdÃƒÂ¼rÃƒÂ¼r. Orada "sunulmaz olur" demek yanlÃ„Â±Ã…Å¸ bir kayÃ„Â±p
	// olurdu; bu, yanlÃ„Â±Ã…Å¸ bedelden kÃƒÂ¶tÃƒÂ¼dÃƒÂ¼r: operatÃƒÂ¶rÃƒÂ¼ gÃƒÂ¼venli bir deÃ„Å¸iÃ…Å¸ikliÃ„Å¸i
	// reddetmeye ya da hiÃƒÂ§ tehlikede olmayan bÃƒÂ¶lgeleri kurtarmaya ÃƒÂ§aÃ„Å¸Ã„Â±rÃ„Â±r.
	if action == dnsEngineActionAdoptUnmanaged {
		if targetRunning {
			return []string{
				"replace_foreign_config", "validate_target", "publish_zones",
				"reload_target", "keep_unknown_zones",
			}
		}
		return []string{
			"replace_foreign_config", "validate_target", "publish_zones",
			"start_target", "keep_unknown_zones",
		}
	}
	// Nothing is serving, so nothing stops and nothing is interrupted. Listing
	// stop_source or brief_dns_interruption here would promise a cost that
	// cannot be paid twice: the outage already happened.
	//
	// HiÃƒÂ§bir Ã…Å¸ey hizmet vermiyor; dolayÃ„Â±sÃ„Â±yla duracak ve kesilecek bir Ã…Å¸ey de
	// yok. Burada stop_source ya da brief_dns_interruption saymak, iki kez
	// ÃƒÂ¶denemeyecek bir bedel vaat etmek olurdu: kesinti ÃƒÂ§oktan yaÃ…Å¸andÃ„Â±.
	if action == dnsEngineActionReinstall {
		return []string{
			"install_target", "validate_target", "publish_zones", "start_target",
		}
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
	if (action == "switch" || action == "install") && target == transport.DNSEnginePowerDNS &&
		snapshot.Topology == transport.DNSTopologyPaired &&
		snapshot.PairRole == transport.DNSPairRolePrimary {
		blockers = addDNSEngineBlocker(blockers, "pdns_primary_switch_paused")
	}
	reinstall := action == dnsEngineActionReinstall
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
	// Ã„Â°lk kurulumun kaynak motoru yoktur; bÃƒÂ¶lgeleri yapÃ„Â±sÃ„Â± gereÃ„Å¸i bekler:
	// onlarÃ„Â± uygulamÃ„Â±Ã…Å¸ olabilecek hiÃƒÂ§bir Ã…Å¸ey yoktur ve kurulumun kendisi her
	// bÃƒÂ¶lgeyi istenen neslinde yayÃ„Â±mlayÃ„Â±p commit'te uygulandÃ„Â± iÃ…Å¸aretler. Bunu
	// engelleyici saymak, ÃƒÂ¶nce alan adÃ„Â± eklenmiÃ…Å¸ her sunucuda ilk motor
	// kurulumunu ulaÃ…Å¸Ã„Â±lamaz kÃ„Â±lÃ„Â±yordu (S-7 T1, defter R-029). Kaynak motor
	// etkinken engelleyici kalÃ„Â±r: orada bekleme, kaynaÃ„Å¸Ã„Â±n yetiÃ…Å¸mediÃ„Å¸i
	// anlamÃ„Â±na gelir ve geÃƒÂ§iÃ…Å¸ oturmamÃ„Â±Ã…Å¸ bir bÃƒÂ¶lge kÃƒÂ¼mesini kopyalamamalÃ„Â±dÃ„Â±r.
	//
	// A reinstall has no source that could catch up either: the engine the
	// pending zones are waiting for does not exist on this host. The reinstall
	// itself republishes every zone at its desired generation, exactly as the
	// first install does, so a pending zone is the reason to run it rather than
	// a reason to refuse it.
	//
	// Yeniden kurulumun da yetiÃ…Å¸ebilecek bir kaynaÃ„Å¸Ã„Â± yoktur: bekleyen
	// bÃƒÂ¶lgelerin beklediÃ„Å¸i motor bu sunucuda mevcut deÃ„Å¸ildir. Yeniden
	// kurulumun kendisi her bÃƒÂ¶lgeyi istenen neslinde ilk kurulumla birebir
	// aynÃ„Â± biÃƒÂ§imde yeniden yayÃ„Â±mlar; dolayÃ„Â±sÃ„Â±yla bekleyen bir bÃƒÂ¶lge, onu
	// reddetme deÃ„Å¸il ÃƒÂ§alÃ„Â±Ã…Å¸tÃ„Â±rma sebebidir.
	if action != "adopt" && !reinstall && snapshot.ActiveEngine != nil &&
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
	// target_already_active is the right refusal for "you asked to switch to
	// the engine you are already running". It is the wrong refusal for "the
	// engine you are recorded as running is not on this machine": there, being
	// the active engine is the whole reason the operator may reinstall it.
	//
	// target_already_active, "zaten ÃƒÂ§alÃ„Â±Ã…Å¸tÃ„Â±rdÃ„Â±Ã„Å¸Ã„Â±nÃ„Â±z motora geÃƒÂ§mek istediniz"
	// iÃƒÂ§in doÃ„Å¸ru retdir. "Ãƒâ€¡alÃ„Â±Ã…Å¸tÃ„Â±rdÃ„Â±Ã„Å¸Ã„Â±nÃ„Â±z kayÃ„Â±tlÃ„Â± motor bu makinede yok" iÃƒÂ§in
	// yanlÃ„Â±Ã…Å¸ retdir: orada etkin motor olmak, operatÃƒÂ¶rÃƒÂ¼n onu yeniden
	// kurabilmesinin ta kendisidir.
	if !reinstall && snapshot.ActiveEngine != nil &&
		*snapshot.ActiveEngine == target {
		blockers = addDNSEngineBlocker(blockers, "target_already_active")
	}
	targetRuntime := snapshot.runtime[target]
	// A durable switch always has a source. Registration-only PowerDNS paths use
	// adopt or reconfigure. Every other source-free switch, and an install while
	// runtime state proves the server is not unconfigured, must stop at preview.
	if snapshot.ActiveEngine == nil &&
		action != "adopt" && action != "reconfigure" &&
		action != dnsEngineActionAdoptUnmanaged &&
		(action == "switch" || snapshot.State != dnsEngineStateUnconfigured) {
		blockers = addDNSEngineBlocker(blockers, "target_unavailable")
	}
	// unmanaged_dns_detected means "someone else installed a DNS server here".
	// It cannot mean that about the engine the panel's own ledger records as the
	// authority on this host while nothing is serving: the packages are either
	// the panel's own interrupted install or a stopped copy of the server the
	// panel already owns, and in both cases the reinstall is the repair. The
	// agent still proves ownership at the active epoch before it touches
	// anything, so nothing is taken on trust here.
	//
	// unmanaged_dns_detected, "buraya baÃ…Å¸ka biri bir DNS sunucusu kurmuÃ…Å¸"
	// demektir. HiÃƒÂ§bir Ã…Å¸ey hizmet vermezken, panelin kendi defterinin bu
	// sunucudaki yetki sahibi olarak kaydettiÃ„Å¸i motor hakkÃ„Â±nda bunu sÃƒÂ¶yleyemez:
	// paketler ya panelin kendi yarÃ„Â±m kalmÃ„Â±Ã…Å¸ kurulumudur ya da panelin zaten
	// sahibi olduÃ„Å¸u sunucunun durdurulmuÃ…Å¸ bir kopyasÃ„Â±dÃ„Â±r; her ikisinde de onarÃ„Â±m
	// yeniden kurulumdur. Agent, hiÃƒÂ§bir Ã…Å¸eye dokunmadan ÃƒÂ¶nce etkin ÃƒÂ§aÃ„Å¸daki
	// sahipliÃ„Å¸i yine de kanÃ„Â±tlar; burada hiÃƒÂ§bir Ã…Å¸ey gÃƒÂ¼vene bÃ„Â±rakÃ„Â±lmaz.
	// The takeover is the answer to unmanaged_dns_detected, not something the
	// blocker may refuse: adoptableUnmanagedDNSEngine has already proven the
	// exact shape, and the action itself carries the operator's consent to it.
	//
	// Devralma, unmanaged_dns_detected'in cevabÃ„Â±dÃ„Â±r; engelleyicinin
	// reddedebileceÃ„Å¸i bir Ã…Å¸ey deÃ„Å¸il: adoptableUnmanagedDNSEngine kesin biÃƒÂ§imi
	// ÃƒÂ§oktan kanÃ„Â±tlamÃ„Â±Ã…Å¸tÃ„Â±r ve eylemin kendisi operatÃƒÂ¶rÃƒÂ¼n buna rÃ„Â±zasÃ„Â±nÃ„Â± taÃ…Å¸Ã„Â±r.
	if !reinstall && action != dnsEngineActionAdoptUnmanaged &&
		targetRuntime.Installed && !targetRuntime.Managed {
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
		// The degraded state IS the reinstall's precondition, not an obstacle
		// to it. Refusing here left the only repair unreachable behind a
		// description of the thing being repaired.
		//
		// BozulmuÃ…Å¸ durum, yeniden kurulumun engeli deÃ„Å¸il ÃƒÂ¶n koÃ…Å¸uludur. Burada
		// reddetmek, tek onarÃ„Â±mÃ„Â±, onarÃ„Â±lan Ã…Å¸eyin tarifinin arkasÃ„Â±nda
		// ulaÃ…Å¸Ã„Â±lamaz bÃ„Â±rakÃ„Â±yordu.
		if !reinstall {
			blockers = addDNSEngineBlocker(blockers, "source_degraded")
		}
	case dnsEngineStateUnmanaged:
		// A running unmanaged engine IS what this state describes, and the
		// takeover is its answer rather than something this branch may refuse:
		// adoptableUnmanagedDNSEngine has already proven the exact shape, and
		// the action carries the operator's consent to it. Without this the
		// preview would name the takeover and block it in the same breath.
		//
		// Ãƒâ€¡alÃ„Â±Ã…Å¸an panel dÃ„Â±Ã…Å¸Ã„Â± bir motor, bu durumun anlattÃ„Â±Ã„Å¸Ã„Â± Ã…Å¸eyin ta
		// kendisidir ve devralma, bu dalÃ„Â±n reddedebileceÃ„Å¸i bir Ã…Å¸ey deÃ„Å¸il onun
		// cevabÃ„Â±dÃ„Â±r: adoptableUnmanagedDNSEngine kesin biÃƒÂ§imi ÃƒÂ§oktan
		// kanÃ„Â±tlamÃ„Â±Ã…Å¸tÃ„Â±r ve eylem operatÃƒÂ¶rÃƒÂ¼n rÃ„Â±zasÃ„Â±nÃ„Â± taÃ…Å¸Ã„Â±r. Bu olmadan
		// ÃƒÂ¶nizleme devralmayÃ„Â± aynÃ„Â± nefeste hem adlandÃ„Â±rÃ„Â±r hem engellerdi.
		if action == dnsEngineActionAdoptUnmanaged {
			break
		}
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
	// A reinstall reuses the epoch it is repairing. Every other mode moves the
	// epoch forward by one because authority changes hands; here it does not,
	// and an epoch bump would tell the host it is serving a tenure it never
	// started.
	//
	// Yeniden kurulum onardÃ„Â±Ã„Å¸Ã„Â± ÃƒÂ§aÃ„Å¸Ã„Â± yeniden kullanÃ„Â±r. DiÃ„Å¸er her kip ÃƒÂ§aÃ„Å¸Ã„Â± bir
	// artÃ„Â±rÃ„Â±r ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ yetki el deÃ„Å¸iÃ…Å¸tirir; burada deÃ„Å¸iÃ…Å¸tirmez ve ÃƒÂ§aÃ„Å¸Ã„Â± artÃ„Â±rmak
	// sunucuya hiÃƒÂ§ baÃ…Å¸lamadÃ„Â±Ã„Å¸Ã„Â± bir dÃƒÂ¶nemi sunduÃ„Å¸unu sÃƒÂ¶ylemek olurdu.
	targetEpoch := state.EngineEpoch + 1
	switch mode {
	case transport.DNSEngineSwitchModeReinstall:
		targetEpoch = state.EngineEpoch
		if state.ActiveEngine != target || state.EngineEpoch < 1 ||
			topology != transport.DNSTopologyStandalone ||
			state.PairRole != "" {
			return mutationpayload.DNSEngineSwitchManifestCommitment{},
				errors.New("DNS engine reinstall identity is invalid")
		}
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
		state.ActiveEngine, target, state.EngineEpoch, targetEpoch,
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
	// What the takeover replaces in this server's own options block, and
	// whether any of it is something the host could not read as a directive of
	// its own. A refusal is raised here, before a token exists, so the operator
	// gets the directive and the line on the screen they are standing on rather
	// than a failed commit later (register R-042).
	//
	// DevralmanÃ„Â±n bu sunucunun kendi seÃƒÂ§enek bloÃ„Å¸unda neyi deÃ„Å¸iÃ…Å¸tirdiÃ„Å¸i ve
	// bunlardan birinin, sunucunun kendi deyimi olarak okuyamadÃ„Â±Ã„Å¸Ã„Â± bir Ã…Å¸ey olup
	// olmadÃ„Â±Ã„Å¸Ã„Â±. Ret burada, daha bir belirteÃƒÂ§ yokken kaldÃ„Â±rÃ„Â±lÃ„Â±r; bÃƒÂ¶ylece operatÃƒÂ¶r
	// direktifi ve satÃ„Â±rÃ„Â±, sonradan dÃƒÂ¼Ã…Å¸en bir commit yerine ÃƒÂ¼zerinde durduÃ„Å¸u
	// ekranda alÃ„Â±r (defter R-042).
	adoptedDirectives := []dnsEngineAdoptedDirective(nil)
	viewFinding := (*dnsEngineViewFinding)(nil)
	if action == dnsEngineActionAdoptUnmanaged {
		adoptedDirectives = dnsEngineAdoptedDirectives(
			snapshot.runtime[request.TargetEngine],
		)
		if dnsEngineAdoptionRefused(adoptedDirectives) {
			blockers = addDNSEngineBlocker(
				blockers, dnsEngineAdoptionOptionsBlocker,
			)
		}
		// A server configured with views cannot be taken over at all yet, and
		// neither can one whose configuration CelikPanel could not read whole.
		// This is decided here, with the options list, because both refusals
		// belong on the same screen at the same moment (register R-044).
		//
		// View ile yapÃ„Â±landÃ„Â±rÃ„Â±lmÃ„Â±Ã…Å¸ bir sunucu henÃƒÂ¼z hiÃƒÂ§ devralÃ„Â±namaz;
		// yapÃ„Â±landÃ„Â±rmasÃ„Â± CelikPanel tarafÃ„Â±ndan bÃƒÂ¼tÃƒÂ¼nÃƒÂ¼yle okunamayan bir sunucu
		// da ÃƒÂ¶yle. Buna, seÃƒÂ§enek listesiyle birlikte burada karar verilir;
		// ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ iki ret de aynÃ„Â± anda aynÃ„Â± ekrana aittir (defter R-044).
		viewFinding = dnsEngineViewFindingOf(
			snapshot.runtime[request.TargetEngine],
		)
		if code := dnsEngineViewBlocker(viewFinding); code != "" {
			blockers = addDNSEngineBlocker(blockers, code)
		}
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
	// A reinstall names a source engine because that engine owns the host, but
	// nothing of it is running: there is no service to interrupt and therefore
	// no outage to acknowledge. Asking for the acknowledgement anyway would
	// make the operator confirm a cost the change does not have.
	//
	// Yeniden kurulum bir kaynak motoru adlandÃ„Â±rÃ„Â±r ÃƒÂ§ÃƒÂ¼nkÃƒÂ¼ o motor sunucunun
	// sahibidir; ama ondan ÃƒÂ§alÃ„Â±Ã…Å¸an hiÃƒÂ§bir Ã…Å¸ey yoktur: kesilecek hizmet, dolayÃ„Â±sÃ„Â±yla
	// onaylanacak kesinti de yoktur. Yine de onay istemek, operatÃƒÂ¶re deÃ„Å¸iÃ…Å¸ikliÃ„Å¸in
	// taÃ…Å¸Ã„Â±madÃ„Â±Ã„Å¸Ã„Â± bir bedeli onaylatmak olurdu.
	requiresAck := (hasSource || action == "reconfigure") &&
		action != dnsEngineActionReinstall
	preview := dnsEngineSwitchPreview{
		PreviewToken: token, SourceEngine: enginePointer(source),
		TargetEngine:     request.TargetEngine,
		ExpectedRevision: request.ExpectedRevision,
		Action:           action, Topology: snapshot.Topology,
		ZoneCount:                       snapshot.ZoneCount,
		PendingZoneCount:                snapshot.PendingZoneCount,
		DNSSECZoneCount:                 snapshot.DNSSECZoneCount,
		RequiresDowntimeAcknowledgement: requiresAck,
		RequiresAdoptionAcknowledgement: action == dnsEngineActionAdoptUnmanaged,
		Blockers:                        blockers,
		Impacts: dnsEngineImpacts(
			action, hasSource, snapshot.runtime[request.TargetEngine].Running,
		),
		AdoptedDirectives: adoptedDirectives,
		ViewFinding:       viewFinding,
	}
	if requiresAck {
		preview.EstimatedDowntimeSeconds = dnsEngineEstimatedOutage
	}
	// A blocked preview never reached the cache, so its token could never be
	// consumed; handing one out anyway meant the commit answered "preview
	// expired or no longer matches this request" for a preview that had
	// neither expired nor changed. The operator was sent to look for a race
	// that never happened, while the real answer Ã¢â‚¬â€ the named blockers Ã¢â‚¬â€ was
	// already on screen. No token, no false trail.
	//
	// EngellenmiÃ…Å¸ ÃƒÂ¶nizleme ÃƒÂ¶nbelleÃ„Å¸e hiÃƒÂ§ girmiyordu; dolayÃ„Â±sÃ„Â±yla belirteci de
	// hiÃƒÂ§ tÃƒÂ¼ketilemezdi. Yine de bir belirteÃƒÂ§ vermek, ne sÃƒÂ¼resi dolmuÃ…Å¸ ne de
	// deÃ„Å¸iÃ…Å¸miÃ…Å¸ bir ÃƒÂ¶nizleme iÃƒÂ§in commit'in "ÃƒÂ¶nizlemenin sÃƒÂ¼resi doldu ya da bu
	// isteÃ„Å¸e artÃ„Â±k uymuyor" demesi anlamÃ„Â±na geliyordu. OperatÃƒÂ¶r hiÃƒÂ§ yaÃ…Å¸anmamÃ„Â±Ã…Å¸
	// bir yarÃ„Â±Ã…Å¸Ã„Â± aramaya gÃƒÂ¶nderiliyor, gerÃƒÂ§ek cevap Ã¢â‚¬â€ adÃ„Â± konmuÃ…Å¸ engelleyiciler
	// Ã¢â‚¬â€ zaten ekranda duruyordu. BelirteÃƒÂ§ yok, yanlÃ„Â±Ã…Å¸ iz yok.
	if len(blockers) != 0 {
		preview.PreviewToken = ""
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
		preview.PreviewToken = ""
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
	// A takeover is a first install whose packages happened to be there
	// already: same mode, same absent source, same epoch 0 to 1, same target.
	// Leaving it outside this scope would give the one shape R-038 exists for
	// a failure with no repair, which is the wedge, not the fix.
	//
	// Devralma, paketleri zaten orada olan bir ilk kurulumdur: aynÃ„Â± kip, aynÃ„Â±
	// yok kaynak, aynÃ„Â± 0'dan 1'e ÃƒÂ§aÃ„Å¸, aynÃ„Â± hedef. Onu bu kapsamÃ„Â±n dÃ„Â±Ã…Å¸Ã„Â±nda
	// bÃ„Â±rakmak, R-038'in var olma sebebi olan biÃƒÂ§ime onarÃ„Â±msÃ„Â±z bir
	// baÃ…Å¸arÃ„Â±sÃ„Â±zlÃ„Â±k verirdi; bu dÃƒÂ¼zeltme deÃ„Å¸il, tam da o ÃƒÂ§Ã„Â±kmazdÃ„Â±r.
	if persisted.Mode != transport.DNSEngineSwitchModeSwitch ||
		(persisted.Action != "install" &&
			persisted.Action != dnsEngineActionAdoptUnmanaged) ||
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
	if persisted.Action == dnsEngineActionAdoptUnmanaged {
		// A takeover that failed must leave the host exactly as it found it,
		// and it can have found it in either of two shapes. The stopped shape
		// had nothing running, so nothing may be running now. The running
		// shape had the operator's own DNS server answering, unmanaged, and it
		// must be answering still - the whole point of adopting in place is
		// that a failure costs no outage, so demanding silence here would
		// refuse the correct outcome and wedge the host after a rollback that
		// worked.
		//
		// What is refused is a target that came back MANAGED: that is not the
		// server the takeover found, it is a half-finished takeover, and it is
		// exactly the state a rollback exists to prevent. Another engine
		// running is refused for the same reason it always was.
		//
		// BaÃ…Å¸arÃ„Â±sÃ„Â±z bir devralma sunucuyu bulduÃ„Å¸u gibi bÃ„Â±rakmalÃ„Â±dÃ„Â±r ve onu iki
		// biÃƒÂ§imden birinde bulmuÃ…Å¸ olabilir. DurmuÃ…Å¸ biÃƒÂ§imde ÃƒÂ§alÃ„Â±Ã…Å¸an bir Ã…Å¸ey
		// yoktu; Ã…Å¸imdi de ÃƒÂ§alÃ„Â±Ã…Å¸an bir Ã…Å¸ey olmamalÃ„Â±. Ãƒâ€¡alÃ„Â±Ã…Å¸an biÃƒÂ§imde
		// operatÃƒÂ¶rÃƒÂ¼n kendi DNS sunucusu panel dÃ„Â±Ã…Å¸Ã„Â± olarak yanÃ„Â±t veriyordu ve
		// hÃƒÂ¢lÃƒÂ¢ yanÃ„Â±t veriyor olmalÃ„Â±dÃ„Â±r - yerinde devralmanÃ„Â±n bÃƒÂ¼tÃƒÂ¼n anlamÃ„Â± bir
		// baÃ…Å¸arÃ„Â±sÃ„Â±zlÃ„Â±Ã„Å¸Ã„Â±n kesintiye mal olmamasÃ„Â±dÃ„Â±r; burada sessizlik istemek
		// doÃ„Å¸ru sonucu reddeder ve ÃƒÂ§alÃ„Â±Ã…Å¸mÃ„Â±Ã…Å¸ bir geri almadan sonra sunucuyu
		// ÃƒÂ§Ã„Â±kmaza sokardÃ„Â±.
		//
		// Reddedilen Ã…Å¸ey YÃƒâ€“NETÃ„Â°LEN dÃƒÂ¶nen bir hedeftir: o, devralmanÃ„Â±n bulduÃ„Å¸u
		// sunucu deÃ„Å¸ildir, yarÃ„Â±m kalmÃ„Â±Ã…Å¸ bir devralmadÃ„Â±r ve geri almanÃ„Â±n
		// ÃƒÂ¶nlemek iÃƒÂ§in var olduÃ„Å¸u durumun ta kendisidir. BaÃ…Å¸ka bir motorun
		// ÃƒÂ§alÃ„Â±Ã…Å¸masÃ„Â±, her zamanki sebeple reddedilir.
		target := runtimes[persisted.TargetEngine]
		if persisted.SourceEngine != "" ||
			persisted.TargetEngine != transport.DNSEngineBIND ||
			target.Managed || (target.Running && !target.Installed) {
			return errors.New("DNS takeover rollback is not proven")
		}
		for engine, runtime := range runtimes {
			if engine != persisted.TargetEngine && runtime.Running {
				return errors.New("another DNS engine is running after takeover failure")
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
// agent is already true of an unverified outcome Ã¢â‚¬â€ the change did not complete
// and state must be refreshed Ã¢â‚¬â€ but the operator's next action is different:
// nothing will retry on its own, the agent's health is the problem, and the
// hold code says which health problem. The message is fixed English and the
// code is one of the stable MutationHold* values; no internal error text is
// forwarded.
// writeDNSEngineMutationsHeld tutulmayÃ„Â± adlandÃ„Â±rÃ„Â±r. Tutulan bir agent hakkÃ„Â±nda
// geri kalan her Ã…Å¸ey doÃ„Å¸rulanmamÃ„Â±Ã…Å¸ bir sonuÃƒÂ§ iÃƒÂ§in zaten geÃƒÂ§erlidir Ã¢â‚¬â€ deÃ„Å¸iÃ…Å¸iklik
// tamamlanmadÃ„Â± ve durum yenilenmeli Ã¢â‚¬â€ ama operatÃƒÂ¶rÃƒÂ¼n bir sonraki adÃ„Â±mÃ„Â±
// farklÃ„Â±dÃ„Â±r: hiÃƒÂ§bir Ã…Å¸ey kendiliÃ„Å¸inden yeniden denemeyecek, sorun agent'Ã„Â±n
// saÃ„Å¸lÃ„Â±Ã„Å¸Ã„Â±dÃ„Â±r ve tutulma kodu hangi saÃ„Å¸lÃ„Â±k sorunu olduÃ„Å¸unu sÃƒÂ¶yler. Mesaj sabit
// Ã„Â°ngilizcedir, kod kararlÃ„Â± MutationHold* deÃ„Å¸erlerinden biridir; hiÃƒÂ§bir iÃƒÂ§ hata
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

// writeUnknownDNSEnginePreviewConflict answers a commit whose preview token the
// panel does not hold. There are two reasons for that and they need different
// words. A preview that was granted and then aged out, or one overtaken by
// another change, really has expired. A preview the panel refused to issue Ã¢â‚¬â€
// because the change was blocked Ã¢â‚¬â€ never entered the cache at all, and telling
// its operator the preview "expired" sent them hunting for a timing problem
// while the actual reason sat unread in the blocker list. Name the blockers.
//
// writeUnknownDNSEnginePreviewConflict, panelin elinde belirteci bulunmayan bir
// commit'i yanÃ„Â±tlar. Bunun iki sebebi vardÃ„Â±r ve farklÃ„Â± sÃƒÂ¶zler gerektirirler.
// VerilmiÃ…Å¸ sonra zaman aÃ…Å¸Ã„Â±mÃ„Â±na uÃ„Å¸ramÃ„Â±Ã…Å¸ ya da baÃ…Å¸ka bir deÃ„Å¸iÃ…Å¸iklikle geÃƒÂ§ilmiÃ…Å¸
// bir ÃƒÂ¶nizlemenin sÃƒÂ¼resi gerÃƒÂ§ekten dolmuÃ…Å¸tur. Panelin Ã¢â‚¬â€ deÃ„Å¸iÃ…Å¸iklik engellendiÃ„Å¸i
// iÃƒÂ§in Ã¢â‚¬â€ vermeyi reddettiÃ„Å¸i ÃƒÂ¶nizleme ise ÃƒÂ¶nbelleÃ„Å¸e hiÃƒÂ§ girmemiÃ…Å¸tir; operatÃƒÂ¶rÃƒÂ¼ne
// ÃƒÂ¶nizlemenin "sÃƒÂ¼resi doldu" demek, gerÃƒÂ§ek sebep engelleyici listesinde
// okunmadan dururken onu bir zamanlama sorununun peÃ…Å¸ine gÃƒÂ¶nderiyordu.
// Engelleyicileri adÃ„Â±yla sÃƒÂ¶yle.
func (p *Panel) writeUnknownDNSEnginePreviewConflict(
	w http.ResponseWriter,
	r *http.Request,
	request dnsEngineSwitchRequest,
) {
	snapshot, err := p.dnsEngineSnapshot(r.Context())
	if err == nil {
		blockers := dnsEnginePreviewBlockers(
			snapshot, request.TargetEngine,
			request.ExpectedSource.engine(), request.ExpectedRevision,
		)
		if len(blockers) != 0 {
			codes := make([]string, 0, len(blockers))
			for _, blocker := range blockers {
				codes = append(codes, blocker.Code)
			}
			writeClientError(w, http.StatusConflict,
				"the preview was blocked: "+strings.Join(codes, ", "))
			return
		}
	}
	writeClientError(w, http.StatusConflict,
		"DNS engine preview expired or no longer matches this request")
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
	// AÃƒÂ§Ã„Â±lÃ„Â±Ã…Å¸ yarÃ„Â±m kalmÃ„Â±Ã…Å¸ servis iÃ…Å¸lemlerini uzlaÃ…Å¸tÃ„Â±ramadÃ„Â±ysa, bu geÃƒÂ§iÃ…Å¸in
	// ÃƒÂ¼zerine kuracaÃ„Å¸Ã„Â± kalÃ„Â±cÃ„Â± durum bilinmiyor. Ãƒâ€¡ÃƒÂ¶zÃƒÂ¼lmemiÃ…Å¸ bir iÃ…Å¸lemin ÃƒÂ¼stÃƒÂ¼ne
	// ikincisini baÃ…Å¸latmak yerine saklanan sebeple reddet.
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
		p.writeUnknownDNSEnginePreviewConflict(w, r, request)
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
		action != dnsEngineActionReinstall && !request.DowntimeAcknowledged {
		writeClientError(w, http.StatusBadRequest,
			"downtime acknowledgement is required")
		return
	}
	if action == dnsEngineActionAdoptUnmanaged &&
		!request.AdoptionAcknowledged {
		writeClientError(w, http.StatusBadRequest,
			"adoption acknowledgement is required")
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
	if action == dnsEngineActionReinstall {
		p.commitDNSEngineReinstall(w, actor, request, manifest)
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
