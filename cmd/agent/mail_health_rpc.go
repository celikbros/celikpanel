package main

import (
	"context"
	"errors"
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
		ptr, aligned, forward, err := publicMailDNSIdentity(context.Background(), resp.ServerIP, resp.Myhostname)
		recordMailDNSLookup(resp, ptr, aligned, forward, err)
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

// recordMailDNSLookup carries the lookup outcome in the health answer: a
// failed lookup leaves PTR, PTRAligned and FCrDNS unset and says it failed and
// why, so the Panel reports the reverse DNS as unknown rather than as missing
// (D-025 invariant 2, 2026-10-10). A name that cannot be looked up is not a
// lookup and is left unmarked.
// Basarisiz sorgu PTR'yi eksik gostermez; sonucu ve sinifi yanitta tasinir.
func recordMailDNSLookup(resp *MailHealthResponse, ptr string, aligned, forward bool, err error) {
	switch {
	case err == nil:
		resp.PTR, resp.PTRAligned, resp.FCrDNS = ptr, aligned, forward
		resp.ReverseDNSLookup = transport.MailDNSLookupDone
	case errors.Is(err, errMailDNSIdentityInvalid):
	default:
		resp.ReverseDNSLookup = transport.MailDNSLookupFailed
		resp.ReverseDNSLookupError = mailDNSLookupErrorClass(err)
	}
}

// isPrivateIP reports whether the server sits behind NAT (dev boxes, home
// labs) — there the port-25 verdict would blame the wrong network.
// isPrivateIP, sunucunun NAT arkasında olup olmadığını bildirir — orada
// 25-portu hükmü yanlış ağı suçlardı.
func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}
