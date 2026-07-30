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
		"draft.role !== savedDraftRole",
		"else if (draft.role === '') clusterBlocker = 'chooseRole';",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("unconfigured DNS role flow is missing %q", required)
		}
	}
	if strings.Contains(source, "role: preserveCluster?.role ?? role,") {
		t.Fatal("an unconfigured server must not inherit the backend's standalone operational fallback as a user selection")
	}
}

func TestDNSSettingsGuidedPairingContract(t *testing.T) {
	source := strings.ReplaceAll(dnsSettingsFrontendSource(t), "\r\n", "\n")
	for _, required := range []string{
		"suggested_local_ns?: string;",
		"suggested_peer_ns?: string;",
		"suggested_peer_ip?: string;",
		"function canonicalIPv4(value: string): string",
		"const mayReplacePeerIP =",
		"currentPeerIP === '' ||",
		"currentPeerIPv4 === serverIPv4",
		"mayReplacePeerIP && safeSuggestedPeerIP ? safeSuggestedPeerIP : current.peer_ip",
		"onChange={() => selectRole(role)}",
		"<ErrorBanner error={apiError} className=\"mb-4\" />",
		"aria-describedby=\"dns-cluster-readiness\"",
		"busy === 'cluster'",
		"role: draft.role,",
		"peer_ip: paired ? canonicalIPv4(draft.peer_ip) || draft.peer_ip.trim() : '',",
		"peer_ns: paired ? cleanHostname(draft.peer_ns) : '',",
		"body: JSON.stringify({ ns1: cleanHostname(draft.ns1), ns2: cleanHostname(draft.ns2) }),",
		"t('dnssrv.setup.step1.title')",
		"t('dnssrv.setup.step2.title')",
		"t('dnssrv.setup.step3.title')",
		"t('dnssrv.reviewTitle')",
		"t('dnssrv.peerIpInvalid')",
		"t('dnssrv.peerIpSame', {",
		"t('dnssrv.localNsRequired')",
		"t('dnssrv.blocker.saveNames')",
		"t('dnssrv.blocker.chooseRole')",
		"t('dnssrv.blocker.peerIp')",
		"t('dnssrv.blocker.peerNs')",
		"t('dnssrv.blocker.noChanges')",
		"t('dnssrv.blocker.busy')",
		"t('dnssrv.readyToSave')",
		"t('dnssrv.saving')",
		"t('dnssrv.savePaired')",
		"t('dnssrv.saveStandalone')",
		"t('dnssrv.publicDnsDraft')",
		"t('dnssrv.whereIntendedPeer')",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("guided DNS pairing UI is missing %q", required)
		}
	}

	for _, forbidden := range []string{
		"sm:grid-cols-2",
		"2xl:grid-cols-2",
		"disabled={busy ||",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("guided DNS pairing UI still contains %q", forbidden)
		}
	}

	ns1Index := strings.Index(source, `id="dns-ns1"`)
	ns2Index := strings.Index(source, `id="dns-ns2"`)
	if ns1Index < 0 || ns2Index < 0 || ns1Index >= ns2Index {
		t.Fatal("canonical nameserver 1/2 order must remain unchanged")
	}

	selectRoleStart := strings.Index(source, "const selectRole =")
	selectRoleEnd := strings.Index(source, "const selectLocalNameserver =")
	if selectRoleStart < 0 || selectRoleEnd <= selectRoleStart {
		t.Fatal("paired-role draft handler was not found")
	}
	selectRoleBody := source[selectRoleStart:selectRoleEnd]
	if strings.Contains(selectRoleBody, "fetch(") || strings.Contains(selectRoleBody, "saveCluster(") {
		t.Fatal("choosing paired mode must prepare a draft and must never save automatically")
	}

	useSuggestionStart := strings.Index(source, "const useDetectedPeer =")
	useSuggestionEnd := strings.Index(source, "const factLocation =")
	if useSuggestionStart < 0 || useSuggestionEnd <= useSuggestionStart {
		t.Fatal("detected peer draft handler was not found")
	}
	useSuggestionBody := source[useSuggestionStart:useSuggestionEnd]
	if strings.Contains(useSuggestionBody, "fetch(") || strings.Contains(useSuggestionBody, "saveCluster(") {
		t.Fatal("using the detected peer must update only the draft and must never save automatically")
	}

	oldLiveFactsGate := "{checksCurrent && (\n                        <div className=\"mb-3 rounded-lg border border-border bg-surface-2/50 p-3\">"
	if strings.Contains(source, oldLiveFactsGate) {
		t.Fatal("public DNS facts must remain visible while a topology draft is being edited")
	}
}
