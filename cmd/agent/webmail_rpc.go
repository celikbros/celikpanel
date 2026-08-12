package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Roundcube is installed from its OWN official tarball, not a distro package
// (operator, 24 Jul: "if it can't be installed on both distros, don't — a
// webmail that runs on all Linux"). This is the Node.js pattern: a pinned
// version, a pinned SHA-256, an HTTPS download from GitHub verified before
// anything is unpacked. ONE path on every Linux — the distro-specific package
// (apt roundcube / pacman roundcubemail, different layouts) is exactly the
// D-004 trap this avoids. Bumping the version is a two-line change here plus a
// live re-hash, the same discipline as Node.
//
// Roundcube dağıtım paketinden değil KENDİ resmi tarball'ından kurulur
// (operatör, 24 Tem: "her iki sürümde kurulamıyorsa kurma — tüm Linux'ta
// çalışan webmail"). Bu Node.js desenidir: sabitlenmiş sürüm, sabitlenmiş
// SHA-256, açılmadan önce doğrulanan HTTPS indirmesi. Her Linux'ta TEK yol —
// dağıtıma özgü paket (apt roundcube / pacman roundcubemail, farklı yerleşim)
// tam da bunun kaçındığı D-004 tuzağıdır. Sürüm yükseltmek burada iki satır +
// canlı yeniden-hash; Node ile aynı disiplin.
const (
	roundcubeVersion = "1.6.15"
	roundcubeSHA256  = "48c9f212c77460132491f670abaf440b765c8276268349a690913764d26afbef"
	roundcubeMaxSize = 100 << 20
)

// webmailBaseDir is where the verified tarball is unpacked. It lives under
// /var/lib — NOT /opt/celikpanel — because nginx (running as the web user)
// must traverse into it to serve /webmail/, and /opt/celikpanel is the
// panel's own private tree that the web user cannot enter (caught live:
// nginx stat() failed with "permission denied" on the /opt path). /var/lib
// is the standard home for such served, mutable app data. The production
// mutation target is fixed; tests may reassign this package variable.
// webmailBaseDir, doğrulanmış tarball'ın açıldığı yerdir. /opt/celikpanel
// altında DEĞİL /var/lib altında yaşar; çünkü nginx (web kullanıcısı olarak)
// /webmail/'i sunmak için içine girebilmeli ve /opt/celikpanel, web
// kullanıcısının giremediği panelin özel ağacıdır (canlıda yakalandı: nginx
// /opt yolunda stat() "permission denied" verdi). Böyle sunulan, değişebilir
// uygulama verisinin standart evi /var/lib'dir. Üretim mutation hedefi sabittir.
var webmailBaseDir = "/var/lib/celikpanel-webmail"

func roundcubeInstalled() bool {
	installed, err := roundcubeInstallState()
	return err == nil && installed
}

func roundcubeInstallState() (bool, error) {
	return roundcubeInstallStateAt(webmailBaseDir)
}

func roundcubeInstallStateAt(baseDir string) (bool, error) {
	for _, path := range []string{
		filepath.Join(baseDir, "public_html", "index.php"),
		filepath.Join(baseDir, "config", "config.inc.php"),
		filepath.Join(baseDir, "db", "roundcube.sqlite3"),
	} {
		exists, err := secureMailFileExists(path)
		if err != nil {
			return false, fmt.Errorf("inspect Roundcube installation %s: %w", path, err)
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

// detectFPMSocket finds the default PHP-FPM socket across distros — Debian
// puts a versioned socket under /run/php (php8.4-fpm.sock), Arch a single one
// under /run/php-fpm. Its existence is the honest "is PHP-FPM running here"
// signal: services.DetectInstalledPHPVersion reads /etc/php/<v>/fpm and so
// returns "" on Arch even when PHP-FPM is up (caught live: Roundcube refused
// on Arch with "PHP-FPM is not installed", and /webmail/ 502'd on the wrong
// socket path). One probe answers both "installed?" and "where?".
// detectFPMSocket, varsayılan PHP-FPM soketini dağıtımlar arası bulur —
// Debian /run/php altında sürümlü bir soket koyar (php8.4-fpm.sock), Arch
// /run/php-fpm altında tek bir tane. Varlığı, dürüst "burada PHP-FPM çalışıyor
// mu" sinyalidir: services.DetectInstalledPHPVersion /etc/php/<v>/fpm okur ve
// Arch'ta PHP-FPM ayakta olsa bile "" döner (canlıda yakalandı: Roundcube
// Arch'ta "PHP-FPM is not installed" ile reddetti, /webmail/ yanlış soket
// yolunda 502 verdi). Tek yoklama hem "kurulu mu?" hem "nerede?" cevabı.
var fpmSocketPatterns = []string{
	"/run/php/php*-fpm.sock",    // Debian, versioned default www pool
	"/run/php-fpm/php-fpm.sock", // Arch
	"/run/php/php-fpm.sock",     // some layouts
	"/var/run/php/php*-fpm.sock",
	"/var/run/php-fpm/php-fpm.sock",
}

func detectFPMSocket() string {
	return detectFPMSocketWithPatterns(fpmSocketPatterns)
}

func detectFPMSocketWithPatterns(patterns []string) string {
	for _, pat := range patterns {
		if m, _ := filepath.Glob(pat); len(m) > 0 {
			return m[len(m)-1] // newest-versioned last, e.g. php8.4 over php8.3
		}
	}
	return ""
}

type InstallRoundcubeResponse = transport.InstallRoundcubeResponse

type WebmailMutationRequest = transport.WebmailMutationRequest

func appendRoundcubeInstallError(resp *InstallRoundcubeResponse, err error) {
	if err == nil {
		return
	}
	if resp.Error == "" {
		resp.Error = err.Error()
		return
	}
	resp.Error = fmt.Sprintf("%s; %v", resp.Error, err)
}

// InstallRoundcube downloads, verifies and configures Roundcube. Idempotent:
// an existing install returns success. On any failure the staging tree is
// discarded so a half-install is never served.
// InstallRoundcube, Roundcube'u indirir, doğrular ve yapılandırır.
// Idempotent: mevcut kurulum başarı döner. Herhangi bir hatada hazırlık ağacı
// atılır; yarım kurulum asla sunulmaz.
func (a *Agent) InstallRoundcube(req *WebmailMutationRequest, resp *InstallRoundcubeResponse) error {
	*resp = InstallRoundcubeResponse{}
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepInstallRoundcube, "roundcube", "", "install"),
	)
	if err != nil {
		*resp = InstallRoundcubeResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	resp.Version = roundcubeVersion
	if err := ensureRoundcubeLifecycleSupported(); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := secureMkdirAll(filepath.Dir(webmailBaseDir), 0o755); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := reconcileRoundcubeArtifacts(webmailBaseDir, ""); err != nil {
		resp.Error = fmt.Sprintf("reconcile Roundcube install artifacts: %v", err)
		return nil
	}
	installed, err := roundcubeInstallState()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if installed {
		resp.Installed = true
		return nil
	}
	// PHP-FPM presence is the socket's existence — portable across distros,
	// unlike the /etc/php version read. phpVer may still be "" on Arch and
	// that is fine: it is only used to name Debian's per-version sqlite
	// package, and Arch's sqlite package is unversioned.
	// PHP-FPM varlığı soketin varlığıdır — /etc/php sürüm okumasının aksine
	// dağıtımlar arası taşınabilir. phpVer Arch'ta yine "" olabilir ve bu
	// sorun değil: yalnız Debian'ın sürüm-başına sqlite paketini adlandırmak
	// için kullanılır, Arch'ın sqlite paketi sürümsüzdür.
	if detectFPMSocket() == "" {
		resp.Error = "PHP-FPM is not installed"
		return nil
	}
	phpVer := services.DetectInstalledPHPVersion()

	url := fmt.Sprintf("https://github.com/roundcube/roundcubemail/releases/download/%s/roundcubemail-%s-complete.tar.gz",
		roundcubeVersion, roundcubeVersion)
	client := &http.Client{Timeout: 5 * time.Minute}

	stage, err := createRoundcubeInstallStage(webmailBaseDir)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer func() {
		if _, err := retireRoundcubeTree(stage); err != nil {
			appendRoundcubeInstallError(resp, fmt.Errorf("clean Roundcube staging tree: %w", err))
		}
	}()
	tmp, err := os.CreateTemp(stage, "rc-dl-*")
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	tmpClosed := false
	defer func() {
		if !tmpClosed {
			appendRoundcubeInstallError(resp, tmp.Close())
		}
		if err := os.Remove(tmp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			appendRoundcubeInstallError(resp, fmt.Errorf("remove Roundcube download: %w", err))
		}
	}()

	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		resp.Error = fmt.Sprintf("download request failed: %v", err)
		return nil
	}
	dl, err := client.Do(downloadReq)
	if err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	defer func() {
		if err := dl.Body.Close(); err != nil {
			appendRoundcubeInstallError(resp, fmt.Errorf("close Roundcube download response: %w", err))
		}
	}()
	if dl.StatusCode != http.StatusOK {
		resp.Error = fmt.Sprintf("download failed: HTTP %d", dl.StatusCode)
		return nil
	}
	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(dl.Body, roundcubeMaxSize+1))
	if err != nil {
		resp.Error = fmt.Sprintf("download failed: %v", err)
		return nil
	}
	if written > roundcubeMaxSize {
		resp.Error = "download exceeded the maximum allowed Roundcube archive size"
		return nil
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != roundcubeSHA256 {
		resp.Error = "checksum mismatch — download discarded"
		return nil
	}

	// Extract to a staging dir, then move into place — a half-extracted tree
	// must never look installed (Node pattern).
	// Hazırlık dizinine aç, sonra yerine taşı — yarım açılmış ağaç asla kurulu
	// görünmemeli (Node deseni).
	if err := tmp.Sync(); err != nil {
		resp.Error = fmt.Sprintf("sync verified download: %v", err)
		return nil
	}
	closeErr := tmp.Close()
	tmpClosed = true
	if closeErr != nil {
		resp.Error = fmt.Sprintf("close verified download: %v", closeErr)
		return nil
	}
	if out, err := serviceMutationCommand(ctx, "tar", "-xzf", tmp.Name(), "-C", stage, "--strip-components=1").CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("extract failed: %v: %s", err, string(out))
		return nil
	}
	if err := os.Remove(tmp.Name()); err != nil {
		resp.Error = fmt.Sprintf("remove verified Roundcube download: %v", err)
		return nil
	}

	if err := writeRoundcubeConfig(stage, phpVer); err != nil {
		resp.Error = err.Error()
		return nil
	}
	// Roundcube's SQLite store needs PHP's pdo_sqlite, which neither distro
	// bundles with PHP and our PHP-FPM install did not pull in. This is where
	// a portable tarball still touches distro packages (the honest limit of
	// "one path on every Linux"): the app is distro-agnostic, its runtime
	// extension is not. Provide it distro-aware — the package name is the only
	// per-distro fact, and the agent already owns that.
	// Roundcube'un SQLite deposu PHP'nin pdo_sqlite'ına muhtaç; ne dağıtım onu
	// PHP'yle paketliyor ne de bizim PHP-FPM kurulumumuz çekti. Taşınabilir bir
	// tarball'ın yine de dağıtım paketlerine değdiği yer burası ("her Linux'ta
	// tek yol"un dürüst sınırı): uygulama dağıtımdan bağımsız, çalışma-zamanı
	// uzantısı değil. Dağıtım-farkındalıklı sağla — dağıtıma özgü tek gerçek
	// paket adıdır ve o bilgi zaten agent'ta.
	if !phpHasSQLite(ctx) {
		if err := a.ensurePHPSQLite(ctx, phpVer); err != nil {
			resp.Error = fmt.Sprintf("PHP SQLite extension is required for webmail and could not be installed: %v", err)
			return nil
		}
	}

	// Build the database and apply permissions inside the staging tree. The
	// generated config points at the final database path, while the schema
	// loader below explicitly opens the staging database. The final rename
	// therefore publishes a complete tree in one step.
	// Veritabanını ve izinleri hazırlık ağacında tamamla. Üretilen yapılandırma
	// son veritabanı yolunu gösterirken aşağıdaki şema yükleyici hazırlık
	// veritabanını açıkça kullanır. Böylece son rename yalnızca tam ağacı yayınlar.
	// Load the schema straight from Roundcube's SQL file, once. Its own
	// initdb.sh re-runs the CREATE block a second time and exits non-zero on
	// the resulting "table already exists" even though the 17 tables were
	// created correctly (verified live) — trusting that exit code would fail a
	// good install. A single PDO exec of sqlite.initial.sql is deterministic.
	// The DB lives in a dedicated db/ subdir because SQLite must WRITE a
	// journal next to the file, so the file's directory — not just the file —
	// has to be group-writable.
	// Şemayı Roundcube'un SQL dosyasından doğrudan, bir kez yükle. Kendi
	// initdb.sh'ı CREATE bloğunu ikinci kez koşturup oluşan "table already
	// exists" ile sıfırdan farklı çıkış verir — 17 tablo doğru oluşmuş olsa da
	// (canlıda doğrulandı); o çıkış koduna güvenmek iyi bir kurulumu bozardı.
	// sqlite.initial.sql'in tek PDO exec'i belirlenimcidir. DB, adanmış bir
	// db/ alt dizininde yaşar çünkü SQLite dosyanın yanına journal YAZMALIDIR;
	// yani yalnız dosya değil, dosyanın dizini de grup-yazılabilir olmalı.
	dbDir := filepath.Join(stage, "db")
	if err := secureMkdirAll(dbDir, 0o775); err != nil {
		resp.Error = err.Error()
		return nil
	}
	initSQL := filepath.Join(stage, "SQL", "sqlite.initial.sql")
	dbPath := filepath.Join(dbDir, "roundcube.sqlite3")
	phpLoad := fmt.Sprintf(
		`$db=new PDO("sqlite:%s"); $db->exec(file_get_contents("%s")); $n=$db->query("SELECT COUNT(*) FROM sqlite_master WHERE type='table'")->fetchColumn(); if($n<10){fwrite(STDERR,"only $n tables");exit(1);}`,
		dbPath, initSQL)
	if out, err := serviceMutationCommand(ctx, "php", "-r", phpLoad).CombinedOutput(); err != nil {
		resp.Error = fmt.Sprintf("db init failed: %v: %s", err, firstLine(string(out)))
		return nil
	}

	// The FPM pool that serves /webmail/ (the default www pool) must READ the
	// tree and WRITE the db dir (sqlite + its journal), temp and logs. Give the
	// tree to the web group; make exactly those three dirs group-writable —
	// never world.
	// /webmail/'i sunan FPM havuzu (varsayılan www havuzu) ağacı OKUMALI ve db
	// dizinine (sqlite + journal'ı), temp'e ve logs'a YAZMALIDIR. Ağacı web
	// grubuna ver; tam o üç dizini grup-yazılabilir yap — asla herkese değil.
	grp := webServerGroup()
	if grp == "" {
		resp.Error = "web server group could not be detected"
		return nil
	}
	if err := applyRoundcubePermissions(ctx, stage, dbPath, grp, runServiceMutationCombinedOutput); err != nil {
		resp.Error = err.Error()
		return nil
	}
	installed, err = roundcubeInstallStateAt(stage)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if !installed {
		resp.Error = "Roundcube staging tree is incomplete after installation"
		return nil
	}
	if err := publishRoundcubeStage(stage, webmailBaseDir); err != nil {
		resp.Error = err.Error()
		return nil
	}

	resp.Installed = true
	return nil
}

type webmailCommandRunner func(context.Context, string, ...string) ([]byte, error)

func applyRoundcubePermissions(
	ctx context.Context,
	baseDir string,
	dbPath string,
	groupName string,
	run webmailCommandRunner,
) error {
	if groupName == "" {
		return fmt.Errorf("web server group is required")
	}
	if run == nil {
		return fmt.Errorf("webmail command runner is required")
	}
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return fmt.Errorf("look up web server group %q: %w", groupName, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("parse web server group id %q: %w", group.Gid, err)
	}
	baseDir, err = validateRoundcubeTreePath(baseDir)
	if err != nil {
		return err
	}
	info, err := os.Lstat(baseDir)
	if err != nil {
		return fmt.Errorf("inspect Roundcube tree: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("Roundcube tree is not a real directory: %s", baseDir)
	}
	out, err := run(ctx, "chown", "-R", "root:"+groupName, "--", baseDir)
	if err != nil {
		return fmt.Errorf("set Roundcube tree ownership: %s", commandFailureDetail("chown", out, err))
	}
	if err := secureSetMailDirectoryMetadata(baseDir, 0o750, 0, gid); err != nil {
		return err
	}
	for _, name := range []string{"db", "temp", "logs"} {
		path := filepath.Join(baseDir, name)
		if err := secureSetMailDirectoryMetadata(path, 0o770, 0, gid); err != nil {
			return fmt.Errorf("secure Roundcube %s directory: %w", name, err)
		}
	}
	if err := secureSetMailFileMetadata(
		filepath.Join(baseDir, "config", "config.inc.php"),
		0o640,
		0,
		gid,
	); err != nil {
		return fmt.Errorf("secure Roundcube configuration: %w", err)
	}
	if err := secureSetMailFileMetadata(dbPath, 0o660, 0, gid); err != nil {
		return fmt.Errorf("secure Roundcube database: %w", err)
	}
	return nil
}

// writeRoundcubeConfig writes a minimal working config: a random des_key, an
// on-disk SQLite store (no database server to provision — the whole point of
// picking SQLite for a per-server webmail), and localhost IMAP/SMTP so it
// talks to the Dovecot/Postfix this panel installs.
// writeRoundcubeConfig, çalışan asgari bir config yazar: rastgele des_key,
// diskte SQLite deposu (kurulacak veritabanı sunucusu yok — sunucu-başı
// webmail için SQLite seçmenin bütün amacı) ve bu panelin kurduğu
// Dovecot/Postfix'e konuşması için localhost IMAP/SMTP.
func writeRoundcubeConfig(root, phpVer string) error {
	key := make([]byte, 18)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("des_key: %v", err)
	}
	// The DB lives in db/ (a group-writable subdir) so SQLite can write its
	// journal beside the file. / DB db/ içinde yaşar (grup-yazılabilir alt
	// dizin) ki SQLite journal'ını dosyanın yanına yazabilsin.
	dbPath := filepath.Join(webmailBaseDir, "db", "roundcube.sqlite3")
	conf := fmt.Sprintf(`<?php
// Managed by CelikPanel. Roundcube installed from the official tarball (D-004:
// one path on every Linux). SQLite store, localhost IMAP/SMTP.
$config = [];
$config['db_dsnw'] = 'sqlite:///%s?mode=0664';
$config['imap_host'] = 'localhost:143';
$config['smtp_host'] = 'localhost:587';
$config['smtp_user'] = '%%u';
$config['smtp_pass'] = '%%p';
$config['support_url'] = '';
$config['des_key'] = '%s';
$config['product_name'] = 'CelikPanel Webmail';
$config['plugins'] = ['archive', 'zipdownload'];
$config['skin'] = 'elastic';
$config['enable_installer'] = false;
`, dbPath, hex.EncodeToString(key))
	// The staging tree's config path (moved into place with the tree).
	// Hazırlık ağacının config yolu (ağaçla birlikte yerine taşınır).
	return secureWriteConfig(filepath.Join(root, "config", "config.inc.php"), []byte(conf), 0o640)
}

// phpHasSQLite reports whether the PHP CLI can open a SQLite PDO — the exact
// capability Roundcube's initdb needs. Asking PHP itself (not guessing from a
// package name) is the honest check: it is true the moment the extension is
// loadable, whatever installed it.
// phpHasSQLite, PHP CLI'ın bir SQLite PDO açıp açamadığını bildirir —
// Roundcube'un initdb'sinin tam ihtiyacı. Paket adından tahmin etmek yerine
// PHP'nin kendisine sormak dürüst denetimdir: uzantı yüklenebilir olduğu an
// doğrudur, onu ne kurmuş olursa olsun.
func phpHasSQLite(ctx context.Context) bool {
	out, err := serviceMutationCommand(ctx, "php", "-r", `echo in_array("sqlite", PDO::getAvailableDrivers()) ? "yes" : "no";`).Output()
	return err == nil && string(out) == "yes"
}

// ensurePHPSQLite installs PHP's SQLite extension for the running PHP. The
// package name is the one distro-specific fact: Debian/Sury ships a
// per-version php<ver>-sqlite3; Arch has a single php-sqlite. dnf mirrors
// Debian's shape. After install, php-fpm is reloaded so a served request sees
// the new extension too (the CLI initdb would see it without a reload, but the
// webmail that follows runs under FPM).
// ensurePHPSQLite, çalışan PHP için PHP'nin SQLite uzantısını kurar. Paket adı
// dağıtıma özgü tek gerçektir: Debian/Sury sürüm-başına php<ver>-sqlite3
// taşır; Arch'ta tek php-sqlite. dnf, Debian'ın biçimini yansıtır. Kurulumdan
// sonra php-fpm yeniden yüklenir ki sunulan bir istek de yeni uzantıyı görsün
// (CLI initdb reload olmadan görürdü ama ardından gelen webmail FPM altında
// çalışır).
func (a *Agent) ensurePHPSQLite(ctx context.Context, phpVer string) error {
	family := detectPkgFamily()
	switch family {
	case "apt", "dnf":
		if phpVer == "" {
			return fmt.Errorf("PHP version could not be detected")
		}
		// Debian/RHEL: the per-version package auto-enables via conf.d.
		// Debian/RHEL: sürüm-başına paket conf.d ile kendini etkinleştirir.
		if _, err := installPackagesContext(ctx, family, []string{fmt.Sprintf("php%s-sqlite3", phpVer)}); err != nil {
			return err
		}
	case "pacman":
		// Arch ships the .so but does NOT enable it — its philosophy leaves
		// that to the operator (caught live: package installed, `php -m` still
		// had no sqlite). Enable it ourselves via a scanned conf.d drop-in.
		// Arch .so'yu getirir ama etkinleştirMEZ — felsefesi bunu operatöre
		// bırakır (canlıda yakalandı: paket kurulu, `php -m`'de hâlâ sqlite
		// yok). Taranan bir conf.d drop-in ile kendimiz etkinleştiririz.
		if _, err := installPackagesContext(ctx, family, []string{"php-sqlite"}); err != nil {
			return err
		}
		configured := false
		for _, dir := range []string{"/etc/php/conf.d", "/etc/php8/conf.d", "/etc/php/php.d"} {
			fi, err := os.Lstat(dir)
			if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
				continue
			}
			if err := secureWriteConfig(
				filepath.Join(dir, "celikpanel-sqlite.ini"),
				[]byte("extension=pdo_sqlite\nextension=sqlite3\n"),
				0o644,
			); err != nil {
				return fmt.Errorf("enable PHP SQLite extension: %w", err)
			}
			configured = true
			break
		}
		if !configured {
			return fmt.Errorf("PHP configuration directory could not be found")
		}
	default:
		return fmt.Errorf("unsupported package manager for this distro")
	}

	// Reload FPM so a served request sees the new extension. Best-effort — the
	// CLI initdb that follows picks it up regardless of the reload.
	// Sunulan istek yeni uzantıyı görsün diye FPM'i yeniden yükle. En-iyi-çaba
	// — ardından gelen CLI initdb reload'dan bağımsız uzantıyı alır.
	if phpVer != "" && family == "apt" {
		if out, err := runServiceMutationCombinedOutput(ctx, "systemctl", "reload", "php"+phpVer+"-fpm"); err != nil {
			return fmt.Errorf("PHP-FPM reload failed: %s", commandFailureDetail("systemctl reload", out, err))
		}
	} else {
		if out, err := runServiceMutationCombinedOutput(ctx, "systemctl", "reload", "php-fpm"); err != nil {
			return fmt.Errorf("PHP-FPM reload failed: %s", commandFailureDetail("systemctl reload", out, err))
		}
	}

	// Verify the driver is actually loadable now — better a clear failure here
	// than a cryptic "could not find driver" from initdb two steps later.
	// Sürücünün artık gerçekten yüklenebilir olduğunu doğrula — iki adım sonra
	// initdb'den gelen anlaşılmaz "could not find driver" yerine burada net
	// bir başarısızlık iyidir.
	if !phpHasSQLite(ctx) {
		return fmt.Errorf("installed but the pdo_sqlite driver is still not loadable")
	}
	return nil
}

type RemoveRoundcubeResponse = transport.RemoveRoundcubeResponse

type roundcubeRetirementResult struct {
	Removed         bool
	MutationApplied bool
}

// RemoveRoundcube deletes the whole webmail tree — the fixed base dir means
// there is nothing else it could delete. Idempotent.
// RemoveRoundcube tüm webmail ağacını siler — sabit taban dizini, silebileceği
// başka bir şey olmadığı anlamına gelir. Idempotenttir.
func (a *Agent) RemoveRoundcube(req *WebmailMutationRequest, resp *RemoveRoundcubeResponse) error {
	*resp = RemoveRoundcubeResponse{}
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	_, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepRemoveRoundcube, "roundcube", "", "remove"),
	)
	if err != nil {
		*resp = RemoveRoundcubeResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	result, err := retireRoundcubeTree(webmailBaseDir)
	resp.Removed = result.Removed
	resp.MutationApplied = result.MutationApplied
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	return nil
}

// Serving Roundcube webmail. Unlike the database tools (admin-only, behind a
// panel session), webmail is for END USERS: a customer with only a mailbox —
// no panel account — signs in with their email credentials. So the panel
// fronts it at a PUBLIC /webmail/ path (Roundcube's own login is the auth),
// and the agent serves it only on a root-bound Unix socket. The split from the
// TCP-based db-tools server is deliberate: different audience, different
// access rule, so a change to one can never widen the other.
//
// Roundcube webmail sunumu. Veritabanı araçlarının (yalnız-admin, panel
// oturumu arkasında) aksine webmail SON KULLANICI içindir: yalnız posta
// kutusu olan bir müşteri — panel hesabı olmadan — e-posta bilgileriyle girer.
// Bu yüzden panel onu PUBLIC bir /webmail/ yolunda önler (kimlik doğrulama
// Roundcube'un kendi girişidir) ve agent onu yalnız root'un bağlayabildiği
// Unix socket'te sunar. db-araçlarının TCP sunucusundan ayrı olması
// bilinçlidir: farklı kitle, farklı erişim kuralı; birine yapılan değişiklik
// diğerini asla genişletemez.

const (
	webmailConfPath = "/etc/nginx/conf.d/celikpanel-webmail.conf"
)

var webmailSetConfigMetadata = secureSetMailFileMetadata

func webmailSocketPathForNginx() (string, error) {
	return validateWebmailSocketPath(transport.WebmailSocketPath())
}

func validateWebmailSocketPath(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		len(path) > 100 || filepath.Ext(path) != ".sock" {
		return "", fmt.Errorf("invalid webmail socket path")
	}
	for _, r := range path {
		if !(r == '/' || r == '-' || r == '_' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')) {
			return "", fmt.Errorf("invalid webmail socket path")
		}
	}
	return path, nil
}

func webmailConfigMetadataIdentity(path string) (int, int) {
	if path == webmailConfPath {
		return 0, 0
	}
	return -1, -1
}

func removeInactiveWebmailSocket() error {
	return removeInactiveWebmailSocketAt(transport.WebmailSocketPath())
}

func removeInactiveWebmailSocketAt(path string) error {
	path, err := validateWebmailSocketPath(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect inactive webmail socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refuse to remove non-socket webmail path")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove inactive webmail socket: %w", err)
	}
	return nil
}

func applyWebmailNginxMutation(
	ctx context.Context,
	path string,
	content []byte,
	present bool,
	run webmailCommandRunner,
) error {
	if run == nil {
		return fmt.Errorf("webmail command runner is required")
	}
	snapshot, err := snapshotMailFile(path)
	if err != nil {
		return fmt.Errorf("snapshot webmail nginx configuration: %w", err)
	}

	rollback := func(cause error, restoreRuntime bool) error {
		var rollbackErrs []error
		if err := restoreMailFile(snapshot); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		} else if restoreRuntime {
			if out, err := run(ctx, "nginx", "-t"); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf(
					"validate restored nginx configuration: %s",
					commandFailureDetail("nginx -t", out, err),
				))
			} else if out, err := run(ctx, "systemctl", "reload", "nginx"); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf(
					"reload restored nginx configuration: %s",
					commandFailureDetail("systemctl reload nginx", out, err),
				))
			}
		}
		if len(rollbackErrs) == 0 {
			return cause
		}
		return errors.Join(cause, fmt.Errorf("webmail nginx rollback failed: %w", errors.Join(rollbackErrs...)))
	}

	if present {
		if err := secureWriteConfig(path, content, 0o600); err != nil {
			return rollback(fmt.Errorf("write webmail nginx configuration: %w", err), false)
		}
		// Unit tests and unprivileged development use a private temporary
		// config path and leave its owner unchanged. Production's fixed /etc
		// path is always explicitly reset to root:root.
		uid, gid := webmailConfigMetadataIdentity(path)
		if err := webmailSetConfigMetadata(path, 0o600, uid, gid); err != nil {
			return rollback(fmt.Errorf("secure webmail nginx configuration metadata: %w", err), false)
		}
	} else if snapshot.exists {
		if err := secureRemoveConfig(path); err != nil {
			return rollback(fmt.Errorf("remove webmail nginx configuration: %w", err), false)
		}
	}

	if out, err := run(ctx, "nginx", "-t"); err != nil {
		return rollback(fmt.Errorf(
			"nginx rejected the webmail configuration: %s",
			commandFailureDetail("nginx -t", out, err),
		), false)
	}
	if out, err := run(ctx, "systemctl", "reload", "nginx"); err != nil {
		return rollback(fmt.Errorf(
			"nginx reload failed: %s",
			commandFailureDetail("systemctl reload nginx", out, err),
		), true)
	}
	if !present && path == webmailConfPath {
		if err := removeInactiveWebmailSocket(); err != nil {
			return rollback(err, true)
		}
	}
	return nil
}

// roundcubeRoot is the public entry point inside our own tarball tree. The
// complete tarball's public_html holds index.php with skins/plugins symlinked
// up one level and program/ real; nginx follows the symlinks, so this single
// root serves the whole app.
// roundcubeRoot, kendi tarball ağacımızın içindeki genel giriş noktasıdır.
// Complete tarball'ın public_html'i index.php'yi tutar; skins/plugins bir üst
// düzeye symlink, program/ gerçek dizindir; nginx symlink'leri izler, bu tek
// kök tüm uygulamayı sunar.
func roundcubeRootDir() string { return filepath.Join(webmailBaseDir, "public_html") }

func renderWebmailNginxConfig(webmailSocket, roundcubeRoot, fpmSocket string) string {
	return fmt.Sprintf(`# Managed by CelikPanel — Roundcube webmail, Unix socket only.
# The panel reverse-proxies /webmail/ here; Roundcube's own login is the auth.
server {
    listen unix:%s;
    server_name _;
    client_max_body_size 25m;

    location /webmail/ {
        alias %s/;
        index index.php;
        try_files $uri $uri/ /webmail/index.php$is_args$args;

        location ~ ^/webmail/(.+\.php)$ {
            alias %s/$1;
            include fastcgi_params;
            fastcgi_param SCRIPT_FILENAME $request_filename;
            fastcgi_pass unix:%s;
        }
    }
}
`, webmailSocket, roundcubeRoot, roundcubeRoot, fpmSocket)
}

type ConfigureWebmailResponse = transport.ConfigureWebmailResponse

// ConfigureWebmail (re)writes the Unix-socket nginx server when Roundcube is
// installed, and removes it when it is not — the mirror of ConfigureDBTools,
// called by the panel after roundcube is installed or uninstalled. Idempotent.
// ConfigureWebmail, Roundcube kuruluyken Unix-socket nginx sunucusunu (yeniden)
// yazar, değilken kaldırır — ConfigureDBTools'un aynası; panel roundcube
// kurulunca/kaldırılınca çağırır. Idempotenttir.
func (a *Agent) ConfigureWebmail(req *WebmailMutationRequest, resp *ConfigureWebmailResponse) error {
	*resp = ConfigureWebmailResponse{}
	if req == nil {
		resp.Error = "missing request"
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepConfigureWebmail, "roundcube", "", "configure"),
	)
	if err != nil {
		*resp = ConfigureWebmailResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	if _, err := exec.LookPath("nginx"); err != nil {
		resp.Error = "nginx is not installed"
		return nil
	}

	// Presence is the entry point's existence — InstallRoundcube renames the
	// tree into place only after a successful build, so a half-install is
	// never served.
	// Varlık, giriş noktasının varlığıdır — InstallRoundcube ağacı yalnız
	// başarılı yapımdan sonra yerine taşır; yarım kurulum asla sunulmaz.
	installed, err := roundcubeInstallState()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if !installed {
		if err := applyWebmailNginxMutation(
			ctx,
			webmailConfPath,
			nil,
			false,
			runServiceMutationCombinedOutput,
		); err != nil {
			resp.Error = err.Error()
			return nil
		}
		resp.Configured = true
		resp.Present = false
		return nil
	}
	resp.Present = true

	// Roundcube is PHP: hand .php to the default FPM socket. Detected across
	// distros (Debian /run/php/php<v>-fpm.sock, Arch /run/php-fpm/php-fpm.sock)
	// — never assume the Debian path.
	// Roundcube PHP'dir: .php'yi varsayılan FPM soketine ver. Dağıtımlar arası
	// tespit edilir (Debian /run/php/php<v>-fpm.sock, Arch
	// /run/php-fpm/php-fpm.sock) — Debian yolunu asla varsayma.
	socket := detectFPMSocket()
	if socket == "" {
		resp.Error = "PHP-FPM is not installed"
		return nil
	}
	webmailSocket, err := webmailSocketPathForNginx()
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	// Path-preserving: the browser path (/webmail/…) and this server's path
	// are identical, so Roundcube's own absolute URLs survive the panel proxy
	// without rewriting — the same trick the db-tools server uses.
	// Yol-koruyan: tarayıcı yolu (/webmail/…) ile bu sunucunun yolu aynıdır;
	// böylece Roundcube'un mutlak URL'leri panel vekilinden yeniden yazım
	// olmadan sağ çıkar — db-araçları sunucusunun kullandığı aynı numara.
	conf := renderWebmailNginxConfig(webmailSocket, roundcubeRootDir(), socket)

	if err := applyWebmailNginxMutation(
		ctx,
		webmailConfPath,
		[]byte(conf),
		true,
		runServiceMutationCombinedOutput,
	); err != nil {
		resp.Error = err.Error()
		return nil
	}
	// Validate before reload — a broken webmail config must not take the web
	// server (and every customer site) down with it.
	// Yeniden yüklemeden önce doğrula — bozuk bir webmail yapılandırması web
	// sunucusunu (ve tüm müşteri sitelerini) beraberinde düşürmemeli.
	resp.Configured = true
	return nil
}
