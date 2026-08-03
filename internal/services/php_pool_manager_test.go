package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func withPoolTree(t *testing.T, version, pool, body string) string {
	t.Helper()
	dir := t.TempDir()
	oldEtc := phpEtcDir
	oldReload := reloadPHPFPM
	oldTest := phpFPMConfigTest
	phpEtcDir = dir
	reloadPHPFPM = func(string) error { return nil }
	phpFPMConfigTest = func(string) error { return nil }
	t.Cleanup(func() {
		phpEtcDir = oldEtc
		reloadPHPFPM = oldReload
		phpFPMConfigTest = oldTest
	})

	poolDir := filepath.Join(dir, version, "fpm", "pool.d")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(poolDir, pool+".conf")
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

const seededPool = `[site42]
user = celik_site42
group = celik_site42
listen = /var/run/php/php8.3-fpm-site42.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
pm.max_requests = 500
chdir = /
`

func TestUpdatePoolConfigRefusesToTakeIdentityFromTheCaller(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	hostile := &core.PHPPoolConfig{
		Name: "site42", User: "root", Group: "root",
		Listen: "/var/run/php/php8.3-fpm-site7.sock", ListenOwner: "root",
		ListenGroup: "root", ListenMode: "0666", PM: "dynamic", PMMaxChildren: 6,
	}
	if err := NewPHPPoolManager().UpdatePoolConfig("8.3", hostile); err != nil {
		t.Fatalf("update pool: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, forbidden := range []string{"user = root", "group = root", "listen.owner = root", "listen.group = root", "listen.mode = 0666", "site7.sock"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("caller-supplied identity reached pool: %q\n%s", forbidden, got)
		}
	}
	for _, required := range []string{"user = celik_site42", "group = celik_site42", "listen = /var/run/php/php8.3-fpm-site42.sock", "listen.owner = www-data", "listen.mode = 0660"} {
		if !strings.Contains(got, required) {
			t.Errorf("managed identity lost: %q\n%s", required, got)
		}
	}
}

func TestUpdatePoolConfigClampsResourceTunables(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	pm := NewPHPPoolManager()
	if err := pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site42", PM: "dynamic", PMMaxChildren: 999999, PMMaxRequests: 999999999}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "pm.max_children = 200") || strings.Contains(string(got), "pm.max_children = 999999") {
		t.Fatalf("resource limit not clamped:\n%s", got)
	}
	if err := pm.UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site42", PM: "ondemand"}); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if strings.Contains(string(got), "pm.max_children = 0") || !strings.Contains(string(got), "pm = ondemand") {
		t.Fatalf("safe defaults or PM mode not applied:\n%s", got)
	}
}

func TestUpdatePoolConfigRejectsUnknownPMModeWithoutChangingFile(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	before, _ := os.ReadFile(path)
	err := NewPHPPoolManager().UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site42", PM: "; malicious", PMMaxChildren: 5})
	if err == nil {
		t.Fatal("invalid PM mode was accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("invalid request changed pool file:\n%s", after)
	}
}

func TestUpdatePoolConfigBoundsThePoolName(t *testing.T) {
	withPoolTree(t, "8.3", "site42", seededPool)
	for _, bad := range []string{"../../../../etc/cron.d/evil", "site42; rm -rf /", "www", ""} {
		if err := NewPHPPoolManager().UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: bad, PM: "dynamic"}); err == nil {
			t.Errorf("pool name %q was accepted", bad)
		}
	}
}

func TestUpdatePoolConfigRefusesWhenThePoolIsAbsent(t *testing.T) {
	withPoolTree(t, "8.3", "site42", seededPool)
	if err := NewPHPPoolManager().UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site99", PM: "dynamic"}); err == nil {
		t.Fatal("missing pool was invented")
	}
	if _, err := os.Stat(poolFilePath("8.3", "site99")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected target state: %v", err)
	}
}

func TestUpdatePoolConfigReloadFailureRestoresExactSnapshot(t *testing.T) {
	path := withPoolTree(t, "8.3", "site42", seededPool)
	before, _ := os.ReadFile(path)
	calls := 0
	reloadPHPFPM = func(string) error {
		calls++
		if calls == 1 {
			return errors.New("reload failed")
		}
		return nil
	}
	err := NewPHPPoolManager().UpdatePoolConfig("8.3", &core.PHPPoolConfig{Name: "site42", PM: "ondemand"})
	if err == nil || !strings.Contains(err.Error(), "previous configuration restored and activated") {
		t.Fatalf("rollback error not reported: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("rollback was not byte exact:\n%s", after)
	}
	if calls != 2 {
		t.Fatalf("reload calls = %d, want apply+rollback", calls)
	}
}

func TestCreatePoolReloadFailureRemovesNewFile(t *testing.T) {
	withPoolTree(t, "8.3", "site42", seededPool)
	calls := 0
	reloadPHPFPM = func(string) error {
		calls++
		if calls == 1 {
			return errors.New("reload failed")
		}
		return nil
	}
	manager, err := NewPHPFPMManager()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreatePool(99, "celik_site99", "8.3"); err == nil {
		t.Fatal("create falsely succeeded")
	}
	if _, err := os.Stat(poolFilePath("8.3", "site99")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed create left a pool behind: %v", err)
	}
	if calls != 2 {
		t.Fatalf("reload calls = %d, want apply+rollback", calls)
	}
}

func TestMigratePoolWritesNewVersionDirectly(t *testing.T) {
	oldPath := withPoolTree(t, "8.3", "site42", seededPool)
	if err := os.MkdirAll(poolDirPath("8.4"), 0o755); err != nil {
		t.Fatal(err)
	}
	reloaded := []string{}
	reloadPHPFPM = func(version string) error { reloaded = append(reloaded, version); return nil }
	if err := NewPHPPoolManager().MigratePool("8.3", "8.4", "site42"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	newBody, err := os.ReadFile(poolFilePath("8.4", "site42"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user = celik_site42", "listen = /var/run/php/php8.4-fpm-site42.sock", "listen.owner = www-data"} {
		if !strings.Contains(string(newBody), want) {
			t.Errorf("new pool missing %q\n%s", want, newBody)
		}
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old pool still exists: %v", err)
	}
	if strings.Join(reloaded, ",") != "8.4,8.3" {
		t.Fatalf("reload order = %v", reloaded)
	}
}

func TestMigratePoolCleanupFailureRestoresSourceAndRemovesTarget(t *testing.T) {
	oldPath := withPoolTree(t, "8.3", "site42", seededPool)
	before, _ := os.ReadFile(oldPath)
	if err := os.MkdirAll(poolDirPath("8.4"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldReloadCalls := 0
	reloadPHPFPM = func(version string) error {
		if version == "8.3" {
			oldReloadCalls++
			if oldReloadCalls == 1 {
				return errors.New("old reload failed")
			}
		}
		return nil
	}
	err := NewPHPPoolManager().MigratePool("8.3", "8.4", "site42")
	if err == nil {
		t.Fatal("migration falsely succeeded")
	}
	after, readErr := os.ReadFile(oldPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("source pool not restored exactly: %v\n%s", readErr, after)
	}
	if _, statErr := os.Stat(poolFilePath("8.4", "site42")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target pool remained after failed source cleanup: %v", statErr)
	}
}
