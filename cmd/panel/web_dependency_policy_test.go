package main

import (
	"strings"
	"testing"
)

func TestCIWebDependencyCommandsDoNotRunNetworkedAudit(t *testing.T) {
	source := frontendSourceFile(t, ".github", "workflows", "ci.yml")

	for lineNumber, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "npm audit") {
			t.Fatalf("CI line %d must not run npm audit without explicit operator approval", lineNumber+1)
		}
		if strings.Contains(trimmed, "npm ci") && !strings.Contains(trimmed, "--no-audit") {
			t.Fatalf("CI line %d must disable npm's install-time network audit", lineNumber+1)
		}
	}

	if !strings.Contains(source, "npm ci --no-audit --no-fund") {
		t.Fatal("CI must install locked web dependencies without audit or funding network requests")
	}
}

func TestNetworkedNPMAuditDocumentationRequiresExactApproval(t *testing.T) {
	const approval = "Paket adları ve sürümlerinin npm’in açık denetim servisine gönderilmesini onaylıyorum."
	for _, path := range [][]string{
		{"docs", "WEB-DEPENDENCY-SECURITY.md"},
		{"docs", "WEB-DEPENDENCY-SECURITY.tr.md"},
	} {
		source := strings.Join(strings.Fields(frontendSourceFile(t, path...)), " ")
		if !strings.Contains(source, approval) {
			t.Fatalf("%s must require the exact networked npm audit approval", strings.Join(path, "/"))
		}
	}
}
