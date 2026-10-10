package services

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Corrections from the final native round (12 Oct 2026).

// What nginx printed on the Arch guest when the PHP vhost named Debian's
// snippet file (evidence set3-20261012, rid3-arch/run-a).
const archNginxRefusal = `nginx: [emerg] open() "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27
nginx: configuration file /etc/nginx/nginx.conf test failed`

func exitStatus(t *testing.T, code string) error {
	t.Helper()
	// A real exit status, the way a refused `nginx -t` returns one.
	err := exec.Command("sh", "-c", "exit "+code).Run()
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		t.Skipf("no shell to produce an exit status: %v", err)
	}
	return err
}

// `nginx -t` that exits with its own status is "nginx refused the
// configuration", with the line that names what it refused; a test that could
// not be run or ran out of time is not.
func TestNginxRefusalIsTypedOnlyForItsOwnExitStatus(t *testing.T) {
	refusal := nginxValidationError([]byte("\n"+archNginxRefusal+"\n"), exitStatus(t, "1"))
	var refused *NginxConfigRefusedError
	if !errors.As(refusal, &refused) {
		t.Fatalf("a refused nginx -t is not typed: %v", refusal)
	}
	if want := `nginx: [emerg] open() "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27`; refused.FirstLine() != want {
		t.Fatalf("first line = %q", refused.FirstLine())
	}
	// The words the failure always had.
	if !strings.HasPrefix(refusal.Error(), "nginx validation failed: nginx: [emerg] open()") || !strings.HasSuffix(refusal.Error(), ": exit status 1") {
		t.Fatalf("words changed: %q", refusal.Error())
	}
	// A warning before the refusal is not the line.
	refused = &NginxConfigRefusedError{Output: "nginx: [warn] conflicting server name\nnginx: [emerg] unknown directive \"x\" in /etc/nginx/nginx.conf:3\n"}
	if !strings.Contains(refused.FirstLine(), "[emerg] unknown directive") {
		t.Fatalf("first line = %q", refused.FirstLine())
	}
	if got := (&NginxConfigRefusedError{Output: "\n  test failed  \n"}).FirstLine(); got != "test failed" {
		t.Fatalf("first line = %q", got)
	}

	for name, err := range map[string]error{
		"time limit":   context.DeadlineExceeded,
		"not runnable": &exec.Error{Name: "nginx", Err: exec.ErrNotFound},
	} {
		failure := nginxValidationError([]byte("partial"), err)
		if errors.As(failure, &refused) {
			t.Errorf("%s was typed as a refusal by nginx", name)
		}
		if failure.Error() != "nginx validation failed: partial: "+err.Error() || !errors.Is(failure, err) {
			t.Errorf("%s: %q", name, failure.Error())
		}
	}
}

// A new vhost that nginx refuses: the file and the link are gone again, nginx
// was asked again and reloaded, and the error says both things by type. When
// the inverse itself fails it is not "restored".
func TestRefusedVhostIsRestoredAndSaysSo(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "sites-available")
	mustMkdir(t, root, "sites-enabled")
	oldDir := nginxDir
	nginxDir = root
	t.Cleanup(func() { nginxDir = oldDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	validations, reloads := 0, 0
	ng.validateNginx = func() error {
		validations++
		if validations == 1 {
			return &NginxConfigRefusedError{Output: archNginxRefusal, Cause: errors.New("exit status 1")}
		}
		return nil
	}
	ng.reloadNginx = func() error { reloads++; return nil }
	failure := ng.ApplyVhost("set3-php.test", "server { listen 80; }\n")
	var refused *NginxConfigRefusedError
	var restored *VhostRestoredError
	if !errors.As(failure, &refused) || !errors.As(failure, &restored) {
		t.Fatalf("failure = %v", failure)
	}
	if !strings.HasSuffix(failure.Error(), "; rollback restored and reloaded the previous vhost") ||
		!strings.HasPrefix(failure.Error(), "nginx validation failed: nginx validation failed: nginx: [emerg]") {
		t.Fatalf("words changed: %q", failure.Error())
	}
	if validations != 2 || reloads != 1 {
		t.Fatalf("validations %d, reloads %d: the previous configuration was not checked and reloaded", validations, reloads)
	}
	for _, left := range []string{"sites-available/set3-php.test.conf", "sites-enabled/set3-php.test.conf"} {
		if matches, _ := filepath.Glob(filepath.Join(root, left)); len(matches) != 0 {
			t.Errorf("%s was left behind", left)
		}
	}

	// The previous configuration is refused too: not restored, and not said.
	ng.validateNginx = func() error {
		return &NginxConfigRefusedError{Output: archNginxRefusal, Cause: errors.New("exit status 1")}
	}
	failure = ng.ApplyVhost("set3-php.test", "server { listen 80; }\n")
	if errors.As(failure, &restored) || !strings.Contains(failure.Error(), "rollback validation failed") {
		t.Fatalf("an inverse that failed was reported as restored: %v", failure)
	}
}

// The configuration files listed for PHP-FPM are the installed PHP-FPM's: no
// version is assumed for a unit whose name carries none (it was "8.3").
func TestPHPFPMConfigFilesAreTheInstalledOnes(t *testing.T) {
	root, _ := withPHPHost(t, "http")
	mustMkdir(t, root, "php-fpm.d")
	arch := phpFPMConfigFiles("")
	if strings.Join(arch, "|") != strings.Join([]string{
		filepath.Join(root, "php-fpm.conf"), filepath.Join(root, "php-fpm.d", "www.conf"), "/usr/local/etc/php-fpm.d/www.conf",
	}, "|") {
		t.Fatalf("single-unit host: %q", arch)
	}
	for _, path := range arch {
		if strings.Contains(path, "8.3") {
			t.Fatalf("a version nobody observed is in %q", path)
		}
	}

	root, _ = withPHPHost(t, "www-data")
	mustMkdir(t, root, "8.4", "fpm", "pool.d")
	debian := phpFPMConfigFiles("8.4")
	if debian[0] != filepath.Join(root, "8.4", "fpm", "php-fpm.conf") || debian[1] != filepath.Join(root, "8.4", "fpm", "pool.d", "www.conf") {
		t.Fatalf("versioned host: %q", debian)
	}
	// Neither layout, or a version that is not a version: nothing under /etc/php.
	for _, version := range []string{"", "8.3", "../../etc"} {
		if got := phpFPMConfigFiles(version); len(got) != 1 || got[0] != "/usr/local/etc/php-fpm.d/www.conf" {
			t.Fatalf("version %q: %q", version, got)
		}
	}
	if got := (&ServiceScanner{}).extractPHPVersion("php8.4-fpm"); got != "8.4" {
		t.Fatalf("version of php8.4-fpm = %q", got)
	}
}
