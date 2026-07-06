package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Real usage numbers for one site: disk from du, this month's traffic from
// the site's nginx access log. A panel must never show invented values —
// a stat that always reads 0 is worse than no stat.
// Bir sitenin gerçek kullanım sayıları: disk du'dan, bu ayın trafiği sitenin
// nginx erişim günlüğünden. Panel asla uydurma değer göstermemeli — hep 0
// okunan bir istatistik, hiç olmamasından kötüdür.

type SiteUsageRequest struct {
	SiteHome string `json:"site_home"` // /var/www/celikpanel/subscriptions/N/sites/M
	Domain   string `json:"domain"`
}

type SiteUsageResponse struct {
	DiskBytes         int64  `json:"disk_bytes"`
	TrafficMonthBytes int64  `json:"traffic_month_bytes"`
	Error             string `json:"error,omitempty"`
}

func (a *Agent) SiteUsage(req *SiteUsageRequest, resp *SiteUsageResponse) error {
	cleanHome := filepath.Clean(req.SiteHome)
	if !strings.HasPrefix(cleanHome, "/var/www/celikpanel/subscriptions/") {
		resp.Error = "site home outside the hosting base"
		return nil
	}

	// du -sb: one fast syscall walk done by a tool that exists everywhere.
	// du -sb: her yerde bulunan bir araçla tek hızlı tarama.
	if out, err := exec.Command("du", "-sb", cleanHome).Output(); err == nil {
		if fields := strings.Fields(string(out)); len(fields) > 0 {
			resp.DiskBytes, _ = strconv.ParseInt(fields[0], 10, 64)
		}
	}

	resp.TrafficMonthBytes = monthTrafficFromLog(req.Domain)
	return nil
}

// monthTrafficFromLog sums $body_bytes_sent for the current month from the
// per-domain access log our vhosts configure. Includes the rotated .1 file
// so a mid-month rotation does not zero the number. Missing logs mean the
// site had no traffic (or nginx is absent) — an honest 0.
// monthTrafficFromLog, vhost'larımızın yapılandırdığı domain-başına erişim
// günlüğünden bu ayın $body_bytes_sent toplamını çıkarır. Ay ortasındaki
// döndürme sayıyı sıfırlamasın diye .1 dosyası da dahildir. Günlük yoksa
// site trafik görmemiştir (ya da nginx yok) — dürüst bir 0.
func monthTrafficFromLog(domain string) int64 {
	if domain == "" || strings.ContainsAny(domain, "/\\") {
		return 0
	}
	// nginx log timestamps look like [05/Jul/2026:22:55:39 +0000].
	// nginx günlük zaman damgaları [05/Jul/2026:22:55:39 +0000] gibidir.
	month := "/" + time.Now().Format("Jan/2006")

	var total int64
	base := "/var/log/nginx/" + domain + "-access.log"
	for _, path := range []string{base, base + ".1"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, month) {
				continue
			}
			// Combined format: ip - - [date zone] "METHOD path proto"
			// status body_bytes "referer" "agent" → status is field 8,
			// size is field 9. Odd request lines are skipped, not guessed.
			// Birleşik biçim: durum 8. alan, boyut 9. alandır. Bozuk istek
			// satırları tahmin edilmez, atlanır.
			fields := strings.Fields(line)
			if len(fields) < 10 || len(fields[8]) != 3 {
				continue
			}
			if n, err := strconv.ParseInt(fields[9], 10, 64); err == nil {
				total += n
			}
		}
		f.Close()
	}
	return total
}
