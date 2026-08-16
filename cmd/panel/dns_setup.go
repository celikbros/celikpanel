package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsClusterSagaSetting      = "dns_cluster_saga_v1"
	dnsClusterSagaVersion      = 1
	dnsClusterSagaDesired      = "desired"
	dnsClusterSagaCompensating = "compensating"

	dnsClusterCommitPhasePrefix    = "commit/dns-cluster-config/v1/"
	dnsClusterCommitPublishedState = "published"
)

type dnsClusterTopology struct {
	Role      string `json:"role"`
	PeerIP    string `json:"peer_ip"`
	PeerNS    string `json:"peer_ns"`
	NS1       string `json:"ns1"`
	NS2       string `json:"ns2"`
	LocalIPv4 string `json:"local_ipv4"`
	RawRole   string `json:"raw_role"`
	RawPeerIP string `json:"raw_peer_ip"`
	RawPeerNS string `json:"raw_peer_ns"`
	RawNS1    string `json:"raw_ns1"`
	RawNS2    string `json:"raw_ns2"`
}

type dnsClusterSaga struct {
	Version   int                `json:"version"`
	Phase     string             `json:"phase"`
	RequestID string             `json:"request_id"`
	OwnerID   string             `json:"owner_id"`
	Qualifier string             `json:"qualifier"`
	Desired   dnsClusterTopology `json:"desired"`
	Previous  dnsClusterTopology `json:"previous"`
}

type dnsClusterPreBeginError struct{ err error }

func (e *dnsClusterPreBeginError) Error() string { return e.err.Error() }
func (e *dnsClusterPreBeginError) Unwrap() error { return e.err }

// requireNoPendingDNSClusterSaga is a read-only fail-closed gate. Ordinary
// zone publication and DNSSEC cannot run against an ambiguous host topology.
func (p *Panel) requireNoPendingDNSClusterSaga(ctx context.Context) error {
	var raw string
	err := p.db.GetDB().QueryRowContext(
		ctx, `SELECT value FROM panel_settings WHERE key = ?`,
		dnsClusterSagaSetting,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && raw == "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending DNS cluster saga: %w", err)
	}
	if _, err := decodeDNSClusterSaga(raw); err != nil {
		return err
	}
	return errors.New("DNS cluster topology is pending recovery")
}

func validDNSClusterTopology(topology dnsClusterTopology, desired bool) bool {
	if topology.Role != "standalone" && topology.Role != "paired" {
		return false
	}
	if canonicalDNSName(topology.NS1) != topology.NS1 ||
		canonicalDNSName(topology.NS2) != topology.NS2 ||
		!validDNSHostname(topology.NS1) ||
		!validDNSHostname(topology.NS2) || topology.NS1 == topology.NS2 {
		return false
	}
	localIPv4, ok := canonicalIPv4(topology.LocalIPv4)
	if !ok || localIPv4 != topology.LocalIPv4 {
		return false
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		topology.Role, topology.PeerIP, topology.PeerNS,
	)
	if err != nil {
		return false
	}
	if desired {
		return topology.RawRole == commitment.Role &&
			topology.RawPeerIP == commitment.PeerIP &&
			topology.RawPeerNS == commitment.PeerNS &&
			topology.RawNS1 == topology.NS1 &&
			topology.RawNS2 == topology.NS2
	}
	return len(topology.RawRole) <= 32 &&
		len(topology.RawPeerIP) <= 64 && len(topology.RawPeerNS) <= 253 &&
		len(topology.RawNS1) <= 253 && len(topology.RawNS2) <= 253
}

func validateDNSClusterSaga(saga dnsClusterSaga) error {
	if saga.Version != dnsClusterSagaVersion ||
		(saga.Phase != dnsClusterSagaDesired &&
			saga.Phase != dnsClusterSagaCompensating) ||
		!validServiceOperationID(saga.RequestID) ||
		!validServiceOperationID(saga.OwnerID) ||
		!mutationpayload.ValidDNSClusterConfigQualifier(saga.Qualifier) ||
		!validDNSClusterTopology(saga.Desired, true) ||
		!validDNSClusterTopology(saga.Previous, false) {
		return errors.New("DNS cluster saga is invalid")
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		saga.Desired.Role, saga.Desired.PeerIP, saga.Desired.PeerNS,
	)
	if err != nil || commitment.Qualifier != saga.Qualifier {
		return errors.New("DNS cluster saga qualifier does not bind its desired topology")
	}
	return nil
}

func encodeDNSClusterSaga(saga dnsClusterSaga) (string, error) {
	if err := validateDNSClusterSaga(saga); err != nil {
		return "", err
	}
	raw, err := json.Marshal(saga)
	return string(raw), err
}

func decodeDNSClusterSaga(raw string) (*dnsClusterSaga, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 16<<10 {
		return nil, errors.New("DNS cluster saga exceeds the size limit")
	}
	var saga dnsClusterSaga
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saga); err != nil {
		return nil, fmt.Errorf("decode DNS cluster saga: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("DNS cluster saga contains trailing data")
	}
	canonical, err := encodeDNSClusterSaga(saga)
	if err != nil || !bytes.Equal([]byte(raw), []byte(canonical)) {
		return nil, errors.New("DNS cluster saga is not canonical")
	}
	return &saga, nil
}

func readDNSClusterTopology(ctx context.Context, p *Panel) (dnsClusterTopology, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return dnsClusterTopology{}, err
	}
	defer tx.Rollback()
	values := make(map[string]string, 5)
	for _, key := range []string{
		settingDNSRole, settingDNSPeerIP, settingDNSPeerNS, settingNS1, settingNS2,
	} {
		values[key], err = settingTx(ctx, tx, key)
		if err != nil {
			return dnsClusterTopology{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return dnsClusterTopology{}, err
	}
	rawRole := values[settingDNSRole]
	role := normalizeDNSRole(strings.TrimSpace(rawRole))
	if strings.TrimSpace(rawRole) == "" {
		role = "standalone"
	}
	if role == "" {
		return dnsClusterTopology{}, fmt.Errorf("stored DNS role %q is invalid", rawRole)
	}
	peerIP := strings.TrimSpace(values[settingDNSPeerIP])
	peerNS := canonicalDNSName(values[settingDNSPeerNS])
	if role == "standalone" {
		peerIP, peerNS = "", ""
	}
	ns1, ns2 := canonicalDNSName(values[settingNS1]), canonicalDNSName(values[settingNS2])
	if ns1 == "" || ns2 == "" {
		ns1, ns2 = p.serverNameservers(ctx)
		ns1, ns2 = canonicalDNSName(ns1), canonicalDNSName(ns2)
	}
	localIPv4, ok := canonicalIPv4(serverPrimaryIP())
	if !ok {
		return dnsClusterTopology{}, errors.New("stored DNS topology has no canonical local IPv4 address")
	}
	return dnsClusterTopology{
		Role: role, PeerIP: peerIP, PeerNS: peerNS, NS1: ns1, NS2: ns2,
		LocalIPv4: localIPv4,
		RawRole:   rawRole, RawPeerIP: values[settingDNSPeerIP],
		RawPeerNS: values[settingDNSPeerNS], RawNS1: values[settingNS1],
		RawNS2: values[settingNS2],
	}, nil
}

func persistDNSClusterDesired(
	ctx context.Context,
	p *Panel,
	saga dnsClusterSaga,
) error {
	raw, err := encodeDNSClusterSaga(saga)
	if err != nil {
		return err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := settingTx(ctx, tx, dnsClusterSagaSetting)
	if err != nil {
		return err
	}
	if existing != "" {
		return errors.New("another DNS cluster topology is pending recovery")
	}
	// Persist only recovery authority before Begin. The active nameserver and
	// cluster settings remain the previous topology until the exact host
	// receipt succeeds; otherwise concurrent zone/template writes could derive
	// records from a topology that has not reached the host.
	if err := setSettingTx(ctx, tx, dnsClusterSagaSetting, raw); err != nil {
		return err
	}
	return tx.Commit()
}

func compensateDNSClusterPreBegin(
	ctx context.Context,
	p *Panel,
	saga dnsClusterSaga,
) error {
	saga.Phase = dnsClusterSagaCompensating
	raw, err := encodeDNSClusterSaga(saga)
	if err != nil {
		return err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := settingTx(ctx, tx, dnsClusterSagaSetting)
	if err != nil {
		return err
	}
	desiredRaw, err := encodeDNSClusterSaga(dnsClusterSaga{
		Version: saga.Version, Phase: dnsClusterSagaDesired,
		RequestID: saga.RequestID, OwnerID: saga.OwnerID,
		Qualifier: saga.Qualifier, Desired: saga.Desired, Previous: saga.Previous,
	})
	if err != nil || current != desiredRaw {
		return errors.New("DNS cluster compensation lost its exact desired CAS")
	}
	if err := setSettingTx(ctx, tx, dnsClusterSagaSetting, raw); err != nil {
		return err
	}
	if err := setSettingTx(ctx, tx, dnsClusterSagaSetting, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func clearDNSClusterSaga(ctx context.Context, p *Panel, saga dnsClusterSaga) error {
	raw, err := encodeDNSClusterSaga(saga)
	if err != nil {
		return err
	}
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE panel_settings
		SET value = '', updated_at = CURRENT_TIMESTAMP
		WHERE key = ? AND value = ?`, dnsClusterSagaSetting, raw)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return errors.New("DNS cluster saga finalization lost its exact CAS")
	}
	return nil
}

func (p *Panel) applyDNSClusterSagaV2(
	ctx context.Context,
	saga dnsClusterSaga,
) (dnsClusterAgentResponse, error) {
	var response dnsClusterAgentResponse
	hostRPCEntered := false
	request := transport.ConfigureDNSClusterV2Request{
		Role: saga.Desired.Role, PeerIP: saga.Desired.PeerIP,
		PeerNS: saga.Desired.PeerNS,
	}
	op := serviceOperation{
		RequestID: saga.RequestID, Kind: "dns_cluster_configure",
		ServiceID: "pdns", PackageName: saga.Qualifier,
	}
	err := p.withStandaloneAgentMutationIdentity(
		ctx, op, saga.OwnerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			hostRPCEntered = true
			request.ServiceMutationBinding = binding
			if err := p.callAgentContext(
				callCtx, "Agent.ConfigureDNSClusterV2", &request, &response,
			); err != nil {
				return err
			}
			if response.Error != "" {
				return errors.New(response.Error)
			}
			if !response.Applied {
				return errors.New("agent did not confirm DNS cluster convergence")
			}
			return nil
		},
	)
	// The mutating RPC response is not commit authority. Even on the happy
	// response path, only the exact durable succeeded receipt with the
	// payload-bound published phase permits the panel ledger to finalize.
	statusCtx, cancel := dnsZoneFinalizeContext(ctx)
	job, statusErr := p.statusAgentMutation(statusCtx, saga.RequestID)
	cancel()
	if statusErr != nil {
		return response, &agentMutationTerminalUncertainError{
			kind: "dns_cluster_configure",
			err: errors.Join(err,
				fmt.Errorf("read exact DNS cluster terminal status: %w", statusErr)),
		}
	}
	identity := agentMutationIdentityForOperation(op, saga.OwnerID)
	if job == nil {
		if err != nil && !hostRPCEntered {
			return response, &dnsClusterPreBeginError{err: err}
		}
		return response, &agentMutationTerminalUncertainError{
			kind: "dns_cluster_configure",
			err: errors.Join(err,
				errors.New("exact DNS cluster mutation has no durable terminal receipt")),
		}
	}
	if identityErr := validateAgentMutationIdentity(job, identity); identityErr != nil {
		return response, identityErr
	}
	if agentMutationActive(job.Status) {
		return response, &agentMutationTerminalUncertainError{
			kind: "dns_cluster_configure",
			err: errors.Join(err,
				errors.New("exact DNS cluster mutation remains active")),
		}
	}
	switch job.Status {
	case agentMutationSucceeded:
		if job.Phase != dnsClusterPublishedPhase(saga) {
			return response, errors.New(
				"exact succeeded DNS cluster child lacks its canonical published receipt",
			)
		}
		return response, nil
	case agentMutationFailed:
		if err != nil {
			return response, err
		}
		return response, errors.New("exact DNS cluster mutation failed")
	default:
		return response, fmt.Errorf(
			"exact DNS cluster mutation has invalid terminal status %q",
			job.Status,
		)
	}
}

func readPendingDNSClusterSaga(ctx context.Context, p *Panel) (*dnsClusterSaga, error) {
	var raw string
	err := p.db.GetDB().QueryRowContext(
		ctx, `SELECT value FROM panel_settings WHERE key = ?`,
		dnsClusterSagaSetting,
	).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && raw == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeDNSClusterSaga(raw)
}

func validDirectDNSClusterConfigure(job *agentMutationJob) bool {
	return job != nil && agentMutationActive(job.Status) &&
		validServiceOperationID(job.RequestID) &&
		validServiceOperationID(job.OwnerID) &&
		job.Kind == "dns_cluster_configure" && job.Target == "pdns" &&
		mutationpayload.ValidDNSClusterConfigQualifier(job.PackageName)
}

func dnsClusterJobMatchesSaga(job *agentMutationJob, saga *dnsClusterSaga) bool {
	return validDirectDNSClusterConfigure(job) && saga != nil &&
		job.RequestID == saga.RequestID && job.OwnerID == saga.OwnerID &&
		job.PackageName == saga.Qualifier
}

func dnsClusterPublishedPhase(saga dnsClusterSaga) string {
	return dnsClusterCommitPhasePrefix + dnsClusterCommitPublishedState + "/" +
		saga.RequestID + "/" + saga.Qualifier
}

// finalizeDNSClusterSagaStartup records only an exact terminal-successful
// agent receipt. recoverInterruptedServiceOperations already holds
// serviceMutationMu, so acquiring topology then publication preserves the
// global lock order. The ledger rewrite advances durable per-zone generations;
// startup deliberately performs no host call and no zone publication here.
func (p *Panel) finalizeDNSClusterSagaStartup(
	ctx context.Context,
	saga dnsClusterSaga,
) error {
	p.dnsTopologyMu.Lock()
	defer p.dnsTopologyMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()

	desired := saga.Desired
	if err := p.saveDNSClusterSettingsAndReconcile(
		ctx,
		desired.Role,
		desired.PeerIP,
		desired.PeerNS,
		desired.NS1,
		desired.NS2,
		desired.LocalIPv4,
	); err != nil {
		return fmt.Errorf("reconcile succeeded DNS cluster topology: %w", err)
	}
	if err := clearDNSClusterSaga(ctx, p, saga); err != nil {
		return fmt.Errorf("clear succeeded DNS cluster saga: %w", err)
	}
	return nil
}

// observeDNSClusterSagaStartup never re-applies or cancels host state. Its
// boolean result reports whether a cluster saga remains pending, allowing
// startup to continue unrelated firewall/mail/VPN recovery while suppressing
// topology-dependent DNS publication.
func (p *Panel) observeDNSClusterSagaStartup(
	ctx context.Context,
	globalJob *agentMutationJob,
) (bool, error) {
	saga, err := readPendingDNSClusterSaga(ctx, p)
	if err != nil {
		return false, fmt.Errorf("read pending DNS cluster saga: %w", err)
	}
	if saga == nil {
		if validDirectDNSClusterConfigure(globalJob) {
			return false, errors.New("active DNS cluster child has no persisted desired saga")
		}
		return false, nil
	}
	if globalJob != nil && agentMutationActive(globalJob.Status) &&
		globalJob.Kind != "dns_cluster_configure" {
		return true, nil
	}
	var observed *agentMutationJob
	if globalJob != nil && agentMutationActive(globalJob.Status) {
		if !dnsClusterJobMatchesSaga(globalJob, saga) {
			return true, errors.New("active DNS cluster mutation does not match the pending DNS cluster saga")
		}
		observed, err = p.waitExpectedAgentMutationTerminal(
			ctx, agentMutationJobIdentity(globalJob),
		)
	} else {
		observed, err = p.statusAgentMutation(ctx, saga.RequestID)
	}
	if err != nil {
		return true, fmt.Errorf("observe exact DNS cluster child: %w", err)
	}
	if observed == nil {
		log.Printf(
			"DNS cluster saga %s has no exact agent receipt; retaining it for operator recovery",
			saga.RequestID,
		)
		return true, nil
	}
	identity := agentMutationIdentity{
		RequestID: saga.RequestID, OwnerID: saga.OwnerID,
		Kind: "dns_cluster_configure", Target: "pdns",
		PackageName: saga.Qualifier,
	}
	if err := validateAgentMutationIdentity(observed, identity); err != nil {
		return true, err
	}
	if agentMutationActive(observed.Status) {
		return true, errors.New("DNS cluster child remained active after startup observation")
	}
	if observed.Status == agentMutationSucceeded {
		if observed.Phase != dnsClusterPublishedPhase(*saga) {
			return true, errors.New(
				"exact succeeded DNS cluster child lacks its canonical published receipt",
			)
		}
		if err := p.finalizeDNSClusterSagaStartup(ctx, *saga); err != nil {
			return true, err
		}
		return false, nil
	}
	if observed.Status != agentMutationFailed {
		return true, fmt.Errorf(
			"exact DNS cluster child has invalid terminal status %q",
			observed.Status,
		)
	}
	phase := strings.TrimSpace(observed.Phase)
	if phase == "" {
		log.Printf(
			"DNS cluster saga %s has a terminal failure without a pre-commit receipt; retaining it for operator recovery",
			saga.RequestID,
		)
		return true, nil
	}
	if strings.HasPrefix(phase, dnsClusterCommitPhasePrefix) {
		return true, errors.New(
			"failed DNS cluster child carries a contradictory durable commit receipt",
		)
	}
	if err := compensateDNSClusterPreBegin(ctx, p, *saga); err != nil {
		return true, fmt.Errorf(
			"compensate proven pre-commit DNS cluster failure: %w",
			err,
		)
	}
	return false, nil
}

// dnsSetupRequest is the complete operator-owned DNS identity. Keeping the
// nameserver pair and the cluster tuple in one request prevents a paired
// nameserver rename from being validated against stale stored peer settings.
type dnsSetupRequest struct {
	NS1    string `json:"ns1"`
	NS2    string `json:"ns2"`
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

// writeDNSSetupRequired keeps the legacy read endpoints available while
// refusing their old partial-write contracts. DNS identity is one topology:
// the pair, role and peer assignment must be validated and committed together.
func writeDNSSetupRequired(w http.ResponseWriter) {
	writeCodedError(
		w,
		http.StatusConflict,
		errCodeDNSSetupRequired,
		"legacy DNS settings writes are disabled; submit the complete nameserver pair and operating mode to /api/v1/settings/dns-setup",
		"/settings?section=dns",
	)
}

func writeDNSTopologyUnsupported(w http.ResponseWriter) {
	writeCodedError(
		w,
		http.StatusConflict,
		errCodeDNSTopologyUnsupported,
		"paired DNS topology is not supported by the active DNS engine",
		"/settings?section=dns",
	)
}

// handleBINDDNSSetupLocked commits only panel-owned standalone identity and
// publishes the rewritten full snapshots through engine+epoch-bound V3. BIND
// has no PowerDNS cluster configuration RPC and must never enter that saga.
// The caller owns serviceMutationMu, dnsTopologyMu and dnsPublicationMu and
// has re-proven the exact active BIND publisher.
func (p *Panel) handleBINDDNSSetupLocked(
	w http.ResponseWriter,
	r *http.Request,
	req dnsSetupRequest,
	localIPv4 string,
) {
	if req.Role != "standalone" {
		writeDNSTopologyUnsupported(w)
		return
	}
	if err := p.requireNoPendingDNSClusterSaga(r.Context()); err != nil {
		writeClientError(
			w, http.StatusConflict,
			"a previous DNS topology operation must finish first",
		)
		return
	}
	if err := p.saveDNSClusterSettingsAndReconcile(
		r.Context(), "standalone", "", "", req.NS1, req.NS2, localIPv4,
	); err != nil {
		writeServerError(w, fmt.Errorf("reconcile standalone BIND identity: %w", err))
		return
	}
	p.audit(r, "settings.dns_setup_saved:standalone engine=bind", "settings", 0)
	if _, err := p.syncAllZonesLocked(r.Context()); err != nil {
		err = fmt.Errorf("publish standalone BIND identity: %w", err)
		if writeDNSPublicationConflict(
			w, err,
			"DNS setup was saved, but one or more zones could not be published; retry the same setup",
		) {
			return
		}
		writeServerError(w, err)
		return
	}
	p.audit(r, "settings.dns_setup_published:standalone engine=bind", "settings", 0)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleDNSSetup applies the complete DNS identity as a forward-only durable
// saga. The desired tuple and exact V2 child identity commit before Begin; an
// active or unqueryable child therefore retains recovery authority rather than
// rolling the database behind a host operation that may still converge.
func (p *Panel) handleDNSSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	if r.Method != http.MethodPut {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req dnsSetupRequest
	if err := decodeStrictJSON(w, r, &req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.NS1 = canonicalDNSName(req.NS1)
	req.NS2 = canonicalDNSName(req.NS2)
	for _, ns := range []string{req.NS1, req.NS2} {
		if !validDNSHostname(ns) {
			writeClientError(w, http.StatusBadRequest, "a nameserver must be a full host name, for example ns1.example.com")
			return
		}
	}
	if req.NS1 == req.NS2 {
		writeClientError(w, http.StatusBadRequest, "the two nameservers must have different names")
		return
	}

	req.Role = normalizeDNSRole(strings.TrimSpace(req.Role))
	req.PeerIP = strings.TrimSpace(req.PeerIP)
	req.PeerNS = canonicalDNSName(req.PeerNS)
	localIPv4, ok := canonicalIPv4(serverPrimaryIP())
	if !ok {
		writeClientError(w, http.StatusConflict, "this server has no usable IPv4 address; set CELIKPANEL_SERVER_IP and retry")
		return
	}

	switch req.Role {
	case "standalone":
		req.PeerIP, req.PeerNS = "", ""
	case "paired":
		peerIP := net.ParseIP(req.PeerIP)
		peerIPv4 := peerIP.To4()
		if peerIPv4 == nil || !peerIPv4.IsGlobalUnicast() {
			writeClientError(w, http.StatusBadRequest, "enter the other server's IPv4 address")
			return
		}
		req.PeerIP = peerIPv4.String()
		if !validDNSHostname(req.PeerNS) {
			writeClientError(w, http.StatusBadRequest, "enter the other server's nameserver name, for example ns2.example.com")
			return
		}
		if req.PeerIP == localIPv4 {
			writeCodedError(w, http.StatusBadRequest, errCodeDNSClusterPeerIsLocal, "the other server cannot be this server", "")
			return
		}
		if req.PeerNS != req.NS1 && req.PeerNS != req.NS2 {
			writeClientError(w, http.StatusBadRequest, "the other server's name must be one of the two nameservers in this setup")
			return
		}
	default:
		writeClientError(w, http.StatusBadRequest, "role must be standalone or paired")
		return
	}
	publisher, publisherReady, err := p.activeDNSPublisher(r.Context())
	if err != nil {
		writeServerError(w, fmt.Errorf("verify active DNS setup publisher: %w", err))
		return
	}
	if !publisherReady || publisher.Epoch < 1 {
		writeDNSEngineWorkflowRequired(w)
		return
	}
	switch publisher.Engine {
	case transport.DNSEngineBIND:
		if req.Role != "standalone" {
			writeDNSTopologyUnsupported(w)
			return
		}
	case transport.DNSEnginePowerDNS:
		// Capability, platform policy and configured PowerDNS are all
		// preconditions. Mixed binaries and an unready host touch neither DB
		// nor the privileged mutation ledger.
		if err := p.requireDNSClusterConfigureV2Agent(r.Context()); err != nil {
			writeServerError(w, fmt.Errorf("verify DNS setup V2 agent: %w", err))
			return
		}
	default:
		writeDNSEngineWorkflowRequired(w)
		return
	}
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	p.dnsTopologyMu.Lock()
	defer p.dnsTopologyMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()

	lockedPublisher, lockedReady, err := p.activeDNSPublisher(r.Context())
	if err != nil {
		writeServerError(w, fmt.Errorf("recheck active DNS setup publisher: %w", err))
		return
	}
	if !lockedReady || lockedPublisher != publisher {
		writeDNSEngineWorkflowRequired(w)
		return
	}
	if lockedPublisher.Engine == transport.DNSEngineBIND {
		p.handleBINDDNSSetupLocked(w, r, req, localIPv4)
		return
	}
	if lockedPublisher.Engine != transport.DNSEnginePowerDNS {
		writeDNSEngineWorkflowRequired(w)
		return
	}

	// Readiness is deliberately checked while holding the complete topology
	// lock chain. A request may have waited behind another host mutation after
	// its capability preflight; only this observation may authorize durable
	// desired state and the exact Begin that follows.
	var readiness dnsClusterReadinessResponse
	if err := p.callAgentContext(
		r.Context(), "Agent.DNSClusterReadiness", &transport.Empty{}, &readiness,
	); err != nil {
		writeServerError(w, fmt.Errorf("read DNS setup readiness: %w", err))
		return
	}
	if !readiness.Ready {
		writeClientError(w, http.StatusConflict,
			"PowerDNS is not ready for a DNS topology change; configure the DNS service first")
		return
	}

	desired := dnsClusterTopology{
		Role: req.Role, PeerIP: req.PeerIP, PeerNS: req.PeerNS,
		NS1: req.NS1, NS2: req.NS2, LocalIPv4: localIPv4,
		RawRole: req.Role, RawPeerIP: req.PeerIP,
		RawPeerNS: req.PeerNS, RawNS1: req.NS1, RawNS2: req.NS2,
	}
	pending, err := readPendingDNSClusterSaga(r.Context(), p)
	if err != nil {
		writeServerError(w, fmt.Errorf("read pending DNS topology: %w", err))
		return
	}
	var saga dnsClusterSaga
	if pending != nil {
		if pending.Phase != dnsClusterSagaDesired || pending.Desired != desired {
			writeClientError(w, http.StatusConflict,
				"another DNS topology is pending operator recovery")
			return
		}
		// A same-desired PUT may repair only the narrow crash-before-Begin
		// window. Query the exact durable request while holding the complete
		// lock chain. Any receipt (active or terminal), observation error, or
		// identity mismatch is evidence that the host may already have acted
		// and therefore blocks a second mutating RPC.
		observed, statusErr := p.statusAgentMutation(
			r.Context(), pending.RequestID,
		)
		if statusErr != nil {
			writeServerError(w, fmt.Errorf(
				"verify pending DNS topology receipt: %w", statusErr,
			))
			return
		}
		if observed != nil {
			identity := agentMutationIdentity{
				RequestID:   pending.RequestID,
				OwnerID:     pending.OwnerID,
				Kind:        "dns_cluster_configure",
				Target:      "pdns",
				PackageName: pending.Qualifier,
			}
			if identityErr := validateAgentMutationIdentity(
				observed, identity,
			); identityErr != nil {
				writeServerError(w, fmt.Errorf(
					"pending DNS topology receipt identity mismatch: %w",
					identityErr,
				))
				return
			}
			writeClientError(w, http.StatusConflict,
				"this DNS topology already has a durable agent receipt and remains pending recovery")
			return
		}
		saga = *pending
	} else {
		previous, err := readDNSClusterTopology(r.Context(), p)
		if err != nil {
			writeServerError(w, fmt.Errorf("read previous DNS cluster topology: %w", err))
			return
		}
		requestID, err := newServiceOperationID()
		if err != nil {
			writeServerError(w, fmt.Errorf("create DNS cluster request identity: %w", err))
			return
		}
		ownerID, err := newServiceOperationID()
		if err != nil {
			writeServerError(w, fmt.Errorf("create DNS cluster owner identity: %w", err))
			return
		}
		commitment, err := mutationpayload.CanonicalDNSClusterConfig(
			req.Role, req.PeerIP, req.PeerNS,
		)
		if err != nil {
			writeClientError(w, http.StatusBadRequest, "DNS cluster tuple is not canonical")
			return
		}
		saga = dnsClusterSaga{
			Version: dnsClusterSagaVersion, Phase: dnsClusterSagaDesired,
			RequestID: requestID, OwnerID: ownerID, Qualifier: commitment.Qualifier,
			Desired:  desired,
			Previous: previous,
		}
		if err := persistDNSClusterDesired(r.Context(), p, saga); err != nil {
			writeServerError(w, fmt.Errorf("persist desired DNS topology: %w", err))
			return
		}
	}
	_, applyErr := p.applyDNSClusterSagaV2(r.Context(), saga)
	if applyErr != nil {
		var preBegin *dnsClusterPreBeginError
		if errors.As(applyErr, &preBegin) {
			if compensateErr := compensateDNSClusterPreBegin(
				r.Context(), p, saga,
			); compensateErr != nil {
				writeServerError(w, errors.Join(applyErr,
					fmt.Errorf("compensate unstarted DNS topology: %w", compensateErr)))
				return
			}
			writeAgentError(w, applyErr, "DNS setup V2 did not begin")
			return
		}
		if mutationTerminalUncertain(applyErr) {
			writeClientError(w, http.StatusConflict,
				"DNS topology convergence is still being reconciled; retry after startup recovery completes")
			return
		}
		// A known terminal failure may have changed host state before failing.
		// Keep the exact desired saga for receipt-based startup reconciliation
		// or explicit operator repair.
		writeClientError(w, http.StatusConflict,
			"DNS topology was not fully confirmed and remains pending recovery")
		return
	}

	if err := p.saveDNSClusterSettingsAndReconcile(
		r.Context(), req.Role, req.PeerIP, req.PeerNS, req.NS1, req.NS2, localIPv4,
	); err != nil {
		writeServerError(w, fmt.Errorf("reconcile desired DNS topology ledger: %w", err))
		return
	}
	// The exact host child is terminal-successful and the rewritten zone
	// records have advanced their durable publication generations. Clear the
	// topology ambiguity before attempting remote publication: a publication
	// failure remains recoverable from dns_zone_sync_state and must not wedge
	// every DNS/PDNS operation behind a completed cluster saga.
	if err := clearDNSClusterSaga(r.Context(), p, saga); err != nil {
		writeServerError(w, fmt.Errorf("finalize DNS topology saga: %w", err))
		return
	}
	p.audit(r, "settings.dns_setup_saved:"+req.Role+" peer="+req.PeerIP, "settings", 0)
	if _, err := p.syncAllZonesLocked(r.Context()); err != nil {
		err = fmt.Errorf("publish DNS setup: %w", err)
		if writeDNSPublicationConflict(w, err,
			"DNS setup was saved, but one or more zones could not be published; retry the same setup") {
			return
		}
		writeServerError(w, err)
		return
	}

	p.audit(r, "settings.dns_setup_published:"+req.Role+" peer="+req.PeerIP, "settings", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}
