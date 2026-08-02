package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/transport"
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

const appUnitMutationLockStripes = 256

var appUnitMutationLocks [appUnitMutationLockStripes]sync.Mutex

// net/rpc cannot cancel a method that was already dispatched. If the panel
// times out and starts compensation, the original method may therefore still
// be running. Serializing mutations for the same site makes the compensation
// deterministically run after that original method, rather than racing it.
func lockAppUnitMutation(siteID int) func() {
	lock := &appUnitMutationLocks[siteID%appUnitMutationLockStripes]
	lock.Lock()
	return lock.Unlock
}

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

type AppApplyRequest = transport.AppApplyRequest
type AppApplyResponse = transport.AppApplyResponse

// ApplyAppUnit writes (or rewrites) the unit file, reloads systemd and
// enables + (re)starts the app.
// ApplyAppUnit, unit dosyasını yazar (ya da yeniden yazar), systemd'yi
// yeniden yükler ve uygulamayı etkinleştirip (yeniden) başlatır.
func (a *Agent) ApplyAppUnit(req *AppApplyRequest, resp *AppApplyResponse) error {
	if req == nil || req.SiteID <= 0 || strings.TrimSpace(req.Command) == "" || req.Port <= 0 {
		resp.Error = "site id, command and port are required"
		return nil
	}
	if strings.ContainsAny(req.Command, "\n") || strings.ContainsAny(req.WorkDir, "\n") {
		resp.Error = "invalid characters in command or workdir"
		return nil
	}
	unlock := lockAppUnitMutation(req.SiteID)
	defer unlock()

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

type AppControlRequest = transport.AppControlRequest

func (a *Agent) ControlAppUnit(req *AppControlRequest, resp *AppApplyResponse) error {
	if req == nil || req.SiteID <= 0 {
		resp.Error = "a positive site identity is required"
		return nil
	}
	switch req.Action {
	case "start", "stop", "restart":
	default:
		resp.Error = "action must be start, stop or restart"
		return nil
	}
	unlock := lockAppUnitMutation(req.SiteID)
	defer unlock()
	name := appUnitName(req.SiteID)
	if out, err := exec.Command("systemctl", systemctlArgs(req.Action, name)...).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(out)))
		return nil
	}
	resp.Unit = name
	return nil
}

func appUnitSystemdState(name string) (loadState, activeState string, err error) {
	out, commandErr := exec.Command(
		"systemctl",
		systemctlArgs(
			"show",
			name,
			"--property=LoadState,ActiveState",
		)...,
	).CombinedOutput()
	if commandErr != nil {
		return "", "", fmt.Errorf(
			"systemctl show: %v: %s",
			commandErr,
			strings.TrimSpace(string(out)),
		)
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			loadState = strings.TrimSpace(value)
		case "ActiveState":
			activeState = strings.TrimSpace(value)
		}
	}
	if loadState == "" || activeState == "" {
		return "", "", errors.New("systemctl returned an incomplete unit state")
	}
	return loadState, activeState, nil
}

// RemoveAppUnit stops, disables and deletes the unit (project type changed
// or site deleted).
// RemoveAppUnit, unit'i durdurur, devre dışı bırakır ve siler (proje tipi
// değişti ya da site silindi).
func (a *Agent) RemoveAppUnit(req *AppControlRequest, resp *AppApplyResponse) error {
	if req == nil || req.SiteID <= 0 {
		resp.Error = "a positive site identity is required"
		return nil
	}
	unlock := lockAppUnitMutation(req.SiteID)
	defer unlock()
	name := appUnitName(req.SiteID)
	dir, err := unitDir()
	if err != nil {
		log.Printf("RemoveAppUnit site %d: locate unit directory: %v", req.SiteID, err)
		resp.Error = "application unit cleanup incomplete"
		return nil
	}
	unitPath := filepath.Join(dir, name+".service")
	info, statErr := os.Lstat(unitPath)
	unitFileExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		log.Printf("RemoveAppUnit site %d: inspect unit: %v", req.SiteID, statErr)
		resp.Error = "application unit cleanup incomplete"
		return nil
	}
	if unitFileExists && !info.Mode().IsRegular() {
		log.Printf("RemoveAppUnit site %d: refusing non-regular unit file", req.SiteID)
		resp.Error = "application unit cleanup refused"
		return nil
	}

	loadState, activeState, stateErr := appUnitSystemdState(name)
	if !unitFileExists && stateErr == nil && loadState == "not-found" && activeState == "inactive" {
		resp.Unit = name
		return nil
	}

	// Even when the unit file is already gone, systemd may still have the unit
	// loaded and its process running. Try every cleanup step, then use a final
	// filesystem + systemd proof to decide success. Intermediate command errors
	// are retained for diagnostics but are not fatal if that final proof shows
	// the requested state was nevertheless reached.
	var intermediateFailures []error
	if stateErr != nil {
		intermediateFailures = append(intermediateFailures, stateErr)
	}
	if output, err := exec.Command("systemctl", systemctlArgs("stop", name)...).CombinedOutput(); err != nil {
		intermediateFailures = append(intermediateFailures, fmt.Errorf("stop: %v: %s", err, strings.TrimSpace(string(output))))
	}
	if output, err := exec.Command("systemctl", systemctlArgs("disable", name)...).CombinedOutput(); err != nil {
		intermediateFailures = append(intermediateFailures, fmt.Errorf("disable: %v: %s", err, strings.TrimSpace(string(output))))
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		intermediateFailures = append(intermediateFailures, fmt.Errorf("remove unit file: %w", err))
	}
	if output, err := exec.Command("systemctl", systemctlArgs("daemon-reload")...).CombinedOutput(); err != nil {
		intermediateFailures = append(intermediateFailures, fmt.Errorf("daemon-reload: %v: %s", err, strings.TrimSpace(string(output))))
	}

	var finalFailures []error
	if _, err := os.Lstat(unitPath); err == nil {
		finalFailures = append(finalFailures, errors.New("unit file still exists"))
	} else if !os.IsNotExist(err) {
		finalFailures = append(finalFailures, fmt.Errorf("verify unit file removal: %w", err))
	}
	finalLoadState, finalActiveState, finalStateErr := appUnitSystemdState(name)
	if finalStateErr != nil {
		finalFailures = append(finalFailures, fmt.Errorf("verify unit state: %w", finalStateErr))
	} else if finalLoadState != "not-found" || finalActiveState != "inactive" {
		finalFailures = append(finalFailures, fmt.Errorf(
			"unit remains load=%s active=%s",
			finalLoadState,
			finalActiveState,
		))
	}
	if finalErr := errors.Join(finalFailures...); finalErr != nil {
		detail := errors.Join(append(intermediateFailures, finalErr)...)
		log.Printf("RemoveAppUnit site %d: %v", req.SiteID, detail)
		resp.Error = "application unit cleanup incomplete"
	} else if warning := errors.Join(intermediateFailures...); warning != nil {
		log.Printf("RemoveAppUnit site %d: cleanup completed after intermediate errors: %v", req.SiteID, warning)
	}
	resp.Unit = name
	return nil
}

type AppStatusResponse = transport.AppStatusResponse

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

type AppLogsRequest = transport.AppLogsRequest
type AppLogsResponse = transport.AppLogsResponse

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
