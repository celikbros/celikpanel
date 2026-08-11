package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
	_ "modernc.org/sqlite"
)

// PowerDNS integration. The panel's ledger is never exposed to pdns —
// password hashes live there. Instead pdns gets its own standard gsqlite3
// database and the panel pushes full zones into it through this RPC on
// every change. Full-zone rewrite is idempotent: a missed sync is repaired
// by the next one.
//
// PowerDNS entegrasyonu. Panelin defteri pdns'e asla açılmaz — parola
// hash'leri orada yaşar. Bunun yerine pdns kendi standart gsqlite3
// veritabanını alır ve panel her değişiklikte tam zone'u bu RPC ile oraya
// iter. Tam-zone yazımı idempotenttir: kaçan bir senkron, sonrakiyle onarılır.

func pdnsDBPath() string {
	if p := os.Getenv("CELIKPANEL_PDNS_DB"); p != "" {
		return p
	}
	return "/var/lib/powerdns/pdns.sqlite3"
}

// pdnsUser returns the account PowerDNS runs as on this host: Debian ships
// "pdns", Arch "powerdns". The database must be owned by whichever exists.
// pdnsUser, PowerDNS'in bu makinede hangi hesapla koştuğunu döndürür: Debian
// "pdns", Arch "powerdns" getirir. Veritabanının sahibi var olan olmalı.
func pdnsUser() string {
	for _, name := range []string{"pdns", "powerdns"} {
		if _, err := user.Lookup(name); err == nil {
			return name
		}
	}
	return "pdns"
}

type ZoneRecord = transport.ZoneRecord

type SyncDNSZoneRequest = transport.SyncDNSZoneRequest

type SyncDNSZoneResponse = transport.SyncDNSZoneResponse

var dnsSyncCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return serviceMutationCommand(ctx, name, args...).CombinedOutput()
}

// SyncDNSZone replaces one zone in the pdns database with the given record
// set (or removes it entirely when Delete is set), then flushes the pdns
// cache for that name so answers change immediately.
// SyncDNSZone, pdns veritabanındaki bir zone'u verilen kayıt setiyle
// değiştirir (Delete işaretliyse tümüyle kaldırır), sonra cevaplar hemen
// değişsin diye o adın pdns önbelleğini boşaltır.
func (a *Agent) SyncDNSZone(req *SyncDNSZoneRequest, resp *SyncDNSZoneResponse) error {
	*resp = SyncDNSZoneResponse{}
	if req == nil {
		return fmt.Errorf("DNS zone sync request is required")
	}
	domain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil {
		*resp = SyncDNSZoneResponse{Error: "invalid domain"}
		return nil
	}
	deleteZone := req.Delete
	action := "sync"
	if deleteZone {
		action = "delete"
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepSyncDNSZone, domain, "", action),
	)
	if err != nil {
		*resp = SyncDNSZoneResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	authorizedReq := *req
	authorizedReq.Domain = domain
	authorizedReq.Delete = deleteZone
	return a.syncDNSZone(ctx, &authorizedReq, resp)
}

func (a *Agent) syncDNSZone(ctx context.Context, req *SyncDNSZoneRequest, resp *SyncDNSZoneResponse) error {
	if req.Domain == "" {
		resp.Error = "domain is required"
		return nil
	}

	db, err := openPdnsDB()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer tx.Rollback()

	zoneType := strings.ToUpper(strings.TrimSpace(req.ZoneType))
	if zoneType == "" {
		zoneType = "NATIVE"
	}
	if zoneType != "NATIVE" && zoneType != "MASTER" {
		resp.Error = "zone type must be NATIVE or MASTER"
		return nil
	}

	var zoneID int64
	var existingType string
	err = tx.QueryRow(`SELECT id, type FROM domains WHERE name = ?`, req.Domain).Scan(&zoneID, &existingType)
	if err == nil && (strings.EqualFold(existingType, "SLAVE") || strings.EqualFold(existingType, "SECONDARY")) {
		resp.Error = "this zone is owned by the peer and is read-only on this server"
		return nil
	}
	switch {
	case err == sql.ErrNoRows && req.Delete:
		// Already absent: deletion is idempotent.
	case err == sql.ErrNoRows:
		res, insertErr := tx.Exec(`INSERT INTO domains (name, type) VALUES (?, ?)`, req.Domain, zoneType)
		if insertErr != nil {
			resp.Error = insertErr.Error()
			return nil
		}
		zoneID, err = res.LastInsertId()
		if err != nil {
			resp.Error = fmt.Sprintf("read inserted zone identity: %v", err)
			return nil
		}
	case err != nil:
		resp.Error = err.Error()
		return nil
	case req.Delete:
		for _, table := range []string{"records", "comments", "domainmetadata", "cryptokeys"} {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE domain_id = ?`, zoneID); err != nil {
				resp.Error = err.Error()
				return nil
			}
		}
		if _, err := tx.Exec(`DELETE FROM domains WHERE id = ?`, zoneID); err != nil {
			resp.Error = err.Error()
			return nil
		}
	default:
		if _, err := tx.Exec(`UPDATE domains SET type = ?, master = NULL WHERE id = ?`, zoneType, zoneID); err != nil {
			resp.Error = err.Error()
			return nil
		}
	}

	if !req.Delete {
		if _, err := tx.Exec(`DELETE FROM records WHERE domain_id = ?`, zoneID); err != nil {
			resp.Error = err.Error()
			return nil
		}
		for _, rec := range req.Records {
			disabled := 0
			if rec.Disabled {
				disabled = 1
			}
			if _, err := tx.Exec(
				`INSERT INTO records (domain_id, name, type, content, ttl, prio, disabled, auth) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
				zoneID, rec.Name, rec.Type, rec.Content, rec.TTL, rec.Prio, disabled); err != nil {
				resp.Error = fmt.Sprintf("insert %s %s: %v", rec.Type, rec.Name, err)
				return nil
			}
		}
	}

	if err := tx.Commit(); err != nil {
		resp.Error = err.Error()
		return nil
	}

	// A full-zone rewrite clears the ordername/auth columns DNSSEC relies
	// on; rectify restores them on signed zones.
	// Tam-zone yazımı, DNSSEC'in dayandığı ordername/auth kolonlarını siler;
	// rectify imzalı zone'larda onları geri kurar.
	if zoneSecured(req.Domain) {
		if out, err := runPDNSUtil(
			[]string{"zone", "rectify", req.Domain},
			[]string{"rectify-zone", req.Domain},
		); err != nil {
			resp.Error = dnssecCommandError("rectify synced zone", out, err)
			return nil
		}
	}

	// Rectification must complete before a primary sends NOTIFY; otherwise a
	// fast secondary can AXFR a signed zone whose denial-of-existence ordering
	// is not ready yet. Cache control remains best-effort after the durable DB
	// work has succeeded.
	_, _ = dnsSyncCommand(ctx, "pdns_control", "purge", req.Domain+"$")
	if !req.Delete && zoneType == "MASTER" {
		if out, err := dnsSyncCommand(ctx, "pdns_control", "notify", req.Domain); err != nil {
			resp.Error = dnssecCommandError("notify peer", out, err)
			return nil
		}
	}

	resp.Synced = true
	return nil
}

// ConfigurePowerDNSSQLite points pdns at our dedicated sqlite database and
// restarts it. Replaces the retired PostgreSQL-era configuration path.
// ConfigurePowerDNSSQLite, pdns'i bize ayrılmış sqlite veritabanına
// yönlendirir ve yeniden başlatır. Emekli PostgreSQL-dönemi yapılandırma
// yolunun yerini alır.
func (a *Agent) ConfigurePowerDNSSQLite(req *ServiceMutationRequest, resp *SyncDNSZoneResponse) error {
	*resp = SyncDNSZoneResponse{}
	if req == nil {
		return fmt.Errorf("PowerDNS configuration request is required")
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepConfigurePowerDNSSQLite, "pdns", "", "configure"),
	)
	if err != nil {
		*resp = SyncDNSZoneResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	dbPath := pdnsDBPath()

	// Create the database with the pdns schema before pdns first reads it.
	// pdns ilk okumadan önce veritabanını pdns şemasıyla oluştur.
	db, err := openPdnsDB()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := db.Close(); err != nil {
		resp.Error = fmt.Sprintf("close initialized PowerDNS database: %v", err)
		return nil
	}

	// pdns must read and write its own database; the account name differs per
	// distro (Debian "pdns", Arch "powerdns").
	// pdns kendi veritabanını okuyup yazabilmeli; hesap adı dağıtıma göre
	// değişir (Debian "pdns", Arch "powerdns").
	owner := pdnsUser()
	if out, err := serviceMutationCommand(
		ctx,
		"chown",
		"-R",
		owner+":"+owner,
		filepath.Dir(dbPath),
	).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf(
			"set PowerDNS database ownership: %v: %s",
			err,
			firstLine(string(out)),
		)
		return nil
	}

	// zone-cache-refresh-interval=0: pdns caches the zone LIST for 300s by
	// default, so a zone created after startup would be REFUSED for up to
	// five minutes. The panel pushes changes instantly; answers must follow
	// instantly. The per-query cost is irrelevant at panel scale.
	// zone-cache-refresh-interval=0: pdns zone LİSTESİNİ varsayılan 300 sn
	// önbellekler; başlangıçtan sonra oluşturulan zone beş dakikaya dek
	// REFUSED olurdu. Panel değişiklikleri anında iter; cevaplar da anında
	// izlemeli. Sorgu başına maliyet panel ölçeğinde önemsiz.
	// Bind only to the machine's public addresses, NOT 0.0.0.0. On Ubuntu
	// systemd-resolved already holds 127.0.0.53:53; a wildcard bind collides
	// with it and pdns fails to start. Serving on the public IPs leaves the
	// stub resolver (and the server's own name resolution) untouched.
	// Yalnız makinenin genel adreslerine bağlan, 0.0.0.0'a DEĞİL. Ubuntu'da
	// systemd-resolved zaten 127.0.0.53:53'ü tutar; joker bağlanma onunla
	// çakışır ve pdns başlayamaz. Genel IP'lerde sunmak, stub çözümleyiciyi
	// (ve sunucunun kendi ad çözümlemesini) bozmadan bırakır.
	listen := publicListenAddresses()
	if listen == "" {
		listen = "0.0.0.0" // no public IP detected; fall back (dev/NAT)
	}
	config := fmt.Sprintf(`# Managed by CelikPanel — do not edit by hand / elle düzenlemeyin
launch=gsqlite3
gsqlite3-dnssec=yes
gsqlite3-database=%s
local-address=%s
zone-cache-refresh-interval=0
webserver=no
api=no
`, dbPath, listen)
	// The drop-in directory is a Debian convention; Arch ships neither the
	// directory nor an active include-dir line in the stock pdns.conf. Create
	// the one and switch on the other, so the same managed drop-in works on
	// both — caught live on Arch: the write failed with "no such file or
	// directory" and pdns crash-looped on its backendless stock config.
	// Drop-in dizini bir Debian geleneğidir; Arch ne dizini ne de stok
	// pdns.conf'ta etkin bir include-dir satırı getirir. Birini oluştur,
	// diğerini aç — Arch'ta canlıda yakalandı: yazma "no such file or
	// directory" ile düştü ve pdns backend'siz stok config'le çevrimde kaldı.
	confDir := "/etc/powerdns/pdns.d"
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}
	// The agent's UMask=0027 turns the 0755 above into 0750, and the pdns
	// user (not in root's group) cannot even traverse the directory — pdns
	// died with "pdns.d is not accessible". Explicit chmod bypasses umask,
	// same as for the drop-in file below.
	// Agent'ın UMask=0027'si yukarıdaki 0755'i 0750'ye çevirir; root'un
	// grubunda olmayan pdns kullanıcısı dizinden geçemez bile — pdns "pdns.d
	// is not accessible" ile öldü. Açık chmod, aşağıdaki drop-in dosyasında
	// olduğu gibi umask'i atlar.
	if err := os.Chmod(confDir, 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}
	mainConf := "/etc/powerdns/pdns.conf"
	if data, err := os.ReadFile(mainConf); err == nil {
		hasInclude := false
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "include-dir=") {
				hasInclude = true
				break
			}
		}
		if !hasInclude {
			addition := "\n# Managed by CelikPanel / CelikPanel yönetir\ninclude-dir=" + confDir + "\n"
			if err := os.WriteFile(mainConf, append(data, []byte(addition)...), 0o644); err != nil {
				resp.Error = err.Error()
				return nil
			}
		}
	}

	confPath := filepath.Join(confDir, "celikpanel.conf")
	if err := os.WriteFile(confPath, []byte(config), 0o644); err != nil {
		resp.Error = err.Error()
		return nil
	}
	// The agent's systemd unit sets UMask=0027 (hardening), which strips the
	// "other" read bit WriteFile asked for, leaving 0640 — unreadable by the
	// pdns user (not in the file's group). An explicit chmod bypasses umask
	// entirely. The file holds no secret (paths, listen addresses only), so
	// world-readable is the right call.
	// Agent'ın systemd unit'i UMask=0027 ayarlar (sertleştirme); bu,
	// WriteFile'ın istediği "other" okuma bitini siler, 0640 kalır — dosyanın
	// grubunda olmayan pdns kullanıcısı okuyamaz. Açık chmod umask'i atlar.
	if err := os.Chmod(confPath, 0o644); err != nil {
		resp.Error = err.Error()
		return nil
	}

	if out, err := runServiceMutationCombinedOutput(ctx, "systemctl", "restart", "pdns"); err != nil {
		resp.Error = fmt.Sprintf("pdns restart: %v: %s", err, firstLine(string(out)))
		return nil
	}

	resp.Synced = true
	return nil
}

// openPdnsDB opens (creating if needed) the dedicated pdns database and
// makes sure the official gsqlite3 schema exists.
// openPdnsDB, ayrılmış pdns veritabanını açar (gerekirse oluşturur) ve resmi
// gsqlite3 şemasının var olduğundan emin olur.
func openPdnsDB() (*sql.DB, error) {
	path := pdnsDBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(pdnsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("pdns schema: %w", err)
	}
	return db, nil
}

// The official PowerDNS 4.x gsqlite3 schema (DNSSEC tables included — the
// backend queries domainmetadata even with DNSSEC off).
// Resmi PowerDNS 4.x gsqlite3 şeması (DNSSEC tabloları dahil — backend,
// DNSSEC kapalıyken bile domainmetadata'yı sorgular).
const pdnsSchema = `
CREATE TABLE IF NOT EXISTS domains (
  id INTEGER PRIMARY KEY,
  name VARCHAR(255) NOT NULL COLLATE NOCASE,
  master VARCHAR(128) DEFAULT NULL,
  last_check INTEGER DEFAULT NULL,
  type VARCHAR(8) NOT NULL,
  notified_serial INTEGER DEFAULT NULL,
  account VARCHAR(40) DEFAULT NULL,
  options VARCHAR(65535) DEFAULT NULL,
  catalog VARCHAR(255) DEFAULT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS name_index ON domains(name);
CREATE INDEX IF NOT EXISTS catalog_idx ON domains(catalog);

CREATE TABLE IF NOT EXISTS records (
  id INTEGER PRIMARY KEY,
  domain_id INTEGER DEFAULT NULL,
  name VARCHAR(255) DEFAULT NULL,
  type VARCHAR(10) DEFAULT NULL,
  content VARCHAR(65535) DEFAULT NULL,
  ttl INTEGER DEFAULT NULL,
  prio INTEGER DEFAULT NULL,
  disabled BOOLEAN DEFAULT 0,
  ordername VARCHAR(255),
  auth BOOL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS records_lookup_idx ON records(name, type);
CREATE INDEX IF NOT EXISTS records_lookup_id_idx ON records(domain_id, name, type);
CREATE INDEX IF NOT EXISTS records_order_idx ON records(domain_id, ordername);

CREATE TABLE IF NOT EXISTS supermasters (
  ip VARCHAR(64) NOT NULL,
  nameserver VARCHAR(255) NOT NULL COLLATE NOCASE,
  account VARCHAR(40) NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS ip_nameserver_pk ON supermasters(ip, nameserver);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY,
  domain_id INTEGER NOT NULL,
  name VARCHAR(255) NOT NULL COLLATE NOCASE,
  type VARCHAR(10) NOT NULL,
  modified_at INT NOT NULL,
  account VARCHAR(40) DEFAULT NULL,
  comment VARCHAR(65535) NOT NULL
);
CREATE INDEX IF NOT EXISTS comments_idx ON comments(domain_id, name, type);
CREATE INDEX IF NOT EXISTS comments_order_idx ON comments(domain_id, modified_at);

CREATE TABLE IF NOT EXISTS domainmetadata (
  id INTEGER PRIMARY KEY,
  domain_id INT NOT NULL,
  kind VARCHAR(32) COLLATE NOCASE,
  content TEXT
);
CREATE INDEX IF NOT EXISTS domainmetadata_idx ON domainmetadata(domain_id);

CREATE TABLE IF NOT EXISTS cryptokeys (
  id INTEGER PRIMARY KEY,
  domain_id INT NOT NULL,
  flags INT NOT NULL,
  active BOOL,
  published BOOL DEFAULT 1,
  content TEXT
);
CREATE INDEX IF NOT EXISTS domainidindex ON cryptokeys(domain_id);

CREATE TABLE IF NOT EXISTS tsigkeys (
  id INTEGER PRIMARY KEY,
  name VARCHAR(255) COLLATE NOCASE,
  algorithm VARCHAR(50) COLLATE NOCASE,
  secret VARCHAR(255)
);
CREATE UNIQUE INDEX IF NOT EXISTS namealgoindex ON tsigkeys(name, algorithm);
`

// publicListenAddresses returns the machine's global (non-loopback, non-
// link-local) unicast IPs as a comma-separated list — where an authoritative
// DNS server should listen so it never fights the local stub resolver.
// publicListenAddresses, makinenin genel (loopback ve link-local olmayan)
// tekil IP'lerini virgülle ayrılmış liste olarak döndürür — yetkili bir DNS
// sunucusunun, yerel stub çözümleyiciyle çakışmadan dinlemesi gereken yer.
func publicListenAddresses() string {
	out, err := serviceMutationCommand(context.Background(), "ip", "-o", "addr", "show", "scope", "global").Output()
	if err != nil {
		return ""
	}
	var addrs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		for i := range f {
			if f[i] == "inet" || f[i] == "inet6" {
				if i+1 < len(f) {
					ip := f[i+1]
					if s := strings.IndexByte(ip, '/'); s >= 0 {
						ip = ip[:s]
					}
					if ip != "" && !seen[ip] {
						seen[ip] = true
						addrs = append(addrs, ip)
					}
				}
			}
		}
	}
	return strings.Join(addrs, ",")
}
