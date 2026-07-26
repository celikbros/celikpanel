package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A paired PowerDNS node can be primary and secondary at the same time because
// those roles belong to zones, not whole machines. Locally managed zones are
// MASTER; zones announced by the configured peer arrive as SLAVE through
// autoprimary. Both machines therefore serve every zone without a panel-to-
// panel account or API channel.
//
// Eşlenmiş PowerDNS düğümü aynı anda birincil ve ikincil olabilir; çünkü bu
// roller makineye değil zone'a aittir. Yerelde yönetilen zone'lar MASTER olur,
// yapılandırılmış eşin bildirdikleri autoprimary üzerinden SLAVE gelir. Böylece
// panelden panele hesap ya da API kanalı olmadan iki makine de her zone'u sunar.

const (
	dnsRoleStandalone = "standalone"
	dnsRolePaired     = "paired"
	dnsRolePrimary    = "primary"
	dnsRoleSecondary  = "secondary"
)

var (
	dnsClusterConf = "/etc/powerdns/pdns.d/celikpanel-cluster.conf"

	dnsClusterLookPath = exec.LookPath
	dnsClusterRestart  = func() ([]byte, error) {
		return exec.Command("systemctl", "restart", "pdns").CombinedOutput()
	}
	dnsClusterRetrieve = func(zone string) ([]byte, error) {
		return exec.Command("pdns_control", "retrieve", zone).CombinedOutput()
	}
	dnsClusterPurge = func(zone string) ([]byte, error) {
		return exec.Command("pdns_control", "purge", zone+"$").CombinedOutput()
	}
	dnsClusterApplyAutoprimaryTx = applyAutoprimaryTx
	dnsClusterSetLocalZoneTypeTx = setLocalZoneTypeTx
)

type DNSClusterRequest struct {
	Role string `json:"role"` // standalone | paired (legacy primary/secondary migrate)
	// PeerIP is both the NOTIFY/autoprimary trust source and the only address
	// allowed to AXFR local zones.
	PeerIP string `json:"peer_ip"`
	// PeerNS is the peer's nameserver host name. PowerDNS records it with the
	// autoprimary entry and stamps it into zones it auto-creates.
	// PeerNS, eşin ad sunucusu makine adıdır. PowerDNS onu otomatik-birincil
	// kaydıyla saklar ve kendiliğinden oluşturduğu zone'lara işler.
	PeerNS string `json:"peer_ns"`
}

type DNSClusterResponse struct {
	Applied bool   `json:"applied"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

func normalizeAgentDNSRole(role string) string {
	switch role {
	case dnsRolePaired, dnsRolePrimary, dnsRoleSecondary:
		return dnsRolePaired
	case dnsRoleStandalone:
		return dnsRoleStandalone
	default:
		return ""
	}
}

func dnsClusterConfig(req *DNSClusterRequest) string {
	if req.Role != dnsRolePaired {
		return ""
	}
	return fmt.Sprintf(`# Managed by CelikPanel - do not edit by hand / elle duzenlemeyin
# DNS pair: this server owns local zones and keeps secondary copies from %s.
# DNS cifti: bu sunucu yerel zonelarin sahibi, %s uzerinden gelenlerin ikincilidir.
primary=yes
secondary=yes
autosecondary=yes
allow-axfr-ips=%s
also-notify=%s
`, req.PeerIP, req.PeerIP, req.PeerIP, req.PeerIP)
}

func (a *Agent) ConfigureDNSCluster(req *DNSClusterRequest, resp *DNSClusterResponse) error {
	if _, err := dnsClusterLookPath("pdns_server"); err != nil {
		resp.Error = "PowerDNS is not installed on this server"
		return nil
	}
	// Migrate the first machine-wide primary/secondary model. PowerDNS roles
	// belong to zones, so a paired server can own its local zones while keeping
	// secondary copies of zones created on its peer.
	req.Role = normalizeAgentDNSRole(req.Role)
	switch req.Role {
	case dnsRoleStandalone, dnsRolePaired:
	default:
		resp.Error = "role must be standalone or paired"
		return nil
	}
	if req.Role != dnsRoleStandalone {
		if net.ParseIP(req.PeerIP) == nil {
			resp.Error = "the other server's IP address is required"
			return nil
		}
		if req.PeerNS == "" {
			resp.Error = "the other server's nameserver name is required"
			return nil
		}
	}

	// Keep the old drop-in so a PowerDNS that refuses to start can be undone —
	// the same write→validate→roll back rule the config editor learned the hard
	// way. A DNS server that will not start takes every hosted domain with it.
	// Eski drop-in'i sakla ki başlamayı reddeden bir PowerDNS geri alınabilsin —
	// yapılandırma editörünün zor yoldan öğrendiği yaz→doğrula→geri al kuralının
	// aynısı. Başlamayan bir DNS sunucusu, barındırılan her alan adını yanında
	// götürür.
	previous, hadPrevious := []byte(nil), false
	if b, err := os.ReadFile(dnsClusterConf); err == nil {
		previous, hadPrevious = b, true
	}
	restoreConfig := func(restart bool) error {
		var restoreErr error
		if hadPrevious {
			if err := os.WriteFile(dnsClusterConf, previous, 0o644); err != nil {
				restoreErr = fmt.Errorf("restore cluster configuration: %w", err)
			}
		} else {
			if err := os.Remove(dnsClusterConf); err != nil && !os.IsNotExist(err) {
				restoreErr = fmt.Errorf("remove cluster configuration: %w", err)
			}
		}
		if restart {
			if out, err := dnsClusterRestart(); err != nil {
				restartErr := fmt.Errorf("restart restored PowerDNS configuration: %s", firstLine(string(out)))
				if restoreErr != nil {
					return fmt.Errorf("%v; %w", restoreErr, restartErr)
				}
				return restartErr
			}
		}
		return restoreErr
	}

	db, err := openPdnsDB()
	if err != nil {
		resp.Error = fmt.Sprintf("powerdns database: %v", err)
		return nil
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		resp.Error = fmt.Sprintf("powerdns database: %v", err)
		return nil
	}
	defer tx.Rollback()
	fail := func(problem error, restart bool) {
		_ = tx.Rollback()
		if rollbackErr := restoreConfig(restart); rollbackErr != nil {
			resp.Error = fmt.Sprintf("%v; rollback failed: %v", problem, rollbackErr)
			return
		}
		resp.Error = problem.Error()
	}

	conf := dnsClusterConfig(req)

	if conf == "" {
		if err := os.Remove(dnsClusterConf); err != nil && !os.IsNotExist(err) {
			fail(err, false)
			return nil
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dnsClusterConf), 0o755); err != nil {
			fail(err, false)
			return nil
		}
		if err := os.WriteFile(dnsClusterConf, []byte(conf), 0o644); err != nil {
			fail(err, false)
			return nil
		}
		_ = os.Chmod(dnsClusterConf, 0o644)
	}

	if err := dnsClusterApplyAutoprimaryTx(tx, req); err != nil {
		fail(err, false)
		return nil
	}
	peerZones, err := dnsClusterSetLocalZoneTypeTx(tx, req)
	if err != nil {
		fail(err, false)
		return nil
	}

	if out, err := dnsClusterRestart(); err != nil {
		fail(fmt.Errorf("PowerDNS refused the new configuration and it was rolled back: %s", firstLine(string(out))), true)
		return nil
	}
	if err := tx.Commit(); err != nil {
		fail(fmt.Errorf("commit powerdns database: %w", err), true)
		return nil
	}

	resp.Applied = true
	switch req.Role {
	case dnsRolePaired:
		resp.Detail = "DNS pairing enabled; local zones are copied to " + req.PeerIP + " and peer zones are copied here"
	default:
		resp.Detail = "this server serves DNS on its own"
	}
	var refreshFailures []string
	for _, zone := range peerZones {
		var out []byte
		var err error
		if req.Role == dnsRoleStandalone {
			out, err = dnsClusterPurge(zone)
		} else {
			out, err = dnsClusterRetrieve(zone)
		}
		if err != nil {
			detail := firstLine(string(out))
			if detail == "" {
				detail = err.Error()
			}
			refreshFailures = append(refreshFailures, zone+": "+detail)
		}
	}
	if len(refreshFailures) > 0 {
		resp.Detail += "; DNS cache/peer refresh needs attention for " + strings.Join(refreshFailures, ", ")
	}
	return nil
}

// applyAutoprimary maintains the secondary's list of trusted primaries. The
// table is PowerDNS's own (`supermasters`), so nothing here invents a schema.
// applyAutoprimary, ikincilin güvendiği birincillerin listesini tutar. Tablo
// PowerDNS'in kendisinindir (`supermasters`); burada şema uydurulmaz.
func applyAutoprimary(req *DNSClusterRequest) error {
	db, err := openPdnsDB()
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	defer tx.Rollback()
	if err := applyAutoprimaryTx(tx, req); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	return nil
}

func applyAutoprimaryTx(tx *sql.Tx, req *DNSClusterRequest) error {
	if _, err := tx.Exec(`DELETE FROM supermasters WHERE account = 'celikpanel'`); err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	if req.Role != dnsRolePaired && req.Role != dnsRoleSecondary {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO supermasters (ip, nameserver, account) VALUES (?, ?, 'celikpanel')`,
		req.PeerIP, strings.TrimSuffix(req.PeerNS, "."))
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	return nil
}

// setLocalZoneType switches the zones this server already holds between NATIVE
// (nobody replicates) and MASTER (this server notifies a secondary). A
// secondary's zones are created by PowerDNS itself as SLAVE. Only zones marked
// with the CelikPanel account are retargeted or removed as pairing changes;
// manually managed secondaries are left alone.
// setLocalZoneType, bu sunucunun hâlihazırda tuttuğu zone'ları NATIVE (kimse
// çoğaltmıyor) ile MASTER (bu sunucu bir ikincile haber veriyor) arasında
// değiştirir. İkincilin zone'larını PowerDNS kendisi SLAVE olarak oluşturur ve
// onlara dokunulmaz.
func setLocalZoneTypeTx(tx *sql.Tx, req *DNSClusterRequest) ([]string, error) {
	switch req.Role {
	case dnsRolePaired, dnsRolePrimary:
		if _, err := tx.Exec(`UPDATE domains SET type = 'MASTER', master = NULL WHERE UPPER(type) = 'NATIVE'`); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		rows, err := tx.Query(`
			SELECT name
			FROM domains
			WHERE account = 'celikpanel'
			  AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			  AND COALESCE(master, '') <> ?
			ORDER BY name`, req.PeerIP)
		if err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		var changed []string
		for rows.Next() {
			var zone string
			if err := rows.Scan(&zone); err != nil {
				rows.Close()
				return nil, fmt.Errorf("powerdns database: %w", err)
			}
			changed = append(changed, zone)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		if _, err := tx.Exec(`
			UPDATE domains
			SET master = ?, last_check = NULL
			WHERE account = 'celikpanel'
			  AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			  AND COALESCE(master, '') <> ?`, req.PeerIP, req.PeerIP); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		return changed, nil
	case dnsRoleStandalone:
		if _, err := tx.Exec(`UPDATE domains SET type = 'NATIVE', master = NULL WHERE UPPER(type) = 'MASTER'`); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		rows, err := tx.Query(`
			SELECT name
			FROM domains
			WHERE account = 'celikpanel' AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			ORDER BY name`)
		if err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		var removed []string
		for rows.Next() {
			var zone string
			if err := rows.Scan(&zone); err != nil {
				rows.Close()
				return nil, fmt.Errorf("powerdns database: %w", err)
			}
			removed = append(removed, zone)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		for _, table := range []string{"records", "comments", "domainmetadata", "cryptokeys"} {
			if _, err := tx.Exec(`DELETE FROM ` + table + ` WHERE domain_id IN (
				SELECT id FROM domains
				WHERE account = 'celikpanel' AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			)`); err != nil {
				return nil, fmt.Errorf("powerdns database: %w", err)
			}
		}
		if _, err := tx.Exec(`
			DELETE FROM domains
			WHERE account = 'celikpanel' AND UPPER(type) IN ('SLAVE', 'SECONDARY')`); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		return removed, nil
	default:
		return nil, nil
	}
}
