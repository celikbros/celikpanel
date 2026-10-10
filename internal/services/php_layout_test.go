package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The PHP-FPM layout is read from the host (11 Oct 2026). Measured on Arch:
// a PHP site could not be created because the pool was written to
// /etc/php/8.5/fpm/pool.d/, which exists only on Debian and Sury, and the
// reload named a unit `php8.5-fpm` that Arch does not have.

func withPHPHost(t *testing.T, groups ...string) (root string, reloads *[]string) {
	t.Helper()
	root = t.TempDir()
	oldEtc, oldReload, oldTest, oldGroup := phpEtcDir, reloadPHPFPM, phpFPMConfigTest, phpLookupGroup
	t.Cleanup(func() {
		phpEtcDir, reloadPHPFPM, phpFPMConfigTest, phpLookupGroup = oldEtc, oldReload, oldTest, oldGroup
	})
	phpEtcDir = root
	reloads = &[]string{}
	// The unit the production reload would name, without running systemctl.
	reloadPHPFPM = func(version string) error {
		*reloads = append(*reloads, phpLayoutFor(version).unit())
		return nil
	}
	phpFPMConfigTest = func(string) error { return nil }
	phpLookupGroup = func(name string) bool {
		for _, group := range groups {
			if group == name {
				return true
			}
		}
		return false
	}
	return root, reloads
}

func mustMkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPHPLayoutIsReadFromTheHost(t *testing.T) {
	// Debian and Sury: one tree, unit and program per version.
	root, _ := withPHPHost(t, "www-data")
	mustMkdir(t, root, "8.4", "fpm", "pool.d")
	debian := phpLayoutFor("8.4")
	if debian.singleUnit ||
		debian.poolDir() != filepath.Join(root, "8.4", "fpm", "pool.d") ||
		debian.iniPath() != filepath.Join(root, "8.4", "fpm", "php.ini") ||
		debian.unit() != "php8.4-fpm" || debian.program() != "php-fpm8.4" ||
		PHPFPMSocketPath("8.4", "site12") != "/var/run/php/php8.4-fpm-site12.sock" {
		t.Fatalf("Debian layout = %+v pool %s ini %s unit %s program %s socket %s", debian, debian.poolDir(), debian.iniPath(),
			debian.unit(), debian.program(), PHPFPMSocketPath("8.4", "site12"))
	}
	// A version tree wins even when a php-fpm.d directory also exists.
	mustMkdir(t, root, "php-fpm.d")
	if phpLayoutFor("8.4").singleUnit {
		t.Fatal("a host with the version's own tree was read as the single-unit layout")
	}

	// Arch: /etc/php without version directories, pools in php-fpm.d, one
	// unit and one program, the runtime directory /run/php-fpm.
	root, _ = withPHPHost(t, "http")
	mustMkdir(t, root, "php-fpm.d")
	mustMkdir(t, root, "conf.d")
	arch := phpLayoutFor("8.5")
	if !arch.singleUnit ||
		arch.poolDir() != filepath.Join(root, "php-fpm.d") ||
		arch.iniPath() != filepath.Join(root, "php.ini") ||
		arch.unit() != "php-fpm" || arch.program() != "php-fpm" ||
		PHPFPMSocketPath("8.5", "site3") != "/run/php-fpm/php8.5-fpm-site3.sock" {
		t.Fatalf("Arch layout = %+v pool %s ini %s unit %s program %s socket %s", arch, arch.poolDir(), arch.iniPath(),
			arch.unit(), arch.program(), PHPFPMSocketPath("8.5", "site3"))
	}
	if poolFilePath("8.5", "site3") != filepath.Join(root, "php-fpm.d", "site3.conf") || phpINIPath("8.5") != filepath.Join(root, "php.ini") {
		t.Fatalf("pool file %s, ini %s", poolFilePath("8.5", "site3"), phpINIPath("8.5"))
	}

	// Neither tree: nothing is guessed. The versioned paths are named, and
	// the operation fails on the path it looked for, as before.
	root, _ = withPHPHost(t)
	if layout := phpLayoutFor("8.3"); layout.singleUnit || layout.poolDir() != filepath.Join(root, "8.3", "fpm", "pool.d") {
		t.Fatalf("layout without any tree = %+v", layout)
	}
}

func TestPHPWebServerAccountIsTheOneThatExists(t *testing.T) {
	for _, c := range []struct {
		groups []string
		want   string
	}{
		{[]string{"www-data"}, "www-data"},
		{[]string{"http"}, "http"},
		{[]string{"nginx", "http"}, "nginx"},
		{nil, "www-data"},
	} {
		withPHPHost(t, c.groups...)
		if got := phpWebServerAccount(); got != c.want {
			t.Fatalf("groups %v: account %q, want %q", c.groups, got, c.want)
		}
	}
}

// The measured sequence, on the Arch layout: create a site's pool, read it,
// change it, delete it. Every file is in /etc/php/php-fpm.d, the socket is in
// /run/php-fpm and belongs to `http`, and every reload names `php-fpm`.
func TestSitePoolLifecycleOnTheSingleUnitLayout(t *testing.T) {
	root, reloads := withPHPHost(t, "http")
	poolDir := mustMkdir(t, root, "php-fpm.d")
	manager, err := NewPHPFPMManager()
	if err != nil {
		t.Fatal(err)
	}

	socket, err := manager.CreatePool(3, "celik_site3", "8.5")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if socket != "/run/php-fpm/php8.5-fpm-site3.sock" {
		t.Fatalf("socket = %s", socket)
	}
	body, err := os.ReadFile(filepath.Join(poolDir, "site3.conf"))
	if err != nil {
		t.Fatalf("the pool file is not in php-fpm.d: %v", err)
	}
	for _, want := range []string{"[site3]", "user = celik_site3", "listen = /run/php-fpm/php8.5-fpm-site3.sock", "listen.owner = http", "listen.group = http"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("pool file lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), "www-data") || strings.Contains(string(body), "/var/run/php/") {
		t.Fatalf("the pool file carries the Debian layout:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(root, "8.5")); !os.IsNotExist(err) {
		t.Fatalf("a version tree was created under /etc/php: %v", err)
	}

	config, err := manager.PoolManager.GetPoolConfig("8.5", "site3")
	if err != nil {
		t.Fatalf("read the pool back: %v", err)
	}
	if config.Listen != socket || config.ListenOwner != "http" || config.User != "celik_site3" {
		t.Fatalf("config = %+v", config)
	}
	pools, err := manager.ListPools("8.5")
	if err != nil || len(pools) != 1 {
		t.Fatalf("pools = %+v, err = %v", pools, err)
	}

	if err := manager.DeletePool(3, "8.5"); err != nil {
		t.Fatalf("delete pool: %v", err)
	}
	if _, err := os.Stat(filepath.Join(poolDir, "site3.conf")); !os.IsNotExist(err) {
		t.Fatalf("the pool file survived its deletion: %v", err)
	}
	if len(*reloads) < 2 {
		t.Fatalf("reloads = %v, want one after the create and one after the delete", *reloads)
	}
	for _, unit := range *reloads {
		if unit != "php-fpm" {
			t.Fatalf("a reload named %q; Arch has the single unit php-fpm (all: %v)", unit, *reloads)
		}
	}
}

// The same lifecycle on the Debian layout is unchanged.
func TestSitePoolOnTheVersionedLayoutIsUnchanged(t *testing.T) {
	root, reloads := withPHPHost(t, "www-data")
	poolDir := mustMkdir(t, root, "8.4", "fpm", "pool.d")
	manager, err := NewPHPFPMManager()
	if err != nil {
		t.Fatal(err)
	}
	socket, err := manager.CreatePool(12, "celik_site12", "8.4")
	if err != nil || socket != "/var/run/php/php8.4-fpm-site12.sock" {
		t.Fatalf("socket = %s, err = %v", socket, err)
	}
	body, err := os.ReadFile(filepath.Join(poolDir, "site12.conf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"listen = /var/run/php/php8.4-fpm-site12.sock", "listen.owner = www-data", "listen.group = www-data"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("pool file lacks %q:\n%s", want, body)
		}
	}
	if len(*reloads) != 1 || (*reloads)[0] != "php8.4-fpm" {
		t.Fatalf("reloads = %v", *reloads)
	}
}

// What `mariadbd --version` prints, and what a client prints. Only the server
// program's own line is a server version (11 Oct 2026; measured on Ubuntu
// 24.04: the client's "15.1" was shown as the version of a 10.11.14 server).
func TestMariaDBVersionComesFromTheServerNeverTheClient(t *testing.T) {
	// Pairs of what was printed and the version it holds. A list, not a map
	// literal: the lines are long, and how a formatter aligns such a literal
	// differs between Go releases.
	deprecated := "mysql: Deprecated program name. It will be removed in a future release, use '/usr/bin/mariadb' instead\n"
	for _, c := range [][2]string{
		{"mariadbd  Ver 10.11.14-MariaDB-0ubuntu0.24.04.1 for debian-linux-gnu on x86_64 (Ubuntu 24.04)\n", "10.11.14"},
		{"/usr/sbin/mariadbd  Ver 11.8.6-MariaDB-0+deb13u1 for debian-linux-gnu on x86_64 (from Debian)\n", "11.8.6"},
		{"mariadbd  Ver 12.0.2-MariaDB for Linux on x86_64 (Arch Linux)\n", "12.0.2"},
		{"mysqld  Ver 8.0.39-0ubuntu0.24.04.2 for Linux on x86_64 ((Ubuntu))\n", "8.0.39"},
		{"mariadb  Ver 15.1 Distrib 10.11.14-MariaDB, for debian-linux-gnu (x86_64) using  EditLine wrapper\n", ""},
		{"mariadb from 11.8.6-MariaDB, client 15.2 for debian-linux-gnu (x86_64) using  EditLine wrapper\n", ""},
		{deprecated, ""},
		{"", ""},
	} {
		if got := mariaDBServerProgramVersion(c[0]); got != c[1] {
			t.Fatalf("%q: version %q, want %q", c[0], got, c[1])
		}
	}

	// The running server's answer is found by its own mark, whatever else the
	// client prints. On Arch the client first says its name is deprecated,
	// and the column name was recorded as the version.
	for _, c := range [][2]string{
		{"CONCAT('version=', VERSION())\nversion=11.8.6-MariaDB-0+deb13u1 from Debian\n", "11.8.6-MariaDB-0+deb13u1 from Debian"},
		{deprecated + "CONCAT('version=', VERSION())\nversion=12.0.2-MariaDB\n", "12.0.2-MariaDB"},
		{"version=10.11.14-MariaDB-0ubuntu0.24.04.1\n", "10.11.14-MariaDB-0ubuntu0.24.04.1"},
		{"VERSION()\n", ""},
		{"CONCAT('version=', VERSION())\nversion=\n", ""},
		{"", ""},
	} {
		got := mariaDBMarkedVersion(c[0])
		if got != c[1] {
			t.Fatalf("%q: version %q, want %q", c[0], got, c[1])
		}
		if got == "VERSION()" || strings.HasPrefix(got, "CONCAT") {
			t.Fatalf("the column name was taken for the version: %q", got)
		}
	}
}
