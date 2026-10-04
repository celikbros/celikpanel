package dnsenginerecovery

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// VerifyPDNSAdoptionDatabaseTx compares an already-open read-only transaction
// with the frozen adoption manifest. The caller owns database path and native
// service admission; this pure SQL proof never creates tables or receipts.
func VerifyPDNSAdoptionDatabaseTx(ctx context.Context, tx *sql.Tx, manifest mutationpayload.DNSEngineSwitchManifestCommitment) error {
	if ctx == nil || tx == nil || manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
		manifest.SourceEngine != "" || manifest.TargetEngine != transport.DNSEnginePowerDNS {
		return errors.New("PowerDNS adoption database proof received an invalid transaction")
	}
	expected := make(map[string]transport.DNSEngineSwitchZoneSnapshot, len(manifest.Zones))
	for _, zone := range manifest.Zones {
		expected[zone.Domain] = zone
		zoneType, records, found, err := readPDNSAdoptionZoneTx(ctx, tx, zone.Domain)
		if err != nil {
			return err
		}
		if zone.Delete {
			if found {
				return errors.New("PowerDNS adoption found a ledger-deleted zone")
			}
			continue
		}
		if !found {
			return errors.New("PowerDNS adoption is missing a ledger zone")
		}
		actual, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, manifest.TargetEpoch,
			zone.DesiredGeneration, zone.Domain, false, zoneType, records,
		)
		if err != nil || actual.Qualifier != zone.ZoneQualifier ||
			actual.ZoneType != zone.ZoneType || !reflect.DeepEqual(actual.Records, zone.Records) {
			return errors.New("PowerDNS adoption zone differs from the panel ledger")
		}
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, COALESCE(master, ''), COALESCE(account, '') FROM domains
		ORDER BY name COLLATE BINARY, type COLLATE BINARY, id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	for rows.Next() {
		var name, zoneType, master, account string
		if err := rows.Scan(&name, &zoneType, &master, &account); err != nil {
			return err
		}
		if !servicemutationledger.ServiceMutationCanonicalFQDN(name) {
			return errors.New("PowerDNS adoption found a noncanonical zone name")
		}
		if _, duplicate := seen[name]; duplicate {
			return errors.New("PowerDNS adoption found duplicate zone authority")
		}
		seen[name] = struct{}{}
		if zone, listed := expected[name]; listed {
			if zone.Delete || zoneType != zone.ZoneType {
				return errors.New("PowerDNS adoption zone type differs from the panel ledger")
			}
			continue
		}
		if manifest.Topology != transport.DNSTopologyPaired ||
			(strings.ToUpper(zoneType) != "SLAVE" && strings.ToUpper(zoneType) != "SECONDARY") ||
			master != manifest.PeerIP || account != "celikpanel" {
			return errors.New("PowerDNS adoption found an unowned extra zone")
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var supermasters, exactSupermasters int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM supermasters`).Scan(&supermasters); err != nil {
		return err
	}
	if manifest.Topology == transport.DNSTopologyPaired {
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM supermasters
			WHERE ip = ? AND nameserver = ? AND account = 'celikpanel'
		`, manifest.PeerIP, manifest.PeerNS).Scan(&exactSupermasters); err != nil {
			return err
		}
		if supermasters != 1 || exactSupermasters != 1 {
			return errors.New("PowerDNS adoption autoprimary peer differs from the manifest")
		}
	} else if supermasters != 0 {
		return errors.New("PowerDNS standalone adoption found an autoprimary peer")
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		if err == nil {
			err = errors.New("PowerDNS adoption database failed quick_check")
		}
		return err
	}
	return nil
}

func readPDNSAdoptionZoneTx(ctx context.Context, tx *sql.Tx, domain string) (string, []transport.ZoneRecord, bool, error) {
	var domainID int64
	var name, zoneType string
	err := tx.QueryRowContext(ctx, `SELECT id, name, type FROM domains WHERE name = ? COLLATE NOCASE`, domain).Scan(&domainID, &name, &zoneType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	if name != domain || (zoneType != "NATIVE" && zoneType != "MASTER") {
		return "", nil, false, errors.New("PowerDNS zone row is not canonical")
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, type, content, ttl, prio, disabled FROM records WHERE domain_id = ?
		ORDER BY name COLLATE BINARY, type COLLATE BINARY, content COLLATE BINARY,
		 ttl, prio, disabled, id
	`, domainID)
	if err != nil {
		return "", nil, false, err
	}
	defer rows.Close()
	records := make([]transport.ZoneRecord, 0)
	for rows.Next() {
		var record transport.ZoneRecord
		var disabled int
		if err := rows.Scan(&record.Name, &record.Type, &record.Content, &record.TTL, &record.Prio, &disabled); err != nil {
			return "", nil, false, err
		}
		if disabled != 0 && disabled != 1 {
			return "", nil, false, errors.New("PowerDNS record has a noncanonical disabled flag")
		}
		record.Disabled = disabled == 1
		records = append(records, record)
	}
	return zoneType, records, true, rows.Err()
}
