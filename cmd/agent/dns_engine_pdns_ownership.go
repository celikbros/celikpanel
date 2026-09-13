package main

import (
	"context"
	"database/sql"
	"errors"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// requirePDNSV3ZoneOwnershipTx protects databases created by the V3 switch path:
// a pre-existing native zone must match its last panel receipt before sync can
// replace or delete it. The separately verified legacy adoption path does not
// use this check on its first V3 write; adoption originally stores an engine
// receipt without per-zone V3 receipts. This does not certify all owner-edit
// detection or migration behavior on adopted installations.
func requirePDNSV3ZoneOwnershipTx(ctx context.Context, tx *sql.Tx, domain string) error {
	if tx == nil {
		return errors.New("PowerDNS zone ownership requires a transaction")
	}
	receipt, found, err := readPDNSV3ReceiptTx(ctx, tx, domain)
	if err != nil {
		return err
	}
	if found {
		zoneType, records, zoneFound, err := readPDNSV3ZoneTx(ctx, tx, domain)
		if err != nil {
			return err
		}
		deleted := receipt.Action == dnsZoneSyncActionDelete
		if deleted == zoneFound || (zoneFound && zoneType != receipt.ZoneType) {
			return errors.New("PowerDNS zone presence differs from its management receipt; reconcile ownership before publishing")
		}
		if deleted {
			zoneType = receipt.ZoneType
		}
		previous, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, receipt.EngineEpoch,
			receipt.DesiredGeneration, receipt.Domain, deleted, zoneType, records,
		)
		if err != nil || previous.Qualifier != receipt.Qualifier {
			return errors.New("PowerDNS zone rows differ from its management receipt; reconcile ownership before publishing")
		}
		return nil
	}
	var domainID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM domains WHERE name = ? COLLATE NOCASE`, domain).Scan(&domainID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("PowerDNS already contains this zone without a matching management receipt; reconcile its ownership before publishing")
}
