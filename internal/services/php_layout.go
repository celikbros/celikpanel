package services

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// Where PHP-FPM keeps its files on this host (11 Oct 2026).
//
// Every path of a site's PHP-FPM pool was built for one layout: Debian's and
// Sury's, where each PHP version has its own tree, unit and program
// (`/etc/php/8.4/fpm/pool.d/`, `php8.4-fpm.service`, `php-fpm8.4`,
// `/run/php/`). Measured on Arch: creating a PHP site, and therefore every
// cPanel import, failed with "open /etc/php/8.5/fpm/pool.d/...: no such file or
// directory" and then tried to reload a unit `php8.5-fpm` that does not exist.
//
// Arch ships one unversioned PHP-FPM. The rest of this codebase already
// records that layout, each fact where it is used:
//
//   - one unit, `php-fpm`, whose version only the program knows
//     (cmd/agent/instance_rpc.go, internal/core/managed_services.go);
//   - `/etc/php` without version directories (the same file);
//   - the stock pool listens under `/run/php-fpm/` (cmd/agent/webmail_rpc.go);
//   - the web server's account is `http`, not `www-data`
//     (cmd/agent/hosting_layout.go, internal/hostingpath).
//
// and the package keeps its pools in `/etc/php/php-fpm.d/`, which
// `/etc/php/php-fpm.conf` includes.
//
// The layout is read from the host, the way the instance listing reads it, not
// from the distribution's name: a version with its own tree under `/etc/php`
// is the versioned layout; a host without that tree but with
// `/etc/php/php-fpm.d` is the single-unit layout. Anything else stays the
// versioned layout and fails as it did, naming the path it looked for.
//
// PHP-FPM'in bu sunucuda dosyalarını nerede tuttuğu. Bir sitenin PHP-FPM
// havuzunun her yolu tek bir düzen için kurulmuştu: her PHP sürümünün kendi
// ağacı, unit'i ve programı olan Debian/Sury düzeni. Arch'ta ölçüldü: PHP
// sitesi oluşturmak, dolayısıyla her cPanel içe aktarımı başarısız oluyordu.
// Arch tek ve sürümsüz bir PHP-FPM gönderir. Düzen dağıtımın adından değil,
// sunucudan okunur.
type phpFPMLayout struct {
	// singleUnit: one unversioned PHP-FPM (Arch).
	singleUnit bool
	version    string
}

func phpIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// phpLayoutFor answers the layout that holds PHP `version` on this host.
func phpLayoutFor(version string) phpFPMLayout {
	layout := phpFPMLayout{version: version}
	if phpIsDir(filepath.Join(phpEtcDir, version)) {
		return layout
	}
	layout.singleUnit = phpIsDir(filepath.Join(phpEtcDir, "php-fpm.d"))
	return layout
}

// poolDir is the directory PHP-FPM reads pool files from.
func (l phpFPMLayout) poolDir() string {
	if l.singleUnit {
		return filepath.Join(phpEtcDir, "php-fpm.d")
	}
	return filepath.Join(phpEtcDir, l.version, "fpm", "pool.d")
}

// iniPath is the php.ini the FPM service reads.
func (l phpFPMLayout) iniPath() string {
	if l.singleUnit {
		return filepath.Join(phpEtcDir, "php.ini")
	}
	return filepath.Join(phpEtcDir, l.version, "fpm", "php.ini")
}

// unit is the systemd unit that runs this PHP-FPM.
func (l phpFPMLayout) unit() string {
	if l.singleUnit {
		return "php-fpm"
	}
	return fmt.Sprintf("php%s-fpm", l.version)
}

// program is the FPM program whose `-t` reads the configuration.
func (l phpFPMLayout) program() string {
	if l.singleUnit {
		return "php-fpm"
	}
	return "php-fpm" + l.version
}

// socketDir is the runtime directory the packaged service creates and in
// which its own stock pool listens; a site's pool listens beside it.
func (l phpFPMLayout) socketDir() string {
	if l.singleUnit {
		return "/run/php-fpm"
	}
	return "/var/run/php"
}

func (l phpFPMLayout) socket(poolName string) string {
	return fmt.Sprintf("%s/php%s-fpm-%s.sock", l.socketDir(), l.version, poolName)
}

// PHPFPMSocketPath is the socket a site's pool listens on for PHP `version`
// on this host. The pool file, the web server's virtual host and the Panel's
// record of the site must all name this one path.
// PHPFPMSocketPath, bir sitenin havuzunun bu sunucuda dinlediği sokettir. Havuz
// dosyası, web sunucusunun sanal konağı ve Panel'in kaydı aynı yolu adlandırır.
func PHPFPMSocketPath(version, poolName string) string {
	return phpLayoutFor(version).socket(poolName)
}

// phpLookupGroup reports whether a system group exists. Swapped by tests.
var phpLookupGroup = func(name string) bool {
	_, err := user.LookupGroup(name)
	return err == nil
}

// phpWebServerAccount is the account the web server runs as, which must be
// able to open a pool's socket: Debian `www-data`, RHEL `nginx`, Arch `http`.
// The first that exists wins, in the order the rest of the product uses
// (cmd/agent/hosting_layout.go); `www-data` when none could be found, which is
// what every pool file was written with before.
func phpWebServerAccount() string {
	for _, name := range []string{"www-data", "nginx", "http"} {
		if phpLookupGroup(name) {
			return name
		}
	}
	return "www-data"
}
