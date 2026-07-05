package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// CreateSite handles site creation with all privileged operations
func (a *Agent) CreateSite(req transport.CreateSiteRequest, reply *transport.CreateSiteResponse) error {
	// 1. Create directory structure
	err := os.MkdirAll(req.DocumentRoot, 0755)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to create directories: %v", err)
		return nil
	}

	// 2. Create Linux user
	err = a.userManager.CreateUser(req.Username, filepath.Dir(req.DocumentRoot), req.Password)
	if err != nil {
		os.RemoveAll(filepath.Dir(req.DocumentRoot)) // Rollback
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to create user: %v", err)
		return nil
	}

	// 3. Set ownership
	if err := a.userManager.SetOwnership(filepath.Dir(req.DocumentRoot), req.Username); err != nil {
		log.Printf("CreateSite %s: ownership: %v", req.Domain, err)
	}

	// 4. Create PHP-FPM pool
	socket, err := a.phpManager.CreatePool(req.SiteID, req.Username, req.PHPVersion)
	if err != nil {
		a.userManager.DeleteUser(req.Username) // Rollback
		os.RemoveAll(filepath.Dir(req.DocumentRoot))
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to create PHP-FPM pool: %v", err)
		return nil
	}
	reply.PHPSocket = socket

	// 5. Create the placeholder page. Deliberately NOT phpinfo(): that leaks
	// paths, modules and settings to anyone who finds the fresh site. The
	// tiny PHP expression still proves PHP execution end-to-end.
	// 5. Yer tutucu sayfayı oluştur. Bilerek phpinfo() DEĞİL: o, taze siteyi
	// bulan herkese yolları, modülleri ve ayarları sızdırır. Küçük PHP
	// ifadesi yine de PHP'nin uçtan uca çalıştığını kanıtlar.
	indexContent := fmt.Sprintf(`<!doctype html>
<html lang="tr">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
  main{text-align:center;padding:2rem}
  h1{font-weight:600}
  p{color:#94a3b8}
</style>
</head>
<body>
<main>
  <h1>%s</h1>
  <p>Bu site hazırlanıyor. / This site is being prepared.</p>
  <p>CelikPanel · PHP <?php echo htmlspecialchars(PHP_VERSION); ?> · <?php echo date('Y'); ?></p>
</main>
</body>
</html>
`, req.Domain, req.Domain)
	os.WriteFile(filepath.Join(req.DocumentRoot, "index.php"), []byte(indexContent), 0644)
	a.userManager.SetOwnership(filepath.Join(req.DocumentRoot, "index.php"), req.Username)

	// 5b. Hosting permission layout: web-server group access, setgid
	// docroot, traverse-only parents — after the placeholder exists so file
	// modes are covered too. Best-effort in dev where the agent is not
	// root; in production a failure surfaces as the site not serving and
	// the log line makes it diagnosable.
	// 5b. Barındırma izin düzeni: web sunucusu grubuna erişim, setgid
	// docroot, yalnız-geçişli üst dizinler — dosya kipleri de kapsansın
	// diye yer tutucu oluştuktan sonra. Agent'ın root olmadığı dev'de
	// en-iyi-çaba; üretimde hata sitenin yayınlanmaması olarak görünür ve
	// günlük satırı teşhis ettirir.
	if err := applyHostingLayout(req.DocumentRoot, req.Username); err != nil {
		log.Printf("CreateSite %s: hosting layout: %v", req.Domain, err)
	}

	// 6. Generate Nginx vhost (after socket is created)
	site := &core.Site{
		ID:           req.SiteID,
		DomainID:     0, // Not needed for template
		DocumentRoot: req.DocumentRoot,
		PHPVersion:   req.PHPVersion,
		PHPFPMSocket: &socket,
		SSLEnabled:   req.SSLType != "none",
	}

	domain := &core.Domain{
		Name: req.Domain,
	}

	vhostConfig, err := a.nginxGen.GenerateVhost(site, domain, req.TempDomain)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to generate vhost: %v", err)
		return nil
	}
	reply.NginxConfig = vhostConfig

	// 7. Write Nginx vhost file
	err = a.nginxGen.WriteVhostFile(req.Domain, vhostConfig)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to write vhost: %v", err)
		return nil
	}

	// 8. Validate and reload Nginx
	err = a.nginxGen.ValidateNginx()
	if err != nil {
		a.nginxGen.DeleteVhost(req.Domain) // Rollback
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("nginx validation failed: %v", err)
		return nil
	}

	err = a.nginxGen.ReloadNginx()
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to reload nginx: %v", err)
		return nil
	}

	reply.Success = true
	return nil
}

type DeleteSiteRequest struct {
	SiteID     int    `json:"site_id"`
	Domain     string `json:"domain"`
	Username   string `json:"username"`
	PHPVersion string `json:"php_version"`
	SiteHome   string `json:"site_home"`
}

type DeleteSiteResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// DeleteSite tears down everything CreateSite built: app unit, vhost, PHP
// pool, system user and files. Idempotent — already-gone pieces are fine, so
// a half-failed delete can be retried. Because this runs as root, the paths
// and the user are strictly validated first: only site homes under the
// hosting base and only non-system users can be removed.
// DeleteSite, CreateSite'ın kurduğu her şeyi söker: uygulama unit'i, vhost,
// PHP havuzu, sistem kullanıcısı ve dosyalar. Idempotenttir — zaten olmayan
// parçalar sorun değildir; yarım kalmış silme yeniden denenebilir. Root
// olarak çalıştığı için önce yol ve kullanıcı sıkıca doğrulanır: yalnız
// barındırma kökü altındaki site dizinleri ve yalnız sistem-dışı
// kullanıcılar silinebilir.
func (a *Agent) DeleteSite(req *DeleteSiteRequest, resp *DeleteSiteResponse) error {
	cleanHome := filepath.Clean(req.SiteHome)
	if !strings.HasPrefix(cleanHome, "/var/www/celikpanel/subscriptions/") {
		resp.Error = fmt.Sprintf("refusing to delete outside the hosting base: %s", req.SiteHome)
		return nil
	}

	// 1. Supervised app unit (node projects) — harmless when absent.
	// 1. Denetimli uygulama unit'i (node projeleri) — yoksa zararsız.
	_ = a.RemoveAppUnit(&AppControlRequest{SiteID: req.SiteID}, &AppApplyResponse{})

	// 2. Vhost out first so nginx stops serving before files vanish.
	// 2. Önce vhost; dosyalar yok olmadan nginx sunmayı bıraksın.
	if req.Domain != "" {
		_ = a.nginxGen.DeleteVhost(req.Domain)
		_ = a.nginxGen.ReloadNginx()
	}

	// 3. PHP-FPM pool for this site.
	// 3. Bu sitenin PHP-FPM havuzu.
	if req.PHPVersion != "" {
		_ = a.phpManager.DeletePool(req.SiteID, req.PHPVersion)
	}

	// 4. System user — never a system account, even if asked.
	// 4. Sistem kullanıcısı — istense bile asla bir sistem hesabı değil.
	if req.Username != "" {
		if u, err := user.Lookup(req.Username); err == nil {
			if uid, _ := strconv.Atoi(u.Uid); uid >= 1000 {
				_ = exec.Command("pkill", "-u", req.Username).Run()
				time.Sleep(300 * time.Millisecond)
				if err := a.userManager.DeleteUser(req.Username); err != nil {
					log.Printf("DeleteSite %s: userdel: %v", req.Domain, err)
				}
			} else {
				log.Printf("DeleteSite %s: refusing to delete system user %q (uid %s)", req.Domain, req.Username, u.Uid)
			}
		}
	}

	// 5. Files — userdel -r usually removed the home already.
	// 5. Dosyalar — userdel -r genelde home'u zaten kaldırdı.
	if err := os.RemoveAll(cleanHome); err != nil {
		resp.Error = fmt.Sprintf("files could not be removed: %v", err)
		return nil
	}

	resp.Success = true
	return nil
}
