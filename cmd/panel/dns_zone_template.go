package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"
)

// The full default record set for a freshly hosted domain — what Plesk/cPanel
// generate on domain creation, so "add a domain, point it at us" works
// without hand-writing records: web (@ / www), mail (MX + mail host),
// self-hosted nameservers with glue, and mail-auth baselines (SPF, DMARC).
// DKIM is added by the mail-auth flow once a key exists; imported domains
// skip this template entirely because their records come from the archive.
//
// Yeni barındırılan bir domain için tam varsayılan kayıt seti — Plesk/cPanel
// domain oluştururken ne üretiyorsa o; böylece "domain ekle, bize yönlendir"
// elle kayıt yazmadan çalışır: web (@ / www), mail (MX + mail host), glue'lu
// kendi ad sunucuları ve mail-auth tabanı (SPF, DMARC). DKIM, anahtar
// üretilince mail-auth akışıyla eklenir; içe aktarılan domain'ler bu şablonu
// hiç kullanmaz çünkü kayıtları arşivden gelir.

// createZoneWithTemplate creates the pdns zone for a domain with the full
// default record set. Idempotent: an existing zone is returned untouched
// (created=false) so re-runs never duplicate or overwrite records.
// createZoneWithTemplate, bir domain'in pdns zone'unu tam varsayılan kayıt
// setiyle oluşturur. Idempotent: var olan zone'a dokunulmadan döndürülür
// (created=false); tekrar çalıştırmak kayıt çoğaltmaz, üzerine yazmaz.
func (p *Panel) createZoneWithTemplate(ctx context.Context, domain string) (int, bool, error) {
	pool := p.db.GetDB()

	var zoneID int
	if err := pool.QueryRowContext(ctx, `SELECT id FROM pdns_domains WHERE name = ?`, domain).Scan(&zoneID); err == nil {
		return zoneID, false, nil
	}

	result, err := pool.ExecContext(ctx, `INSERT INTO pdns_domains (name, type) VALUES (?, 'NATIVE')`, domain)
	if err != nil {
		return 0, false, err
	}
	id64, _ := result.LastInsertId()
	zoneID = int(id64)

	type record struct {
		name    string
		typ     string
		content string
		prio    *int
	}
	mxPrio := 10

	// Serial YYYYMMDDHH matches ensureZone; fits 32-bit SOA serial space.
	// Seri YYYYMMDDHH ensureZone ile aynı; 32-bit SOA seri alanına sığar.
	soa := fmt.Sprintf("ns1.%s hostmaster.%s %s 10800 3600 604800 3600",
		domain, domain, time.Now().Format("2006010215"))

	// NS record content is the nameserver name; both point at this panel
	// host until the user delegates elsewhere.
	// NS kaydının içeriği ad sunucusunun adıdır; kullanıcı başka yere
	// devredene dek ikisi de bu panel makinesini gösterir.
	records := []record{
		{domain, "SOA", soa, nil},
		{domain, "NS", "ns1." + domain, nil},
		{domain, "NS", "ns2." + domain, nil},
		{domain, "MX", "mail." + domain, &mxPrio},
		{"www." + domain, "CNAME", domain, nil},
		{domain, "TXT", splitTXTContent(spfRecommended()), nil},
		{"_dmarc." + domain, "TXT", splitTXTContent(dmarcRecommended(domain, "")), nil},
	}

	// Address records only when we actually know this host's address —
	// guessing an IP would serve wrong DNS, which is worse than a record the
	// user fills in. MX target and NS glue must be A/AAAA, never CNAME.
	// Adres kayıtları yalnız bu makinenin adresini gerçekten bildiğimizde —
	// IP tahmin etmek yanlış DNS sunmaktır; kullanıcının dolduracağı boş
	// kayıttan kötüdür. MX hedefi ve NS glue A/AAAA olmalı, asla CNAME değil.
	if ip4 := serverPrimaryIP(); ip4 != "" {
		for _, host := range []string{domain, "mail." + domain, "ns1." + domain, "ns2." + domain} {
			records = append(records, record{host, "A", ip4, nil})
		}
	}
	if ip6 := serverPrimaryIPv6(); ip6 != "" {
		for _, host := range []string{domain, "mail." + domain} {
			records = append(records, record{host, "AAAA", ip6, nil})
		}
	}

	for _, rec := range records {
		if _, err := pool.ExecContext(ctx,
			`INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio) VALUES (?, ?, ?, ?, 3600, ?)`,
			zoneID, rec.name, rec.typ, rec.content, rec.prio); err != nil {
			return zoneID, true, fmt.Errorf("insert %s %s: %w", rec.typ, rec.name, err)
		}
	}
	return zoneID, true, nil
}

// serverPrimaryIP reports this host's outbound IPv4 address: the explicit
// CELIKPANEL_SERVER_IP override first (NAT setups, where the interface
// address is not the public one), else the default-route source address.
// The UDP dial sends no packets; it only resolves routing.
// serverPrimaryIP, bu makinenin dışa dönük IPv4 adresini bildirir: önce açık
// CELIKPANEL_SERVER_IP değişkeni (NAT kurulumları — arayüz adresi kamusal
// adres değildir), yoksa varsayılan rotanın kaynak adresi. UDP dial paket
// göndermez; yalnız yönlendirmeyi çözer.
func serverPrimaryIP() string {
	if ip := os.Getenv("CELIKPANEL_SERVER_IP"); ip != "" {
		return ip
	}
	conn, err := net.Dial("udp4", "192.0.2.1:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

// serverPrimaryIPv6 reports the host's global IPv6 address, or "" when the
// host has none — private/link-local addresses must not end up in public DNS.
// serverPrimaryIPv6, makinenin küresel IPv6 adresini bildirir; yoksa "" —
// özel/link-local adresler kamusal DNS'e girmemelidir.
func serverPrimaryIPv6() string {
	if ip := os.Getenv("CELIKPANEL_SERVER_IPV6"); ip != "" {
		return ip
	}
	conn, err := net.Dial("udp6", "[2001:db8::1]:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || !addr.IP.IsGlobalUnicast() || addr.IP.IsPrivate() {
		return ""
	}
	return addr.IP.String()
}
