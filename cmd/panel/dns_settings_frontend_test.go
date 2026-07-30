package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func dnsSettingsFrontendSource(t *testing.T) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(testFile), "..", "..", "web", "src", "components", "DNSServerSettings.tsx",
	))
	content, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Clean(filepath.Join(
			"..", "..", "web", "src", "components", "DNSServerSettings.tsx",
		))
		content, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("read DNSServerSettings.tsx: %v", err)
		}
	}
	return string(content)
}

func TestDNSSettingsUnconfiguredRoleRequiresExplicitSelection(t *testing.T) {
	source := strings.ReplaceAll(dnsSettingsFrontendSource(t), "\r\n", "\n")
	for _, required := range []string{
		"type DraftDNSRole = DNSRole | '';",
		"role: preserveCluster?.role ?? (snapshot.configured ? role : ''),",
		"if (!draft || draft.role === '') return;",
		"const savedDraftRole: DraftDNSRole = saved.configured ? saved.role : '';",
		"const canSaveCluster =\n        draft.role !== '' &&",
		"draft.role !== savedDraftRole",
		"draft.role !== '' && (",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("unconfigured DNS role flow is missing %q", required)
		}
	}
	if strings.Contains(source, "role: preserveCluster?.role ?? role,") {
		t.Fatal("an unconfigured server must not inherit the backend's standalone operational fallback as a user selection")
	}
}
