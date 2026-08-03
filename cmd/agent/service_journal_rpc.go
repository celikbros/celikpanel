package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// ServiceJournal returns a unit's recent journal lines. Nine components had a
// hand-written management page and every other one — Rspamd, ClamAV, Redis,
// Memcached, Node — landed on a dead end when the operator pressed Manage
// (25 Jul: "birçok servisin manage'i doğru düzgün çalışmıyor. dinamik manage
// sistemi yok sanırım"). They were right: there was no generic path. The
// generic page is built from what the panel already knows plus this — the one
// thing every daemon has and nobody could see from the panel: its own log.
//
// Read-only by construction: journalctl is queried, never written, and the
// unit name is validated so the argument cannot become an option or a second
// command.
//
// ServiceJournal, bir unit'in son günlük satırlarını döndürür. Dokuz bileşenin
// elle yazılmış yönetim sayfası vardı; geri kalan her biri — Rspamd, ClamAV,
// Redis, Memcached, Node — operatör Yönet'e bastığında çıkmaz sokağa
// düşüyordu (25 Tem: "birçok servisin manage'i doğru düzgün çalışmıyor.
// dinamik manage sistemi yok sanırım"). Haklıydılar: genel bir yol yoktu.
// Genel sayfa, panelin zaten bildiklerinden ve buradan kurulur — her
// daemon'da bulunan ve panelden görülemeyen tek şeyden: kendi günlüğünden.
//
// Yapısı gereği salt-okunur: journalctl sorgulanır, asla yazılmaz ve unit adı
// doğrulanır ki argüman bir seçeneğe ya da ikinci bir komuta dönüşemesin.
type ServiceJournalRequest = transport.ServiceJournalRequest

type ServiceJournalResponse = transport.ServiceJournalResponse

func (a *Agent) ServiceJournal(req *ServiceJournalRequest, resp *ServiceJournalResponse) error {
	unit := strings.TrimSpace(req.Unit)
	if !validUnitName(unit) {
		resp.Error = "invalid unit name"
		return nil
	}
	lines := req.Lines
	if lines <= 0 || lines > 500 {
		lines = 200
	}
	resp.Unit = unit

	out, err := exec.Command("journalctl", "-u", unit, "-n", fmt.Sprint(lines),
		"--no-pager", "--output=short-iso").CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err != nil && text == "" {
		resp.Error = fmt.Sprintf("journalctl: %v", err)
		return nil
	}
	if text == "" {
		return nil
	}
	resp.Lines = strings.Split(text, "\n")
	return nil
}

// validUnitName accepts what systemd itself accepts and nothing more, so a
// crafted name can never turn into a flag ("--rotate") or escape into a shell.
// validUnitName, systemd'nin kabul ettiğini kabul eder, fazlasını değil;
// böylece uydurma bir ad asla bir bayrağa ("--rotate") dönüşemez ya da kabuğa
// kaçamaz.
func validUnitName(name string) bool {
	if name == "" || len(name) > 128 || strings.HasPrefix(name, "-") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '@', r == '\\', r == ':':
		default:
			return false
		}
	}
	return true
}
