package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// This file reads real service state from the system (postqueue, doveadm,
// fail2ban-client, nginx). Nothing here fabricates numbers: if a tool is
// missing or a service is down, the result says so and the UI shows an
// honest empty/stopped state.
//
// Bu dosya gerçek servis durumunu sistemden okur (postqueue, doveadm,
// fail2ban-client, nginx). Burada hiçbir şey sayı uydurmaz: bir araç yoksa
// ya da servis kapalıysa, sonuç bunu söyler ve arayüz dürüst bir boş/durdu
// durumu gösterir.

// Swapped by tests. Production asks the installed Postfix.
// Testlerde değiştirilir.
var (
	postfixQueueLookPath = exec.LookPath
	postfixQueueList     = func() ([]byte, error) {
		cmd := exec.Command("postqueue", "-j")
		cmd.Env = append(os.Environ(), "LC_ALL=C", "LANGUAGE=C")
		return cmd.Output()
	}
)

// errPostfixQueueUnreadable carries the exact transport text so the Panel can
// classify it; see transport.PostfixQueueUnreadable.
var errPostfixQueueUnreadable = errors.New(transport.PostfixQueueUnreadable)

// What Postfix's programs print when they stop on their own configuration: a
// value they cannot use ("fatal: bad numerical configuration: name = value";
// also boolean, time and string length), or a fatal that names main.cf or
// master.cf and a line.
var (
	postfixBadConfiguration = regexp.MustCompile(`fatal: (bad [a-z ]+ configuration: .+)$`)
	postfixConfigurationAt  = regexp.MustCompile(`fatal: (\S*/(?:main|master)\.cf, line \d+: .+)$`)
)

// postfixQueueUnreadable is the unknown-queue answer with what is known about
// why (10 Oct 2026): the first line `postqueue` printed as the detail, and the
// cause only where that line itself identifies it. Measured: with a main.cf
// Postfix refuses, `postqueue -j` exits 69 with "fatal: bad numerical
// configuration"; with Postfix stopped it reads the queue directly and
// succeeds. So "check that Postfix is running" was never the action.
// postfixQueueUnreadable, nedeni hakkında bilinenle birlikte bilinmeyen-kuyruk
// yanıtıdır: `postqueue`nin yazdığı ilk satır ve yalnız o satırın kendisinin
// belirlediği neden.
func postfixQueueUnreadable(said string) error {
	detail, cause := "", ""
	for _, raw := range strings.Split(said, "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		if line == "" {
			continue
		}
		if detail == "" {
			detail = line
		}
		if match := postfixBadConfiguration.FindStringSubmatch(line); match != nil {
			detail, cause = match[1], transport.UnreadablePostfixConfig
			break
		}
		if match := postfixConfigurationAt.FindStringSubmatch(line); match != nil {
			detail, cause = match[1], transport.UnreadablePostfixConfig
			break
		}
	}
	if detail != "" {
		detail = hostcmd.Bounded(dbConfigSecret.ReplaceAllString(detail, "${1}…"), 300)
	}
	return errors.New(transport.UnreadableWithEvidence(transport.PostfixQueueUnreadable, cause, detail))
}

// PostfixQueue returns the real mail queue via `postqueue -j` (JSON lines).
//
// Two answers are known: Postfix is not on this server (Installed false, no
// items), and the queue as `postqueue -j` printed it. Everything else is
// unknown and is an error: `postqueue` failing (it does when the mail system is
// down), a line that is not the JSON it prints, an output that could not be
// read to its end. Before 9 Oct 2026 each of those was answered as an empty
// queue, and the screen said "the queue is empty" over a queue nobody had read.
//
// PostfixQueue, gerçek mail kuyruğunu `postqueue -j` (JSON satırları) ile
// döndürür. İki yanıt bilinir: Postfix bu sunucuda yok, ya da `postqueue -j`nin
// yazdığı kuyruk. Geri kalan her şey bilinmeyendir ve hatadır; 9 Eki 2026'dan
// önce her biri boş kuyruk diye yanıtlanıyordu.
func (a *Agent) PostfixQueue(args *transport.Empty, resp *core.PostfixQueueResult) error {
	*resp = core.PostfixQueueResult{Items: []core.PostfixQueueItem{}}
	if _, err := postfixQueueLookPath("postqueue"); err != nil {
		return nil
	}
	out, err := postfixQueueList()
	if err != nil {
		log.Printf("mail queue: postqueue -j failed: %s", hostcmd.Diagnostic(out, err))
		return postfixQueueUnreadable(hostcmd.Stderr(err))
	}
	resp.Installed = true

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry struct {
			QueueName   string `json:"queue_name"`
			QueueID     string `json:"queue_id"`
			ArrivalTime int64  `json:"arrival_time"`
			MessageSize int64  `json:"message_size"`
			Sender      string `json:"sender"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.QueueID == "" {
			// A message that cannot be listed makes the list a partial one,
			// and a partial list reads as the whole queue.
			// Listelenemeyen bir ileti listeyi eksik yapar.
			log.Printf("mail queue: postqueue -j printed a line that is not a queue entry")
			*resp = core.PostfixQueueResult{Items: []core.PostfixQueueItem{}}
			return errPostfixQueueUnreadable
		}
		resp.Items = append(resp.Items, core.PostfixQueueItem{
			ID:      entry.QueueID,
			Size:    humanSize(entry.MessageSize),
			Sender:  entry.Sender,
			Arrival: time.Unix(entry.ArrivalTime, 0).Format("2006-01-02 15:04"),
			Status:  entry.QueueName,
		})
		switch entry.QueueName {
		case "active":
			resp.Summary.Active++
		case "deferred":
			resp.Summary.Deferred++
		case "hold":
			resp.Summary.Hold++
		case "corrupt":
			resp.Summary.Corrupt++
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("mail queue: the output of postqueue -j could not be read to its end: %v", err)
		*resp = core.PostfixQueueResult{Items: []core.PostfixQueueItem{}}
		return errPostfixQueueUnreadable
	}
	return nil
}

// PostfixQueueAction flushes or purges the queue.
// PostfixQueueAction kuyruğu boşaltır ya da temizler.
func (a *Agent) PostfixQueueAction(req *core.PostfixActionRequest, resp *bool) error {
	var cmd *exec.Cmd
	switch req.Action {
	case "flush":
		cmd = exec.Command("postqueue", "-f")
	case "delete_all":
		cmd = exec.Command("postsuper", "-d", "ALL")
	case "delete_id":
		if req.ID == "" {
			return fmt.Errorf("id required")
		}
		cmd = exec.Command("postsuper", "-d", req.ID)
	default:
		return fmt.Errorf("unknown action %q", req.Action)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("queue action failed: %w", err)
	}
	*resp = true
	return nil
}

// DovecotStats returns what can actually be measured: whether Dovecot is
// present, its uptime, and the current connection count from `doveadm who`.
// Login/auth counters need the stats plugin and are left at zero rather than
// invented.
//
// DovecotStats, gerçekten ölçülebilenleri döndürür: Dovecot'un var olup
// olmadığı, çalışma süresi ve `doveadm who`'dan mevcut bağlantı sayısı.
// Giriş/kimlik sayaçları stats eklentisi gerektirir ve uydurulmak yerine
// sıfır bırakılır.
func (a *Agent) DovecotStats(args *transport.Empty, resp *core.DovecotStatsResult) error {
	if _, err := exec.LookPath("doveadm"); err != nil {
		resp.Installed = false
		return nil
	}
	resp.Installed = true

	// Current connections: count data lines from `doveadm who`.
	// Mevcut bağlantılar: `doveadm who`'nun veri satırlarını say.
	if out, err := exec.Command("doveadm", "who").Output(); err == nil {
		n := 0
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "username") {
				continue
			}
			n++
		}
		resp.Stats.Connections = n
	}
	resp.Stats.Uptime = systemctlUptime("dovecot")
	return nil
}

// Fail2banStatus returns the real jail list and banned IPs via
// fail2ban-client. When the tool is missing, Installed is false.
// Fail2banStatus, gerçek jail listesini ve banlı IP'leri fail2ban-client
// ile döndürür. Araç yoksa Installed false olur.
func (a *Agent) Fail2banStatus(args *transport.Empty, resp *core.Fail2banStatusResult) error {
	resp.Jails = []core.Fail2banJail{}
	resp.Banned = []core.Fail2banBannedIP{}

	out, err := exec.Command("fail2ban-client", "status").Output()
	if err != nil {
		resp.Installed = false
		return nil
	}
	resp.Installed = true

	// The "Jail list:" line holds comma-separated jail names.
	// "Jail list:" satırı virgülle ayrılmış jail adlarını tutar.
	var jailNames []string
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.Index(line, "Jail list:"); idx >= 0 {
			list := strings.TrimSpace(line[idx+len("Jail list:"):])
			for _, name := range strings.Split(list, ",") {
				if n := strings.TrimSpace(name); n != "" {
					jailNames = append(jailNames, n)
				}
			}
		}
	}

	for _, name := range jailNames {
		jailOut, err := exec.Command("fail2ban-client", "status", name).Output()
		if err != nil {
			continue
		}
		banned, ips := parseJailStatus(string(jailOut))
		resp.Jails = append(resp.Jails, core.Fail2banJail{
			Name:    name,
			Enabled: true, // present in runtime = enabled
			Active:  true,
			Banned:  banned,
		})
		for _, ip := range ips {
			resp.Banned = append(resp.Banned, core.Fail2banBannedIP{IP: ip, Jail: name, Country: "—"})
		}
	}
	return nil
}

// parseJailStatus pulls the banned count and banned IP list from a
// `fail2ban-client status <jail>` block.
// parseJailStatus, bir `fail2ban-client status <jail>` bloğundan banlı
// sayısını ve banlı IP listesini çıkarır.
func parseJailStatus(out string) (int, []string) {
	banned := 0
	var ips []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "Currently banned:"); i >= 0 {
			fmt.Sscanf(strings.TrimSpace(line[i+len("Currently banned:"):]), "%d", &banned)
		}
		if i := strings.Index(line, "Banned IP list:"); i >= 0 {
			for _, ip := range strings.Fields(line[i+len("Banned IP list:"):]) {
				ips = append(ips, ip)
			}
		}
	}
	return banned, ips
}

// Fail2banUnban removes an IP ban via fail2ban-client.
// Fail2banUnban, bir IP yasağını fail2ban-client ile kaldırır.
func (a *Agent) Fail2banUnban(req *core.Fail2banUnbanRequest, resp *bool) error {
	if req.Jail == "" || req.IP == "" {
		return fmt.Errorf("jail and ip required")
	}
	if err := exec.Command("fail2ban-client", "set", req.Jail, "unbanip", req.IP).Run(); err != nil {
		return fmt.Errorf("unban failed: %w", err)
	}
	*resp = true
	return nil
}

// Fail2banToggleJail starts or stops a jail at runtime via fail2ban-client.
// Fail2banToggleJail, bir jail'i fail2ban-client ile çalışma zamanında
// başlatır ya da durdurur.
func (a *Agent) Fail2banToggleJail(req *core.Fail2banJailRequest, resp *bool) error {
	if err := services.ValidateSQLIdentifier(strings.ReplaceAll(req.Name, "-", "_")); err != nil {
		return fmt.Errorf("invalid jail name: %w", err)
	}
	action := "stop"
	if req.Enabled {
		action = "start"
	}
	if err := exec.Command("fail2ban-client", action, req.Name).Run(); err != nil {
		return fmt.Errorf("jail %s failed: %w", action, err)
	}
	*resp = true
	return nil
}

// Fail2banConfig reads the real global defaults from jail.local (preferred)
// or jail.conf. Missing keys stay empty rather than being invented.
// Fail2banConfig, gerçek global varsayılanları jail.local (tercihen) ya da
// jail.conf'tan okur. Eksik anahtarlar uydurulmak yerine boş kalır.
func (a *Agent) Fail2banConfig(args *transport.Empty, resp *core.Fail2banConfig) error {
	var content string
	for _, path := range []string{"/etc/fail2ban/jail.local", "/etc/fail2ban/jail.conf"} {
		if b, err := os.ReadFile(path); err == nil {
			content = string(b)
			break
		}
	}
	resp.IgnoreIP = []string{}
	if content == "" {
		return nil
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "bantime":
			if resp.BanTime == "" {
				resp.BanTime = val
			}
		case "findtime":
			if resp.FindTime == "" {
				resp.FindTime = val
			}
		case "maxretry":
			if resp.MaxRetry == 0 {
				fmt.Sscanf(val, "%d", &resp.MaxRetry)
			}
		case "ignoreip":
			if len(resp.IgnoreIP) == 0 {
				resp.IgnoreIP = strings.Fields(val)
			}
		}
	}
	return nil
}

// NginxInspect reads the real, effective nginx configuration via `nginx -T`
// (which dumps every included file). Directives that aren't set stay empty
// rather than being filled with plausible defaults.
//
// NginxInspect, gerçek, etkin nginx yapılandırmasını `nginx -T` ile okur
// (dahil edilen her dosyayı döker). Ayarlanmamış direktifler makul
// varsayılanlarla doldurulmak yerine boş kalır.
func (a *Agent) NginxInspect(args *transport.Empty, resp *core.NginxInspectResult) error {
	resp.RateLimits = []core.NginxRateLimit{}
	// `nginx -T` prints config to stdout; needs root (the agent has it).
	// `nginx -T` config'i stdout'a yazar; root ister (agent'ta var).
	out, err := exec.Command("nginx", "-T").CombinedOutput()
	if err != nil {
		resp.Installed = false
		return nil
	}
	resp.Installed = true
	text := string(out)

	resp.Global = core.NginxGlobalConfig{
		WorkerProcesses:   nginxDirective(text, "worker_processes"),
		WorkerConnections: nginxDirective(text, "worker_connections"),
		KeepaliveTimeout:  nginxDirective(text, "keepalive_timeout"),
		ClientMaxBodySize: nginxDirective(text, "client_max_body_size"),
		ServerTokens:      nginxDirective(text, "server_tokens"),
		Gzip:              nginxDirective(text, "gzip"),
	}
	resp.SSL = core.NginxSSLConfig{
		SSLProtocols:           nginxDirective(text, "ssl_protocols"),
		SSLCiphers:             nginxDirective(text, "ssl_ciphers"),
		SSLPreferServerCiphers: nginxDirective(text, "ssl_prefer_server_ciphers"),
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ";"))
		if strings.HasPrefix(line, "limit_req_zone ") || strings.HasPrefix(line, "limit_conn_zone ") {
			resp.RateLimits = append(resp.RateLimits, parseRateLimit(line))
		}
	}
	return nil
}

// nginxDirective returns the value of the first `<name> <value>;` directive
// found in the config dump.
// nginxDirective, config dökümünde bulunan ilk `<name> <value>;`
// direktifinin değerini döndürür.
func nginxDirective(text, name string) string {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == name {
			val := strings.TrimSpace(strings.TrimSuffix(strings.Join(fields[1:], " "), ";"))
			return val
		}
	}
	return ""
}

// parseRateLimit turns a limit_*_zone directive line into a structured entry.
// parseRateLimit, bir limit_*_zone direktif satırını yapılandırılmış bir
// girdiye dönüştürür.
func parseRateLimit(line string) core.NginxRateLimit {
	fields := strings.Fields(line)
	rl := core.NginxRateLimit{Name: fields[0]}
	if len(fields) >= 2 {
		rl.Zone = fields[1]
	}
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "zone=") {
			if _, sz, ok := strings.Cut(strings.TrimPrefix(f, "zone="), ":"); ok {
				rl.Size = sz
			}
		}
		if strings.HasPrefix(f, "rate=") {
			rl.Rate = strings.TrimPrefix(f, "rate=")
		}
	}
	return rl
}

// humanSize renders a byte count compactly.
// humanSize bir bayt sayısını derli toplu gösterir.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGT"[exp])
}

// systemctlUptime returns a service's uptime derived from its active-enter
// timestamp, or "—" when unavailable.
// systemctlUptime, bir servisin çalışma süresini aktif-giriş zaman
// damgasından türetir, yoksa "—" döndürür.
func systemctlUptime(service string) string {
	out, err := exec.Command("systemctl", "show", service, "--property=ActiveEnterTimestamp", "--value").Output()
	if err != nil {
		return "—"
	}
	ts := strings.TrimSpace(string(out))
	if ts == "" {
		return "—"
	}
	// Format e.g. "Thu 2026-07-03 21:00:00 UTC"
	t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", ts)
	if err != nil {
		return "—"
	}
	d := time.Since(t)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
