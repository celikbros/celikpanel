package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// RBL (DNS blocklist) check for the server's outbound IPv4 address. A mail
// server on a blocklist has its mail silently rejected everywhere, and the
// operator usually finds out only from bounced messages — this surfaces it
// on demand. We query the well-known zones by the standard reversed-octet
// convention; a returned A record means "listed".
//
// Sunucunun dışa dönük IPv4 adresi için RBL (DNS kara-liste) kontrolü. Kara
// listedeki bir posta sunucusunun postası her yerde sessizce reddedilir ve
// operatör bunu genelde yalnız geri dönen iletilerden öğrenir — bu, talep
// üzerine gösterir. İyi bilinen zone'ları standart ters-oktet kuralıyla
// sorgularız; dönen bir A kaydı "listede" demektir.

var rblZones = []string{
	"zen.spamhaus.org",
	"bl.spamcop.net",
	"b.barracudacentral.org",
	"dnsbl.sorbs.net",
}

type RBLResult struct {
	Zone   string `json:"zone"`
	Listed bool   `json:"listed"`
	Detail string `json:"detail,omitempty"`
}

type CheckRBLResponse struct {
	IP      string      `json:"ip"`
	Results []RBLResult `json:"results"`
	Error   string      `json:"error,omitempty"`
}

func (a *Agent) CheckRBL(_ *struct{}, resp *CheckRBLResponse) error {
	ip := outboundIPv4()
	if ip == "" {
		resp.Error = "could not determine the server's outbound IPv4 address"
		return nil
	}
	resp.IP = ip

	rev, err := reverseIPv4(ip)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	// Query zones in parallel with a bounded timeout — a slow blocklist must
	// not hang the whole check.
	// Zone'ları sınırlı zaman aşımıyla paralel sorgula — yavaş bir kara-liste
	// tüm kontrolü askıya almamalı.
	results := make([]RBLResult, len(rblZones))
	var wg sync.WaitGroup
	for i, zone := range rblZones {
		wg.Add(1)
		go func(i int, zone string) {
			defer wg.Done()
			results[i] = queryRBL(rev, zone)
		}(i, zone)
	}
	wg.Wait()

	resp.Results = results
	return nil
}

// queryRBL looks up <reversed-ip>.<zone>; an A answer means the IP is listed,
// and a listing usually carries a TXT reason we surface as the detail.
// queryRBL, <ters-ip>.<zone>'u arar; bir A yanıtı IP'nin listede olduğu
// demektir ve listeleme genelde detay olarak gösterdiğimiz bir TXT sebep
// taşır.
func queryRBL(reversedIP, zone string) RBLResult {
	res := RBLResult{Zone: zone}
	resolver := &net.Resolver{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	host := reversedIP + "." + zone
	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		// NXDOMAIN is the normal "not listed" answer.
		// NXDOMAIN normal "listede değil" yanıtıdır.
		return res
	}
	res.Listed = true
	if txts, err := resolver.LookupTXT(ctx, host); err == nil && len(txts) > 0 {
		res.Detail = strings.TrimSpace(txts[0])
	}
	return res
}

// outboundIPv4 mirrors the panel's own resolution: an explicit override
// first (NAT), else the default-route source address. The UDP dial sends
// nothing; it only resolves routing.
// outboundIPv4, panelin kendi çözümünü yansıtır: önce açık geçersiz kılma
// (NAT), yoksa varsayılan rotanın kaynak adresi.
func outboundIPv4() string {
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

// reverseIPv4 turns 203.0.113.7 into 7.113.0.203, the RBL query convention.
// reverseIPv4, 203.0.113.7'yi RBL sorgu kuralı olan 7.113.0.203'e çevirir.
func reverseIPv4(ip string) (string, error) {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return "", fmt.Errorf("%q is not an IPv4 address", ip)
	}
	return fmt.Sprintf("%d.%d.%d.%d", parsed[3], parsed[2], parsed[1], parsed[0]), nil
}
