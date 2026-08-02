package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ensureInstalledDBServers registers the database engines that are actually
// installed on the machine (MariaDB, PostgreSQL) so the user never has to
// "add a database server" by hand — a friction Plesk imposes but we don't.
// It is idempotent: the UNIQUE(subscription, host, port) constraint plus
// INSERT OR IGNORE make repeated calls a no-op.
//
// ensureInstalledDBServers, makinede gerçekten kurulu olan veritabanı
// motorlarını (MariaDB, PostgreSQL) kaydeder; böylece kullanıcı asla elle
// "veritabanı sunucusu ekle" demek zorunda kalmaz — Plesk'in dayattığı ama
// bizim dayatmadığımız bir sürtünme. İşlem idempotenttir.
func (p *Panel) ensureInstalledDBServers(ctx context.Context, subscriptionID int) error {
	if p == nil || p.db == nil {
		return fmt.Errorf("database server autodiscovery: panel database is unavailable")
	}
	if subscriptionID <= 0 {
		return fmt.Errorf("database server autodiscovery: invalid subscription %d", subscriptionID)
	}

	var allServices []core.Service
	if err := p.callAgentContext(ctx, "Agent.GetServices", &transport.Empty{}, &allServices); err != nil {
		return fmt.Errorf("database server autodiscovery: list installed services: %w", err)
	}
	return reconcileInstalledDBServers(ctx, p.db.GetDB(), subscriptionID, allServices)
}

func reconcileInstalledDBServers(
	ctx context.Context,
	db *sql.DB,
	subscriptionID int,
	allServices []core.Service,
) error {
	if db == nil {
		return fmt.Errorf("database server autodiscovery: database is unavailable")
	}

	// Map an installed engine's type name to its detected systemd unit.
	// Kurulu bir motorun tip adını, tespit edilen systemd unit'ine eşle.
	detected := map[string]core.Service{}
	for _, svc := range allServices {
		name := strings.ToLower(svc.Name)
		switch {
		case strings.HasPrefix(name, "postgresql"):
			if _, ok := detected["postgresql"]; !ok {
				detected["postgresql"] = svc
			}
		case strings.HasPrefix(name, "mariadb"), strings.HasPrefix(name, "mysql"):
			if _, ok := detected["mariadb"]; !ok {
				detected["mariadb"] = svc
			}
		}
	}
	if len(detected) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database server autodiscovery: begin reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Which engine types are already registered for this subscription?
	// Bu abonelik için hangi motor tipleri zaten kayıtlı?
	existing := map[string]bool{}
	rows, err := tx.QueryContext(ctx,
		`SELECT dst.name FROM database_servers ds
		 JOIN database_server_types dst ON ds.type_id = dst.id
		 WHERE ds.subscription_id = ?`, subscriptionID)
	if err != nil {
		return fmt.Errorf("database server autodiscovery: list registered engines: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return fmt.Errorf("database server autodiscovery: read registered engine: %w", err)
		}
		existing[strings.ToLower(strings.TrimSpace(name))] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("database server autodiscovery: iterate registered engines: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("database server autodiscovery: close registered engine rows: %w", err)
	}

	typeNames := make([]string, 0, len(detected))
	for typeName := range detected {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)

	for _, typeName := range typeNames {
		svc := detected[typeName]
		if existing[typeName] {
			continue
		}
		var typeID, port int
		var displayName string
		if err := tx.QueryRowContext(ctx,
			`SELECT id, display_name, default_port FROM database_server_types WHERE name = ?`, typeName,
		).Scan(&typeID, &displayName, &port); err != nil {
			return fmt.Errorf("database server autodiscovery: load %s engine metadata: %w", typeName, err)
		}
		result, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO database_servers
			 (subscription_id, type_id, name, version, host, port, is_default, status)
			 VALUES (?, ?, ?, ?, 'localhost', ?, 0, 'active')`,
			subscriptionID, typeID, displayName, svc.Version, port)
		if err != nil {
			return fmt.Errorf("database server autodiscovery: register %s engine: %w", typeName, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("database server autodiscovery: verify %s registration: %w", typeName, err)
		}
		if affected == 0 {
			var registered bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS(
					SELECT 1
					FROM database_servers ds
					JOIN database_server_types dst ON dst.id = ds.type_id
					WHERE ds.subscription_id = ? AND dst.name = ?
				)`, subscriptionID, typeName).Scan(&registered); err != nil {
				return fmt.Errorf("database server autodiscovery: verify ignored %s registration: %w", typeName, err)
			}
			if !registered {
				return fmt.Errorf(
					"database server autodiscovery: %s conflicts with existing localhost:%d metadata",
					typeName,
					port,
				)
			}
		}
	}

	// Guarantee exactly one default: promote the lowest-id server if none
	// is marked default yet.
	// Tam olarak bir varsayılan garanti et: hiçbiri varsayılan değilse en
	// düşük kimlikli sunucuyu yükselt.
	if _, err := tx.ExecContext(ctx,
		`UPDATE database_servers SET is_default = 1
		 WHERE subscription_id = ?
		   AND id = (SELECT MIN(id) FROM database_servers WHERE subscription_id = ?)
		   AND NOT EXISTS (SELECT 1 FROM database_servers WHERE subscription_id = ? AND is_default = 1)`,
		subscriptionID, subscriptionID, subscriptionID); err != nil {
		return fmt.Errorf("database server autodiscovery: select default engine: %w", err)
	}

	var defaultCount int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM database_servers WHERE subscription_id = ? AND is_default = 1`,
		subscriptionID,
	).Scan(&defaultCount); err != nil {
		return fmt.Errorf("database server autodiscovery: verify default engine: %w", err)
	}
	if defaultCount != 1 {
		return fmt.Errorf(
			"database server autodiscovery: subscription %d has %d default engines; expected exactly one",
			subscriptionID,
			defaultCount,
		)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("database server autodiscovery: commit reconciliation: %w", err)
	}
	return nil
}
