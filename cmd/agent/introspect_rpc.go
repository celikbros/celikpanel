package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
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

// PostfixQueue returns the real mail queue via `postqueue -j` (JSON lines).
// PostfixQueue, gerçek mail kuyruğunu `postqueue -j` (JSON satırları) ile döndürür.
func (a *Agent) PostfixQueue(args *transport.Empty, resp *core.PostfixQueueResult) error {
	out, err := exec.Command("postqueue", "-j").Output()
	if err != nil {
		// postqueue absent or queue unreadable: not installed / nothing to show.
		// postqueue yok ya da kuyruk okunamıyor: kurulu değil / gösterilecek yok.
		resp.Installed = false
		resp.Items = []core.PostfixQueueItem{}
		return nil
	}
	resp.Installed = true
	resp.Items = []core.PostfixQueueItem{}

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
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
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
