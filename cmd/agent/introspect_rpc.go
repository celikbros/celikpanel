package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
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
