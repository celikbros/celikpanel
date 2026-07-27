package services

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestApplyVhostsValidatesAndReloadsOnlyOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	vhosts := make([]RenderedVhost, 0, 128)
	for index := 0; index < 128; index++ {
		vhosts = append(vhosts, RenderedVhost{
			Domain: "batch-" + integerString(index) + ".example",
			Config: "new config " + integerString(index) + "\n",
		})
	}
	if err := ng.ApplyVhosts(vhosts); err != nil {
		t.Fatalf("apply vhost batch: %v", err)
	}
	if validateCalls != 1 || reloadCalls != 1 {
		t.Fatalf(
			"128 vhosts must use one validate/reload, validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
	for _, item := range vhosts {
		assertRenderedVhostContent(t, item.Domain, item.Config)
	}
}

func TestApplyVhostsValidationFailureRestoresEntirePreviousSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	const (
		enabledDomain  = "batch-enabled.example"
		disabledDomain = "batch-disabled.example"
		newDomain      = "batch-new.example"
	)
	if err := ng.WriteVhostFile(enabledDomain, "old enabled\n"); err != nil {
		t.Fatal(err)
	}
	if err := ng.WriteVhostFile(disabledDomain, "old disabled\n"); err != nil {
		t.Fatal(err)
	}
	_, disabledLink := vhostPaths(disabledDomain)
	if err := os.Remove(disabledLink); err != nil {
		t.Fatal(err)
	}

	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		if validateCalls == 1 {
			return errors.New("generated set is invalid")
		}
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	err = ng.ApplyVhosts([]RenderedVhost{
		{Domain: enabledDomain, Config: "new enabled\n"},
		{Domain: disabledDomain, Config: "new disabled\n"},
		{Domain: newDomain, Config: "new site\n"},
	})
	if err == nil || !strings.Contains(
		err.Error(),
		"rollback restored and reloaded all touched vhosts",
	) {
		t.Fatalf("validation failure must report completed batch rollback, got %v", err)
	}
	if validateCalls != 2 || reloadCalls != 1 {
		t.Fatalf(
			"failed set and restored set counts: validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
	assertRenderedVhostContent(t, enabledDomain, "old enabled\n")
	assertRenderedVhostFileContent(t, disabledDomain, "old disabled\n")
	if _, err := os.Lstat(disabledLink); !os.IsNotExist(err) {
		t.Fatalf("formerly disabled vhost was enabled by rollback: %v", err)
	}
	newAvailable, newEnabled := vhostPaths(newDomain)
	for _, candidate := range []string{newAvailable, newEnabled} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("failed new batch vhost remains at %s: %v", candidate, err)
		}
	}
}

func TestApplyVhostsReloadFailureRestoresEntirePreviousSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []RenderedVhost{
		{Domain: "reload-one.example", Config: "old one\n"},
		{Domain: "reload-two.example", Config: "old two\n"},
	} {
		if err := ng.WriteVhostFile(item.Domain, item.Config); err != nil {
			t.Fatal(err)
		}
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		if reloadCalls == 1 {
			return errors.New("reload refused")
		}
		return nil
	}

	err = ng.ApplyVhosts([]RenderedVhost{
		{Domain: "reload-one.example", Config: "new one\n"},
		{Domain: "reload-two.example", Config: "new two\n"},
	})
	if err == nil || !strings.Contains(
		err.Error(),
		"rollback restored and reloaded all touched vhosts",
	) {
		t.Fatalf("reload failure must report completed batch rollback, got %v", err)
	}
	if validateCalls != 2 || reloadCalls != 2 {
		t.Fatalf(
			"new and restored set counts: validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
	assertRenderedVhostContent(t, "reload-one.example", "old one\n")
	assertRenderedVhostContent(t, "reload-two.example", "old two\n")
}

func TestApplyVhostsWriteFailureRestoresOnlyTouchedPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	const (
		firstDomain  = "write-prefix-first.example"
		secondDomain = "write-prefix-second.example"
		thirdDomain  = "write-prefix-untouched.example"
	)
	for _, domain := range []string{firstDomain, secondDomain, thirdDomain} {
		if err := ng.WriteVhostFile(domain, "old "+domain+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	_, thirdEnabled := vhostPaths(thirdDomain)
	if err := os.Remove(thirdEnabled); err != nil {
		t.Fatal(err)
	}
	const untouchedEnabledSentinel = "not a symlink and never touched"
	if err := os.WriteFile(
		thirdEnabled,
		[]byte(untouchedEnabledSentinel),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	ng.writeVhostBatch = func(domain, config string) error {
		if err := ng.writeVhostFile(domain, config); err != nil {
			return err
		}
		if domain == secondDomain {
			return errors.New("injected failure after partial write")
		}
		return nil
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	err = ng.ApplyVhosts([]RenderedVhost{
		{Domain: firstDomain, Config: "new first\n"},
		{Domain: secondDomain, Config: "new second\n"},
		{Domain: thirdDomain, Config: "new third\n"},
	})
	if err == nil || !strings.Contains(
		err.Error(),
		"rollback restored and reloaded all touched vhosts",
	) {
		t.Fatalf("partial write failure rollback = %v", err)
	}
	if validateCalls != 1 || reloadCalls != 1 {
		t.Fatalf(
			"partial write rollback validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
	assertRenderedVhostContent(t, firstDomain, "old "+firstDomain+"\n")
	assertRenderedVhostContent(t, secondDomain, "old "+secondDomain+"\n")
	assertRenderedVhostFileContent(t, thirdDomain, "old "+thirdDomain+"\n")
	info, err := os.Lstat(thirdEnabled)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("untouched trailing vhost enabled sentinel was replaced by a symlink")
	}
	content, err := os.ReadFile(thirdEnabled)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != untouchedEnabledSentinel {
		t.Fatalf("untouched enabled sentinel = %q", content)
	}
}

func TestApplyVhostsRollbackRestoreFailureDoesNotReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	const domain = "rollback-restore-fails.example"
	available, enabled := vhostPaths(domain)
	if err := os.MkdirAll(filepath.Dir(available), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(available, []byte("mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(enabled, "blocks-remove"),
		[]byte("sentinel"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	err = ng.rollbackVhostMutations(
		[]RenderedVhost{{Domain: domain}},
		map[string]vhostSnapshot{
			domain: {config: "old\n", exists: true, enabled: true},
		},
		errors.New("forward mutation failed"),
	)
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("restore failure rollback error = %v", err)
	}
	if validateCalls != 0 || reloadCalls != 0 {
		t.Fatalf(
			"failed restore must not validate/reload, validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
}

func TestApplyVhostsRollbackValidationFailureDoesNotReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	const domain = "rollback-validation-fails.example"
	if err := ng.WriteVhostFile(domain, "old\n"); err != nil {
		t.Fatal(err)
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return errors.New("nginx validation remains broken")
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	err = ng.ApplyVhosts([]RenderedVhost{
		{Domain: domain, Config: "new\n"},
	})
	if err == nil || !strings.Contains(err.Error(), "rollback validation") {
		t.Fatalf("rollback validation error = %v", err)
	}
	if validateCalls != 2 || reloadCalls != 0 {
		t.Fatalf(
			"failed rollback validation must not reload, validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
	assertRenderedVhostContent(t, domain, "old\n")
}

func TestApplyVhostsRejectsDuplicateBeforeMutation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create production-style enabled symlinks")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	err = ng.ApplyVhosts([]RenderedVhost{
		{Domain: "duplicate.example", Config: "first\n"},
		{Domain: "duplicate.example", Config: "second\n"},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate vhost domain") {
		t.Fatalf("duplicate batch error = %v", err)
	}
	if validateCalls != 0 || reloadCalls != 0 {
		t.Fatalf(
			"duplicate batch mutated nginx, validate=%d reload=%d",
			validateCalls,
			reloadCalls,
		)
	}
	available, enabled := vhostPaths("duplicate.example")
	for _, candidate := range []string{available, enabled} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("duplicate batch wrote %s: %v", candidate, err)
		}
	}
}

func assertRenderedVhostContent(t *testing.T, domain, want string) {
	t.Helper()
	assertRenderedVhostFileContent(t, domain, want)
	available, enabled := vhostPaths(domain)
	target, err := os.Readlink(enabled)
	if err != nil {
		t.Fatalf("read enabled link for %s: %v", domain, err)
	}
	if target != available {
		t.Fatalf("%s enabled link = %q, want %q", domain, target, available)
	}
}

func assertRenderedVhostFileContent(t *testing.T, domain, want string) {
	t.Helper()
	available, _ := vhostPaths(domain)
	content, err := os.ReadFile(available)
	if err != nil {
		t.Fatalf("read %s: %v", domain, err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", domain, content, want)
	}
}

func integerString(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
