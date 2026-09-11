//go:build linux

package main

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

func setupProtectedFile(path string) ([]byte, bool) {
	parent, err := openTrustedRootOwnedDirectory(filepath.Dir(path))
	if err != nil {
		return nil, false
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, filepath.Base(path), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > 1<<20 {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	return data, err == nil && len(data) <= 1<<20
}

// Reject an unsupported or ambiguous renewal route rather than treating the
// presence of a timer as proof that the challenge can be served.
func setupPanelRenewalAuthenticator(config []byte) string {
	values := map[string]string{}
	for _, line := range strings.Split(string(config), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "authenticator" && key != "webroot_path" {
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return ""
		}
		values[key] = value
	}
	switch values["authenticator"] {
	case "standalone":
		return "standalone"
	case "webroot":
		if strings.TrimSuffix(values["webroot_path"], ",") == hostingpath.PanelACMEChallengeRoot() {
			return "webroot"
		}
	}
	return ""
}

func setupPanelRenewalRouteReady(ctx context.Context, domain, authenticator string) bool {
	if authenticator == "standalone" {
		if _, err := exec.LookPath("nginx"); err == nil {
			return false
		}
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", "127.0.0.1:80")
		if err == nil {
			conn.Close()
			return false
		}
		return ctx.Err() == nil
	}
	if authenticator != "webroot" {
		return false
	}
	available := filepath.Join("/etc/nginx/sites-available", panelACMEVhostName(domain)+".conf")
	enabled := filepath.Join("/etc/nginx/sites-enabled", panelACMEVhostName(domain)+".conf")
	contents, ok := setupProtectedFile(available)
	if !ok || string(contents) != renderPanelACMEChallengeVhost(domain, hostingpath.PanelACMEChallengeRoot()) {
		return false
	}
	target, err := filepath.EvalSymlinks(enabled)
	if err != nil || target != available {
		return false
	}
	return serviceMutationCommand(ctx, "systemctl", "is-active", "--quiet", "nginx").Run() == nil
}

// Read-only: checks the active identity, exact managed hook, renewal lineage
// and timer. It never issues a certificate or enables a unit.
// Salt-okur: kimligi, yonetilen hook'u, lineage'i ve zamanlayiciyi kontrol eder.
func (a *Agent) PanelRenewalReadiness(req *transport.PanelRenewalReadinessRequest, resp *transport.PanelRenewalReadinessResponse) error {
	resp.Ready = false
	resp.Code = "panel_renewal_required"
	if req == nil {
		return nil
	}
	domain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil || domain != req.Domain {
		return nil
	}
	active, found, err := activePanelCertificateIdentity(managedPanelTLSDir)
	if err != nil || !found || active != domain {
		return nil
	}
	hook, ok := setupProtectedFile("/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert")
	if !ok || string(hook) != renderPanelCertDeployHook() {
		return nil
	}
	hookInfo, err := os.Stat("/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert")
	if err != nil || hookInfo.Mode().Perm()&0100 == 0 {
		return nil
	}
	config, ok := setupProtectedFile(filepath.Join("/etc/letsencrypt/renewal", panelCertLineageName(domain)+".conf"))
	if !ok {
		return nil
	}
	authenticator := setupPanelRenewalAuthenticator(config)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if !setupPanelRenewalRouteReady(ctx, domain, authenticator) {
		return nil
	}
	for _, timer := range []string{"certbot.timer", "certbot-renew.timer"} {
		if serviceMutationCommand(ctx, "systemctl", "is-active", "--quiet", timer).Run() != nil {
			continue
		}
		if serviceMutationCommand(ctx, "systemctl", "is-enabled", "--quiet", timer).Run() != nil {
			continue
		}
		resp.Ready = true
		resp.Code = "ready"
		return nil
	}
	return nil
}
