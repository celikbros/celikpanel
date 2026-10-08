package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func componentOperationSource(t *testing.T) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(testFile), "..", "..", "web", "src", "components", "ComponentOperation.tsx",
	))
	content, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Clean(filepath.Join(
			"..", "..", "web", "src", "components", "ComponentOperation.tsx",
		))
		content, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("read ComponentOperation.tsx: %v", err)
		}
	}
	return string(content)
}

func serviceListSource(t *testing.T) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(testFile), "..", "..", "web", "src", "components", "ServiceList.tsx",
	))
	content, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Clean(filepath.Join(
			"..", "..", "web", "src", "components", "ServiceList.tsx",
		))
		content, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("read ServiceList.tsx: %v", err)
		}
	}
	return string(content)
}

func sourceSection(t *testing.T, source, start, end string) string {
	t.Helper()
	startIndex := strings.Index(source, start)
	if startIndex < 0 {
		t.Fatalf("source start marker %q not found", start)
	}
	endIndex := strings.Index(source[startIndex:], end)
	if endIndex < 0 {
		t.Fatalf("source end marker %q not found", end)
	}
	return source[startIndex : startIndex+endIndex]
}

func TestComponentOperationUnverifiableResponsesStayFailClosed(t *testing.T) {
	source := componentOperationSource(t)
	recovery := sourceSection(
		t,
		source,
		"const recoverCurrentOperation = async (",
		"// A POST can reach the server while its response is lost.",
	)
	if strings.Contains(recovery, "finishFailure(") {
		t.Fatal("unverifiable recovery path must not clear the operation marker or unlock the page")
	}
	for _, required := range []string{
		"?request_id=${encodeURIComponent(marker.request_id)}",
		"/api/v1/service/operation?active=1",
		"setConnectionInterrupted(true)",
	} {
		if !strings.Contains(recovery, required) {
			t.Fatalf("recovery path is missing fail-closed evidence step %q", required)
		}
	}

	poll := sourceSection(t, source, "poll = async () => {", "        poll();")
	if strings.Contains(poll, "finishFailure(") {
		t.Fatal("operation poll must reconcile unverifiable responses instead of unlocking")
	}
	if got := strings.Count(poll, "await reconcileUnverifiableOperation();"); got < 5 {
		t.Fatalf("operation poll has only %d fail-closed reconciliation branches; want at least 5", got)
	}
	if got := strings.Count(poll, "clearStoredOperation();"); got != 1 {
		t.Fatalf("operation poll clears durable state %d times; want exactly one verified success path", got)
	}
	decodeIndex := strings.LastIndex(poll, "decodeManagedServicesSnapshot(snapshot)")
	clearIndex := strings.Index(poll, "clearStoredOperation();")
	if decodeIndex < 0 || clearIndex < 0 || decodeIndex > clearIndex {
		t.Fatal("operation state may only clear after a valid fresh managed-services snapshot")
	}

	// 2026-10-08: one typed reply has its own evidence. A scan refused because a
	// running server setup owns the host (409 server_setup_busy) is replaced by a
	// read of the scan the operation itself stored. That refusal is read in one
	// place, releases nothing by itself, and its snapshot passes the same decode
	// and confirmation before the single clear above. Every other refused, failed
	// or unreadable scan reply stays on the retry branches.
	if got := strings.Count(source, "scanRefusedBySetup("); got != 1 {
		t.Fatalf("setup scan refusal is read %d times; want exactly one typed exception", got)
	}
	if got := strings.Count(source, "storedByOperation = true;"); got != 1 {
		t.Fatalf("stored operation scan is selected %d times; want only the setup refusal", got)
	}
	refusedIndex := strings.Index(poll, "if (await scanRefusedBySetup(scanResponse)) {")
	selectedIndex := strings.Index(poll, "storedByOperation = true;")
	storedReadIndex := strings.Index(poll, "scanResponse = await fetch('/api/v1/managed-services', {")
	confirmIndex := strings.Index(
		poll,
		"|| !snapshotConfirmsTerminalOperation(freshSnapshot, next, storedByOperation)",
	)
	if refusedIndex < 0 || selectedIndex < refusedIndex || storedReadIndex < selectedIndex ||
		decodeIndex < storedReadIndex || confirmIndex < decodeIndex || clearIndex < confirmIndex {
		t.Fatal("a scan refused by server setup may only lead to the stored scan, checked like a fresh one before the clear")
	}
	if got := strings.Count(poll, "setOperation(null);"); got != 1 {
		t.Fatalf("operation poll releases the overlay %d times; want exactly one verified success path", got)
	}
}

func TestComponentOperationTerminalFailureWaitsForFreshSnapshot(t *testing.T) {
	source := componentOperationSource(t)
	refreshFailure := sourceSection(
		t,
		source,
		"const refreshFailedSnapshot = async (",
		"        let poll: () => Promise<void>;",
	)
	decodeIndex := strings.Index(refreshFailure, "decodeManagedServicesSnapshot(snapshot)")
	finishIndex := strings.Index(refreshFailure, "finishFailure(terminalFailure)")
	if decodeIndex < 0 || finishIndex < 0 || decodeIndex > finishIndex {
		t.Fatal("terminal failure must remain locked until a valid fresh managed-services snapshot")
	}
	// The stored-scan exception belongs to a verified success only.
	if !strings.Contains(refreshFailure, "|| !snapshotConfirmsTerminalOperation(freshSnapshot, terminalOperation)\n") ||
		strings.Contains(refreshFailure, "storedByOperation") ||
		strings.Contains(refreshFailure, "scanRefusedBySetup") {
		t.Fatal("terminal failure must be confirmed by a fresh scan, never by the stored one")
	}
}

func TestComponentOperationActiveDiscoveryUnlocksOnlyVerifiedAbsence(t *testing.T) {
	source := componentOperationSource(t)
	discovery := sourceSection(
		t,
		source,
		"const syncActiveOperation = async (retrying = false) => {",
		"        const onFocus = () => {",
	)
	if got := strings.Count(discovery, "lockedRef.current = false;"); got != 1 {
		t.Fatalf("active discovery releases the lock %d times; want one verified-absence path", got)
	}
	for _, required := range []string{
		"else if (verifiedNoActive)",
		"activeDiscoveryRetryTimer = window.setTimeout(",
		"setConnectionInterrupted(true)",
	} {
		if !strings.Contains(discovery, required) {
			t.Fatalf("active discovery is missing fail-closed step %q", required)
		}
	}
}

func TestComponentOperationInitialDiscoveryLocksMutationsWithoutBlockingPage(t *testing.T) {
	source := componentOperationSource(t)
	for _, required := range []string{
		"() => initialSession.operation === null && initialSession.recoveryMarker === null",
		"const interactionBlocksRef = useRef(new Map<object, InteractionBlockView>());",
		"const acquireInteractionBlock = useCallback((view: InteractionBlockView): InteractionBlockLease => {",
		"interactionBlocksRef.current.set(id, view);",
		"interactionBlocksRef.current.delete(id)",
		"|| interactionBlocksRef.current.size > 0",
		"const locked = discoveringActive || interactionBlocked;",
		"if (!interactionBlocked) return;",
		"{interactionBlocked && createPortal(",
		"void syncActiveOperation(true);",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("initial active-operation discovery is missing non-blocking safety step %q", required)
		}
	}
	if strings.Contains(source, "{locked && createPortal(") {
		t.Fatal("background discovery must not open the blocking operation overlay")
	}
	if !strings.Contains(source, "root?.setAttribute('inert', '');") {
		t.Fatal("real package operations must still make the underlying page inert")
	}
}

func TestComponentOperationTerminalSnapshotProvesFreshExpectedState(t *testing.T) {
	source := componentOperationSource(t)
	confirmation := sourceSection(
		t,
		source,
		"function snapshotConfirmsTerminalOperation(",
		"function readSessionValue(",
	)
	for _, required := range []string{
		"scannedAt < finishedAt",
		// The stored scan counts only on the caller's word and only when it is
		// no older than the second the operation started (2026-10-08).
		"storedByOperation = false,",
		"!storedByOperation",
		"scannedAt < Math.floor(startedAt / 1000) * 1000",
		"operation.status === 'failed'",
		"service.is_installed !== true",
		"operation.kind !== 'runtime_install'",
		"instances.some",
	} {
		if !strings.Contains(confirmation, required) {
			t.Fatalf("terminal snapshot confirmation is missing %q", required)
		}
	}
	if got := strings.Count(source, "snapshotConfirmsTerminalOperation(freshSnapshot,"); got != 2 {
		t.Fatalf("terminal snapshot confirmation is used %d times; want success and failure paths", got)
	}
}

func TestServiceListDoesNotRepaintStaleCacheAfterOperationScan(t *testing.T) {
	source := serviceListSource(t)
	for _, required := range []string{
		"const latestScannedAtRef = useRef<string | null>(null);",
		"nextTimestamp < currentTimestamp",
		"source === 'load' && nextTimestamp === currentTimestamp",
		"useLayoutEffect(() => {",
		"applySnapshot(catalogSnapshot, 'scan')",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("fresh operation snapshot protection is missing %q", required)
		}
	}
}
