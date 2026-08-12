package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func panelFunctionSource(t *testing.T, filename, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, contents, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != name {
			continue
		}
		start := fset.Position(function.Pos()).Offset
		end := fset.Position(function.End()).Offset
		return string(contents[start:end])
	}
	t.Fatalf("function %s is missing from %s", name, filename)
	return ""
}

func assertSourceOrder(t *testing.T, source string, markers ...string) {
	t.Helper()
	previous := -1
	for _, marker := range markers {
		index := strings.Index(source, marker)
		if index < 0 {
			t.Fatalf("source marker %q is missing", marker)
		}
		if index <= previous {
			t.Fatalf("source marker %q is out of order", marker)
		}
		previous = index
	}
}

func TestMailTLSV2LockOrderAndDirectChildCallsitesArePinned(t *testing.T) {
	public := panelFunctionSource(t, "mail_tls.go", "resyncMailTLSForTarget")
	assertSourceOrder(
		t, public,
		"p.serviceMutationMu.Lock()",
		"p.resyncMailTLSForTargetLocked(",
	)

	locked := panelFunctionSource(t, "mail_tls.go", "resyncMailTLSForTargetLocked")
	if strings.Contains(locked, "serviceMutationMu.Lock") {
		t.Fatal("locked Mail TLS helper must not reacquire the outer mutation lock")
	}
	assertSourceOrder(
		t, locked,
		"p.requireMailTLSSyncV2Agent(ctx)",
		"mailTLSSyncMu.Lock()",
		"p.loadMailTLSSnapshotLocked(",
		"p.applyCanonicalMailTLSV2Identity(",
	)

	child := panelFunctionSource(t, "service_operations.go", "syncMailTLSAfterOperation")
	if strings.Contains(child, "serviceMutationMu.Lock") ||
		strings.Contains(child, "resyncMailTLS") {
		t.Fatal("post-terminal Mail TLS child must retain the outer lock and use the direct locked path")
	}
	assertSourceOrder(
		t, child,
		"p.requireMailTLSSyncV2Agent(syncCtx)",
		"mailTLSSyncMu.Lock()",
		"p.loadMailTLSSnapshotLocked(",
		"p.applyCanonicalMailTLSV2Identity(",
	)

	serviceOperations, err := os.ReadFile("service_operations.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(serviceOperations), "p.syncMailTLSAfterOperation("); got != 2 {
		t.Fatalf("post-terminal Mail TLS child callsites=%d, want launch and startup recovery", got)
	}
}

func TestMailTLSV2TimeoutCoversAgentForwardConvergence(t *testing.T) {
	const agentForwardConvergenceLimit = 2 * time.Minute
	if panelMailTLSSyncTimeout < agentForwardConvergenceLimit+time.Minute {
		t.Fatalf(
			"panel Mail TLS timeout %s lacks margin above agent convergence %s",
			panelMailTLSSyncTimeout, agentForwardConvergenceLimit,
		)
	}
	if startupCertificateDependentTimeout != panelMailTLSSyncTimeout {
		t.Fatalf(
			"startup dependent timeout=%s, want dedicated Mail TLS timeout=%s",
			startupCertificateDependentTimeout, panelMailTLSSyncTimeout,
		)
	}
	identity := agentMutationIdentity{Kind: "mail_tls_sync"}
	if got := panelMutationTerminalReconcileTimeout(identity); got != panelMailTLSSyncTimeout {
		t.Fatalf("Mail TLS terminal reconcile timeout=%s, want %s", got, panelMailTLSSyncTimeout)
	}
	if got := panelMutationTerminalReconcileTimeout(agentMutationIdentity{Kind: "service_install"}); got != panelMutationFinishTimeout {
		t.Fatalf("generic terminal reconcile timeout=%s, want %s", got, panelMutationFinishTimeout)
	}
	child := panelFunctionSource(t, "service_operations.go", "syncMailTLSAfterOperation")
	if !strings.Contains(child, "panelMailTLSSyncTimeout") ||
		strings.Contains(child, "panelMutationRecoveryTimeout") {
		t.Fatal("mail-profile direct child is not using the dedicated Mail TLS timeout")
	}
}

func TestMailTLSV2ProductionRPCCallInventory(t *testing.T) {
	legacy := []string{
		`"Agent.SecureMailTLS"`,
		`"Agent.ReconcileMailTLSMutation"`,
	}
	v2Calls := 0
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(contents)
		for _, method := range legacy {
			if strings.Contains(source, method) {
				t.Errorf("legacy Mail TLS RPC literal %s remains in production file %s", method, path)
			}
		}
		v2Calls += strings.Count(source, `"Agent.SyncMailTLSV2"`)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2Calls == 0 {
		t.Fatal("production panel has no Mail TLS V2 RPC admission or callsite")
	}
}
