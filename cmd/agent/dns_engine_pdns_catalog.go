package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const pdnsBINDCatalogAccount = "celikpanel-bind-catalog-v1"

const pdnsPeerCatalogAccount = "celikpanel-peer-catalog-v1"

type managedPDNSCatalog struct {
	Domain        string
	LocalIP       string
	PeerIP        string
	Serial        uint32
	Members       []string
	MemberSerials []uint32
}

func peerPDNSCatalog(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (dnsCatalogAXFRResult, string, error) {
	if manifest.Topology != transport.DNSTopologyPaired ||
		manifest.PairRole != transport.DNSPairRoleSecondary {
		return dnsCatalogAXFRResult{}, "", errors.New("PowerDNS peer catalog requires a secondary manifest")
	}
	domain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		return dnsCatalogAXFRResult{}, "", err
	}
	catalog, err := probeDNSCatalogAXFR(ctx, manifest.PeerIP, domain)
	if err != nil || catalog.Serial == 0 {
		return dnsCatalogAXFRResult{}, "", errors.New("paired primary catalog is unavailable")
	}
	return catalog, domain, nil
}

func stagePDNSPairSecondaryTx(
	ctx context.Context,
	tx *sql.Tx,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	catalog dnsCatalogAXFRResult,
	catalogDomain string,
) error {
	if tx == nil || catalogDomain == "" {
		return errors.New("PowerDNS secondary staging identity is incomplete")
	}
	if catalog.Serial == 0 {
		return errors.New("PowerDNS secondary catalog serial is invalid")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO domains(name,type,master,account)
		VALUES(?, 'CONSUMER', ?, ?)
	`, catalogDomain, manifest.PeerIP, pdnsPeerCatalogAccount)
	return err
}

func verifyPDNSPairSecondaryTx(
	ctx context.Context,
	tx *sql.Tx,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	catalog dnsCatalogAXFRResult,
	catalogDomain string,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT name, UPPER(type), COALESCE(master,''), COALESCE(account,''),
		       COALESCE(catalog,'')
		FROM domains ORDER BY name COLLATE BINARY
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name, zoneType, master, account, memberCatalog string
		if err := rows.Scan(&name, &zoneType, &master, &account, &memberCatalog); err != nil {
			return err
		}
		if name != catalogDomain || zoneType != "CONSUMER" ||
			master != manifest.PeerIP || account != pdnsPeerCatalogAccount ||
			memberCatalog != "" {
			return errors.New("PowerDNS secondary candidate is not an exact catalog consumer")
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if seen != 1 {
		return errors.New("PowerDNS secondary candidate has no exact catalog consumer")
	}
	return nil
}

func retrievePDNSPairSecondaryZones(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	catalog, domain, err := peerPDNSCatalog(ctx, manifest)
	if err != nil {
		return err
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dnsPairProofLimit)
	defer cancel()
	if output, err := dnsClusterRetrieve(recoveryCtx, domain); err != nil {
		return fmt.Errorf("retrieve paired PowerDNS catalog: %w: %s", err, firstLine(string(output)))
	}
	for {
		ready := true
		for _, zone := range catalog.Members {
			if output, err := dnsClusterRetrieve(recoveryCtx, zone); err != nil {
				_ = output
				ready = false
				break
			}
		}
		if ready {
			for _, zone := range append([]string{domain}, catalog.Members...) {
				if _, err := dnsClusterPurge(recoveryCtx, zone); err != nil {
					return errors.New("paired PowerDNS cache purge failed")
				}
			}
			return nil
		}
		select {
		case <-recoveryCtx.Done():
			return errors.New("paired PowerDNS catalog members were not provisioned")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func managedPDNSCatalogIdentity() (managedPDNSCatalog, bool, error) {
	if err := validateDNSClusterConfigTarget(); err != nil {
		return managedPDNSCatalog{}, false, err
	}
	config, err := dnsClusterConfigReadFile(dnsClusterConf)
	if errors.Is(err, os.ErrNotExist) {
		return managedPDNSCatalog{}, false, nil
	}
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if !validDNSClusterPowerDNSConfig(string(config)) {
		return managedPDNSCatalog{}, false,
			errors.New("managed PowerDNS pair configuration is not canonical")
	}
	peerIP := ""
	for _, raw := range strings.Split(string(config), "\n") {
		key, value, found := powerDNSConfigDirective(raw)
		if found && key == "allow-axfr-ips" {
			peerIP = value
		}
	}
	if peerIP == "" {
		return managedPDNSCatalog{}, false,
			errors.New("managed PowerDNS pair has no exact peer address")
	}
	localIP, err := dnsPairLocalProofAddress()
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	domain, err := binddns.CatalogDomain(localIP)
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	return managedPDNSCatalog{
		Domain: domain, LocalIP: localIP, PeerIP: peerIP,
	}, true, nil
}

func reconcilePDNSBINDCatalogTx(
	ctx context.Context,
	tx *sql.Tx,
	enabled bool,
	localIP string,
) (managedPDNSCatalog, error) {
	return reconcilePDNSBINDCatalogFromSnapshotTx(ctx, tx, enabled, localIP, nil)
}

func reconcilePDNSBINDCatalogFromSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	enabled bool,
	localIP string,
	previous *managedPDNSCatalog,
) (managedPDNSCatalog, error) {
	if tx == nil {
		return managedPDNSCatalog{}, errors.New("PowerDNS catalog transaction is required")
	}
	if !enabled && previous != nil {
		return managedPDNSCatalog{}, errors.New("disabled PowerDNS catalog cannot have a membership snapshot")
	}
	if !enabled {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, name FROM domains WHERE account = ?
		`, pdnsBINDCatalogAccount)
		if err != nil {
			return managedPDNSCatalog{}, err
		}
		var ids []int64
		var names []string
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return managedPDNSCatalog{}, err
			}
			ids = append(ids, id)
			names = append(names, name)
		}
		if err := rows.Close(); err != nil {
			return managedPDNSCatalog{}, err
		}
		for index, id := range ids {
			if _, err := tx.ExecContext(
				ctx, "UPDATE domains SET catalog = NULL WHERE catalog = ?", names[index],
			); err != nil {
				return managedPDNSCatalog{}, err
			}
			for _, table := range []string{"records", "comments", "domainmetadata", "cryptokeys"} {
				if _, err := tx.ExecContext(
					ctx, "DELETE FROM "+table+" WHERE domain_id = ?", id,
				); err != nil {
					return managedPDNSCatalog{}, err
				}
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM domains WHERE id = ?", id); err != nil {
				return managedPDNSCatalog{}, err
			}
		}
		return managedPDNSCatalog{}, nil
	}

	domain, err := binddns.CatalogDomain(localIP)
	if err != nil {
		return managedPDNSCatalog{}, err
	}
	var ownedCatalogs int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM domains WHERE account = ?
	`, pdnsBINDCatalogAccount).Scan(&ownedCatalogs); err != nil {
		return managedPDNSCatalog{}, err
	}
	if ownedCatalogs > 1 {
		return managedPDNSCatalog{}, errors.New("PowerDNS contains multiple managed catalogs")
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, COALESCE(catalog, '') FROM domains
		WHERE UPPER(type) IN ('NATIVE','MASTER','PRIMARY')
		  AND COALESCE(account, '') <> ?
		ORDER BY name COLLATE BINARY
	`, pdnsBINDCatalogAccount)
	if err != nil {
		return managedPDNSCatalog{}, err
	}
	members := make([]string, 0)
	for rows.Next() {
		var member, currentCatalog string
		if err := rows.Scan(&member, &currentCatalog); err != nil {
			rows.Close()
			return managedPDNSCatalog{}, err
		}
		if member == domain {
			rows.Close()
			return managedPDNSCatalog{}, errors.New("PowerDNS catalog identity is owned outside CelikPanel")
		}
		if currentCatalog != "" && currentCatalog != domain {
			rows.Close()
			return managedPDNSCatalog{}, errors.New("PowerDNS zone belongs to a foreign catalog")
		}
		members = append(members, member)
	}
	if err := rows.Close(); err != nil {
		return managedPDNSCatalog{}, err
	}

	var domainID int64
	var zoneType, account string
	err = tx.QueryRowContext(ctx, `
		SELECT id, type, COALESCE(account, '') FROM domains
		WHERE name = ? COLLATE NOCASE
	`, domain).Scan(&domainID, &zoneType, &account)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return managedPDNSCatalog{}, err
	}
	if exists {
		if strings.ToUpper(zoneType) != "PRODUCER" ||
			account != pdnsBINDCatalogAccount || ownedCatalogs != 1 {
			return managedPDNSCatalog{}, errors.New("PowerDNS catalog row is not exact panel authority")
		}
	} else {
		if ownedCatalogs != 0 {
			return managedPDNSCatalog{}, errors.New("PowerDNS managed catalog address changed")
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO domains (name, type, account) VALUES (?, 'PRODUCER', ?)
		`, domain, pdnsBINDCatalogAccount)
		if err != nil {
			return managedPDNSCatalog{}, err
		}
		domainID, err = result.LastInsertId()
		if err != nil {
			return managedPDNSCatalog{}, err
		}
	}
	serial := uint32(1)
	if !exists {
		if previous != nil {
			return managedPDNSCatalog{}, errors.New("PowerDNS catalog membership snapshot has no producer")
		}
		records, recordErr := canonicalPDNSCatalogBaseRecords(localIP, serial, members)
		if recordErr != nil {
			return managedPDNSCatalog{}, recordErr
		}
		for _, record := range records {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO records
				(domain_id, name, type, content, ttl, prio, disabled, auth)
				VALUES (?, ?, ?, ?, ?, ?, 0, 1)
			`, domainID, record.Name, record.Type, record.Content,
				record.TTL, record.Prio); err != nil {
				return managedPDNSCatalog{}, err
			}
		}
	} else {
		stored, currentSerial, recordErr := readPDNSBINDCatalogRecordsTx(
			ctx, tx, domainID, domain,
		)
		if recordErr != nil {
			return managedPDNSCatalog{}, recordErr
		}
		expected, recordErr := canonicalPDNSCatalogBaseRecords(
			localIP, currentSerial, members,
		)
		if recordErr != nil {
			return managedPDNSCatalog{}, recordErr
		}
		if !reflect.DeepEqual(canonicalPDNSCatalogRecords(stored), expected) {
			return managedPDNSCatalog{}, errors.New("PowerDNS catalog base records are not canonical")
		}
		serial = currentSerial
	}

	// A delete removes the only row that proves the old membership. V3 callers
	// therefore pass the snapshot taken before their zone mutation; all other
	// callers compare the producer's current exact membership in this tx.
	previousMembers, err := readPDNSBINDCatalogMembersTx(ctx, tx, domain)
	if err != nil {
		return managedPDNSCatalog{}, err
	}
	if previous != nil {
		if previous.Domain != domain || previous.LocalIP != localIP ||
			previous.Serial != serial {
			return managedPDNSCatalog{}, errors.New("PowerDNS catalog membership snapshot is stale")
		}
		previousMembers = append([]string(nil), previous.Members...)
		sort.Strings(previousMembers)
	}
	membershipChanged := exists && !reflect.DeepEqual(previousMembers, members)
	if membershipChanged {
		// Wrapping a catalog serial could make a stale secondary appear current.
		// Refuse the whole transaction instead; the caller rolls back both the
		// zone mutation and this producer update.
		if serial == ^uint32(0) {
			return managedPDNSCatalog{}, errors.New("PowerDNS catalog serial is exhausted")
		}
		serial++
		records, recordErr := canonicalPDNSCatalogBaseRecords(localIP, serial, members)
		if recordErr != nil {
			return managedPDNSCatalog{}, recordErr
		}
		var soa transport.ZoneRecord
		for _, record := range records {
			if record.Type == "SOA" {
				soa = record
				break
			}
		}
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE records SET content = ?, ttl = ?, prio = ?, disabled = 0,
			 ordername = NULL, auth = 1
			WHERE domain_id = ? AND name = ? COLLATE BINARY AND type = 'SOA'
		`, soa.Content, soa.TTL, soa.Prio, domainID, domain)
		if updateErr != nil {
			return managedPDNSCatalog{}, updateErr
		}
		if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
			if rowsErr == nil {
				rowsErr = errors.New("PowerDNS catalog SOA serial update was not exact")
			}
			return managedPDNSCatalog{}, rowsErr
		}
	}

	desired := make(map[string]bool, len(members))
	for _, member := range members {
		desired[member] = true
	}
	currentMembers, err := readPDNSBINDCatalogMembersTx(ctx, tx, domain)
	if err != nil {
		return managedPDNSCatalog{}, err
	}
	for _, member := range currentMembers {
		if desired[member] {
			continue
		}
		result, clearErr := tx.ExecContext(ctx, `
			UPDATE domains SET catalog = NULL
			WHERE name = ? COLLATE BINARY AND catalog = ? COLLATE NOCASE
			  AND UPPER(type) NOT IN ('PRODUCER','CONSUMER')
		`, member, domain)
		if clearErr != nil {
			return managedPDNSCatalog{}, clearErr
		}
		if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
			if rowsErr == nil {
				rowsErr = errors.New("PowerDNS catalog member removal was not exact")
			}
			return managedPDNSCatalog{}, rowsErr
		}
	}
	for _, member := range members {
		result, err := tx.ExecContext(ctx, `
			UPDATE domains SET catalog = ? WHERE name = ? COLLATE NOCASE
			  AND UPPER(type) IN ('NATIVE','MASTER','PRIMARY')
		`, domain, member)
		if err != nil {
			return managedPDNSCatalog{}, err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err == nil {
				err = errors.New("PowerDNS catalog member update was not exact")
			}
			return managedPDNSCatalog{}, err
		}
	}
	return managedPDNSCatalog{
		Domain: domain, LocalIP: localIP, Serial: serial, Members: members,
	}, nil
}

func readPDNSBINDCatalogMembersTx(
	ctx context.Context,
	tx *sql.Tx,
	domain string,
) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT name, UPPER(type) FROM domains
		WHERE catalog = ? COLLATE NOCASE
		ORDER BY name COLLATE BINARY
	`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]string, 0)
	for rows.Next() {
		var member, zoneType string
		if err := rows.Scan(&member, &zoneType); err != nil {
			return nil, err
		}
		if zoneType == "PRODUCER" || zoneType == "CONSUMER" {
			return nil, errors.New("PowerDNS catalog contains an invalid member row")
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func canonicalPDNSCatalogBaseRecords(
	localIP string,
	serial uint32,
	members []string,
) ([]transport.ZoneRecord, error) {
	_, rendered, err := binddns.CatalogZoneRecords(localIP, serial, members)
	if err != nil {
		return nil, err
	}
	records := make([]transport.ZoneRecord, 0, 2)
	types := make(map[string]int, 2)
	for _, record := range rendered {
		if record.Type == "SOA" || record.Type == "NS" {
			records = append(records, record)
			types[record.Type]++
		}
	}
	if len(records) != 2 || types["SOA"] != 1 || types["NS"] != 1 {
		return nil, errors.New("PowerDNS catalog base renderer is incomplete")
	}
	return canonicalPDNSCatalogRecords(records), nil
}

func readPDNSBINDCatalogRecordsTx(
	ctx context.Context,
	tx *sql.Tx,
	domainID int64,
	domain string,
) ([]transport.ZoneRecord, uint32, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, content, ttl, prio, disabled,
		       COALESCE(ordername, ''), COALESCE(auth, -1)
		FROM records WHERE domain_id = ?
		ORDER BY name COLLATE BINARY, type COLLATE BINARY,
		 content COLLATE BINARY, ttl, prio, disabled, id
	`, domainID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	records := make([]transport.ZoneRecord, 0)
	var serial uint32
	soaCount := 0
	for rows.Next() {
		var record transport.ZoneRecord
		var disabled, auth int
		var ordername string
		if err := rows.Scan(
			&record.Name, &record.Type, &record.Content,
			&record.TTL, &record.Prio, &disabled, &ordername, &auth,
		); err != nil {
			return nil, 0, err
		}
		if disabled != 0 || ordername != "" || auth != 1 {
			return nil, 0, errors.New("PowerDNS catalog contains a noncanonical record")
		}
		if record.Name == domain && record.Type == "SOA" {
			fields := strings.Fields(record.Content)
			if len(fields) != 7 {
				return nil, 0, errors.New("PowerDNS catalog SOA is malformed")
			}
			value, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil || value == 0 {
				return nil, 0, errors.New("PowerDNS catalog serial is invalid")
			}
			serial = uint32(value)
			soaCount++
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if soaCount != 1 {
		return nil, 0, errors.New("PowerDNS catalog has no exact SOA")
	}
	return records, serial, nil
}

func canonicalPDNSCatalogRecords(records []transport.ZoneRecord) []transport.ZoneRecord {
	copy := append([]transport.ZoneRecord(nil), records...)
	sort.Slice(copy, func(i, j int) bool {
		left := fmt.Sprintf(
			"%s\x00%s\x00%s\x00%010d\x00%010d",
			copy[i].Name, copy[i].Type, copy[i].Content, copy[i].TTL, copy[i].Prio,
		)
		right := fmt.Sprintf(
			"%s\x00%s\x00%s\x00%010d\x00%010d",
			copy[j].Name, copy[j].Type, copy[j].Content, copy[j].TTL, copy[j].Prio,
		)
		return left < right
	})
	return copy
}

func reconcileManagedPDNSBINDCatalogTx(
	ctx context.Context,
	tx *sql.Tx,
) (managedPDNSCatalog, bool, error) {
	return reconcileManagedPDNSBINDCatalogFromSnapshotTx(ctx, tx, nil)
}

func reconcileManagedPDNSBINDCatalogFromSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	previous *managedPDNSCatalog,
) (managedPDNSCatalog, bool, error) {
	identity, enabled, err := managedPDNSCatalogIdentity()
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if previous != nil && enabled && previous.PeerIP != identity.PeerIP {
		return managedPDNSCatalog{}, false,
			errors.New("managed PowerDNS catalog peer changed during zone mutation")
	}
	result, err := reconcilePDNSBINDCatalogFromSnapshotTx(
		ctx, tx, enabled, identity.LocalIP, previous,
	)
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if enabled {
		result.PeerIP = identity.PeerIP
	}
	return result, enabled, nil
}

func verifyManagedPDNSBINDCatalog(
	ctx context.Context,
	requirePeer bool,
) error {
	identity, enabled, err := readManagedPDNSPrimaryCatalog(ctx)
	if err != nil || !enabled {
		return err
	}
	if _, err := dnsClusterPurge(ctx, identity.Domain); err != nil {
		return errors.New("PowerDNS catalog cache purge failed")
	}
	live, err := probeDNSCatalogAXFR(ctx, identity.LocalIP, identity.Domain)
	if err != nil || live.Serial != identity.Serial ||
		!reflect.DeepEqual(live.Members, identity.Members) {
		return errors.New("PowerDNS live catalog differs from its database")
	}
	if requirePeer {
		if err := verifyDNSPrimaryPairReadyAt(ctx, dnsPrimaryCatalogEvidence{
			LocalIP: identity.LocalIP, PeerIP: identity.PeerIP,
			Domain: identity.Domain, Serial: identity.Serial,
			Members: identity.Members, MemberSerials: identity.MemberSerials,
		}, probeDNSZoneSOA, probeDNSCatalogAXFR, probeDNSBoundCatalogAXFR); err != nil {
			return errors.New("PowerDNS primary catalog did not converge on the paired peer")
		}
	}
	return nil
}

// readManagedPDNSPrimaryCatalog returns one transactionally consistent,
// canonical producer identity: configuration, stored SOA serial and sorted
// membership. Callers still have to prove the same evidence over DNS.
func readManagedPDNSPrimaryCatalog(
	ctx context.Context,
) (managedPDNSCatalog, bool, error) {
	identity, enabled, err := managedPDNSCatalogIdentity()
	if err != nil || !enabled {
		return managedPDNSCatalog{}, enabled, err
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	defer tx.Rollback()
	var ownedCatalogs, peerCatalogs int
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM domains WHERE account = ?`,
		pdnsBINDCatalogAccount,
	).Scan(&ownedCatalogs); err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM domains WHERE account = ?`,
		pdnsPeerCatalogAccount,
	).Scan(&peerCatalogs); err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if ownedCatalogs == 0 && peerCatalogs == 1 {
		peerDomain, domainErr := binddns.CatalogDomain(identity.PeerIP)
		if domainErr != nil {
			return managedPDNSCatalog{}, false, domainErr
		}
		var name, zoneType, master, catalog string
		if err := tx.QueryRowContext(ctx, `
			SELECT name, UPPER(type), COALESCE(master,''), COALESCE(catalog,'')
			FROM domains WHERE account = ?
		`, pdnsPeerCatalogAccount).Scan(
			&name, &zoneType, &master, &catalog,
		); err != nil {
			return managedPDNSCatalog{}, false, err
		}
		if name != peerDomain || zoneType != "CONSUMER" ||
			master != identity.PeerIP || catalog != "" {
			return managedPDNSCatalog{}, false,
				errors.New("PowerDNS managed consumer identity is not exact")
		}
		if err := tx.Commit(); err != nil {
			return managedPDNSCatalog{}, false, err
		}
		return managedPDNSCatalog{}, false, nil
	}
	if ownedCatalogs != 1 || peerCatalogs != 0 {
		return managedPDNSCatalog{}, false,
			errors.New("PowerDNS managed catalog ownership is ambiguous")
	}
	var domainID int64
	var account, zoneType string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, COALESCE(account, ''), UPPER(type)
		FROM domains WHERE name = ? COLLATE NOCASE
	`, identity.Domain).Scan(&domainID, &account, &zoneType); err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if account != pdnsBINDCatalogAccount || zoneType != "PRODUCER" {
		return managedPDNSCatalog{}, false, errors.New("PowerDNS live catalog is not panel owned")
	}
	serial, err := verifyPDNSProducerBaseTx(
		ctx, tx, domainID, identity.Domain, identity.LocalIP,
	)
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	members, err := readPDNSBINDCatalogMembersTx(ctx, tx, identity.Domain)
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	if _, err := canonicalPDNSCatalogBaseRecords(
		identity.LocalIP, serial, members,
	); err != nil {
		return managedPDNSCatalog{}, false, err
	}
	memberSerials := make([]uint32, len(members))
	for index, member := range members {
		zoneType, records, found, readErr := readPDNSV3ZoneTx(ctx, tx, member)
		if readErr != nil || !found {
			if readErr == nil {
				readErr = errors.New("PowerDNS catalog member has no durable zone")
			}
			return managedPDNSCatalog{}, false, readErr
		}
		expected, expectedErr := expectedDNSZoneAuthorities(
			[]transport.DNSEngineSwitchZoneSnapshot{{
				Domain: member, ZoneType: zoneType, Records: records,
			}},
		)
		if expectedErr != nil {
			return managedPDNSCatalog{}, false, expectedErr
		}
		memberSerials[index] = expected[0].Serial
	}
	if err := tx.Commit(); err != nil {
		return managedPDNSCatalog{}, false, err
	}
	identity.Serial = serial
	identity.Members = members
	identity.MemberSerials = memberSerials
	return identity, true, nil
}

func verifyPDNSProducerBaseTx(
	ctx context.Context,
	tx *sql.Tx,
	domainID int64,
	domain string,
	localIP string,
) (uint32, error) {
	records, serial, err := readPDNSBINDCatalogRecordsTx(
		ctx, tx, domainID, domain,
	)
	if err != nil {
		return 0, err
	}
	expected, err := canonicalPDNSCatalogBaseRecords(localIP, serial, nil)
	if err != nil {
		return 0, err
	}
	if !reflect.DeepEqual(canonicalPDNSCatalogRecords(records), expected) {
		return 0, errors.New("PowerDNS producer contains noncanonical base records")
	}
	for _, table := range []string{"comments", "cryptokeys"} {
		var count int
		if err := tx.QueryRowContext(
			ctx, "SELECT COUNT(*) FROM "+table+" WHERE domain_id = ?", domainID,
		).Scan(&count); err != nil || count != 0 {
			if err == nil {
				err = errors.New("PowerDNS producer contains unexpected side data")
			}
			return 0, err
		}
	}
	return serial, nil
}

func verifyPDNSProducerMembershipTx(
	ctx context.Context,
	tx *sql.Tx,
	localIP string,
	expectedMembers []string,
) error {
	domain, err := binddns.CatalogDomain(localIP)
	if err != nil {
		return err
	}
	var domainID int64
	var zoneType, account string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, UPPER(type), COALESCE(account, '')
		FROM domains WHERE name = ? COLLATE NOCASE
	`, domain).Scan(&domainID, &zoneType, &account); err != nil {
		return err
	}
	if zoneType != "PRODUCER" || account != pdnsBINDCatalogAccount {
		return errors.New("PowerDNS producer row is not exact panel authority")
	}
	if _, err := verifyPDNSProducerBaseTx(
		ctx, tx, domainID, domain, localIP,
	); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name FROM domains WHERE catalog = ? COLLATE NOCASE
		ORDER BY name COLLATE BINARY
	`, domain)
	if err != nil {
		return err
	}
	actual := make([]string, 0)
	for rows.Next() {
		var member string
		if err := rows.Scan(&member); err != nil {
			rows.Close()
			return err
		}
		actual = append(actual, member)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	expected := append([]string(nil), expectedMembers...)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("PowerDNS producer membership differs from the switch manifest")
	}
	return nil
}

func verifyManagedPDNSBINDCatalogLive(ctx context.Context) error {
	return verifyManagedPDNSBINDCatalog(ctx, false)
}

func verifyManagedPDNSBINDCatalogPeer(ctx context.Context) error {
	return verifyManagedPDNSBINDCatalog(ctx, true)
}
