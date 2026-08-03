package main

import (
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Server-side deliverability facts: the checks that decide whether the big
// providers accept our mail, gathered where they can actually be measured.
// PTR is the one the panel cannot fix — it is set at the hosting provider —
// so the result carries enough detail for the UI to tell the operator
// exactly what to enter there.
//
// Sunucu tarafı teslim edilebilirlik gerçekleri: büyük sağlayıcıların
// postamızı kabul edip etmeyeceğine karar veren kontroller, gerçekten
// ölçülebildikleri yerde toplanır. PTR panelin düzeltemeyeceği tek şeydir —
// barındırma sağlayıcısında ayarlanır — bu yüzden sonuç, arayüzün operatöre
// oraya tam olarak ne gireceğini söyleyebileceği kadar ayrıntı taşır.

type MailHealthResponse = transport.MailHealthResponse

// MailHealth measures the server-level deliverability facts.
// MailHealth, sunucu düzeyi teslim edilebilirlik gerçeklerini ölçer.
func (a *Agent) MailHealth(_ *transport.Empty, resp *MailHealthResponse) error {
	resp.ServerIP = detectPublicIP()

	if out, err := exec.Command("postconf", "-h", "myhostname").Output(); err == nil {
		resp.Myhostname = strings.TrimSpace(string(out))
	}
	resp.HostnameFQDN = strings.Contains(resp.Myhostname, ".")

	if out, err := exec.Command("postconf", "-h", "smtpd_tls_security_level").Output(); err == nil {
		level := strings.TrimSpace(string(out))
		resp.TLSEnabled = level != "" && level != "none"
	}

	if resp.ServerIP != "" {
		if names, err := net.LookupAddr(resp.ServerIP); err == nil && len(names) > 0 {
			resp.PTR = strings.TrimSuffix(names[0], ".")
			resp.PTRAligned = resp.PTR != "" && strings.EqualFold(resp.PTR, resp.Myhostname)
			if addrs, err := net.LookupHost(resp.PTR); err == nil {
				for _, a := range addrs {
					if a == resp.ServerIP {
						resp.FCrDNS = true
						break
					}
				}
			}
		}
	}

	// One well-known MX, short timeout. Failure here almost always means the
	// provider filters outbound 25 (private ranges get "unknown").
	// Bilinen bir MX, kısa zaman aşımı. Buradaki hata hemen her zaman
	// sağlayıcının giden 25'i süzdüğü anlamına gelir.
	resp.OutboundPort25 = "unknown"
	if conn, err := net.DialTimeout("tcp", "gmail-smtp-in.l.google.com:25", 5*time.Second); err == nil {
		conn.Close()
		resp.OutboundPort25 = "open"
	} else if !isPrivateIP(resp.ServerIP) {
		resp.OutboundPort25 = "blocked"
	}
	return nil
}

// isPrivateIP reports whether the server sits behind NAT (dev boxes, home
// labs) — there the port-25 verdict would blame the wrong network.
// isPrivateIP, sunucunun NAT arkasında olup olmadığını bildirir — orada
// 25-portu hükmü yanlış ağı suçlardı.
func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}
