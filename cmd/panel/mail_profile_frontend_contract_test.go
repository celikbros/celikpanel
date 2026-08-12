package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mailProfileUIContractSource(t *testing.T) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal(`resolve mail-profile frontend contract test path`)
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(testFile), `..`, `..`, `web`, `tests`, `mail-profile-ui-contract.test.mjs`,
	))
	content, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Clean(filepath.Join(`..`, `..`, `web`, `tests`, `mail-profile-ui-contract.test.mjs`))
		content, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf(`read mail-profile UI contract: %v`, err)
		}
	}
	return string(content)
}

func TestMailProfileFrontendSnapshotFailsClosed(t *testing.T) {
	source := componentOperationSource(t)
	decoder := sourceSection(
		t,
		source,
		`function decodeManagedMailProfiles(`,
		`// A terminal operation may unlock the page only after`,
	)
	for _, required := range []string{
		`value.length !== MAIL_PROFILE_IDS.length`,
		`profileIDs.has(id)`,
		`profile.services.length === 0`,
		`serviceIDs.has(serviceID)`,
		`new Set(profile.services).size !== profile.services.length`,
		`profile.available !== (`,
		`status === 'blocked'`,
		`profile.blocked_reason`,
		`profile.warning`,
	} {
		if !strings.Contains(decoder, required) {
			t.Fatalf(`strict mail-profile decoder is missing %q`, required)
		}
	}
	for _, profileID := range []string{`core-mail`, `webmail`, `protected-mail`} {
		if !strings.Contains(source, profileID) {
			t.Fatalf(`closed mail-profile id set is missing %q`, profileID)
		}
	}
}

func TestMailProfileFrontendRequiresFreshFullTerminalProof(t *testing.T) {
	source := componentOperationSource(t)
	proof := sourceSection(
		t,
		source,
		`function decodeVerifiedMailProfileResult(`,
		`function readSessionValue(`,
	)
	for _, required := range []string{
		`operation.kind !== 'mail_profile_install'`,
		`profile.status !== 'complete'`,
		`result.success !== true`,
		`result.profile_id !== operation.service_id`,
		`stringArrayMatchesSet(result.services, profile.services)`,
		`stringArrayMatchesSet(result.completed_services, profile.services)`,
		`tls.configured !== true`,
		`tls.fallback_only !== (tls.sni_count === 0)`,
		`result.submission_configured !== true`,
		`tls.fallback_only && warnings.length === 0`,
	} {
		if !strings.Contains(proof, required) {
			t.Fatalf(`mail-profile terminal proof is missing %q`, required)
		}
	}
	if !strings.Contains(source, `scannedAt < finishedAt`) {
		t.Fatal(`mail-profile success must retain the common fresh-scan time proof`)
	}
}

func TestMailProfileFrontendRecoveryAndFallbackStayKindBound(t *testing.T) {
	source := componentOperationSource(t)
	for _, required := range []string{
		`const OPERATION_RECOVERY_VERSION = 3;`,
		`function isMailProfileID(value: unknown)`,
		`value.version !== 2`,
		`operation_kind: operationKind`,
		`operation.kind === marker.operation_kind`,
		`operation.kind !== 'mail_profile_install'`,
		`!isMailProfileID(serviceID)`,
		`!isMailProfileID(value.service_id)`,
		`!isMailProfileID(operation.service_id)`,
		`'/api/v1/service/profile/install'`,
		`profile_id: request.serviceId`,
		`verifiedProfileResult?.fallbackOnly`,
		`showToast('warning', t('services.mailProfiles.fallbackWarning'`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf(`mail-profile recovery contract is missing %q`, required)
		}
	}
	decoder := sourceSection(
		t,
		source,
		`export function decodeOperationRecoveryMarker(`,
		`function readStoredRecoveryMarker(`,
	)
	guardIndex := strings.Index(decoder, `!isMailProfileID(value.service_id)`)
	createIndex := strings.Index(decoder, `return createOperationRecoveryMarker({`)
	if guardIndex < 0 || createIndex < 0 || guardIndex > createIndex {
		t.Fatal(`unknown V3 mail-profile marker must fail closed before marker reconstruction`)
	}
	nodeContract := mailProfileUIContractSource(t)
	for _, required := range []string{
		`const unknownCall = {`,
		`serviceId: 'unknown-mail'`,
		`operationKind: 'mail_profile_install'`,
		`createOperationRecoveryMarker(unknownCall, 1, requestID)`,
		`const unknownV3Marker = {`,
		`service_id: 'unknown-mail'`,
		`decodeOperationRecoveryMarker(JSON.stringify(unknownV3Marker))`,
	} {
		if !strings.Contains(nodeContract, required) {
			t.Fatalf(`runtime unknown V3 marker contract is missing %q`, required)
		}
	}
}

func TestMailProfileCardsUseServerMembershipAndClosedActions(t *testing.T) {
	source := serviceListSource(t)
	cards := sourceSection(t, source, `function MailProfileCards(`, `function InstallServiceDialog(`)
	for _, required := range []string{
		`profile.services.map((id)`,
		`profile.status === 'available'`,
		`profile.status === 'partial'`,
		`profile.status === 'complete'`,
		`disabled={disabled || !actionable}`,
		`profile.status === 'complete' && profile.warning`,
		`services.mailProfiles.profileComponentsNeedRepair`,
	} {
		if !strings.Contains(cards, required) {
			t.Fatalf(`mail-profile cards are missing %q`, required)
		}
	}
	if strings.Contains(cards, `['postfix'`) || strings.Contains(cards, `['dovecot'`) {
		t.Fatal(`mail-profile cards must not hard-code server membership`)
	}
	if !strings.Contains(source, `operationKind: 'mail_profile_install'`) {
		t.Fatal(`mail-profile action must start a kind-bound durable operation`)
	}
}
