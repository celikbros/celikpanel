package main

import (
	"context"
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
func (p *Panel) ensureInstalledDBServers(ctx context.Context, subscriptionID int) {
	var allServices []core.Service
	if err := p.agentClient.Call("Agent.GetServices", &transport.Empty{}, &allServices); err != nil {
		return
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
		return
	}

	db := p.db.GetDB()

	// Which engine types are already registered for this subscription?
	// Bu abonelik için hangi motor tipleri zaten kayıtlı?
	existing := map[string]bool{}
	rows, err := db.QueryContext(ctx,
		`SELECT dst.name FROM database_servers ds
		 JOIN database_server_types dst ON ds.type_id = dst.id
		 WHERE ds.subscription_id = ?`, subscriptionID)
	if err == nil {
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				existing[n] = true
			}
		}
		rows.Close()
	}

	for typeName, svc := range detected {
		if existing[typeName] {
			continue
		}
		var typeID, port int
		var displayName string
		if err := db.QueryRowContext(ctx,
			`SELECT id, display_name, default_port FROM database_server_types WHERE name = ?`, typeName,
		).Scan(&typeID, &displayName, &port); err != nil {
			continue
		}
		_, _ = db.ExecContext(ctx,
			`INSERT OR IGNORE INTO database_servers
			 (subscription_id, type_id, name, version, host, port, is_default, status)
			 VALUES (?, ?, ?, ?, 'localhost', ?, 0, 'active')`,
			subscriptionID, typeID, displayName, svc.Version, port)
	}

	// Guarantee exactly one default: promote the lowest-id server if none
	// is marked default yet.
	// Tam olarak bir varsayılan garanti et: hiçbiri varsayılan değilse en
	// düşük kimlikli sunucuyu yükselt.
	_, _ = db.ExecContext(ctx,
		`UPDATE database_servers SET is_default = 1
		 WHERE subscription_id = ?
		   AND id = (SELECT MIN(id) FROM database_servers WHERE subscription_id = ?)
		   AND NOT EXISTS (SELECT 1 FROM database_servers WHERE subscription_id = ? AND is_default = 1)`,
		subscriptionID, subscriptionID, subscriptionID)
}
