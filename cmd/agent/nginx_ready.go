package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// A web server that is installed but cannot serve the panel's vhosts, db-tools
// or webmail is only half-installed — and "installed" must never mean that
// (operator, 24 Jul: "how can there be roundcube without nginx, nonsense").
// Debian's nginx ships an http block that already includes conf.d/*.conf and
// sites-enabled/*.conf; Arch's minimal nginx.conf includes neither and the
// directories do not even exist, so every conf the panel drops is invisible.
// EnsureNginxReady makes a freshly installed nginx panel-ready on any distro:
// the drop-in dirs exist and the http block includes them. Idempotent — on
// Debian it finds everything already in place and changes nothing.
//
// Kurulu ama panelin vhost'larını, db-araçlarını ya da webmail'ini
// sunamayan bir web sunucusu yalnız yarı kuruludur — ve "kurulu" bu asla
// demek olmamalı (operatör, 24 Tem: "nginx yokken roundcube nasıl olur,
// saçmalık"). Debian'ın nginx'i, conf.d/*.conf ve sites-enabled/*.conf'u
// zaten dahil eden bir http bloğuyla gelir; Arch'ın minimal nginx.conf'u
// hiçbirini dahil etmez ve dizinler bile yoktur, bu yüzden panelin bıraktığı
// her conf görünmez. EnsureNginxReady, yeni kurulmuş bir nginx'i her dağıtımda
// panel-hazır yapar: drop-in dizinleri var ve http bloğu onları dahil eder.
// Idempotent — Debian'da her şeyi yerinde bulur ve hiçbir şey değiştirmez.

const nginxMainConf = "/etc/nginx/nginx.conf"

type EnsureNginxReadyResponse struct {
	Ready   bool   `json:"ready"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

func (a *Agent) EnsureNginxReady(_ *struct{}, resp *EnsureNginxReadyResponse) error {
	if _, err := exec.LookPath("nginx"); err != nil {
		resp.Error = "nginx is not installed"
		return nil
	}

	// The drop-in dirs the panel writes into (site vhosts, db-tools, webmail).
	// Panelin içine yazdığı drop-in dizinleri (site vhost'ları, db-araçları, webmail).
	for _, d := range []string{
		"/etc/nginx/conf.d",
		"/etc/nginx/sites-available",
		"/etc/nginx/sites-enabled",
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			resp.Error = fmt.Sprintf("mkdir %s: %v", d, err)
			return nil
		}
	}

	data, err := os.ReadFile(nginxMainConf)
	if err != nil {
		resp.Error = fmt.Sprintf("read nginx.conf: %v", err)
		return nil
	}
	content := string(data)

	// Which includes are missing from the http block? Debian already has both.
	// http bloğunda hangi include'lar eksik? Debian'da ikisi de var.
	needConfD := !strings.Contains(content, "conf.d/*.conf")
	needSites := !strings.Contains(content, "sites-enabled/")
	if !needConfD && !needSites {
		resp.Ready = true
		return nil
	}

	// Insert right after the http block opens. Finding "http {" is enough —
	// nginx.conf always has exactly one, and inserting at its top keeps the
	// includes before any server block that might depend on them.
	// http bloğu açılır açılmaz ekle. "http {" bulmak yeter — nginx.conf'ta
	// tam bir tane vardır ve en başına eklemek, include'ları onlara bağımlı
	// olabilecek herhangi bir server bloğundan önce tutar.
	marker := "http {"
	idx := strings.Index(content, marker)
	if idx < 0 {
		resp.Error = "nginx.conf has no http block to extend"
		return nil
	}
	var b strings.Builder
	b.WriteString("\n    # Added by CelikPanel so the panel's drop-in configs are served.\n")
	if needConfD {
		b.WriteString("    include /etc/nginx/conf.d/*.conf;\n")
	}
	if needSites {
		b.WriteString("    include /etc/nginx/sites-enabled/*.conf;\n")
	}
	cut := idx + len(marker)
	updated := content[:cut] + b.String() + content[cut:]

	// Write, validate, and roll back on failure — nginx.conf is the one file
	// whose breakage takes the whole web server down.
	// Yaz, doğrula, hatada geri al — nginx.conf, bozulması tüm web sunucusunu
	// düşüren tek dosyadır.
	backup := nginxMainConf + ".celikpanel.bak"
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		resp.Error = fmt.Sprintf("backup: %v", err)
		return nil
	}
	if err := os.WriteFile(nginxMainConf, []byte(updated), 0o644); err != nil {
		resp.Error = fmt.Sprintf("write nginx.conf: %v", err)
		return nil
	}
	if out, err := exec.Command("nginx", "-t").CombinedOutput(); err != nil {
		_ = os.WriteFile(nginxMainConf, data, 0o644) // restore
		_ = os.Remove(backup)
		resp.Error = fmt.Sprintf("nginx rejected the updated config, rolled back: %s", firstLine(string(out)))
		return nil
	}
	_ = os.Remove(backup)
	if err := a.systemdMgr.Reload("nginx"); err != nil {
		resp.Error = fmt.Sprintf("nginx reload failed: %v", err)
		return nil
	}

	resp.Ready = true
	resp.Changed = true
	return nil
}
