package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

var errServerSetupInfrastructureDNSWaiting = errors.New("the reviewed native DNS pair must be ready before preparing access records")
var errServerSetupInfrastructureDNSUnknown = errors.New("the exact infrastructure DNS publication is being reconciled")
var errServerSetupInfrastructureDNSChanged = errors.New("infrastructure DNS ownership or records changed; review the plan again")

// These are infrastructure records only. A setup plan never creates a tenant,
// website, mailbox, MX, SPF or application apex record as a side effect.
type serverSetupInfrastructureDNSRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Action  string `json:"action"`
}

type serverSetupInfrastructureDNSPlan struct {
	ZoneType       string                               `json:"zone_type"`
	Zone           string                               `json:"zone"`
	ExistingZoneID int64                                `json:"existing_zone_id,omitempty"`
	ExpectedDigest string                               `json:"expected_digest"`
	Records        []serverSetupInfrastructureDNSRecord `json:"records"`
}

type serverSetupInfrastructureDNSPlanError struct {
	Code    string
	Message string
}

func (e *serverSetupInfrastructureDNSPlanError) Error() string { return e.Message }
func infrastructureDNSReviewError(message string) error {
	return &serverSetupInfrastructureDNSPlanError{Code: "infrastructure_dns_conflict", Message: message}
}

type serverSetupInfrastructureDNSQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type serverSetupInfrastructureDNSOwner struct {
	Zone   string `json:"zone"`
	ZoneID int64  `json:"zone_id"`
	PlanID string `json:"plan_id"`
}

type serverSetupInfrastructureDNSReceipt struct {
	PlanID             string              `json:"plan_id"`
	StepID             string              `json:"step_id"`
	RequestID          string              `json:"request_id"`
	OwnerID            string              `json:"owner_id"`
	Zone               string              `json:"zone"`
	ZoneID             int64               `json:"zone_id"`
	Digest             string              `json:"digest"`
	Engine             transport.DNSEngine `json:"engine"`
	Epoch              int64               `json:"epoch"`
	PreparedGeneration int64               `json:"prepared_generation"`
	Failed             bool                `json:"failed,omitempty"`
}

type serverSetupInfrastructureDNSSnapshot struct {
	Zone      string
	ZoneID    int64
	ZoneType  string
	Master    string
	Ownership string
	Records   []serverSetupInfrastructureDNSStoredRecord
}

type serverSetupInfrastructureDNSStoredRecord struct {
	ID        int64
	Name      string
	Type      string
	Content   string
	TTL       int
	Priority  int
	Disabled  int
	Auth      int
	OrderName string
}

func serverSetupInfrastructureDNSOwnerKey(zone string) string {
	return "server_setup_infrastructure_zone:" + zone
}
func serverSetupInfrastructureDNSReceiptKey(requestID string) string {
	return "server_setup_infrastructure_dns:" + requestID
}
func serverSetupDNSNameWithin(name, zone string) bool {
	return name == zone || strings.HasSuffix(name, "."+zone)
}

func readServerSetupInfrastructureDNSSnapshot(ctx context.Context, q serverSetupInfrastructureDNSQuery, zone string) (serverSetupInfrastructureDNSSnapshot, error) {
	s := serverSetupInfrastructureDNSSnapshot{Zone: zone, Records: []serverSetupInfrastructureDNSStoredRecord{}}
	// Creating an ancestor/child authority is a separate delegation change. This
	// bounded bootstrap helper must not infer permission to shadow another zone.
	rows, err := q.QueryContext(ctx, `SELECT name FROM pdns_domains WHERE name<>? AND (? LIKE '%.'||name OR name LIKE '%.'||?) LIMIT 1`, zone, zone, zone)
	if err != nil {
		return s, err
	}
	overlap := rows.Next()
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return s, rowErr
	}
	if overlap {
		return s, infrastructureDNSReviewError("the infrastructure zone overlaps another managed DNS zone; review its delegation first")
	}
	rows, err = q.QueryContext(ctx, `SELECT id,subscription_id,name,dns_management FROM domains WHERE name=? OR ? LIKE '%.'||name LIMIT 129`, zone, zone)
	if err != nil {
		return s, err
	}
	count := 0
	for rows.Next() {
		var name, mode string
		var domainID, subscriptionID int64
		if err := rows.Scan(&domainID, &subscriptionID, &name, &mode); err != nil {
			rows.Close()
			return s, err
		}
		count++
		if mode != setupDNSModeLocal {
			rows.Close()
			return s, infrastructureDNSReviewError("the infrastructure zone belongs to external or remote DNS; preserve that ownership")
		}
		if name == zone {
			s.Ownership = fmt.Sprintf("local-domain:%d:%d", domainID, subscriptionID)
		}
	}
	rowErr = rows.Err()
	rows.Close()
	if rowErr != nil {
		return s, rowErr
	}
	if count > 128 {
		return s, infrastructureDNSReviewError("too many overlapping DNS ownership entries")
	}
	err = q.QueryRowContext(ctx, `SELECT id,type,COALESCE(master,'') FROM pdns_domains WHERE name=?`, zone).Scan(&s.ZoneID, &s.ZoneType, &s.Master)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if s.Master != "" || !slices.Contains([]string{"MASTER", "NATIVE"}, s.ZoneType) {
		return s, infrastructureDNSReviewError("the infrastructure zone is not a local primary zone")
	}
	if s.Ownership == "" {
		var raw string
		err = q.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key=?`, serverSetupInfrastructureDNSOwnerKey(zone)).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return s, err
		}
		var owner serverSetupInfrastructureDNSOwner
		if err != nil || json.Unmarshal([]byte(raw), &owner) != nil || owner.Zone != zone || owner.ZoneID != s.ZoneID || !validServiceOperationID(owner.PlanID) {
			return s, infrastructureDNSReviewError("the existing DNS zone has no verified local ownership; reconcile it before setup")
		}
		s.Ownership = raw
	}
	var signed int
	if err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM pdns_cryptokeys WHERE domain_id=?) + (SELECT COUNT(*) FROM pdns_domainmetadata WHERE domain_id=? AND kind IN ('PRESIGNED','NSEC3PARAM')) + (SELECT COUNT(*) FROM pdns_records WHERE domain_id=? AND type IN ('DNSKEY','RRSIG','NSEC','NSEC3','NSEC3PARAM'))`, s.ZoneID, s.ZoneID, s.ZoneID).Scan(&signed); err != nil {
		return s, err
	}
	if signed != 0 {
		return s, infrastructureDNSReviewError("existing infrastructure DNSSEC must be managed through its signing workflow; setup will preserve it")
	}
	rows, err = q.QueryContext(ctx, `SELECT id,COALESCE(name,''),COALESCE(type,''),COALESCE(content,''),COALESCE(ttl,3600),COALESCE(prio,0),COALESCE(disabled,0),COALESCE(auth,1),COALESCE(ordername,'') FROM pdns_records WHERE domain_id=? ORDER BY id LIMIT 2049`, s.ZoneID)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var r serverSetupInfrastructureDNSStoredRecord
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Content, &r.TTL, &r.Priority, &r.Disabled, &r.Auth, &r.OrderName); err != nil {
			return s, err
		}
		s.Records = append(s.Records, r)
	}
	if err := rows.Err(); err != nil {
		return s, err
	}
	if len(s.Records) > 2048 {
		return s, infrastructureDNSReviewError("infrastructure zone exceeds the bounded setup record limit")
	}
	return s, nil
}

func serverSetupInfrastructureDNSSnapshotDigest(s serverSetupInfrastructureDNSSnapshot, allowSerialAdvance bool) string {
	if allowSerialAdvance {
		s.Records = append([]serverSetupInfrastructureDNSStoredRecord(nil), s.Records...)
		for i := range s.Records {
			if s.Records[i].Type == "SOA" {
				fields := strings.Fields(s.Records[i].Content)
				if len(fields) == 7 {
					fields[2] = "0"
					s.Records[i].Content = strings.Join(fields, " ")
				}
			}
		}
	}
	raw, _ := json.Marshal(s)
	return serverSetupID(string(raw))
}

func serverSetupInfrastructureDNSDesired(d serverSetupDraft) ([]serverSetupInfrastructureDNSRecord, error) {
	if d.InfrastructureDNS == nil {
		return nil, nil
	}
	if d.InfrastructureDNS.Zone == "" {
		return nil, infrastructureDNSReviewError("choose the exact infrastructure DNS zone before reviewing setup")
	}
	if d.DNSMode != setupDNSModeLocal || d.DNSRole != transport.DNSPairRolePrimary {
		return nil, infrastructureDNSReviewError("only the reviewed local primary may prepare infrastructure DNS records")
	}
	zone, err := hostname.CanonicalFQDN(d.InfrastructureDNS.Zone)
	if err != nil || zone != d.InfrastructureDNS.Zone {
		return nil, infrastructureDNSReviewError("choose the exact fully qualified infrastructure DNS zone")
	}
	if _, _, err := setupDNSIdentity(d); err != nil {
		return nil, infrastructureDNSReviewError(err.Error())
	}
	if d.PanelDomain == "" || !serverSetupDNSNameWithin(d.PanelDomain, zone) {
		return nil, infrastructureDNSReviewError("the reviewed panel hostname must belong to the selected infrastructure zone")
	}
	records := []serverSetupInfrastructureDNSRecord{
		{Name: zone, Type: "SOA", Content: fmt.Sprintf("%s hostmaster.%s 1 3600 600 604800 300", d.NS1, zone), TTL: 3600, Action: "add"},
		{Name: zone, Type: "NS", Content: d.NS1, TTL: 3600, Action: "add"},
		{Name: zone, Type: "NS", Content: d.NS2, TTL: 3600, Action: "add"},
	}
	addAddress := func(name, ip string) error {
		canon, err := hostname.CanonicalFQDN(name)
		if err != nil || canon != name || !serverSetupDNSNameWithin(name, zone) {
			return infrastructureDNSReviewError("every prepared access hostname must belong to the reviewed infrastructure zone")
		}
		for _, r := range records {
			if r.Name == name && r.Type == "A" {
				if r.Content != ip {
					return infrastructureDNSReviewError("the same access hostname cannot point to both servers")
				}
				return nil
			}
		}
		records = append(records, serverSetupInfrastructureDNSRecord{Name: name, Type: "A", Content: ip, TTL: 300, Action: "add"})
		return nil
	}
	for _, ns := range []string{d.NS1, d.NS2} {
		if serverSetupDNSNameWithin(ns, zone) {
			ip := d.LocalIP
			if ns == d.PeerNS {
				ip = d.PeerIP
			}
			if err := addAddress(ns, ip); err != nil {
				return nil, err
			}
		}
	}
	if err := addAddress(d.PanelDomain, d.LocalIP); err != nil {
		return nil, err
	}
	if serverSetupHasComponent(d, "postfix") && d.MailHostname != "" {
		if err := addAddress(d.MailHostname, d.LocalIP); err != nil {
			return nil, err
		}
	}
	if d.InfrastructureDNS.PeerPanelDomain != "" {
		if err := addAddress(d.InfrastructureDNS.PeerPanelDomain, d.PeerIP); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func buildServerSetupInfrastructureDNSAgainstSnapshot(d serverSetupDraft, s serverSetupInfrastructureDNSSnapshot) (*serverSetupInfrastructureDNSPlan, error) {
	desired, err := serverSetupInfrastructureDNSDesired(d)
	if err != nil || desired == nil {
		return nil, err
	}
	plan := &serverSetupInfrastructureDNSPlan{Zone: s.Zone, ZoneType: "MASTER", ExistingZoneID: s.ZoneID, ExpectedDigest: serverSetupInfrastructureDNSSnapshotDigest(s, false), Records: desired}
	for i := range plan.Records {
		want := &plan.Records[i]
		seen := 0
		for _, got := range s.Records {
			name := canonicalDNSName(got.Name)
			// Neither an alias nor a delegated child may be overwritten by an A record.
			if got.Disabled == 0 && ((got.Type == "DNAME" && serverSetupDNSNameWithin(want.Name, name)) || (got.Type == "NS" && name != s.Zone && serverSetupDNSNameWithin(want.Name, name)) || (got.Type == "CNAME" && name == want.Name)) {
				return nil, infrastructureDNSReviewError(fmt.Sprintf("infrastructure hostname %s conflicts with an alias or delegated child", want.Name))
			}
			if name != want.Name || got.Type != want.Type {
				continue
			}
			if got.Disabled != 0 {
				return nil, infrastructureDNSReviewError(fmt.Sprintf("infrastructure record %s %s is disabled; review it first", want.Name, want.Type))
			}
			if want.Type == "SOA" {
				seen++
				if len(strings.Fields(got.Content)) != 7 || seen > 1 {
					return nil, infrastructureDNSReviewError("existing infrastructure SOA is ambiguous or invalid")
				}
				want.Content = got.Content
				want.TTL = got.TTL
				want.Action = "keep"
			} else if want.Type == "A" {
				if got.Content != want.Content {
					return nil, infrastructureDNSReviewError(fmt.Sprintf("infrastructure address for %s differs from the reviewed server address", want.Name))
				}
				want.TTL = got.TTL
				want.Action = "keep"
			} else if want.Type == "NS" {
				content := canonicalDNSName(got.Content)
				if content != d.NS1 && content != d.NS2 {
					return nil, infrastructureDNSReviewError("existing nameserver records differ from the reviewed DNS pair")
				}
				if content == want.Content {
					want.TTL = got.TTL
					want.Action = "keep"
				}
			}
		}
	}
	return plan, nil
}

func (p *Panel) buildServerSetupInfrastructureDNSPlan(ctx context.Context, d serverSetupDraft) (*serverSetupInfrastructureDNSPlan, error) {
	desired, err := serverSetupInfrastructureDNSDesired(d)
	if err != nil || desired == nil {
		return nil, err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	snapshot, err := readServerSetupInfrastructureDNSSnapshot(ctx, tx, d.InfrastructureDNS.Zone)
	if err != nil {
		return nil, err
	}
	return buildServerSetupInfrastructureDNSAgainstSnapshot(d, snapshot)
}

// prepareServerSetupInfrastructureDNSRecords commits additions and their owner
// receipt together. A retry reads the receipt and compares every existing record;
// it cannot turn an owner's changed address back into the old reviewed address.
func (p *Panel) prepareServerSetupInfrastructureDNSRecords(ctx context.Context, plan serverSetupPlan, step serverSetupExecutionStep, publisher dnsPublisherIdentity) (serverSetupInfrastructureDNSReceipt, error) {
	var receipt serverSetupInfrastructureDNSReceipt
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	s, err := readServerSetupInfrastructureDNSSnapshot(ctx, tx, plan.InfrastructureDNS.Zone)
	if err != nil {
		return receipt, err
	}
	current, err := buildServerSetupInfrastructureDNSAgainstSnapshot(plan.Draft, s)
	if err != nil {
		return receipt, err
	}
	expectedJSON, _ := json.Marshal(plan.InfrastructureDNS)
	currentJSON, _ := json.Marshal(current)
	if string(expectedJSON) != string(currentJSON) {
		return receipt, errServerSetupInfrastructureDNSChanged
	}
	zoneID := s.ZoneID
	if zoneID == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO pdns_domains(name,type) VALUES (?,'MASTER')`, s.Zone)
		if err != nil {
			return receipt, err
		}
		zoneID, err = result.LastInsertId()
		if err != nil {
			return receipt, err
		}
		owner, _ := json.Marshal(serverSetupInfrastructureDNSOwner{Zone: s.Zone, ZoneID: zoneID, PlanID: plan.ID})
		if _, err := tx.ExecContext(ctx, `INSERT INTO panel_settings(key,value) VALUES(?,?)`, serverSetupInfrastructureDNSOwnerKey(s.Zone), string(owner)); err != nil {
			return receipt, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pdns_domains SET type=? WHERE id=? AND type<>?`, current.ZoneType, zoneID, current.ZoneType); err != nil {
		return receipt, err
	}
	for _, r := range current.Records {
		if r.Action != "add" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pdns_records(domain_id,name,type,content,ttl) VALUES(?,?,?,?,?)`, zoneID, r.Name, r.Type, r.Content, r.TTL); err != nil {
			return receipt, err
		}
	}
	s, err = readServerSetupInfrastructureDNSSnapshot(ctx, tx, s.Zone)
	if err != nil {
		return receipt, err
	}
	syncState, err := readDNSZoneSyncState(ctx, tx, s.Zone)
	if err != nil {
		return receipt, err
	}
	receipt = serverSetupInfrastructureDNSReceipt{PreparedGeneration: syncState.DesiredGeneration, PlanID: plan.ID, StepID: step.ID, RequestID: step.RequestID, OwnerID: step.OwnerID, Zone: s.Zone, ZoneID: zoneID, Digest: serverSetupInfrastructureDNSSnapshotDigest(s, true), Engine: publisher.Engine, Epoch: publisher.Epoch}
	raw, _ := json.Marshal(receipt)
	if _, err := tx.ExecContext(ctx, `INSERT INTO panel_settings(key,value) VALUES(?,?)`, serverSetupInfrastructureDNSReceiptKey(step.RequestID), string(raw)); err != nil {
		return receipt, err
	}
	return receipt, tx.Commit()
}

func (p *Panel) infrastructureDNSReceiptApplied(ctx context.Context, r serverSetupInfrastructureDNSReceipt) (bool, error) {
	var count int
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM dns_zone_sync_state s JOIN dns_zone_engine_applications a ON a.zone_name=s.zone_name WHERE s.zone_name=? AND s.source_domain_id=? AND s.status='applied' AND s.desired_action='sync' AND s.desired_generation=s.applied_generation AND a.engine=? AND a.engine_epoch=? AND a.applied_generation=s.desired_generation AND a.applied_action='sync'`, r.Zone, r.ZoneID, r.Engine, r.Epoch).Scan(&count)
	return count == 1, err
}

func (p *Panel) runServerSetupInfrastructureDNS(ctx context.Context, plan serverSetupPlan, step serverSetupExecutionStep) (bool, error) {
	if err := p.requireServerSetupAdmission(); err != nil {
		return false, err
	}
	if plan.InfrastructureDNS == nil || plan.Draft.InfrastructureDNS == nil || plan.Draft.DNSMode != setupDNSModeLocal || plan.Draft.DNSRole != transport.DNSPairRolePrimary || plan.InfrastructureDNS.Zone != plan.Draft.InfrastructureDNS.Zone || step.Kind != "infrastructure_dns" || step.Target != plan.InfrastructureDNS.Zone || plan.ID != serverSetupPlanIdentity(plan) || !validServiceOperationID(step.RequestID) || !validServiceOperationID(step.OwnerID) {
		return false, errors.New("infrastructure DNS step does not match its immutable reviewed plan")
	}
	stepFound := false
	for _, candidate := range plan.Steps {
		if candidate == step.serverSetupPlanStep {
			stepFound = true
		}
	}
	if !stepFound {
		return false, errors.New("infrastructure DNS step is not in the reviewed plan")
	}
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	request, local, err := setupDNSIdentity(plan.Draft)
	if err != nil {
		return false, err
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return false, err
	}
	if !setupDNSDraftMatchesState(plan.Draft, request, local, state) {
		return false, errServerSetupInfrastructureDNSChanged
	}
	if err := p.requireNoPendingDNSClusterSaga(ctx); err != nil {
		return false, errServerSetupInfrastructureDNSWaiting
	}
	var raw string
	err = p.db.GetDB().QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key=?`, serverSetupInfrastructureDNSReceiptKey(step.RequestID)).Scan(&raw)
	var receipt serverSetupInfrastructureDNSReceipt
	if err == nil {
		if json.Unmarshal([]byte(raw), &receipt) != nil || receipt.PlanID != plan.ID || receipt.StepID != step.ID || receipt.RequestID != step.RequestID || receipt.OwnerID != step.OwnerID || receipt.Zone != plan.InfrastructureDNS.Zone || receipt.Engine != state.ActiveEngine || receipt.Epoch != state.EngineEpoch {
			return false, errServerSetupInfrastructureDNSChanged
		}
		s, err := readServerSetupInfrastructureDNSSnapshot(ctx, p.db.GetDB(), receipt.Zone)
		if err != nil {
			return false, err
		}
		if s.ZoneID != receipt.ZoneID || serverSetupInfrastructureDNSSnapshotDigest(s, true) != receipt.Digest {
			return false, errServerSetupInfrastructureDNSChanged
		}
		if applied, err := p.infrastructureDNSReceiptApplied(ctx, receipt); err != nil || applied {
			return applied, err
		}
		syncState, stateErr := readDNSZoneSyncState(ctx, p.db.GetDB(), receipt.Zone)
		if stateErr != nil {
			return false, errServerSetupInfrastructureDNSUnknown
		}
		if receipt.Failed || (syncState.Status == "error" && syncState.DesiredGeneration > receipt.PreparedGeneration) {
			return false, errors.New("infrastructure DNS publication failed; resolve the DNS operation and review a revised plan")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if plan.BuildCommit != strings.TrimSpace(buildCommit) {
			return false, errServerSetupBuildChanged
		}
		// Do not prepare records behind an older unresolved publication identity.
		if previous, stateErr := readDNSZoneSyncState(ctx, p.db.GetDB(), plan.InfrastructureDNS.Zone); stateErr == nil {
			if previous.hasLease() {
				return false, errServerSetupInfrastructureDNSUnknown
			}
		} else if !errors.Is(stateErr, sql.ErrNoRows) {
			return false, stateErr
		}
		if _, err := readDNSZoneEngineLease(ctx, p.db.GetDB(), plan.InfrastructureDNS.Zone); err == nil {
			return false, errServerSetupInfrastructureDNSUnknown
		} else if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		publisher, ready, err := p.activeDNSPublisher(ctx)
		if err != nil {
			return false, errServerSetupInfrastructureDNSUnknown
		}
		if !ready || publisher.PairRole != transport.DNSPairRolePrimary {
			return false, errServerSetupInfrastructureDNSWaiting
		}
		receipt, err = p.prepareServerSetupInfrastructureDNSRecords(ctx, plan, step, publisher)
		if err != nil {
			return false, err
		}
	} else {
		return false, err
	}
	// syncZoneToDNSLocked first reconciles any exact V3 lease. It never needs a
	// remote panel API; the native pair transfers the published owner zone.
	if err := p.syncZoneToDNSLocked(ctx, receipt.Zone, false); err != nil {
		if _, leaseErr := readDNSZoneEngineLease(ctx, p.db.GetDB(), receipt.Zone); leaseErr == nil {
			return false, errServerSetupInfrastructureDNSUnknown
		} else if !errors.Is(leaseErr, sql.ErrNoRows) {
			return false, errServerSetupInfrastructureDNSUnknown
		}
		syncState, stateErr := readDNSZoneSyncState(ctx, p.db.GetDB(), receipt.Zone)
		if stateErr != nil {
			return false, errServerSetupInfrastructureDNSUnknown
		}
		if syncState.Status == "error" && syncState.DesiredGeneration > receipt.PreparedGeneration {
			receipt.Failed = true
			next, _ := json.Marshal(receipt)
			if _, writeErr := p.db.GetDB().ExecContext(ctx, `UPDATE panel_settings SET value=? WHERE key=?`, string(next), serverSetupInfrastructureDNSReceiptKey(step.RequestID)); writeErr != nil {
				return false, errServerSetupInfrastructureDNSUnknown
			}
			return false, errors.New("infrastructure DNS publication failed; inspect DNS infrastructure and review a revised plan")
		}
		_, ready, readinessErr := p.activeDNSPublisher(ctx)
		if readinessErr != nil || ready {
			return false, errServerSetupInfrastructureDNSUnknown
		}
		return false, errServerSetupInfrastructureDNSWaiting
	}
	currentSnapshot, snapshotErr := readServerSetupInfrastructureDNSSnapshot(ctx, p.db.GetDB(), receipt.Zone)
	if snapshotErr != nil {
		return false, snapshotErr
	}
	if serverSetupInfrastructureDNSSnapshotDigest(currentSnapshot, true) != receipt.Digest {
		return false, errServerSetupInfrastructureDNSChanged
	}
	applied, err := p.infrastructureDNSReceiptApplied(ctx, receipt)
	if err != nil {
		return false, errServerSetupInfrastructureDNSUnknown
	}
	if !applied {
		return false, errServerSetupInfrastructureDNSUnknown
	}
	return true, nil
}
