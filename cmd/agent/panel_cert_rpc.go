package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// lookupGroupID resolves a system group's numeric gid.
// lookupGroupID, bir sistem grubunun sayısal gid'ini çözer.
func lookupGroupID(name string) (int, bool) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	return gid, err == nil
}

// The panel's own certificate. Out of the box the panel serves HTTPS with a
// self-signed certificate — safe, but every browser warns. This RPC turns the
// operator's "get a real certificate" click into: ensure certbot exists,
// issue via Let's Encrypt (standalone :80), install the result where the
// panel loads its TLS material from, and drop a certbot deploy hook so future
// automatic renewals reach the panel too. The panel restart that activates
// the new certificate is triggered by the panel itself after replying.
//
// Panelin kendi sertifikası. Panel kutudan çıktığında HTTPS'i kendinden
// imzalı sertifikayla sunar — güvenli ama her tarayıcı uyarır. Bu RPC,
// operatörün "gerçek sertifika al" tıklamasını şuna çevirir: certbot'un
// varlığını sağla, Let's Encrypt ile al (standalone :80), sonucu panelin TLS
// malzemesini yüklediği yere kur ve gelecekteki otomatik yenilemeler panele
// de ulaşsın diye bir certbot deploy kancası bırak. Yeni sertifikayı
// etkinleştiren panel yeniden başlatması, cevap verdikten sonra panelin
// kendisi tarafından tetiklenir.

// validPanelCertDomain: a plain FQDN — becomes a certbot arg and part of a
// filesystem path, so nothing but hostname characters may pass.
// validPanelCertDomain: düz bir FQDN — certbot argümanı ve dosya yolu parçası
// olur; makine adı karakterlerinden başkası geçemez.
var validPanelCertDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
var validStagedSiteLineage = regexp.MustCompile(`^cp-site-[1-9][0-9]*-[a-f0-9]{24}$`)

var (
	certificateCleanupIsolatedStorage = isolatedSiteCertbotStorage
	certificateCleanupLegacyConfigDir = func() string { return legacyCertbotConfigDir }
	certificateCleanupLookPath        = exec.LookPath
	certificateCleanupRunCertbot      = func(args ...string) ([]byte, error) {
		return exec.Command("certbot", args...).CombinedOutput()
	}
)

type IssuePanelCertRequest struct {
	Domain              string `json:"domain"`
	Email               string `json:"email"`
	TLSDir              string `json:"tls_dir"` // where the panel loads panel.crt/panel.key from
	ExpectedBuildCommit string `json:"expected_build_commit,omitempty"`
}

type IssuePanelCertResponse struct {
	Issued    bool      `json:"issued"`
	ExpiresAt time.Time `json:"expires_at"`
	Detail    string    `json:"detail,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func (a *Agent) IssuePanelCertificate(req *IssuePanelCertRequest, resp *IssuePanelCertResponse) error {
	if req == nil {
		resp.Error = "panel certificate request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "issue panel certificate"); err != nil {
		resp.Error = err.Error()
		return nil
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if !validPanelCertDomain.MatchString(domain) {
		resp.Error = "invalid domain name"
		return nil
	}
	if req.TLSDir == "" || !strings.HasPrefix(filepath.Clean(req.TLSDir), "/") {
		resp.Error = "invalid TLS directory"
		return nil
	}

	// certbot is installed on first use — a deliberate, user-initiated install
	// (the button says so), consistent with the minimal-install principle.
	// certbot ilk kullanımda kurulur — bilinçli, kullanıcı-tetikli bir kurulum
	// (düğme bunu söyler); minimal kurulum ilkesiyle tutarlı.
	installedCertbot := false
	if _, err := exec.LookPath("certbot"); err != nil {
		family := detectPkgFamily()
		if _, err := installPackages(family, []string{"certbot"}); err != nil {
			resp.Error = fmt.Sprintf("certbot install failed: %v", err)
			return nil
		}
		installedCertbot = true
	}

	// Standalone answers the HTTP-01 challenge on :80 itself — right for a
	// panel-only server. If a web server holds :80 the error says so honestly.
	// Standalone, HTTP-01 doğrulamasını :80'de kendisi cevaplar — yalnız-panel
	// sunucu için doğru olan. :80'i bir web sunucusu tutuyorsa hata bunu
	// dürüstçe söyler.
	args := []string{
		"certonly", "--standalone", "--preferred-challenges", "http",
		"-d", domain,
		"--agree-tos", "--non-interactive", "--keep-until-expiring",
	}
	if req.Email != "" {
		args = append(args, "--email", req.Email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	out, err := exec.Command("certbot", args...).CombinedOutput()
	if err != nil {
		resp.Error = fmt.Sprintf("certbot failed: %s", certbotFirstError(string(out)))
		return nil
	}

	if err := installPanelCertFiles(domain, req.TLSDir); err != nil {
		resp.Error = err.Error()
		return nil
	}

	// Deploy hook: certbot's own timer renews the certificate; this hook
	// copies each renewal into the panel's TLS dir and restarts the panel, so
	// renewal needs no one to remember anything.
	// Deploy kancası: sertifikayı certbot'un kendi zamanlayıcısı yeniler; bu
	// kanca her yenilemeyi panelin TLS dizinine kopyalar ve paneli yeniden
	// başlatır — yenileme kimsenin bir şey hatırlamasını gerektirmez.
	writePanelCertDeployHook(domain, req.TLSDir)

	// "certbot's own timer" must actually exist and run: Debian's package
	// enables certbot.timer by itself, Arch ships certbot-renew.timer
	// DISABLED. Enable whichever this distro has — a certificate that
	// silently dies in 90 days is a trap, not a feature. Caught live on Arch.
	// "certbot'un kendi zamanlayıcısı" gerçekten var olmalı ve koşmalı:
	// Debian'ın paketi certbot.timer'ı kendisi etkinleştirir, Arch
	// certbot-renew.timer'ı KAPALI getirir. Bu dağıtımda hangisi varsa aç —
	// 90 günde sessizce ölen sertifika özellik değil tuzaktır. Arch'ta
	// canlıda yakalandı.
	for _, timer := range []string{"certbot.timer", "certbot-renew.timer"} {
		if exec.Command("systemctl", "enable", "--now", timer).Run() == nil {
			break
		}
	}

	resp.Issued = true
	resp.ExpiresAt = panelCertExpiry(filepath.Join(req.TLSDir, "panel.crt"))
	if installedCertbot {
		resp.Detail = "certbot installed"
	}
	return nil
}

// installPanelCertFiles copies the live LE material over the panel's
// cert/key pair: root-owned, group-readable so the low-privilege panel
// (celikpanel group) can load it but nobody else can read the key.
// installPanelCertFiles, canlı LE malzemesini panelin cert/key çiftinin
// üzerine kopyalar: root sahipli, grup-okunur — düşük yetkili panel
// (celikpanel grubu) yükleyebilsin ama anahtarı başkası okuyamasın.
func installPanelCertFiles(domain, tlsDir string) error {
	live := filepath.Join("/etc/letsencrypt/live", domain)
	if err := os.MkdirAll(tlsDir, 0o750); err != nil {
		return fmt.Errorf("tls dir: %v", err)
	}
	for _, f := range [][2]string{
		{filepath.Join(live, "fullchain.pem"), filepath.Join(tlsDir, "panel.crt")},
		{filepath.Join(live, "privkey.pem"), filepath.Join(tlsDir, "panel.key")},
	} {
		data, err := os.ReadFile(f[0])
		if err != nil {
			return fmt.Errorf("read %s: %v", f[0], err)
		}
		if err := os.WriteFile(f[1], data, 0o640); err != nil {
			return fmt.Errorf("write %s: %v", f[1], err)
		}
		chownRootPanelGroup(f[1])
	}
	return nil
}

// chownRootPanelGroup sets root:celikpanel so the panel user reads via group.
// chownRootPanelGroup, root:celikpanel yapar; panel kullanıcısı grup üzerinden okur.
func chownRootPanelGroup(path string) {
	if gid, ok := lookupGroupID("celikpanel"); ok {
		_ = os.Chown(path, 0, gid)
	}
	_ = os.Chmod(path, 0o640)
}

func writePanelCertDeployHook(domain, tlsDir string) {
	hookDir := "/etc/letsencrypt/renewal-hooks/deploy"
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return
	}
	script := fmt.Sprintf(`#!/bin/sh
# Managed by CelikPanel — after certbot renews the panel's certificate, copy
# it where the panel loads TLS from and restart the panel to serve it.
# CelikPanel yönetir — certbot panelin sertifikasını yenileyince, panelin TLS
# yüklediği yere kopyala ve sunması için paneli yeniden başlat.
if [ "$RENEWED_LINEAGE" = "/etc/letsencrypt/live/%s" ]; then
  cp -L "$RENEWED_LINEAGE/fullchain.pem" %s/panel.crt
  cp -L "$RENEWED_LINEAGE/privkey.pem" %s/panel.key
  chown root:celikpanel %s/panel.crt %s/panel.key
  chmod 640 %s/panel.crt %s/panel.key
  systemctl restart celikpanel-panel
fi
`, domain, tlsDir, tlsDir, tlsDir, tlsDir, tlsDir, tlsDir)
	_ = os.WriteFile(filepath.Join(hookDir, "celikpanel-panel-cert"), []byte(script), 0o755)
}

type DeleteCertLineageRequest struct {
	Domain              string   `json:"domain"`
	DeleteCanonical     bool     `json:"delete_canonical,omitempty"`
	LineageNames        []string `json:"lineage_names,omitempty"`
	SnapshotPath        string   `json:"snapshot_path,omitempty"`
	ExpectedBuildCommit string   `json:"expected_build_commit,omitempty"`
}

type DeleteCertLineageResponse struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// DeleteCertLineage removes only explicitly authorized CelikPanel
// customer-site lineages and one verified, exact immutable snapshot version.
// It never removes a domain snapshot root or deletes from the global
// /etc/letsencrypt store, where panel and operator-owned lineages may live.
// DeleteCertLineage yalnız açıkça yetkilendirilmiş CelikPanel müşteri-site
// lineage'larını ve doğrulanmış tek bir değişmez snapshot sürümünü siler.
// Domain snapshot kökünü veya panel/operatör lineage'larının bulunabileceği
// global /etc/letsencrypt deposunu asla silmez.
func (a *Agent) DeleteCertLineage(req *DeleteCertLineageRequest, resp *DeleteCertLineageResponse) error {
	if req == nil {
		resp.Error = "certificate lineage deletion request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "delete certificate lineage"); err != nil {
		resp.Error = err.Error()
		return nil
	}
	domain := strings.TrimSpace(req.Domain)
	if domain != "" {
		var err error
		domain, err = canonicalCertificateDomain(domain)
		if err != nil {
			resp.Error = "invalid domain name"
			return nil
		}
	}
	snapshotPath := strings.TrimSpace(req.SnapshotPath)
	if snapshotPath != req.SnapshotPath {
		resp.Error = "managed certificate snapshot path must be canonical"
		return nil
	}
	if req.DeleteCanonical && domain == "" {
		resp.Error = "canonical lineage deletion requires a domain"
		return nil
	}
	if snapshotPath != "" && domain == "" {
		resp.Error = "snapshot deletion requires a domain"
		return nil
	}
	if !req.DeleteCanonical && len(req.LineageNames) == 0 &&
		snapshotPath == "" {
		resp.Error = "a canonical lineage, staged lineage, or exact snapshot is required"
		return nil
	}
	if len(req.LineageNames) > 100 {
		resp.Error = "too many staged lineages"
		return nil
	}
	seen := make(map[string]struct{}, len(req.LineageNames))
	normalizedLineages := make([]string, 0, len(req.LineageNames))
	for _, raw := range req.LineageNames {
		lineage := strings.ToLower(strings.TrimSpace(raw))
		if !validStagedSiteLineage.MatchString(lineage) {
			resp.Error = "invalid staged lineage name"
			return nil
		}
		if _, ok := seen[lineage]; ok {
			continue
		}
		seen[lineage] = struct{}{}
		normalizedLineages = append(normalizedLineages, lineage)
	}
	var snapshotVersionDir string
	if snapshotPath != "" {
		var err error
		snapshotVersionDir, err = verifyManagedCertificateVersionPath(
			domain, snapshotPath,
		)
		if err != nil {
			resp.Error = fmt.Sprintf(
				"refuse unsafe immutable certificate snapshot cleanup: %v", err,
			)
			return nil
		}
	}
	if !acquireSiteCertbot() {
		resp.Error = "another site certificate operation is already running; retry shortly"
		return nil
	}
	defer releaseSiteCertbot()

	deleteLineage := func(storage certbotStorage, lineage string) error {
		if !certbotLineageExists(storage.ConfigDir, lineage) {
			return nil
		}
		if _, err := certificateCleanupLookPath("certbot"); err != nil {
			return fmt.Errorf("certbot lineage exists but certbot is missing")
		}
		args := []string{"delete"}
		args = append(args, storage.commandArgs()...)
		args = append(args, "--cert-name", lineage, "--non-interactive")
		out, err := certificateCleanupRunCertbot(args...)
		if err != nil {
			return fmt.Errorf("%s", certbotFirstError(string(out)))
		}
		resp.Deleted = true
		return nil
	}

	var cleanupErrors []string
	isolated := certificateCleanupIsolatedStorage()
	if req.DeleteCanonical {
		// The canonical domain lineage is removed only from CelikPanel's
		// isolated store. A same-named global lineage may be the panel
		// certificate or operator-owned legacy material.
		if err := deleteLineage(isolated, domain); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
	}

	legacy := isolated
	legacy.ConfigDir = certificateCleanupLegacyConfigDir()
	for _, lineage := range normalizedLineages {
		if err := deleteLineage(isolated, lineage); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
		// A staged reissue of an upgraded legacy certificate deliberately
		// reuses that legacy account store. The random, agent-generated prefix
		// makes this exact deletion safe without exposing arbitrary names.
		if err := deleteLineage(legacy, lineage); err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		}
	}

	if snapshotVersionDir != "" {
		versionRoot, err := customCertificateDirectory(domain)
		if err != nil {
			cleanupErrors = append(cleanupErrors, err.Error())
		} else {
			managedRoot := filepath.Dir(versionRoot)
			relativeVersion, err := filepath.Rel(managedRoot, snapshotVersionDir)
			if err != nil || relativeVersion == "." || relativeVersion == ".." ||
				strings.HasPrefix(relativeVersion, ".."+string(filepath.Separator)) {
				cleanupErrors = append(
					cleanupErrors,
					"refuse snapshot cleanup outside the managed certificate root",
				)
			} else if err := secureDeleteFileOrDir(
				managedRoot, filepath.ToSlash(relativeVersion),
			); err != nil {
				cleanupErrors = append(
					cleanupErrors,
					fmt.Sprintf(
						"remove exact immutable certificate snapshot: %v", err,
					),
				)
			} else {
				resp.Deleted = true
			}
		}
	}
	if len(cleanupErrors) != 0 {
		resp.Error = strings.Join(cleanupErrors, "; ")
	}
	return nil
}

type RestartPanelSoonRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit,omitempty"`
}

// RestartPanelSoon restarts the panel a moment from now, detached via
// systemd-run so the RPC (and the HTTP response that triggered it) completes
// first — activating a new certificate must not kill the reply in flight.
// RestartPanelSoon, paneli birazdan yeniden başlatır; systemd-run ile ayrık —
// önce RPC (ve onu tetikleyen HTTP cevabı) tamamlanır: yeni sertifikayı
// etkinleştirmek, yoldaki cevabı öldürmemeli.
func (a *Agent) RestartPanelSoon(req *RestartPanelSoonRequest, resp *bool) error {
	if req == nil {
		return fmt.Errorf("panel restart request is required")
	}
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "restart panel"); err != nil {
		return err
	}
	err := exec.Command("systemd-run", "--on-active=2", "--timer-property=AccuracySec=100ms",
		"systemctl", "restart", "celikpanel-panel").Run()
	*resp = err == nil
	return nil
}

// panelCertExpiry parses NotAfter from a PEM certificate, zero on any failure.
// panelCertExpiry, PEM sertifikadan NotAfter okur; hata durumunda sıfır.
func panelCertExpiry(path string) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}
	}
	return cert.NotAfter
}

// certbotFirstError extracts certbot's actual complaint from its output.
// certbotFirstError, certbot'un asıl şikâyetini çıktısından ayıklar.
func certbotFirstError(out string) string {
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "Problem") || strings.Contains(l, "Error") ||
			strings.Contains(l, "error") || strings.Contains(l, "failed") ||
			strings.Contains(l, "Invalid") || strings.Contains(l, "Timeout") {
			return l
		}
	}
	return firstLine(out)
}
