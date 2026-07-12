package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// The firewall — the third leg of attack-surface management. A default-deny
// inbound policy means a server exposes only the ports it actually needs: the
// panel opens a service's ports when it is installed and closes them when it
// is removed, so the open-port set always equals the running-service set.
//
// Safety first: SSH is detected from the live listeners and ALWAYS kept open,
// and loopback + established connections always pass. A misconfigured rule can
// therefore never lock the operator out of the box. Turning the firewall off
// deletes our table entirely, returning to the distro default (all open).
//
// Güvenlik duvarı — saldırı yüzeyi yönetiminin üçüncü ayağı. Varsayılan-reddet
// gelen politikası: sunucu yalnız gerçekten ihtiyaç duyduğu portları açar.
// Panel servis kurulunca portlarını açar, kaldırılınca kapatır; açık-port
// kümesi her zaman koşan-servis kümesine eşittir.
//
// Önce güvenlik: SSH canlı dinleyicilerden tespit edilir ve DAİMA açık tutulur;
// loopback + kurulu bağlantılar her zaman geçer. Yanlış bir kural operatörü
// asla kutudan kilitleyemez. Güvenlik duvarını kapatmak tablomuzu tümüyle
// siler (dağıtım varsayılanına, her şey açık, döner).

const fwTable = "celikpanel_fw"

type ApplyFirewallRequest struct {
	Enabled  bool  `json:"enabled"`
	TCPPorts []int `json:"tcp_ports"`
	UDPPorts []int `json:"udp_ports"`
}

type FirewallStatusResponse struct {
	Enabled  bool   `json:"enabled"`
	TCPPorts []int  `json:"tcp_ports"`
	UDPPorts []int  `json:"udp_ports"`
	SSHPorts []int  `json:"ssh_ports"`
	Error    string `json:"error,omitempty"`
}

// ApplyFirewall installs (or tears down) our nftables table. Enabled=false
// removes it. Enabled=true builds a default-drop input chain that always
// admits loopback, established/related, ICMP and SSH, plus the requested
// service ports.
// ApplyFirewall, nftables tablomuzu kurar (ya da kaldırır).
func (a *Agent) ApplyFirewall(req *ApplyFirewallRequest, resp *FirewallStatusResponse) error {
	if _, err := exec.LookPath("nft"); err != nil {
		resp.Error = "nftables (nft) is not available on this system"
		return nil
	}

	// Always tear down our old table first so re-apply is idempotent and a
	// disable is a clean removal.
	// Önce eski tablomuzu kaldır ki yeniden uygulama idempotent olsun ve
	// kapatma temiz bir kaldırma olsun.
	_ = exec.Command("nft", "delete", "table", "inet", fwTable).Run()

	if !req.Enabled {
		resp.Enabled = false
		return nil
	}

	sshPorts := detectSSHPorts()
	tcp := dedupeSorted(append(append([]int{}, req.TCPPorts...), sshPorts...))
	udp := dedupeSorted(req.UDPPorts)

	// Build the ruleset as a single atomic `nft -f -` script: if any line is
	// wrong, nft applies none of it — never a half-open firewall.
	// Kural setini tek atomik `nft -f -` betiği olarak kur: bir satır yanlışsa
	// nft hiçbirini uygulamaz — asla yarı-açık güvenlik duvarı.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("table inet %s {\n", fwTable))
	b.WriteString("  chain input {\n")
	b.WriteString("    type filter hook input priority 0; policy drop;\n")
	b.WriteString("    iif lo accept\n")
	b.WriteString("    ct state established,related accept\n")
	b.WriteString("    ct state invalid drop\n")
	b.WriteString("    meta l4proto icmp accept\n")
	b.WriteString("    meta l4proto ipv6-icmp accept\n")
	if len(tcp) > 0 {
		b.WriteString(fmt.Sprintf("    tcp dport { %s } accept\n", joinInts(tcp)))
	}
	if len(udp) > 0 {
		b.WriteString(fmt.Sprintf("    udp dport { %s } accept\n", joinInts(udp)))
	}
	b.WriteString("  }\n}\n")

	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("nft apply failed: %s", strings.TrimSpace(string(out)))
		return nil
	}

	resp.Enabled = true
	resp.TCPPorts = tcp
	resp.UDPPorts = udp
	resp.SSHPorts = sshPorts
	return nil
}

// FirewallStatus reports whether our table is present and what it admits.
// FirewallStatus, tablomuzun var olup olmadığını ve neyi kabul ettiğini bildirir.
func (a *Agent) FirewallStatus(_ *struct{}, resp *FirewallStatusResponse) error {
	if _, err := exec.LookPath("nft"); err != nil {
		return nil
	}
	resp.SSHPorts = detectSSHPorts()
	out, err := exec.Command("nft", "list", "table", "inet", fwTable).Output()
	if err != nil {
		return nil // table absent → firewall off
	}
	resp.Enabled = true
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if p, ok := parsePortLine(line, "tcp dport"); ok {
			resp.TCPPorts = p
		}
		if p, ok := parsePortLine(line, "udp dport"); ok {
			resp.UDPPorts = p
		}
	}
	return nil
}

// detectSSHPorts finds every port a listening sshd is bound to, so the
// firewall can never cut the operator's own connection. Falls back to 22.
// detectSSHPorts, dinleyen bir sshd'nin bağlı olduğu her portu bulur; güvenlik
// duvarı operatörün kendi bağlantısını asla kesemez. 22'ye düşer.
func detectSSHPorts() []int {
	// -p is required for the process column; without it "sshd" never matches
	// and we would always fall back to 22 (fine here, wrong on a custom port).
	// -p, süreç sütunu için şart; onsuz "sshd" asla eşleşmez ve hep 22'ye
	// düşerdik (burada doğru, özel portta yanlış).
	out, err := exec.Command("ss", "-ltnpH").Output()
	if err != nil {
		return []int{22}
	}
	seen := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "sshd") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3] // e.g. 0.0.0.0:22 or [::]:22
		if i := strings.LastIndexByte(local, ':'); i >= 0 {
			if n, err := strconv.Atoi(local[i+1:]); err == nil {
				seen[n] = true
			}
		}
	}
	if len(seen) == 0 {
		return []int{22}
	}
	return dedupeSorted(mapKeys(seen))
}

func parsePortLine(line, prefix string) ([]int, bool) {
	if !strings.HasPrefix(line, prefix) {
		return nil, false
	}
	open, close := strings.IndexByte(line, '{'), strings.IndexByte(line, '}')
	if open < 0 || close < 0 || close < open {
		return nil, false
	}
	var ports []int
	for _, tok := range strings.Split(line[open+1:close], ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(tok)); err == nil {
			ports = append(ports, n)
		}
	}
	return ports, true
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func dedupeSorted(ns []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range ns {
		if n > 0 && n < 65536 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

func mapKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
