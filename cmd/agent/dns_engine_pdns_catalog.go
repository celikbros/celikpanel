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
	Domain  string
	LocalIP string
	PeerIP  string
	Serial  uint32
	Members []string
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
	if tx == nil {
		return managedPDNSCatalog{}, errors.New("PowerDNS catalog transaction is required")
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
	_, records, err := binddns.CatalogZoneRecords(localIP, 1, nil)
	if err != nil {
		return managedPDNSCatalog{}, err
	}
	if !exists {
		for _, record := range records {
			if record.Type != "SOA" && record.Type != "NS" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO records
				(domain_id, name, type, content, ttl, prio, disabled, auth)
				VALUES (?, ?, ?, ?, ?, ?, 0, 1)
			`, domainID, record.Name, record.Type, record.Content,
				record.TTL, record.Prio); err != nil {
				return managedPDNSCatalog{}, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE domains SET catalog = NULL
		WHERE catalog = ? AND UPPER(type) NOT IN ('PRODUCER','CONSUMER')
	`, domain); err != nil {
		return managedPDNSCatalog{}, err
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
		Domain: domain, LocalIP: localIP, Serial: 1, Members: members,
	}, nil
}

func readPDNSBINDCatalogRecordsTx(
	ctx context.Context,
	tx *sql.Tx,
	domainID int64,
	domain string,
) ([]transport.ZoneRecord, uint32, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, content, ttl, prio, disabled
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
		var disabled int
		if err := rows.Scan(
			&record.Name, &record.Type, &record.Content,
			&record.TTL, &record.Prio, &disabled,
		); err != nil {
			return nil, 0, err
		}
		if disabled != 0 {
			return nil, 0, errors.New("PowerDNS catalog contains a disabled record")
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
	identity, enabled, err := managedPDNSCatalogIdentity()
	if err != nil {
		return managedPDNSCatalog{}, false, err
	}
	result, err := reconcilePDNSBINDCatalogTx(
		ctx, tx, enabled, identity.LocalIP,
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
	identity, enabled, err := managedPDNSCatalogIdentity()
	if err != nil || !enabled {
		return err
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var domainID int64
	var account, zoneType string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, COALESCE(account, ''), type FROM domains WHERE name = ?
	`, identity.Domain).Scan(&domainID, &account, &zoneType); err != nil {
		return err
	}
	if account != pdnsBINDCatalogAccount || strings.ToUpper(zoneType) != "PRODUCER" {
		return errors.New("PowerDNS live catalog is not panel owned")
	}
	if err := verifyPDNSProducerBaseTx(ctx, tx, domainID, identity.Domain); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name FROM domains WHERE catalog = ? COLLATE NOCASE
		ORDER BY name COLLATE BINARY
	`, identity.Domain)
	if err != nil {
		return err
	}
	members := make([]string, 0)
	for rows.Next() {
		var member string
		if err := rows.Scan(&member); err != nil {
			rows.Close()
			return err
		}
		members = append(members, member)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := dnsClusterPurge(ctx, identity.Domain); err != nil {
		return errors.New("PowerDNS catalog cache purge failed")
	}
	live, err := probeDNSCatalogAXFR(ctx, identity.LocalIP, identity.Domain)
	if err != nil || live.Serial == 0 || !reflect.DeepEqual(live.Members, members) {
		return errors.New("PowerDNS live catalog differs from its database")
	}
	if requirePeer {
		if err := waitForExactBINDPairZoneSet(
			ctx, identity.LocalIP, identity.PeerIP, members, probeDNSZoneSOA,
		); err != nil {
			return errors.New("PowerDNS primary zones did not converge on the paired peer")
		}
	}
	return nil
}

func verifyPDNSProducerBaseTx(
	ctx context.Context,
	tx *sql.Tx,
	domainID int64,
	domain string,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, content, ttl, prio, disabled
		FROM records WHERE domain_id = ?
		ORDER BY name COLLATE BINARY, type COLLATE BINARY, content COLLATE BINARY
	`, domainID)
	if err != nil {
		return err
	}
	defer rows.Close()
	types := map[string]int{}
	for rows.Next() {
		var record transport.ZoneRecord
		var disabled int
		if err := rows.Scan(
			&record.Name, &record.Type, &record.Content,
			&record.TTL, &record.Prio, &disabled,
		); err != nil {
			return err
		}
		if record.Name != domain || disabled != 0 ||
			(record.Type != "SOA" && record.Type != "NS") {
			return errors.New("PowerDNS producer contains noncanonical base records")
		}
		types[record.Type]++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if types["SOA"] != 1 || types["NS"] != 1 || len(types) != 2 {
		return errors.New("PowerDNS producer has no exact SOA and NS base")
	}
	for _, table := range []string{"comments", "cryptokeys"} {
		var count int
		if err := tx.QueryRowContext(
			ctx, "SELECT COUNT(*) FROM "+table+" WHERE domain_id = ?", domainID,
		).Scan(&count); err != nil || count != 0 {
			if err == nil {
				err = errors.New("PowerDNS producer contains unexpected side data")
			}
			return err
		}
	}
	return nil
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
	if err := verifyPDNSProducerBaseTx(ctx, tx, domainID, domain); err != nil {
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
