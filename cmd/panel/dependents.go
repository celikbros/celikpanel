package main

import (
	"context"
	"database/sql"
	"fmt"
)

// Deletion protection (B3d). One question, asked before anything is removed:
// WHO breaks if this goes away? The answers come from the live database —
// there is deliberately NO site_runtimes ledger table: sites.php_version and
// sites.runtime_version already record the facts, and a second copy would be
// the exact "same knowledge, two owners" disease B3 exists to cure. The
// ledger is a query, not a table.
//
// Silme koruması (B3d). Her kaldırmadan önce tek soru sorulur: bu giderse
// KİM kırılır? Cevaplar canlı veritabanından gelir — bilerek site_runtimes
// diye bir defter TABLOSU yok: sites.php_version ile sites.runtime_version
// gerçeği zaten kaydediyor; ikinci bir kopya, B3'ün tedavi etmeye çalıştığı
// "aynı bilgi, iki sahip" hastalığının ta kendisi olurdu. Defter tablo değil,
// sorgudur.
//
// D-014's rider applies: the refusal must count WHO blocks. Today only sites
// can block; when plans start offering versions (the chain's missing link),
// plan counts join these queries — noted here so the extension point is
// written down, not rediscovered.
// D-014'ün şerhi geçerli: ret, KİMİN engellediğini saymalı. Bugün yalnız
// siteler engelleyebilir; planlar sürüm sunmaya başlayınca (zincirin eksik
// halkası) plan sayıları da bu sorgulara katılır — uzatma noktası burada
// yazılı dursun, yeniden keşfedilmesin.

// blockerCap: how many example lines a refusal carries. Enough to act on,
// small enough to read; the rest is "+N more".
// blockerCap: bir retin taşıdığı örnek satır sayısı. Eyleme yetecek kadar
// çok, okunacak kadar az; kalanı "+N tane daha".
const blockerCap = 10

// siteLabelQuery joins a sites row to its domain and owner so a refusal can
// say "example.com (ali)" instead of "site 17". Sites store neither a name
// nor an owner; the chain sites→domains→subscriptions→users is the only
// honest source of a label.
// siteLabelQuery, bir sites satırını domain'i ve sahibiyle birleştirir;
// böylece ret "site 17" değil "example.com (ali)" diyebilir. Site tablosunda
// ad da sahip de yok; tek dürüst etiket kaynağı
// sites→domains→subscriptions→users zinciridir.
const siteLabelQuery = `
	SELECT d.name, u.username
	FROM sites s
	JOIN domains d ON s.domain_id = d.id
	JOIN subscriptions sub ON d.subscription_id = sub.id
	JOIN users u ON sub.owner_id = u.id
	WHERE %s
	ORDER BY d.name`

// collectBlockers runs a label query and folds the rows into display lines,
// capped with an honest "+N more" tail (never silent truncation).
// collectBlockers bir etiket sorgusunu koşturur, satırları görüntü
// satırlarına katlar; sınırı dürüst "+N tane daha" kuyruğuyla koyar (asla
// sessiz kesme).
func collectBlockers(ctx context.Context, db *sql.DB, where string, args ...any) (int, []string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(siteLabelQuery, where), args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	count := 0
	lines := []string{}
	for rows.Next() {
		var domain, owner string
		if rows.Scan(&domain, &owner) != nil {
			continue
		}
		count++
		if count <= blockerCap {
			lines = append(lines, fmt.Sprintf("%s (%s)", domain, owner))
		}
	}
	if count > blockerCap {
		lines = append(lines, fmt.Sprintf("+%d", count-blockerCap))
	}
	return count, lines, rows.Err()
}

// runtimeVersionBlockers: the sites that run on ONE specific version — the
// RUNTIME_IN_USE evidence.
//
// PHP filter notes (field-verified schema quirks, do not "simplify"):
//   - COALESCE(project_type,'php'): pre-005 rows have NULL and mean php.
//   - project_type must be checked: a node/static/dnsonly site still CARRIES
//     a php_version column value (legacy '8.2' default, import fixtures),
//     but does not USE PHP — counting it would block removals with ghosts.
//
// Node: runtime_version is NULL or empty for the legacy "system node" rows; those
// never pin a specific tarball version, so they cannot block one.
//
// runtimeVersionBlockers: TEK bir sürümün üstünde koşan siteler —
// RUNTIME_IN_USE'un kanıtı.
//
// PHP süzgeç notları (sahada doğrulanmış şema tuhaflıkları, "sadeleştirme"):
//   - COALESCE(project_type,'php'): 005 öncesi satırlar NULL'dır ve php demektir.
//   - project_type şart: node/static/dnsonly site de php_version DEĞERİ taşır
//     (eski '8.2' varsayılanı, import kalıntıları) ama PHP KULLANMAZ —
//     sayılsaydı kaldırmaları hayaletler engellerdi.
//
// Node: eski "sistem node'u" satırlarında runtime_version NULL veya boştur; belirli
// bir tarball sürümüne bağlanmazlar, dolayısıyla birini engelleyemezler.
func runtimeVersionBlockers(ctx context.Context, db *sql.DB, serviceID, version string) (int, []string, error) {
	switch serviceID {
	case "php-fpm":
		return collectBlockers(ctx, db,
			`COALESCE(s.project_type, 'php') = 'php' AND s.php_version = ?`, version)
	case "node":
		return collectBlockers(ctx, db,
			`s.project_type = 'node' AND s.runtime_version = ?`, version)
	}
	return 0, nil, nil
}

// serviceDependents: what breaks if the WHOLE component goes — the
// SERVICE_HAS_DEPENDENTS evidence. Each case states its reason in one line;
// a service not listed here has no dependents the panel can know about, and
// inventing a guard for it would be theater.
// serviceDependents: bileşen BÜTÜNÜYLE giderse ne kırılır —
// SERVICE_HAS_DEPENDENTS'ın kanıtı. Her durum gerekçesini tek satırda söyler;
// burada olmayan servisin panelin bilebileceği bağımlısı yoktur, ona bekçi
// uydurmak tiyatro olurdu.
func serviceDependents(ctx context.Context, db *sql.DB, serviceID string) (int, []string, error) {
	switch serviceID {
	case "php-fpm":
		// Any PHP site, any version. / Herhangi bir sürümde herhangi bir PHP sitesi.
		return collectBlockers(ctx, db, `COALESCE(s.project_type, 'php') = 'php'`)

	case "node":
		// Node sites — including the legacy system-node rows (NULL/''
		// runtime_version): they too stop working if node goes entirely.
		// Node siteleri — eski sistem-node satırları (NULL/'' runtime_version)
		// dahil: node bütünüyle giderse onlar da durur.
		return collectBlockers(ctx, db, `s.project_type = 'node'`)

	case "nginx", "apache":
		// Every site except pure-DNS domains needs the web server: static and
		// php are served by it, node/proxy/forwarding live behind it as a
		// reverse proxy / redirect vhost.
		// Salt-DNS domain'ler dışında her site web sunucusuna muhtaç: statik
		// ve php onunla sunulur; node/proxy/forwarding onun arkasında ters
		// vekil / yönlendirme vhost'u olarak yaşar.
		return collectBlockers(ctx, db, `COALESCE(s.project_type, 'php') != 'dnsonly'`)

	case "pdns", "bind":
		// D-009's mirror: while domains exist, the DNS server serving their
		// zones cannot go — every domain would silently go dark. (This
		// upgrades the old uncoded guard that lived in the handler.)
		// D-009'un aynası: domain'ler varken zone'larını sunan DNS gidemez —
		// her domain sessizce kararırdı. (Handler'daki eski kodsuz bekçinin
		// yükseltilmiş hâli.)
		count := 0
		lines := []string{}
		rows, err := db.QueryContext(ctx, `
			SELECT d.name, u.username
			FROM domains d
			JOIN subscriptions sub ON d.subscription_id = sub.id
			JOIN users u ON sub.owner_id = u.id
			ORDER BY d.name`)
		if err != nil {
			return 0, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var domain, owner string
			if rows.Scan(&domain, &owner) != nil {
				continue
			}
			count++
			if count <= blockerCap {
				lines = append(lines, fmt.Sprintf("%s (%s)", domain, owner))
			}
		}
		if count > blockerCap {
			lines = append(lines, fmt.Sprintf("+%d", count-blockerCap))
		}
		return count, lines, rows.Err()

	case "postfix", "dovecot":
		// Mailboxes and forwardings die with the mail stack. There is no
		// mail_domains table; both hang off domains directly.
		// Posta kutuları ve yönlendirmeler posta yığınıyla ölür. mail_domains
		// tablosu yok; ikisi de doğrudan domains'e bağlı.
		count := 0
		lines := []string{}
		rows, err := db.QueryContext(ctx, `
			SELECT a.address FROM email_accounts a
			UNION ALL
			SELECT f.source FROM email_forwardings f
			ORDER BY 1`)
		if err != nil {
			return 0, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var addr string
			if rows.Scan(&addr) != nil {
				continue
			}
			count++
			if count <= blockerCap {
				lines = append(lines, addr)
			}
		}
		if count > blockerCap {
			lines = append(lines, fmt.Sprintf("+%d", count-blockerCap))
		}
		return count, lines, rows.Err()

	case "mariadb", "postgresql":
		// Engine identity: databases_v2 → database_servers → type name. The
		// legacy `databases` table is counted too (its db_type has synonyms —
		// postgres/mysql — matching dbDriverTypeFor's mapping).
		// Motor kimliği: databases_v2 → database_servers → tip adı. Eski
		// `databases` tablosu da sayılır (db_type eşanlamlıları —
		// postgres/mysql — dbDriverTypeFor'un eşlemesiyle aynı).
		synonym := map[string]string{"mariadb": "mysql", "postgresql": "postgres"}[serviceID]
		count := 0
		lines := []string{}
		rows, err := db.QueryContext(ctx, `
			SELECT db.name FROM databases_v2 db
			JOIN database_servers ds ON db.server_id = ds.id
			JOIN database_server_types dst ON ds.type_id = dst.id
			WHERE dst.name = ?
			UNION ALL
			SELECT name FROM databases WHERE LOWER(db_type) IN (?, ?)
			ORDER BY 1`, serviceID, serviceID, synonym)
		if err != nil {
			return 0, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if rows.Scan(&name) != nil {
				continue
			}
			count++
			if count <= blockerCap {
				lines = append(lines, name)
			}
		}
		if count > blockerCap {
			lines = append(lines, fmt.Sprintf("+%d", count-blockerCap))
		}
		return count, lines, rows.Err()
	}
	return 0, nil, nil
}
