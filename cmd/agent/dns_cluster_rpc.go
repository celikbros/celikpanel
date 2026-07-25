package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

// Two nameservers on two machines — the thing "ns1 and ns2" is supposed to mean.
//
// The operator asked the right question (25 Jul): "can we use Frankfurt and
// Boston as the two nameservers, backing each other up? Or am I thinking
// nonsense?" It is not nonsense; it is exactly what a hosting provider does.
// Today both ns1.celikhost.com and ns2.celikhost.com resolve to ONE machine, so
// the pair is decoration: one reboot and every hosted domain goes dark. Some
// registries refuse glue for a pair sharing an address for precisely this
// reason.
//
// The professional shape is a primary/secondary pair: one server owns the zone
// data, the other holds a full copy and answers with equal authority. Both
// serve EVERY zone, regardless of which machine hosts the website — DNS
// authority and web hosting are separate jobs, and conflating them is what made
// the earlier ns1.<domain> design wrong.
//
// The replication uses PowerDNS's own autoprimary mechanism rather than any
// panel-to-panel API: the secondary is told "trust NOTIFYs from this address"
// once, and thereafter every zone created on the primary appears there by
// itself. That keeps the panel single-server — two panels never have to talk,
// authenticate to each other, or stay in step.
//
// İki makinede iki ad sunucusu — "ns1 ve ns2"nin zaten anlatması gereken şey.
//
// Operatör doğru soruyu sordu (25 Tem): "Frankfurt'u ve Boston'u iki ad
// sunucusu olarak kullanıp birbirlerini yedekleyebilir miyiz? Yoksa saçma mı
// düşünüyorum?" Saçma değil; bir barındırma sağlayıcısının yaptığının ta
// kendisi. Bugün ns1.celikhost.com ile ns2.celikhost.com TEK makineye
// çözülüyor, yani çift bir süs: bir yeniden başlatma ve barındırılan her alan
// adı kararıyor. Bazı kayıt kuruluşları tam da bu yüzden aynı adresi paylaşan
// bir çifte glue vermeyi reddeder.
//
// Profesyonel şekil birincil/ikincil çiftidir: bir sunucu zone verisinin
// sahibidir, diğeri tam bir kopya tutar ve eşit yetkiyle cevap verir. İkisi de
// HER zone'u sunar, siteyi hangi makine barındırırsa barındırsın — DNS
// yetkisi ile web barındırma ayrı işlerdir ve bunları karıştırmak, önceki
// ns1.<alanadı> tasarımını yanlışlayan şeydi.
//
// Çoğaltma, panelden panele bir API yerine PowerDNS'in kendi otomatik-birincil
// mekanizmasını kullanır: ikincile bir kez "bu adresten gelen NOTIFY'lara
// güven" denir ve ondan sonra birincilde oluşturulan her zone orada kendiliğinden
// belirir. Bu, paneli tek-sunucu olarak korur — iki panelin birbiriyle
// konuşması, kimlik doğrulaması ya da senkron kalması hiç gerekmez.

const (
	dnsRoleStandalone = "standalone"
	dnsRolePrimary    = "primary"
	dnsRoleSecondary  = "secondary"
)

var dnsClusterConf = "/etc/powerdns/pdns.d/celikpanel-cluster.conf"

type DNSClusterRequest struct {
	Role string `json:"role"` // standalone | primary | secondary
	// PeerIP is the other server's address: where to send NOTIFY and allow
	// AXFR from (primary), or whose NOTIFYs to trust (secondary).
	// PeerIP diğer sunucunun adresidir: NOTIFY'ın gönderileceği ve AXFR'ye izin
	// verilecek adres (birincil) ya da NOTIFY'larına güvenilecek adres (ikincil).
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

func (a *Agent) ConfigureDNSCluster(req *DNSClusterRequest, resp *DNSClusterResponse) error {
	if _, err := exec.LookPath("pdns_server"); err != nil {
		resp.Error = "PowerDNS is not installed on this server"
		return nil
	}
	switch req.Role {
	case dnsRoleStandalone, dnsRolePrimary, dnsRoleSecondary:
	default:
		resp.Error = "role must be standalone, primary or secondary"
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
	restore := func() {
		if hadPrevious {
			_ = os.WriteFile(dnsClusterConf, previous, 0o644)
		} else {
			_ = os.Remove(dnsClusterConf)
		}
		_ = exec.Command("systemctl", "restart", "pdns").Run()
	}

	var conf string
	switch req.Role {
	case dnsRolePrimary:
		// The primary owns the data: it must let the secondary pull a full copy
		// (AXFR) and must tell it the moment a zone changes (NOTIFY).
		// Birincil verinin sahibidir: ikincilin tam kopya çekmesine (AXFR) izin
		// vermeli ve bir zone değişir değişmez ona haber vermelidir (NOTIFY).
		conf = fmt.Sprintf(`# Managed by CelikPanel — do not edit by hand / elle düzenlemeyin
# DNS cluster role: primary. The secondary at %s holds a full copy.
# DNS küme rolü: birincil. %s adresindeki ikincil tam kopya tutar.
primary=yes
allow-axfr-ip=%s
also-notify=%s
`, req.PeerIP, req.PeerIP, req.PeerIP, req.PeerIP)

	case dnsRoleSecondary:
		// The secondary trusts NOTIFYs from the primary and creates unknown
		// zones on its own (autosecondary). No panel-to-panel call is needed.
		// İkincil, birincilden gelen NOTIFY'lara güvenir ve bilmediği zone'ları
		// kendiliğinden oluşturur (autosecondary). Panelden panele çağrı gerekmez.
		conf = fmt.Sprintf(`# Managed by CelikPanel — do not edit by hand / elle düzenlemeyin
# DNS cluster role: secondary of %s. Zones arrive by themselves.
# DNS küme rolü: %s'in ikincili. Zone'lar kendiliğinden gelir.
secondary=yes
autosecondary=yes
`, req.PeerIP, req.PeerIP)
	}

	if conf == "" {
		_ = os.Remove(dnsClusterConf)
	} else {
		if err := os.MkdirAll("/etc/powerdns/pdns.d", 0o755); err != nil {
			resp.Error = err.Error()
			return nil
		}
		if err := os.WriteFile(dnsClusterConf, []byte(conf), 0o644); err != nil {
			resp.Error = err.Error()
			return nil
		}
		_ = os.Chmod(dnsClusterConf, 0o644)
	}

	if err := applyAutoprimary(req); err != nil {
		restore()
		resp.Error = err.Error()
		return nil
	}
	if err := setLocalZoneType(req.Role); err != nil {
		restore()
		resp.Error = err.Error()
		return nil
	}

	if out, err := exec.Command("systemctl", "restart", "pdns").CombinedOutput(); err != nil {
		restore()
		resp.Error = fmt.Sprintf("PowerDNS refused the new configuration and it was rolled back: %s", firstLine(string(out)))
		return nil
	}

	resp.Applied = true
	switch req.Role {
	case dnsRolePrimary:
		resp.Detail = "this server is the DNS primary; " + req.PeerIP + " will receive a copy of every zone"
	case dnsRoleSecondary:
		resp.Detail = "this server is a DNS secondary of " + req.PeerIP + "; zones will arrive on their own"
	default:
		resp.Detail = "this server serves DNS on its own"
	}
	return nil
}

// applyAutoprimary maintains the secondary's list of trusted primaries. The
// table is PowerDNS's own (`supermasters`), so nothing here invents a schema.
// applyAutoprimary, ikincilin güvendiği birincillerin listesini tutar. Tablo
// PowerDNS'in kendisinindir (`supermasters`); burada şema uydurulmaz.
func applyAutoprimary(req *DNSClusterRequest) error {
	db, err := sql.Open("sqlite", pdnsDBPath())
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec(`DELETE FROM supermasters WHERE account = 'celikpanel'`); err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	if req.Role != dnsRoleSecondary {
		return nil
	}
	_, err = db.Exec(`INSERT INTO supermasters (ip, nameserver, account) VALUES (?, ?, 'celikpanel')`,
		req.PeerIP, strings.TrimSuffix(req.PeerNS, "."))
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	return nil
}

// setLocalZoneType switches the zones this server already holds between NATIVE
// (nobody replicates) and MASTER (this server notifies a secondary). A
// secondary's zones are created by PowerDNS itself as SLAVE and are left alone.
// setLocalZoneType, bu sunucunun hâlihazırda tuttuğu zone'ları NATIVE (kimse
// çoğaltmıyor) ile MASTER (bu sunucu bir ikincile haber veriyor) arasında
// değiştirir. İkincilin zone'larını PowerDNS kendisi SLAVE olarak oluşturur ve
// onlara dokunulmaz.
func setLocalZoneType(role string) error {
	db, err := sql.Open("sqlite", pdnsDBPath())
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	defer db.Close()

	switch role {
	case dnsRolePrimary:
		_, err = db.Exec(`UPDATE domains SET type = 'MASTER' WHERE type = 'NATIVE'`)
	case dnsRoleStandalone:
		_, err = db.Exec(`UPDATE domains SET type = 'NATIVE' WHERE type = 'MASTER'`)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	return nil
}
