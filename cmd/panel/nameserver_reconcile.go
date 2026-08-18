package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

type nameserverAddressPlan struct {
	Role      string
	NS1       string
	NS2       string
	PeerNS    string
	LocalIPv4 string
	PeerIPv4  string
}

type nameserverAddress struct {
	name string
	ipv4 string
}

func canonicalDNSName(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func canonicalIPv4(value string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil || ip.To4() == nil {
		return "", false
	}
	return ip.To4().String(), true
}

// localNameserverForPlan returns the shared name that belongs to this panel.
// In paired mode the configured peer owns one name and this server owns the
// other; standalone and the pre-role setup state use ns1 as the SOA primary.
func localNameserverForPlan(plan nameserverAddressPlan) (string, error) {
	ns1, ns2 := canonicalDNSName(plan.NS1), canonicalDNSName(plan.NS2)
	if ns1 == "" || ns2 == "" || ns1 == ns2 {
		return "", fmt.Errorf("two distinct nameserver names are required")
	}

	rawRole := strings.TrimSpace(plan.Role)
	if rawRole == "" || normalizeDNSRole(rawRole) == "standalone" {
		return ns1, nil
	}
	if normalizeDNSRole(rawRole) != "paired" {
		return "", fmt.Errorf("unsupported DNS role %q", rawRole)
	}
	peerNS := canonicalDNSName(plan.PeerNS)
	switch peerNS {
	case ns1:
		return ns2, nil
	case ns2:
		return ns1, nil
	default:
		return "", fmt.Errorf("the peer nameserver must be one of the saved nameservers")
	}
}

// nameserverAddressesForPlan turns the saved topology into the exact two A
// records it promises. An empty role is deliberately a no-op: the UI saves
// the shared names first, then asks the operator to choose a mode.
func nameserverAddressesForPlan(plan nameserverAddressPlan) ([]nameserverAddress, bool, error) {
	rawRole := strings.TrimSpace(plan.Role)
	if rawRole == "" {
		return nil, false, nil
	}
	role := normalizeDNSRole(rawRole)
	if role == "" {
		return nil, false, fmt.Errorf("unsupported DNS role %q", rawRole)
	}

	ns1, ns2 := canonicalDNSName(plan.NS1), canonicalDNSName(plan.NS2)
	if ns1 == "" || ns2 == "" || ns1 == ns2 {
		return nil, false, fmt.Errorf("two distinct nameserver names are required")
	}
	localIPv4, ok := canonicalIPv4(plan.LocalIPv4)
	if !ok {
		return nil, false, fmt.Errorf("this server has no usable IPv4 address")
	}

	addresses := []nameserverAddress{{name: ns1, ipv4: localIPv4}, {name: ns2, ipv4: localIPv4}}
	if role == "standalone" {
		return addresses, true, nil
	}

	peerNS := canonicalDNSName(plan.PeerNS)
	if peerNS != ns1 && peerNS != ns2 {
		return nil, false, fmt.Errorf("the peer nameserver must be one of the saved nameservers")
	}
	peerIPv4, ok := canonicalIPv4(plan.PeerIPv4)
	if !ok {
		return nil, false, fmt.Errorf("the peer server has no usable IPv4 address")
	}
	if peerIPv4 == localIPv4 {
		return nil, false, fmt.Errorf("the peer server cannot be this server")
	}
	for i := range addresses {
		if addresses[i].name == peerNS {
			addresses[i].ipv4 = peerIPv4
		}
	}
	return addresses, true, nil
}

func dnsNameIsInZone(name, zone string) bool {
	name, zone = canonicalDNSName(name), canonicalDNSName(zone)
	return name != "" && zone != "" && (name == zone || strings.HasSuffix(name, "."+zone))
}

// reconcileNameserverAddressesTx rewrites only A records for the saved pair,
// and only in ledger zones that own those names. AAAA records are intentionally
// outside this operation: a peer IPv6 address cannot be inferred from IPv4.
func reconcileNameserverAddressesTx(ctx context.Context, tx *sql.Tx, plan nameserverAddressPlan) error {
	addresses, configured, err := nameserverAddressesForPlan(plan)
	if err != nil || !configured {
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM pdns_domains ORDER BY id`)
	if err != nil {
		return err
	}
	type ledgerZone struct {
		id   int
		name string
	}
	var zones []ledgerZone
	for rows.Next() {
		var zone ledgerZone
		if err := rows.Scan(&zone.id, &zone.name); err != nil {
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

	for _, zone := range zones {
		for _, address := range addresses {
			if !dnsNameIsInZone(address.name, zone.name) {
				continue
			}
			// DNS names and RR types are case-insensitive. Remove every spelling
			// of this exact A RRset, then install one canonical enabled record.
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM pdns_records
				WHERE domain_id = ? AND LOWER(TRIM(name, '.')) = ? AND UPPER(type) = 'A'`,
				zone.id, address.name); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO pdns_records (domain_id, name, type, content, ttl, disabled, auth)
				VALUES (?, ?, 'A', ?, 3600, 0, 1)`,
				zone.id, address.name, address.ipv4); err != nil {
				return err
			}
		}
	}
	return nil
}

func setSettingTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO panel_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, key, value)
	return err
}

func settingTx(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func rewriteZoneSOAMNameTx(ctx context.Context, tx *sql.Tx, zoneID int, zoneName, localNS string) error {
	var recordID int
	var soa string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, content FROM pdns_records
		WHERE domain_id = ? AND LOWER(TRIM(name, '.')) = ? AND UPPER(type) = 'SOA'
		LIMIT 1`, zoneID, canonicalDNSName(zoneName)).Scan(&recordID, &soa); err != nil {
		return fmt.Errorf("zone %s has no SOA: %w", zoneName, err)
	}
	fields := strings.Fields(soa)
	if len(fields) < 7 {
		return fmt.Errorf("zone %s has an invalid SOA", zoneName)
	}
	fields[0] = localNS
	if _, err := tx.ExecContext(ctx,
		`UPDATE pdns_records SET content = ? WHERE id = ?`, strings.Join(fields, " "), recordID); err != nil {
		return err
	}
	return nil
}

func rewriteAllZoneSOAMNamesTx(ctx context.Context, tx *sql.Tx, localNS string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM pdns_domains ORDER BY id`)
	if err != nil {
		return err
	}
	type zone struct {
		id   int
		name string
	}
	var zones []zone
	for rows.Next() {
		var z zone
		if err := rows.Scan(&z.id, &z.name); err != nil {
			rows.Close()
			return err
		}
		zones = append(zones, z)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, z := range zones {
		if err := rewriteZoneSOAMNameTx(ctx, tx, z.id, z.name, localNS); err != nil {
			return err
		}
	}
	return nil
}

// saveDNSClusterSettingsAndReconcile makes the selected nameserver pair,
// operating mode and the records that express them visible in one ledger
// transaction. This matters when a paired setup renames the name owned by its
// peer: validating or saving the names against the old peer tuple first would
// either reject the change or leave partially rewritten zones behind.
func (p *Panel) saveDNSClusterSettingsAndReconcile(ctx context.Context, role, peerIP, peerNS, ns1, ns2, localIPv4 string) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := reconcileDNSEngineTopologyTx(ctx, tx, role); err != nil {
		return err
	}
	if err := saveDNSClusterSettingsAndReconcileTx(
		ctx, tx, role, peerIP, peerNS, ns1, ns2, localIPv4,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func saveDNSClusterSettingsAndReconcileTx(
	ctx context.Context,
	tx *sql.Tx,
	role, peerIP, peerNS, ns1, ns2, localIPv4 string,
) error {
	settings := []struct{ key, value string }{
		{settingNS1, ns1},
		{settingNS2, ns2},
		{settingDNSRole, role},
		{settingDNSPeerIP, peerIP},
		{settingDNSPeerNS, peerNS},
	}
	for _, setting := range settings {
		if err := setSettingTx(ctx, tx, setting.key, setting.value); err != nil {
			return err
		}
	}
	plan := nameserverAddressPlan{
		Role: role, NS1: ns1, NS2: ns2, PeerNS: peerNS,
		LocalIPv4: localIPv4, PeerIPv4: peerIP,
	}
	localNS, err := localNameserverForPlan(plan)
	if err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM pdns_domains ORDER BY id`)
	if err != nil {
		return err
	}
	type zone struct {
		id   int
		name string
	}
	var zones []zone
	for rows.Next() {
		var z zone
		if err := rows.Scan(&z.id, &z.name); err != nil {
			rows.Close()
			return err
		}
		zones = append(zones, z)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, z := range zones {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM pdns_records
			WHERE domain_id = ? AND LOWER(TRIM(name, '.')) = ? AND UPPER(type) = 'NS'`,
			z.id, canonicalDNSName(z.name)); err != nil {
			return err
		}
		for _, ns := range []string{ns1, ns2} {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO pdns_records (domain_id, name, type, content, ttl, disabled, auth)
				VALUES (?, ?, 'NS', ?, 3600, 0, 1)`, z.id, z.name, ns); err != nil {
				return err
			}
		}
		if err := rewriteZoneSOAMNameTx(ctx, tx, z.id, z.name, localNS); err != nil {
			return err
		}
	}
	if err := reconcileNameserverAddressesTx(ctx, tx, plan); err != nil {
		return err
	}
	return nil
}

// stageDNSClusterSettingsAndReconcile binds the complete DNS identity to the
// exact unresolved engine revision. All durable ambiguity checks, the revision
// CAS and the nameserver/zone rewrite commit as one transaction. Runtime facts
// are re-proven by the caller while it owns the complete DNS mutation lock.
func (p *Panel) stageDNSClusterSettingsAndReconcile(
	ctx context.Context,
	expected dnsEngineDBState,
	role, peerIP, peerNS, ns1, ns2, localIPv4 string,
) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	pairExists, err := hasPersistedDNSPairIdentity(ctx, tx)
	if err != nil {
		return fmt.Errorf("read staged DNS pair identity: %w", err)
	}
	if pairExists {
		return fmt.Errorf("%w: paired identity is already epoch-bound", errDNSIdentityStagingConflict)
	}
	state, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return fmt.Errorf("read staged DNS engine state: %w", err)
	}
	if state != expected || !exactUninitializedDNSEngineState(state) {
		return fmt.Errorf("%w: unresolved engine revision changed", errDNSIdentityStagingConflict)
	}
	marker, err := readDNSEngineOperationMarker(ctx, tx)
	if err != nil {
		return fmt.Errorf("read DNS engine operation marker: %w", err)
	}
	if marker != nil {
		return fmt.Errorf("%w: engine operation is pending", errDNSIdentityStagingConflict)
	}
	rawSaga, err := settingTx(ctx, tx, dnsClusterSagaSetting)
	if err != nil {
		return fmt.Errorf("read DNS cluster operation marker: %w", err)
	}
	if rawSaga != "" {
		return fmt.Errorf("%w: topology operation is pending", errDNSIdentityStagingConflict)
	}
	pendingPublication, err := hasDNSPublicationPending(ctx, tx)
	if err != nil {
		return fmt.Errorf("read DNS publication state: %w", err)
	}
	if pendingPublication {
		return fmt.Errorf("%w: publication is pending", errDNSIdentityStagingConflict)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_state
		SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		WHERE singleton_id = 1 AND active_engine IS NULL
		  AND active_epoch = 0 AND revision = ? AND topology = 'standalone'
		  AND current_switch_id IS NULL`, expected.Revision)
	if err != nil {
		return fmt.Errorf("advance DNS identity staging revision: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: engine state CAS failed", errDNSIdentityStagingConflict)
	}
	if err := saveDNSClusterSettingsAndReconcileTx(
		ctx, tx, role, peerIP, peerNS, ns1, ns2, localIPv4,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// reconcileDNSEngineTopologyTx keeps the durable publisher topology and the
// operator-owned DNS identity in the same transaction. A PowerDNS topology
// transition is authorized only by the exact published cluster saga. An active
// BIND pair keeps its inherited topology in the separate epoch-bound pair
// ledger and is read-only here; unresolved legacy PowerDNS retains its settings
// for explicit adoption without guessing authority.
func reconcileDNSEngineTopologyTx(
	ctx context.Context,
	tx *sql.Tx,
	role string,
) error {
	var active sql.NullString
	var currentSwitch sql.NullString
	var epoch, revision int64
	var topology string
	if err := tx.QueryRowContext(ctx, `
		SELECT active_engine, active_epoch, revision, topology, current_switch_id
		FROM dns_engine_state WHERE singleton_id = 1`,
	).Scan(&active, &epoch, &revision, &topology, &currentSwitch); err != nil {
		return fmt.Errorf("read durable DNS engine topology: %w", err)
	}
	if currentSwitch.Valid {
		return errors.New("DNS engine switch is attached during topology reconciliation")
	}
	if !active.Valid {
		if epoch != 0 || topology != transport.DNSTopologyStandalone {
			return errors.New("unresolved DNS engine state is not canonical")
		}
		return nil
	}
	if active.String == string(transport.DNSEngineBIND) {
		if topology != transport.DNSTopologyStandalone || role != "standalone" {
			return errors.New("BIND DNS topology must remain standalone")
		}
		return nil
	}
	if active.String != string(transport.DNSEnginePowerDNS) || epoch < 1 {
		return errors.New("durable DNS engine topology is invalid")
	}
	if role != transport.DNSTopologyStandalone &&
		role != transport.DNSTopologyPaired {
		return errors.New("PowerDNS topology is invalid")
	}
	if topology == role {
		return nil
	}
	raw, err := settingTx(ctx, tx, dnsClusterSagaSetting)
	if err != nil {
		return fmt.Errorf("read DNS topology finalization authority: %w", err)
	}
	saga, err := decodeDNSClusterSaga(raw)
	if err != nil || saga == nil || saga.Phase != dnsClusterSagaPublished ||
		saga.Previous.Role != topology || saga.Desired.Role != role {
		return errors.New("DNS topology change lacks its exact published saga")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE dns_engine_state
		SET topology = ?, revision = revision + 1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE singleton_id = 1 AND active_engine = 'pdns'
		  AND active_epoch = ? AND revision = ? AND topology = ?
		  AND current_switch_id IS NULL`,
		role, epoch, revision, topology,
	)
	if err != nil {
		return fmt.Errorf("advance durable PowerDNS topology: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return errors.New("durable PowerDNS topology lost its exact state CAS")
	}
	return nil
}

// saveNameservers changes the server identity and every existing zone in one
// ledger transaction. The served PowerDNS copies are published afterwards;
// syncZoneToDNS advances each SOA serial before sending it.
func (p *Panel) saveNameservers(ctx context.Context, ns1, ns2 string) error {
	if err := p.requireDNSZoneSyncV2Agent(ctx); err != nil {
		return fmt.Errorf("verify nameserver publisher: %w", err)
	}
	if _, err := p.saveNameserversLedger(ctx, ns1, ns2, serverPrimaryIP()); err != nil {
		return err
	}
	if _, err := p.syncAllZonesStrict(ctx); err != nil {
		return fmt.Errorf("publish nameserver settings: %w", err)
	}
	return nil
}

func (p *Panel) saveNameserversLedger(ctx context.Context, ns1, ns2, localIPv4 string) ([]string, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, setting := range []struct{ key, value string }{{settingNS1, ns1}, {settingNS2, ns2}} {
		if err := setSettingTx(ctx, tx, setting.key, setting.value); err != nil {
			return nil, err
		}
	}
	role, err := settingTx(ctx, tx, settingDNSRole)
	if err != nil {
		return nil, err
	}
	peerIP, err := settingTx(ctx, tx, settingDNSPeerIP)
	if err != nil {
		return nil, err
	}
	peerNS, err := settingTx(ctx, tx, settingDNSPeerNS)
	if err != nil {
		return nil, err
	}
	plan := nameserverAddressPlan{
		Role: role, NS1: ns1, NS2: ns2, PeerNS: peerNS,
		LocalIPv4: localIPv4, PeerIPv4: peerIP,
	}
	localNS, err := localNameserverForPlan(plan)
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM pdns_domains ORDER BY id`)
	if err != nil {
		return nil, err
	}
	type zone struct {
		id   int
		name string
	}
	var zones []zone
	for rows.Next() {
		var z zone
		if err := rows.Scan(&z.id, &z.name); err != nil {
			rows.Close()
			return nil, err
		}
		zones = append(zones, z)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, z := range zones {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pdns_records WHERE domain_id = ? AND name = ? AND type = 'NS'`, z.id, z.name); err != nil {
			return nil, err
		}
		for _, ns := range []string{ns1, ns2} {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO pdns_records (domain_id, name, type, content, ttl, disabled, auth)
				VALUES (?, ?, 'NS', ?, 3600, 0, 1)`, z.id, z.name, ns); err != nil {
				return nil, err
			}
		}
		if err := rewriteZoneSOAMNameTx(ctx, tx, z.id, z.name, localNS); err != nil {
			return nil, err
		}
	}
	if err := reconcileNameserverAddressesTx(ctx, tx, plan); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	zoneNames := make([]string, 0, len(zones))
	for _, zone := range zones {
		zoneNames = append(zoneNames, zone.name)
	}
	return zoneNames, nil
}
