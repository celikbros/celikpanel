package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

var errServerSetupDNSReconciliationRequired = errors.New("the exact DNS setup operation must finish reconciliation")

// setupDNSIdentity validates the reviewed topology without rewriting the OS,
// panel address, mail identity or a saved authoritative ownership epoch.
func setupDNSIdentity(draft serverSetupDraft) (dnsSetupRequest, string, error) {
	request := dnsSetupRequest{NS1: canonicalDNSName(draft.NS1), NS2: canonicalDNSName(draft.NS2), Role: transport.DNSTopologyPaired,
		PeerIP: strings.TrimSpace(draft.PeerIP), PeerNS: canonicalDNSName(draft.PeerNS)}
	if draft.DNSMode != setupDNSModeLocal || !transport.ValidDNSEngine(transport.DNSEngine(draft.DNSEngine)) {
		return request, "", errors.New("local DNS setup needs a supported authoritative engine")
	}
	if !validDNSHostname(request.NS1) || !validDNSHostname(request.NS2) || request.NS1 == request.NS2 {
		return request, "", errors.New("two different full nameserver hostnames are required")
	}
	local, valid := canonicalIPv4(strings.TrimSpace(draft.LocalIP))
	if !valid {
		return request, "", errors.New("local DNS IPv4 address is invalid")
	}
	peer, valid := canonicalIPv4(request.PeerIP)
	if !valid || peer == local {
		return request, "", errors.New("DNS redundancy requires a different server IPv4 address")
	}
	request.PeerIP = peer
	switch draft.DNSRole {
	case transport.DNSPairRolePrimary:
		if request.PeerNS != request.NS2 {
			return request, "", errors.New("a primary DNS server requires the secondary nameserver as its peer")
		}
	case transport.DNSPairRoleSecondary:
		if request.PeerNS != request.NS1 {
			return request, "", errors.New("a secondary DNS server requires the primary nameserver as its peer")
		}
	default:
		return request, "", errors.New("DNS role must be primary or secondary")
	}
	return request, local, nil
}

func setupDNSDraftMatchesState(draft serverSetupDraft, request dnsSetupRequest, local string, state dnsEngineDBState) bool {
	localNS := request.NS1
	if draft.DNSRole == transport.DNSPairRoleSecondary {
		localNS = request.NS2
	}
	return state.ActiveEngine == transport.DNSEngine(draft.DNSEngine) && state.CurrentSwitchID == "" &&
		state.Topology == transport.DNSTopologyPaired && state.PairRole == draft.DNSRole &&
		state.LocalIP == local && state.LocalNS == localNS && state.PeerIP == request.PeerIP && state.PeerNS == request.PeerNS
}

// startServerSetupDNS executes only a first authoritative installation, using
// the same immutable manifest, mutation lease, engine receipts and recovery as
// the DNS infrastructure workflow. The parent persisted requestID before entry.
// A retry reconciles that exact child and cannot create a replacement operation.
// Callers must not hold the service/topology/publication locks.
func (p *Panel) startServerSetupDNS(ctx context.Context, draft serverSetupDraft, requestID string, actor serviceOperationActor) error {
	request, local, err := setupDNSIdentity(draft)
	if err != nil {
		return err
	}
	if !validServiceOperationID(requestID) {
		return errors.New("DNS setup request identity is invalid")
	}
	actualIP, valid := canonicalIPv4(serverPrimaryIP())
	if !valid || actualIP != local {
		return errors.New("reviewed DNS address no longer matches this server")
	}
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	p.dnsTopologyMu.Lock()
	defer p.dnsTopologyMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()

	localNS := request.NS1
	if draft.DNSRole == transport.DNSPairRoleSecondary {
		localNS = request.NS2
	}
	persisted, readErr := readDNSEngineSwitchByRequest(ctx, p.db.GetDB(), requestID)
	if readErr == nil {
		if persisted.SourceEngine != "" || persisted.TargetEngine != transport.DNSEngine(draft.DNSEngine) ||
			persisted.Topology != transport.DNSTopologyPaired || persisted.PairRole != draft.DNSRole ||
			persisted.LocalIP != local || persisted.LocalNS != localNS || persisted.PeerIP != request.PeerIP || persisted.PeerNS != request.PeerNS {
			return errors.New("saved DNS operation does not match the reviewed setup")
		}
		persisted.Action = "install"
		if persisted.Phase == "rolled_back" {
			return errors.New("the DNS setup operation was rolled back; review a new plan")
		}
		if persisted.Phase != "committed" {
			if err := attachDNSEngineOperationAction(ctx, p.db.GetDB(), &persisted); err != nil {
				return setupDNSReconciliationError(err)
			}
			job, err := p.statusAgentMutation(ctx, requestID)
			if err != nil {
				return setupDNSReconciliationError(err)
			}
			if job == nil && persisted.Phase == "planned" {
				manifest, err := p.reconstructPersistedDNSEngineManifest(ctx, persisted)
				if err != nil {
					return setupDNSReconciliationError(err)
				}
				if err := p.executeDNSEngineSwitch(ctx, persisted, manifest); err != nil {
					return p.reconcileServerSetupDNSFailure(ctx, persisted, err)
				}
			} else {
				if _, _, err := p.reconcileDNSEngineSwitchLocked(ctx); err != nil {
					return fmt.Errorf("%w: %v", errServerSetupDNSReconciliationRequired, err)
				}
			}
			persisted, err = readDNSEngineSwitchByRequest(ctx, p.db.GetDB(), requestID)
			if err != nil {
				return setupDNSReconciliationError(err)
			}
			if persisted.Phase == "rolled_back" {
				return errors.New("the DNS setup operation was rolled back; review a new plan")
			}
			if persisted.Phase != "committed" {
				return fmt.Errorf("%w: DNS operation remains %s", errServerSetupDNSReconciliationRequired, persisted.Phase)
			}
			persisted.Action = "install"
		}
		marker, err := readDNSEngineOperationMarker(ctx, p.db.GetDB())
		if err != nil {
			return setupDNSReconciliationError(err)
		}
		if marker != nil {
			if !exactDNSEnginePostCommitMarker(marker, persisted) {
				return errors.New("DNS post-commit identity does not match setup")
			}
			result := p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
			if result.failed() {
				return fmt.Errorf("%w: %v", errServerSetupDNSReconciliationRequired, &dnsEngineReconcilePostCommitError{Result: result})
			}
		}
		state, err := readDNSEngineDBState(ctx, p.db.GetDB())
		if err != nil {
			return setupDNSReconciliationError(err)
		}
		if !setupDNSDraftMatchesState(draft, request, local, state) {
			return errors.New("DNS setup final identity does not match its reviewed plan")
		}
		return setupDNSReconciliationError(p.saveSetupDNSManagementMode(ctx, setupDNSModeLocal))
	}
	if !errors.Is(readErr, sql.ErrNoRows) {
		return setupDNSReconciliationError(readErr)
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return err
	}
	if state.ActiveEngine != "" {
		if !setupDNSDraftMatchesState(draft, request, local, state) {
			return errors.New("existing DNS ownership must be managed in DNS infrastructure; setup will not replace it")
		}
		return setupDNSReconciliationError(p.saveSetupDNSManagementMode(ctx, setupDNSModeLocal))
	}
	if !exactUnresolvedDNSEngineState(state) {
		return errors.New("another DNS operation must finish before setup")
	}
	if err := p.requireNoPendingDNSClusterSaga(ctx); err != nil {
		return err
	}
	if err := p.requireDNSEngineSwitchV1Agent(ctx); err != nil {
		return err
	}
	runtimes, conflict, hold, err := p.readDNSBackendRuntime(ctx)
	if err != nil {
		return err
	}
	zones, err := dnsIdentityStagingZoneCount(ctx, p.db.GetDB())
	if err != nil {
		return err
	}
	if hold != "" || zones != 0 || dnsIdentityStagingKind(runtimes, conflict, request.Role, draft.DNSRole, zones) != dnsIdentityStagingFresh {
		return errors.New("setup cannot adopt unmanaged DNS or replace existing zones")
	}
	for _, runtime := range runtimes {
		if runtime.Installed || runtime.Running {
			return errors.New("setup cannot replace an existing DNS installation")
		}
	}
	if _, err := p.stageDNSClusterSettingsAndReconcile(ctx, state, dnsIdentityStagingFresh, request.Role, request.PeerIP, request.PeerNS, request.NS1, request.NS2, local); err != nil {
		return err
	}
	snapshot, err := p.dnsEngineSnapshot(ctx)
	if err != nil {
		return err
	}
	target := transport.DNSEngine(draft.DNSEngine)
	if blockers := dnsEnginePreviewBlockers(snapshot, target, "", snapshot.Revision); len(blockers) != 0 {
		return fmt.Errorf("DNS setup prerequisites changed: %s", blockers[0].Code)
	}
	action := dnsEngineAction(snapshot, target)
	if action != "install" {
		return errors.New("setup may only install a new DNS engine")
	}
	state, err = readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return err
	}
	manifest, err := p.buildDNSEngineManifest(ctx, state, target, action, snapshot.Topology)
	if err != nil {
		return err
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return err
	}
	switchID, err := newServiceOperationID()
	if err != nil {
		return err
	}
	persisted, err = p.persistDNSEngineSwitch(ctx, dnsEngineSwitchRequest{RequestID: requestID, TargetEngine: target, ExpectedRevision: state.Revision}, ownerID, switchID, action, manifest)
	if err != nil {
		return err
	}
	p.auditDNSEngineBounded(dnsEngineAuditActor{UserID: actor.UserID, IP: actor.IP, UserAgent: actor.UserAgent}, "setup.accepted", persisted)
	if err := p.executeDNSEngineSwitch(ctx, persisted, manifest); err != nil {
		p.auditDNSEngineBounded(dnsEngineAuditActor{UserID: actor.UserID, IP: actor.IP, UserAgent: actor.UserAgent}, "setup.reconcile_required", persisted)
		return p.reconcileServerSetupDNSFailure(ctx, persisted, err)
	}
	result := p.reconcileDNSEnginePostCommitLocked(ctx, persisted)
	if result.failed() {
		return fmt.Errorf("%w: %v", errServerSetupDNSReconciliationRequired, &dnsEngineReconcilePostCommitError{Result: result})
	}
	p.auditDNSEngineBounded(dnsEngineAuditActor{UserID: actor.UserID, IP: actor.IP, UserAgent: actor.UserAgent}, "setup.completed", persisted)
	return setupDNSReconciliationError(p.saveSetupDNSManagementMode(ctx, setupDNSModeLocal))
}

func (p *Panel) serverSetupDNSOperationStatus(ctx context.Context, requestID string) (string, error) {
	op, err := readDNSEngineSwitchByRequest(ctx, p.db.GetDB(), requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	status, err := dnsEngineOperationStatus(op.Phase)
	if err != nil {
		return "", err
	}
	if status == "succeeded" {
		marker, err := readDNSEngineOperationMarker(ctx, p.db.GetDB())
		if err != nil {
			return "", err
		}
		mode, err := p.setupDNSManagementMode(ctx)
		if err != nil {
			return "", err
		}
		if marker != nil || mode != setupDNSModeLocal {
			return "running", nil
		}
	}
	if status == "rolled_back" {
		status = "failed"
	}
	return status, nil
}

// A synchronous agent rejection is not sufficient to release an accepted DNS
// operation. Reuse the exact marker, terminal receipt and stable restored-host
// proof from normal DNS reconciliation before permitting another reviewed plan.
// The caller holds serviceMutationMu, dnsTopologyMu and dnsPublicationMu.
func (p *Panel) reconcileServerSetupDNSFailure(ctx context.Context, persisted persistedDNSEngineSwitch, cause error) error {
	pending := func(err error) error { return fmt.Errorf("%w: %v", errServerSetupDNSReconciliationRequired, err) }
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return pending(err)
	}
	if state.CurrentSwitchID != persisted.SwitchID {
		return pending(cause)
	}
	reconciled, _, err := p.reconcileDNSEngineSwitchLocked(ctx)
	if err != nil {
		return pending(err)
	}
	if reconciled.SwitchID != persisted.SwitchID || reconciled.RequestID != persisted.RequestID || reconciled.OwnerID != persisted.OwnerID || reconciled.Qualifier != persisted.Qualifier {
		return pending(errors.New("DNS reconciliation identity differs"))
	}
	final, err := readDNSEngineSwitchByRequest(ctx, p.db.GetDB(), persisted.RequestID)
	if err != nil {
		return pending(err)
	}
	if final.Phase == "rolled_back" {
		return cause
	}
	// A committed target still resumes its exact post-commit/mode-save path;
	// neither an applied receipt nor missing evidence is reported as failure.
	return pending(cause)
}

func setupDNSReconciliationError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", errServerSetupDNSReconciliationRequired, err)
}
