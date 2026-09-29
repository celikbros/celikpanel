package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A fresh PowerDNS paired secondary has no prior database: its rollback
// removes the live database. Before, the rollback accepted only a live
// database that verified as the staged target - which needs the peer catalog
// over the network and, since PowerDNS 4.9 writes the consumer label into each
// consumed member's options, refused every database the started daemon had
// written (batch 5 cells c3 and c4, 2026-09-29), leaving the journal at
// rolling-back and the host with no DNS.
//
// The removal is now admitted, read-only and without the network, when the
// live database is provably the staged candidate plus what this operation's
// catalog consumer made in it, and nothing else:
//
//   - schema: the same tables, indexes and statements the candidate was
//     created with (sqlite_master compared with a freshly initialized schema);
//   - celikpanel_dns_engine_manifest_receipt: this operation's exact receipt;
//   - celikpanel_dns_zone_sync_v3_receipts and the zones they name: exactly the
//     manifest's zones, as staged (the candidate's own rows);
//   - domains: exactly one catalog consumer row as staged (name, CONSUMER,
//     master = the peer, account = the peer catalog account, no catalog and no
//     options); last_check and notified_serial are daemon-made and ignored;
//     every other row a daemon-made member: SLAVE/SECONDARY, master = the peer,
//     catalog = the consumer, account empty or the peer catalog account, named
//     by a PTR under zones.<consumer> in the database's own copy of the
//     catalog, and options empty or the exact consumer object naming that PTR's
//     label (verifyPDNSConsumedMemberOptions);
//   - records: every row belongs to one of those domains (the consumer's and
//     members' rows are what a transfer writes; the manifest zones' rows were
//     verified exactly above); no row without a domain;
//   - comments, domainmetadata, cryptokeys, tsigkeys, supermasters and the
//     legacy celikpanel_dns_zone_sync_receipts: empty, as staged.
//
// Anything else is refused and the database is kept; owner data is never
// deleted. The rollback runs this only after the target has been stopped and
// proved stopped.
//
// Taze bir PowerDNS eşli ikincilin önceki veritabanı yoktur; geri alma canlı
// veritabanını siler. Bu silme, veritabanı kanıtlanabilir biçimde hazırlanan
// aday ile bu işlemin katalog tüketicisinin eklediklerinden ibaretse kabul
// edilir; başka her durumda reddedilir ve veritabanı korunur.

// freshPDNSPairSecondaryRollbackJournal is the journal shape this admission
// applies to: a first PowerDNS install as a paired secondary, no prior engine,
// state receipt or database.
func freshPDNSPairSecondaryRollbackJournal(journal dnsEngineSwitchJournal) bool {
	return journal.TargetEngine == transport.DNSEnginePowerDNS &&
		journal.Mode == transport.DNSEngineSwitchModeSwitch &&
		journal.SourceEngine == "" && journal.SourceEpoch == 0 &&
		!journal.StateBefore.Exists && journal.PDNSBackupSHA256 == "" &&
		journal.Topology == transport.DNSTopologyPaired &&
		journal.PairRole == transport.DNSPairRoleSecondary
}

type pdnsSchemaObject struct {
	Type, Name, Table, SQL string
}

func readPDNSSchemaObjects(ctx context.Context, query interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]pdnsSchemaObject, error) {
	rows, err := query.QueryContext(ctx, `
		SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var objects []pdnsSchemaObject
	for rows.Next() {
		var object pdnsSchemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, rows.Err()
}

// expectedPDNSCandidateSchema is the schema initializePDNSEngineDB creates.
func expectedPDNSCandidateSchema(ctx context.Context) ([]pdnsSchemaObject, error) {
	db, err := sql.Open("sqlite", "file:pdns-candidate-schema?mode=memory&cache=private")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{pdnsSchema, pdnsEngineV3Schema} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return nil, err
		}
	}
	return readPDNSSchemaObjects(ctx, db)
}

// verifyFreshPDNSSecondaryRollbackDatabase admits removing the live database
// of a fresh paired-secondary rollback (see above). The error names what was
// found.
func verifyFreshPDNSSecondaryRollbackDatabase(
	ctx context.Context,
	path string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) error {
	if manifest.Topology != transport.DNSTopologyPaired ||
		manifest.PairRole != transport.DNSPairRoleSecondary {
		return errors.New("the operation is not a paired secondary")
	}
	catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		return err
	}
	expectedSchema, err := expectedPDNSCandidateSchema(ctx)
	if err != nil {
		return fmt.Errorf("build the candidate schema for comparison: %w", err)
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	liveSchema, err := readPDNSSchemaObjects(ctx, tx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(liveSchema, expectedSchema) {
		return errors.New("its schema is not the one the staged candidate was created with")
	}
	if err := verifyPDNSSwitchManifestReceiptTx(ctx, tx, manifest, binding); err != nil {
		return fmt.Errorf("its manifest receipt is not this operation's: %w", err)
	}
	if err := verifyPDNSSwitchManifestZonesTx(ctx, tx, manifest, binding); err != nil {
		return fmt.Errorf("a staged zone is not as staged: %w", err)
	}
	var receipts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+pdnsV3ReceiptTable).Scan(&receipts); err != nil {
		return err
	}
	if receipts != len(manifest.Zones) {
		return fmt.Errorf("it holds %d zone receipts; the operation staged %d", receipts, len(manifest.Zones))
	}
	for _, table := range []string{
		"comments", "domainmetadata", "cryptokeys", "tsigkeys", "supermasters",
		"celikpanel_dns_zone_sync_receipts",
	} {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("table %s holds %d rows; the staged candidate and a catalog transfer create none", table, count)
		}
	}
	labels, err := localPDNSCatalogMemberLabelsTx(ctx, tx, catalogDomain)
	if err != nil {
		return err
	}
	staged := make(map[string]bool, len(manifest.Zones))
	for _, zone := range manifest.Zones {
		if !zone.Delete {
			staged[zone.Domain] = true
		}
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, UPPER(type), COALESCE(master,''), COALESCE(account,''),
		       COALESCE(catalog,''), COALESCE(options,'')
		FROM domains ORDER BY name COLLATE BINARY
	`)
	if err != nil {
		return err
	}
	consumers := 0
	for rows.Next() {
		var name, zoneType, master, account, memberCatalog, options string
		if err := rows.Scan(&name, &zoneType, &master, &account, &memberCatalog, &options); err != nil {
			rows.Close()
			return err
		}
		switch {
		case staged[name]:
			// Verified exactly with its receipt above.
		case name == catalogDomain:
			if zoneType != "CONSUMER" || master != manifest.PeerIP ||
				account != pdnsPeerCatalogAccount || memberCatalog != "" || options != "" {
				rows.Close()
				return fmt.Errorf("the catalog consumer row %s is not the one the operation staged", name)
			}
			consumers++
		default:
			if (zoneType != "SLAVE" && zoneType != "SECONDARY") ||
				master != manifest.PeerIP || memberCatalog != catalogDomain ||
				(account != "" && account != pdnsPeerCatalogAccount) {
				rows.Close()
				return fmt.Errorf("zone %s (type %s) is not a member created by this operation's catalog consumer", name, zoneType)
			}
			label, named := labels[name]
			if !named {
				rows.Close()
				return fmt.Errorf("zone %s is not named by the local copy of catalog %s", name, catalogDomain)
			}
			if err := verifyPDNSConsumedMemberOptions(options, label); err != nil {
				rows.Close()
				return fmt.Errorf("member %s: %w", name, err)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if consumers != 1 {
		return fmt.Errorf("it holds %d catalog consumer rows for %s; the operation staged one", consumers, catalogDomain)
	}
	var orphans int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM records
		WHERE domain_id IS NULL OR domain_id NOT IN (SELECT id FROM domains)
	`).Scan(&orphans); err != nil {
		return err
	}
	if orphans != 0 {
		return fmt.Errorf("%d record rows belong to no zone", orphans)
	}
	return tx.Commit()
}

// localPDNSCatalogMemberLabelsTx reads the consumer's own copy of the catalog:
// each PTR <label>.zones.<catalog> maps its member to label.
func localPDNSCatalogMemberLabelsTx(ctx context.Context, tx *sql.Tx, catalogDomain string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.name, COALESCE(r.content,'') FROM records r JOIN domains d ON r.domain_id = d.id
		WHERE d.name = ? COLLATE BINARY AND UPPER(COALESCE(r.type,'')) = 'PTR'
	`, catalogDomain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	suffix := ".zones." + catalogDomain
	labels := map[string]string{}
	for rows.Next() {
		var owner, content string
		if err := rows.Scan(&owner, &content); err != nil {
			return nil, err
		}
		owner = strings.ToLower(strings.TrimSuffix(owner, "."))
		member := strings.ToLower(strings.TrimSuffix(content, "."))
		label := strings.TrimSuffix(owner, suffix)
		if !strings.HasSuffix(owner, suffix) || !validPDNSCatalogUniqueLabel(label) ||
			!serviceMutationCanonicalFQDN(member) {
			return nil, fmt.Errorf("the local copy of catalog %s has an unexpected member record %s", catalogDomain, owner)
		}
		if _, duplicate := labels[member]; duplicate {
			return nil, fmt.Errorf("the local copy of catalog %s names member %s twice", catalogDomain, member)
		}
		labels[member] = label
	}
	return labels, rows.Err()
}
