package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Supervised app processes (Node projects) — roadmap 3A. Every app is a
// systemd unit (celikapp-<siteID>.service): start/stop/restart, restart on
// crash, coming back after reboot and journald logs all come from the init
// system instead of a process manager dependency (deliberately no PM2).
//
// In production the agent is root and manages system units. On a non-root
// development agent, CELIKPANEL_SYSTEMD_USER=1 switches to `systemctl --user`
// and ~/.config/systemd/user — the same code path, honestly supervised.
//
// Gözetimli uygulama süreçleri (Node projeleri) — yol haritası 3A. Her
// uygulama bir systemd unit'idir (celikapp-<siteID>.service): başlat/durdur/
// yeniden başlat, çökünce yeniden başlama, reboot sonrası ayağa kalkma ve
// journald logları bir süreç yöneticisi bağımlılığı yerine init sisteminden
// gelir (bilinçli olarak PM2 yok).

var systemdUserMode = os.Getenv("CELIKPANEL_SYSTEMD_USER") == "1"

func systemctlArgs(args ...string) []string {
	if systemdUserMode {
		return append([]string{"--user"}, args...)
	}
	return args
}

func unitDir() (string, error) {
	if systemdUserMode {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "systemd", "user"), nil
	}
	return "/etc/systemd/system", nil
}

var appUnitNameRe = regexp.MustCompile(`^celikapp-[0-9]+$`)

// appUnitName builds and validates the unit name for a site.
// appUnitName, bir site için unit adını üretir ve doğrular.
func appUnitName(siteID int) string { return fmt.Sprintf("celikapp-%d", siteID) }

type AppApplyRequest struct {
	SiteID      int    `json:"site_id"`
	Description string `json:"description"` // domain name, for `systemctl status` readability
	WorkDir     string `json:"work_dir"`
	Command     string `json:"command"`
	Port        int    `json:"port"`
	NodeVersion string `json:"node_version"` // installed runtime version ("" = system PATH)
	RunAsUser   string `json:"run_as_user,omitempty"`
}

type AppApplyResponse struct {
	Unit  string `json:"unit"`
	Error string `json:"error,omitempty"`
}

// ApplyAppUnit writes (or rewrites) the unit file, reloads systemd and
// enables + (re)starts the app.
// ApplyAppUnit, unit dosyasını yazar (ya da yeniden yazar), systemd'yi
// yeniden yükler ve uygulamayı etkinleştirip (yeniden) başlatır.
func (a *Agent) ApplyAppUnit(req *AppApplyRequest, resp *AppApplyResponse) error {
	if req.SiteID <= 0 || strings.TrimSpace(req.Command) == "" || req.Port <= 0 {
		resp.Error = "site id, command and port are required"
		return nil
	}
	if strings.ContainsAny(req.Command, "\n") || strings.ContainsAny(req.WorkDir, "\n") {
		resp.Error = "invalid characters in command or workdir"
		return nil
	}

	name := appUnitName(req.SiteID)
	dir, err := unitDir()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}

	pathEnv := os.Getenv("PATH")
	if req.NodeVersion != "" {
		bin := nodeBinPath(req.NodeVersion)
		if _, err := os.Stat(bin); err != nil {
			// Honest: refuse to write a unit that cannot start.
			// Dürüstlük: başlayamayacak bir unit yazmayı reddet.
			resp.Error = fmt.Sprintf("node %s is not installed on this server", req.NodeVersion)
			return nil
		}
		pathEnv = filepath.Dir(bin) + ":" + pathEnv
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by CelikPanel — do not edit by hand.\n")
	fmt.Fprintf(&b, "# CelikPanel tarafından yönetilir — elle düzenlemeyin.\n")
	fmt.Fprintf(&b, "[Unit]\nDescription=CelikPanel app: %s\nAfter=network.target\n\n", sanitizeUnitValue(req.Description))
	fmt.Fprintf(&b, "[Service]\n")
	if !systemdUserMode && req.RunAsUser != "" {
		fmt.Fprintf(&b, "User=%s\n", sanitizeUnitValue(req.RunAsUser))
	}
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", req.WorkDir)
	fmt.Fprintf(&b, "Environment=PORT=%d\n", req.Port)
	fmt.Fprintf(&b, "Environment=NODE_ENV=production\n")
	fmt.Fprintf(&b, "Environment=PATH=%s\n", pathEnv)
	fmt.Fprintf(&b, "ExecStart=/bin/sh -lc '%s'\n", strings.ReplaceAll(req.Command, "'", `'\''`))
	fmt.Fprintf(&b, "Restart=on-failure\nRestartSec=3\n\n")
	fmt.Fprintf(&b, "[Install]\nWantedBy=%s\n", installTarget())

	if err := os.WriteFile(filepath.Join(dir, name+".service"), []byte(b.String()), 0o644); err != nil {
		resp.Error = err.Error()
		return nil
	}

	steps := [][]string{
		systemctlArgs("daemon-reload"),
		systemctlArgs("enable", name),
		systemctlArgs("restart", name),
	}
	for _, args := range steps {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			resp.Error = fmt.Sprintf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			return nil
		}
	}
	resp.Unit = name
	return nil
}

func installTarget() string {
	if systemdUserMode {
		return "default.target"
	}
	return "multi-user.target"
}

// sanitizeUnitValue strips newlines so a crafted value cannot inject unit
// directives.
// sanitizeUnitValue, satır sonlarını temizler; kurgulanmış bir değer unit
// yönergesi enjekte edemesin.
func sanitizeUnitValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
}

type AppControlRequest struct {
	SiteID int    `json:"site_id"`
	Action string `json:"action"` // start | stop | restart
}

func (a *Agent) ControlAppUnit(req *AppControlRequest, resp *AppApplyResponse) error {
	switch req.Action {
	case "start", "stop", "restart":
	default:
		resp.Error = "action must be start, stop or restart"
		return nil
	}
	name := appUnitName(req.SiteID)
	if out, err := exec.Command("systemctl", systemctlArgs(req.Action, name)...).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))
		return nil
	}
	resp.Unit = name
	return nil
}

// RemoveAppUnit stops, disables and deletes the unit (project type changed
// or site deleted).
// RemoveAppUnit, unit'i durdurur, devre dışı bırakır ve siler (proje tipi
// değişti ya da site silindi).
func (a *Agent) RemoveAppUnit(req *AppControlRequest, resp *AppApplyResponse) error {
	name := appUnitName(req.SiteID)
	_ = exec.Command("systemctl", systemctlArgs("stop", name)...).Run()
	_ = exec.Command("systemctl", systemctlArgs("disable", name)...).Run()
	dir, err := unitDir()
	if err == nil {
		_ = os.Remove(filepath.Join(dir, name+".service"))
	}
	_ = exec.Command("systemctl", systemctlArgs("daemon-reload")...).Run()
	resp.Unit = name
	return nil
}

type AppStatusResponse struct {
	Exists   bool   `json:"exists"`
	Active   string `json:"active"` // active | inactive | failed | activating...
	PID      int    `json:"pid"`
	MemoryMB int64  `json:"memory_mb"`
	CPUUsec  int64  `json:"cpu_usec"`
	Uptime   string `json:"uptime"`
}

// AppUnitStatus reads live state from systemd (PID, memory, CPU time) — the
// same honest numbers `systemctl status` would show.
// AppUnitStatus, systemd'den canlı durumu okur (PID, bellek, CPU süresi) —
// `systemctl status`un göstereceği dürüst sayıların aynısı.
func (a *Agent) AppUnitStatus(req *AppControlRequest, resp *AppStatusResponse) error {
	name := appUnitName(req.SiteID)
	out, err := exec.Command("systemctl", systemctlArgs("show", name,
		"--property=LoadState,ActiveState,MainPID,MemoryCurrent,CPUUsageNSec,ActiveEnterTimestamp")...).Output()
	if err != nil {
		return nil
	}

	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}

	resp.Exists = props["LoadState"] == "loaded"
	resp.Active = props["ActiveState"]
	resp.PID, _ = strconv.Atoi(props["MainPID"])
	if mem, err := strconv.ParseInt(props["MemoryCurrent"], 10, 64); err == nil && mem >= 0 {
		resp.MemoryMB = mem / (1024 * 1024)
	}
	if cpu, err := strconv.ParseInt(props["CPUUsageNSec"], 10, 64); err == nil && cpu >= 0 {
		resp.CPUUsec = cpu / 1000
	}
	resp.Uptime = props["ActiveEnterTimestamp"]
	return nil
}

type AppLogsRequest struct {
	SiteID int `json:"site_id"`
	Lines  int `json:"lines"`
}

type AppLogsResponse struct {
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}

// AppUnitLogs tails the app's journal.
// AppUnitLogs, uygulamanın journal'ını okur.
func (a *Agent) AppUnitLogs(req *AppLogsRequest, resp *AppLogsResponse) error {
	if req.Lines <= 0 || req.Lines > 1000 {
		req.Lines = 100
	}
	name := appUnitName(req.SiteID)
	if !appUnitNameRe.MatchString(name) {
		resp.Error = "invalid unit"
		return nil
	}

	args := []string{"-u", name + ".service", "-n", strconv.Itoa(req.Lines), "--no-pager", "-o", "short-iso"}
	if systemdUserMode {
		args = append([]string{"--user"}, args...)
	}
	out, err := exec.Command("journalctl", args...).CombinedOutput()
	if err != nil {
		resp.Error = strings.TrimSpace(string(out))
		return nil
	}

	resp.Lines = []string{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "-- ") {
			resp.Lines = append(resp.Lines, line)
		}
	}
	return nil
}
