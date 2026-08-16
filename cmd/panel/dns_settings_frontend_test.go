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
		t.Fatal(`resolve test source path`)
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(testFile), `..`, `..`, `web`, `src`, `components`, `DNSServerSettings.tsx`,
	))
	content, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Clean(filepath.Join(
			`..`, `..`, `web`, `src`, `components`, `DNSServerSettings.tsx`,
		))
		content, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf(`read DNSServerSettings.tsx: %v`, err)
		}
	}
	return string(content)
}

func TestDNSSettingsGuidedPairingContract(t *testing.T) {
	source := dnsSettingsFrontendSource(t)
	for _, required := range []string{
		`suggested_local_ns?: string;`,
		`suggested_peer_ns?: string;`,
		`suggested_peer_ip?: string;`,
		`dns_service_known?: boolean;`,
		`dns_service_ready?: boolean;`,
		`dns_service_detail?: string;`,
		`function canonicalIPv4(value: string): string`,
		`function isGlobalUnicastIPv4(value: string): boolean`,
		`function isValidNameserver(value: string): boolean`,
		`const [needsClusterRetry, setNeedsClusterRetry] = useState(false);`,
		`const [activeStep, setActiveStep] = useState<WizardStep>(1);`,
		`const [detectedPeerStaged, setDetectedPeerStaged] = useState(false);`,
		`const autoStageDetectedPeer =`,
		`const storedPeerValid =`,
		`const storedPeerMatchesSuggestion =`,
		`!storedPeerMatchesSuggestion`,
		`const mayReplacePeerIP =`,
		`currentPeerIPv4 === serverIPv4`,
		`: mayReplacePeerIP`,
		`const detectedAssignmentNeedsApply =`,
		`const peerIPUsable =`,
		`onChange={() => selectRole(role)}`,
		`const useDetectedAssignment =`,
		`const currentAssignmentMatchesDetected =`,
		`role === 'paired' && detectedAssignmentAvailable && !currentAssignmentValid;`,
		`peer_ip: useDetectedAssignment`,
		`peer_ns: useDetectedAssignment`,
		`const saveAndPublish = async () =>`,
		`fetch('/api/v1/settings/dns-setup', {`,
		`ns1: draftNS1,`,
		`ns2: draftNS2,`,
		`if (error.code === 'DNS_PUBLICATION_FAILED')`,
		`const reloaded = await load();`,
		`setNeedsClusterRetry(true);`,
		`showToast('warning', t('dnssrv.publicationPending'));`,
		`t('dnssrv.applyIncomplete')`,
		`onClick={saveAndPublish}`,
		`data-testid="dns-wizard-save"`,
		`aria-describedby="dns-setup-readiness"`,
		`data-testid={` + "`" + `dns-wizard-step-${step}` + "`" + `}`,
		`data-testid="dns-wizard-continue-mode"`,
		`data-testid="dns-wizard-continue-assignment"`,
		`{activeStep === 2 && (`,
		`{activeStep === 3 && (`,
		`t('dnssrv.modeChoiceTitle')`,
		`t('dnssrv.namesTitle')`,
		`t('dnssrv.assignmentTitle')`,
		`const namesReadyForSetup = namesValid;`,
		`t('dnssrv.namesInferredReview')`,
		`t('dnssrv.peerCorrectionStaged'`,
		`aria-label={t('dnssrv.assignmentSummary')}`,
		`t('dnssrv.detectedAssignment')`,
		`t('dnssrv.blocker.names')`,
		`t('dnssrv.blocker.chooseRole')`,
		`t('dnssrv.blocker.peerIp')`,
		`t('dnssrv.blocker.peerNs')`,
		`t('dnssrv.blocker.powerdns')`,
		`t('dnssrv.saveAndPublish')`,
		`t('dnssrv.retryPublication')`,
		`to="/services"`,
		`const dnsServiceMissing = dnsServiceKnown && !dnsServiceReady;`,
		`id="dns-peer-ip-invalid"`,
		`id="dns-peer-ip-same"`,
		`const stepTwoReady = namesReadyForSetup && draft.role !== '' && assignmentReady;`,
		`disabled={!stepTwoReady}`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf(`guided DNS pairing UI is missing %q`, required)
		}
	}

	for _, forbidden := range []string{
		`busy === 'cluster'`,
		`const saveNS =`,
		`const saveCluster =`,
		`const verifyAgain =`,
		`const saveAndVerify =`,
		`onClick={hasChanges ? saveAndVerify : verifyAgain}`,
		`fetch('/api/v1/settings/nameservers', {`,
		`fetch('/api/v1/settings/dns-cluster', {`,
		`if (!suggestedLocalNS`,
		`if (!suggestedPeerNS`,
		`t('dnssrv.partialSave'`,
		`t('dnssrv.stateUnsaved')`,
		`t('dnssrv.blocker.noChanges')`,
		`t('dnssrv.verifyAndRepublish')`,
		`t('dnssrv.retryApply')`,
		`<details open={!namesValid}`,
		`derivedNamesReplaced`,
		`ns1NeedsReplacement`,
		`ns2NeedsReplacement`,
		`current.role !== 'paired' && detectedAssignmentAvailable`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf(`guided DNS pairing UI still contains %q`, forbidden)
		}
	}

	if got := strings.Count(source, `fetch('/api/v1/settings/dns-setup', {`); got != 1 {
		t.Fatalf(`DNS setup must have exactly one mutation endpoint call, got %d`, got)
	}
	if got := strings.Count(source, `data-testid="dns-wizard-save"`); got != 1 {
		t.Fatalf(`DNS setup must expose one final save control, got %d`, got)
	}
}

func TestDNSSettingsSuggestionsRemainBoundToTheSavedPair(t *testing.T) {
	source := dnsSettingsFrontendSource(t)
	for _, required := range []string{
		`const suggestionMatchesSavedPair =`,
		`!namesDirty`,
		`savedNameserverNames.includes(rawSuggestedLocalNS)`,
		`savedNameserverNames.includes(rawSuggestedPeerNS)`,
		`const suggestedLocalNS = suggestionMatchesSavedPair ? rawSuggestedLocalNS : '';`,
		`const suggestedPeerNS = suggestionMatchesSavedPair ? rawSuggestedPeerNS : '';`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf(`saved-pair suggestion provenance is missing %q`, required)
		}
	}
	if strings.Contains(source, `!saved.namesDerived`) {
		t.Fatal(`valid inferred names must remain eligible for a verified public-DNS assignment`)
	}
}

func TestDNSSettingsUnconfiguredRoleRequiresExplicitSelection(t *testing.T) {
	source := dnsSettingsFrontendSource(t)
	for _, required := range []string{
		`type DraftDNSRole = DNSRole | '';`,
		`const role = normalizeRole(cluster.role);`,
		`const bindIdentityLocked = activeEngine === 'bind' && saved.configured;`,
		`: preserveCluster?.role ?? (snapshot.configured ? role : ''),`,
		`if (!draft || draft.role === '') return;`,
		`const savedDraftRole: DraftDNSRole = saved.configured ? saved.role : '';`,
		`draft.role !== savedDraftRole`,
		`else if (draft.role === '') clusterBlocker = 'chooseRole';`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf(`unconfigured DNS role flow is missing %q`, required)
		}
	}
	if strings.Contains(source, `role: preserveCluster?.role ?? role,`) {
		t.Fatal(`an unconfigured server must not inherit the backend standalone fallback as a user selection`)
	}
}
