import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const operationSource = readFileSync(
  new URL('../src/components/ComponentOperation.tsx', import.meta.url),
  'utf8',
);
const operationOverlaySource = readFileSync(
  new URL('../src/components/OperationOverlay.tsx', import.meta.url),
  'utf8',
);
const serviceSource = readFileSync(
  new URL('../src/components/ServiceList.tsx', import.meta.url),
  'utf8',
);
const enSource = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const trSource = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

async function loadMarkerRuntime() {
  const versionStart = operationSource.indexOf('const OPERATION_RECOVERY_VERSION');
  const versionEnd = operationSource.indexOf('\n', versionStart);
  const profilesStart = operationSource.indexOf('const MAIL_PROFILE_IDS');
  const profilesEnd = operationSource.indexOf('export interface ManagedMailProfile', profilesStart);
  const functionsStart = operationSource.indexOf('function boundedMarkerString(');
  const functionsEnd = operationSource.indexOf('function readStoredRecoveryMarker(', functionsStart);
  assert.ok(versionStart >= 0 && versionEnd > versionStart);
  assert.ok(profilesStart >= 0 && profilesEnd > profilesStart);
  assert.ok(functionsStart >= 0 && functionsEnd > functionsStart);
  const source = [
    operationSource.slice(versionStart, versionEnd),
    operationSource.slice(profilesStart, profilesEnd),
    operationSource.slice(functionsStart, functionsEnd),
  ].join('\n');
  const javascript = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const url = `data:text/javascript;base64,${Buffer.from(javascript).toString('base64')}`;
  return import(url);
}

test('mail profile snapshots accept only the closed server-defined graph', () => {
  assert.match(operationSource, /MAIL_PROFILE_IDS = \['core-mail', 'webmail', 'protected-mail'\]/);
  assert.match(operationSource, /value\.length !== MAIL_PROFILE_IDS\.length/);
  assert.match(operationSource, /profileIDs\.has\(id\)/);
  assert.match(operationSource, /serviceIDs\.has\(serviceID\)/);
  assert.match(operationSource, /new Set\(profile\.services\)\.size !== profile\.services\.length/);
  assert.match(operationSource, /profile\.available !== \(/);
  assert.match(operationSource, /status === 'blocked'/);
  assert.match(serviceSource, /decodeManagedMailProfiles\(payload\.profiles, serviceIDs\)/);
  assert.match(operationSource, /typeof payload\.dns_identity_ready !== 'boolean'/);
  assert.match(serviceSource, /typeof payload\.dns_identity_ready !== 'boolean'/);
});

test('profile recovery markers are v3 kind-bound with a safe v2 migration', async () => {
  assert.match(operationSource, /OPERATION_RECOVERY_VERSION = 3/);
  assert.match(operationSource, /value\.version !== 2/);
  assert.match(operationSource, /operation\.kind === marker\.operation_kind/);
  assert.match(operationSource, /!isMailProfileID\(serviceID\) \|\| Boolean\(packageName \|\| runtimeVersion\)/);
  assert.match(operationSource, /'\/api\/v1\/service\/profile\/install'/);
  assert.match(operationSource, /profile_id: request\.serviceId/);
  assert.match(operationSource, /profile_id: request\.serviceId, request_id: marker\.request_id, confirmed: true/);

  const { createOperationRecoveryMarker, decodeOperationRecoveryMarker } = await loadMarkerRuntime();
  const requestID = 'a'.repeat(32);
  const unknownCall = {
    serviceId: 'unknown-mail',
    name: 'Unknown mail profile',
    operationKind: 'mail_profile_install',
  };
  assert.equal(createOperationRecoveryMarker(unknownCall, 1, requestID), null);

  const unknownV3Marker = {
    version: 3,
    operation_kind: 'mail_profile_install',
    request_id: requestID,
    service_id: 'unknown-mail',
    label: 'Unknown mail profile',
    created_at: 1,
  };
  assert.equal(decodeOperationRecoveryMarker(JSON.stringify(unknownV3Marker)), null);
  assert.equal(
    decodeOperationRecoveryMarker(JSON.stringify({ ...unknownV3Marker, service_id: 'core-mail' }))?.service_id,
    'core-mail',
  );
  assert.equal(
    decodeOperationRecoveryMarker(JSON.stringify({
      ...unknownV3Marker,
      operation_kind: 'service_install',
    }))?.service_id,
    'unknown-mail',
  );
});

test('profile success needs fresh complete state and full operation evidence', () => {
  const start = operationSource.indexOf('function decodeVerifiedMailProfileResult(');
  const end = operationSource.indexOf('function readSessionValue(', start);
  assert.ok(start >= 0 && end > start);
  const proof = operationSource.slice(start, end);
  assert.match(proof, /result\.success !== true/);
  assert.match(proof, /profile\.status !== 'complete'/);
  assert.match(proof, /result\.profile_id !== operation\.service_id/);
  assert.match(proof, /stringArrayMatchesSet\(result\.services, profile\.services\)/);
  assert.match(proof, /stringArrayMatchesSet\(result\.completed_services, profile\.services\)/);
  assert.match(proof, /tls\.fallback_only !== \(tls\.sni_count === 0\)/);
  assert.match(proof, /result\.submission_configured !== true/);
  assert.doesNotMatch(proof, /profile\.warning/);
  assert.match(operationSource, /verifiedProfileResult\?\.fallbackOnly/);
  assert.match(operationSource, /showToast\('warning', t\('services\.mailProfiles\.fallbackWarning'/);
});

test('profile cards preserve individual services and use server membership', () => {
  assert.match(serviceSource, /profile\.services\.map\(\(id\)/);
  assert.match(serviceSource, /operationKind: 'mail_profile_install'/);
  assert.match(serviceSource, /profile\.status === 'available'/);
  assert.match(serviceSource, /profile\.status === 'partial'/);
  assert.match(serviceSource, /profile\.status === 'complete'/);
  assert.match(serviceSource, /disabled=\{disabled \|\| !dnsIdentityReady \|\| !actionable\}/);
  assert.match(serviceSource, /dnsIdentityReady && profile\.available/);
  assert.match(serviceSource, /navigate\('\/settings\?section=dns'\)/);
  assert.match(serviceSource, /profile\.status === 'complete' && profile\.warning/);
  assert.match(serviceSource, /services\.mailProfiles\.profileComponentsNeedRepair/);
  assert.match(serviceSource, /setProfileTarget\(profile\)/);
  assert.match(serviceSource, /role='dialog'/);
  assert.match(serviceSource, /aria-modal='true'/);
  assert.match(serviceSource, /type='checkbox'/);
  assert.match(serviceSource, /disabled=\{!acknowledged\}/);
  assert.match(serviceSource, /\/api\/v1\/service\/candidate\?id=/);
  assert.match(serviceSource, /service\.packages/);
  assert.match(serviceSource, /service\.ports/);
  assert.match(serviceSource, /\{loading \? \(/, 'existing individual service catalogue remains rendered');
});

test('complete profile copy claims only component state and localizes repair guidance', () => {
  const enComplete = enSource.match(/'services\.mailProfiles\.status\.complete': '([^']+)'/);
  const trComplete = trSource.match(/'services\.mailProfiles\.status\.complete': '([^']+)'/);
  assert.equal(enComplete?.[1], 'Components running');
  assert.equal(trComplete?.[1], 'Bileşenler çalışıyor');
  assert.doesNotMatch(enComplete?.[1] ?? '', /installed|configured|profile ready/i);
  assert.doesNotMatch(trComplete?.[1] ?? '', /kurulu|yapılandırıldı|profil hazır/i);
});

test('mail profile copy stays in EN/TR parity', () => {
  const keys = [
    'services.mailProfiles.title',
    'services.mailProfiles.subtitle',
    'services.mailProfiles.includes',
    'services.mailProfiles.install',
    'services.mailProfiles.continue',
    'services.mailProfiles.repair',
    'services.mailProfiles.unavailable',
    'services.mailProfiles.configureDNS',
    'services.mailProfiles.dnsRequired',
    'services.mailProfiles.dnsRequiredShort',
    'services.mailProfiles.status.unknown',
    'services.mailProfiles.status.available',
    'services.mailProfiles.status.partial',
    'services.mailProfiles.status.complete',
    'services.mailProfiles.status.blocked',
    'services.mailProfiles.profileComponentsNeedRepair',
    'services.mailProfiles.fallbackWarning',
    'services.mailProfiles.plan.title.install',
    'services.mailProfiles.plan.title.continue',
    'services.mailProfiles.plan.title.repair',
    'services.mailProfiles.plan.description',
    'services.mailProfiles.plan.component',
    'services.mailProfiles.plan.version',
    'services.mailProfiles.plan.installed',
    'services.mailProfiles.plan.repositoryCandidate',
    'services.mailProfiles.plan.serviceImpact',
    'services.mailProfiles.plan.restarts',
    'services.mailProfiles.plan.noRestarts',
    'services.mailProfiles.plan.firewallImpact',
    'services.mailProfiles.plan.ports',
    'services.mailProfiles.plan.noPorts',
    'services.mailProfiles.plan.tls',
    'services.mailProfiles.plan.partialProgress',
    'services.mailProfiles.plan.acknowledgement',
    'services.mailProfiles.plan.confirm.install',
    'services.mailProfiles.plan.confirm.continue',
    'services.mailProfiles.plan.confirm.repair',
    'err.mail_profile_install_failed',
    'err.mail_profile_confirmation_required',
    'err.mail_profile_server_hostname_invalid',
    'err.mail_profile_dns_identity_not_ready',
  ];
  for (const key of keys) {
    assert.ok(enSource.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(trSource.includes(`'${key}':`), 'missing TR key ' + key);
  }
});

test('mail profile terminal errors preserve the safe code for the shared localized banner', () => {
  const start = operationSource.indexOf('function operationError(');
  const end = operationSource.indexOf('function restoredOperation(', start);
  assert.ok(start >= 0 && end > start);
  const transport = operationSource.slice(start, end);
  assert.match(transport, /code: typeof raw\.code === 'string' \? raw\.code : undefined/);
  assert.match(operationSource, /finishFailure\(terminalFailure\)/);
  assert.match(operationSource, /<OperationOverlay\s+failure=\{failure\}/);
  assert.match(operationOverlaySource, /<ErrorBanner error=\{props\.failure\}/);
});
